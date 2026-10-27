package lifecycle

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func untilDefinitions() []reconciliation.BlockDefinition {
	definitions := []reconciliation.BlockDefinition{
		stagedDefinition("artifacts", reconciliation.StageInfrastructureServices),
		stagedDefinition("cluster", reconciliation.StageClusters),
		stagedDefinition("network", reconciliation.StageAddOns, "cluster"),
		stagedDefinition("first", reconciliation.StageAddOns, "cluster"),
		stagedDefinition("second", reconciliation.StageAddOns, "cluster", "first"),
		stagedDefinition("third", reconciliation.StageAddOns, "cluster"),
	}
	definitions[2].Object = "lab"
	for i := 3; i < len(definitions); i++ {
		definitions[i].Object = "lab." + definitions[i].ID
	}
	return definitions
}

// --until starts only the named add-on instance and what it requires, pauses,
// and a later apply without it continues the same operation to completion.
func TestApplyUntilAnAddOnPausesAndALaterApplyResumes(t *testing.T) {
	h := newPlannedHarness(t, untilDefinitions())
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab", Until: "lab.second"})
	if err != nil {
		t.Fatal(err)
	}
	marks := map[string]string{}
	for _, step := range preview.Steps {
		marks[step.ID] = step.Selection
	}
	if preview.Until != "lab.second" || marks["cluster"] != StepStart || marks["artifacts"] != StepNotSelected || marks["third"] != StepNotSelected || marks["second"] != StepWaiting {
		t.Fatalf("preview = %q %v", preview.Until, marks)
	}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true, Until: "lab.second"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.State != string(reconciliation.OperationPaused) || result.Receipt.Next != "continue-apply" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.applies, []string{"cluster", "first", "second"}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
	operation := result.Receipt.Operation
	_, err = h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true, Until: "lab.second"})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.stage" || !strings.HasSuffix(reported[0].Remediation, " without --until") {
		t.Fatalf("a done instance refused with %+v", reported)
	}
	result, err = h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Operation != operation || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("resume receipt = %+v, want operation %s done", result.Receipt, operation)
	}
	if !slices.Equal(h.capability.applies, []string{"cluster", "first", "second", "artifacts", "network", "third"}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
}

func TestApplyUntilRefusesBeforeAnyEffect(t *testing.T) {
	for _, tc := range []struct {
		name, until, message, remedy string
		stages                       []string
	}{
		{"unknown instance", "lab.missing", "the plan has no add-on instance named by --until", "name one of: lab.first, lab.second, lab.third", nil},
		{"not an add-on instance", "artifacts", "the plan has no add-on instance named by --until", "name one of: lab.first, lab.second, lab.third", nil},
		{"an add-ons block that is no instance", "lab", "the plan has no add-on instance named by --until", "name one of: lab.first, lab.second, lab.third", nil},
		{"stages admit nothing", "lab.second", "the selected stages have nothing to start", "repeat bootwright apply --context lab --stage clusters,add-ons --until lab.second", []string{"add-ons"}},
		{"stages admit nothing it requires", "lab.first", "the selected stages have nothing to start", "repeat bootwright apply --context lab --stage infrastructure-services,clusters --until lab.first", []string{"infrastructure-services"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newPlannedHarness(t, untilDefinitions())
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true, Until: tc.until, Stages: tc.stages})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.stage" || reported[0].Message != tc.message || reported[0].Remediation != tc.remedy {
				t.Fatalf("refusal = %+v", reported)
			}
			if len(h.capability.applies) != 0 || len(h.workspace.area.files) != 0 || h.workspace.mutations != 0 {
				t.Fatal("a refused --until registered an operation or started a block")
			}
		})
	}
}

// A failed block the named instance requires, in a stage the selection
// leaves out, is retried by widening --stage and keeping --until.
func TestApplyUntilWidensTheStageOfAFailedBlockItRequires(t *testing.T) {
	h := newPlannedHarness(t, untilDefinitions())
	failApply(t, h, "cluster")
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true, Stages: []string{"add-ons"}, Until: "lab.second"})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.stage" || reported[0].Message != "the block this operation must retry is outside the selected stages" ||
		reported[0].Remediation != "repeat bootwright apply --context lab --stage clusters,add-ons --until lab.second" {
		t.Fatalf("refusal = %+v", reported)
	}
}
