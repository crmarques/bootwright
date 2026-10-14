//go:build linux && amd64

package localkeyring

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// A Workspace area takes one store record publication per callback, the rule
// the lifecycle tests' lent area holds custody doubles to: a release through a
// second session of the same callback, as custody opens one per call, is
// refused with the host's diagnostic after the first is already durable, and
// releases in separate callbacks each publish.
func TestOneCallbackTakesOneStoreRecordPublication(t *testing.T) {
	ctx := context.Background()
	h := newIntegrationStore(t)
	credential := declaration("credential", "opaque", "contextStore")
	material := opaque("bound")
	defer material.Clear()
	var version secretstore.Version
	if err := h.mutate(func(session secretstore.StoreSession) error {
		versions, err := session.PutBatch(ctx, []secretstore.Put{{Declaration: credential, Material: material}})
		if err == nil {
			version = versions[0]
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	bind := func() string {
		var id string
		if err := h.mutate(func(session secretstore.StoreSession) error {
			binding, err := session.Bind(ctx, []secretstore.BoundInput{{Declaration: credential, Version: version.ID, Material: material}})
			id = binding.ID
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first, second := bind(), bind()
	var refused error
	if err := h.workspace.MutateSecrets(ctx, h.context, func(area secretstore.Area) error {
		release := func(id string) error {
			session, err := h.implementation.Open(ctx, h.context, area, readTestSelector(t, area), nil)
			if err != nil {
				return err
			}
			defer session.Close()
			_, err = session.Release(ctx, id)
			return err
		}
		if err := release(first); err != nil {
			return err
		}
		refused = release(second)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if reported := diagnostics.Of(refused); len(reported) != 1 || reported[0].Code != "secret.store.conflict" || reported[0].Message != "secret storage callback has already finished" {
		t.Fatalf("a second publication in one callback was not refused as finished: %v %#v", refused, reported)
	}
	held := func() []string {
		var ids []string
		if err := h.view(func(session secretstore.StoreSession) error {
			snapshot, err := session.Inspect(ctx)
			for _, binding := range snapshot.Bindings {
				ids = append(ids, binding.ID)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	if ids := held(); len(ids) != 1 || ids[0] != second {
		t.Fatalf("the first release is not durable beside the refused second: %v", ids)
	}
	if err := h.mutate(func(session secretstore.StoreSession) error {
		_, err := session.Release(ctx, second)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if ids := held(); len(ids) != 0 {
		t.Fatalf("a release in its own callback did not publish: %v", ids)
	}
}
