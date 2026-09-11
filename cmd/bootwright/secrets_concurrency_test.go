//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type secretConfirmationFunc func(context.Context, string, string) error

func (f secretConfirmationFunc) Confirm(ctx context.Context, action, name string) error {
	return f(ctx, action, name)
}

func TestSecretConfirmationsHoldMutationLeaseAndReleaseItOnRefusal(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	addSecretInput(t, input, "secret.yaml", secretDocument("payload", "opaque", ""))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "encryption", "init")
	snapshot, err := repository.SecretContext(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	confirmations := 0
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace = repository, repository
	deps.Confirmer = secretConfirmationFunc(func(ctx context.Context, _, _ string) error {
		confirmations++
		entered := false
		err := repository.MutateSecrets(ctx, snapshot.Context, func(secretstore.Area) error { entered = true; return nil })
		if err == nil || entered {
			t.Fatal("confirmation did not hold the mutation lease")
		}
		return errors.New("synthetic confirmation refusal")
	})
	services = assembleServices(deps)
	value := addSecretInput(t, t.TempDir(), "value", "synthetic-lease-canary")
	contextRun(t, services, 0, "secret", "set", "--name", "payload", "--value-file", value)
	if confirmations != 0 {
		t.Fatal("initial set requested confirmation")
	}
	for _, args := range [][]string{
		{"secret", "set", "--name", "payload", "--value-file", value},
		{"secret", "delete", "--name", "payload"},
		{"secret", "encryption", "rotate"},
	} {
		before := stateFingerprint(t, root)
		contextRun(t, services, 1, args...)
		if !sameFingerprints(before, stateFingerprint(t, root)) {
			t.Fatal("refused confirmation changed state")
		}
		if err := repository.MutateSecrets(context.Background(), snapshot.Context, func(secretstore.Area) error { return nil }); err != nil {
			t.Fatal("refused confirmation retained a lease", err)
		}
	}
	if confirmations != 3 {
		t.Fatal("unexpected confirmation count", confirmations)
	}
}

func TestSecretExpectedSnapshotRefusesContextUpdateBeforeEffects(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	addSecretInput(t, input, "secret.yaml", secretDocument("payload", "opaque", ""))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	snapshot, err := repository.SecretContext(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	addSecretInput(t, input, "secret.yaml", secretDocument("payload", "token", ""))
	contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input, "--yes")
	before := stateFingerprint(t, root)
	callback := func(secretstore.Area) error { t.Fatal("stale snapshot reached secret effects"); return nil }
	if err := repository.ReadSecrets(context.Background(), snapshot.Context, callback); err == nil {
		t.Fatal("stale read token was accepted")
	}
	if err := repository.MutateSecrets(context.Background(), snapshot.Context, callback); err == nil {
		t.Fatal("stale mutation token was accepted")
	}
	if !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatal("stale secret snapshot changed state")
	}
}
