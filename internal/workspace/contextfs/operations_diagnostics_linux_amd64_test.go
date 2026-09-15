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
