package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A refusal names a lifecycle operation entry relative to the state root, as
// every store refusal does, so it reads the same whatever the root's location
// on the host.
func TestALifecycleOperationEntryRefusalNamesItsStoreRelativePath(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	if err := syscall.Mkfifo(filepath.Join(operationsRoot(t, store, "example"), "op-1"), 0600); err != nil {
		t.Fatal(err)
	}
	err := store.ReadLifecycle(ctx, "example", func(view lifecycle.View) error {
		_, _, err := view.Operations().Read(ctx, "op-1/state.json", 1024)
		return err
	})
	reported := diagnostics.Of(err)
	const want = "lifecycle operation entry is not this store's own: contexts/example/state/operations/op-1"
	if len(reported) != 1 || reported[0].Code != "context.state" || !strings.HasPrefix(reported[0].Message, want) || strings.Contains(reported[0].Message, store.options.Root) {
		t.Fatalf("the refusal does not name its store-relative entry: %v %#v", err, reported)
	}
}

// Deciding whether a root without a registry is empty lists it through a
// fresh handle, so the held root handle keeps its directory offset for every
// later listing through it.
func TestReadingTheRegistryLeavesTheHeldRootListingAlone(t *testing.T) {
	ctx := context.Background()
	store, _ := fixture(t)
	if err := os.Mkdir(store.options.Root, 0700); err != nil {
		t.Fatal(err)
	}
	const stage = "pending-0123456789abcdef0123456789abcdef.json"
	if err := os.WriteFile(filepath.Join(store.options.Root, stage), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := store.openRoot(ctx, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer root.file.Close()
	if _, exists, err := readRegistry(ctx, root); exists || err == nil {
		t.Fatalf("a root holding only a foreign stage read as a registry: exists=%v err=%v", exists, err)
	}
	names, err := root.file.Readdirnames(-1)
	if err != nil || !slices.Equal(names, []string{stage}) {
		t.Fatalf("the held root handle lists %v (%v), want only %s", names, err, stage)
	}
}
