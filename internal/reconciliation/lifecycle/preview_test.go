package lifecycle

import (
	"context"
	"reflect"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// A fresh plan previews the very decision a fresh apply registers. A preview
// that succeeds is followed by an apply that registers exactly the plan it
// showed, and a preview that refuses reports the refusal apply makes before it
// registers anything, with the same code, message and remedy. Either way the
// preview writes, binds and locks nothing a read does not.
func TestFreshPlanAndApplyShareOneDecision(t *testing.T) {
	controlled := append([]reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
	}, nestedDefinitions()...)
	cases := []struct {
		name        string
		definitions []reconciliation.BlockDefinition
		prepare     func(*harness)
		stages      []string
		// refusal is the code both verbs report, or empty for a plan both
		// accept.
		refusal string
	}{
		{name: "a valid plan", definitions: nestedDefinitions()},
		{name: "a valid selection", definitions: nestedDefinitions(), stages: []string{"substrates"}},
		{
			name: "an unsupported shape", definitions: nestedDefinitions(), refusal: "lifecycle.unsupported",
			prepare: func(h *harness) { h.capability.unsupported = []string{"ContainerCluster/sno", "Machine/guest"} },
		},
		{
			name: "an unclaimed kind", definitions: nestedDefinitions(), refusal: "lifecycle.unsupported",
			prepare: func(h *harness) { h.service.compiler = testCompiler{state: withEnabledPlaybook()} },
		},
		{name: "a zero-block plan", refusal: "lifecycle.state"},
		{name: "a stage boundary", definitions: controlled, stages: []string{"infra-components"}, refusal: "lifecycle.stage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newPlannedHarness(t, tc.definitions)
			if tc.prepare != nil {
				tc.prepare(h)
			}
			preview, previewErr := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab", Stages: tc.stages})
			if h.workspace.mutations != 0 || h.workspace.runs != 0 || len(h.workspace.area.files) != 0 || len(h.binder.bound) != 0 {
				t.Fatal("the preview locked, bound or wrote durable state")
			}
			result, applyErr := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true, Stages: tc.stages})
			if tc.refusal != "" {
				agreeOnRefusal(t, h, tc.refusal, previewErr, applyErr)
				return
			}
			if previewErr != nil || applyErr != nil {
				t.Fatalf("preview = %v, apply = %v", previewErr, applyErr)
			}
			agreeOnPlan(t, h, preview, result)
		})
	}
}

// A completed destroy leaves the context where the next plan is fresh again,
// so the preview it offers is the same decision the next apply takes.
func TestAFreshPlanAfterADestroyRefusesAsItsApplyDoes(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.unsupported = []string{"Machine/guest"}
	mutations, files := h.workspace.mutations, len(h.workspace.area.files)
	_, previewErr := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if h.workspace.mutations != mutations || len(h.workspace.area.files) != files {
		t.Fatal("the preview wrote durable state")
	}
	_, applyErr := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(previewErr); code != "lifecycle.unsupported" {
		t.Fatalf("preview after a destroy = %q (%v)", code, previewErr)
	}
	if !reflect.DeepEqual(diagnostics.Of(previewErr), diagnostics.Of(applyErr)) {
		t.Fatalf("preview reported %+v, apply reported %+v", diagnostics.Of(previewErr), diagnostics.Of(applyErr))
	}
}

// agreeOnRefusal proves both verbs refused with identical diagnostics and that
// the apply refused before it presented, locked or registered anything.
func agreeOnRefusal(t *testing.T, h *harness, code string, previewErr, applyErr error) {
	t.Helper()
	if firstCode(applyErr) != code {
		t.Fatalf("apply refusal = %q (%v)", firstCode(applyErr), applyErr)
	}
	if !reflect.DeepEqual(diagnostics.Of(previewErr), diagnostics.Of(applyErr)) {
		t.Fatalf("preview reported %+v (%v), apply reported %+v", diagnostics.Of(previewErr), previewErr, diagnostics.Of(applyErr))
	}
	if len(h.presenter.presented) != 0 || h.workspace.mutations != 0 || len(h.workspace.area.files) != 0 || len(h.binder.bound) != 0 {
		t.Fatal("the refused apply presented, bound or registered")
	}
}

// agreeOnPlan proves the apply presented exactly the preview and froze exactly
// the blocks it showed.
func agreeOnPlan(t *testing.T, h *harness, preview *PlanResult, result *OperationResult) {
	t.Helper()
	if preview.Receipt != (Receipt{Operation: "none", Verb: "plan", State: "preview", Next: "apply"}) || preview.Verb != "apply" {
		t.Fatalf("preview = %+v", preview)
	}
	if len(h.presenter.presented) != 1 {
		t.Fatalf("apply presented %d plans", len(h.presenter.presented))
	}
	presented, shown := h.presenter.presented[0], *preview
	presented.Context, presented.Receipt, shown.Context, shown.Receipt = ContextIdentity{}, Receipt{}, ContextIdentity{}, Receipt{}
	if !reflect.DeepEqual(presented, shown) {
		t.Fatalf("apply presented %+v, plan previewed %+v", presented, shown)
	}
	store := operationstore.New(h.workspace.area, time.Now)
	frozen, err := store.ReadPlan(context.Background(), result.Receipt.Operation)
	if err != nil {
		t.Fatal(err)
	}
	registered := steps(frozen, nil)
	unmarked := make([]PlanStep, len(preview.Steps))
	for index, step := range preview.Steps {
		step.Selection, step.WaitsOn = "", ""
		unmarked[index] = step
	}
	if !reflect.DeepEqual(registered, unmarked) {
		t.Fatalf("apply registered %+v, plan previewed %+v", registered, unmarked)
	}
}

// withEnabledPlaybook is the harness's Environment with an enabled
// CustomPlaybook, an object no capability realizes.
func withEnabledPlaybook() *compilation.State {
	catalog := api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
			api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
		)),
		api.NewObject(api.CustomPlaybook, "tune", api.Value{}, api.MapValue(
			api.FieldValue{Name: "enabled", Value: api.BoolValue(true)},
		)),
	})
	return compilation.NewState(catalog, catalog, nil)
}
