//go:build linux && amd64

package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
)

// keyringState is what a context's keyring publishes: its identity
// reservations, which it keeps forever against reuse, and its bindings.
type keyringState struct {
	identities int
	bindings   []string
}

type bindingListing interface {
	Bindings(context.Context, custody.BindingsRequest) ([]string, error)
}

func keyringOf(t *testing.T, repository *contextfs.Store, services cli.Services, name string) keyringState {
	t.Helper()
	ctx := context.Background()
	snapshot, err := repository.SecretContext(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	var state keyringState
	if err := repository.ReadSecrets(ctx, snapshot.Context, func(area secretstore.Area) error {
		entries, err := area.Entries(ctx, "identities")
		state.identities = len(entries)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	listing, ok := services.Secrets.(bindingListing)
	if !ok {
		t.Fatal("binding custody is not composed")
	}
	if state.bindings, err = listing.Bindings(ctx, custody.BindingsRequest{ContextName: name}); err != nil {
		t.Fatal(err)
	}
	return state
}

// A bounded consumer of one context reads its material while a bounded run of
// another holds the context store's shared lock, over the real context store
// and local keyring: reading binds nothing, so it needs no exclusive lock. Any
// number of reads leaves that keyring's identity reservations and bindings as
// they were.
func TestBoundedConsumersOfTwoContextsReadTogetherAndLeaveTheKeyringAsTheyFoundIt(t *testing.T) {
	services, repository, input, _ := contextFixture(t)
	addSecretInput(t, input, "secret.yaml", secretDocument("opaque", "opaque", ""))
	for _, name := range []string{"alpha", "beta"} {
		contextRun(t, services, 0, "context", "init", "--name", name, "--input-dir", input)
		contextRun(t, services, 0, "secret", "encryption", "init", "--context", name)
		contextRun(t, services, 0, "secret", "set", "--context", name, "--name", "opaque",
			"--value-file", addSecretInput(t, t.TempDir(), "value", "synthetic-"+name+"-canary"))
	}
	reconciler, ok := services.Lifecycle.(lifecycle.Service)
	if !ok {
		t.Fatal("the lifecycle is not composed")
	}
	before := keyringOf(t, repository, services, "beta")
	if before.identities == 0 {
		t.Fatal("the keyring holds no identity reservation to compare")
	}
	ctx := context.Background()
	for attempt := 1; attempt <= 3; attempt++ {
		read := false
		err := repository.RunLifecycle(ctx, "alpha", func(lifecycle.RunView) error {
			return reconciler.WithMaterial(ctx, lifecycle.MaterialRequest{ContextName: "beta", Secrets: []string{"opaque"}},
				func(_ context.Context, material map[string]secrets.Material) error {
					value, _ := material["opaque"].Part(secrets.ValuePart)
					defer clear(value)
					read = string(value) == "synthetic-beta-canary"
					return nil
				})
		})
		if err != nil || !read {
			t.Fatalf("read %d of beta beside a bounded run of alpha = %v, read its own material %t", attempt, err, read)
		}
	}
	if after := keyringOf(t, repository, services, "beta"); !reflect.DeepEqual(after, before) {
		t.Fatalf("three bounded reads moved beta's keyring from %+v to %+v", before, after)
	}
}
