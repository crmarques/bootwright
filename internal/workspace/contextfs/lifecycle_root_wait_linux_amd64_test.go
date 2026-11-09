//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const rootHold = 150 * time.Millisecond

func holdRoot(t *testing.T, store *Store, how int) func() {
	t.Helper()
	file, err := os.Open(store.options.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB); err != nil {
		file.Close()
		t.Fatal(err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			file.Close()
		})
	}
}

func rootWaitClaim() []prerequisites.HostReservation {
	return []prerequisites.HostReservation{{Context: "example", Kind: "test", Service: "service", Keys: []string{"path:/var/lib/example"}}}
}

func reservationsHeld(t *testing.T, store *Store) int {
	t.Helper()
	held := 0
	if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		held = len(view.State.Reservations)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return held
}

func rootWaitFixture(t *testing.T) *Store {
	t.Helper()
	store, sources := fixture(t)
	publish(t, store, "example", sources)
	publishControllerState(t, store, syntheticControllerState(t))
	return store
}

func TestAnInOperationRootTransactionWaitsOutAHeldRootLock(t *testing.T) {
	for name, how := range map[string]int{"shared": syscall.LOCK_SH, "exclusive": syscall.LOCK_EX} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := rootWaitFixture(t)
			var waited time.Duration
			if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
				release := holdRoot(t, store, how)
				defer release()
				timer := time.AfterFunc(rootHold, release)
				defer timer.Stop()
				started := time.Now()
				err := tx.Reserve(ctx, rootWaitClaim())
				waited = time.Since(started)
				return err
			}); err != nil {
				t.Fatalf("the in-operation reservation refused a briefly held root lock: %v", err)
			}
			if waited < rootHold {
				t.Fatalf("the reservation published after %v while the root lock was held for %v", waited, rootHold)
			}
			if held := reservationsHeld(t, store); held != 1 {
				t.Fatalf("the controller record holds %d reservations, want 1", held)
			}
		})
	}
}

func TestAnInOperationBundleReadWaitsOutAHeldRootLock(t *testing.T) {
	store := boundFixture(t)
	ctx := context.Background()
	if err := withClientArea(t, store, func(_ lifecycle.Transaction, area prerequisites.BundleArea) error {
		release := holdRoot(t, store, syscall.LOCK_EX)
		defer release()
		timer := time.AfterFunc(rootHold, release)
		defer timer.Stop()
		return area.EnsureDirectory(ctx, "tools/helm")
	}); err != nil {
		t.Fatalf("the in-operation bundle write refused a briefly held root lock: %v", err)
	}
}

func TestAnInOperationRootWaitPastItsBoundRefusesBeforeItPublishes(t *testing.T) {
	ctx := context.Background()
	store := rootWaitFixture(t)
	store.rootWait = rootHold
	var waited time.Duration
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		release := holdRoot(t, store, syscall.LOCK_SH)
		defer release()
		started := time.Now()
		err := tx.Reserve(ctx, rootWaitClaim())
		waited = time.Since(started)
		return err
	})
	expectBusy(t, err)
	if waited < rootHold {
		t.Fatalf("the reservation refused after %v, before its %v bound", waited, rootHold)
	}
	if held := reservationsHeld(t, store); held != 0 {
		t.Fatalf("a refused reservation published %d reservations", held)
	}
}

func TestCancellationEndsAnInOperationRootWait(t *testing.T) {
	store := rootWaitFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var waited time.Duration
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		release := holdRoot(t, store, syscall.LOCK_EX)
		defer release()
		timer := time.AfterFunc(rootHold, cancel)
		defer timer.Stop()
		started := time.Now()
		err := tx.Reserve(ctx, rootWaitClaim())
		waited = time.Since(started)
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled root wait returned %v, want context.Canceled", err)
	}
	if waited > rootHold+5*time.Second {
		t.Fatalf("the cancelled root wait lasted %v", waited)
	}
	if held := reservationsHeld(t, store); held != 0 {
		t.Fatalf("a cancelled reservation published %d reservations", held)
	}
}

func TestACancelledRootWaitTakesNoLock(t *testing.T) {
	store := rootWaitFixture(t)
	root, err := store.openRoot(context.Background(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer root.file.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := awaitRoot(ctx, store, root, syscall.LOCK_EX); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled root wait over a free root lock returned %v, want context.Canceled", err)
	}
	holdRoot(t, store, syscall.LOCK_EX)()
}

func TestAMutatorWaitsOutALogReadersHoldThenRefusesPastTheBound(t *testing.T) {
	ctx := context.Background()
	store := rootWaitFixture(t)
	release := holdRoot(t, store, syscall.LOCK_SH)
	timer := time.AfterFunc(rootHold, release)
	defer timer.Stop()
	started := time.Now()
	entered := false
	if err := store.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error { entered = true; return nil }); err != nil || !entered {
		t.Fatalf("a mutator beside a reader's short hold = %v, entered %t", err, entered)
	}
	if waited := time.Since(started); waited < rootHold {
		t.Fatalf("the mutator entered after %v, while the reader still held the root", waited)
	}
	store.rootWait = rootHold
	held := holdRoot(t, store, syscall.LOCK_SH)
	defer held()
	started = time.Now()
	expectBusy(t, store.Transact(ctx, false, nil, func(contexts.Transaction) error { t.Fatal("a mutator entered a held root"); return nil }))
	if waited := time.Since(started); waited < rootHold {
		t.Fatalf("the transaction refused after %v, before its %v bound", waited, rootHold)
	}
	canceled, cancel := context.WithCancel(ctx)
	time.AfterFunc(rootHold/3, cancel)
	store.rootWait = time.Hour
	if err := store.Transact(canceled, false, nil, func(contexts.Transaction) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled registry transaction wait = %v, want context.Canceled", err)
	}
}

func waitsOutAReader(t *testing.T, store *Store, name string, mutate func() error) {
	t.Helper()
	release := holdRoot(t, store, syscall.LOCK_SH)
	timer := time.AfterFunc(rootHold, release)
	defer timer.Stop()
	started := time.Now()
	if err := mutate(); err != nil {
		t.Fatalf("%s beside a reader's short hold = %#v", name, diagnostics.Of(err))
	}
	if waited := time.Since(started); waited < rootHold {
		t.Fatalf("%s ran after %v, while the reader still held the root", name, waited)
	}
}

func TestASecretWriteAndAMediaPublicationWaitOutAReadersHold(t *testing.T) {
	store, record := lifecycleFixture(t)
	waitsOutAReader(t, store, "a secret write", func() error {
		return store.MutateSecrets(context.Background(), secretToken(record), func(secretstore.Area) error { return nil })
	})
	const data = "installer image"
	stage := fillStage(t, store, "demo.iso", data)
	waitsOutAReader(t, store, "a media publication", func() error { return publishStage(store, "demo.iso", data, stage, false) })
	if _, err := os.Stat(filepath.Join(store.options.Root, "media", "demo.iso")); err != nil {
		t.Fatalf("the publication that waited published nothing: %v", err)
	}
}
