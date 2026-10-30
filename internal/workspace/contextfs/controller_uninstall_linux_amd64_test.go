//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func uninstallView(t *testing.T, store *Store) (prerequisites.UninstallView, error) {
	t.Helper()
	var view prerequisites.UninstallView
	err := store.Uninstall(context.Background(), func(tx prerequisites.UninstallTransaction) error {
		view = tx.View()
		return nil
	})
	return view, err
}

func uninstallState(t *testing.T, store *Store) []string {
	t.Helper()
	var removed []string
	if err := store.Uninstall(context.Background(), func(tx prerequisites.UninstallTransaction) error {
		var err error
		removed, err = tx.RemoveState(context.Background())
		return err
	}); err != nil {
		t.Fatalf("the uninstall failed: %#v", diagnostics.Of(err))
	}
	return removed
}

func installedStore(t *testing.T) (*Store, string) {
	t.Helper()
	base := t.TempDir()
	store := New(testOptions(filepath.Join(base, "bootwright")))
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		state := setupReceiptState()
		state.Format = prerequisites.SetupStateFormat
		return tx.Record(context.Background(), state)
	})
	return store, base
}

func mkdirs(t *testing.T, mode os.FileMode, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
}

func uninstallDiagnostic(t *testing.T, err error, code, message string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != code || !strings.Contains(reported[0].Message, message) {
		t.Fatalf("refusal = %#v, want %s %q", reported, code, message)
	}
}

// Uninstall reads the install record under both locks, then renames the state
// root and the staging directory aside in their parent and removes each
// renamed tree without following a link: a link inside to a file outside is
// removed as itself and the file it names survives.
func TestAnUninstallRenamesTheStateAsideThenRemovesIt(t *testing.T) {
	store, base := installedStore(t)
	outside := filepath.Join(base, "operator-data")
	if err := os.WriteFile(outside, []byte("keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(base, "bootwright", "setup", "cache")
	mkdirs(t, 0700, filepath.Join(cache, "wheels", "deep", "er"))
	if err := os.Symlink(outside, filepath.Join(cache, "escape")); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(base, "bootwright-staging")
	mkdirs(t, 0711, staging)
	mkdirs(t, 0700, filepath.Join(staging, "resolver-0123456789abcdef"), filepath.Join(staging, "runs", "run-1"))
	if err := os.WriteFile(filepath.Join(staging, "resolver-0123456789abcdef", "lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(staging, "runs", "run-1", "escape")); err != nil {
		t.Fatal(err)
	}
	view, err := uninstallView(t, store)
	if err != nil {
		t.Fatalf("the inspection failed: %#v", diagnostics.Of(err))
	}
	if !view.RootExists || !view.StagingSeen || len(view.Contexts) != 0 || len(view.Media) != 0 || len(view.Leftovers) != 0 ||
		view.Setup.State.Receipt == nil || view.Setup.State.Receipt.Packages[0].Provider != "podman-5:5.8.1-1.fc44.x86_64" {
		t.Fatalf("view = %+v", view)
	}
	removed := uninstallState(t, store)
	if !slices.Equal(removed, []string{filepath.Join(base, "bootwright"), staging}) {
		t.Fatalf("removed = %q", removed)
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 1 || entries[0].Name() != "operator-data" {
		t.Fatalf("the parent holds %v (%v)", entries, err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep\n" {
		t.Fatalf("the linked file = %q (%v)", data, err)
	}
	if _, err := store.ReadSetup(context.Background()); err != nil {
		t.Fatalf("a removed store reads as absent: %#v", err)
	}
	if view, err := uninstallView(t, store); err != nil || view.RootExists || view.StagingSeen || len(view.Leftovers) != 0 {
		t.Fatalf("a second inspection = %+v (%v)", view, err)
	}
}

// An uninstall interrupted after its rename is finished by the next one, which
// removes every renamed tree of the store's owner and reports it, and leaves
// a link or a file that only looks like one.
func TestTheNextUninstallFinishesAnInterruptedOne(t *testing.T) {
	base := t.TempDir()
	store := New(testOptions(filepath.Join(base, "bootwright")))
	root := filepath.Join(base, ".bootwright-uninstall-"+strings.Repeat("a", 32))
	staging := filepath.Join(base, ".bootwright-staging-uninstall-"+strings.Repeat("b", 32))
	mkdirs(t, 0700, filepath.Join(root, "setup", "runtimes", "x"), staging)
	if err := os.WriteFile(filepath.Join(root, "setup", "state.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, ".bootwright-uninstall-"+strings.Repeat("c", 32))
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	short := filepath.Join(base, ".bootwright-uninstall-abc")
	mkdirs(t, 0700, short)
	view, err := uninstallView(t, store)
	if err != nil || view.RootExists || view.StagingSeen || !slices.Equal(view.Leftovers, []string{staging, root}) {
		t.Fatalf("view = %+v (%v)", view, err)
	}
	if removed := uninstallState(t, store); !slices.Equal(removed, []string{staging, root}) {
		t.Fatalf("removed = %q", removed)
	}
	for _, path := range []string{root, staging} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived (%v)", path, err)
		}
	}
	for _, path := range []string{link, short} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("%s was removed (%v)", path, err)
		}
	}
}

// A context or an image is named for the service to refuse; a held staging
// extraction, a held root lock and a held setup lock each refuse busy rather
// than wait; and a store another build wrote refuses with its own remedy.
func TestAnUninstallInspectionRefusesWhatItMustNotRemove(t *testing.T) {
	t.Run("contexts and media", func(t *testing.T) {
		store, sources := fixture(t)
		publish(t, store, "lab", sources)
		media := filepath.Join(store.options.Root, "media")
		mkdirs(t, 0700, media)
		for _, name := range []string{"rhel.iso", "rhel.iso.json", "fedora.iso.json"} {
			if err := os.WriteFile(filepath.Join(media, name), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		view, err := uninstallView(t, store)
		if err != nil || !slices.Equal(view.Contexts, []string{"lab"}) || !slices.Equal(view.Media, []string{"fedora.iso", "rhel.iso"}) {
			t.Fatalf("view = %+v (%#v)", view, diagnostics.Of(err))
		}
	})
	t.Run("a held staging extraction", func(t *testing.T) {
		store, base := installedStore(t)
		stage := filepath.Join(base, "bootwright-staging", "native-0123456789abcdef")
		mkdirs(t, 0711, filepath.Dir(stage))
		mkdirs(t, 0700, stage)
		lock, err := os.OpenFile(filepath.Join(stage, "lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		_, err = uninstallView(t, store)
		uninstallDiagnostic(t, err, "lifecycle.lease", "staging extraction")
	})
	t.Run("the root lock", func(t *testing.T) {
		store, _ := installedStore(t)
		if err := store.Transact(context.Background(), false, nil, func(contexts.Transaction) error {
			_, err := uninstallView(t, store)
			uninstallDiagnostic(t, err, "lifecycle.lease", "held by another")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("the setup lock", func(t *testing.T) {
		store, _ := installedStore(t)
		mutateSetup(t, store, func(prerequisites.SetupTransaction) error {
			_, err := uninstallView(t, store)
			uninstallDiagnostic(t, err, "lifecycle.lease", "controller setup is held")
			return nil
		})
	})
	t.Run("another build's store", func(t *testing.T) {
		store, base := installedStore(t)
		if err := os.WriteFile(filepath.Join(base, "bootwright", "registry.json"), []byte(`{"version":5,"contexts":[]}`+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := uninstallView(t, store)
		uninstallDiagnostic(t, err, "context.state", "registry version 5")
	})
	t.Run("a staging directory of another type", func(t *testing.T) {
		store, base := installedStore(t)
		if err := os.Symlink(t.TempDir(), filepath.Join(base, "bootwright-staging")); err != nil {
			t.Fatal(err)
		}
		_, err := uninstallView(t, store)
		uninstallDiagnostic(t, err, "controller.uninstall", "is not a directory Bootwright owns")
	})
}

// A host with no state root, no staging directory and no renamed tree reads
// empty, and removing its state removes nothing.
func TestAnUninstallOfAnEmptyHostRemovesNothing(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "bootwright")))
	view, err := uninstallView(t, store)
	if err != nil || view.RootExists || view.StagingSeen || len(view.Leftovers) != 0 {
		t.Fatalf("view = %+v (%v)", view, err)
	}
	if removed := uninstallState(t, store); len(removed) != 0 {
		t.Fatalf("removed = %q", removed)
	}
}

// A state root or staging directory that is its own mount, even a bind mount
// on the same filesystem, refuses during inspection, before any effect. A
// bind mount needs privilege, so the entry read stands in for one.
func TestAnUninstallRefusesAMountedRootOrStagingBeforeAnyEffect(t *testing.T) {
	for _, mounted := range []string{"bootwright", "bootwright-staging"} {
		t.Run(mounted, func(t *testing.T) {
			store, base := installedStore(t)
			mkdirs(t, 0711, filepath.Join(base, "bootwright-staging"))
			store.uninstallEntry = func(parent, name string) (syscall.Stat_t, error) {
				if name == mounted {
					return syscall.Stat_t{}, syscall.EXDEV
				}
				var stat syscall.Stat_t
				err := syscall.Lstat(filepath.Join(parent, name), &stat)
				return stat, err
			}
			called := false
			err := store.Uninstall(context.Background(), func(prerequisites.UninstallTransaction) error {
				called = true
				return nil
			})
			uninstallDiagnostic(t, err, "controller.uninstall", filepath.Join(base, mounted)+" is a mount point")
			if called {
				t.Fatal("a mounted tree reached the callback")
			}
		})
	}
}

// Renamed trees are removed before this run renames anything, so a rename
// that then fails strands none of them.
func TestAnUninstallFinishesLeftoversBeforeItsOwnRenames(t *testing.T) {
	store, base := installedStore(t)
	leftover := filepath.Join(base, ".bootwright-uninstall-"+strings.Repeat("d", 32))
	mkdirs(t, 0700, filepath.Join(leftover, "setup"))
	err := store.Uninstall(context.Background(), func(tx prerequisites.UninstallTransaction) error {
		if err := os.Rename(filepath.Join(base, "bootwright"), filepath.Join(base, "moved")); err != nil {
			t.Fatal(err)
		}
		mkdirs(t, 0700, filepath.Join(base, "bootwright"))
		_, err := tx.RemoveState(context.Background())
		return err
	})
	uninstallDiagnostic(t, err, "context.state", "replaced while uninstall inspected it")
	if _, err := os.Lstat(leftover); !os.IsNotExist(err) {
		t.Fatalf("the leftover survived a failed rename (%v)", err)
	}
}

// A staging directory another user may write is not Bootwright's; a file or a
// held tree named like a leftover is never removed, and a held one refuses
// busy, so two finishing runs never race.
func TestAnUninstallLeavesWhatIsNotItsOwn(t *testing.T) {
	store, base := installedStore(t)
	staging := filepath.Join(base, "bootwright-staging")
	mkdirs(t, 0731, staging)
	_, err := uninstallView(t, store)
	uninstallDiagnostic(t, err, "controller.uninstall", "is not a directory Bootwright owns")
	mkdirs(t, 0711, staging)
	file := filepath.Join(base, ".bootwright-uninstall-"+strings.Repeat("e", 32))
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if view, err := uninstallView(t, store); err != nil || len(view.Leftovers) != 0 {
		t.Fatalf("view = %+v (%v)", view, err)
	}
	leftover := filepath.Join(base, ".bootwright-uninstall-"+strings.Repeat("f", 32))
	mkdirs(t, 0700, leftover)
	holder, err := os.Open(leftover)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	_, err = uninstallView(t, store)
	uninstallDiagnostic(t, err, "lifecycle.lease", "another Bootwright command holds "+leftover)
	if _, err := os.Lstat(file); err != nil {
		t.Fatalf("the look-alike file was removed (%v)", err)
	}
}
