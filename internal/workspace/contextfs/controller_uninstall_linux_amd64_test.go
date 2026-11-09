//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	cache := filepath.Join(base, "bootwright", controllerContainer, installSubtree, "cache")
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
	mkdirs(t, 0700, filepath.Join(root, controllerContainer, installSubtree, "runtimes", "x"), staging)
	if err := os.WriteFile(filepath.Join(root, controllerContainer, installSubtree, "state.json"), []byte("{}"), 0600); err != nil {
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
		if err := os.WriteFile(filepath.Join(base, "bootwright", "registry.json"), []byte(`{"version":99,"contexts":[]}`+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		view, err := uninstallView(t, store)
		if err != nil || !view.RootExists {
			t.Fatalf("view = %+v (%#v)", view, diagnostics.Of(err))
		}
		uninstallDiagnostic(t, view.Unreadable, "context.state", "no registry.json this build can read")
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
	mkdirs(t, 0700, filepath.Join(leftover, controllerContainer, installSubtree))
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

// everyRootStore is an installed store whose log root lies in a parent of its
// own, as /var/log is, with a hosted root and a libvirt image prefix.
func everyRootStore(t *testing.T) (*Store, string) {
	t.Helper()
	base := t.TempDir()
	options := testOptions(filepath.Join(base, "bootwright"))
	options.Logs = filepath.Join(base, "log", "bootwright")
	options.LibvirtImages = filepath.Join(base, "images", "bootwright")
	store := New(options)
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		state := setupReceiptState()
		state.Format = prerequisites.SetupStateFormat
		return tx.Record(context.Background(), state)
	})
	mkdirs(t, 0700, filepath.Join(base, "log", "bootwright", "contexts", "lab", "operations", "op-1"),
		filepath.Join(base, "bootwright-hosted", "lab", "machines", "rhel-01", "erase", "op-1"))
	mkdirs(t, 0711, filepath.Join(base, "images", "bootwright"))
	for path, data := range map[string]string{
		filepath.Join(base, "log", "bootwright", "contexts", "lab", "operations", "op-1", "operation.jsonl"):    "{}\n",
		filepath.Join(base, "bootwright-hosted", "lab", "machines", "rhel-01", "erase", "op-1", "receipt.json"): "{}\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return store, base
}

// A plain uninstall removes the log root and the hosted root whole, a
// physical-erase receipt included, and an empty libvirt image prefix, each
// renamed aside within its own parent, leaving every sibling there.
func TestAnUninstallRemovesEveryRootItKeeps(t *testing.T) {
	store, base := everyRootStore(t)
	for _, sibling := range []string{filepath.Join(base, "log", "messages"), filepath.Join(base, "images", "operator.qcow2")} {
		if err := os.WriteFile(sibling, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	view, err := uninstallView(t, store)
	if err != nil || !view.RootExists || !view.LogsSeen || !view.HostedSeen || !view.ImagesSeen || view.StagingSeen ||
		len(view.GuestDisks) != 0 || view.Unreadable != nil || view.Hosted != filepath.Join(base, "bootwright-hosted") {
		t.Fatalf("view = %+v (%#v)", view, diagnostics.Of(err))
	}
	removed := uninstallState(t, store)
	want := []string{filepath.Join(base, "bootwright"), filepath.Join(base, "log", "bootwright"), filepath.Join(base, "bootwright-hosted"), filepath.Join(base, "images", "bootwright")}
	if !slices.Equal(removed, want) {
		t.Fatalf("removed = %q, want %q", removed, want)
	}
	for parent, survivor := range map[string]string{base: "images", filepath.Join(base, "log"): "messages", filepath.Join(base, "images"): "operator.qcow2"} {
		entries, err := os.ReadDir(parent)
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		if err != nil || !slices.Contains(names, survivor) || slices.ContainsFunc(names, func(name string) bool { return strings.Contains(name, "bootwright") }) {
			t.Fatalf("%s holds %q (%v)", parent, names, err)
		}
	}
}

// What the libvirt image prefix holds is named for the service to refuse, a
// link there is not Bootwright's, and a mounted log or hosted root refuses
// before any effect.
func TestAnUninstallNamesGuestDisksAndRefusesForeignRoots(t *testing.T) {
	store, base := everyRootStore(t)
	disk := filepath.Join(base, "images", "bootwright", "lab")
	mkdirs(t, 0700, filepath.Join(disk, "machines", "rhel-01"))
	view, err := uninstallView(t, store)
	if err != nil || !slices.Equal(view.GuestDisks, []string{disk}) {
		t.Fatalf("view = %+v (%#v)", view, diagnostics.Of(err))
	}
	for _, mounted := range []string{"bootwright-hosted", "log/bootwright"} {
		t.Run(mounted, func(t *testing.T) {
			store, base := everyRootStore(t)
			store.uninstallEntry = func(parent, name string) (syscall.Stat_t, error) {
				if filepath.Join(parent, name) == filepath.Join(base, mounted) {
					return syscall.Stat_t{}, syscall.EXDEV
				}
				var stat syscall.Stat_t
				err := syscall.Lstat(filepath.Join(parent, name), &stat)
				return stat, err
			}
			_, err := uninstallView(t, store)
			uninstallDiagnostic(t, err, "controller.uninstall", filepath.Join(base, mounted)+" is a mount point")
		})
	}
	store, base = everyRootStore(t)
	hosted := filepath.Join(base, "bootwright-hosted")
	if err := os.Chmod(hosted, 0770); err != nil {
		t.Fatal(err)
	}
	_, err = uninstallView(t, store)
	uninstallDiagnostic(t, err, "controller.uninstall", hosted+" is not a directory Bootwright owns")
}

// A registry of another version and state without a registry are another
// build's store, and an entry this build cannot attribute beside its own
// registry is unreadable too, each handed to the service as Unreadable under
// the same locks; a malformed registry of this version, a held root lock and a
// held setup lock refuse instead.
func TestAnUninstallHandsAnotherBuildsStoreToTheService(t *testing.T) {
	unreadable := map[string]func(root string){
		"another registry version": func(root string) {
			writePrivate(t, filepath.Join(root, "registry.json"), []byte(`{"version":99,"contexts":[]}`+"\n"))
		},
		"unsupported root state": func(root string) { mkdirs(t, 0700, filepath.Join(root, "host")) },
		"state without a registry": func(root string) {
			if err := os.Remove(filepath.Join(root, "registry.json")); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		},
	}
	for name, damage := range unreadable {
		t.Run(name, func(t *testing.T) {
			store, base := everyRootStore(t)
			damage(filepath.Join(base, "bootwright"))
			view, err := uninstallView(t, store)
			if err != nil || view.Unreadable == nil || !view.RootExists || view.Setup.Exists || !view.LogsSeen || !view.HostedSeen {
				t.Fatalf("view = %+v (%#v)", view, diagnostics.Of(err))
			}
			uninstallDiagnostic(t, view.Unreadable, "context.state", "")
		})
	}
	t.Run("a malformed registry", func(t *testing.T) {
		store, base := everyRootStore(t)
		writePrivate(t, filepath.Join(base, "bootwright", "registry.json"), []byte("{\n"))
		_, err := uninstallView(t, store)
		uninstallDiagnostic(t, err, "context.state", "malformed")
	})
	t.Run("a held setup lock", func(t *testing.T) {
		store, base := everyRootStore(t)
		writePrivate(t, filepath.Join(base, "bootwright", "registry.json"), []byte(`{"version":99,"contexts":[]}`+"\n"))
		lock, err := os.OpenFile(filepath.Join(base, "bootwright", controllerContainer, installSubtree, setupLockName), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		_, err = uninstallView(t, store)
		uninstallDiagnostic(t, err, "lifecycle.lease", "controller setup is held")
	})
	t.Run("a held root lock", func(t *testing.T) {
		store, base := everyRootStore(t)
		writePrivate(t, filepath.Join(base, "bootwright", "registry.json"), []byte(`{"version":99,"contexts":[]}`+"\n"))
		holder, err := os.Open(filepath.Join(base, "bootwright"))
		if err != nil {
			t.Fatal(err)
		}
		defer holder.Close()
		if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		_, err = uninstallView(t, store)
		uninstallDiagnostic(t, err, "lifecycle.lease", "held by another")
	})
}

// interruptedContext reports cancellation once after calls of Err have
// answered, as a killed uninstall stops between two effects.
type interruptedContext struct {
	context.Context
	calls *int
	after int
}

func (c interruptedContext) Err() error {
	*c.calls++
	if *c.calls > c.after {
		return context.Canceled
	}
	return nil
}

// A discard interrupted after its renames, over another build's store and
// guest disks, leaves renamed trees in /var/lib, /var/log and the image
// prefix's parent, which the next uninstall finds as leftovers and removes.
func TestTheNextUninstallFinishesAnInterruptedDiscard(t *testing.T) {
	store, base := everyRootStore(t)
	writePrivate(t, filepath.Join(base, "bootwright", "registry.json"), []byte(`{"version":99,"contexts":[]}`+"\n"))
	mkdirs(t, 0700, filepath.Join(base, "images", "bootwright", "lab", "machines", "rhel-01"))
	calls := 0
	err := store.Uninstall(context.Background(), func(tx prerequisites.UninstallTransaction) error {
		if tx.View().Unreadable == nil || len(tx.View().GuestDisks) != 1 {
			t.Fatalf("view = %+v", tx.View())
		}
		_, err := tx.RemoveState(interruptedContext{Context: context.Background(), calls: &calls, after: 1})
		return err
	})
	if err == nil {
		t.Fatal("the interrupted discard reported success")
	}
	view, err := uninstallView(t, store)
	if err != nil || view.RootExists || view.LogsSeen || view.HostedSeen || view.ImagesSeen || len(view.Leftovers) != 4 {
		t.Fatalf("view after the interruption = %+v (%#v)", view, diagnostics.Of(err))
	}
	for _, leftover := range view.Leftovers {
		if parent := filepath.Dir(leftover); parent != base && parent != filepath.Join(base, "log") && parent != filepath.Join(base, "images") {
			t.Fatalf("leftover %s lies outside the roots' parents", leftover)
		}
	}
	if removed := uninstallState(t, store); !slices.Equal(removed, view.Leftovers) {
		t.Fatalf("removed = %q, want %q", removed, view.Leftovers)
	}
	if view, err := uninstallView(t, store); err != nil || len(view.Leftovers) != 0 || view.RootExists {
		t.Fatalf("a final inspection = %+v (%v)", view, err)
	}
}

type noOwnedObjects struct{ proofs *int }

func (o noOwnedObjects) OwnedObjects(context.Context) ([]prerequisites.OwnedObject, error) {
	*o.proofs++
	return nil, nil
}

type refusingPresenter struct{}

func (refusingPresenter) PresentUninstallPlan(context.Context, prerequisites.UninstallPlan) error {
	return errors.New("a refused discard reached its plan")
}

func (refusingPresenter) RemovalOutput() prerequisites.RunOutput { return nil }

func (refusingPresenter) PresentControllerScope(context.Context, string, prerequisites.Report) error {
	return errors.New("a discard presented a controller scope")
}

func (refusingPresenter) PresentControllerPlan(context.Context, prerequisites.Report) error {
	return errors.New("a discard presented a controller plan")
}

// An entry this build cannot attribute beside a registry it decodes never
// hides what that registry names: the discard still refuses each context and
// each image, after the inspection read both under the root lock.
func TestADiscardBesideAnUnattributedEntryStillRefusesContextsAndMedia(t *testing.T) {
	store, sources := fixture(t)
	publish(t, store, "lab", sources)
	for name, stray := range map[string]string{"in the root": filepath.Join(store.options.Root, "notes.txt"), "in the host container": filepath.Join(store.options.Root, controllerContainer, "notes.txt")} {
		t.Run(name, func(t *testing.T) {
			mkdirs(t, 0700, filepath.Join(store.options.Root, controllerContainer))
			writePrivate(t, stray, nil)
			defer os.Remove(stray)
			view, err := uninstallView(t, store)
			if err != nil || !slices.Equal(view.Contexts, []string{"lab"}) {
				t.Fatalf("view = %+v (%#v)", view, diagnostics.Of(err))
			}
			uninstallDiagnostic(t, view.Unreadable, "context.state", unsupportedRootStateMessage)
			proofs := 0
			service := prerequisites.New(store, nil, nil, prerequisites.Ports{Owned: noOwnedObjects{&proofs}}, prerequisites.Options{Presenter: refusingPresenter{}})
			_, err = service.Uninstall(context.Background(), prerequisites.UninstallRequest{DiscardUnreadable: true})
			uninstallDiagnostic(t, err, "controller.uninstall", "this host holds contexts: lab")
			if proofs != 0 {
				t.Fatal("a refused discard ran the owned-object proof")
			}
		})
	}
	t.Run("media", func(t *testing.T) {
		store, base := everyRootStore(t)
		media := filepath.Join(base, "bootwright", "media")
		mkdirs(t, 0700, media)
		writePrivate(t, filepath.Join(media, "rhel.iso.json"), nil)
		writePrivate(t, filepath.Join(base, "bootwright", "notes.txt"), nil)
		view, err := uninstallView(t, store)
		if err != nil || view.Unreadable == nil || !slices.Equal(view.Media, []string{"rhel.iso"}) {
			t.Fatalf("view = %+v (%#v)", view, diagnostics.Of(err))
		}
	})
}

// A listing that fails or outgrows its bound is no unreadable store: it
// refuses as itself, never as Unreadable, even where the entries listed would
// each be unattributable, and a pending registry stage that cannot be read
// beside no registry refuses rather than reads as another build's.
func TestADiscardRefusesAListingOrReadFailureOfTheStore(t *testing.T) {
	for name, directory := range map[string]string{"the root": "", "the host container": controllerContainer} {
		t.Run(name, func(t *testing.T) {
			store, base := everyRootStore(t)
			parent := filepath.Join(base, "bootwright", directory)
			writePrivate(t, filepath.Join(parent, "stray"), nil)
			if view, err := uninstallView(t, store); err != nil || view.Unreadable == nil {
				t.Fatalf("one stray entry: view = %+v (%#v)", view, diagnostics.Of(err))
			}
			for i := range maxContexts + 1 {
				writePrivate(t, filepath.Join(parent, "stray-"+strconv.Itoa(i)), nil)
			}
			view, err := uninstallView(t, store)
			if view.Unreadable != nil || isUnattributedEntry(err) || isAnotherBuilds(err) {
				t.Fatalf("an over-bound listing read as unreadable: %+v (%v)", view, err)
			}
			uninstallDiagnostic(t, err, "context.state", unsupportedRootStateMessage)
		})
	}
	t.Run("an unsafe registry stage", func(t *testing.T) {
		store, base := everyRootStore(t)
		root := filepath.Join(base, "bootwright")
		if err := os.RemoveAll(filepath.Join(root, controllerContainer)); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, "registry.json")); err != nil {
			t.Fatal(err)
		}
		stage := filepath.Join(root, "pending-"+strings.Repeat("a", 32)+".json")
		writePrivate(t, stage, []byte("{}\n"))
		if view, err := uninstallView(t, store); err != nil || !isAnotherBuilds(view.Unreadable) {
			t.Fatalf("a readable foreign stage: view = %+v (%#v)", view, diagnostics.Of(err))
		}
		if err := os.Chmod(stage, 0644); err != nil {
			t.Fatal(err)
		}
		view, err := uninstallView(t, store)
		if view.Unreadable != nil || err == nil || isAnotherBuilds(err) {
			t.Fatalf("an unsafe stage read as another build's: %+v (%v)", view, err)
		}
		uninstallDiagnostic(t, err, "context.state", missingRegistryMessage)
	})
}

// A root whose parent a group or other user may write refuses before any
// effect unless that parent is sticky and the owner's, while such a parent
// with nothing in it to act on refuses nothing.
func TestAnUninstallRefusesAParentAnotherUserMayWrite(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "bootwright")))
	base := filepath.Dir(store.options.Root)
	mkdirs(t, 0777, base)
	if view, err := uninstallView(t, store); err != nil || view.RootExists || len(view.Leftovers) != 0 {
		t.Fatalf("an empty writable parent: view = %+v (%v)", view, err)
	}
	store, base = installedStore(t)
	for _, mode := range []os.FileMode{0770, 0757} {
		mkdirs(t, mode, base)
		_, err := uninstallView(t, store)
		uninstallDiagnostic(t, err, "controller.uninstall", base+" may be written by a user other than root")
	}
	mkdirs(t, 0777|os.ModeSticky, base)
	if view, err := uninstallView(t, store); err != nil || !view.RootExists {
		t.Fatalf("a sticky parent: view = %+v (%#v)", view, diagnostics.Of(err))
	}
	mkdirs(t, 0755, base)
	leftover := filepath.Join(base, ".bootwright-uninstall-"+strings.Repeat("a", 32))
	mkdirs(t, 0700, leftover)
	mkdirs(t, 0775, base)
	_, err := uninstallView(t, store)
	uninstallDiagnostic(t, err, "controller.uninstall", base+" may be written by a user other than root")
	if _, err := os.Lstat(leftover); err != nil {
		t.Fatalf("a refused uninstall removed the leftover (%v)", err)
	}
}

// A tree whose renamed name no longer reads as the tree uninstall inspected
// is renamed back, so no leftover a later run would remove remains, and the
// uninstall refuses.
func TestAnUninstallRenamesBackATreeThatChangedAcrossItsRename(t *testing.T) {
	store, base := installedStore(t)
	fired := 0
	store.uninstallEntry = func(parent, name string) (syscall.Stat_t, error) {
		var stat syscall.Stat_t
		err := syscall.Lstat(filepath.Join(parent, name), &stat)
		if strings.HasPrefix(name, ".bootwright-uninstall-") {
			fired++
			stat.Ino++
		}
		return stat, err
	}
	err := store.Uninstall(context.Background(), func(tx prerequisites.UninstallTransaction) error {
		_, err := tx.RemoveState(context.Background())
		return err
	})
	if fired != 1 {
		t.Fatalf("the renamed name was read %d times", fired)
	}
	uninstallDiagnostic(t, err, "context.state", "renamed it back: "+filepath.Join(base, "bootwright"))
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 1 || entries[0].Name() != "bootwright" {
		t.Fatalf("the parent holds %v (%v)", entries, err)
	}
	if _, err := store.ReadSetup(context.Background()); err != nil {
		t.Fatalf("the restored store does not read: %#v", diagnostics.Of(err))
	}
}

// A root of the store's owner but another group is not Bootwright's, as a
// leftover of that group is not, so no renamed copy of it is ever skipped.
func TestAnUninstallRefusesARootOfAnotherGroup(t *testing.T) {
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	other := slices.IndexFunc(groups, func(group int) bool { return group != os.Getegid() })
	if other < 0 {
		t.Skip("the test user belongs to no second group")
	}
	store, base := everyRootStore(t)
	hosted := filepath.Join(base, "bootwright-hosted")
	if err := os.Lchown(hosted, -1, groups[other]); err != nil {
		t.Fatal(err)
	}
	_, err = uninstallView(t, store)
	uninstallDiagnostic(t, err, "controller.uninstall", hosted+" is not a directory Bootwright owns")
}
