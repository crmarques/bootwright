//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A setup receipt that lists one proxy bypass twice is refused, while the same
// list without the repeat is accepted, so the record holds each bypass once
// (X33's mutants, B255).
func TestAControllerReceiptRefusesARepeatedProxyBypass(t *testing.T) {
	value := syntheticControllerState(t, prerequisites.SetupContext{})
	value.Receipt.Egress = prerequisites.SetupEgress{HTTPSProxy: "https://proxy.example.test:3128", NoProxy: []string{"example.test", "192.0.2.0/24"}}
	value.Receipt.PlanDigest, _ = prerequisites.SetupPlanDigest(value.Host, value.Receipt)
	if err := validateControllerState(value); err != nil {
		t.Fatalf("distinct bypasses were refused: %v", err)
	}
	repeated := cloneControllerState(value)
	repeated.Receipt.Egress.NoProxy = []string{"example.test", "example.test"}
	repeated.Receipt.PlanDigest, _ = prerequisites.SetupPlanDigest(repeated.Host, repeated.Receipt)
	if err := validateControllerState(repeated); err == nil {
		t.Fatal("a bypass listed twice was accepted")
	}
}

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
