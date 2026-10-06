//go:build linux && amd64

package nativelocal

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// recordingStaging hands out the stages of the staging it wraps and records
// the kind and path of each.
type recordingStaging struct {
	staging *Staging
	kinds   []string
	paths   []string
}

func (r *recordingStaging) Stage(ctx context.Context, kind string) (prerequisites.Stage, error) {
	stage, err := r.staging.Stage(ctx, kind)
	if err == nil {
		r.kinds = append(r.kinds, kind)
		r.paths = append(r.paths, stage.Path)
	}
	return stage, err
}

// syntheticHost is a provided foundation whose database copy is one file, so
// a stage is built without the host's interpreter or package database.
func syntheticHost() providedHost {
	identity := databaseIdentity{Files: [2]fileIdentity{{Present: true, Device: 1, Inode: 2, Size: 3}}}
	return providedHost{
		interpreter: func(prerequisites.Platform) (string, error) { return "/usr/bin/python3", nil },
		state:       func(prerequisites.Platform) (databaseIdentity, error) { return identity, nil },
		copy: func(_ prerequisites.Platform, work string) (string, databaseIdentity, error) {
			root := filepath.Join(work, "snapshot")
			target := filepath.Join(root, "usr/lib/sysimage/rpm")
			if err := os.MkdirAll(target, 0700); err != nil {
				return "", identity, err
			}
			return root, identity, os.WriteFile(filepath.Join(target, "rpmdb.sqlite"), []byte("inventory"), 0600)
		},
	}
}

func beneath(parent, path string) bool { return strings.HasPrefix(path, parent+"/") }

func stagingEntries(t *testing.T, parent string) []string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// Every native stage and the retained database snapshot are created beneath
// the staging parent, never in the shared temporary directory, and an
// invocation that ends leaves that parent empty.
func TestResolutionStagesBeneathTheStagingRoot(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "staging")
	staging := &recordingStaging{staging: NewStaging(parent)}
	resolver := New(nil, staging)
	resolver.host = syntheticHost()
	stage, err := resolver.newStage(t.Context(), fedoraPlatform())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(staging.kinds, []string{"native", "rpmdb"}) {
		t.Fatalf("stages = %q", staging.kinds)
	}
	for _, path := range append(slices.Clone(staging.paths), stage.root, stage.work, stage.script, stage.snapshot) {
		if !beneath(parent, path) {
			t.Fatalf("%s is not beneath the staging parent %s", path, parent)
		}
	}
	if _, err := os.Stat(filepath.Join(stage.snapshot, "usr/lib/sysimage/rpm/rpmdb.sqlite")); err != nil {
		t.Fatalf("the snapshot was not staged: %v", err)
	}
	again, err := resolver.newStage(t.Context(), fedoraPlatform())
	if err != nil {
		t.Fatal(err)
	}
	if again.snapshot != stage.snapshot || len(staging.kinds) != 3 {
		t.Fatalf("an unchanged database was staged again: %q", staging.kinds)
	}
	stage.release()
	again.release()
	if names := stagingEntries(t, parent); len(names) != 1 || !strings.HasPrefix(names[0], "rpmdb-") {
		t.Fatalf("released native stages remain or the retained snapshot is gone: %q", names)
	}
	resolver.Close()
	if names := stagingEntries(t, parent); len(names) != 0 {
		t.Fatalf("the invocation left %q behind", names)
	}
}

// stageAt makes a stage as an invocation that has since ended leaves it: its
// directory, lock and marker, and content beneath it, a link to outside among
// it, and no holder.
func stageAt(t *testing.T, parent, name, outside string, marker bool) string {
	t.Helper()
	stage := filepath.Join(parent, name)
	if err := os.MkdirAll(filepath.Join(stage, "root", "work"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stage, 0711); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "root", "work", "repomd.xml"), []byte("metadata"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(stage, "root", "outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, stageLock), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if marker {
		if err := os.WriteFile(filepath.Join(stage, stageOwner), []byte("native 1\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return stage
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// The next stage removes a stage whose lock no one holds, with everything in
// it and without following its links. It leaves a stage another open file
// description holds, an entry that is a link, a directory another identity may
// write, and a stage without its marker, which is still being created.
func TestAStaleStageIsSweptAndAHeldOneIsNot(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "staging")
	if err := os.Mkdir(parent, 0711); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	kept := filepath.Join(outside, "kept")
	if err := os.WriteFile(kept, []byte("not the stage's"), 0600); err != nil {
		t.Fatal(err)
	}
	stale := stageAt(t, parent, "native-0123456789abcdef", outside, true)
	resolver := stageAt(t, parent, "resolver-00000000000000aa", outside, true)
	held := stageAt(t, parent, "rpmdb-00000000000000bb", outside, true)
	lock, err := os.OpenFile(filepath.Join(held, stageLock), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	creating := stageAt(t, parent, "native-00000000000000cc", outside, false)
	writable := stageAt(t, parent, "native-00000000000000dd", outside, true)
	if err := os.Chmod(writable, 0731); err != nil {
		t.Fatal(err)
	}
	elsewhere := stageAt(t, t.TempDir(), "native-00000000000000ee", outside, true)
	link := filepath.Join(parent, "native-00000000000000ee")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(parent, "notes")
	if err := os.WriteFile(unrelated, nil, 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := NewStaging(parent).Stage(t.Context(), "native")
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Release()
	if exists(stale) || exists(resolver) {
		t.Fatal("a stage whose lock is free was not swept")
	}
	if !exists(filepath.Join(held, "root", "work", "repomd.xml")) {
		t.Fatal("a stage another holder keeps was swept")
	}
	for name, path := range map[string]string{
		"a stage without its marker":             filepath.Join(creating, "root", "work", "repomd.xml"),
		"a directory another identity may write": filepath.Join(writable, "root", "work", "repomd.xml"),
		"a link to a stage":                      link,
		"the stage a link names":                 filepath.Join(elsewhere, "root", "work", "repomd.xml"),
		"an entry that is not a stage":           unrelated,
		"what a swept stage's link names":        kept,
	} {
		if !exists(path) {
			t.Fatalf("the sweep removed %s", name)
		}
	}
	if !beneath(parent, stage.Path) || !strings.HasPrefix(filepath.Base(stage.Path), "native-") {
		t.Fatalf("stage %s", stage.Path)
	}
	if !exists(filepath.Join(stage.Path, stageLock)) || !exists(filepath.Join(stage.Path, stageOwner)) {
		t.Fatal("a new stage holds no lock or marker")
	}
	info, err := os.Stat(stage.Path)
	if err != nil || info.Mode().Perm() != 0711 {
		t.Fatalf("a new stage is not traversable alone: %v %v", info, err)
	}
	stage.Release()
	stage.Release()
	if exists(stage.Path) {
		t.Fatal("a released stage remains")
	}
	if os.Geteuid() == 0 {
		foreign := stageAt(t, parent, "native-00000000000000ff", outside, true)
		if err := os.Lchown(foreign, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		again, err := NewStaging(parent).Stage(t.Context(), "rpmdb")
		if err != nil {
			t.Fatal(err)
		}
		again.Release()
		if !exists(filepath.Join(foreign, stageLock)) {
			t.Fatal("the sweep removed a stage another identity owns")
		}
	}
}

// A parent another identity could write, one reached through a link and one
// that is not a directory refuse, naming the parent and its correction.
func TestTheStagingRootRefusesAnUnsafeParent(t *testing.T) {
	for name, build := range map[string]func(string) error{
		"group-writable": func(parent string) error {
			if err := os.Mkdir(parent, 0700); err != nil {
				return err
			}
			return os.Chmod(parent, 0770)
		},
		"link": func(parent string) error {
			target := parent + "-target"
			if err := os.Mkdir(target, 0711); err != nil {
				return err
			}
			return os.Symlink(target, parent)
		},
		"file": func(parent string) error { return os.WriteFile(parent, nil, 0600) },
	} {
		t.Run(name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "staging")
			if err := build(parent); err != nil {
				t.Fatal(err)
			}
			_, err := NewStaging(parent).Stage(t.Context(), "resolver")
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "controller.setup" || !strings.Contains(found[0].Message, parent) || !strings.Contains(found[0].Remediation, parent) {
				t.Fatalf("an unsafe staging parent was not refused by name: %+v", found)
			}
		})
	}
	parent := filepath.Join(t.TempDir(), "staging")
	stage, err := NewStaging(parent).Stage(t.Context(), "resolver")
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Release()
	info, err := os.Stat(parent)
	if err != nil || info.Mode().Perm() != 0711 {
		t.Fatalf("an absent parent was not created traversable alone: %v %v", info, err)
	}
	if _, err := NewStaging(parent).Stage(t.Context(), "unknown"); err == nil {
		t.Fatal("a stage of an unknown kind was created")
	}
}

// nest makes levels directories beneath base, each inside the last.
func nest(t *testing.T, base string, levels int) {
	t.Helper()
	path := base
	for level := range levels {
		path = filepath.Join(path, "d"+strconv.Itoa(level))
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
}

// refusedByParent requires the refusal of a sweep: setup's code, a message
// naming the parent and what it holds, and a correction naming the parent.
func refusedByParent(t *testing.T, err error, parent, holds string) {
	t.Helper()
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "controller.setup" || !strings.Contains(found[0].Message, parent) || !strings.Contains(found[0].Message, holds) || !strings.Contains(found[0].Remediation, parent) {
		t.Fatalf("the sweep did not refuse naming %s and %q: %+v", parent, holds, found)
	}
}

// keepsItsClaim requires a stage the sweep could not finish to keep the lock
// and marker that make the next sweep fail closed rather than adopt it.
func keepsItsClaim(t *testing.T, stage string) {
	t.Helper()
	for _, name := range []string{stageOwner, stageLock} {
		if !exists(filepath.Join(stage, name)) {
			t.Fatalf("the stage beyond the bound lost its %s", name)
		}
	}
}

func stagingParentAt(t *testing.T) string {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "staging")
	if err := os.Mkdir(parent, 0711); err != nil {
		t.Fatal(err)
	}
	return parent
}

// A sweep reads no more than it declares: so many entries in the parent, and
// so many levels and entries in one stale stage, each bound inclusive. Past
// any of them Stage refuses naming the parent, creates nothing, and a stage it
// could not finish keeps its marker and lock, so every later sweep refuses too.
func TestTheStagingSweepRefusesBeyondItsBounds(t *testing.T) {
	outside := t.TempDir()
	t.Run("parent entries", func(t *testing.T) {
		parent := stagingParentAt(t)
		for entry := range maxParentEntries {
			if err := os.WriteFile(filepath.Join(parent, "entry-"+strconv.Itoa(entry)), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		stage, err := NewStaging(parent).Stage(t.Context(), "native")
		if err != nil {
			t.Fatalf("a parent holding as many entries as the bound refused: %v", err)
		}
		stage.Release()
		if err := os.WriteFile(filepath.Join(parent, "entry-"+strconv.Itoa(maxParentEntries)), nil, 0600); err != nil {
			t.Fatal(err)
		}
		_, err = NewStaging(parent).Stage(t.Context(), "native")
		refusedByParent(t, err, parent, "more than "+strconv.Itoa(maxParentEntries)+" entries")
		if names := stagingEntries(t, parent); len(names) != maxParentEntries+1 {
			t.Fatalf("a refused sweep left %d entries", len(names))
		}
	})
	t.Run("stage depth", func(t *testing.T) {
		parent := stagingParentAt(t)
		within := stageAt(t, parent, "native-00000000000000a1", outside, true)
		nest(t, within, maxStageTreeDepth)
		stage, err := NewStaging(parent).Stage(t.Context(), "native")
		if err != nil {
			t.Fatalf("a stale stage as deep as the bound refused: %v", err)
		}
		stage.Release()
		if exists(within) {
			t.Fatal("a stale stage within the depth bound stayed")
		}
		beyond := stageAt(t, parent, "native-00000000000000b2", outside, true)
		nest(t, beyond, maxStageTreeDepth+1)
		for range 2 {
			_, err = NewStaging(parent).Stage(t.Context(), "rpmdb")
			refusedByParent(t, err, parent, "could not be removed safely")
		}
		keepsItsClaim(t, beyond)
		if names := stagingEntries(t, parent); !slices.Equal(names, []string{"native-00000000000000b2"}) {
			t.Fatalf("a refused sweep created a stage: %q", names)
		}
	})
	t.Run("stage entries", func(t *testing.T) {
		parent := stagingParentAt(t)
		staging := NewStaging(parent)
		// A stage as stageAt leaves it holds six entries: its lock, marker and
		// root, root's work and link, and work's metadata.
		staging.entries = 6
		within := stageAt(t, parent, "native-00000000000000c3", outside, true)
		stage, err := staging.Stage(t.Context(), "native")
		if err != nil {
			t.Fatalf("a stale stage holding as many entries as the bound refused: %v", err)
		}
		stage.Release()
		if exists(within) {
			t.Fatal("a stale stage within the entry bound stayed")
		}
		beyond := stageAt(t, parent, "native-00000000000000d4", outside, true)
		if err := os.WriteFile(filepath.Join(beyond, "root", "work", "primary.xml"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		_, err = staging.Stage(t.Context(), "native")
		refusedByParent(t, err, parent, "could not be removed safely")
		keepsItsClaim(t, beyond)
	})
}

// A mount is never emptied. One a killed invocation left inside its stage
// makes every sweep refuse; one standing where a stage would is not a stage.
func TestTheStagingSweepNeverEmptiesAMount(t *testing.T) {
	mount := func(t *testing.T, dir string) {
		t.Helper()
		if err := syscall.Mount("tmpfs", dir, "tmpfs", 0, "mode=0700"); err != nil {
			t.Skipf("mounting a filesystem needs privilege this test does not have: %v", err)
		}
		t.Cleanup(func() { _ = syscall.Unmount(dir, syscall.MNT_DETACH) })
	}
	outside := t.TempDir()
	t.Run("inside a stage", func(t *testing.T) {
		parent := stagingParentAt(t)
		stale := stageAt(t, parent, "native-00000000000000e5", outside, true)
		mounted := filepath.Join(stale, "root", "mounted")
		if err := os.Mkdir(mounted, 0700); err != nil {
			t.Fatal(err)
		}
		mount(t, mounted)
		if err := os.WriteFile(filepath.Join(mounted, "host-file"), []byte("another filesystem's"), 0600); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			_, err := NewStaging(parent).Stage(t.Context(), "native")
			refusedByParent(t, err, parent, "could not be removed safely")
		}
		if !exists(filepath.Join(mounted, "host-file")) {
			t.Fatal("the sweep emptied a mount")
		}
		keepsItsClaim(t, stale)
	})
	t.Run("as a stage", func(t *testing.T) {
		parent := stagingParentAt(t)
		mounted := filepath.Join(parent, "native-00000000000000f6")
		if err := os.Mkdir(mounted, 0700); err != nil {
			t.Fatal(err)
		}
		mount(t, mounted)
		stageAt(t, parent, "native-00000000000000f6", outside, true)
		stage, err := NewStaging(parent).Stage(t.Context(), "native")
		if err != nil {
			t.Fatalf("a mount standing where a stage would refused staging: %v", err)
		}
		stage.Release()
		if !exists(filepath.Join(mounted, "root", "work", "repomd.xml")) {
			t.Fatal("the sweep emptied a mount")
		}
	})
}
