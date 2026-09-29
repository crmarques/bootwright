package bundlelocal

import (
	"bytes"
	"context"
	"errors"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type bundleProbe func(context.Context, prerequisites.BundleArea, prerequisites.Definition) error

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
	if location.Sealed && record.Bootstrap != nil {
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

// presentFiles is the presence check for a sealed bundle: the retained sources
// by size, the published projection by file count and total bytes, the
// collection documentation that count leaves out, the private interpreter,
// and each target tool's source and files. It reads no bytes.
func presentFiles(ctx context.Context, area prerequisites.BundleArea, record catalogRecord, tools []prerequisites.ToolDefinition) (prerequisites.BundleInspection, error) {
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
	for index, source := range record.Baseline {
		if err := ctx.Err(); err != nil {
			return prerequisites.BundleInspection{}, err
		}
		name := sourcePath(source)
		var data []byte
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
			return prerequisites.BundleInspection{}, err
		}
		if !approvedBytes(source, data) {
			return prerequisites.BundleInspection{}, bundleFailure("dependency source changed before bundle publication")
		}
		if err := projectSource(ctx, projected, index, data); err != nil {
			return prerequisites.BundleInspection{}, err
		}
		if _, found := entries[name]; !found {
			// Retain verified publisher bytes before any derived file. Exact
			// retry can then reconstruct every attributable partial projection.
			if err := area.Write(ctx, name, data, false); err != nil {
				return prerequisites.BundleInspection{}, err
			}
		}
	}
	if !projected.matches(record.Bootstrap) {
		return prerequisites.BundleInspection{}, bundleFailure("bootstrap projection differs from its frozen file closure")
	}
	report("publishing " + strconv.Itoa(len(projected.files)) + " bundle files")
	for _, name := range projected.directories() {
		if err := area.EnsureDirectory(ctx, name); err != nil {
			return prerequisites.BundleInspection{}, err
		}
	}
	names := make([]string, 0, len(projected.files))
	for name := range projected.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return prerequisites.BundleInspection{}, err
		}
		file := projected.files[name]
		if existing, found := entries[name]; found {
			data, err := area.Read(ctx, name, len(file.data))
			if err != nil {
				return prerequisites.BundleInspection{}, err
			}
			if existing.Directory || existing.Executable != file.executable || !bytes.Equal(data, file.data) {
				return prerequisites.BundleInspection{}, bundleFailure("existing bundle file changed during preparation")
			}
			continue
		}
		if err := area.Write(ctx, name, file.data, file.executable); err != nil {
			return prerequisites.BundleInspection{}, err
		}
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

func validateDefinition(definition prerequisites.Definition) (catalogRecord, error) {
	if definition.Bootstrap != nil {
		if err := prerequisites.ValidateResolvedDefinition(definition); err != nil {
			return catalogRecord{}, err
		}
		bootstrap := definition.Bootstrap
		// The provided execution profile is checked first because no local
		// reprojection can repair it, so a resolution failing both must not be
		// reported as the automation revision a host can settle by itself.
		if err := qualifiedFoundation(bootstrap); err != nil {
			return catalogRecord{}, err
		}
		if bootstrap.AutomationDigest != ansible.Digest() {
			return catalogRecord{}, errors.Join(prerequisites.ErrBootstrapIncompatible, prerequisites.ErrAutomationSuperseded, bundleFailure("retained bootstrap automation is superseded by the current executable"))
		}
		record := catalogRecord{Bootstrap: bootstrap, PythonVersion: bootstrap.PythonVersion, AnsibleVersion: bootstrap.AnsibleVersion, Baseline: slices.Clone(bootstrap.Sources)}
		if definition.Native != nil {
			record.Native = []nativeRecord{{Packages: slices.Clone(definition.Native.Packages)}}
		}
		return record, nil
	}
	record, _, err := compiledCatalog()
	if err != nil {
		return record, err
	}
	for _, tool := range definition.Tools {
		if err := validateFrozenTool(tool); err != nil {
			return record, err
		}
	}
	for _, native := range record.Native {
		platform := prerequisites.Platform{OS: native.OS, Release: native.Release, Architecture: "amd64"}
		expected, err := (Catalog{}).Select(platform, definition.NativeRequirements)
		if err != nil {
			continue
		}
		if definition.BaseCatalogDigest != "" || len(definition.Tools) != 0 {
			expected, err = prerequisites.WithTools(expected, definition.Tools)
			if err != nil {
				return record, err
			}
		}
		if !equalDefinition(expected, definition) {
			continue
		}
		// Only sources from this selected native closure are attributable to
		// the bundle. Other platforms and unselected optional packages remain
		// unexpected content even when present in the compiled catalog.
		native.Packages, err = selectedNativePackages(native, definition.NativeRequirements)
		if err != nil {
			return record, err
		}
		record.Native = []nativeRecord{native}
		return record, nil
	}
	return record, bundleFailure("bundle definition differs from its exact compiled native and frozen target tool closure")
}

// qualifiedFoundation admits a retained bootstrap against the provided
// execution profile this executable was compiled against. That profile is host
// evidence rather than retained content, so a mismatch is a plain
// incompatibility no reprojection can settle.
func qualifiedFoundation(bootstrap *prerequisites.BootstrapDefinition) error {
	foundation, _, err := compiledCatalog()
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
// automation digest and the projection identity it produces may differ.
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
			return prerequisites.BootstrapDefinition{}, err
		}
		if !approvedBytes(source, data) {
			return prerequisites.BootstrapDefinition{}, bundleFailure("retained dependency source differs from its approved identity")
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

func equalDefinition(a, b prerequisites.Definition) bool {
	return a.CatalogDigest == b.CatalogDigest && a.BaseCatalogDigest == b.BaseCatalogDigest &&
		a.NativeRequirements == b.NativeRequirements && a.PythonVersion == b.PythonVersion && a.AnsibleVersion == b.AnsibleVersion &&
		slices.Equal(a.Sources, b.Sources) && equalExecution(a.Execution, b.Execution) && equalRuntime(a.Runtime, b.Runtime) &&
		slices.EqualFunc(a.Tools, b.Tools, func(a, b prerequisites.ToolDefinition) bool {
			return a.Kind == b.Kind && a.Version == b.Version && a.Compatibility == b.Compatibility &&
				a.Archive == b.Archive && a.Source == b.Source && slices.Equal(a.Files, b.Files)
		})
}

func equalRuntime(a, b prerequisites.RuntimeRequirement) bool {
	return a.Version == b.Version && a.LockPath == b.LockPath && a.SELinuxMode == b.SELinuxMode &&
		slices.Equal(a.Files, b.Files) && slices.Equal(a.Links, b.Links)
}

func inspectFiles(ctx context.Context, area prerequisites.BundleArea, record catalogRecord, selectedTools ...[]prerequisites.ToolDefinition) (prerequisites.BundleInspection, map[string]prerequisites.BundleEntry, error) {
	if err := ctx.Err(); err != nil {
		return prerequisites.BundleInspection{}, nil, err
	}
	if area == nil {
		return prerequisites.BundleInspection{}, nil, bundleFailure("bundle storage capability is unavailable")
	}
	if err := area.Verify(ctx); err != nil {
		return prerequisites.BundleInspection{}, nil, err
	}
	listed, err := area.Entries(ctx)
	if err != nil {
		return prerequisites.BundleInspection{}, nil, err
	}
	if len(listed) > maxArchiveEntries {
		return prerequisites.BundleInspection{}, nil, bundleFailure("bundle entry inventory exceeds the qualified limit")
	}
	entries := make(map[string]prerequisites.BundleEntry, len(listed))
	for _, entry := range listed {
		if _, duplicate := entries[entry.Path]; duplicate || !validPath(entry.Path) {
			return prerequisites.BundleInspection{}, nil, bundleFailure("bundle inventory contains invalid or duplicate entries")
		}
		entries[entry.Path] = entry
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
	for _, native := range record.Native {
		for _, pkg := range native.Packages {
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
	}
	for name, file := range projected.files {
		expected[name] = file
	}
	directories := map[string]bool{"sources": true}
	for _, name := range projected.directories() {
		directories[name] = true
	}
	toolsReady := true
	var toolBytes, toolExpanded int64
	if len(selectedTools) > 1 {
		return prerequisites.BundleInspection{}, entries, bundleFailure("target tool inspection has ambiguous closure inputs")
	}
	if len(selectedTools) == 1 {
		for _, tool := range selectedTools[0] {
			if err := validateFrozenTool(tool); err != nil {
				return prerequisites.BundleInspection{}, entries, err
			}
			if tool.Source.Bytes <= 0 || tool.Source.Bytes > maxToolSourceBytes || toolBytes > maxToolExpandedBytes-tool.Source.Bytes {
				return prerequisites.BundleInspection{}, entries, bundleFailure("target source closure exceeds its bounded inspection size")
			}
			toolBytes += tool.Source.Bytes
			for _, file := range tool.Files {
				for directory := path.Dir(file.Path); directory != "."; directory = path.Dir(directory) {
					directories[directory] = true
				}
			}
			name := sourcePath(tool.Source)
			entry, found := entries[name]
			if !found {
				toolsReady = false
				continue
			}
			if entry.Directory || entry.Executable || entry.Size != tool.Source.Bytes {
				return prerequisites.BundleInspection{}, entries, nil
			}
			data, err := area.Read(ctx, name, int(tool.Source.Bytes))
			if err != nil {
				return prerequisites.BundleInspection{}, entries, err
			}
			files, err := projectTool(ctx, tool, data)
			if err != nil {
				return prerequisites.BundleInspection{}, entries, err
			}
			expected[name] = projectedFile{data: data}
			for name, file := range files {
				if _, exists := expected[name]; exists || toolExpanded > maxToolExpandedBytes-int64(len(file.data)) {
					return prerequisites.BundleInspection{}, entries, bundleFailure("target executable closure conflicts or exceeds its bounded size")
				}
				toolExpanded += int64(len(file.data))
				expected[name] = file
				_, exists := entries[name]
				toolsReady = toolsReady && exists
			}
		}
	}
	for name, entry := range entries {
		if err := ctx.Err(); err != nil {
			return prerequisites.BundleInspection{}, entries, err
		}
		if _, documented := projected.documentation[name]; documented {
			if !attributableDocumentation(entry) {
				return prerequisites.BundleInspection{}, entries, nil
			}
			continue
		}
		if entry.Directory {
			if !directories[name] {
				return prerequisites.BundleInspection{}, entries, nil
			}
			continue
		}
		file, found := expected[name]
		if !found || entry.Executable != file.executable || entry.Size != int64(len(file.data)) {
			return prerequisites.BundleInspection{}, entries, nil
		}
		// Retained source bytes were just read and checked above.
		if strings.HasPrefix(name, "sources/") {
			continue
		}
		data, err := area.Read(ctx, name, len(file.data))
		if err != nil {
			return prerequisites.BundleInspection{}, entries, err
		}
		if !bytes.Equal(data, file.data) {
			return prerequisites.BundleInspection{}, entries, nil
		}
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
	return prerequisites.BundleInspection{Ready: ready, ToolsReady: toolsReady, Recoverable: true}, entries, nil
}

func projectSource(ctx context.Context, projected *projection, index int, data []byte) error {
	if index == 0 {
		return projected.archive(ctx, data)
	}
	return projected.wheel(ctx, data)
}

func sourcePath(source prerequisites.DependencySource) string { return path.Join("sources", source.ID) }
