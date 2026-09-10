//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func replaceInput(store *Store, record contexts.Record, sources desiredstate.Sources) error {
	ctx := context.Background()
	return store.Transact(ctx, false, sources.Roots, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(ctx, record.ID); err != nil {
			return err
		}
		revision, err := tx.Publish(ctx, record.ID, sources.Roots[0], sources)
		if err != nil {
			return err
		}
		registry := tx.Registry()
		for index := range registry.Contexts {
			if registry.Contexts[index].ID == record.ID {
				registry.Contexts[index].Revision = revision
			}
		}
		return tx.Commit(ctx, registry)
	})
}

func revisionDirectory(store *Store, record contexts.Record) string {
	return filepath.Join(store.options.Root, "contexts", record.Name, "desired-state", "revisions")
}

func requireSelectedInput(t *testing.T, store *Store, record contexts.Record, content []byte, retained int) contexts.Record {
	t.Helper()
	registry, err := store.View(context.Background())
	if err != nil || len(registry.Contexts) != 1 {
		t.Fatalf("context publication: %#v %v", registry, err)
	}
	selected := registry.Contexts[0]
	input, err := store.ReadInputs(context.Background(), selected.Name, selected.ID)
	if err != nil || len(input.Files) != 1 || !bytes.Equal(input.Files[0].Bytes(), content) {
		t.Fatalf("selected input is not readable: %#v %v", input, err)
	}
	entries, err := os.ReadDir(revisionDirectory(store, record))
	if err != nil || len(entries) != retained {
		t.Fatalf("retained revisions: count=%d wanted=%d err=%v", len(entries), retained, err)
	}
	return selected
}

func TestRepeatedInputUpdatesCollectUnselectedRevisions(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	for index := range 32 {
		data := fmt.Appendf(nil, "version: %d\n", index)
		sources.Files[0] = desiredstate.NewSourceFile(sources.Files[0].Path(), data)
		if err := replaceInput(store, record, sources); err != nil {
			t.Fatal(err)
		}
		record = requireSelectedInput(t, store, record, data, 1)
	}
}

func TestInputCleanupWaitsForDurableSelectionAndResumes(t *testing.T) {
	for _, point := range []string{"before-registry-rename", "after-registry-rename", "sync-published-registry", "before-revision-cleanup", "before-context-unlink", "before-revision-rmdir"} {
		t.Run(point, func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			original := sources.Files[0].Bytes()
			updated := []byte("version: replacement\n")
			sources.Files[0] = desiredstate.NewSourceFile(sources.Files[0].Path(), updated)
			reached, renamed := false, false
			store.fail = func(checkpoint string) error {
				if checkpoint == "after-registry-rename" {
					renamed = true
				}
				if checkpoint == point || point == "sync-published-registry" && renamed && checkpoint == "sync-directory" {
					reached = true
					return errors.New("synthetic input cleanup interruption")
				}
				return nil
			}
			err := replaceInput(store, record, sources)
			expectState(t, err)
			store.fail = nil
			if !reached {
				t.Fatal("interruption boundary was not reached")
			}
			content := updated
			if point == "before-registry-rename" {
				content = original
			}
			selected := requireSelectedInput(t, store, record, content, 2)
			if point == "before-registry-rename" && selected.Revision != record.Revision || point != "before-registry-rename" && selected.Revision == record.Revision {
				t.Fatal("selected revision disagrees with the publication boundary")
			}
			if point == "after-registry-rename" || point == "sync-published-registry" || point == "before-revision-cleanup" {
				data, err := os.ReadFile(filepath.Join(revisionDirectory(store, record), record.Revision, "file-0000"))
				if err != nil || !bytes.Equal(data, original) {
					t.Fatal("unconfirmed cleanup removed old input")
				}
			}
			if strings.HasPrefix(point, "before-revision-") || point == "before-context-unlink" {
				if !strings.Contains(desiredstate.DiagnosticsOf(err)[0].Message, "was published") {
					t.Fatal("post-publication failure did not report committed input")
				}
			}
			if err := replaceInput(store, record, sources); err != nil {
				t.Fatal(err)
			}
			requireSelectedInput(t, store, record, updated, 1)
		})
	}
}

func TestInputCleanupSyncFailureKeepsDurableSelection(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	removing, failed := false, false
	store.fail = func(point string) error {
		if point == "before-revision-rmdir" {
			removing = true
		}
		if removing && point == "sync-directory" {
			failed = true
			return errors.New("synthetic removed-revision durability failure")
		}
		return nil
	}
	expectState(t, replaceInput(store, record, sources))
	store.fail = nil
	if !failed {
		t.Fatal("cleanup durability boundary was not reached")
	}
	selected := requireSelectedInput(t, store, record, sources.Files[0].Bytes(), 1)
	if selected.Revision == record.Revision {
		t.Fatal("cleanup failure rolled back published input")
	}
	if err := replaceInput(store, record, sources); err != nil {
		t.Fatal(err)
	}
	requireSelectedInput(t, store, record, sources.Files[0].Bytes(), 1)
}

func TestInputCleanupPreservesProtectedRevisions(t *testing.T) {
	for _, evidence := range []string{
		"{\"version\":1,\"operation\":\"applied\",\"ownership\":\"retained\"}\n",
		"{\"version\":1,\"operation\":\"pending\",\"ownership\":\"none\"}\n",
		"{\"version\":2,\"operation\":\"none\",\"ownership\":\"none\"}\n",
	} {
		t.Run(evidence, func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			writePrivate(t, filepath.Join(store.options.Root, "contexts", record.Name, "state", "mutation.json"), []byte(evidence))
			if err := replaceInput(store, record, sources); err != nil {
				t.Fatal(err)
			}
			requireSelectedInput(t, store, record, sources.Files[0].Bytes(), 2)
		})
	}
}

func TestInputCleanupRefusesReplacedRevision(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	old := filepath.Join(revisionDirectory(store, record), record.Revision)
	retained := filepath.Join(filepath.Dir(store.options.Root), "retained-revision")
	replaced := false
	store.fail = func(point string) error {
		if point != "before-revision-remove" || replaced {
			return nil
		}
		if err := os.Rename(old, retained); err != nil {
			return err
		}
		if err := os.Mkdir(old, 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(old, "file-0000"), []byte("substituted input"), 0600); err != nil {
			return err
		}
		replaced = true
		return nil
	}
	expectState(t, replaceInput(store, record, sources))
	store.fail = nil
	if !replaced {
		t.Fatal("revision substitution was not reached")
	}
	for _, path := range []string{old, retained} {
		if _, err := os.Stat(filepath.Join(path, "file-0000")); err != nil {
			t.Fatal("cleanup removed content after directory substitution")
		}
	}
	requireSelectedInput(t, store, record, sources.Files[0].Bytes(), 2)
}

func TestInputCleanupRefusesUnsafeUnselectedPayload(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	blob := filepath.Join(revisionDirectory(store, record), record.Revision, "file-0000")
	if err := os.Remove(blob); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(blob, 0600); err != nil {
		t.Fatal(err)
	}
	expectState(t, replaceInput(store, record, sources))
	requireSelectedInput(t, store, record, sources.Files[0].Bytes(), 2)
	if info, err := os.Lstat(blob); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("cleanup opened or removed unsafe unselected payload")
	}
}

func TestInputCleanupRevalidatesSelectionAndEvidenceBeforeUnlink(t *testing.T) {
	for _, target := range []string{"selection", "mutation evidence"} {
		t.Run(target, func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			registryPath := filepath.Join(store.options.Root, "registry.json")
			oldRegistry, err := os.ReadFile(registryPath)
			if err != nil {
				t.Fatal(err)
			}
			changed := false
			store.fail = func(point string) error {
				if point != "before-context-unlink" || changed {
					return nil
				}
				changed = true
				if target == "selection" {
					return os.WriteFile(registryPath, oldRegistry, 0600)
				}
				path := filepath.Join(store.options.Root, "contexts", record.Name, "state", "mutation.json")
				return os.WriteFile(path, []byte("{\"version\":1,\"operation\":\"pending\",\"ownership\":\"none\"}\n"), 0600)
			}
			expectState(t, replaceInput(store, record, sources))
			store.fail = nil
			if !changed {
				t.Fatal("effect-boundary substitution did not occur")
			}
			data, err := os.ReadFile(filepath.Join(revisionDirectory(store, record), record.Revision, "file-0000"))
			if err != nil || !bytes.Equal(data, sources.Files[0].Bytes()) {
				t.Fatal("cleanup removed input after its retention proof changed")
			}
			requireSelectedInput(t, store, record, sources.Files[0].Bytes(), 2)
		})
	}
}

func TestInputUpdateReclaimsFullLegacyRevisionDirectory(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	for index := range maxRevisions - 1 {
		name := fmt.Sprintf("rev-%032x", index)
		if err := os.Mkdir(filepath.Join(revisionDirectory(store, record), name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	store.fail = func(point string) error {
		if point == "sync-directory" {
			return errors.New("synthetic existing-selection durability failure")
		}
		return nil
	}
	expectState(t, replaceInput(store, record, sources))
	store.fail = nil
	requireSelectedInput(t, store, record, sources.Files[0].Bytes(), maxRevisions)
	if err := replaceInput(store, record, sources); err != nil {
		t.Fatal(err)
	}
	requireSelectedInput(t, store, record, sources.Files[0].Bytes(), 1)
}
