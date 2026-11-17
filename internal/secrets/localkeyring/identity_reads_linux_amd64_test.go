//go:build linux && amd64

package localkeyring

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type identityReadArea struct {
	secretstore.Area
	reads int
}

func (a *identityReadArea) Read(ctx context.Context, path string, maximum int) ([]byte, bool, error) {
	if strings.HasPrefix(path, "identities/") {
		a.reads++
	}
	return a.Area.Read(ctx, path, maximum)
}

// A read-only session lists the identity reservations, so a missing one
// still refuses, but reads and decodes them only when it publishes: opening
// and inspecting the store reads none, and a publication reads each.
func TestIdentityReservationsAreReadOnlyWhenPublishing(t *testing.T) {
	h := newIntegrationStore(t)
	d := declaration("credential", "opaque", "contextStore")
	material := opaque("synthetic-value")
	defer material.Clear()
	for range 3 {
		if err := h.mutate(func(session secretstore.StoreSession) error {
			_, err := session.PutBatch(context.Background(), []secretstore.Put{{Declaration: d, Material: material}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	counted := &identityReadArea{}
	if err := h.workspace.ReadSecrets(context.Background(), h.context, func(area secretstore.Area) error {
		counted.Area = area
		session, err := h.implementation.Open(context.Background(), h.context, counted, readTestSelector(t, area), nil)
		if err != nil {
			return err
		}
		defer session.Close()
		_, err = session.Inspect(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if counted.reads != 0 {
		t.Fatalf("opening and inspecting the store read %d identity reservations, want none", counted.reads)
	}
	publishing := &identityReadArea{}
	if err := h.workspace.MutateSecrets(context.Background(), h.context, func(area secretstore.Area) error {
		publishing.Area = area
		session, err := h.implementation.Open(context.Background(), h.context, publishing, readTestSelector(t, area), nil)
		if err != nil {
			return err
		}
		defer session.Close()
		_, err = session.PutBatch(context.Background(), []secretstore.Put{{Declaration: d, Material: material}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if publishing.reads == 0 {
		t.Fatal("a publication read no identity reservation")
	}
}

// The cheap open does not read a reservation's bytes, so the inspection
// commands an operator is sent to verify them: a reservation whose content
// names another context still opens and inspects, and Verify refuses it.
func TestVerifyRefusesAForeignIdentityReservation(t *testing.T) {
	h := newIntegrationStore(t)
	d := declaration("credential", "opaque", "contextStore")
	material := opaque("synthetic-value")
	defer material.Clear()
	if err := h.mutate(func(session secretstore.StoreSession) error {
		_, err := session.PutBatch(context.Background(), []secretstore.Put{{Declaration: d, Material: material}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(h.root, "contexts", h.context.Name, "secrets", "identities")
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("identity reservations: %v %d", err, len(entries))
	}
	target := filepath.Join(directory, entries[0].Name())
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	foreign := strings.Replace(string(data), `"context":"`+h.context.Name+`"`, `"context":"other"`, 1)
	if foreign == string(data) {
		t.Fatalf("the reservation does not name its context: %s", data)
	}
	if err := os.WriteFile(target, []byte(foreign), 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.view(func(session secretstore.StoreSession) error {
		if _, err := session.Inspect(context.Background()); err != nil {
			t.Fatalf("inspecting a store with a foreign reservation = %v", err)
		}
		if err := session.Verify(context.Background()); failureCode(err) != "secret.store.corrupt" {
			t.Fatalf("verifying a store with a foreign reservation = %v, want secret.store.corrupt", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
