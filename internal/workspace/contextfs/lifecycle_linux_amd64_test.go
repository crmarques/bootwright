//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/secrets/localstore"
	"github.com/crmarques/bootwright/internal/secrets/storage"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func deleteContext(t *testing.T, store *Store, record contexts.Record) error {
	t.Helper()
	return store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		if record.Mode == contexts.Ready {
			if _, err := tx.MutationState(context.Background(), record.ID); err != nil {
				return err
			}
		}
		return tx.Delete(context.Background(), record)
	})
}

func TestExplicitRootUsesRootOwnershipByDefault(t *testing.T) {
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		t.Skip("requires namespace root with mapped user and group IDs")
	}
	for _, owner := range []Ownership{{UID: 1}, {GID: 1}} {
		t.Run(fmt.Sprintf("uid-%d-gid-%d", owner.UID, owner.GID), func(t *testing.T) {
			fixtureStore, sources := fixture(t)
			store := New(Options{Root: fixtureStore.options.Root})
			record := publish(t, store, "example", sources)
			err := filepath.WalkDir(store.options.Root, func(path string, _ fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				info, err := os.Lstat(path)
				if err != nil {
					return err
				}
				stat, ok := info.Sys().(*syscall.Stat_t)
				if !ok || stat.Uid != 0 || stat.Gid != 0 {
					return errors.New("default storage ownership is not root:root")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			reservation := filepath.Join(store.options.Root, "contexts", record.Name, "state", "reservation.json")
			if err := os.Chown(reservation, int(owner.UID), int(owner.GID)); err != nil {
				t.Fatal(err)
			}
			_, err = store.ReadInputs(context.Background(), record.Name, record.ID)
			expectState(t, err)
		})
	}
}

func TestDirectNamedLayoutAndPermanentDeletion(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	base := filepath.Join(store.options.Root, "contexts", "example")
	for _, relative := range []string{"context.yaml", "state/reservation.json", "state/mutation.json", "desired-state/revisions/" + record.Revision + "/manifest.json", "secrets"} {
		info, err := os.Stat(filepath.Join(base, relative))
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("wrong permissions for %s", relative)
		}
	}
	if err := deleteContext(t, store, record); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted context tree remains")
	}
	registry, err := store.View(context.Background())
	if err != nil || registry.Version != 3 || len(registry.Contexts) != 0 || len(registry.Identities) != 0 || registry.NextIdentity != 2 {
		t.Fatalf("deletion allocation state: %#v %v", registry, err)
	}
	next := publish(t, store, "example", sources)
	if next.ID == record.ID {
		t.Fatal("name reuse reused identity")
	}
	if _, err := store.ReadInputs(context.Background(), "example", record.ID); err == nil {
		t.Fatal("stale selected identity was retargeted")
	}
	for _, name := range []string{"archives", "staging"} {
		if _, err := os.Stat(filepath.Join(store.options.Root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected %s directory", name)
		}
	}
}

func TestInterruptedDeletionResumesOnlyRecordedIdentity(t *testing.T) {
	for _, point := range []string{"before-context-unlink", "before-context-rmdir"} {
		t.Run(point, func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			failed := false
			store.fail = func(name string) error {
				if name == point && !failed {
					failed = true
					return errors.New("synthetic interruption")
				}
				return nil
			}
			expectState(t, deleteContext(t, store, record))
			store.fail = nil
			registry, err := store.View(context.Background())
			if err != nil || len(registry.Contexts) != 1 || registry.Contexts[0].Mode != contexts.Deleting {
				t.Fatalf("pending deletion: %#v %v", registry, err)
			}
			if _, err := store.ReadInputs(context.Background(), "example", record.ID); err == nil {
				t.Fatal("deleting context remained usable")
			}
			if err := deleteContext(t, store, registry.Contexts[0]); err != nil {
				t.Fatal(err)
			}
			next := publish(t, store, "example", sources)
			if next.ID == record.ID {
				t.Fatal("deleted identity was reused")
			}
		})
	}
}

func TestRetiredIdentityIsNotReusedWithRepeatedRandomBytes(t *testing.T) {
	store, sources := fixture(t)
	store.random = constantRandom(0)
	record := publish(t, store, "example", sources)
	if err := deleteContext(t, store, record); err != nil {
		t.Fatal(err)
	}
	config := contexts.Configuration{Name: "example", SecretStore: contexts.SecretStoreConfiguration{Type: "local-keyring"}}.Canonical()
	err := store.Transact(context.Background(), true, nil, func(tx contexts.Transaction) error {
		_, err := tx.Reserve(context.Background(), "example", "", config)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.View(context.Background())
	if err != nil || len(registry.Contexts) != 1 || len(registry.Identities) != 0 || registry.NextIdentity != 3 || registry.Contexts[0].ID == record.ID {
		t.Fatalf("retired identity was reused: %#v %v", registry, err)
	}
	if _, err := os.Stat(filepath.Join(store.options.Root, "contexts", "example")); err != nil {
		t.Fatal("fresh identity did not create its context directory")
	}
}

func TestDeletionRefusesUnknownObjectsBeforeRemovingAnything(t *testing.T) {
	for _, kind := range []string{"unknown", "link", "hardlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			base := filepath.Join(store.options.Root, "contexts", record.Name)
			target := filepath.Join(base, "secrets", "unexpected")
			switch kind {
			case "unknown":
				if err := os.Mkdir(filepath.Join(base, "unknown"), 0700); err != nil {
					t.Fatal(err)
				}
			case "link":
				if err := os.Symlink(filepath.Join(base, "context.yaml"), target); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(filepath.Join(base, "context.yaml"), target); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(target, 0600); err != nil {
					t.Fatal(err)
				}
			}
			expectState(t, deleteContext(t, store, record))
			if _, err := os.Stat(filepath.Join(base, "state", "reservation.json")); err != nil {
				t.Fatal("refused deletion removed context identity")
			}
		})
	}
}

func TestPendingInitializationDoesNotPoisonUnrelatedContexts(t *testing.T) {
	store, sources := fixture(t)
	ready := publish(t, store, "ready", sources)
	config := contexts.DefaultConfiguration("pending").Canonical()
	var pending contexts.Record
	err := store.Transact(context.Background(), true, nil, func(tx contexts.Transaction) error {
		var err error
		pending, err = tx.Reserve(context.Background(), "pending", "", config)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.View(context.Background())
	if err != nil || len(registry.Contexts) != 2 {
		t.Fatalf("pending list: %v", err)
	}
	if _, err := store.ReadInputs(context.Background(), "ready", ready.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadInputs(context.Background(), "pending", pending.ID); err == nil {
		t.Fatal("initializing context was usable")
	}
	updated := publish(t, store, "ready", sources)
	if updated.ID != ready.ID {
		t.Fatal("pending context disturbed another identity")
	}
	err = store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		resumed, err := tx.Reserve(context.Background(), "pending", "", config)
		if err != nil {
			return err
		}
		if resumed.ID != pending.ID {
			t.Fatal("retry changed identity")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(store.options.Root, "contexts", "pending")
	if err := os.Rename(base, base+"-replaced"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	expectState(t, store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		_, err := tx.Reserve(context.Background(), "pending", "", config)
		return err
	}))
}

func TestInitializationInterruptionReservesExactIdentity(t *testing.T) {
	for _, point := range []string{"after-context-reservation", "after-context-directory", "before-context-ready"} {
		t.Run(point, func(t *testing.T) {
			store, sources := fixture(t)
			ready := publish(t, store, "ready", sources)
			config := contexts.DefaultConfiguration("pending").Canonical()
			store.fail = func(name string) error {
				if name == point {
					return errors.New("synthetic initialization interruption")
				}
				return nil
			}
			err := store.Transact(context.Background(), true, nil, func(tx contexts.Transaction) error {
				record, err := tx.Reserve(context.Background(), "pending", "", config)
				if err != nil {
					return err
				}
				if _, err := tx.MutationState(context.Background(), record.ID); err != nil {
					return err
				}
				registry := tx.Registry()
				for i := range registry.Contexts {
					if registry.Contexts[i].ID == record.ID {
						registry.Contexts[i].Mode = contexts.Ready
					}
				}
				return tx.Commit(context.Background(), registry)
			})
			expectState(t, err)
			store.fail = nil
			registry, err := store.View(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			index := slices.IndexFunc(registry.Contexts, func(record contexts.Record) bool { return record.Name == "pending" })
			if index < 0 || registry.Contexts[index].Mode != contexts.Initializing {
				t.Fatal("interruption lost pending identity")
			}
			if _, err := store.ReadInputs(context.Background(), "ready", ready.ID); err != nil {
				t.Fatal(err)
			}
			pending := registry.Contexts[index]
			err = store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
				record, err := tx.Reserve(context.Background(), "pending", "", config)
				if err == nil && record.ID != pending.ID {
					t.Fatal("retry changed identity")
				}
				return err
			})
			if point == "after-context-directory" {
				expectState(t, err)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEagerSecretInitializationNeedsNoDesiredInput(t *testing.T) {
	store, _ := fixture(t)
	var ready contexts.Record
	err := store.Transact(context.Background(), true, nil, func(tx contexts.Transaction) error {
		record, err := tx.Reserve(context.Background(), "empty", "", contexts.DefaultConfiguration("empty").Canonical())
		if err != nil {
			return err
		}
		err = tx.InitializeSecrets(context.Background(), record.ID, func(area storage.Area) error {
			session, err := localstore.New().Initialize(context.Background(), secretToken(record), area, nil)
			if err != nil {
				return err
			}
			return session.Close()
		})
		if err != nil {
			return err
		}
		registry := tx.Registry()
		ready = record
		ready.Mode = contexts.Ready
		registry.Contexts[0] = ready
		return tx.Commit(context.Background(), registry)
	})
	if err != nil {
		t.Fatalf("eager initialization: %#v", err)
	}
	revisions, err := os.ReadDir(filepath.Join(store.options.Root, "contexts", "empty", "desired-state", "revisions"))
	if err != nil || len(revisions) != 0 {
		t.Fatalf("initial desired-state revision directory: %#v %v", revisions, err)
	}
	snapshot, err := store.SecretContext(context.Background(), "empty")
	if err != nil || snapshot.Context != secretToken(ready) || snapshot.SecretStoreType != "local-keyring" || len(snapshot.Inputs.Files) != 0 {
		t.Fatalf("empty snapshot: %#v %v", snapshot, err)
	}
	err = storage.NewAccess(store, storage.NewCatalog(localstore.New()), nil).View(context.Background(), snapshot.Context, true, func(session storage.StoreSession, _ storage.Selection) error {
		_, err := session.Inspect(context.Background())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ReadInputs(context.Background(), "empty", ready.ID)
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "context.input" || diagnostics[0].Message != "context has no desired state; run context update --name empty --input-dir <dir>" {
		t.Fatalf("missing-input guidance: %#v", diagnostics)
	}
	if err := deleteContext(t, store, ready); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.options.Root, "contexts", "empty")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("permanent delete retained context key material")
	}
}
