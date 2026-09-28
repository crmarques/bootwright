//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// A planted stage is exactly what a killed publication leaves: a private file
// named pending- and 32 hexadecimal digits beside its target. The checkpoint
// harness supplies the real kills.

// collectedRun is the bounded run whose area the tests plant a stage in.
var collectedRun = "run-" + strings.Repeat("a", 32)

func plantedStageName(index int, suffix string) string {
	return fmt.Sprintf("pending-%032x%s", index, suffix)
}

func plantStage(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func contextStatePath(store *Store) string {
	return filepath.Join(store.options.Root, "contexts", checkpointContext, "state")
}

// collectingLease is the smallest command that takes the context's lease.
func collectingLease(ctx context.Context, store *Store) error {
	return checkpointMutateLifecycle(ctx, store, func(lifecycle.Transaction) error { return nil })
}

// layOutEveryStateEntry gives the context all five state entries: its two
// records and its operation, run and trust areas.
func layOutEveryStateEntry(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	if err := checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
		return tx.Operations().EnsureDirectory(ctx, "op-1")
	}); err != nil {
		t.Fatalf("operation directory: %#v", diagnostics.Of(err))
	}
	if err := store.RunLifecycle(ctx, checkpointContext, func(view lifecycle.RunView) error {
		return view.Runs().EnsureDirectory(ctx, collectedRun)
	}); err != nil {
		t.Fatalf("run directory: %#v", diagnostics.Of(err))
	}
	if err := store.ReplaceHostKeys(ctx, checkpointContext, []byte(trustRecords), nil); err != nil {
		t.Fatalf("trust records: %#v", diagnostics.Of(err))
	}
	entries, err := os.ReadDir(contextStatePath(store))
	if err != nil || len(entries) != maxContextStateEntries {
		t.Fatalf("the state directory holds %d entries, want %d (%v)", len(entries), maxContextStateEntries, err)
	}
}

// stagesBeneath lists every entry named as a stage beneath directory.
func stagesBeneath(t *testing.T, directory string) []string {
	t.Helper()
	var stages []string
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(entry.Name(), "pending-") {
			stages = append(stages, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return stages
}

func expectRefusal(t *testing.T, err error, message string) {
	t.Helper()
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != message {
		t.Fatalf("want the refusal %q, got %v %#v", message, err, reported)
	}
}

// A kill leaves complete and torn stages in the state directory and in the
// operation, run and trust areas; the next command that takes the lease
// removes every one before it verifies the layout.
func TestTheNextLeaseCollectsWhatAKilledPublicationLeft(t *testing.T) {
	store, _ := lifecycleFixture(t)
	layOutEveryStateEntry(t, store)
	runtime := contextStatePath(store)
	complete, err := os.ReadFile(filepath.Join(runtime, "mutation.json"))
	if err != nil {
		t.Fatal(err)
	}
	for index, area := range []struct{ directory, suffix string }{
		{runtime, ".json"},
		{filepath.Join(runtime, "operations", "op-1"), ""},
		{filepath.Join(runtime, "runs", collectedRun), ""},
		{filepath.Join(runtime, trustSubtree), ".json"},
	} {
		plantStage(t, filepath.Join(area.directory, plantedStageName(2*index, area.suffix)), complete)
		plantStage(t, filepath.Join(area.directory, plantedStageName(2*index+1, area.suffix)), complete[:len(complete)/2])
	}
	if planted := stagesBeneath(t, runtime); len(planted) != 8 {
		t.Fatalf("planted %d stages, want 8", len(planted))
	}
	if err := collectingLease(context.Background(), store); err != nil {
		t.Fatalf("the lease refused what a killed publication left: %#v", diagnostics.Of(err))
	}
	if remaining := stagesBeneath(t, runtime); len(remaining) != 0 {
		t.Fatalf("the lease left %v", remaining)
	}
}

// Deleting a ready context takes its lease first, so what a killed
// publication left never refuses the walk that removes the context.
func TestAContextDeleteCollectsBeforeItWalks(t *testing.T) {
	store, record := lifecycleFixture(t)
	runtime := contextStatePath(store)
	for index := range 2 {
		plantStage(t, filepath.Join(runtime, plantedStageName(index, ".json")), []byte("{}\n"))
	}
	if err := deleteContext(t, store, record); err != nil {
		t.Fatalf("the delete refused what a killed publication left: %#v", diagnostics.Of(err))
	}
	if _, err := os.Stat(filepath.Dir(runtime)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the deleted context remains (%v)", err)
	}
}

// A secret mutation verifies the context's layout too, so it collects the
// state directory's stages first even when no lease came before it.
func TestASecretMutationFirstCollectsTheContextsStages(t *testing.T) {
	store, record := lifecycleFixture(t)
	runtime := contextStatePath(store)
	for index := range 2 {
		plantStage(t, filepath.Join(runtime, plantedStageName(index, ".json")), []byte("{}\n"))
	}
	if err := store.MutateSecrets(context.Background(), secretToken(record), func(secretstore.Area) error { return nil }); err != nil {
		t.Fatalf("the secret mutation refused what a killed publication left: %#v", diagnostics.Of(err))
	}
	if remaining := stagesBeneath(t, runtime); len(remaining) != 0 {
		t.Fatalf("the secret mutation left %v", remaining)
	}
}

// An earlier build leaves at most maxControllerStages stages beside the receipt
// and its bundles, the count at which every controller publication refuses;
// the next command that opens a registry transaction removes all of them.
func TestTheNextExclusiveCommandCollectsControllerStages(t *testing.T) {
	for _, command := range []struct {
		name string
		run  func(context.Context, *Store) error
	}{
		{"commit", func(ctx context.Context, store *Store) error { return checkpointCommit(ctx, store, "") }},
		{"reserve", func(ctx context.Context, store *Store) error {
			return checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
				return tx.Reserve(ctx, checkpointClaim)
			})
		}},
	} {
		t.Run(command.name, func(t *testing.T) {
			store := checkpointSealedFixture(t)
			directory := filepath.Join(store.options.Root, "controller")
			receipt, err := os.ReadFile(filepath.Join(directory, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			for index := range maxControllerStages {
				plantStage(t, filepath.Join(directory, plantedStageName(index, ".json")), receipt)
			}
			if entries := controllerEntries(t, store); len(entries) != maxControllerStages+2 {
				t.Fatalf("the controller directory holds %d entries, want %d", len(entries), maxControllerStages+2)
			}
			if err := command.run(context.Background(), store); err != nil {
				t.Fatalf("the %s refused what killed publications left: %#v", command.name, diagnostics.Of(err))
			}
			if entries := controllerEntries(t, store); !slices.Equal(entries, []string{"bundles", "state.json"}) {
				t.Fatalf("the %s left %v", command.name, entries)
			}
		})
	}
}

// A read holds only the shared root lock, which a live writer of a stage may
// hold beside it, so no read removes anything.
func TestReadsNeverCollectAStage(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	if err := checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
		return tx.Operations().EnsureDirectory(ctx, "op-1")
	}); err != nil {
		t.Fatal(err)
	}
	receipt, err := os.ReadFile(filepath.Join(store.options.Root, "controller", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	planted := map[string][]byte{
		filepath.Join(store.options.Root, "controller", plantedStageName(1, ".json")):         receipt,
		filepath.Join(contextStatePath(store), "operations", "op-1", plantedStageName(2, "")): []byte("{}\n"),
		filepath.Join(contextStatePath(store), plantedStageName(3, ".json")):                  []byte("{}\n"),
	}
	for path, data := range planted {
		plantStage(t, path, data)
	}
	for _, read := range []struct {
		name string
		run  func() error
	}{
		{"view", func() error { _, err := store.View(ctx); return err }},
		{"lifecycle", func() error {
			return store.ReadLifecycle(ctx, checkpointContext, func(lifecycle.View) error { return nil })
		}},
		{"bounded run", func() error {
			return store.RunLifecycle(ctx, checkpointContext, func(lifecycle.RunView) error { return nil })
		}},
		{"controller", func() error {
			return store.ReadController(ctx, "", func(prerequisites.StorageView) error { return nil })
		}},
		{"host keys", func() error { _, err := store.ReadHostKeys(ctx, checkpointContext); return err }},
		{"secrets", func() error {
			return store.ReadSecrets(ctx, secretToken(record), func(secretstore.Area) error { return nil })
		}},
	} {
		if err := read.run(); err != nil {
			t.Fatalf("the %s read refused: %#v", read.name, diagnostics.Of(err))
		}
		for path, data := range planted {
			if stored, err := os.ReadFile(path); err != nil || !bytes.Equal(stored, data) {
				t.Fatalf("the %s read changed %s (%v)", read.name, path, err)
			}
		}
	}
}

// The state directory's own five entries already fill its layout bound, so
// collection lists it with room for abandoned stages beside them.
func TestTheStateDirectoryCollectsBesideItsFullLayout(t *testing.T) {
	store, _ := lifecycleFixture(t)
	layOutEveryStateEntry(t, store)
	runtime := contextStatePath(store)
	for index := range 2 {
		plantStage(t, filepath.Join(runtime, plantedStageName(index, ".json")), []byte("{}\n"))
	}
	if err := collectingLease(context.Background(), store); err != nil {
		t.Fatalf("the lease refused stages beside a full layout: %#v", diagnostics.Of(err))
	}
	entries, err := os.ReadDir(runtime)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !contextStateEntry(entry.Name()) {
			t.Errorf("the lease left %s", entry.Name())
		}
	}
}

// Beyond its allowance the state directory is not listed for collection at
// all, so the layout verification refuses it exactly as it did before.
func TestStagesBeyondTheStateAllowanceStillRefuse(t *testing.T) {
	store, _ := lifecycleFixture(t)
	layOutEveryStateEntry(t, store)
	runtime := contextStatePath(store)
	for index := range maxContextStateStages + 1 {
		plantStage(t, filepath.Join(runtime, plantedStageName(index, ".json")), []byte("{}\n"))
	}
	expectRefusal(t, collectingLease(context.Background(), store), "state directory entry count exceeds its limit")
	if remaining := stagesBeneath(t, runtime); len(remaining) != maxContextStateStages+1 {
		t.Fatalf("the refused lease removed stages: %d remain", len(remaining))
	}
}

// Collection removes only what it proves is a stage of the directory it
// collects; everything else stays for the verification that refuses it, or
// for the component that keeps it. A proven stage that is substituted before
// its removal stays too, and the command refuses, naming it.
func TestTheCollectorLeavesWhatItCannotProve(t *testing.T) {
	ctx := context.Background()
	content := []byte("{}\n")
	for _, unproven := range []struct {
		name, stage string
		plant       func(t *testing.T, store *Store, path string)
	}{
		{"bare stage in state", plantedStageName(1, ""), func(t *testing.T, _ *Store, path string) { plantStage(t, path, content) }},
		{"symlink stage in state", plantedStageName(1, ".json"), func(t *testing.T, _ *Store, path string) {
			if err := os.Symlink("mutation.json", path); err != nil {
				t.Fatal(err)
			}
		}},
		{"oversized stage in state", plantedStageName(1, ".json"), func(t *testing.T, _ *Store, path string) {
			plantStage(t, path, bytes.Repeat([]byte{' '}, maxRecord+1))
		}},
		{"shared stage in state", plantedStageName(1, ".json"), func(t *testing.T, _ *Store, path string) {
			plantStage(t, path, content)
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"hard-linked stage in state", plantedStageName(1, ".json"), func(t *testing.T, store *Store, path string) {
			outside := filepath.Join(filepath.Dir(store.options.Root), "outside")
			plantStage(t, outside, content)
			if err := os.Link(outside, path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(unproven.name, func(t *testing.T) {
			store, _ := lifecycleFixture(t)
			path := filepath.Join(contextStatePath(store), unproven.stage)
			unproven.plant(t, store, path)
			planted, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			expectRefusal(t, collectingLease(ctx, store), "context contains unsupported state; mutation is refused")
			if kept, err := os.Lstat(path); err != nil || !os.SameFile(planted, kept) {
				t.Fatalf("the refused lease removed %s (%v)", unproven.stage, err)
			}
		})
	}
	for _, entry := range []struct {
		name, stage string
		occupy      func(path string) error
	}{
		{"directory stage under an operation", plantedStageName(1, ".json"), func(path string) error { return os.Mkdir(path, 0700) }},
		{"short bare stage under an operation", "pending-0123456789abcdef", func(path string) error { return os.WriteFile(path, content, 0600) }},
	} {
		t.Run(entry.name, func(t *testing.T) {
			store, _ := lifecycleFixture(t)
			if err := checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error {
				return tx.Operations().EnsureDirectory(ctx, "op-1")
			}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(contextStatePath(store), "operations", "op-1", entry.stage)
			if err := entry.occupy(path); err != nil {
				t.Fatal(err)
			}
			planted, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := collectingLease(ctx, store); err != nil {
				t.Fatalf("the lease refused an operation entry: %#v", diagnostics.Of(err))
			}
			if kept, err := os.Lstat(path); err != nil || !os.SameFile(planted, kept) {
				t.Fatalf("the lease removed %s (%v)", entry.stage, err)
			}
		})
	}
	t.Run("stage substituted before its removal", func(t *testing.T) {
		store, _ := lifecycleFixture(t)
		path := filepath.Join(contextStatePath(store), plantedStageName(1, ".json"))
		plantStage(t, path, content)
		var substitute os.FileInfo
		store.fail = func(point string) error {
			if point != string(checkpointBeforeStageCollection) || substitute != nil {
				return nil
			}
			replacement := filepath.Join(filepath.Dir(store.options.Root), "substitute")
			if err := os.WriteFile(replacement, content, 0600); err != nil {
				return err
			}
			if err := os.Rename(replacement, path); err != nil {
				return err
			}
			info, err := os.Lstat(path)
			substitute = info
			return err
		}
		err := collectingLease(ctx, store)
		store.fail = nil
		if substitute == nil {
			t.Fatal("the lease never reached the stage's removal")
		}
		expectRefusal(t, err, "abandoned publication stage could not be removed: "+path)
		if kept, err := os.Lstat(path); err != nil || !os.SameFile(substitute, kept) {
			t.Fatalf("the refused lease removed the substituted stage (%v)", err)
		}
	})
	for _, controller := range []struct {
		name, stage, message string
		mode                 os.FileMode
	}{
		{"shared controller stage", plantedStageName(1, ".json"), "unpublished registry stage is unsafe", 0644},
		{"bare controller stage", plantedStageName(1, ""), "controller directory contains unsupported state", 0600},
	} {
		t.Run(controller.name, func(t *testing.T) {
			store := checkpointSealedFixture(t)
			path := filepath.Join(store.options.Root, "controller", controller.stage)
			plantStage(t, path, []byte("{}\n"))
			if err := os.Chmod(path, controller.mode); err != nil {
				t.Fatal(err)
			}
			expectRefusal(t, checkpointCommit(ctx, store, ""), controller.message)
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("the refused commit removed %s (%v)", controller.stage, err)
			}
		})
	}
	t.Run("root secrets and media stages", func(t *testing.T) {
		store := mediaFixture(t)
		addMedia(t, store, "demo.iso", "image bytes", false)
		registry, err := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
		if err != nil {
			t.Fatal(err)
		}
		kept := map[string][]byte{
			filepath.Join(store.options.Root, plantedStageName(1, ".json")):                                      registry,
			filepath.Join(store.options.Root, "contexts", checkpointContext, "secrets", plantedStageName(2, "")): []byte("{\"ver"),
			filepath.Join(store.options.Root, "media", plantedStageName(3, "")):                                  []byte("{}\n"),
		}
		for path, data := range kept {
			plantStage(t, path, data)
		}
		record, found, err := checkpointRecord(ctx, store, checkpointContext)
		if err != nil || !found {
			t.Fatalf("the context has no record (%v)", err)
		}
		for _, command := range []struct {
			name string
			run  func() error
		}{
			{"commit", func() error { return checkpointCommit(ctx, store, checkpointContext) }},
			{"lease", func() error { return collectingLease(ctx, store) }},
			{"secret mutation", func() error {
				return store.MutateSecrets(ctx, secretToken(record), func(secretstore.Area) error { return nil })
			}},
		} {
			if err := command.run(); err != nil {
				t.Fatalf("the %s refused: %#v", command.name, diagnostics.Of(err))
			}
			for path, data := range kept {
				if stored, err := os.ReadFile(path); err != nil || !bytes.Equal(stored, data) {
					t.Fatalf("the %s changed %s (%v)", command.name, path, err)
				}
			}
		}
	})
}

// A cancelled command stops walking at the next entry, stage or not, so a
// large operation tree never outlasts the cancellation.
func TestACancelledCollectionStopsItsWalk(t *testing.T) {
	store, record := lifecycleFixture(t)
	root, err := store.openRoot(context.Background(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer root.file.Close()
	container, dir, err := openContext(root, record)
	if err != nil {
		t.Fatal(err)
	}
	defer container.file.Close()
	defer dir.file.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.collectContextStages(ctx, dir, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled collection walked on: %v", err)
	}
}
