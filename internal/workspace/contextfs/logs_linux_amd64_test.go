//go:build linux && amd64

package contextfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore/areacontract"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func TestTheLogAreasHonourTheAreaContract(t *testing.T) {
	t.Run("operation logs", func(t *testing.T) {
		areacontract.Verify(t, func(t *testing.T, use func(operationstore.Area)) {
			store, _ := lifecycleFixture(t)
			if err := store.MutateLifecycle(context.Background(), "example", func(tx lifecycle.Transaction) error {
				use(tx.Logs())
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	})
	t.Run("runs", func(t *testing.T) {
		areacontract.Verify(t, func(t *testing.T, use func(operationstore.Area)) {
			store, _ := lifecycleFixture(t)
			if err := store.RunLifecycle(context.Background(), "example", func(view lifecycle.RunView) error {
				use(view.Runs())
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	})
}

func contextLogsPath(store *Store, name string) string {
	return filepath.Join(store.options.Root+logsSuffix, logContexts, name)
}

func appendOperationLog(store *Store) error {
	ctx := context.Background()
	return store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Logs().Append(ctx, "op-one/operation.jsonl", []byte("{}\n"))
	})
}

// The first log write creates the log root and every directory beneath it
// private; an existing one that is a link or holds another mode refuses
// context.state naming its path, and nothing repairs it.
func TestTheLogRootIsCreatedPrivateAndRefusesALinkOrForeignMode(t *testing.T) {
	store, _ := lifecycleFixture(t)
	if err := appendOperationLog(store); err != nil {
		t.Fatalf("the first log write failed: %#v", diagnostics.Of(err))
	}
	logs := store.options.Root + logsSuffix
	for _, path := range []string{logs, filepath.Join(logs, logContexts), contextLogsPath(store, "example"), filepath.Join(contextLogsPath(store, "example"), operationLogs, "op-one")} {
		if info, err := os.Lstat(path); err != nil || info.Mode() != os.ModeDir|0700 {
			t.Fatalf("%s is %v (%v), want a 0700 directory", path, info.Mode(), err)
		}
	}
	if info, err := os.Lstat(filepath.Join(contextLogsPath(store, "example"), operationLogs, "op-one", "operation.jsonl")); err != nil || info.Mode() != 0600 {
		t.Fatalf("the log file is %v (%v), want 0600", info.Mode(), err)
	}
	refuses := func(t *testing.T, store *Store, path string) {
		t.Helper()
		reported := diagnostics.Of(appendOperationLog(store))
		if len(reported) != 1 || reported[0].Code != "context.state" || !strings.Contains(reported[0].Message, "the log directory "+path+" ") || !strings.Contains(reported[0].Remediation, path) {
			t.Fatalf("an unsafe log directory reported %#v, want the refusal naming %s", reported, path)
		}
	}
	t.Run("a log root that is a link", func(t *testing.T) {
		store, _ := lifecycleFixture(t)
		target := t.TempDir()
		if err := os.Symlink(target, store.options.Root+logsSuffix); err != nil {
			t.Fatal(err)
		}
		refuses(t, store, store.options.Root+logsSuffix)
		if entries, _ := os.ReadDir(target); len(entries) != 0 {
			t.Fatalf("the refused log root wrote %v through its link", entries)
		}
	})
	t.Run("a log root of another mode", func(t *testing.T) {
		store, _ := lifecycleFixture(t)
		mkdirs(t, 0700, store.options.Root+logsSuffix)
		if err := os.Chmod(store.options.Root+logsSuffix, 0755); err != nil {
			t.Fatal(err)
		}
		refuses(t, store, store.options.Root+logsSuffix)
		if info, _ := os.Lstat(store.options.Root + logsSuffix); info.Mode() != os.ModeDir|0755 {
			t.Fatalf("the refused log root was repaired to %v", info.Mode())
		}
	})
	t.Run("a context log directory of another mode", func(t *testing.T) {
		store, _ := lifecycleFixture(t)
		mkdirs(t, 0700, contextLogsPath(store, "example"))
		if err := os.Chmod(contextLogsPath(store, "example"), 0750); err != nil {
			t.Fatal(err)
		}
		refuses(t, store, contextLogsPath(store, "example"))
	})
}

func registerOperation(t *testing.T, store *Store, id string) {
	t.Helper()
	ctx := context.Background()
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{{
		ID: "alpha", Description: "serve alpha", Stage: reconciliation.StageInfrastructureServices,
		Kind: "ArtifactServer", Object: "alpha", Implementation: "artifact-server-nginx-v1",
		ContentDigest: strings.Repeat("a", 64), Request: json.RawMessage(`{"name":"alpha"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC) }
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		operations := operationstore.New(tx.Operations(), tx.Logs(), clock)
		if _, err := operations.Index(ctx); err != nil {
			return err
		}
		operation := operationstore.Operation{
			Version: operationstore.OperationVersion, ID: id, Verb: plan.Verb, Context: "example", Revision: "rev-" + strings.Repeat("ef", 16),
			InputDigest: strings.Repeat("1", 64), PlanDigest: digest, AutomationDigest: strings.Repeat("2", 64),
			Executable: operationstore.Executable{Version: "devel", Commit: "abcdef1"},
			Closure:    &operationstore.Closure{Digest: strings.Repeat("3", 64), Python: "3.14", Ansible: "2.21.4"},
			Bindings:   []string{}, State: reconciliation.OperationRunning,
			Created: "2026-11-08T12:00:00Z", Updated: "2026-11-08T12:00:00Z",
		}
		if err := operations.Register(ctx, operation, plan); err != nil {
			return err
		}
		log, err := operations.OpenLog(ctx, operationstore.OperationLogPath(id))
		if err != nil {
			return err
		}
		return log.Close(ctx)
	}); err != nil {
		t.Fatalf("registering %s failed: %#v", id, diagnostics.Of(err))
	}
}

// Each registration points the context's current link at its own log
// directory. The store never reads through it: a current link planted to
// name another directory changes no read, and the next registration replaces
// it as a link without writing where it pointed.
func TestTheCurrentLinkNamesTheNewestRegistrationAndIsNeverFollowed(t *testing.T) {
	store, _ := lifecycleFixture(t)
	current := filepath.Join(contextLogsPath(store, "example"), currentLink)
	first, second, third := fmt.Sprintf("op-%032x", 1), fmt.Sprintf("op-%032x", 2), fmt.Sprintf("op-%032x", 3)
	registerOperation(t, store, first)
	if target, err := os.Readlink(current); err != nil || target != operationLogs+"/"+first {
		t.Fatalf("the current link names %q (%v), want %s", target, err, first)
	}
	registerOperation(t, store, second)
	if target, err := os.Readlink(current); err != nil || target != operationLogs+"/"+second {
		t.Fatalf("the current link names %q (%v), want %s", target, err, second)
	}
	elsewhere := t.TempDir()
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, current); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.ReadLifecycle(ctx, "example", func(view lifecycle.View) error {
		entries, err := view.Logs().Entries(ctx, "")
		if err != nil || !slices.Equal([]string{entries[0].Name, entries[1].Name}, []string{first, second}) || len(entries) != 2 {
			t.Fatalf("the log area lists %+v (%v), want the two operations", entries, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	registerOperation(t, store, third)
	if target, err := os.Readlink(current); err != nil || target != operationLogs+"/"+third {
		t.Fatalf("the current link names %q (%v), want %s", target, err, third)
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Fatalf("a registration wrote %v where a planted link pointed", entries)
	}
}

func plantOperationLog(t *testing.T, store *Store, id string, modified time.Time, size int64) {
	t.Helper()
	directory := filepath.Join(contextLogsPath(store, "example"), operationLogs, id)
	mkdirs(t, 0700, filepath.Join(directory, "blocks", "alpha"))
	writePrivate(t, filepath.Join(directory, "operation.jsonl"), []byte("{}\n"))
	output := filepath.Join(directory, "blocks", "alpha", "attempt-000001.output")
	writePrivate(t, output, nil)
	if err := os.Truncate(output, size); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(directory, modified, modified); err != nil {
		t.Fatal(err)
	}
}

// A registration removes the oldest operation log directories, by when each
// last changed, until fewer than maxOperationLogs remain and they hold at most
// half the context log bound, and never the logs of the operation the index
// named or of the one registering.
func TestOperationLogRetentionKeepsCurrentWithinCountAndBytes(t *testing.T) {
	t.Run("count", func(t *testing.T) {
		store, _ := lifecycleFixture(t)
		current := fmt.Sprintf("op-%032x", 0xc0)
		registerOperation(t, store, current)
		base := time.Now().Add(-time.Hour)
		if err := os.Chtimes(filepath.Join(contextLogsPath(store, "example"), operationLogs, current), base.Add(-time.Hour), base.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		for index := range maxOperationLogs + 6 {
			plantOperationLog(t, store, fmt.Sprintf("op-%032x", index+1), base.Add(time.Duration(index)*time.Second), 0)
		}
		registered := fmt.Sprintf("op-%032x", 0xc1)
		registerOperation(t, store, registered)
		held := heldNamesOf(t, filepath.Join(contextLogsPath(store, "example"), operationLogs))
		if len(held) != maxOperationLogs || !slices.Contains(held, current) || !slices.Contains(held, registered) || slices.Contains(held, fmt.Sprintf("op-%032x", 8)) {
			t.Fatalf("retention kept %d directories %v, want %d with the current and the registering operation and no planted one older than the newest %d", len(held), held, maxOperationLogs, maxOperationLogs-2)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		store, _ := lifecycleFixture(t)
		base := time.Now().Add(-time.Hour)
		large, small := fmt.Sprintf("op-%032x", 1), fmt.Sprintf("op-%032x", 2)
		plantOperationLog(t, store, large, base, retainedLogBytes+1)
		plantOperationLog(t, store, small, base.Add(time.Second), 1)
		registerOperation(t, store, fmt.Sprintf("op-%032x", 3))
		if held := heldNamesOf(t, filepath.Join(contextLogsPath(store, "example"), operationLogs)); slices.Contains(held, large) || !slices.Contains(held, small) {
			t.Fatalf("retention over %d bytes kept %v, want the oldest removed and the rest kept", retainedLogBytes+2, held)
		}
	})
}

func deleteExample(store *Store, record contexts.Record) error {
	ctx := context.Background()
	return store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		for _, held := range tx.Registry().Contexts {
			if held.Name == record.Name {
				record = held
			}
		}
		if record.Mode == contexts.Ready {
			if _, err := tx.MutationState(ctx, record.Name); err != nil {
				return err
			}
		}
		return tx.Delete(ctx, record)
	})
}

// Deleting a context removes its log directory, links as links, and a log
// directory holding what this store would not have created refuses the
// deletion naming that entry, leaving the registry entry for a repetition.
func TestDeletionRemovesTheContextLogDirectory(t *testing.T) {
	store, record := lifecycleFixture(t)
	registerOperation(t, store, fmt.Sprintf("op-%032x", 1))
	logs := contextLogsPath(store, "example")
	if err := os.Symlink("/etc", filepath.Join(logs, operationLogs, "elsewhere")); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(logs, "kept")
	mkdirs(t, 0700, foreign)
	if err := os.Chmod(foreign, 0755); err != nil {
		t.Fatal(err)
	}
	reported := diagnostics.Of(deleteExample(store, record))
	if len(reported) != 1 || reported[0].Code != "context.state" || !strings.Contains(reported[0].Message, foreign) {
		t.Fatalf("a log directory holding a foreign entry reported %#v, want its refusal", reported)
	}
	if err := os.Chmod(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	if err := deleteExample(store, record); err != nil {
		t.Fatalf("the repeated deletion failed: %#v", diagnostics.Of(err))
	}
	if _, err := os.Lstat(logs); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the deleted context left its log directory (%v)", err)
	}
	if _, err := os.Stat("/etc/passwd"); err != nil {
		t.Fatalf("the deletion followed a link: %v", err)
	}
}

// A context initialized under a name an earlier context left logs under starts
// with none: initialization removes the leftover directory before the context
// is ready.
func TestInitRemovesALeftoverLogDirectory(t *testing.T) {
	store, sources := fixture(t)
	leftover := filepath.Join(store.options.Root+logsSuffix, logContexts, "example", operationLogs, fmt.Sprintf("op-%032x", 1))
	mkdirs(t, 0700, leftover)
	writePrivate(t, filepath.Join(leftover, "operation.jsonl"), []byte("{}\n"))
	publish(t, store, "example", sources)
	if _, err := os.Lstat(contextLogsPath(store, "example")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("initialization left the earlier context's logs (%v)", err)
	}
}

// A log area write the root refuses is an error its caller sees, which the
// lifecycle latches as runtime.log; the store repairs nothing.
func TestALogAreaWriteFailureLatchesRuntimeLog(t *testing.T) {
	store, _ := lifecycleFixture(t)
	if err := appendOperationLog(store); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(contextLogsPath(store, "example"), operationLogs)
	if err := os.Chmod(directory, 0750); err != nil {
		t.Fatal(err)
	}
	if err := appendOperationLog(store); err == nil {
		t.Fatal("a log write beneath an unsafe directory succeeded")
	}
	var stat syscall.Stat_t
	if err := syscall.Lstat(directory, &stat); err != nil || stat.Mode&0777 != 0750 {
		t.Fatalf("the refused directory was repaired (%v)", err)
	}
}
