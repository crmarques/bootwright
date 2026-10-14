//go:build linux && amd64

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/desiredstate"
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
	registerDoneUnder(ctx, t, store, "rev-1", strings.Repeat("a", 64), id, source, plan, runs...)
}

// registerDoneUnder is registerDone over the input revision and input digest
// the operation froze.
func registerDoneUnder(ctx context.Context, t *testing.T, store *operationstore.Store, revision, input, id, source string, plan reconciliation.Plan, runs ...string) {
	t.Helper()
	digest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	stamp := exitStamp().Format(time.RFC3339)
	operation := operationstore.Operation{
		Version: operationstore.OperationVersion, ID: id, Verb: plan.Verb, Context: "alpha", Revision: revision,
		InputDigest: input, PlanDigest: digest, AutomationDigest: strings.Repeat("b", 64),
		Closure: exitClosure(), Source: source, Bindings: []string{}, State: reconciliation.OperationRunning, Created: stamp, Updated: stamp,
	}
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	for _, block := range runs {
		number, err := store.StartAttempt(ctx, id, block, exitBuild())
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
	return &operationstore.Closure{Digest: strings.Repeat("d", 64), Python: "3.13", Ansible: "2.21.4"}
}

// exitBuild is the build each attempt these records start names.
func exitBuild() operationstore.Build {
	return operationstore.Build{AutomationDigest: strings.Repeat("b", 64), ContentDigest: strings.Repeat("e", 64)}
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
// operation, admits neither verb. Both refusals name the deletion the context
// guard admits, and that command, run exactly as named, removes the context:
// a plain one over evidence it reads as pristine, and the orphan
// acknowledgement over any other, which over evidence it cannot read abandons
// what it cannot list.
func TestTheDeletionARefusalNamesOverUnrecognizedEvidenceRuns(t *testing.T) {
	acknowledged := []string{"context", "delete", "--name", "alpha", "--purge", "--allow-orphans"}
	unrecognized := "the mutation evidence reads an unrecognized record"
	for name, test := range map[string]struct {
		evidence string
		named    []string
		entry    string
	}{
		"respelled protected evidence": {`{"ownership":"retained","operation":"applied","version":1}`, acknowledged, unrecognized},
		"respelled pristine evidence":  {`{"version":1,"operation":"none","ownership":"none"}`, []string{"context", "delete", "--name", "alpha", "--purge"}, unrecognized},
		"corrupt evidence":             {`{`, acknowledged, unrecognized},
		"unsupported evidence":         {`{"version":2,"operation":"none","ownership":"none"}` + "\n", acknowledged, unrecognized},
		"empty evidence":               {``, acknowledged, "the mutation evidence is absent or empty"},
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
				if !strings.Contains(stderr, "lifecycle.state") || !strings.Contains(stderr, test.entry) ||
					!slices.Equal(named, test.named) || strings.Contains(stderr, "restore the whole store") {
					t.Fatalf("the %s refused with %s", verb, stderr)
				}
			}
			out, _ := contextRun(t, services, 0, append(slices.Clone(test.named), "--yes")...)
			if abandoned := strings.Contains(out, "Orphans abandoned  true"); abandoned != slices.Contains(test.named, "--allow-orphans") {
				t.Fatalf("the deletion reported abandonment %t: %s", abandoned, out)
			}
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

// Changed input over a completed apply is published with its warning, and the
// next apply then refuses naming the destroy it needs, registers no operation
// and changes nothing.
func TestAnUpdateOverACompletedApplyWarnsAndTheNextApplyRefuses(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	ctx := context.Background()
	if err := repository.MutateLifecycle(ctx, "alpha", func(tx lifecycle.Transaction) error {
		registerDoneUnder(ctx, t, operationstore.New(tx.Operations(), exitStamp), tx.Identity().Revision, inputDigestOf(tx.Inputs()),
			"op-"+strings.Repeat("01", 16), "", exitPlan(t), "alpha")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mutation := filepath.Join(root, "contexts", "alpha", "state", "mutation.json")
	if err := os.WriteFile(mutation, []byte(`{"version":1,"operation":"applied","ownership":"retained"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input, "environment.yaml"), []byte(strings.ReplaceAll(syntheticEnvironment, "example.test", "changed.test")), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr := contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", input, "--yes")
	if !strings.HasPrefix(out, "[OK] Context updated\n") || !strings.HasPrefix(stderr, "[WARN] lifecycle.state: context alpha holds a completed apply") {
		t.Fatalf("the update over a completed apply: %s %q", out, stderr)
	}
	before := stateFingerprint(t, root)
	_, stderr = contextRun(t, services, 1, "apply", "--context", "alpha", "--yes")
	const named = "the desired state changed after this apply completed: it changes Environment/synthetic (spec.domains.base, spec.domains.clusters and 3 more fields), " +
		"Machine/service-host (spec.network.addresses[fqdn].address)"
	const remedy = "take back what it owns with bootwright destroy --context alpha, then run bootwright apply --context alpha; bootwright diff --context alpha shows each change"
	if !strings.Contains(stderr, "lifecycle.state") || !strings.Contains(stderr, named) || !strings.Contains(stderr, remedy) ||
		!sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatalf("the apply over the changed input refused with %s", stderr)
	}
	for _, command := range regexp.MustCompile(`bootwright [a-z]+ --context alpha`).FindAllString(remedy, -1) {
		contextRun(t, services, 0, append(strings.Fields(command)[1:], "--help")...)
	}
	contextRun(t, services, 2, "diffs", "--context", "alpha", "--help")
	if !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatal("parsing the remedy's commands changed the context")
	}
}

// inputDigestOf is the input digest an apply records over sources: the
// reconciliation.input-v1 hash of each file's path and content digest.
func inputDigestOf(sources desiredstate.Sources) string {
	var parts []string
	for _, file := range append(slices.Clone(sources.Files), sources.Markers...) {
		content := sha256.Sum256(file.Bytes())
		parts = append(parts, file.Path()+"\x00"+hex.EncodeToString(content[:]))
	}
	slices.Sort(parts)
	digest := sha256.Sum256([]byte("bootwright.reconciliation.input-v1\x00" + strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
