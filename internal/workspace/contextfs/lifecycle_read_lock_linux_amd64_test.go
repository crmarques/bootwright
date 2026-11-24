//go:build linux && amd64

package contextfs

import (
	"context"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A lifecycle read takes the shared root lock and no lease, so it runs beside
// an operation that holds its context's lease for its effects, and it refuses
// context.busy only while another command holds the root lock exclusively. A
// bounded run, which diff --observe is, takes the shared lease and refuses
// context.busy while the lease is held.
func TestALifecycleReadRunsBesideAHeldLeaseAndRefusesBusyOnlyOnTheRootLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	root, err := store.openRoot(ctx, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer root.File.Close()
	container, dir, err := openContext(root, record)
	if err != nil {
		t.Fatal(err)
	}
	defer container.File.Close()
	defer dir.File.Close()
	if err := syscall.Flock(int(dir.File.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	read := func() error {
		return store.ReadLifecycle(ctx, record.Name, func(lifecycle.View) error { return nil })
	}
	if err := read(); err != nil {
		t.Fatalf("a read beside a held lease = %#v", diagnostics.Of(err))
	}
	run := store.RunLifecycle(ctx, record.Name, func(lifecycle.RunView) error { return nil })
	if reported := diagnostics.Of(run); len(reported) != 1 || reported[0].Code != "context.busy" {
		t.Fatalf("a bounded run beside a held lease = %#v", reported)
	}
	if err := syscall.Flock(int(root.File.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if reported := diagnostics.Of(read()); len(reported) != 1 || reported[0].Code != "context.busy" {
		t.Fatalf("a read beside the exclusive root lock = %#v", reported)
	}
}
