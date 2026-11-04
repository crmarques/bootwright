package renderlocal

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

func tree(paths ...string) []rendering.TreeFile {
	files := make([]rendering.TreeFile, 0, len(paths))
	for _, path := range paths {
		files = append(files, rendering.TreeFile{Path: path, Data: []byte("data of " + path + "\n")})
	}
	return files
}

func TestPublishTreeWritesPrivateDirectoriesAndFiles(t *testing.T) {
	previous := syscall.Umask(0)
	defer syscall.Umask(previous)
	directory := filepath.Join(t.TempDir(), "a", "cmp")
	path, err := (Files{}).PublishTree(context.Background(), directory, tree("desired/x/pool.yaml", "applied/x/pool.yaml", "notes.txt"))
	if err != nil || path != directory {
		t.Fatalf("path %q, error %v", path, err)
	}
	for name, mode := range map[string]os.FileMode{
		"": 0o700, "desired": 0o700, "desired/x": 0o700, "applied/x": 0o700,
		"desired/x/pool.yaml": 0o600, "notes.txt": 0o600,
	} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil || info.Mode().Perm() != mode {
			t.Errorf("%q: mode %v, error %v, want %v", name, info, err, mode)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(directory, "desired/x/pool.yaml")); string(data) != "data of desired/x/pool.yaml\n" {
		t.Fatalf("content %q", data)
	}
}

func TestPublishTreeWritesIntoAnEmptyDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := (Files{}).PublishTree(context.Background(), directory, tree("notes.txt")); err != nil {
		t.Fatal(err)
	}
	if names := entries(t, directory); !slices.Equal(names, []string{"notes.txt"}) {
		t.Fatalf("entries %q", names)
	}
}

func TestPublishTreeRefusesANonemptyDirectoryAndLeavesItAlone(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "mine"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (Files{}).PublishTree(context.Background(), directory, tree("notes.txt"))
	refusal(t, err)
	if names := entries(t, directory); !slices.Equal(names, []string{"mine"}) {
		t.Fatalf("entries %q", names)
	}
	file := filepath.Join(directory, "mine")
	_, err = (Files{}).PublishTree(context.Background(), file, tree("notes.txt"))
	refusal(t, err)
}

func TestPublishTreeRefusesUnsafePathsBeforeCreatingAnything(t *testing.T) {
	for _, paths := range [][]string{
		{"../escape"}, {"/etc/passwd"}, {"a/../../b"}, {"a/./b"}, {""}, {"."}, {"a//b"}, {"a/"}, {"ok", "ok"}, {"a\x00b"},
	} {
		directory := filepath.Join(t.TempDir(), "cmp")
		_, err := (Files{}).PublishTree(context.Background(), directory, tree(paths...))
		refusal(t, err)
		if _, statErr := os.Lstat(directory); !os.IsNotExist(statErr) {
			t.Errorf("%q: the directory exists after the refusal: %v", paths, statErr)
		}
	}
}

func TestPublishTreeRemovesWhatItMadeWhenAFileFails(t *testing.T) {
	collision := tree("a", "a/b")
	parent := t.TempDir()
	missing := filepath.Join(parent, "x", "cmp")
	_, err := (Files{}).PublishTree(context.Background(), missing, collision)
	refusal(t, err)
	if _, statErr := os.Lstat(filepath.Join(parent, "x")); !os.IsNotExist(statErr) {
		t.Fatalf("a directory the failed write made was left: %v", statErr)
	}
	empty := filepath.Join(parent, "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = (Files{}).PublishTree(context.Background(), empty, collision)
	refusal(t, err)
	if names := entries(t, empty); len(names) != 0 {
		t.Fatalf("a failed write left %q in the directory it was given", names)
	}
}

func TestPublishTreeRefusesADirectoryOthersCanWrite(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	_, err := (Files{}).PublishTree(context.Background(), directory, tree("notes.txt"))
	refusal(t, err)
	if names := entries(t, directory); len(names) != 0 {
		t.Fatalf("entries %q", names)
	}
}

func TestPublishTreeStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	directory := filepath.Join(t.TempDir(), "cmp")
	if _, err := (Files{}).PublishTree(ctx, directory, tree("notes.txt")); err == nil {
		t.Fatal("a canceled context wrote a tree")
	}
	if _, statErr := os.Lstat(directory); !os.IsNotExist(statErr) {
		t.Fatalf("the directory exists after cancellation: %v", statErr)
	}
}

func victim(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "victim")
	if err := os.Mkdir(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "keep"), []byte("keep"), 0o640); err != nil {
		t.Fatal(err)
	}
	return directory
}

func untouched(t *testing.T, directory string) {
	t.Helper()
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("the directory outside the tree changed: %v, %v", info, err)
	}
	if names := entries(t, directory); !slices.Equal(names, []string{"keep"}) {
		t.Fatalf("the directory outside the tree holds %q", names)
	}
	if data, err := os.ReadFile(filepath.Join(directory, "keep")); err != nil || string(data) != "keep" {
		t.Fatalf("the file outside the tree changed: %q, %v", data, err)
	}
}

func TestPublishTreeRefusesALinkAtTheLastComponentAndTouchesNothingBehindIt(t *testing.T) {
	target := victim(t)
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.Mkdir(empty, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "cmp")
	if err := os.Symlink(empty, link); err != nil {
		t.Fatal(err)
	}
	refusal(t, publishOrError((Files{}).PublishTree(context.Background(), link, tree("notes.txt"))))
	if names := entries(t, empty); len(names) != 0 {
		t.Fatalf("a link was followed into %q", names)
	}
	if info, err := os.Stat(empty); err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("the directory behind the link was re-moded: %v, %v", info, err)
	}
	swapped := filepath.Join(t.TempDir(), "cmp")
	if err := os.Mkdir(swapped, 0o700); err != nil {
		t.Fatal(err)
	}
	swap := func(stage string) {
		if stage == "open" {
			if err := os.Remove(swapped); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, swapped); err != nil {
				t.Fatal(err)
			}
		}
	}
	refusal(t, publishOrError((Files{step: swap}).PublishTree(context.Background(), swapped, tree("a", "b/c"))))
	untouched(t, target)
	if info, err := os.Lstat(swapped); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link that was swapped in was removed: %v, %v", info, err)
	}
}

func TestPublishTreeRefusesALinkSwappedInForADirectoryItCreates(t *testing.T) {
	target := victim(t)
	missing := filepath.Join(t.TempDir(), "cmp")
	swap := func(stage string) {
		if stage == "open" {
			if err := os.Symlink(target, missing); err != nil {
				t.Fatal(err)
			}
		}
	}
	refusal(t, publishOrError((Files{step: swap}).PublishTree(context.Background(), missing, tree("a"))))
	untouched(t, target)
}

func TestPublishTreeLeavesAGivenDirectoryItsModeAndRemovesOnlyWhatItMade(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "cmp")
	if err := os.Mkdir(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	plant := func(stage string) {
		if stage == "x/a" {
			if err := os.WriteFile(filepath.Join(directory, "foreign"), []byte("foreign"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	refusal(t, publishOrError((Files{step: plant}).PublishTree(context.Background(), directory, tree("x/a", "x/b", "a", "a/b"))))
	if names := entries(t, directory); !slices.Equal(names, []string{"foreign"}) {
		t.Fatalf("entries %q, want only the one this call did not make", names)
	}
	if info, err := os.Stat(directory); err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("the given directory was re-moded: %v, %v", info, err)
	}
	if err := os.Remove(filepath.Join(directory, "foreign")); err != nil {
		t.Fatal(err)
	}
	if _, err := (Files{}).PublishTree(context.Background(), directory, tree("notes.txt")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(directory); err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("a successful write re-moded the given directory: %v, %v", info, err)
	}
}

func TestPublishTreeRemovesTheDirectoryItMadeOnlyWhileItIsStillThatDirectory(t *testing.T) {
	parent := t.TempDir()
	directory, moved := filepath.Join(parent, "cmp"), filepath.Join(parent, "moved")
	replace := func(stage string) {
		if stage == "a" {
			if err := os.Rename(directory, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	refusal(t, publishOrError((Files{step: replace}).PublishTree(context.Background(), directory, tree("a", "a/b"))))
	if names := entries(t, parent); !slices.Equal(names, []string{"cmp", "moved"}) {
		t.Fatalf("entries %q", names)
	}
	if names := entries(t, moved); len(names) != 0 {
		t.Fatalf("the moved directory kept %q this call made", names)
	}
}

func TestPublishTreeRefusesADirectoryAnotherAccountOwns(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	other := func() int { return os.Geteuid() + 1 }
	refusal(t, publishOrError((Files{UID: other}).PublishTree(context.Background(), directory, tree("notes.txt"))))
	if names := entries(t, directory); len(names) != 0 {
		t.Fatalf("entries %q", names)
	}
}

func publishOrError(_ string, err error) error { return err }
