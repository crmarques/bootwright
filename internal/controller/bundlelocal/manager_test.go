package bundlelocal

import (
	"context"
	"errors"
	"github.com/crmarques/bootwright/ansible"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// resolvedDefinitionFixture is a complete Fedora resolution under the
// automation this executable embeds.
func resolvedDefinitionFixture(t *testing.T) prerequisites.Definition {
	t.Helper()
	return pinnedResolution(t, fedoraPlatform, ansible.Digest())
}

func TestBootstrapIncompatibilityNeverMasksCorruptResolution(t *testing.T) {
	for _, change := range []string{"automation", "execution", "corruption"} {
		t.Run(change, func(t *testing.T) {
			definition := resolvedDefinitionFixture(t)
			if change == "corruption" {
				definition.Bootstrap.Sources[0].SHA256 = strings.Repeat("f", 64)
			} else {
				if change == "automation" {
					definition.Bootstrap.AutomationDigest = strings.Repeat("f", 64)
				} else {
					definition.Bootstrap.Execution.Files[0].SHA256 = strings.Repeat("f", 64)
				}
				bootstrap, err := prerequisites.CanonicalBootstrap(*definition.Bootstrap)
				if err != nil {
					t.Fatal(err)
				}
				definition, err = prerequisites.NewResolvedDefinition(bootstrap, *definition.Native)
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := validateDefinition(definition)
			if err == nil || errors.Is(err, prerequisites.ErrBootstrapIncompatible) != (change != "corruption") {
				t.Fatalf("wrong incompatibility boundary: %v", err)
			}
			// Only a resolution this host can reproject names itself as one. A
			// changed provided foundation is checked first precisely so that a
			// resolution failing both is never offered as settleable.
			if errors.Is(err, prerequisites.ErrAutomationSuperseded) != (change == "automation") {
				t.Fatalf("automation supersession claimed for a %s change: %v", change, err)
			}
		})
	}
}

// Validate needs no area and refuses what Prepare refuses before it reads one,
// so a pending receipt whose bundle holds no area is judged before any effect.
func TestValidateRefusesWithoutAnAreaWhatPrepareRefuses(t *testing.T) {
	manager := New(nil)
	current := resolvedDefinitionFixture(t)
	if err := manager.Validate(current); err != nil {
		t.Fatalf("the current resolution was refused: %v", err)
	}
	moved := *current.Bootstrap
	moved.AutomationDigest = strings.Repeat("f", 64)
	bootstrap, err := prerequisites.CanonicalBootstrap(moved)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := prerequisites.NewResolvedDefinition(bootstrap, *current.Native)
	if err != nil {
		t.Fatal(err)
	}
	refused := manager.Validate(definition)
	_, prepared := manager.Prepare(context.Background(), nil, nil, definition, prerequisites.SetupEgress{}, nil)
	for _, err := range []error{refused, prepared} {
		if !errors.Is(err, prerequisites.ErrBootstrapIncompatible) || !errors.Is(err, prerequisites.ErrAutomationSuperseded) {
			t.Fatalf("a resolution under moved automation: validate %v, prepare %v", refused, prepared)
		}
	}
}

type sealedArea struct {
	entries []prerequisites.BundleEntry
	reads   int
}

func (a *sealedArea) Read(context.Context, string, int) ([]byte, error) {
	a.reads++
	return nil, errors.New("sealed readiness must not read bundle bytes")
}
func (*sealedArea) Write(context.Context, string, []byte, bool) error { return errors.New("sealed") }
func (*sealedArea) EnsureDirectory(context.Context, string) error     { return errors.New("sealed") }
func (*sealedArea) Verify(context.Context) error                      { return nil }
func (a *sealedArea) Entries(context.Context) ([]prerequisites.BundleEntry, error) {
	return slices.Clone(a.entries), nil
}
func (*sealedArea) Location(context.Context) (prerequisites.BundleLocation, error) {
	return prerequisites.BundleLocation{Path: "/sealed", Device: 1, Inode: 1, Sealed: true}, nil
}

// A sealed bundle was verified byte for byte and probed when it was published,
// so readiness is presence only: it reads no bytes and launches no interpreter.
// Documentation must be present but, like the projection identity, the file
// count and byte total leave it out, so a README of any size is admitted.
func TestSealedBundleReadinessIsPresenceOnly(t *testing.T) {
	resolved := resolvedDefinitionFixture(t)
	projected := *resolved.Bootstrap
	projected.FileCount, projected.ExpandedBytes = 1, 1
	bootstrap, err := prerequisites.CanonicalBootstrap(projected)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := prerequisites.NewResolvedDefinition(bootstrap, *resolved.Native)
	if err != nil {
		t.Fatal(err)
	}
	readme := "automation/collections/ansible_collections/bootwright/core/README.md"
	changelog := "automation/collections/ansible_collections/bootwright/core/CHANGELOG.rst"
	entries := []prerequisites.BundleEntry{
		{Path: "python", Directory: true}, {Path: "python/bin", Directory: true}, {Path: definition.Bootstrap.PythonExecutable, Executable: true, Size: 1},
		{Path: readme, Size: 4096}, {Path: changelog, Size: 1},
		{Path: "sources", Directory: true},
	}
	for _, source := range definition.Bootstrap.Sources {
		entries = append(entries, prerequisites.BundleEntry{Path: sourcePath(source), Size: source.Bytes})
	}
	manager := New(ExecutionGuard{})
	manager.probe = func(context.Context, prerequisites.BundleArea, prerequisites.Definition) error {
		return errors.New("sealed readiness must not launch the interpreter")
	}
	area := &sealedArea{entries: entries}
	inspection, err := manager.Inspect(t.Context(), area, definition, true)
	if err != nil || !inspection.Ready || !inspection.Sealed || !inspection.ToolsReady || area.reads != 0 {
		t.Fatalf("sealed presence: %+v %v reads=%d", inspection, err, area.reads)
	}
	last := len(entries) - 1
	for name, change := range map[string]func([]prerequisites.BundleEntry) []prerequisites.BundleEntry{
		"missing-source": func(value []prerequisites.BundleEntry) []prerequisites.BundleEntry { return value[:last] },
		"short-source": func(value []prerequisites.BundleEntry) []prerequisites.BundleEntry {
			changed := slices.Clone(value)
			changed[last].Size--
			return changed
		},
		"missing-interpreter": func(value []prerequisites.BundleEntry) []prerequisites.BundleEntry {
			return slices.DeleteFunc(slices.Clone(value), func(entry prerequisites.BundleEntry) bool { return entry.Path == definition.Bootstrap.PythonExecutable })
		},
		"extra-file": func(value []prerequisites.BundleEntry) []prerequisites.BundleEntry {
			return append(slices.Clone(value), prerequisites.BundleEntry{Path: "python/extra", Size: 1})
		},
		"missing-documentation": func(value []prerequisites.BundleEntry) []prerequisites.BundleEntry {
			return slices.DeleteFunc(slices.Clone(value), func(entry prerequisites.BundleEntry) bool { return entry.Path == changelog })
		},
		"executable-documentation": func(value []prerequisites.BundleEntry) []prerequisites.BundleEntry {
			changed := slices.Clone(value)
			for index := range changed {
				if changed[index].Path == readme {
					changed[index].Executable = true
				}
			}
			return changed
		},
	} {
		t.Run(name, func(t *testing.T) {
			area := &sealedArea{entries: change(entries)}
			inspection, err := manager.Inspect(t.Context(), area, definition, true)
			if err != nil || inspection.Ready || area.reads != 0 {
				t.Fatalf("%s reported ready: %+v %v reads=%d", name, inspection, err, area.reads)
			}
		})
	}
}

const (
	bundledReadme    = "automation/collections/ansible_collections/bootwright/core/README.md"
	bundledChangelog = "automation/collections/ansible_collections/bootwright/core/CHANGELOG.rst"
)

// publishedPendingArea is a pending area a complete preparation published from
// sources this host retains, with the manager and definition that published it.
func publishedPendingArea(t *testing.T) (*Manager, prerequisites.Definition, *memoryArea) {
	t.Helper()
	retained, held := retainedBootstrap(t)
	manager := New(nil)
	manager.probe = func(context.Context, prerequisites.BundleArea, prerequisites.Definition) error { return nil }
	manager.fetch = func(_ context.Context, source prerequisites.DependencySource, _ prerequisites.SetupEgress) ([]byte, error) {
		t.Fatalf("acquired %s although this host retains it", source.ID)
		return nil, nil
	}
	rebased, err := manager.Rebase(t.Context(), held, retained)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := prerequisites.NewResolvedDefinition(rebased, *resolvedDefinitionFixture(t).Native)
	if err != nil {
		t.Fatal(err)
	}
	area := newMemoryArea()
	if _, err := manager.Prepare(t.Context(), area, held, definition, prerequisites.SetupEgress{}, nil); err != nil {
		t.Fatal(err)
	}
	return manager, definition, area
}

// Documentation leaves the automation digest, so a pending area holding an
// earlier release's README still holds this executable's automation: it is
// attributable and complete, and preparation keeps that README rather than
// refusing it. Documentation the area lacks is written like any file it lacks.
func TestAPendingAreaWithOlderDocumentationIsRecoverableAndCompletes(t *testing.T) {
	manager, definition, area := publishedPendingArea(t)
	record := mustValidate(t, definition)
	inspect := func(ready bool) {
		t.Helper()
		inspection, _, err := inspectFiles(t.Context(), area, record)
		if err != nil || inspection.Ready != ready || !inspection.Recoverable {
			t.Fatalf("inspection = %+v (%v); want ready=%v and recoverable", inspection, err, ready)
		}
	}
	older := []byte("# An earlier release of the collection\n")
	area.files[bundledReadme] = projectedFile{data: older}
	inspect(true)
	if _, err := manager.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, nil); err != nil {
		t.Fatalf("preparation refused an earlier release's README: %v", err)
	}
	delete(area.files, bundledChangelog)
	inspect(false)
	published, err := manager.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, nil)
	if err != nil || !published.Ready {
		t.Fatalf("preparation over an area missing its CHANGELOG = %+v (%v)", published, err)
	}
	written := area.files[bundledChangelog]
	if !slices.Equal(written.data, ansible.Documentation()[strings.TrimPrefix(bundledChangelog, "automation/")]) || written.executable {
		t.Fatal("preparation did not write the embedded CHANGELOG as a regular file")
	}
	if !slices.Equal(area.files[bundledReadme].data, older) {
		t.Fatal("preparation replaced the README the area held")
	}
	inspect(true)
}

// resizedArea reports one entry at a size its bytes do not have, as a bundle
// could hold documentation no member bound admits.
type resizedArea struct {
	*memoryArea
	name string
	size int64
}

func (a resizedArea) Entries(ctx context.Context) ([]prerequisites.BundleEntry, error) {
	entries, err := a.memoryArea.Entries(ctx)
	for index := range entries {
		if entries[index].Path == a.name {
			entries[index].Size = a.size
		}
	}
	return entries, err
}

// Documentation is attributable only as a regular, non-executable file within
// the member bound. A directory or an executable at its path, or a file larger
// than any member, is content no approved closure explains, so inspection
// refuses to recover it and preparation refuses to publish over it.
func TestDocumentationThatIsADirectoryOrExecutableIsNotAttributable(t *testing.T) {
	for name, change := range map[string]func(*memoryArea) prerequisites.BundleArea{
		"directory": func(area *memoryArea) prerequisites.BundleArea {
			delete(area.files, bundledReadme)
			area.directories[bundledReadme] = true
			return area
		},
		"executable": func(area *memoryArea) prerequisites.BundleArea {
			file := area.files[bundledReadme]
			file.executable = true
			area.files[bundledReadme] = file
			return area
		},
		"oversized": func(area *memoryArea) prerequisites.BundleArea {
			return resizedArea{memoryArea: area, name: bundledChangelog, size: maxMemberBytes + 1}
		},
	} {
		t.Run(name, func(t *testing.T) {
			manager, definition, published := publishedPendingArea(t)
			area := change(published)
			inspection, _, err := inspectFiles(t.Context(), area, mustValidate(t, definition))
			if err != nil || inspection.Ready || inspection.Recoverable {
				t.Fatalf("inspection = %+v (%v); want neither ready nor recoverable", inspection, err)
			}
			if _, err := manager.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, nil); err == nil {
				t.Fatal("preparation published over documentation it cannot attribute")
			}
		})
	}
}

// discardingArea is a pending area with the removal the store's write
// capability grants, which removes only a file smaller than approved, and
// records each file it removed.
type discardingArea struct {
	*memoryArea
	discarded []string
}

func (a *discardingArea) DiscardPartial(_ context.Context, name string, approved int64) error {
	file, found := a.files[name]
	if !found || int64(len(file.data)) >= approved {
		return errors.New("only a file smaller than approved is removed")
	}
	delete(a.files, name)
	a.discarded = append(a.discarded, name)
	return nil
}

// shortened is an area an earlier build left with name cut short, as a write
// killed before its sync leaves it under its final name, and the bytes it
// was meant to hold. That build published the sources in order before any
// projected file, so for a source the area holds only the sources before it,
// and the replay acquires that source and every later one again.
func shortened(t *testing.T, name string) (*Manager, prerequisites.Definition, *discardingArea, projectedFile) {
	t.Helper()
	manager, definition, published := publishedPendingArea(t)
	complete, found := published.files[name]
	if !found || len(complete.data) < 2 {
		t.Fatalf("the published area holds no file %s to cut short", name)
	}
	sources := map[string][]byte{}
	for _, source := range definition.Bootstrap.Sources {
		sources[sourcePath(source)] = slices.Clone(published.files[sourcePath(source)].data)
	}
	if index := slices.IndexFunc(definition.Bootstrap.Sources, func(source prerequisites.DependencySource) bool { return sourcePath(source) == name }); index >= 0 {
		for path := range published.files {
			if !slices.ContainsFunc(definition.Bootstrap.Sources[:index], func(source prerequisites.DependencySource) bool { return sourcePath(source) == path }) {
				delete(published.files, path)
			}
		}
		published.directories = map[string]bool{"sources": true}
	}
	published.files[name] = projectedFile{data: slices.Clone(complete.data[:len(complete.data)/2]), executable: complete.executable}
	area := &discardingArea{memoryArea: published}
	manager.fetch = func(_ context.Context, source prerequisites.DependencySource, _ prerequisites.SetupEgress) ([]byte, error) {
		data, approved := sources[sourcePath(source)]
		if _, held := area.files[sourcePath(source)]; held || !approved {
			t.Fatalf("acquired %s, which the area holds", source.ID)
		}
		return slices.Clone(data), nil
	}
	return manager, definition, area, complete
}

// replayed fails the test unless inspection found the short file recoverable
// and not ready, and the exact replay removed that one file and published the
// approved bytes in its place.
func replayed(t *testing.T, manager *Manager, definition prerequisites.Definition, area *discardingArea, name string, complete projectedFile) {
	t.Helper()
	inspection, _, err := inspectFiles(t.Context(), area, mustValidate(t, definition))
	if err != nil || inspection.Ready || !inspection.Recoverable {
		t.Fatalf("the short file inspected as %+v (%v); want recoverable and not ready", inspection, err)
	}
	published, err := manager.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, nil)
	if err != nil || !published.Ready || !published.Recoverable {
		t.Fatalf("the exact replay over the short file = %+v (%v)", published, err)
	}
	if !slices.Equal(area.discarded, []string{name}) || !slices.Equal(area.files[name].data, complete.data) || area.files[name].executable != complete.executable {
		t.Fatalf("the replay removed %v and holds %d bytes at %s", area.discarded, len(area.files[name].data), name)
	}
	writes := area.writes
	inspection, err = manager.Inspect(t.Context(), area, definition, true)
	if err != nil || !inspection.Ready || !inspection.Recoverable {
		t.Fatalf("the replayed area inspected as %+v (%v)", inspection, err)
	}
	if again, err := manager.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, nil); err != nil || !again.Ready || area.writes != writes || len(area.discarded) != 1 {
		t.Fatalf("the next preparation found the replayed area unfinished: %+v (%v), %d writes", again, err, area.writes-writes)
	}
}

// A retained source an earlier build left shorter than approved under its
// final name, which only a write killed before its sync leaves, is removed
// through the area's write capability and acquired again, and the exact
// replay completes.
func TestAnExactReplayOverAShortSourceAnEarlierBuildLeftCompletes(t *testing.T) {
	_, definition, _ := publishedPendingArea(t)
	name := sourcePath(definition.Bootstrap.Sources[1])
	manager, definition, area, complete := shortened(t, name)
	replayed(t, manager, definition, area, name, complete)
}

// A projected file left short, the private interpreter among them, is removed
// and projected again from the sources the area holds, and the exact replay
// completes without acquiring anything.
func TestAnExactReplayOverAShortProjectedFileAnEarlierBuildLeftCompletes(t *testing.T) {
	_, definition, _ := publishedPendingArea(t)
	name := definition.Bootstrap.PythonExecutable
	manager, definition, area, complete := shortened(t, name)
	if !complete.executable {
		t.Fatalf("%s is not the executable projected file", name)
	}
	replayed(t, manager, definition, area, name, complete)
}

// Only a short file is an earlier build's partial write. A file of its
// approved size with other bytes is content no write explains, so inspection
// refuses to recover it and preparation refuses before it removes anything;
// and an area that offers no removal refuses a short file with a named exit.
func TestASameSizeFileWithOtherBytesStillRefuses(t *testing.T) {
	_, definition, _ := publishedPendingArea(t)
	for name, path := range map[string]string{"a source": sourcePath(definition.Bootstrap.Sources[1]), "a projected file": definition.Bootstrap.PythonExecutable} {
		t.Run(name, func(t *testing.T) {
			manager, definition, published := publishedPendingArea(t)
			file := published.files[path]
			changed := slices.Clone(file.data)
			changed[0] ^= 0xff
			published.files[path] = projectedFile{data: changed, executable: file.executable}
			area := &discardingArea{memoryArea: published}
			inspection, _, err := inspectFiles(t.Context(), area, mustValidate(t, definition))
			if err != nil || inspection.Ready || inspection.Recoverable {
				t.Fatalf("a same-size file with other bytes inspected as %+v (%v)", inspection, err)
			}
			_, err = manager.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, nil)
			if found := diagnostics.Of(err); len(found) != 1 || found[0].Message != "existing bundle content is not attributable to the approved closure" || len(area.discarded) != 0 {
				t.Fatalf("preparation over other bytes: %#v, removed %v", found, area.discarded)
			}
		})
	}
	t.Run("an area without the removal", func(t *testing.T) {
		manager, definition, area, _ := shortened(t, definition.Bootstrap.PythonExecutable)
		_, err := manager.Prepare(t.Context(), area.memoryArea, nil, definition, prerequisites.SetupEgress{}, nil)
		if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.unsupported" ||
			found[0].Message != "the bundle file "+definition.Bootstrap.PythonExecutable+" an earlier build left incomplete cannot be removed, because this bundle area offers no removal" ||
			found[0].Remediation != "Use an executable whose controller store removes an incomplete bundle file, then rerun bootwright setup." {
			t.Fatalf("preparation without the removal: %#v (%v)", found, err)
		}
	})
}
