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

func (m *Manager) Prepare(ctx context.Context, area prerequisites.BundleArea, definition prerequisites.Definition, egress prerequisites.SetupEgress, progress func(prerequisites.ProgressEvent)) error {
	record, err := validateDefinition(definition)
	if err != nil {
		return err
	}
	if m == nil || m.fetch == nil || m.probe == nil {
		return bundleFailure("bundle preparation adapters are unavailable")
	}
	// Every phase that can take seconds is announced first, because a silent
	// acquisition, projection or interpreter probe looks identical to a hang.
	report := func(detail string) {
		if progress != nil {
			progress(prerequisites.ProgressEvent{Status: "running", Detail: detail})
		}
	}
	before, entries, err := inspectFiles(ctx, area, record, definition.Tools)
	if err != nil {
		return err
	}
	if !before.Recoverable {
		return bundleFailure("existing bundle content is not attributable to the approved closure")
	}
	if before.Ready {
		report("qualifying the private interpreter")
		return m.probe(ctx, area, definition)
	}
	if err := area.EnsureDirectory(ctx, "sources"); err != nil {
		return err
	}
	projected := projectionFor(record)
	if err := projected.automation(ctx); err != nil {
		return err
	}
	for index, source := range record.Baseline {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := sourcePath(source)
		var data []byte
		if _, found := entries[name]; found {
			data, err = area.Read(ctx, name, int(source.Bytes))
		} else {
			report("acquiring " + path.Base(source.ID) + ", source " + strconv.Itoa(index+1) + " of " + strconv.Itoa(len(record.Baseline)))
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
	if !projected.matches(record.Bootstrap) {
		return bundleFailure("bootstrap projection differs from its frozen file closure")
	}
	report("publishing " + strconv.Itoa(len(projected.files)) + " bundle files")
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
	report("verifying the published bundle")
	after, _, err := inspectFiles(ctx, area, record, definition.Tools)
	if err != nil {
		return err
	}
	if !after.Ready {
		return bundleFailure("published bundle does not match its complete approved projection")
	}
	report("qualifying the private interpreter")
	return m.probe(ctx, area, definition)
}

func validateDefinition(definition prerequisites.Definition) (catalogRecord, error) {
	if definition.Bootstrap != nil {
		if err := prerequisites.ValidateResolvedDefinition(definition); err != nil {
			return catalogRecord{}, err
		}
		bootstrap := definition.Bootstrap
		if bootstrap.AutomationDigest != ansible.Digest() {
			return catalogRecord{}, errors.Join(prerequisites.ErrBootstrapIncompatible, bundleFailure("retained bootstrap automation is incompatible with the current executable"))
		}
		foundation, _, err := compiledCatalog()
		if err != nil {
			return catalogRecord{}, err
		}
		native, ok := selectNative(foundation, bootstrap.Platform)
		expectedExecution := cloneExecution(native.Execution)
		expectedExecution.PythonExecutable = bootstrap.PythonExecutable
		if !ok || !equalExecution(expectedExecution, bootstrap.Execution) || !slices.Equal(bootstrap.ExecutionPackages, executionPackageOwners()) {
			return catalogRecord{}, errors.Join(prerequisites.ErrBootstrapIncompatible, bundleFailure("retained bootstrap requires a different provided execution foundation"))
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
	ready := allBaseline
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
