package bundlelocal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type bundleProbe func(context.Context, prerequisites.BundleArea, prerequisites.Definition) error

// closureRecord is what one resolved definition's bundle holds: the bootstrap
// it projects, the sources that projection is built from, and the native
// packages attributable to it as retained sources.
type closureRecord struct {
	Bootstrap *prerequisites.BootstrapDefinition
	Baseline  []prerequisites.DependencySource
	Native    []prerequisites.NativePackage
}

// Manager has no filesystem authority of its own. Its callbacks receive only
// the invocation-scoped bundle area while Workspace holds coordination.
type Manager struct {
	fetch sourceFetcher
	probe bundleProbe
}

func New(guard prerequisites.PythonExecutionGuard) *Manager {
	return &Manager{fetch: fetchSource, probe: func(ctx context.Context, area prerequisites.BundleArea, definition prerequisites.Definition) error {
		return probeBundle(ctx, guard, area, definition)
	}}
}

func (m *Manager) Inspect(ctx context.Context, area prerequisites.BundleArea, definition prerequisites.Definition, execute bool) (inspection prerequisites.BundleInspection, err error) {
	if area == nil {
		return inspection, bundleFailure("bundle inspection capability is unavailable")
	}
	location, err := area.Location(ctx)
	if err != nil {
		return inspection, err
	}
	defer func() { inspection.Sealed = location.Sealed }()
	record, err := validateDefinition(definition)
	if err != nil {
		return prerequisites.BundleInspection{}, err
	}
	// A sealed bundle was verified byte for byte and probed when it was
	// published; its readiness confirms only that the published files remain.
	if location.Sealed {
		inspection, err = presentFiles(ctx, area, record, definition.Tools)
		return inspection, err
	}
	inspection, _, err = inspectFiles(ctx, area, record, definition.Tools)
	if err != nil || !inspection.Ready || !execute {
		return inspection, err
	}
	if m == nil || m.probe == nil {
		inspection.Ready = false
		return inspection, bundleFailure("bundle execution verifier is unavailable")
	}
	if err := m.probe(ctx, area, definition); err != nil {
		inspection.Ready = false
		return inspection, err
	}
	return inspection, nil
}

// Validate refuses what Inspect and Prepare refuse before they read an area,
// so a definition whose bundle holds no area yet is judged all the same.
func (m *Manager) Validate(definition prerequisites.Definition) error {
	_, err := validateDefinition(definition)
	return err
}

// presentFiles is the presence check for a sealed bundle: the retained sources
// by size, the published projection by file count and total bytes, the
// collection documentation that count leaves out, the private interpreter,
// and each target tool's source and files. It reads no bytes.
func presentFiles(ctx context.Context, area prerequisites.BundleArea, record closureRecord, tools []prerequisites.ToolDefinition) (prerequisites.BundleInspection, error) {
	if err := ctx.Err(); err != nil {
		return prerequisites.BundleInspection{}, err
	}
	listed, err := area.Entries(ctx)
	if err != nil {
		return prerequisites.BundleInspection{}, err
	}
	entries := make(map[string]prerequisites.BundleEntry, len(listed))
	for _, entry := range listed {
		entries[entry.Path] = entry
	}
	present := func(name string, size int64) bool {
		entry, found := entries[name]
		return found && !entry.Directory && (size < 0 || entry.Size == size)
	}
	ready := present(record.Bootstrap.PythonExecutable, -1) && entries[record.Bootstrap.PythonExecutable].Executable
	for _, source := range record.Baseline {
		ready = ready && present(sourcePath(source), source.Bytes)
	}
	toolsReady := true
	for _, tool := range tools {
		toolsReady = toolsReady && present(sourcePath(tool.Source), tool.Source.Bytes)
		for _, file := range tool.Files {
			toolsReady = toolsReady && present(file.Path, -1)
		}
	}
	documentation := documentationPaths()
	files, size := projectedTotals(entries, documentation)
	ready = ready && documentationPresent(entries, documentation) && files == record.Bootstrap.FileCount && size == record.Bootstrap.ExpandedBytes
	return prerequisites.BundleInspection{Ready: ready, ToolsReady: toolsReady, Recoverable: true}, nil
}

// projectedTotals counts the published projection's files and bytes the way
// its identity does: without the retained sources, the target tools or the
// collection documentation.
func projectedTotals(entries map[string]prerequisites.BundleEntry, documentation []string) (int, int64) {
	files, size := 0, int64(0)
	for name, entry := range entries {
		if entry.Directory || strings.HasPrefix(name, "sources/") || strings.HasPrefix(name, "tools/") || slices.Contains(documentation, name) {
			continue
		}
		files++
		size += entry.Size
	}
	return files, size
}

// documentationPresent reports whether the area holds every documentation
// path as attributable documentation.
func documentationPresent(entries map[string]prerequisites.BundleEntry, documentation []string) bool {
	for _, name := range documentation {
		entry, found := entries[name]
		if !found || !attributableDocumentation(entry) {
			return false
		}
	}
	return true
}

// attributableDocumentation admits collection documentation an area holds at
// its projected path: a regular, non-executable file within the member bound.
// Its size and bytes are never compared, because documentation leaves the
// automation digest, so a bundle published under an earlier release's README
// still carries this executable's automation.
func attributableDocumentation(entry prerequisites.BundleEntry) bool {
	return !entry.Directory && !entry.Executable && entry.Size >= 0 && entry.Size <= maxMemberBytes
}

// retainedSource reads one approved source from a sealed area this host already
// holds. A missing, unreadable or unexpected entry is not a failure: the caller
// acquires that source from its publisher exactly as it otherwise would.
func retainedSource(ctx context.Context, retained prerequisites.BundleArea, source prerequisites.DependencySource) []byte {
	if retained == nil || source.Bytes <= 0 || source.Bytes > maxMemberBytes {
		return nil
	}
	data, err := retained.Read(ctx, sourcePath(source), int(source.Bytes))
	if err != nil || !approvedBytes(source, data) {
		return nil
	}
	return data
}

// Prepare returns the verification of what it published, so the complete
// closure is read back and the private interpreter qualified exactly once. The
// caller decides on that evidence rather than repeating the whole pass.
func (m *Manager) Prepare(ctx context.Context, area, retained prerequisites.BundleArea, definition prerequisites.Definition, egress prerequisites.SetupEgress, progress func(prerequisites.ProgressEvent)) (prerequisites.BundleInspection, error) {
	record, err := validateDefinition(definition)
	if err != nil {
		return prerequisites.BundleInspection{}, err
	}
	if m == nil || m.fetch == nil || m.probe == nil {
		return prerequisites.BundleInspection{}, bundleFailure("bundle preparation adapters are unavailable")
	}
	// Every phase that can take seconds is announced first, because a silent
	// acquisition, projection or interpreter probe looks identical to a hang.
	report := func(detail string) {
		if progress != nil {
			progress(prerequisites.ProgressEvent{Status: "running", Detail: detail})
		}
	}
	// Acquisition is the one phase whose remaining work is known before it
	// starts, so it reports how many of its sources it has published.
	acquiring := func(detail string, completed int) {
		if progress != nil {
			progress(prerequisites.ProgressEvent{Status: "running", Detail: detail, Completed: completed, Declared: len(record.Baseline)})
		}
	}
	before, entries, err := inspectFiles(ctx, area, record, definition.Tools)
	if err != nil {
		return prerequisites.BundleInspection{}, err
	}
	if !before.Recoverable {
		return prerequisites.BundleInspection{}, bundleFailure("existing bundle content is not attributable to the approved closure")
	}
	if before.Ready {
		// An earlier attempt published this closure. Its bytes were just read
		// back, so only the interpreter still has to prove that it runs.
		report("qualifying the private interpreter")
		if err := m.probe(ctx, area, definition); err != nil {
			return prerequisites.BundleInspection{}, err
		}
		return before, nil
	}
	if err := area.EnsureDirectory(ctx, "sources"); err != nil {
		return prerequisites.BundleInspection{}, err
	}
	projected := projectionFor(record)
	if err := projected.automation(ctx); err != nil {
		return prerequisites.BundleInspection{}, err
	}
	if err := m.acquireSources(ctx, area, retained, record, entries, projected, egress, acquiring); err != nil {
		return prerequisites.BundleInspection{}, err
	}
	if !projected.matches(record.Bootstrap) {
		return prerequisites.BundleInspection{}, bundleFailure("bootstrap projection differs from its frozen file closure")
	}
	report("publishing " + strconv.Itoa(len(projected.files)) + " bundle files")
	if err := publishProjection(ctx, area, projected, entries); err != nil {
		return prerequisites.BundleInspection{}, err
	}
	if err := publishDocumentation(ctx, area, projected, entries); err != nil {
		return prerequisites.BundleInspection{}, err
	}
	report("verifying the published bundle")
	after, _, err := inspectFiles(ctx, area, record, definition.Tools)
	if err != nil {
		return prerequisites.BundleInspection{}, err
	}
	if !after.Ready {
		return after, bundleFailure("published bundle does not match its complete approved projection")
	}
	report("qualifying the private interpreter")
	if err := m.probe(ctx, area, definition); err != nil {
		return prerequisites.BundleInspection{}, err
	}
	return after, nil
}

func (m *Manager) acquireSources(ctx context.Context, area, retained prerequisites.BundleArea, record closureRecord, entries map[string]prerequisites.BundleEntry, projected *projection, egress prerequisites.SetupEgress, acquiring func(string, int)) error {
	for index, source := range record.Baseline {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := sourcePath(source)
		var data []byte
		var err error
		if _, found := entries[name]; found {
			data, err = area.Read(ctx, name, int(source.Bytes))
		} else if held := retainedSource(ctx, retained, source); held != nil {
			acquiring("recovering "+path.Base(source.ID), index)
			data, err = held, nil
		} else {
			acquiring("acquiring "+path.Base(source.ID), index)
			data, err = m.fetch(ctx, source, egress)
		}
		if err != nil {
			return err
		}
		if !approvedBytes(source, data) {
			return bundleFailure("dependency source changed before bundle publication")
		}
		if err := projectSource(ctx, projected, index, data); err != nil {
			return err
		}
		if _, found := entries[name]; !found {
			// Retain verified publisher bytes before any derived file. Exact
			// retry can then reconstruct every attributable partial projection.
			if err := area.Write(ctx, name, data, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func publishProjection(ctx context.Context, area prerequisites.BundleArea, projected *projection, entries map[string]prerequisites.BundleEntry) error {
	for _, name := range projected.directories() {
		if err := area.EnsureDirectory(ctx, name); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(projected.files))
	for name := range projected.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		file := projected.files[name]
		if existing, found := entries[name]; found {
			data, err := area.Read(ctx, name, len(file.data))
			if err != nil {
				return err
			}
			if existing.Directory || existing.Executable != file.executable || !bytes.Equal(data, file.data) {
				return bundleFailure("existing bundle file changed during preparation")
			}
			continue
		}
		if err := area.Write(ctx, name, file.data, file.executable); err != nil {
			return err
		}
	}
	return nil
}

// publishDocumentation writes the collection documentation the area lacks,
// after the projected files. Attributable documentation the area already holds
// stays, whatever release wrote it, because no check compares its bytes.
func publishDocumentation(ctx context.Context, area prerequisites.BundleArea, projected *projection, entries map[string]prerequisites.BundleEntry) error {
	for _, name := range projected.documentationNames() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if existing, found := entries[name]; found {
			if !attributableDocumentation(existing) {
				return bundleFailure("existing bundle file changed during preparation")
			}
			continue
		}
		if err := area.Write(ctx, name, projected.documentation[name], false); err != nil {
			return err
		}
	}
	return nil
}

// validateDefinition admits only a resolved definition: a bundle holds the
// closure one resolution froze, and no other identity names a bundle.
func validateDefinition(definition prerequisites.Definition) (closureRecord, error) {
	if err := prerequisites.ValidateResolvedDefinition(definition); err != nil {
		return closureRecord{}, err
	}
	bootstrap := definition.Bootstrap
	// The provided execution profile is checked first because no local
	// reprojection can repair it, so a resolution failing both must not be
	// reported as the automation revision a host can settle by itself.
	if err := qualifiedFoundation(bootstrap); err != nil {
		return closureRecord{}, err
	}
	if bootstrap.AutomationDigest != ansible.Digest() {
		return closureRecord{}, errors.Join(prerequisites.ErrBootstrapIncompatible, prerequisites.ErrAutomationSuperseded, bundleFailure("retained bootstrap automation is superseded by the current executable"))
	}
	return closureRecord{Bootstrap: bootstrap, Baseline: slices.Clone(bootstrap.Sources), Native: slices.Clone(definition.Native.Packages)}, nil
}

// qualifiedFoundation admits a retained bootstrap against the provided
// execution profile this executable was compiled against. That profile is host
// evidence rather than retained content, so a mismatch is a plain
// incompatibility no reprojection can settle.
func qualifiedFoundation(bootstrap *prerequisites.BootstrapDefinition) error {
	foundation, err := compiledCatalog()
	if err != nil {
		return err
	}
	native, ok := selectNative(foundation, bootstrap.Platform)
	expected := cloneExecution(native.Execution)
	expected.PythonExecutable = bootstrap.PythonExecutable
	if !ok || !equalExecution(expected, bootstrap.Execution) || !slices.Equal(bootstrap.ExecutionPackages, executionPackageOwners()) {
		return errors.Join(prerequisites.ErrBootstrapIncompatible, bundleFailure("retained bootstrap requires a different provided execution foundation"))
	}
	return nil
}

// Rebase carries a retained resolution onto the automation this executable
// embeds. It reads that resolution's own approved sources from the sealed area
// holding them and projects them again, so every release, byte count, signer
// and publisher origin survives unchanged and nothing is acquired. Only the
// automation digest and the projection identity it produces may differ. A
// source the area cannot serve as its approved bytes returns
// ErrRetainedSourceUnavailable; a canceled read returns the cancellation.
func (m *Manager) Rebase(ctx context.Context, area prerequisites.BundleArea, retained prerequisites.BootstrapDefinition) (prerequisites.BootstrapDefinition, error) {
	if area == nil {
		return prerequisites.BootstrapDefinition{}, bundleFailure("retained bundle inspection capability is unavailable")
	}
	if err := prerequisites.ValidateBootstrap(retained); err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	if err := qualifiedFoundation(&retained); err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	projected := newProjection()
	projected.site = retained.SitePackages
	if err := projected.automation(ctx); err != nil {
		return prerequisites.BootstrapDefinition{}, err
	}
	for index, source := range retained.Sources {
		if err := ctx.Err(); err != nil {
			return prerequisites.BootstrapDefinition{}, err
		}
		data, err := area.Read(ctx, sourcePath(source), int(source.Bytes))
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return prerequisites.BootstrapDefinition{}, ctxErr
			}
			return prerequisites.BootstrapDefinition{}, errors.Join(prerequisites.ErrRetainedSourceUnavailable, bundleFailure("retained dependency source cannot be read"))
		}
		if !approvedBytes(source, data) {
			return prerequisites.BootstrapDefinition{}, errors.Join(prerequisites.ErrRetainedSourceUnavailable, bundleFailure("retained dependency source differs from its approved identity"))
		}
		if err := projectSource(ctx, projected, index, data); err != nil {
			return prerequisites.BootstrapDefinition{}, err
		}
	}
	if _, found := projected.files[retained.PythonExecutable]; !found {
		return prerequisites.BootstrapDefinition{}, bundleFailure("retained Python projection lacks its declared executable")
	}
	retained.AutomationDigest = ansible.Digest()
	retained.ProjectionSHA256, retained.FileCount, retained.ExpandedBytes = projected.identity(), len(projected.files), projected.bytes
	return prerequisites.CanonicalBootstrap(retained)
}

func inspectFiles(ctx context.Context, area prerequisites.BundleArea, record closureRecord, selectedTools ...[]prerequisites.ToolDefinition) (prerequisites.BundleInspection, map[string]prerequisites.BundleEntry, error) {
	entries, err := bundleInventory(ctx, area)
	if err != nil {
		return prerequisites.BundleInspection{}, nil, err
	}
	projected := projectionFor(record)
	if err := projected.automation(ctx); err != nil {
		return prerequisites.BundleInspection{}, entries, err
	}
	expected := make(map[string]projectedFile)
	allBaseline := true
	for index, source := range record.Baseline {
		name := sourcePath(source)
		entry, found := entries[name]
		if !found {
			allBaseline = false
			continue
		}
		if entry.Directory || entry.Executable || entry.Size != source.Bytes {
			return prerequisites.BundleInspection{}, entries, nil
		}
		data, err := area.Read(ctx, name, int(source.Bytes))
		if err != nil {
			return prerequisites.BundleInspection{}, entries, err
		}
		if !approvedBytes(source, data) {
			return prerequisites.BundleInspection{}, entries, nil
		}
		expected[name] = projectedFile{data: data}
		if err := projectSource(ctx, projected, index, data); err != nil {
			return prerequisites.BundleInspection{}, entries, err
		}
	}
	for _, pkg := range record.Native {
		name := sourcePath(pkg.Source)
		entry, found := entries[name]
		if !found {
			continue
		}
		if entry.Directory || entry.Executable || entry.Size != pkg.Source.Bytes {
			return prerequisites.BundleInspection{}, entries, nil
		}
		data, err := area.Read(ctx, name, int(pkg.Source.Bytes))
		if err != nil {
			return prerequisites.BundleInspection{}, entries, err
		}
		if !approvedBytes(pkg.Source, data) {
			return prerequisites.BundleInspection{}, entries, nil
		}
		expected[name] = projectedFile{data: data}
	}
	for name, file := range projected.files {
		expected[name] = file
	}
	directories := map[string]bool{"sources": true}
	for _, name := range projected.directories() {
		directories[name] = true
	}
	if len(selectedTools) > 1 {
		return prerequisites.BundleInspection{}, entries, bundleFailure("target tool inspection has ambiguous closure inputs")
	}
	var selected []prerequisites.ToolDefinition
	if len(selectedTools) == 1 {
		selected = selectedTools[0]
	}
	tools, err := inspectTools(ctx, area, selected, entries, directories, expected)
	if err != nil || !tools.consistent {
		return prerequisites.BundleInspection{}, entries, err
	}
	attributable, err := entriesAttributable(ctx, area, entries, projected, expected, directories, tools)
	if err != nil || !attributable {
		return prerequisites.BundleInspection{}, entries, err
	}
	if allBaseline && !projected.matches(record.Bootstrap) {
		return prerequisites.BundleInspection{}, entries, bundleFailure("retained bootstrap sources differ from the frozen projection")
	}
	ready := allBaseline && documentationPresent(entries, projected.documentationNames())
	for name := range projected.files {
		_, exists := entries[name]
		ready = ready && exists
	}
	if err := area.Verify(ctx); err != nil {
		return prerequisites.BundleInspection{}, entries, err
	}
	return prerequisites.BundleInspection{Ready: ready, ToolsReady: tools.ready, Recoverable: true}, entries, nil
}

func bundleInventory(ctx context.Context, area prerequisites.BundleArea) (map[string]prerequisites.BundleEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if area == nil {
		return nil, bundleFailure("bundle storage capability is unavailable")
	}
	if err := area.Verify(ctx); err != nil {
		return nil, err
	}
	listed, err := area.Entries(ctx)
	if err != nil {
		return nil, err
	}
	if len(listed) > maxArchiveEntries {
		return nil, bundleFailure("bundle entry inventory exceeds the qualified limit")
	}
	entries := make(map[string]prerequisites.BundleEntry, len(listed))
	for _, entry := range listed {
		if _, duplicate := entries[entry.Path]; duplicate || !validPath(entry.Path) {
			return nil, bundleFailure("bundle inventory contains invalid or duplicate entries")
		}
		entries[entry.Path] = entry
	}
	return entries, nil
}

func entriesAttributable(ctx context.Context, area prerequisites.BundleArea, entries map[string]prerequisites.BundleEntry, projected *projection, expected map[string]projectedFile, directories map[string]bool, tools toolInspection) (bool, error) {
	for name, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if _, documented := projected.documentation[name]; documented {
			if !attributableDocumentation(entry) {
				return false, nil
			}
			continue
		}
		if entry.Directory {
			if !directories[name] {
				return false, nil
			}
			continue
		}
		if tool, streamed := tools.files[name]; streamed {
			matches, err := tools.matches(ctx, name, entry, tool)
			if err != nil || !matches {
				return false, err
			}
			continue
		}
		file, found := expected[name]
		if !found || entry.Executable != file.executable || entry.Size != int64(len(file.data)) {
			return false, nil
		}
		// Retained source bytes were already read and checked.
		if strings.HasPrefix(name, "sources/") {
			continue
		}
		data, err := area.Read(ctx, name, len(file.data))
		if err != nil {
			return false, err
		}
		if !bytes.Equal(data, file.data) {
			return false, nil
		}
	}
	return true, nil
}

type toolInspection struct {
	stream     prerequisites.BundleStream
	files      map[string]streamedFile
	ready      bool
	consistent bool
}

// inspectTools streams each selected tool's retained source once through its
// projection, so inspection holds of a tool only the digests it streamed.
func inspectTools(ctx context.Context, area prerequisites.BundleArea, tools []prerequisites.ToolDefinition, entries map[string]prerequisites.BundleEntry, directories map[string]bool, expected map[string]projectedFile) (toolInspection, error) {
	inspection := toolInspection{files: map[string]streamedFile{}, ready: true, consistent: true}
	if len(tools) == 0 {
		return inspection, nil
	}
	stream, streams := area.(prerequisites.BundleStream)
	if !streams {
		return inspection, bundleFailure("bundle streaming capability is unavailable")
	}
	inspection.stream = stream
	var sourceBytes, expandedBytes int64
	for _, tool := range tools {
		if err := validateFrozenTool(tool); err != nil {
			return inspection, err
		}
		if tool.Source.Bytes <= 0 || tool.Source.Bytes > maxToolSourceBytes || sourceBytes > maxToolExpandedBytes-tool.Source.Bytes {
			return inspection, bundleFailure("target source closure exceeds its bounded inspection size")
		}
		sourceBytes += tool.Source.Bytes
		for _, file := range tool.Files {
			for directory := path.Dir(file.Path); directory != "."; directory = path.Dir(directory) {
				directories[directory] = true
			}
		}
		name := sourcePath(tool.Source)
		entry, found := entries[name]
		if !found {
			inspection.ready = false
			continue
		}
		if entry.Directory || entry.Executable || entry.Size != tool.Source.Bytes {
			inspection.consistent = false
			return inspection, nil
		}
		var files map[string]streamedFile
		if err := stream.Stream(ctx, name, tool.Source.Bytes, func(source prerequisites.BundleReader) (err error) {
			files, err = projectTool(ctx, tool, source)
			return err
		}); err != nil {
			return inspection, err
		}
		inspection.files[name] = streamedFile{sha256: tool.Source.SHA256, size: tool.Source.Bytes}
		for member, file := range files {
			_, projected := expected[member]
			_, repeated := inspection.files[member]
			if projected || repeated || expandedBytes > maxToolExpandedBytes-file.size {
				return inspection, bundleFailure("target executable closure conflicts or exceeds its bounded size")
			}
			expandedBytes += file.size
			inspection.files[member] = file
			_, exists := entries[member]
			inspection.ready = inspection.ready && exists
		}
	}
	return inspection, nil
}

// matches reports whether a published tool entry is the streamed file. A
// retained source was streamed whole by inspectTools, so only its metadata is
// compared again.
func (inspection toolInspection) matches(ctx context.Context, name string, entry prerequisites.BundleEntry, file streamedFile) (bool, error) {
	if entry.Executable != file.executable || entry.Size != file.size {
		return false, nil
	}
	if strings.HasPrefix(name, "sources/") {
		return true, nil
	}
	digest := sha256.New()
	if err := inspection.stream.Stream(ctx, name, file.size, func(published prerequisites.BundleReader) error {
		_, err := io.Copy(digest, contextReader{ctx: ctx, reader: published})
		return err
	}); err != nil {
		return false, err
	}
	return hex.EncodeToString(digest.Sum(nil)) == file.sha256, nil
}

func projectSource(ctx context.Context, projected *projection, index int, data []byte) error {
	if index == 0 {
		return projected.archive(ctx, data)
	}
	return projected.wheel(ctx, data)
}

func sourcePath(source prerequisites.DependencySource) string { return path.Join("sources", source.ID) }
