//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A context whose operation index the retired Ceph qualification fixture wrote
// holds evidence nothing this build reads: an update's transaction, a lifecycle
// view and a lifecycle mutation refuse with the exit that index names, under
// the lease, and the deletion the orphan acknowledgement admits removes it.
func TestAContextHoldingTheRetiredFixturesIndexRefusesAndIsDeletedOnlyAsUnreadable(t *testing.T) {
	ctx := context.Background()
	source := "op-" + strings.Repeat("ab", 16)
	anchor := `"qualification":{"id":"qual-` + strings.Repeat("cd", 16) + `","source":"` + source + `"}`
	for form, index := range map[string]string{
		"version 2":              `{"version":2,"current":"` + source + `",` + anchor + "}\n",
		"a qualification member": `{"version":1,"current":"` + source + `",` + anchor + "}\n",
	} {
		store, record := lifecycleFixture(t)
		if err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
			return tx.Operations().WriteExclusive(ctx, "index.json", []byte(index))
		}); err != nil {
			t.Fatal(err)
		}
		retired := func(what string, err error) {
			t.Helper()
			reported := diagnostics.Of(err)
			if !errors.Is(err, contexts.ErrUnreadableEvidence) || !errors.Is(err, operationstore.ErrRetiredIndex) ||
				len(reported) != 1 || reported[0].Remediation != operationstore.RetiredIndexRemedy(record.Name) {
				t.Fatalf("%s: %s over the retired index = %#v (%v)", form, what, reported, err)
			}
		}
		var state error
		if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
			_, state = tx.MutationState(ctx, record.Name)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		retired("an update's mutation state", state)
		retired("a lifecycle view", store.ReadLifecycle(ctx, record.Name, func(lifecycle.View) error { return nil }))
		retired("a lifecycle mutation", store.MutateLifecycle(ctx, record.Name, func(lifecycle.Transaction) error { return nil }))
		directory := filepath.Join(store.options.Root, "contexts", record.Name)
		if err := store.TransactDeletion(ctx, record.Name, func(tx contexts.Transaction) error {
			_, state = tx.MutationState(ctx, record.Name)
			if !errors.Is(state, contexts.ErrUnreadableEvidence) {
				return state
			}
			return tx.Delete(ctx, record)
		}); err != nil {
			t.Fatalf("%s: the acknowledged deletion refused: %#v", form, diagnostics.Of(err))
		}
		retired("a deletion's mutation state", state)
		if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: the deleted context remains (%v)", form, err)
		}
	}
}

func TestADamagedOperationIndexStillLetsTheAcknowledgedDeletionRemoveTheContext(t *testing.T) {
	ctx := context.Background()
	for form, damage := range map[string]func(*testing.T, string){
		"oversized": func(t *testing.T, path string) {
			if err := os.WriteFile(path, make([]byte, operationstore.MaxIndexBytes+1), 0600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		store, record := lifecycleFixture(t)
		if err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
			return tx.Operations().WriteExclusive(ctx, "index.json", []byte(`{"version":1}`+"\n"))
		}); err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(store.options.Root, "contexts", record.Name)
		var index string
		if err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && entry.Name() == "index.json" && filepath.Base(filepath.Dir(path)) == "operations" {
				index = path
			}
			return err
		}); err != nil || index == "" {
			t.Fatalf("%s: the written index was not found (%v)", form, err)
		}
		damage(t, index)
		var state error
		if err := store.TransactDeletion(ctx, record.Name, func(tx contexts.Transaction) error {
			_, state = tx.MutationState(ctx, record.Name)
			if !errors.Is(state, contexts.ErrUnreadableEvidence) {
				return state
			}
			return tx.Delete(ctx, record)
		}); err != nil {
			t.Fatalf("%s: the acknowledged deletion refused: %#v", form, diagnostics.Of(err))
		}
		if !errors.Is(state, contexts.ErrUnreadableEvidence) {
			t.Fatalf("%s: the damaged index state = %v, want unreadable evidence", form, state)
		}
		if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: the deleted context remains (%v)", form, err)
		}
	}
}
