//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func lifecycleFixture(t *testing.T) (*Store, contexts.Record) {
	t.Helper()
	store, sources := fixture(t)
	return store, publish(t, store, "example", sources)
}

func TestLifecycleViewExposesTheContextSnapshot(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	seen := false
	err := store.ReadLifecycle(ctx, "example", func(view lifecycle.View) error {
		seen = true
		identity := view.Identity()
		if identity.Name != record.Name || identity.ID != record.ID || identity.Revision != record.Revision {
			t.Fatalf("identity = %+v, want %+v", identity, record)
		}
		if len(view.Inputs().Files) != 1 {
			t.Fatalf("inputs = %+v", view.Inputs())
		}
		if string(view.Evidence()) != pristineMutation {
			t.Fatalf("evidence = %q", view.Evidence())
		}
		if _, found, err := view.Operations().Read(ctx, "index.json", 4096); err != nil || found {
			t.Fatalf("a fresh context already has operation records (%v)", err)
		}
		return nil
	})
	if err != nil || !seen {
		t.Fatalf("read = %v seen=%t", err, seen)
	}
}

func TestLifecycleReadRefusesUnusableContexts(t *testing.T) {
	ctx := context.Background()
	store, _ := fixture(t)
	if err := store.ReadLifecycle(ctx, "example", func(lifecycle.View) error { return nil }); err == nil {
		t.Fatal("an absent store produced a lifecycle view")
	}
	store, sources := fixture(t)
	publish(t, store, "example", sources)
	for name, target := range map[string]string{"unknown": "missing", "invalid": "Example"} {
		t.Run(name, func(t *testing.T) {
			if err := store.ReadLifecycle(ctx, target, func(lifecycle.View) error { return nil }); err == nil {
				t.Fatal("an unusable context produced a lifecycle view")
			}
		})
	}
	if err := store.ReadLifecycle(ctx, "example", nil); err == nil {
		t.Fatal("a missing callback was accepted")
	}
}

func TestLifecycleReadRefusesAContextWithoutInput(t *testing.T) {
	ctx := context.Background()
	store, sources := fixture(t)
	var record contexts.Record
	err := store.Transact(ctx, true, sources.Roots, func(tx contexts.Transaction) error {
		var err error
		record, err = tx.Reserve(ctx, "empty", sources.Roots[0], contexts.DefaultConfiguration("empty").Canonical())
		if err != nil {
			return err
		}
		if _, err := tx.MutationState(ctx, record.ID); err != nil {
			return err
		}
		registry := tx.Registry()
		record.Mode = contexts.Ready
		for i := range registry.Contexts {
			if registry.Contexts[i].ID == record.ID {
				registry.Contexts[i] = record
			}
		}
		return tx.Commit(ctx, registry)
	})
	if err != nil {
		t.Fatal(err)
	}
	readErr := store.ReadLifecycle(ctx, "empty", func(lifecycle.View) error { return nil })
	reported := diagnostics.Of(readErr)
	if readErr == nil || len(reported) != 1 || reported[0].Code != "context.state" || !strings.Contains(reported[0].Message, "revision") || !strings.Contains(reported[0].Remediation, "context update") {
		t.Fatalf("a context without input produced a lifecycle view: %+v", reported)
	}
}

func TestOperationAreaPublishesAndReplacesRecords(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		area := tx.Operations()
		if err := area.WriteExclusive(ctx, "index.json", []byte("{\"version\":1}\n")); err != nil {
			return err
		}
		if err := area.WriteExclusive(ctx, "index.json", []byte("x")); err == nil {
			t.Fatal("an exclusive write overwrote an existing record")
		}
		if err := area.Replace(ctx, "index.json", []byte("next\n"), []byte("wrong")); err == nil {
			t.Fatal("a replacement ignored its expectation")
		}
		if err := area.Replace(ctx, "index.json", []byte("next\n"), []byte("{\"version\":1}\n")); err != nil {
			return err
		}
		data, found, err := area.Read(ctx, "index.json", 4096)
		if err != nil || !found || string(data) != "next\n" {
			t.Fatalf("read back = %q %t (%v)", data, found, err)
		}
		if err := area.Replace(ctx, "fresh.json", []byte("a\n"), nil); err != nil {
			return err
		}
		if err := area.Replace(ctx, "fresh.json", []byte("b\n"), nil); err == nil {
			t.Fatal("a nil expectation overwrote an existing record")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOperationAreaConfinesEveryPath(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		area := tx.Operations()
		for _, target := range []string{
			"../escape.json", "/absolute.json", "a/../../b.json", "..", ".hidden",
			"UPPER.json", "a b.json", "a\x00b", strings.Repeat("a/", 8) + "deep.json",
			strings.Repeat("x", 129), "-leading", "trailing.", "two.dots.json",
		} {
			if err := area.WriteExclusive(ctx, target, []byte("x\n")); err == nil {
				t.Fatalf("an unsafe path was accepted: %q", target)
			}
			if _, _, err := area.Read(ctx, target, 16); err == nil {
				t.Fatalf("an unsafe path was read: %q", target)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOperationAreaIsReadOnlyForInspection(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	err := store.ReadLifecycle(ctx, "example", func(view lifecycle.View) error {
		area := view.Operations()
		if err := area.WriteExclusive(ctx, "index.json", []byte("x\n")); err == nil {
			t.Fatal("inspection wrote an operation record")
		}
		if err := area.EnsureDirectory(ctx, "op-1"); err == nil {
			t.Fatal("inspection created an operation directory")
		}
		if err := area.Append(ctx, "log.jsonl", []byte("x\n")); err == nil {
			t.Fatal("inspection appended to a log")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOperationCapabilityExpiresWithItsCallback(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	var escaped operationstore.Area
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		escaped = tx.Operations()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := escaped.WriteExclusive(ctx, "index.json", []byte("x\n")); err == nil {
		t.Fatal("an expired operation capability still wrote")
	}
	if _, _, err := escaped.Read(ctx, "index.json", 16); err == nil {
		t.Fatal("an expired operation capability still read")
	}
}

func TestLifecycleEvidenceIsPublishedUnderItsExpectation(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	pending, err := reconciliation.Evidence{Operation: reconciliation.MutationPending, Ownership: reconciliation.OwnershipRetained}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		if string(tx.Evidence()) != pristineMutation {
			t.Fatalf("initial evidence = %q", tx.Evidence())
		}
		if err := tx.PublishEvidence(ctx, pending); err != nil {
			return err
		}
		if string(tx.Evidence()) != string(pending) {
			t.Fatal("the transaction did not observe its own publication")
		}
		return tx.PublishEvidence(ctx, pending)
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(store.options.Root, "contexts", record.Name, "state", "mutation.json"))
	if err != nil || string(stored) != string(pending) {
		t.Fatalf("stored evidence = %q (%v)", stored, err)
	}
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.PublishEvidence(ctx, []byte(""))
	}); err == nil {
		t.Fatal("empty evidence was published")
	}
}

func TestSecondLifecycleMutatorRefusesWhileOneHoldsTheStore(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	other := New(Options{Root: store.options.Root, Owner: &Ownership{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}})
	err := store.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error {
		return other.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error {
			t.Fatal("a second mutator entered the store")
			return nil
		})
	})
	if err == nil {
		t.Fatal("a concurrent lifecycle mutation succeeded")
	}
}

// reserveFixture publishes a controller receipt so reservations have a record
// to live in, mirroring a host that completed bastion setup.
func reserveFixture(t *testing.T, store *Store, record contexts.Record) {
	t.Helper()
	scope := prerequisites.SetupContext{Name: record.Name, ID: record.ID, Revision: record.Revision, Machine: "bastion"}
	publishControllerState(t, store, scope, completeControllerState(syntheticControllerState(t, scope)))
}

func TestReservationsRefuseAnotherContextsKeys(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	reserveFixture(t, store, record)
	claim := []prerequisites.HostReservation{{
		ContextID: record.ID, Kind: "artifact-server", Service: "lab-artifacts",
		Keys: []string{"socket:192.0.2.1:8443", "unit:bootwright-artifacts"},
	}}
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Reserve(ctx, claim)
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Reserve(ctx, claim)
	}); err != nil {
		t.Fatal("a context could not replace its own reservation:", err)
	}
	foreign := slices.Clone(claim)
	foreign[0].ContextID = "ctx-" + strings.Repeat("ff", 16)
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Reserve(ctx, foreign)
	}); err == nil {
		t.Fatal("a context reserved on behalf of another context")
	}
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.ReleaseReservations(ctx)
	}); err != nil {
		t.Fatal(err)
	}
	err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
		if len(view.State.Reservations) != 0 {
			t.Fatalf("reservations survived release: %+v", view.State.Reservations)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReservationRequiresACompletedSetup(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Reserve(ctx, []prerequisites.HostReservation{{
			ContextID: record.ID, Kind: "artifact-server", Service: "lab-artifacts", Keys: []string{"unit:x"},
		}})
	})
	if err == nil {
		t.Fatal("a reservation was published without controller evidence")
	}
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.ReleaseReservations(ctx)
	}); err != nil {
		t.Fatal("releasing nothing without controller evidence failed:", err)
	}
}

// Every publication checkpoint must be reachable, or the fault injection below
// proves nothing about interruption.
func TestLifecyclePublicationCheckpointsFireAndFailClosed(t *testing.T) {
	ctx := context.Background()
	for _, checkpoint := range []string{"before-evidence", "before-reservation", "before-operation-rename", "after-operation-rename", "append-operation-log"} {
		t.Run(checkpoint, func(t *testing.T) {
			store, record := lifecycleFixture(t)
			reserveFixture(t, store, record)
			fired := false
			store.fail = func(name string) error {
				if name != checkpoint {
					return nil
				}
				fired = true
				return errors.New("interrupted at " + name)
			}
			err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
				area := tx.Operations()
				if err := area.WriteExclusive(ctx, "index.json", []byte("{\"version\":1}\n")); err != nil {
					return err
				}
				if err := area.Replace(ctx, "index.json", []byte("next\n"), []byte("{\"version\":1}\n")); err != nil {
					return err
				}
				if err := area.Append(ctx, "log.jsonl", []byte("{}\n")); err != nil {
					return err
				}
				pending, err := reconciliation.Evidence{Operation: reconciliation.MutationPending, Ownership: reconciliation.OwnershipRetained}.Bytes()
				if err != nil {
					return err
				}
				if err := tx.PublishEvidence(ctx, pending); err != nil {
					return err
				}
				return tx.Reserve(ctx, []prerequisites.HostReservation{{
					ContextID: record.ID, Kind: "artifact-server", Service: "lab-artifacts", Keys: []string{"unit:x"},
				}})
			})
			if !fired {
				t.Fatalf("checkpoint %s was never reached", checkpoint)
			}
			if err == nil {
				t.Fatalf("checkpoint %s did not fail the operation", checkpoint)
			}
		})
	}
}

// Acquiring confidential material inside a lifecycle transaction must refuse
// rather than block: both take the same exclusive root lock, so a lifecycle
// operation binds its Secrets before it opens the transaction.
func TestSecretAcquisitionInsideALifecycleTransactionRefuses(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	selected := secretstore.Context{Name: record.Name, ID: record.ID, Mode: string(record.Mode), Revision: record.Revision}
	entered := false
	err := store.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error {
		return store.MutateSecrets(ctx, selected, func(secretstore.Area) error {
			entered = true
			return nil
		})
	})
	if err == nil || entered {
		t.Fatalf("a nested secret mutation entered the store: entered=%t err=%v", entered, err)
	}
	if err := store.MutateSecrets(ctx, selected, func(secretstore.Area) error { return nil }); err != nil {
		t.Fatal("secret mutation failed outside the lifecycle transaction:", err)
	}
}
