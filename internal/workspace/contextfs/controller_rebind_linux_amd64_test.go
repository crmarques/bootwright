//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func rebindHost(t *testing.T, machine string) controller.InstalledHostIdentity {
	t.Helper()
	host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat(machine, 32), "99999999-2222-4333-8444-555555555555", "99999999-3333-4444-8555-666666666666")
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func storedController(t *testing.T, store *Store) prerequisites.HostState {
	t.Helper()
	var state prerequisites.HostState
	if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		state = view.State
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return state
}

func rebindRefused(t *testing.T, err error, message string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.identity" || !strings.Contains(reported[0].Message, message) {
		t.Fatalf("refusal = %#v, want controller.identity %q", reported, message)
	}
}

// A rebind over a changed identity rewrites the recorded host once, keeps
// every binding, now proved against the new host, and every reservation, and
// a context bound before it runs on the new host and no longer on the old.
// Repeated, it changes nothing.
func TestARebindKeepsEveryBindingAndReservation(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	if err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		return tx.Reserve(ctx, []prerequisites.HostReservation{{Context: record.Name, Kind: "listener", Service: "lab-dns", Keys: []string{"udp/53"}}})
	}); err != nil {
		t.Fatalf("the reservation failed: %#v", diagnostics.Of(err))
	}
	before, old, next := storedController(t, store), syntheticHost(t), rebindHost(t, "9")
	result, err := store.RebindController(ctx, old, next)
	if err != nil || result != (prerequisites.RebindResult{Outcome: "rebound", Bindings: 1, Reservations: 1}) {
		t.Fatalf("the rebind = %+v (%#v)", result, diagnostics.Of(err))
	}
	after := storedController(t, store)
	digest, err := next.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	if !after.Host.Equal(next) || len(after.Bindings) != 1 || after.Bindings[0].Context != before.Bindings[0].Context ||
		after.Bindings[0].Machine != before.Bindings[0].Machine || after.Bindings[0].Tools != before.Bindings[0].Tools || after.Bindings[0].HostDigest != digest ||
		!slices.EqualFunc(after.Reservations, before.Reservations, func(a, b prerequisites.HostReservation) bool {
			return a.Context == b.Context && a.Kind == b.Kind && a.Service == b.Service && slices.Equal(a.Keys, b.Keys)
		}) {
		t.Fatalf("the rebind left %+v, from %+v", after, before)
	}
	bind := func(host controller.InstalledHostIdentity) error {
		return store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error { return tx.Bind(ctx, "controller", host) })
	}
	if err := bind(next); err != nil {
		t.Fatalf("the rebound context does not run on the new host: %#v", diagnostics.Of(err))
	}
	rebindRefused(t, bind(old), "this host is not the host this controller state belongs to")
	_, err = store.RebindController(ctx, old, next)
	rebindRefused(t, err, "already names this host; nothing changed")
}

// A store whose controller directory is not the one its registry attributed,
// as a file-by-file copy's is not, refuses; a byte-for-byte clone keeps those
// directories and passes, which the operator's affirmation covers. A record
// that changed since the confirmation read it is not the one confirmed: both
// refusals leave the record as it was.
func TestARebindRefusesAnotherStoreAndAChangedRecord(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	before := storedController(t, store)
	_, err := store.RebindController(ctx, rebindHost(t, "8"), rebindHost(t, "9"))
	rebindRefused(t, err, "changed while its rebind was being confirmed")
	controllerDir := filepath.Join(store.options.Root, "controller")
	if err := os.Rename(controllerDir, controllerDir+".kept"); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("cp", "-a", controllerDir+".kept", controllerDir).Run(); err != nil {
		t.Fatal(err)
	}
	_, err = store.RebindController(ctx, syntheticHost(t), rebindHost(t, "9"))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "context.state" || !strings.Contains(reported[0].Message, "controller directory was replaced") {
		t.Fatalf("a rebind over a copied controller directory reported %#v", reported)
	}
	if err := os.RemoveAll(controllerDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(controllerDir+".kept", controllerDir); err != nil {
		t.Fatal(err)
	}
	if after := storedController(t, store); !after.Host.Equal(before.Host) || after.Bindings[0] != before.Bindings[0] {
		t.Fatalf("a refused rebind changed the record to %+v", after)
	}
}

// A rebind stopped before its record is renamed into place changes nothing,
// and its retry rebinds; one stopped after it reports an uncertain outcome
// whose retry finds the record already naming this host.
func TestAnInterruptedRebindIsRetried(t *testing.T) {
	ctx := context.Background()
	for _, point := range []checkpoint{checkpointBeforeControllerRename, checkpointAfterControllerRename} {
		t.Run(string(point), func(t *testing.T) {
			store, record := lifecycleFixture(t)
			sealedBundleFixture(t, store, record)
			old, next := syntheticHost(t), rebindHost(t, "9")
			store.fail = func(at string) error {
				if at == string(point) {
					return errors.New("interrupted")
				}
				return nil
			}
			if _, err := store.RebindController(ctx, old, next); err == nil {
				t.Fatal("an interrupted rebind reported success")
			}
			store.fail = nil
			if point == checkpointBeforeControllerRename {
				if host := storedController(t, store).Host; !host.Equal(old) {
					t.Fatal("a rebind stopped before its rename changed the record")
				}
				if _, err := store.RebindController(ctx, old, next); err != nil {
					t.Fatalf("the retry failed: %#v", diagnostics.Of(err))
				}
			} else {
				_, err := store.RebindController(ctx, old, next)
				rebindRefused(t, err, "already names this host; nothing changed")
			}
			if host := storedController(t, store).Host; !host.Equal(next) {
				t.Fatal("the retried rebind left the old host recorded")
			}
		})
	}
}
