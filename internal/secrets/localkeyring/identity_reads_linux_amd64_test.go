//go:build linux && amd64

package localkeyring

import (
	"context"
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
