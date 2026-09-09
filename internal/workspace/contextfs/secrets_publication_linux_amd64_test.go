//go:build linux && amd64

package contextfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/localstore"
	"github.com/crmarques/bootwright/internal/secrets/storage"
)

func secretPublicationFixture(t *testing.T) (*Store, storage.Context) {
	t.Helper()
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	token := storage.Context{Name: record.Name, ID: record.ID, Revision: record.Revision, Mode: string(record.Mode)}
	access := storage.NewAccess(store, storage.NewCatalog(localstore.New()), nil)
	if err := access.Initialize(context.Background(), token, "local-keyring", func(storage.StoreSession, storage.Selection, bool) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := publishTestSecret(store, token, "original-canary"); err != nil {
		t.Fatal(err)
	}
	return store, token
}

func publishTestSecret(store *Store, token storage.Context, value string) error {
	declaration := secrets.Declaration{Name: "payload", Type: "opaque", Source: "contextStore"}
	encoded, err := json.Marshal(declaration)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	declaration.Fingerprint = hex.EncodeToString(digest[:])
	material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})
	defer material.Clear()
	access := storage.NewAccess(store, storage.NewCatalog(localstore.New()), nil)
	return access.Mutate(context.Background(), token, func(session storage.StoreSession, _ storage.Selection) error {
		_, err := session.PutBatch(context.Background(), []storage.Put{{Declaration: declaration, Material: material}})
		return err
	})
}

func readTestSecret(t *testing.T, store *Store, token storage.Context) string {
	t.Helper()
	access := storage.NewAccess(store, storage.NewCatalog(localstore.New()), nil)
	result := ""
	err := access.View(context.Background(), token, true, func(session storage.StoreSession, _ storage.Selection) error {
		snapshot, err := session.Inspect(context.Background())
		if err != nil {
			return err
		}
		if len(snapshot.Current) != 1 || snapshot.Current[0].Name != "payload" {
			return errors.New("incomplete active secret mapping")
		}
		material, err := session.Read(context.Background(), snapshot.Current[0].Version)
		if err != nil {
			return err
		}
		defer material.Clear()
		value, exists := material.Part(secrets.ValuePart)
		if !exists {
			return errors.New("incomplete secret material")
		}
		result = string(value)
		clear(value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSecretEveryPublicationEffectIsAtomic(t *testing.T) {
	baseline, token := secretPublicationFixture(t)
	points := []string{}
	baseline.fail = func(point string) error { points = append(points, point); return nil }
	if err := publishTestSecret(baseline, token, "replacement-canary"); err != nil {
		t.Fatal(err)
	}
	baseline.fail = nil
	if len(points) == 0 {
		t.Fatal("publication has no failure-injection checkpoints")
	}
	for index, point := range points {
		t.Run(fmt.Sprintf("%03d-%s", index, point), func(t *testing.T) {
			store, token := secretPublicationFixture(t)
			visited := 0
			fired := false
			store.fail = func(string) error {
				defer func() { visited++ }()
				if visited == index {
					fired = true
					return errors.New("injected secret publication failure")
				}
				return nil
			}
			err := publishTestSecret(store, token, "replacement-canary")
			store.fail = nil
			if !fired || err == nil {
				t.Fatal("publication failure was not observed")
			}
			value := readTestSecret(t, store, token)
			if value != "original-canary" && value != "replacement-canary" {
				t.Fatal("interruption exposed partial material")
			}
			// Inspect before retry: a visible committed replacement is not replayed.
			if value == "original-canary" {
				if err := publishTestSecret(store, token, "replacement-canary"); err != nil {
					t.Fatal("verified retry failed", err)
				}
			}
			if readTestSecret(t, store, token) != "replacement-canary" {
				t.Fatal("retry did not reach the intended complete state")
			}
		})
	}
}

func TestSecretEveryRotationEffectPreservesCompleteMaterial(t *testing.T) {
	rotate := func(store *Store, token storage.Context) error {
		access := storage.NewAccess(store, storage.NewCatalog(localstore.New()), nil)
		return access.Mutate(context.Background(), token, func(session storage.StoreSession, _ storage.Selection) error {
			_, err := session.Rotate(context.Background())
			return err
		})
	}
	key := func(t *testing.T, store *Store, token storage.Context) string {
		t.Helper()
		access := storage.NewAccess(store, storage.NewCatalog(localstore.New()), nil)
		active := ""
		err := access.View(context.Background(), token, false, func(session storage.StoreSession, _ storage.Selection) error {
			snapshot, err := session.Inspect(context.Background())
			active = snapshot.ActiveKey
			return err
		})
		if err != nil || active == "" {
			t.Fatal("rotation left no authenticated active key", err)
		}
		return active
	}
	baseline, token := secretPublicationFixture(t)
	points := []string{}
	baseline.fail = func(point string) error { points = append(points, point); return nil }
	if err := rotate(baseline, token); err != nil {
		t.Fatal(err)
	}
	baseline.fail = nil
	if len(points) == 0 {
		t.Fatal("rotation has no effect checkpoints")
	}
	for index, point := range points {
		t.Run(fmt.Sprintf("%03d-%s", index, point), func(t *testing.T) {
			store, token := secretPublicationFixture(t)
			originalKey := key(t, store, token)
			visited, fired := 0, false
			store.fail = func(string) error {
				defer func() { visited++ }()
				if visited == index {
					fired = true
					return errors.New("injected rotation failure")
				}
				return nil
			}
			err := rotate(store, token)
			store.fail = nil
			if !fired || err == nil {
				t.Fatal("rotation failure was not observed")
			}
			if readTestSecret(t, store, token) != "original-canary" {
				t.Fatal("failed rotation changed logical material")
			}
			if key(t, store, token) == originalKey {
				if err := rotate(store, token); err != nil {
					t.Fatal("verified rotation retry failed", err)
				}
			}
			if key(t, store, token) == originalKey || readTestSecret(t, store, token) != "original-canary" {
				t.Fatal("rotation retry did not produce complete reencrypted material")
			}
		})
	}
}

func TestSecretPublicationProcessDeathReleasesLeases(t *testing.T) {
	for _, point := range []string{"before-secret-rename", "after-secret-rename"} {
		t.Run(point, func(t *testing.T) {
			store, token := secretPublicationFixture(t)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestSecretPublicationCrashChild$")
			command.Env = append(os.Environ(), "BOOTWRIGHT_SECRET_TEST_ROOT="+store.options.Root, "BOOTWRIGHT_SECRET_TEST_POINT="+point)
			output, err := command.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 77 {
				t.Fatalf("secret crash helper: %v %s", err, output)
			}
			want := "original-canary"
			if point == "after-secret-rename" {
				want = "replacement-canary"
			}
			if readTestSecret(t, store, token) != want {
				t.Fatal("process death crossed the selector visibility boundary")
			}
			if err := publishTestSecret(store, token, "final-canary"); err != nil {
				t.Fatal("process death retained a mutation lease", err)
			}
		})
	}
}

func TestSecretPublicationCrashChild(t *testing.T) {
	root := os.Getenv("BOOTWRIGHT_SECRET_TEST_ROOT")
	if root == "" {
		return
	}
	store := New(testOptions(root))
	snapshot, err := store.SecretContext(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	point := os.Getenv("BOOTWRIGHT_SECRET_TEST_POINT")
	renames := 0
	store.fail = func(name string) error {
		if name == point {
			renames++
			// A Put advances its seal ledger before publishing its selector.
			if renames == 2 {
				os.Exit(77)
			}
		}
		return nil
	}
	if err := publishTestSecret(store, snapshot.Context, "replacement-canary"); err != nil {
		t.Fatal(err)
	}
	t.Fatal("secret crash checkpoint was not reached")
}
