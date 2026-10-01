//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
)

func exitStamp() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }

func exitPlan(t *testing.T) reconciliation.Plan {
	t.Helper()
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{{
		ID: "alpha", Description: "serve alpha", Stage: reconciliation.StageInfraComponents,
		Kind: "ArtifactServer", Object: "alpha", Implementation: "artifact-server-nginx-v1",
		ContentDigest: strings.Repeat("c", 64), Request: json.RawMessage(`{"name":"alpha"}`),
		Groups: []reconciliation.Group{{ID: "pull-image", Description: "acquire the pinned server image", Machines: []string{"service-host"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// registerDone registers plan as operation id, completes each block that
// runs, and records the operation done.
func registerDone(ctx context.Context, t *testing.T, store *operationstore.Store, id, source string, plan reconciliation.Plan, runs ...string) {
	t.Helper()
	digest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	stamp := exitStamp().Format(time.RFC3339)
	operation := operationstore.Operation{
		Version: operationstore.OperationVersion, ID: id, Verb: plan.Verb, Context: "alpha", Revision: "rev-1",
		InputDigest: strings.Repeat("a", 64), PlanDigest: digest, AutomationDigest: strings.Repeat("b", 64),
		Closure: exitClosure(), Source: source, Bindings: []string{}, State: reconciliation.OperationRunning, Created: stamp, Updated: stamp,
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	for _, block := range runs {
		number, err := store.StartAttempt(ctx, id, block)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.CompleteAttempt(ctx, id, block, number, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone, json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	operation.State = reconciliation.OperationDone
	if err := store.UpdateOperation(ctx, operation); err != nil {
		t.Fatal(err)
	}
}

// exitClosure is the execution closure a registration of this build freezes;
// no verb here continues an operation, so none compares it.
func exitClosure() *operationstore.Closure {
	return &operationstore.Closure{Digest: strings.Repeat("d", 64), Python: "3.13.15", Ansible: "2.21.4"}
}

func writeRecords(t *testing.T, repository *contextfs.Store, write func(context.Context, *operationstore.Store)) {
	t.Helper()
	ctx := context.Background()
	if err := repository.MutateLifecycle(ctx, "alpha", func(tx lifecycle.Transaction) error {
		write(ctx, operationstore.New(tx.Operations(), exitStamp))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A lost index beside operation directories that list block records, and a
// completed destroy holding a block that is not done, admit neither verb. Where
// the context's evidence is pristine, both refusals name a plain deletion as
// the exit, and that deletion, with no orphan acknowledgement, removes the
// context.
func TestAPlainDeleteClearsTheRefusedRecordStatesBesidePristineEvidence(t *testing.T) {
	applied, removal := "op-"+strings.Repeat("01", 16), "op-"+strings.Repeat("02", 16)
	for name, arrange := range map[string]func(*testing.T, *contextfs.Store, string){
		"a lost index beside a started operation directory": func(t *testing.T, repository *contextfs.Store, root string) {
			writeRecords(t, repository, func(ctx context.Context, store *operationstore.Store) {
				registerDone(ctx, t, store, applied, "", exitPlan(t), "alpha")
			})
			if err := os.Remove(filepath.Join(root, "contexts", "alpha", "state", "operations", "index.json")); err != nil {
				t.Fatal(err)
			}
		},
		"a completed destroy holding a block that is not done": func(t *testing.T, repository *contextfs.Store, _ string) {
			writeRecords(t, repository, func(ctx context.Context, store *operationstore.Store) {
				plan := exitPlan(t)
				registerDone(ctx, t, store, applied, "", plan, "alpha")
				inverse, err := plan.Inverse()
				if err != nil {
					t.Fatal(err)
				}
				registerDone(ctx, t, store, removal, applied, inverse)
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			services, repository, input, root := contextFixture(t)
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
			arrange(t, repository, root)
			mutation, err := os.ReadFile(filepath.Join(root, "contexts", "alpha", "state", "mutation.json"))
			if err != nil || !strings.Contains(string(mutation), `"operation":"none","ownership":"none"`) {
				t.Fatalf("the evidence is not pristine: %s (%v)", mutation, err)
			}
			for _, verb := range []string{"apply", "destroy"} {
				_, stderr := contextRun(t, services, 1, verb, "--context", "alpha", "--yes")
				if !strings.Contains(stderr, "lifecycle.state") || !strings.Contains(stderr, "bootwright context delete --name alpha --purge") ||
					strings.Contains(stderr, "--allow-orphans") {
					t.Fatalf("the %s refused with %s", verb, stderr)
				}
			}
			contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--yes")
			registry, err := repository.View(context.Background())
			if err != nil || len(registry.Contexts) != 0 {
				t.Fatalf("the registry still holds %+v (%v)", registry.Contexts, err)
			}
			if _, err := os.Stat(filepath.Join(root, "contexts", "alpha")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the deleted context remains (%v)", err)
			}
		})
	}
}

var namedDeletion = regexp.MustCompile(`bootwright (context delete[^,\n]*)`)

// Evidence spelled otherwise than this build publishes it, beside no
// operation, admits neither verb. Where the context guard still reads it, both
// refusals name the deletion the guard admits, and that command, run exactly as
// named, removes the context. Evidence the guard cannot read admits no
// deletion, acknowledged or not, so both refusals name none, and the
// acknowledged deletion refuses and leaves the context in place.
func TestTheDeletionARefusalNamesOverUnrecognizedEvidenceRuns(t *testing.T) {
	for name, test := range map[string]struct {
		evidence string
		named    []string
	}{
		"respelled protected evidence": {`{"ownership":"retained","operation":"applied","version":1}`, []string{"context", "delete", "--name", "alpha", "--purge", "--allow-orphans"}},
		"respelled pristine evidence":  {`{"version":1,"operation":"none","ownership":"none"}`, []string{"context", "delete", "--name", "alpha", "--purge"}},
		"corrupt evidence":             {`{`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			services, repository, input, root := contextFixture(t)
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
			if err := os.WriteFile(filepath.Join(root, "contexts", "alpha", "state", "mutation.json"), []byte(test.evidence), 0600); err != nil {
				t.Fatal(err)
			}
			for _, verb := range []string{"apply", "destroy"} {
				_, stderr := contextRun(t, services, 1, verb, "--context", "alpha", "--yes")
				var named []string
				if found := namedDeletion.FindStringSubmatch(stderr); found != nil {
					named = strings.Fields(found[1])
				}
				if !strings.Contains(stderr, "lifecycle.state") || !strings.Contains(stderr, "the mutation evidence reads an unrecognized record") ||
					!slices.Equal(named, test.named) || (named == nil) != strings.Contains(stderr, "restore the whole store from a matching backup") {
					t.Fatalf("the %s refused with %s", verb, stderr)
				}
			}
			if test.named == nil {
				contextRun(t, services, 1, "context", "delete", "--name", "alpha", "--purge", "--allow-orphans", "--yes")
				registry, err := repository.View(context.Background())
				if err != nil || len(registry.Contexts) != 1 {
					t.Fatalf("the refused deletion left %+v (%v)", registry.Contexts, err)
				}
				return
			}
			contextRun(t, services, 0, append(slices.Clone(test.named), "--yes")...)
			registry, err := repository.View(context.Background())
			if err != nil || len(registry.Contexts) != 0 {
				t.Fatalf("the registry still holds %+v (%v)", registry.Contexts, err)
			}
			if _, err := os.Stat(filepath.Join(root, "contexts", "alpha")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the deleted context remains (%v)", err)
			}
		})
	}
}
