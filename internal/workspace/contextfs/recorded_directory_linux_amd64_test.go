//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/logview"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

var recordedDevice = regexp.MustCompile(`"directoryDevice":([1-9][0-9]*)`)

// renumberRecordedDevices rewrites every directory device the registry and
// the controller record hold, in place, as a reboot that gives the btrfs
// subvolume holding the store another anonymous device number leaves them:
// the same directories, the same inodes, a device number that no longer
// matches. It returns how many it rewrote.
func renumberRecordedDevices(t *testing.T, store *Store) int {
	t.Helper()
	count := 0
	for _, path := range []string{
		filepath.Join(store.options.Root, "registry.json"),
		filepath.Join(store.options.Root, controllerContainer, controllerSubtree, "state.json"),
	} {
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		next := recordedDevice.ReplaceAllFunc(data, func(match []byte) []byte {
			value, err := strconv.ParseUint(string(recordedDevice.FindSubmatch(match)[1]), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			count++
			return []byte(`"directoryDevice":` + strconv.FormatUint(value+2, 10))
		})
		if err := os.WriteFile(path, next, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return count
}

func statOf(t *testing.T, path string) syscall.Stat_t {
	t.Helper()
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		t.Fatal(err)
	}
	return stat
}

func incarnationOf(t *testing.T, store *Store, name string) string {
	t.Helper()
	var incarnation string
	if err := store.ReadLifecycle(context.Background(), name, func(view lifecycle.View) error {
		incarnation = view.Identity().Incarnation
		return nil
	}); err != nil {
		t.Fatalf("the context does not open: %#v", diagnostics.Of(err))
	}
	return incarnation
}

// A store whose recorded directory devices differ from the ones its
// directories report now, while every inode matches, is the store a reboot
// left on btrfs, and opens: reads, registry transactions, lifecycle
// transactions, bundle reads and a tool area's retirement all proceed, and the
// context's incarnation names the recorded device, so a reboot leaves it as
// it was.
func TestAStoreWhoseRecordedDevicesARebootRenumberedOpens(t *testing.T) {
	ctx := context.Background()
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	sealedBundleFixture(t, store, record)
	if err := publishClients(store, clientClosure, true); err != nil {
		t.Fatalf("client publication failed: %#v", diagnostics.Of(err))
	}
	before := incarnationOf(t, store, record.Name)
	if count := renumberRecordedDevices(t, store); count < 4 {
		t.Fatalf("renumbered %d recorded devices, want the controller's, the context's and both tool areas'", count)
	}
	if _, err := store.View(ctx); err != nil {
		t.Fatalf("the registry view refused: %#v", diagnostics.Of(err))
	}
	if after, want := incarnationOf(t, store, record.Name), strconv.FormatUint(record.DirectoryDevice+2, 10)+before[len(strconv.FormatUint(record.DirectoryDevice, 10)):]; after != want {
		t.Fatalf("the incarnation is %q, want %q, which names the recorded device and not the one the directory reports", after, want)
	}
	if err := store.ReadController(ctx, record.Name, func(view prerequisites.StorageView) error {
		area, err := view.OpenBundle(ctx, sealedToolArea)
		if err != nil {
			return err
		}
		if err := area.Verify(ctx); err != nil {
			return err
		}
		location, err := area.Location(ctx)
		if err == nil && location.Device != uint64(statOf(t, location.Path).Dev) {
			t.Fatalf("the area names device %d, not the one it is on now", location.Device)
		}
		return err
	}); err != nil {
		t.Fatalf("the sealed tool area does not open: %#v", diagnostics.Of(err))
	}
	if err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		return tx.Bind(ctx, "controller", syntheticHost(t))
	}); err != nil {
		t.Fatalf("a lifecycle transaction refused: %#v", diagnostics.Of(err))
	}
	if err := retireToolAreas(store, record.Name, clientClosure); err != nil {
		t.Fatalf("the retirement refused: %#v", diagnostics.Of(err))
	}
	requireAreas(t, store, clientClosure)
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		_, err := tx.MutationState(ctx, record.Name)
		return err
	}); err != nil {
		t.Fatalf("a registry transaction refused: %#v", diagnostics.Of(err))
	}
}

// Every other place that proves a directory against its record opens after the
// same renumbering: the log and run reads, the trust area, a retained input
// revision, a controller record whose tool area is sealed, and the retry of an
// interrupted initialization.
func TestEveryRecordedDirectorySiteOpensAfterARenumbering(t *testing.T) {
	ctx := context.Background()
	t.Run("opened contexts", func(t *testing.T) {
		store, retained, selected := retainedFixture(t)
		sealedBundleFixture(t, store, selected)
		if count := renumberRecordedDevices(t, store); count < 3 {
			t.Fatalf("renumbered %d recorded devices, want the controller's, the context's and a tool area's", count)
		}
		if err := store.ReadLogs(ctx, selected.Name, func(logview.View) error { return nil }); err != nil {
			t.Fatalf("the log read refused: %#v", diagnostics.Of(err))
		}
		if _, err := store.ReadHostKeys(ctx, selected.Name); err != nil {
			t.Fatalf("the trust read refused: %#v", diagnostics.Of(err))
		}
		if err := store.ReplaceHostKeys(ctx, selected.Name, []byte("{}\n"), nil); err != nil {
			t.Fatalf("the trust publication refused: %#v", diagnostics.Of(err))
		}
		sources, err := readRevision(t, store, retained.Revision)
		if err != nil || len(sources.Files) != 1 || string(sources.Files[0].Bytes()) != "version: original\n" {
			t.Fatalf("the retained revision read %+v (%v)", sources, diagnostics.Of(err))
		}
		if err := store.ReadController(ctx, selected.Name, func(prerequisites.StorageView) error { return nil }); err != nil {
			t.Fatalf("the controller read refused: %#v", diagnostics.Of(err))
		}
	})
	t.Run("an interrupted initialization", func(t *testing.T) {
		store, _ := fixture(t)
		config := contexts.DefaultConfiguration("pending").Canonical()
		interrupted := errors.New("the first attempt stopped after it recorded the directory")
		err := store.Transact(ctx, true, nil, func(tx contexts.Transaction) error {
			if _, err := tx.Reserve(ctx, "pending", "", config); err != nil {
				return err
			}
			return interrupted
		})
		if err == nil {
			t.Fatal("the interrupted first attempt succeeded")
		}
		registry, err := store.View(ctx)
		if err != nil || len(registry.Contexts) != 1 || registry.Contexts[0].Mode != contexts.Initializing || registry.Contexts[0].DirectoryInode == 0 {
			t.Fatalf("the first attempt left %#v (%v), want an initializing context with a recorded directory", registry, err)
		}
		if renumberRecordedDevices(t, store) < 1 {
			t.Fatal("no recorded device was renumbered")
		}
		if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
			_, err := tx.Reserve(ctx, "pending", "", config)
			return err
		}); err != nil {
			t.Fatalf("the retry refused: %#v", diagnostics.Of(err))
		}
	})
}

// A recorded inode the directory at its path no longer has is another
// directory, whatever device either names, and still refuses as replaced.
func TestADirectoryWithAnotherInodeStillRefusesAsReplaced(t *testing.T) {
	t.Run("context", func(t *testing.T) {
		store, record := lifecycleFixture(t)
		renumberRecordedDevices(t, store)
		contextDir := filepath.Join(store.options.Root, "contexts", record.Name)
		kept := store.options.Root + "-record"
		if err := os.Rename(contextDir, kept); err != nil {
			t.Fatal(err)
		}
		copyTree(t, kept, contextDir)
		err := store.ReadLifecycle(context.Background(), record.Name, func(lifecycle.View) error { return nil })
		refusedWith(t, err, "context directory was replaced")
	})
	t.Run("controller", func(t *testing.T) {
		store, record := lifecycleFixture(t)
		sealedBundleFixture(t, store, record)
		renumberRecordedDevices(t, store)
		controllerDir := filepath.Join(store.options.Root, controllerContainer, controllerSubtree)
		kept := store.options.Root + "-record"
		if err := os.Rename(controllerDir, kept); err != nil {
			t.Fatal(err)
		}
		copyTree(t, kept, controllerDir)
		_, err := store.View(context.Background())
		refusedWith(t, err, "controller directory was replaced")
	})
}

// A directory is the recorded one only with the recorded inode and on the
// filesystem of the directory holding it now; the device either had when it
// was recorded plays no part. A test cannot mount, so the comparison is held
// to fabricated status values.
func TestARecordedDirectoryIsItsInodeOnItsParentsFilesystem(t *testing.T) {
	parent := syscall.Stat_t{Dev: 101, Ino: 7}
	for _, test := range []struct {
		name  string
		child syscall.Stat_t
		inode uint64
		want  bool
	}{
		{name: "the recorded inode on its parent's filesystem", child: syscall.Stat_t{Dev: 101, Ino: 1001}, inode: 1001, want: true},
		{name: "another inode on its parent's filesystem", child: syscall.Stat_t{Dev: 101, Ino: 1002}, inode: 1001},
		{name: "the recorded inode on another filesystem than its parent", child: syscall.Stat_t{Dev: 102, Ino: 1001}, inode: 1001},
		{name: "no recorded inode", child: syscall.Stat_t{Dev: 101}, inode: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := recordedDirectory(test.child, parent, test.inode); got != test.want {
				t.Fatalf("recordedDirectory = %t, want %t", got, test.want)
			}
		})
	}
	orphan := &directory{identity: syscall.Stat_t{Dev: 101, Ino: 1001}}
	if orphan.recorded(1001) {
		t.Fatal("a directory held without its parent was taken as recorded")
	}
}
