//go:build linux

package nativefs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func TestOpenNoFollowRefusesALinkAtEveryComponent(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "a", "b", "c", "file"), "data")
	components := []string{"a", "b", "c", "file"}
	for index := range components {
		t.Run(strings.Join(components[:index+1], "/"), func(t *testing.T) {
			root := t.TempDir()
			real := filepath.Join(base, filepath.Join(components[:index+1]...))
			parent := filepath.Join(root, filepath.Join(components[:index]...))
			if err := os.MkdirAll(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			link(t, real, filepath.Join(parent, components[index]))
			for _, rest := range components[index+1:] {
				parent = filepath.Join(parent, rest)
			}
			path := filepath.Join(root, filepath.Join(components...))
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("the planted link does not resolve: %v", err)
			}
			file, err := OpenNoFollow(path, syscall.O_RDONLY)
			if !errors.Is(err, syscall.ELOOP) {
				if file != nil {
					file.Close()
				}
				t.Fatalf("a link at %s opened: %v", components[index], err)
			}
		})
	}
	file, err := OpenNoFollow(filepath.Join(base, "a", "b", "c", "file"), syscall.O_RDONLY)
	if err != nil {
		t.Fatalf("a path without links refused: %v", err)
	}
	file.Close()
}

func TestOpenNoFollowHoldsATrailingLinkOnlyAsAPathHandle(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "file"), "data")
	link(t, filepath.Join(base, "file"), filepath.Join(base, "link"))
	file, err := OpenNoFollow(filepath.Join(base, "link"), PathOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	stat, err := Stat(file)
	if err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFLNK {
		t.Fatalf("the path handle is not the link: %v %o", err, stat.Mode)
	}
}

func TestOpenNoFollowRefusesARelativeOrUncleanPathAndANonDirectoryAncestor(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "file"), "data")
	for _, path := range []string{"relative", base + "/./file", base + "/x/../file", base + "/file\x00"} {
		if _, err := OpenNoFollow(path, syscall.O_RDONLY); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("%q: %v", path, err)
		}
	}
	if _, err := OpenNoFollow(filepath.Join(base, "file", "below"), syscall.O_RDONLY); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("a file as an ancestor: %v", err)
	}
	if _, err := OpenNoFollow(filepath.Join(base, "missing"), syscall.O_RDONLY); !errors.Is(err, syscall.ENOENT) {
		t.Errorf("a missing path: %v", err)
	}
}

func held(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func TestOpenBeneathStaysBeneathItsDirectoryWithoutALink(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "outside"), "secret")
	write(t, filepath.Join(base, "dir", "a", "file"), "data")
	link(t, filepath.Join(base, "dir", "a"), filepath.Join(base, "dir", "linked"))
	link(t, "file", filepath.Join(base, "dir", "a", "relative"))
	dir := held(t, filepath.Join(base, "dir"))
	for name, want := range map[string]error{
		"linked/file":   syscall.ELOOP,
		"a/relative":    syscall.ELOOP,
		"../outside":    syscall.EXDEV,
		base + "/dir/a": syscall.EXDEV,
		"a\x00":         ErrInvalidName,
	} {
		if fd, err := OpenBeneath(int(dir.Fd()), name, syscall.O_RDONLY, 0); !errors.Is(err, want) {
			if err == nil {
				syscall.Close(fd)
			}
			t.Errorf("%q: %v, want %v", name, err, want)
		}
	}
	fd, err := OpenBeneath(int(dir.Fd()), "a/file", syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	syscall.Close(fd)
	fd, err = OpenBeneath(int(dir.Fd()), "new", syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil || stat.Mode&0o777 != 0o600 {
		t.Fatalf("the created file's mode: %v %o", err, stat.Mode)
	}
	syscall.Close(fd)
}

func TestOpenAtAndTheProcessSessionRefuseATrailingLink(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "file"), "data")
	link(t, "file", filepath.Join(base, "link"))
	session := ProcessFiles{}
	root, err := session.Root()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if stat, err := Stat(root); err != nil || stat.Ino != rootInode(t) {
		t.Fatalf("the session root is not /: %v", err)
	}
	dir := held(t, base)
	if _, err := session.OpenAt(dir, "link", syscall.O_RDONLY); !errors.Is(err, syscall.ELOOP) {
		t.Fatalf("the trailing link opened: %v", err)
	}
	file, err := session.OpenAt(dir, "file", syscall.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if session.Close() != nil {
		t.Fatal("the process session failed to close")
	}
}

func rootInode(t *testing.T) uint64 {
	t.Helper()
	var stat syscall.Stat_t
	if err := syscall.Stat("/", &stat); err != nil {
		t.Fatal(err)
	}
	return stat.Ino
}

func TestIdentityTellsAReplacementAndAChangeFromTheHeldFile(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "file")
	write(t, path, "data")
	before, err := Stat(held(t, path))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(base, "other"), "data")
	if err := os.Rename(filepath.Join(base, "other"), path); err != nil {
		t.Fatal(err)
	}
	after, err := Stat(held(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if SameIdentity(before, after) || SameStable(before, after) {
		t.Fatal("a file renamed over the name kept the identity")
	}
	file := held(t, path)
	opened, _ := Stat(file)
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, _ := Stat(file)
	if !SameIdentity(opened, changed) || SameStable(opened, changed) {
		t.Fatal("a write in place is not a change of a stable file")
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if moded, _ := Stat(file); SameIdentity(opened, moded) {
		t.Fatal("a mode change kept the identity")
	}
}

type rootDenial struct{}

func (rootDenial) Error() string      { return "denied" }
func (rootDenial) DeniedToRoot() bool { return true }

func TestDenialsNameWhoWasDenied(t *testing.T) {
	if !Denied(syscall.EACCES) || !Denied(&os.PathError{Err: syscall.EPERM}) || Denied(syscall.ENOENT) {
		t.Fatal("Denied misclassifies")
	}
	if !DeniedToRoot(errors.Join(syscall.EACCES, rootDenial{})) || DeniedToRoot(syscall.EACCES) {
		t.Fatal("DeniedToRoot misclassifies")
	}
}

func TestReadBoundedReadsUpToItsBound(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "file")
	data := strings.Repeat("x", 70000)
	write(t, path, data)
	for _, row := range []struct {
		maximum int
		want    int
	}{{1, 1}, {70000, 70000}, {70001, 70000}, {80000, 70000}} {
		got, err := ReadBounded(context.Background(), held(t, path), row.maximum)
		if err != nil || len(got) != row.want || !bytes.Equal(got, []byte(data[:row.want])) {
			t.Errorf("bound %d: %d bytes, %v", row.maximum, len(got), err)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadBounded(canceled, held(t, path), 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled read: %v", err)
	}
	directory := held(t, base)
	if _, err := ReadBounded(context.Background(), directory, 10); err == nil {
		t.Fatal("a directory read succeeded")
	}
}

func publishRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root, dir
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, entry := range entries {
		found = append(found, entry.Name())
	}
	return found
}

func TestPublishReplacesTheNameWithCompleteContent(t *testing.T) {
	root, dir := publishRoot(t)
	write(t, filepath.Join(dir, "target"), "old")
	if err := Publish(context.Background(), root, "target", []byte("new"), Publication{Prefix: ".stage-", Mode: 0o640}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dir, "target"))
	if data, _ := os.ReadFile(filepath.Join(dir, "target")); err != nil || string(data) != "new" || info.Mode().Perm() != 0o640 {
		t.Fatalf("published %q mode %v: %v", data, info.Mode(), err)
	}
	if got := names(t, dir); !slices.Equal(got, []string{"target"}) {
		t.Fatalf("entries after publication: %v", got)
	}
	for _, name := range []string{"", ".", "..", "a/b"} {
		if err := Publish(context.Background(), root, name, nil, Publication{}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestPublishLeavesTheOldContentAndNoStageAtEveryPointBeforeTheRename(t *testing.T) {
	injected := errors.New("injected")
	for _, point := range []string{"created", "written", "synced"} {
		t.Run(point, func(t *testing.T) {
			root, dir := publishRoot(t)
			write(t, filepath.Join(dir, "target"), "old")
			fired := false
			how := Publication{Prefix: ".stage-", Mode: 0o600, step: func(at string) error {
				if at == point {
					fired = true
					return injected
				}
				return nil
			}}
			if err := Publish(context.Background(), root, "target", []byte("new"), how); !errors.Is(err, injected) || !fired {
				t.Fatalf("the failure at %s: fired %v, %v", point, fired, err)
			}
			if data, _ := os.ReadFile(filepath.Join(dir, "target")); string(data) != "old" {
				t.Fatalf("the target became %q", data)
			}
			if got := names(t, dir); !slices.Equal(got, []string{"target"}) {
				t.Fatalf("entries after the failure: %v", got)
			}
		})
	}
}

func TestPublishReportsAnUnsyncedPublicationAfterTheRename(t *testing.T) {
	root, dir := publishRoot(t)
	injected := errors.New("injected")
	how := Publication{Prefix: ".stage-", Mode: 0o600, step: func(at string) error {
		if at == "renamed" {
			return injected
		}
		return nil
	}}
	err := Publish(context.Background(), root, "target", []byte("new"), how)
	var unsynced *UnsyncedError
	if !errors.As(err, &unsynced) || !errors.Is(err, injected) {
		t.Fatalf("a failure after the rename: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "target")); string(data) != "new" {
		t.Fatalf("the target is %q", data)
	}
}

func TestPublishRefusesAStageReplacedBeforeTheRenameAndKeepsTheReplacement(t *testing.T) {
	root, dir := publishRoot(t)
	how := Publication{Prefix: ".stage-", Mode: 0o600, step: func(at string) error {
		if at != "synced" {
			return nil
		}
		for _, name := range names(t, dir) {
			if strings.HasPrefix(name, ".stage-") {
				write(t, filepath.Join(dir, "planted"), "planted")
				return os.Rename(filepath.Join(dir, "planted"), filepath.Join(dir, name))
			}
		}
		return errors.New("no stage")
	}}
	if err := Publish(context.Background(), root, "target", []byte("new"), how); !errors.Is(err, ErrStageReplaced) {
		t.Fatalf("a replaced stage: %v", err)
	}
	got := names(t, dir)
	if len(got) != 1 || !strings.HasPrefix(got[0], ".stage-") {
		t.Fatalf("entries after the refusal: %v", got)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, got[0])); string(data) != "planted" {
		t.Fatalf("the replacement became %q", data)
	}
}

func TestPublishReturnsTheCancellation(t *testing.T) {
	root, dir := publishRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Publish(ctx, root, "target", []byte("new"), Publication{Mode: 0o600}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled publication: %v", err)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Fatalf("entries after cancellation: %v", got)
	}
}

func treeRoot(t *testing.T, dir string) (*os.Root, uint64) {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	var stat syscall.Stat_t
	if err := syscall.Stat(dir, &stat); err != nil {
		t.Fatal(err)
	}
	return root, stat.Dev
}

func TestRemoveTreeRemovesLinksAsThemselves(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	write(t, filepath.Join(outside, "kept"), "kept")
	tree := filepath.Join(base, "tree")
	write(t, filepath.Join(tree, "a", "b", "file"), "data")
	write(t, filepath.Join(tree, "top"), "data")
	link(t, outside, filepath.Join(tree, "a", "dirlink"))
	link(t, filepath.Join(outside, "kept"), filepath.Join(tree, "filelink"))
	root, device := treeRoot(t, tree)
	entries := 100
	if err := RemoveTree(root, Tree{Subject: "tree", Device: device, MaxDepth: 8, Entries: &entries}, "top"); err != nil {
		t.Fatal(err)
	}
	if got := names(t, tree); len(got) != 0 {
		t.Fatalf("entries left: %v", got)
	}
	if data, err := os.ReadFile(filepath.Join(outside, "kept")); err != nil || string(data) != "kept" {
		t.Fatalf("a link target was touched: %v", err)
	}
	if entries != 100-6 {
		t.Fatalf("the entry budget is %d", entries)
	}
}

func TestRemoveTreeKeepsItsBounds(t *testing.T) {
	base := t.TempDir()
	deep := filepath.Join(base, "deep")
	write(t, filepath.Join(deep, "1", "2", "3", "file"), "data")
	root, device := treeRoot(t, deep)
	entries := 100
	if err := RemoveTree(root, Tree{Subject: "stage", Device: device, MaxDepth: 2, Entries: &entries}); err == nil || err.Error() != "stage depth bound" {
		t.Fatalf("beyond the depth bound: %v", err)
	}
	wide := filepath.Join(base, "wide")
	for _, name := range []string{"a", "b", "c"} {
		write(t, filepath.Join(wide, name), "data")
	}
	root, device = treeRoot(t, wide)
	entries = 2
	if err := RemoveTree(root, Tree{Subject: "stage", Device: device, MaxDepth: 2, Entries: &entries}); err == nil || err.Error() != "stage entry bound" {
		t.Fatalf("beyond the entry bound: %v", err)
	}
	if got := names(t, wide); len(got) != 3 {
		t.Fatalf("an over-bound tree lost entries: %v", got)
	}
	entries = 100
	if err := RemoveTree(root, Tree{Subject: "stage", Device: device + 1, MaxDepth: 2, Entries: &entries}); err == nil || err.Error() != "stage crosses a filesystem" {
		t.Fatalf("another device: %v", err)
	}
}

func TestRemoveTreeRemovesTheLastNamesLast(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"lock", "a", "owner", "z"} {
		write(t, filepath.Join(base, name), name)
	}
	root, device := treeRoot(t, base)
	entries := 100
	tree := Tree{Subject: "stage", Device: device, MaxDepth: 2, Entries: &entries}
	names, err := tree.names(root)
	if err != nil {
		t.Fatal(err)
	}
	last := []string{"owner", "lock"}
	slices.SortStableFunc(names, func(a, b string) int { return slices.Index(last, a) - slices.Index(last, b) })
	if got := names[len(names)-2:]; !slices.Equal(got, last) {
		t.Fatalf("the removal order ends in %v", got)
	}
	if err := RemoveTree(root, tree, last...); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveTreeRefusesASubtreeReplacedAfterItWasListed(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "tree", "sub", "file"), "data")
	write(t, filepath.Join(base, "other", "kept"), "kept")
	root, device := treeRoot(t, filepath.Join(base, "tree"))
	listed, err := root.Lstat("sub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(base, "tree", "sub"), filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(base, "other"), filepath.Join(base, "tree", "sub")); err != nil {
		t.Fatal(err)
	}
	entries := 100
	tree := Tree{Subject: "stage", Device: device, MaxDepth: 2, Entries: &entries}
	if err := tree.child(root, "sub", listed, 0); err == nil || err.Error() != "stage entry changed" {
		t.Fatalf("a replaced subtree: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(base, "tree", "sub", "kept")); err != nil || string(data) != "kept" {
		t.Fatalf("the replacement was emptied: %v", err)
	}
}

func TestTheDescriptorEffectsUseTheirTypedCalls(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "a"), "a")
	write(t, filepath.Join(base, "b"), "b")
	dir := held(t, base)
	fd := int(dir.Fd())
	if err := RenameNoReplace(fd, "a", "b"); !errors.Is(err, syscall.EEXIST) {
		t.Fatalf("a rename over an entry: %v", err)
	}
	if err := RenameNoReplace(fd, "a", "c"); err != nil {
		t.Fatal(err)
	}
	if err := RenameNoReplace(fd, "c", "../d"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("a rename out of the directory: %v", err)
	}
	if err := Symlink("c", fd, "link"); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(base, "link")); err != nil || target != "c" {
		t.Fatalf("the link names %q: %v", target, err)
	}
	if err := Symlink("", fd, "empty"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("an empty link target: %v", err)
	}
	if err := os.Mkdir(filepath.Join(base, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Unlink(fd, "dir", false); err == nil {
		t.Fatal("a directory unlinked as a file")
	}
	for _, entry := range []struct {
		name      string
		directory bool
	}{{"dir", true}, {"link", false}, {"c", false}, {"b", false}} {
		if err := Unlink(fd, entry.name, entry.directory); err != nil {
			t.Fatalf("%s: %v", entry.name, err)
		}
	}
	if err := Unlink(fd, "..", true); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("an unlink of the parent: %v", err)
	}
	if got := names(t, base); len(got) != 0 {
		t.Fatalf("entries left: %v", got)
	}
	if err := SyncFilesystem(fd); err != nil {
		t.Fatal(err)
	}
}
