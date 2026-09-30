//go:build linux && amd64

package contextfs

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A context deleted with the objects it owns abandoned still holds the host
// keys its operation reserved, and no context is left to release them. The
// deletion releases exactly those, beside the rest of what the controller
// record holds for it, so another context can then reserve the same keys,
// and every other context's reservations stay.
func TestADeletedContextsReservationsAreReleasedForAnotherContext(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	_, sources := fixture(t)
	publish(t, store, "second", sources)
	publish(t, store, "third", sources)
	reserveFixture(t, store, record)
	machine := []string{"libvirt-domain:lab-rhel-01", "path:/var/lib/bootwright/machines/rhel-01", "socket:192.0.2.1:8000", "unit:bootwright-bmc-lab-rhel-01"}
	reserve := func(name string, keys []string) error {
		return store.MutateLifecycle(ctx, name, func(tx lifecycle.Transaction) error {
			return tx.Reserve(ctx, []prerequisites.HostReservation{{Context: name, Kind: "substrate-machine", Service: "rhel-01", Keys: keys}})
		})
	}
	if err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		if err := tx.Reserve(ctx, []prerequisites.HostReservation{{Context: record.Name, Kind: "substrate-machine", Service: "rhel-01", Keys: machine}}); err != nil {
			return err
		}
		return tx.PublishEvidence(ctx, []byte(`{"version":1,"operation":"unknown","ownership":"retained"}`))
	}); err != nil {
		t.Fatalf("the abandoned context's reservation failed: %#v", diagnostics.Of(err))
	}
	if err := reserve("third", []string{"unit:bootwright-proxy"}); err != nil {
		t.Fatalf("an unrelated reservation failed: %#v", diagnostics.Of(err))
	}
	err := reserve("second", machine)
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, record.Name) {
		t.Fatalf("a held key was reserved again: %#v", reported)
	}
	var released []string
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(ctx, record.Name); err != nil {
			return err
		}
		keys, err := tx.HostReservations(ctx, record.Name)
		if err != nil {
			return err
		}
		released = keys
		return tx.Delete(ctx, record)
	}); err != nil {
		t.Fatalf("deleting the context failed: %#v", diagnostics.Of(err))
	}
	if !slices.Equal(released, machine) {
		t.Fatalf("the deletion reported %v, want %v", released, machine)
	}
	if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
		for _, reservation := range view.State.Reservations {
			if reservation.Context == record.Name {
				t.Fatalf("the deleted context still reserves %v", reservation.Keys)
			}
		}
		if len(view.State.Reservations) != 1 || view.State.Reservations[0].Context != "third" {
			t.Fatalf("another context's reservation did not survive: %+v", view.State.Reservations)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := reserve("second", machine); err != nil {
		t.Fatalf("another context could not reserve the released keys: %#v", diagnostics.Of(err))
	}
}
