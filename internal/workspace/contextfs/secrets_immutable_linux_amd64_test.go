//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/workspace/contextfs/kernel"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/localkeyring"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func TestSecretCapacityUsesStableLimitCode(t *testing.T) {
	t.Parallel()
	err := (&secretArea{}).capacity(context.Background(), 0, maxSecretBytes+1)
	expectSecretFailureCode(t, err, "secret.store.limit")
}

func TestReadOnlySecretScanToleratesCompletedPendingRename(t *testing.T) {
	t.Parallel()
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	token := secretToken(record)
	initializeImmutableTestStore(t, store, token)
	const pending = "pending-00000000000000000000000000000000"
	const final = "blob-00000000000000000000000000000000.enc"
	if err := store.MutateSecrets(context.Background(), token, func(area secretstore.Area) error {
		return area.WriteExclusive(context.Background(), "parts/"+pending, []byte("staged"))
	}); err != nil {
		t.Fatal(err)
	}
	err := store.ReadSecrets(context.Background(), token, func(area secretstore.Area) error {
		concrete := area.(*secretArea)
		directory, err := kernel.OpenDirectory(concrete.secrets, "parts")
		if err != nil {
			return err
		}
		defer directory.File.Close()
		names, err := secretDirectoryNames(context.Background(), directory, maxSecretEntries)
		if err != nil {
			return err
		}
		if len(names) != 1 || names[0] != pending {
			return errors.New("staging name was not enumerated")
		}
		if err := renameNoReplaceAt(directory, pending, final); err != nil {
			return err
		}
		if _, strictErr := inspectSecretDirectoryNames(context.Background(), directory, names, false, false); strictErr == nil {
			return errors.New("mutating scan tolerated a vanished staging name")
		}
		entries, err := inspectSecretDirectoryNames(context.Background(), directory, names, false, true)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return errors.New("vanished staging name remained visible")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSecretReadSessionPreventsConcurrentMutation(t *testing.T) {
	t.Parallel()
	store, sources := fixture(t)
	impatient(store)
	record := publish(t, store, "example", sources)
	token := secretToken(record)
	implementation := initializeImmutableTestStore(t, store, token)
	err := store.ReadSecrets(context.Background(), token, func(area secretstore.Area) error {
		selector, exists, err := secretstore.ReadSelector(context.Background(), area, token.Name)
		if err != nil || !exists {
			return errors.New("initialized selector is unavailable")
		}
		session, err := implementation.Open(context.Background(), token, area, selector, nil)
		if err != nil {
			return err
		}
		defer session.Close()
		writer := make(chan error, 1)
		var entered atomic.Bool
		go func() {
			writer <- store.MutateSecrets(context.Background(), token, func(secretstore.Area) error {
				entered.Store(true)
				return nil
			})
		}()
		select {
		case err := <-writer:
			if err == nil || entered.Load() {
				return errors.New("writer entered a shared read lock")
			}
		case <-time.After(5 * time.Second):
			return errors.New("writer contention did not refuse promptly")
		}
		_, err = session.Inspect(context.Background())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func initializeImmutableTestStore(t *testing.T, store *Store, token secretstore.Context) *localkeyring.Implementation {
	t.Helper()
	implementation := localkeyring.New()
	err := store.MutateSecrets(context.Background(), token, func(area secretstore.Area) error {
		session, err := implementation.Initialize(context.Background(), token, area, nil)
		if session != nil {
			defer session.Close()
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return implementation
}

func expectSecretFailureCode(t *testing.T, err error, want string) {
	t.Helper()
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != want {
		t.Fatalf("failure diagnostics = %+v, want code %q", diagnostics, want)
	}
}
