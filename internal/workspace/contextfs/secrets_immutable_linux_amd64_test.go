//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/localkeyring"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const immutableCrashRootEnvironment = "BOOTWRIGHT_TEST_IMMUTABLE_CRASH_ROOT"

var immutableIdentityPath = "identities/ver-00000000000000000000000000000000.json"

func TestSecretImmutableWritePublishesOnlyCompleteFinalName(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	token := secretToken(record)
	data := []byte(strings.Repeat("complete", 8192))
	var writes int
	store.fail = func(point string) error {
		if point == "write-file" {
			writes++
			if writes == 2 {
				return errors.New("injected mid-write interruption")
			}
		}
		return nil
	}
	err := store.MutateSecrets(context.Background(), token, func(area secretstore.Area) error {
		if err := area.EnsureDirectory(context.Background(), "identities"); err != nil {
			return err
		}
		return area.PublishExclusive(context.Background(), immutableIdentityPath, data)
	})
	store.fail = nil
	expectSecretFailureCode(t, err, "secret.store.conflict")
	directory := filepath.Join(store.options.Root, "contexts", record.Name, "secrets", "identities")
	if _, err := os.Stat(filepath.Join(directory, filepath.Base(immutableIdentityPath))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial immutable artifact became visible at its final name")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "pending-") {
		t.Fatalf("retained staging artifact: %#v %v", entries, err)
	}
}

func TestSecretCapacityUsesStableLimitCode(t *testing.T) {
	err := (&secretArea{}).capacity(context.Background(), 0, maxSecretBytes+1)
	expectSecretFailureCode(t, err, "secret.store.limit")
}

func TestReadOnlySecretScanToleratesCompletedPendingRename(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	token := secretToken(record)
	initializeImmutableTestStore(t, store, token)
	const pending = "pending-00000000000000000000000000000000"
	const final = "ver-00000000000000000000000000000000.json"
	if err := store.MutateSecrets(context.Background(), token, func(area secretstore.Area) error {
		return area.WriteExclusive(context.Background(), "identities/"+pending, []byte("staged"))
	}); err != nil {
		t.Fatal(err)
	}
	err := store.ReadSecrets(context.Background(), token, func(area secretstore.Area) error {
		concrete := area.(*secretArea)
		directory, err := openDirectory(concrete.secrets, "identities")
		if err != nil {
			return err
		}
		defer directory.file.Close()
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

func TestSecretImmutableWriteDoesNotReplaceExistingFinalName(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	token := secretToken(record)
	path := immutableIdentityPath
	var firstErr, secondErr error
	err := store.MutateSecrets(context.Background(), token, func(area secretstore.Area) error {
		if err := area.EnsureDirectory(context.Background(), "identities"); err != nil {
			return err
		}
		firstErr = area.PublishExclusive(context.Background(), path, []byte("first"))
		if firstErr != nil {
			return firstErr
		}
		secondErr = area.PublishExclusive(context.Background(), path, []byte("second"))
		return nil
	})
	if err != nil || firstErr != nil {
		t.Fatalf("first immutable publication: outer=%#v inner=%#v", err, firstErr)
	}
	if secondErr == nil {
		t.Fatal("second immutable publication replaced an existing name")
	}
	expectSecretFailureCode(t, secondErr, "secret.store.conflict")
	data, err := os.ReadFile(filepath.Join(store.options.Root, "contexts", record.Name, "secrets", path))
	if err != nil || string(data) != "first" {
		t.Fatalf("immutable final content: %q %v", data, err)
	}
}

func TestSecretReadSessionPreventsConcurrentMutation(t *testing.T) {
	store, sources := fixture(t)
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

func TestSecretImmutableWriteSurvivesSubprocessDeathMidWrite(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	token := secretToken(record)
	implementation := initializeImmutableTestStore(t, store, token)
	command := exec.Command(os.Args[0], "-test.run=^TestSecretImmutableWriteSubprocessHelper$", "-test.count=1")
	command.Env = append(os.Environ(), immutableCrashRootEnvironment+"="+store.options.Root)
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 86 {
		t.Fatalf("subprocess did not stop at the mid-write boundary: %v %s", err, output)
	}
	directory := filepath.Join(store.options.Root, "contexts", record.Name, "secrets", "identities")
	if _, err := os.Stat(filepath.Join(store.options.Root, "contexts", record.Name, "secrets", immutableIdentityPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("subprocess exposed a partial final immutable artifact")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "pending-") {
		t.Fatalf("subprocess staging evidence: %#v %v", entries, err)
	}
	err = store.ReadSecrets(context.Background(), token, func(area secretstore.Area) error {
		selector, exists, err := secretstore.ReadSelector(context.Background(), area, token.Name)
		if err != nil || !exists {
			return errors.New("initialized selector is unavailable after subprocess death")
		}
		session, err := implementation.Open(context.Background(), token, area, selector, nil)
		if err != nil {
			return err
		}
		defer session.Close()
		snapshot, err := session.Inspect(context.Background())
		if err != nil {
			return err
		}
		if snapshot.RetainedArtifacts == 0 || !snapshot.CleanupRequired {
			return errors.New("subprocess staging evidence was not reported as retained")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSecretImmutableWriteSubprocessHelper(t *testing.T) {
	root := os.Getenv(immutableCrashRootEnvironment)
	if root == "" {
		return
	}
	store := New(testOptions(root))
	snapshot, err := store.SecretContext(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	store.fail = func(point string) error {
		if point == "write-file" {
			writes++
			if writes == 2 {
				os.Exit(86)
			}
		}
		return nil
	}
	err = store.MutateSecrets(context.Background(), snapshot.Context, func(area secretstore.Area) error {
		return area.PublishExclusive(context.Background(), immutableIdentityPath, []byte(strings.Repeat("partial", 16384)))
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("immutable write unexpectedly completed")
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
