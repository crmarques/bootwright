//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A fresh reservation never adopts a context directory it did not create: one
// already present under the name the registry has just reserved refuses, and
// nothing is written into it (X33's mutants, B255).
func TestAFreshReservationNeverAdoptsAnExistingDirectory(t *testing.T) {
	store, _ := fixture(t)
	ctx := context.Background()
	if err := store.Transact(ctx, true, nil, func(contexts.Transaction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	// The planted directory carries a reservation that verifies, so only the
	// freshness of the registry entry tells it from this reservation's own.
	foreign := filepath.Join(store.options.Root, "contexts", "pending")
	if err := os.MkdirAll(filepath.Join(foreign, "state"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := encodeRecord(reservation{Version: ReservationVersion, Name: "pending"}, maxRecord)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "state", "reservation.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	err = store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		_, err := tx.Reserve(ctx, "pending", "", contexts.DefaultConfiguration("pending").Canonical())
		return err
	})
	expectState(t, err)
	if _, statErr := os.Stat(filepath.Join(foreign, "context.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("the reservation wrote into a directory it did not create (%v)", statErr)
	}
}
