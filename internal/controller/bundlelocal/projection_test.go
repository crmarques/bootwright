package bundlelocal

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type archiveMember struct {
	name     string
	data     string
	link     string
	typeflag byte
}

func pythonArchive(t *testing.T, members ...archiveMember) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gzipWriter)
	for _, member := range members {
		kind := member.typeflag
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{Name: member.name, Mode: 0755, Size: int64(len(member.data)), Typeflag: kind, Linkname: member.link}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(member.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func wheelArchive(t *testing.T, names ...string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("package")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestProjectionFlattensOnlyIncludedFileLinks(t *testing.T) {
	p := newProjection()
	data := pythonArchive(t,
		archiveMember{name: "python/bin/python3.13", data: "qualified executable"},
		archiveMember{name: "python/bin/python3", link: "python3.13", typeflag: tar.TypeSymlink},
		archiveMember{name: "python/bin/python", link: "python3", typeflag: tar.TypeSymlink},
	)
	if err := p.archive(t.Context(), data); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"python/bin/python3.13", "python/bin/python3", "python/bin/python"} {
		if got := p.files[name]; string(got.data) != "qualified executable" || !got.executable {
			t.Fatalf("incorrect projection of %q", name)
		}
	}
	if got := p.directories(); !slices.Equal(got, []string{"python", "python/bin"}) {
		t.Fatalf("directories: %v", got)
	}
}

func TestProjectionRejectsTraversalLinksAndConflicts(t *testing.T) {
	tests := map[string][]archiveMember{
		"traversal":       {{name: "python/../escape", data: "unsafe"}},
		"absolute":        {{name: "/python/bin/tool", data: "unsafe"}},
		"outside root":    {{name: "elsewhere/tool", data: "unsafe"}},
		"escaping link":   {{name: "python/bin/tool", link: "../../../escape", typeflag: tar.TypeSymlink}},
		"absolute link":   {{name: "python/bin/tool", link: "/usr/bin/python", typeflag: tar.TypeSymlink}},
		"absent link":     {{name: "python/bin/tool", link: "absent", typeflag: tar.TypeSymlink}},
		"directory link":  {{name: "python/bin/tool", data: "tool"}, {name: "python/bin/dir", link: ".", typeflag: tar.TypeSymlink}},
		"cycle":           {{name: "python/a", link: "b", typeflag: tar.TypeSymlink}, {name: "python/b", link: "a", typeflag: tar.TypeSymlink}},
		"duplicate":       {{name: "python/bin/tool", data: "first"}, {name: "python/bin/tool", data: "second"}},
		"ancestor file":   {{name: "python/bin", data: "file"}, {name: "python/bin/tool", data: "tool"}},
		"descendant file": {{name: "python/bin/tool", data: "tool"}, {name: "python/bin", data: "file"}},
		"fifo":            {{name: "python/fifo", typeflag: tar.TypeFifo}},
	}
	for name, members := range tests {
		t.Run(name, func(t *testing.T) {
			if err := newProjection().archive(t.Context(), pythonArchive(t, members...)); err == nil {
				t.Fatal("accepted unqualified archive")
			}
		})
	}
}

func TestWheelProjectionRefusesPathHooksAndRelocation(t *testing.T) {
	for _, name := range []string{"../escape.py", "/absolute.py", "package/../escape.py", "ambient.pth", "sitecustomize.py", "usercustomize.py", "package.data/scripts/tool"} {
		t.Run(name, func(t *testing.T) {
			if err := newProjection().wheel(t.Context(), wheelArchive(t, name)); err == nil {
				t.Fatal("accepted unqualified wheel")
			}
		})
	}
	p := newProjection()
	if err := p.wheel(t.Context(), wheelArchive(t, "package/__init__.py")); err != nil {
		t.Fatal(err)
	}
	if _, exists := p.files[sitePackages+"package/__init__.py"]; !exists {
		t.Fatal("wheel was not confined to site-packages")
	}
	if err := p.wheel(t.Context(), wheelArchive(t, "package/__init__.py")); err == nil {
		t.Fatal("accepted overlapping wheel")
	}
}

func fixtureSource(id string, data []byte) prerequisites.DependencySource {
	digest := sha256.Sum256(data)
	return prerequisites.DependencySource{ID: id, URL: "https://files.pythonhosted.org/qualified/" + id, SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data))}
}

type memoryArea struct {
	files       map[string]projectedFile
	directories map[string]bool
	writes      int
	sealed      bool
}

func newMemoryArea() *memoryArea {
	return &memoryArea{files: map[string]projectedFile{}, directories: map[string]bool{}}
}
func (a *memoryArea) Read(ctx context.Context, name string, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, found := a.files[name]
	if !found {
		return nil, fs.ErrNotExist
	}
	if len(file.data) > limit {
		return nil, errors.New("read limit exceeded")
	}
	return slices.Clone(file.data), nil
}
func (a *memoryArea) Write(ctx context.Context, name string, data []byte, executable bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, found := a.files[name]; found {
		return fs.ErrExist
	}
	a.files[name] = projectedFile{data: slices.Clone(data), executable: executable}
	a.writes++
	return nil
}
func (a *memoryArea) EnsureDirectory(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.directories[name] = true
	a.writes++
	return nil
}
func (a *memoryArea) Entries(ctx context.Context) ([]prerequisites.BundleEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var entries []prerequisites.BundleEntry
	for name := range a.directories {
		entries = append(entries, prerequisites.BundleEntry{Path: name, Directory: true})
	}
	for name, file := range a.files {
		entries = append(entries, prerequisites.BundleEntry{Path: name, Size: int64(len(file.data)), Executable: file.executable})
	}
	return entries, nil
}
func (*memoryArea) Verify(ctx context.Context) error { return ctx.Err() }
func (a *memoryArea) Location(context.Context) (prerequisites.BundleLocation, error) {
	return prerequisites.BundleLocation{Sealed: a.sealed}, nil
}

func TestInspectionRequiresCompleteExactProjectionAndAttributablePartialFiles(t *testing.T) {
	archive := pythonArchive(t, archiveMember{name: "python/bin/python3.13", data: "executable"})
	wheel := wheelArchive(t, "package/__init__.py")
	record := catalogRecord{Baseline: []prerequisites.DependencySource{fixtureSource("python", archive), fixtureSource("wheel", wheel)}}
	area := newMemoryArea()
	assertInspection := func(ready, recoverable bool) {
		t.Helper()
		before := area.writes
		got, _, err := inspectFiles(t.Context(), area, record)
		if err != nil {
			t.Fatal(err)
		}
		if got.Ready != ready || got.Recoverable != recoverable {
			t.Fatalf("inspection: %+v; expected ready=%v recoverable=%v", got, ready, recoverable)
		}
		if area.writes != before {
			t.Fatal("inspection performed writes")
		}
	}
	assertInspection(false, true)
	area.files["sources/python"] = projectedFile{data: archive}
	assertInspection(false, true)
	area.files["python/bin/python3.13"] = projectedFile{data: []byte("executable"), executable: true}
	assertInspection(false, true)
	area.files["sources/wheel"] = projectedFile{data: wheel}
	area.files[sitePackages+"package/__init__.py"] = projectedFile{data: []byte("package")}
	assertInspection(false, true)
	assets := newProjection()
	if err := assets.automation(t.Context()); err != nil {
		t.Fatal(err)
	}
	for name, file := range assets.files {
		area.files[name] = file
	}
	assertInspection(true, true)
	area.files["python/lib/python3.13/sitecustomize.py"] = projectedFile{data: []byte("unapproved")}
	assertInspection(false, false)
	delete(area.files, "python/lib/python3.13/sitecustomize.py")
	area.files["python/bin/python3.13"] = projectedFile{data: []byte("executable")}
	assertInspection(false, false)
	area.files["python/bin/python3.13"] = projectedFile{data: []byte("tampered!!"), executable: true}
	assertInspection(false, false)
	area.files["python/bin/python3.13"] = projectedFile{data: []byte("executable"), executable: true}
	delete(area.files, "sources/python")
	assertInspection(false, false)
}

func TestArchiveAndInspectionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := newProjection().archive(ctx, pythonArchive(t, archiveMember{name: "python/file", data: "data"})); !errors.Is(err, context.Canceled) {
		t.Fatalf("archive cancellation: %v", err)
	}
	if _, _, err := inspectFiles(ctx, newMemoryArea(), catalogRecord{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("inspection cancellation: %v", err)
	}
}
