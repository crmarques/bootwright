//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// removableCapabilities offers the removal of the exit plan's block, which is
// all a destroy decides from; nothing here runs, probes or observes.
type removableCapabilities struct{ testLifecycleCapabilities }

func (removableCapabilities) Resolve(kind, implementation string) (lifecycle.Capability, bool) {
	return removableCapability{}, kind == "ArtifactServer" && implementation == "artifact-server-nginx-v1"
}

type removableCapability struct{}

var errNotReached = errors.New("a refused transition reached a capability effect")

func (removableCapability) Plan(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	return lifecycle.CapabilityPlan{}, errNotReached
}

func (removableCapability) Removal(_ context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	return lifecycle.Removal{Description: "remove " + block.Object, Impacts: []string{"remove-" + block.Object}}, nil
}

func (removableCapability) Apply(context.Context, lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.Result{}, errNotReached
}

func (removableCapability) Observe(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{}, errNotReached
}

func (removableCapability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{}, errNotReached
}

func (removableCapability) Destroy(context.Context, lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.Result{}, errNotReached
}

func (removableCapability) ObserveRemoval(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{}, errNotReached
}

type bindingCustody interface {
	Bind(context.Context, custody.BindRequest) (secretstore.Binding, error)
	Release(context.Context, custody.BindingRequest) (bool, error)
}

func custodyOf(t *testing.T, services cli.Services) bindingCustody {
	t.Helper()
	bindings, ok := services.Secrets.(bindingCustody)
	if !ok {
		t.Fatal("binding custody is not composed")
	}
	return bindings
}

// failedApplyNaming registers an apply of the exit plan whose block failed,
// naming binding, under the evidence a failed apply publishes.
func failedApplyNaming(t *testing.T, repository *contextfs.Store, binding string) string {
	t.Helper()
	id := "op-" + strings.Repeat("0a", 16)
	plan := exitPlan(t)
	digest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationFailed)
	if err != nil {
		t.Fatal(err)
	}
	published, err := evidence.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	effect, state, err := reconciliation.AttemptTransition(reconciliation.OutcomeFailed)
	if err != nil {
		t.Fatal(err)
	}
	stamp := exitStamp().Format(time.RFC3339)
	operation := operationstore.Operation{
		Version: operationstore.OperationVersion, ID: id, Verb: reconciliation.Apply, Context: "alpha", Revision: "rev-1",
		InputDigest: strings.Repeat("a", 64), PlanDigest: digest, AutomationDigest: strings.Repeat("b", 64),
		Closure: exitClosure(), Bindings: []string{binding}, State: reconciliation.OperationRunning, Created: stamp, Updated: stamp,
	}
	ctx := context.Background()
	if err := repository.MutateLifecycle(ctx, "alpha", func(tx lifecycle.Transaction) error {
		store := operationstore.New(tx.Operations(), exitStamp)
		if err := tx.PublishEvidence(ctx, published); err != nil {
			return err
		}
		if err := store.Register(ctx, operation, plan); err != nil {
			return err
		}
		number, err := store.StartAttempt(ctx, id, "alpha")
		if err != nil {
			return err
		}
		if err := store.CompleteAttempt(ctx, id, "alpha", number, reconciliation.OutcomeFailed, effect, state, json.RawMessage(`{}`)); err != nil {
			return err
		}
		operation.State = reconciliation.OperationFailed
		return store.UpdateOperation(ctx, operation)
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// A failed apply whose frozen Secret binding is lost, over the real context
// store and local keyring, whether the binding was dropped from the keyring or
// the version it binds lost its part file: apply and destroy each refuse the
// same way three times, writing nothing, and name the operation, the binding
// and both exits; status offers the orphan-acknowledged deletion as its only
// next step once the binding is unlisted; a deletion without the
// acknowledgement refuses, and with it removes the context and its keyring.
func TestALostFrozenBindingLeavesOnlyTheOrphanAcknowledgedDelete(t *testing.T) {
	for _, loss := range []struct {
		name     string
		lose     func(*testing.T, cli.Services, string, string)
		why      string
		unlisted bool
	}{
		{
			name: "a binding the keyring no longer lists", why: "which the context's keyring no longer lists", unlisted: true,
			lose: func(t *testing.T, services cli.Services, _, binding string) {
				released, err := custodyOf(t, services).Release(context.Background(), custody.BindingRequest{ContextName: "alpha", BindingID: binding})
				if err != nil || !released {
					t.Fatalf("the release = %v (%v)", released, err)
				}
			},
		},
		{
			name: "a bound version whose part file is missing", why: "whose material the context's keyring cannot read: referenced secret artifact is missing",
			lose: func(t *testing.T, _ cli.Services, root, _ string) {
				parts, err := filepath.Glob(filepath.Join(root, "contexts", "alpha", "secrets", "parts", "*.enc"))
				if err != nil || len(parts) != 1 {
					t.Fatalf("the keyring holds parts %v (%v)", parts, err)
				}
				if err := os.Remove(parts[0]); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(loss.name, func(t *testing.T) {
			parent := t.TempDir()
			input, root := filepath.Join(parent, "input"), filepath.Join(parent, "state")
			if err := os.Mkdir(input, 0700); err != nil {
				t.Fatal(err)
			}
			addSecretInput(t, input, "environment.yaml", syntheticEnvironment)
			addSecretInput(t, input, "controller.yaml", serviceHost)
			addSecretInput(t, input, "secret.yaml", secretDocument("opaque", "opaque", ""))
			repository := testRepository(root)
			deps := testContextWiring(t, root)
			deps.Repository, deps.Workspace, deps.Trust = repository, repository, repository
			deps.Lifecycle = lifecycleDependencies{
				Workspace: repository, Inputs: contexts.Inputs{Repository: repository, Selection: deps.Selection},
				Host: testLifecycleHost{}, Guard: testLifecycleGuard{}, Selection: deps.Selection,
				Presenter: testLifecyclePresenter{}, Confirmer: deps.Confirmer, Capabilities: removableCapabilities{},
			}
			services := assembleServices(deps)
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
			contextRun(t, services, 0, "secret", "encryption", "init")
			contextRun(t, services, 0, "secret", "set", "--name", "opaque", "--value-file", addSecretInput(t, t.TempDir(), "value", "synthetic-opaque-canary"))
			bound, err := custodyOf(t, services).Bind(context.Background(), custody.BindRequest{ContextName: "alpha", Names: []string{"opaque"}})
			if err != nil {
				t.Fatal(err)
			}
			applied := failedApplyNaming(t, repository, bound.ID)
			loss.lose(t, services, root, bound.ID)
			before := stateFingerprint(t, root)
			for _, verb := range []string{"apply", "destroy"} {
				var first string
				for attempt := 1; attempt <= 3; attempt++ {
					_, stderr := contextRun(t, services, 1, verb, "--context", "alpha", "--yes")
					if attempt == 1 {
						first = stderr
					} else if stderr != first {
						t.Fatalf("the %s refused\n%s\nthen\n%s", verb, first, stderr)
					}
				}
				for _, want := range []string{"lifecycle.state", "the apply " + applied + " (failed) froze the Secret binding " + bound.ID + ", " + loss.why + ";",
					"ArtifactServer/alpha", "restore the context's keyring from a complete backup", "bootwright context delete --name alpha --purge --allow-orphans"} {
					if !strings.Contains(first, want) {
						t.Fatalf("the %s refusal does not name %q:\n%s", verb, want, first)
					}
				}
				if !sameFingerprints(before, stateFingerprint(t, root)) {
					t.Fatalf("the refused %s wrote state", verb)
				}
			}
			status := secretResult(t, services, 0, "status", "--context", "alpha")
			var steps []string
			if err := json.Unmarshal(status["nextSteps"], &steps); err != nil {
				t.Fatal(err)
			}
			if exit := []string{"bootwright context delete --name alpha --purge --allow-orphans"}; slices.Equal(steps, exit) != loss.unlisted {
				t.Fatalf("status offers %v", steps)
			}
			if loss.unlisted != strings.Contains(string(status["contradictions"]), "froze the Secret binding "+bound.ID+", which the context's keyring no longer lists") {
				t.Fatalf("status names %s", status["contradictions"])
			}
			_, stderr := contextRun(t, services, 1, "context", "delete", "--name", "alpha", "--purge", "--yes")
			if !strings.Contains(stderr, "--allow-orphans") {
				t.Fatalf("the unacknowledged deletion refused with %s", stderr)
			}
			contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--allow-orphans", "--yes")
			registry, err := repository.View(context.Background())
			if err != nil || len(registry.Contexts) != 0 {
				t.Fatalf("the registry still holds %+v (%v)", registry.Contexts, err)
			}
			if _, err := os.Stat(filepath.Join(root, "contexts", "alpha")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the deleted context and its keyring remain (%v)", err)
			}
		})
	}
}
