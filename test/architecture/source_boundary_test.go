package architecture_test

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"
)

// sourceTree is the repository's source as a checkout without ignored files
// has it: the files Git tracks, and the untracked ones it does not ignore, so
// a file under edit is checked before it is added. A walk of it never reads an
// ignored directory, so a cache, a build output or a scratch directory of the
// working tree, readable or not, neither fails nor decides a source check.
type sourceTree struct {
	fs.FS
	present map[string]bool
}

func newSourceTree(root string) (sourceTree, error) {
	names, err := gitListing(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--deduplicate")
	if err != nil {
		return sourceTree{}, err
	}
	names = slices.DeleteFunc(names, func(name string) bool {
		_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		return err != nil
	})
	return sourceTree{FS: os.DirFS(root), present: trackedPaths(names)}, nil
}

func (tree sourceTree) ReadDir(name string) ([]fs.DirEntry, error) {
	if !tree.present[name] {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries, err := fs.ReadDir(tree.FS, name)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(entries, func(entry fs.DirEntry) bool { return !tree.present[path.Join(name, entry.Name())] }), nil
}

// walkSource walks directory under root as filepath.WalkDir does, but only
// through sourceTree: the paths it visits are joined to root.
func walkSource(root, directory string, visit fs.WalkDirFunc) error {
	tree, err := newSourceTree(root)
	if err != nil {
		return err
	}
	return fs.WalkDir(tree, filepath.ToSlash(directory), func(name string, entry fs.DirEntry, err error) error {
		return visit(filepath.Join(root, filepath.FromSlash(name)), entry, err)
	})
}

// TestSourceChecksSkipAnIgnoredUnreadableDirectory proves the source boundary
// against a repository whose ignored cache directory cannot be read (B616):
// the request check and the source walkers still see every tracked and every
// unignored untracked source file, and none of the ignored ones, where a walk
// of the working tree fails on the cache. The permission is restored on
// cleanup so the temporary directory can be removed.
func TestSourceChecksSkipAnIgnoredUnreadableDirectory(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/fixture\n\ngo 1.26\n")
	write(".gitignore", "cache/\nout/\n")
	write("internal/widget/requests.go", "package widget\n\ntype BuildRequest struct{ Planted bool }\n")
	write("internal/widget/out/ignored.go", "package out\n")
	write("cache/inner/ignored.go", "package inner\n")
	ratchetGit(t, root, "init", "-q")
	ratchetGit(t, root, "add", "go.mod", ".gitignore", "internal/widget/requests.go")
	write("internal/widget/untracked.go", "package widget\n")

	cache := filepath.Join(root, "cache")
	t.Cleanup(func() { _ = os.Chmod(cache, 0o755) })
	if err := os.Chmod(cache, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(cache); err == nil {
		t.Skip("the process reads a directory without permission, so the cache cannot be made unreadable")
	}
	if err := fs.WalkDir(os.DirFS(root), ".", func(_ string, _ fs.DirEntry, err error) error { return err }); err == nil {
		t.Fatal("a walk of the working tree reads the unreadable cache, so the fixture proves nothing")
	}

	tree, err := newSourceTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if unread, want := unreadRequestFields(t, tree), []string{"example.com/fixture/internal/widget.BuildRequest.Planted"}; !slices.Equal(unread, want) {
		t.Errorf("unread request fields = %q, want %q", unread, want)
	}
	var visited []string
	err = walkSource(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			relative, _ := filepath.Rel(root, name)
			visited = append(visited, filepath.ToSlash(relative))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "go.mod", "internal/widget/requests.go", "internal/widget/untracked.go"}
	if !slices.Equal(visited, want) {
		t.Errorf("walkSource visited %q, want %q", visited, want)
	}
}
