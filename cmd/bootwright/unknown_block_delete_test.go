//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
)

// publishCompletedSetup publishes a controller setup that completed, so host
// reservations have a controller record to live in, as a host that ran
// bootwright setup carries.
func publishCompletedSetup(t *testing.T, repository *contextfs.Store) {
	t.Helper()
	host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, "0123456789abcdef0123456789abcdef", "12345678-1234-5678-9abc-def012345678", "fedcba98-7654-3210-fedc-ba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	value := prerequisites.HostState{Host: host, Bindings: []prerequisites.ControllerBinding{}, RetainedSources: []prerequisites.DependencySource{}, Receipt: prerequisites.SetupReceipt{
		ID: "setup-" + strings.Repeat("1", 32), CatalogDigest: strings.Repeat("a", 64),
		Egress: prerequisites.SetupEgress{NoProxy: []string{}}, Sources: []prerequisites.DependencySource{},
		Actions: []prerequisites.SetupAction{{ID: "baseline-bundle", Request: []byte(`{"dependency":"synthetic-python"}`), Phase: "planned", Evidence: []byte(`{}`)}}, Status: "pending",
	}}
	if value.Receipt.PlanDigest, err = prerequisites.SetupPlanDigest(value.Host, value.Receipt); err != nil {
		t.Fatal(err)
	}
	value.Receipt.Status = "complete"
	value.Receipt.Actions[0].Phase, value.Receipt.Actions[0].Outcome, value.Receipt.Actions[0].Evidence = "observed", "unchanged", []byte(`{"ready":true}`)
	if err := repository.MutateController(context.Background(), prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
		_, err := tx.Publish(context.Background(), value)
		return err
	}); err != nil {
		t.Fatalf("the controller setup was not published: %+v", diagnostics.Of(err))
	}
}

func reserveFor(t *testing.T, repository *contextfs.Store, name string, keys ...string) {
	t.Helper()
	if err := repository.MutateLifecycle(context.Background(), name, func(tx lifecycle.Transaction) error {
		return tx.Reserve(context.Background(), []prerequisites.HostReservation{{Context: name, Kind: "artifact-server", Service: name, Keys: keys}})
	}); err != nil {
		t.Fatalf("reserving for %s: %+v", name, diagnostics.Of(err))
	}
}

// registerUnknownApply registers exitPlan as an apply whose one block's
// attempt left its outcome unknown, with the evidence that state requires.
func registerUnknownApply(t *testing.T, repository *contextfs.Store) {
	t.Helper()
	id := "op-" + strings.Repeat("0b", 16)
	plan := exitPlan(t)
	digest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationUnknown)
	if err != nil {
		t.Fatal(err)
	}
	published, err := evidence.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	stamp := exitStamp().Format(time.RFC3339)
	operation := operationstore.Operation{
		Version: operationstore.OperationVersion, ID: id, Verb: reconciliation.Apply, Context: "alpha", Revision: "rev-1",
		InputDigest: strings.Repeat("a", 64), PlanDigest: digest, AutomationDigest: strings.Repeat("b", 64),
		Closure: exitClosure(), Bindings: []string{}, State: reconciliation.OperationRunning, Created: stamp, Updated: stamp,
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
		if err := store.CompleteAttempt(ctx, id, "alpha", number, reconciliation.OutcomeUnknown, reconciliation.EffectUnknown, reconciliation.BlockUnknown, json.RawMessage(`{}`)); err != nil {
			return err
		}
		operation.State = reconciliation.OperationUnknown
		return store.UpdateOperation(ctx, operation)
	}); err != nil {
		t.Fatalf("registering the unknown apply: %+v", diagnostics.Of(err))
	}
}

func heldReservations(t *testing.T, repository *contextfs.Store) []prerequisites.HostReservation {
	t.Helper()
	var held []prerequisites.HostReservation
	if err := repository.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		held = slices.Clone(view.State.Reservations)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return held
}

// A context holding an apply whose block stayed unknown, beside host
// reservations in the controller record, is named unresolved by status. Its
// deletion refuses without the orphan acknowledgement and releases nothing;
// with it, the deletion reports and releases exactly that context's
// reservations, and another context's stay held.
func TestDeletingAContextHoldingAnUnknownBlockReleasesItsReservations(t *testing.T) {
	services, repository, input, _ := contextFixture(t)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "context", "init", "--name", "bravo", "--input-dir", input)
	publishCompletedSetup(t, repository)
	reserveFor(t, repository, "alpha", "socket:192.0.2.1:8080", "unit:bootwright-artifact-alpha")
	reserveFor(t, repository, "bravo", "socket:192.0.2.1:8081")
	registerUnknownApply(t, repository)
	before := heldReservations(t, repository)
	if len(before) != 2 {
		t.Fatalf("the controller record holds %+v", before)
	}
	if out, _ := contextRun(t, services, 0, "status", "--context", "alpha"); !strings.Contains(out, "Unresolved alpha") {
		t.Fatalf("status names no unresolved block:\n%s", out)
	}
	_, stderr := contextRun(t, services, 1, "context", "delete", "--name", "alpha", "--purge", "--yes")
	if !strings.Contains(stderr, "--allow-orphans") {
		t.Fatalf("the unacknowledged deletion refused with %s", stderr)
	}
	if held := heldReservations(t, repository); !slices.EqualFunc(held, before, func(x, y prerequisites.HostReservation) bool {
		return x.Context == y.Context && slices.Equal(x.Keys, y.Keys)
	}) {
		t.Fatalf("the refused deletion changed the reservations %+v to %+v", before, held)
	}
	out, _ := contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--allow-orphans", "--yes")
	if !strings.Contains(out, "socket:192.0.2.1:8080, unit:bootwright-artifact-alpha") {
		t.Fatalf("the deletion reported no released reservation:\n%s", out)
	}
	held := heldReservations(t, repository)
	if len(held) != 1 || held[0].Context != "bravo" || !slices.Equal(held[0].Keys, []string{"socket:192.0.2.1:8081"}) {
		t.Fatalf("after the deletion the controller record holds %+v", held)
	}
}
