package installation

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func treeFixture(t *testing.T) (lifecycle.Execution, TreeRequest) {
	t.Helper()
	plan, err := NewTree(nil).WithMedia(labMedia()).Plan(context.Background(), planInput(reconciliation.Apply))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("tree plan: %+v (%v)", plan, diagnostics.Of(err))
	}
	definition := plan.Definitions[0]
	request, err := DecodeTreeRequest(definition.Request)
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{Context: testContext, Block: reconciliation.Block{BlockDefinition: definition, RequestDigest: strings.Repeat("1", 64)}}, request
}

func TestThreeInstallationsHaveOneSharedPublication(t *testing.T) {
	input := planInput(reconciliation.Apply)
	catalog := labCatalog(api.NewObject(api.Machine, "rhel-02", api.Value{}, guest().Spec()), api.NewObject(api.Machine, "rhel-03", api.Value{}, guest().Spec()))
	input.State = compilation.NewState(catalog, catalog, nil)
	plan, err := NewTree(nil).WithMedia(labMedia()).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("tree plan: %+v (%v)", plan, diagnostics.Of(err))
	}
	block := plan.Definitions[0]
	if block.Kind != TreeKind || block.Stage != reconciliation.StageMachines || len(block.Exclusive) != 1 {
		t.Fatalf("publication: %+v", block)
	}
	if !slices.Contains(block.Requires, reconciliation.ObjectRef{Kind: "ArtifactServer", Object: "lab-artifacts"}) {
		t.Fatal(block.Requires)
	}
	controller := false
	for _, reference := range block.Requires {
		controller = controller || reference.Kind == "Environment"
	}
	if !controller {
		t.Fatal("no controller dependency")
	}
	machines, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, machine := range machines.Definitions {
		if len(machine.Exclusive) != 0 || !slices.Contains(machine.Requires, reconciliation.ObjectRef{Kind: TreeKind, Object: block.Object}) {
			t.Fatalf("consumer: %+v", machine)
		}
	}
}

func TestTreeRequestAndEvidenceGoldens(t *testing.T) {
	execution, request := treeFixture(t)
	matchesGolden(t, "tree-request", execution.Block.Request)
	if _, err := DecodeTreeRequest(execution.Block.Request); err != nil {
		t.Fatal(err)
	}
	for name, evidence := range map[string]TreeEvidence{
		"complete": {Content: true, Tree: true, Identity: request.Media.SHA256, Postcondition: true},
		"partial":  {Staging: true}, "removal-partial": {Content: true}, "removed": {Absent: true, Postcondition: true},
	} {
		evidence.Version, evidence.Request = TreeEvidenceVersion, execution.Block.RequestDigest
		data, err := reconciliation.Freeze(evidence, "tree evidence")
		if err != nil {
			t.Fatal(err)
		}
		matchesGolden(t, "tree-evidence-"+name, data)
		if _, err := decodeTreeEvidence(data, execution.Block.RequestDigest); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTreeContinuationClassifiesPartialAndInterruptedRemoval(t *testing.T) {
	execution, request := treeFixture(t)
	for name, test := range map[string]struct {
		evidence      TreeEvidence
		apply, remove reconciliation.EffectState
	}{
		"nothing":                 {TreeEvidence{}, reconciliation.EffectNoEffect, reconciliation.EffectCompleted},
		"staging":                 {TreeEvidence{Staging: true}, reconciliation.EffectPartial, reconciliation.EffectPartial},
		"interrupted removal":     {TreeEvidence{Content: true}, reconciliation.EffectPartial, reconciliation.EffectPartial},
		"complete":                {TreeEvidence{Tree: true, Content: true, Identity: request.Media.SHA256, Postcondition: true}, reconciliation.EffectCompleted, reconciliation.EffectNoEffect},
		"absent with content":     {TreeEvidence{Absent: true, Content: true}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		"absent with staging":     {TreeEvidence{Absent: true, Staging: true}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		"complete foreign source": {TreeEvidence{Tree: true, Content: true, Identity: strings.Repeat("a", 64)}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		"malformed identity":      {TreeEvidence{Content: true, Identity: "invalid"}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		"contradictory complete":  {TreeEvidence{Tree: true, Identity: strings.Repeat("a", 64)}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		"another source":          {TreeEvidence{Content: true, Identity: strings.Repeat("a", 64)}, reconciliation.EffectUnknown, reconciliation.EffectPartial},
	} {
		t.Run(name, func(t *testing.T) {
			test.evidence.Version, test.evidence.Request = TreeEvidenceVersion, execution.Block.RequestDigest
			data, _ := json.Marshal(test.evidence)
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: data}}
			capability := NewTree(runner)
			applied, err := capability.Observe(context.Background(), execution)
			if err != nil || applied.Effect != test.apply {
				t.Fatalf("apply: %+v (%v)", applied, err)
			}
			removed, err := capability.ObserveRemoval(context.Background(), execution)
			if err != nil || removed.Effect != test.remove {
				t.Fatalf("remove: %+v (%v)", removed, err)
			}
		})
	}
}

// A tree apply refuses a source image changed after the plan froze it, and a
// tree another image published at its path, before it extracts anything, and
// each refusal reaches the operator naming the profile, what changed and the
// way back. Neither is named outside an apply.
func TestATreeApplyNamesItsRefusalsWithTheirRemedy(t *testing.T) {
	execution, request := treeFixture(t)
	execution.Context = "lab-b"
	for reason, test := range map[string]struct{ message, remediation string }{
		"media-changed-tree": {
			"the media store's image " + dvdImageName + " no longer has the size and SHA-256 this operation froze, so nothing was published or booted",
			"take this context back with bootwright destroy --context lab-b, import the image again with bootwright media add --name " +
				dvdImageName + " --from-file <path>, or --from-url <url> --sha256 <digest>, then run bootwright apply --context lab-b",
		},
		"tree-source-drift": {
			"the package tree at " + request.Publication.Path + " was extracted from another image than the " + dvdImageName +
				" this operation froze, so it was left unchanged and nothing was published",
			"take this context back with bootwright destroy --context lab-b, which removes that tree, then run bootwright apply --context lab-b",
		},
	} {
		t.Run(reason, func(t *testing.T) {
			result, err := NewTree(refusingRunner{reason: reason}).Apply(context.Background(), execution)
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Object == nil || reported[0].Object.Kind != string(api.MachineInstallProfile) ||
				reported[0].Object.Name != request.Identity.Profile {
				t.Fatalf("diagnostics = %#v (%v)", reported, err)
			}
			if reported[0].Message != test.message || reported[0].Remediation != test.remediation {
				t.Fatalf("diagnostic = %q / %q", reported[0].Message, reported[0].Remediation)
			}
			if result.Outcome == reconciliation.OutcomeChanged || result.Outcome == reconciliation.OutcomeUnchanged {
				t.Fatalf("a refused publication reports outcome %v", result.Outcome)
			}
		})
	}
	runner := &fakeRunner{}
	capability := NewTree(runner)
	_, _ = capability.Apply(context.Background(), execution)
	_, _ = capability.Destroy(context.Background(), execution)
	_, _ = capability.Observe(context.Background(), execution)
	named := map[string]int{}
	for _, sent := range runner.requests {
		named[sent.Operation] = len(sent.Refusals)
	}
	if want := map[string]int{"apply": 2, "destroy": 0, "observe": 0}; len(runner.requests) != 3 || !maps.Equal(named, want) {
		t.Fatalf("refusals named by operation = %v, want %v", named, want)
	}
}

func TestTreeOldRequestAndEvidenceVersionsRefuse(t *testing.T) {
	execution, _ := treeFixture(t)
	if _, err := DecodeTreeRequest([]byte(`{"version":"os-package-tree-v0"}`)); err == nil {
		t.Fatal("accepted old tree request")
	}
	evidence, _ := json.Marshal(TreeEvidence{Version: "os-package-tree-evidence-v0", Request: execution.Block.RequestDigest})
	if _, err := decodeTreeEvidence(evidence, execution.Block.RequestDigest); err == nil {
		t.Fatal("accepted old tree evidence")
	}
}
