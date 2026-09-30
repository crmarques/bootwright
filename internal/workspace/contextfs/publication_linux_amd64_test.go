//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// controllerEntries lists the controller directory as the command that just
// returned left it.
func controllerEntries(t *testing.T, store *Store) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(store.options.Root, "controller"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// openContextState opens the fixture context's state directory as a command
// that holds it would, for a publication made in it directly.
func openContextState(t *testing.T, store *Store, record contexts.Record) *directory {
	t.Helper()
	root, err := store.openRoot(context.Background(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.file.Close() })
	container, dir, err := openContext(root, record)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { container.file.Close(); dir.file.Close() })
	runtime, err := openDirectory(dir, "state")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.file.Close() })
	return runtime
}

// A controller publication that fails before its rename, whether refused,
// cancelled or refused by its own destination proof, removes its stage before
// the command returns; left behind, stages accumulate until every controller
// publication on the host refuses.
func TestARefusedControllerPublicationLeavesNoStage(t *testing.T) {
	for _, interruption := range []struct {
		name    string
		message string
		act     func(store *Store, cancel context.CancelFunc) error
	}{
		{
			name: "refused",
			act: func(*Store, context.CancelFunc) error {
				return errors.New("refused before the controller rename")
			},
		},
		{
			name: "cancelled",
			act: func(_ *Store, cancel context.CancelFunc) error {
				cancel()
				return context.Canceled
			},
		},
		{
			name:    "replaced-destination",
			message: "controller receipt changed before publication",
			act: func(store *Store, _ context.CancelFunc) error {
				directory := filepath.Join(store.options.Root, "controller")
				data, err := os.ReadFile(filepath.Join(directory, "state.json"))
				if err != nil {
					return err
				}
				replacement := filepath.Join(filepath.Dir(store.options.Root), "replacement")
				if err := os.WriteFile(replacement, data, 0600); err != nil {
					return err
				}
				return os.Rename(replacement, filepath.Join(directory, "state.json"))
			},
		},
	} {
		t.Run(interruption.name, func(t *testing.T) {
			store := checkpointSealedFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fired := false
			store.fail = func(point string) error {
				if point != string(checkpointBeforeControllerRename) || fired {
					return nil
				}
				fired = true
				return interruption.act(store, cancel)
			}
			err := checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
				return tx.Reserve(ctx, checkpointClaim)
			})
			store.fail = nil
			if !fired {
				t.Fatal("the reservation never reached its controller rename")
			}
			if err == nil {
				t.Fatal("the interrupted reservation was published")
			}
			if reported := diagnostics.Of(err); interruption.message != "" && (len(reported) != 1 || reported[0].Message != interruption.message) {
				t.Fatalf("the replaced destination was not refused: %#v", reported)
			}
			if entries := controllerEntries(t, store); !slices.Equal(entries, []string{"bundles", "state.json"}) {
				t.Fatalf("the refused publication left %v", entries)
			}
		})
	}
}

// A first controller receipt is published without replacing anything, so a
// destination that appeared before the rename refuses it, and its stage goes
// with the refusal.
func TestAFirstControllerPublicationOverAnExistingRecordLeavesNoStage(t *testing.T) {
	ctx := context.Background()
	store, sources := fixture(t)
	publish(t, store, "alpha", sources)
	value := syntheticControllerState(t, prerequisites.SetupContext{})
	planted := false
	store.fail = func(point string) error {
		if point != string(checkpointBeforeControllerRename) || planted {
			return nil
		}
		planted = true
		return os.WriteFile(filepath.Join(store.options.Root, "controller", "state.json"), []byte("{}\n"), 0600)
	}
	err := store.MutateController(ctx, prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
		outcome, err := tx.Publish(ctx, value)
		if err != nil && outcome != prerequisites.NotCommitted {
			t.Errorf("the refused first receipt reported %v", outcome)
		}
		return err
	})
	store.fail = nil
	if !planted {
		t.Fatal("the first receipt never reached its rename")
	}
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "initial controller receipt destination exists" {
		t.Fatalf("the occupied destination was not refused: %#v", reported)
	}
	for _, entry := range controllerEntries(t, store) {
		if strings.HasPrefix(entry, "pending-") {
			t.Errorf("the refused first receipt left its stage %s", entry)
		}
	}
}

// Once the stage is renamed the receipt may be durable, so any later failure is
// reported as an unknown outcome, never as uncommitted, and nothing is staged.
func TestAFailureAfterTheRenameIsAnUnknownOutcome(t *testing.T) {
	ctx := context.Background()
	store := checkpointSealedFixture(t)
	path := filepath.Join(store.options.Root, "controller", "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	store.fail = func(point string) error {
		if point != string(checkpointAfterControllerRename) || fired {
			return nil
		}
		fired = true
		return errors.New("refused after the controller rename")
	}
	err = checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
		return tx.Reserve(ctx, checkpointClaim)
	})
	store.fail = nil
	if !fired {
		t.Fatal("the reservation never reached its controller rename")
	}
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "controller.unknown" {
		t.Fatalf("a failure after the rename was not an unknown outcome: %#v", reported)
	}
	if after, err := os.ReadFile(path); err != nil || bytes.Equal(after, before) {
		t.Fatalf("the renamed receipt is not in place (%v)", err)
	}
	err = store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
		if !slices.ContainsFunc(view.State.Reservations, func(reservation prerequisites.HostReservation) bool {
			return reservation.Context == checkpointContext && reservation.Service == checkpointClaim[0].Service
		}) {
			return errors.New("the published receipt does not hold the reservation")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if entries := controllerEntries(t, store); !slices.Equal(entries, []string{"bundles", "state.json"}) {
		t.Fatalf("the publication left %v", entries)
	}
}

// The destination is proved right before the rename, yet the filesystem can
// still refuse the rename itself. That failure is uncommitted, reports the
// publication's own message or the rename's error, and removes the stage.
func TestARenameTheFilesystemRefusesLeavesNoStage(t *testing.T) {
	const refusal = "test record could not be atomically published"
	refused := func(err error) bool {
		reported := diagnostics.Of(err)
		return len(reported) == 1 && reported[0].Message == refusal
	}
	directory := func(path string) error { return os.Mkdir(path, 0700) }
	file := func(path string) error { return os.WriteFile(path, []byte("{}\n"), 0600) }
	for _, publication := range []struct {
		name          string
		replace       bool
		renameFailure string
		occupy        func(path string) error
		expect        func(error) bool
	}{
		{"replacement over a directory", true, refusal, directory, refused},
		{"first publication over a file", false, refusal, file, refused},
		{"first publication keeping the rename's error", false, "", file, func(err error) bool { return errors.Is(err, syscall.EEXIST) }},
	} {
		t.Run(publication.name, func(t *testing.T) {
			ctx := context.Background()
			store, record := lifecycleFixture(t)
			runtime := openContextState(t, store, record)
			target := filepath.Join(contextStatePath(store), "target.json")
			outcome, err := store.publishStage(ctx, runtime, "target.json", []byte("{\"version\":1}\n"), stagedPublication{
				subject: "test record", suffix: ".json", replace: publication.replace, immutable: true, bound: maxRecord,
				renameFailure: publication.renameFailure,
				prove:         func(context.Context) error { return publication.occupy(target) },
			}, checkpointBeforeEvidenceRename, checkpointAfterEvidenceRename)
			if outcome != publicationNotCommitted || !publication.expect(err) {
				t.Fatalf("the refused rename reported %v: %v %#v", outcome, err, diagnostics.Of(err))
			}
			if stages := stagesBeneath(t, contextStatePath(store)); len(stages) != 0 {
				t.Fatalf("the refused rename left %v", stages)
			}
		})
	}
}

// A publication proves its own stage as well as its destination: a stage that
// changed or was substituted before the rename is never published, and a target
// replaced after the rename is never reported as committed.
func TestAPublicationProvesItsStageAndItsTarget(t *testing.T) {
	data := []byte("{\"version\":1}\n")
	substitute := func(store *Store, path string) error {
		replacement := filepath.Join(filepath.Dir(store.options.Root), "substitute")
		if err := os.WriteFile(replacement, data, 0600); err != nil {
			return err
		}
		return os.Rename(replacement, path)
	}
	for _, interference := range []struct {
		name    string
		point   checkpoint
		renamed bool
		act     func(store *Store, stage, target string) error
		outcome publicationOutcome
		message string
	}{
		{"stage rewritten before its read-back", checkpointSyncDirectory, false, func(_ *Store, stage, _ string) error {
			return os.WriteFile(stage, []byte("{\"version\":2}\n"), 0600)
		}, publicationNotCommitted, "staged test record changed before publication"},
		{"stage substituted before the rename", checkpointBeforeEvidenceRename, false, func(store *Store, stage, _ string) error {
			return substitute(store, stage)
		}, publicationNotCommitted, "staged test record was substituted"},
		{"target substituted after the rename", checkpointAfterEvidenceRename, true, func(store *Store, _, target string) error {
			return substitute(store, target)
		}, publicationUnknown, "published test record is unsafe"},
		{"target substituted before its final read-back", checkpointSyncDirectory, true, func(store *Store, _, target string) error {
			return substitute(store, target)
		}, publicationUnknown, "published test record is unsafe"},
	} {
		t.Run(interference.name, func(t *testing.T) {
			ctx := context.Background()
			store, record := lifecycleFixture(t)
			runtime := openContextState(t, store, record)
			target := filepath.Join(contextStatePath(store), "target.json")
			renamed, acted := false, false
			store.fail = func(point string) error {
				renamed = renamed || point == string(checkpointAfterEvidenceRename)
				if acted || point != string(interference.point) || renamed != interference.renamed {
					return nil
				}
				acted = true
				stages := stagesBeneath(t, contextStatePath(store))
				if len(stages) > 1 {
					return errors.New("more than one stage is staged")
				}
				return interference.act(store, strings.Join(stages, ""), target)
			}
			outcome, err := store.publishStage(ctx, runtime, "target.json", data, stagedPublication{
				subject: "test record", suffix: ".json", replace: true, immutable: true, bound: maxRecord,
			}, checkpointBeforeEvidenceRename, checkpointAfterEvidenceRename)
			store.fail = nil
			if !acted {
				t.Fatal("the publication never reached the interference")
			}
			if reported := diagnostics.Of(err); outcome != interference.outcome || len(reported) != 1 || reported[0].Message != interference.message {
				t.Fatalf("the interference reported %v: %v %#v", outcome, err, reported)
			}
			if _, err := os.Lstat(target); interference.outcome == publicationNotCommitted && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("an unproved stage was published (%v)", err)
			}
		})
	}
}

// substituteSameBytes replaces the file at path with a new inode holding the
// same bytes, which only an identity proof tells apart.
func substituteSameBytes(store *Store, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	replacement := filepath.Join(filepath.Dir(store.options.Root), "replacement")
	if err := os.WriteFile(replacement, data, 0600); err != nil {
		return err
	}
	return os.Rename(replacement, path)
}

// A registry replacement that fails before its rename, whether refused,
// cancelled or refused by its own destination proof, removes its stage, and
// one that fails after the rename is uncertain and has no stage left.
func TestARefusedRegistryReplacementLeavesNoStage(t *testing.T) {
	refuse := func(*Store, context.CancelFunc) error { return errors.New("refused at the registry rename") }
	for _, interruption := range []struct {
		name    string
		point   checkpoint
		message string
		act     func(store *Store, cancel context.CancelFunc) error
	}{
		{name: "refused", point: checkpointBeforeRegistryRename, act: refuse},
		{name: "cancelled", point: checkpointBeforeRegistryRename, act: func(_ *Store, cancel context.CancelFunc) error {
			cancel()
			return context.Canceled
		}},
		{name: "replaced-destination", point: checkpointBeforeRegistryRename, message: "registry was replaced or modified during the transaction", act: func(store *Store, _ context.CancelFunc) error {
			return substituteSameBytes(store, filepath.Join(store.options.Root, "registry.json"))
		}},
		{name: "refused-after-rename", point: checkpointAfterRegistryRename, message: "registry publication may have completed, but its disk state is unconfirmed", act: refuse},
	} {
		t.Run(interruption.name, func(t *testing.T) {
			store, _ := lifecycleFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fired := false
			store.fail = func(point string) error {
				if point != string(interruption.point) || fired {
					return nil
				}
				fired = true
				return interruption.act(store, cancel)
			}
			err := checkpointCommit(ctx, store, checkpointContext)
			store.fail = nil
			if !fired {
				t.Fatalf("the commit never reached %s", interruption.point)
			}
			if err == nil {
				t.Fatal("the interrupted registry replacement succeeded")
			}
			if reported := diagnostics.Of(err); interruption.message != "" && (len(reported) != 1 || reported[0].Message != interruption.message) {
				t.Fatalf("the interruption was not reported as %q: %v %#v", interruption.message, err, reported)
			}
			entries, err := os.ReadDir(store.options.Root)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if stageName(entry.Name(), true) {
					t.Errorf("the interrupted registry replacement left %s", entry.Name())
				}
			}
		})
	}
}

// A secret replacement that fails before its rename removes its stage and is
// not committed; one that fails after the rename is uncertain and finishes the
// callback's mutation authority.
func TestARefusedSecretReplacementLeavesNoStage(t *testing.T) {
	refuse := func(*Store, context.CancelFunc) error { return errors.New("refused at the secret rename") }
	for _, interruption := range []struct {
		name    string
		point   checkpoint
		outcome secretstore.Outcome
		message string
		act     func(store *Store, cancel context.CancelFunc) error
	}{
		{name: "refused", point: checkpointBeforeSecretRename, outcome: secretstore.NotCommitted, act: refuse},
		{name: "cancelled", point: checkpointBeforeSecretRename, outcome: secretstore.NotCommitted, act: func(_ *Store, cancel context.CancelFunc) error {
			cancel()
			return context.Canceled
		}},
		{name: "replaced-destination", point: checkpointBeforeSecretRename, outcome: secretstore.NotCommitted, message: "secret state was replaced or modified before publication", act: func(store *Store, _ context.CancelFunc) error {
			return substituteSameBytes(store, filepath.Join(store.options.Root, "contexts", checkpointContext, "secrets", secretstore.RecordPath))
		}},
		{name: "refused-after-rename", point: checkpointAfterSecretRename, outcome: secretstore.Uncertain, message: "secret state publication has uncertain durability; inspect it before retrying", act: refuse},
	} {
		t.Run(interruption.name, func(t *testing.T) {
			store, token := secretPublicationFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fired := false
			store.fail = func(point string) error {
				if point != string(interruption.point) || fired {
					return nil
				}
				fired = true
				return interruption.act(store, cancel)
			}
			var outcome secretstore.Outcome
			var replaced, later error
			_ = store.MutateSecrets(ctx, token, func(area secretstore.Area) error {
				expected, exists, err := area.ReadMutable(ctx, secretstore.RecordPath, maxSecretBytes)
				if err != nil || !exists {
					return errors.New("the fixture has no secret record")
				}
				outcome, replaced = area.Replace(ctx, secretstore.RecordPath, []byte("{}\n"), expected)
				if outcome == secretstore.Uncertain {
					later = area.WriteExclusive(context.Background(), "later", []byte("{}\n"))
				}
				return nil
			})
			store.fail = nil
			if !fired {
				t.Fatalf("the replacement never reached %s", interruption.point)
			}
			if outcome != interruption.outcome || replaced == nil {
				t.Fatalf("the interrupted replacement reported %s (%v)", outcome, replaced)
			}
			if reported := diagnostics.Of(replaced); interruption.message != "" && (len(reported) != 1 || reported[0].Message != interruption.message) {
				t.Fatalf("the interruption was not reported as %q: %v %#v", interruption.message, replaced, reported)
			}
			if outcome == secretstore.Uncertain {
				if reported := diagnostics.Of(later); len(reported) != 1 || reported[0].Message != "secret storage callback has already finished" {
					t.Fatalf("an uncertain replacement left the callback writable: %v %#v", later, reported)
				}
			}
			if stages := stagesBeneath(t, filepath.Join(store.options.Root, "contexts", checkpointContext, "secrets")); len(stages) != 0 {
				t.Fatalf("the interrupted replacement left %v", stages)
			}
		})
	}
}
