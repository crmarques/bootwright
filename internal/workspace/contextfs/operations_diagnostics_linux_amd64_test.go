package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// operationsRoot is where this store keeps one context's operation records. A
// test reaches it directly to plant the kind of entry an interrupted or foreign
// writer leaves behind.
func operationsRoot(t *testing.T, store *Store, name string) string {
	t.Helper()
	root := filepath.Join(store.options.Root, "contexts", name, "state", "operations")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

// Every lifecycle write measures the whole operations subtree before it
// allocates, so one entry this store does not own refuses every later write.
// The diagnostic has to name that entry: without the path it says only that
// something, somewhere, is unsafe, and an operator has no way to find it.
func TestAForeignOperationEntryIsRefusedByName(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	root := operationsRoot(t, store, "example")
	foreign := filepath.Join(root, "op-leftover")
	if err := os.Mkdir(foreign, 0755); err != nil {
		t.Fatal(err)
	}
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Operations().WriteExclusive(ctx, "index.json", []byte("{}\n"))
	})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "context.state" {
		t.Fatalf("refusal = %#v", reported)
	}
	if !strings.Contains(reported[0].Message, foreign) {
		t.Fatalf("the refusal does not name the entry it refused: %q", reported[0].Message)
	}
}

// Every atomic publication stages a pending name beside its target and renames
// it away, while every write and log append measures the whole subtree first.
// The walk lists a directory and then opens each entry separately, so a name it
// already listed can be gone by the time it opens it. That is a vanished entry,
// not a foreign one, and refusing the write over it is what failed a real first
// apply and then passed on a retry that could no longer reproduce it.
func TestAnEntryThatVanishesDuringMeasurementDoesNotRefuseTheWrite(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	root := operationsRoot(t, store, "example")
	vanishing := filepath.Join(root, "pending-0123456789abcdef.json")
	if err := os.WriteFile(vanishing, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	removed := false
	store.fail = func(name string) error {
		if name != "measure-operation-entry" || removed {
			return nil
		}
		removed = true
		return os.Remove(vanishing)
	}
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Operations().WriteExclusive(ctx, "index.json", []byte("{}\n"))
	})
	if !removed {
		t.Fatal("the measuring walk never reached an entry")
	}
	if err != nil {
		t.Fatalf("a vanished entry refused the write: %#v", diagnostics.Of(err))
	}
}

// A file where a directory component belongs fails to open as a directory,
// which is neither absence nor a permission fault; it is named the same way.
func TestAFileInPlaceOfAnOperationDirectoryIsRefusedByName(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	root := operationsRoot(t, store, "example")
	blocking := filepath.Join(root, "op-1")
	if err := os.WriteFile(blocking, []byte("not a directory\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Operations().WriteExclusive(ctx, "op-1/blocks/state.json", []byte("{}\n"))
	})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || !strings.Contains(reported[0].Message, blocking) {
		t.Fatalf("the refusal does not name the blocking entry: %#v", reported)
	}
}
