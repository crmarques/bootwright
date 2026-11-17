//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func lockFree(t *testing.T, path string) error {
	t.Helper()
	probe, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	return syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func copyDescriptor(t *testing.T, file *os.File) {
	t.Helper()
	copied, err := syscall.Dup(int(file.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(copied) })
}

// A tool area unlocks its directory before it closes it, so a copy of the
// descriptor a child forked meanwhile still holds never keeps the next
// stage out.
func TestToolAreaUnlocksWhileACopyOfItsDescriptorLives(t *testing.T) {
	t.Parallel()
	store := boundFixture(t)
	var path string
	err := withClientArea(t, store, func(_ lifecycle.Transaction, area prerequisites.BundleArea) error {
		held := area.(*controllerBundleArea).dir
		path = held.path
		copyDescriptor(t, held.file)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lockFree(t, path); err != nil {
		t.Fatalf("the closed tool area stays locked for a copy of its descriptor: %v", err)
	}
}

// Releasing a runtime hold unlocks it before closing it, so a copy of its
// descriptor a child forked meanwhile still holds never keeps retirement out.
func TestRuntimeReleaseUnlocksWhileACopyOfItsDescriptorLives(t *testing.T) {
	t.Parallel()
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	digest := setupDigest(6)
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		_, err := tx.PublishRuntime(context.Background(), stageRuntime(t, tx, "held", digest), digest)
		return err
	})
	hold, err := store.HoldRuntime(context.Background(), digest)
	if err != nil {
		t.Fatal(err)
	}
	copyDescriptor(t, hold.(*runtimeHold).file)
	hold.Release()
	var retired bool
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		var err error
		retired, err = tx.RetireRuntime(context.Background(), digest)
		return err
	})
	if !retired {
		t.Fatal("retirement skipped a released runtime a copy of its descriptor still names")
	}
}
