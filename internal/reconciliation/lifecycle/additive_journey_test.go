package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestAContextAppliedWithoutPackagesGainsAnInstanceInASecondOperation(t *testing.T) {
	for name, initial := range map[string][]api.Object{
		"an added binding":                  nil,
		"a binding that declared no member": {addonBinding("cluster")},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAdditiveFixture(t, initial...)
			root := f.root(t)
			ranBefore := slices.Clone(f.capability.applies)
			f.publish("rev-2", addonBinding("cluster", "nmstate"))
			result, err := apply(f.harness)
			if err != nil || result.Receipt.State != "done" {
				t.Fatalf("apply = %+v (%v)", result, diagnostics.Of(err))
			}
			head, plan := headPlan(t, f.harness)
			if head.ID == root.ID || head.Predecessor != root.ID || head.Verb != reconciliation.Apply || head.State != reconciliation.OperationDone {
				t.Fatalf("the head is %+v beside the root %s", head, root.ID)
			}
			if got := blockIDs(plan); !slices.Equal(got, []string{"addon-nmstate"}) {
				t.Fatalf("the additive plan holds %v", got)
			}
			if block, _ := plan.Block("addon-nmstate"); !slices.Equal(block.Prerequisites, []string{"producer-a"}) || len(block.Dependencies) != 0 {
				t.Fatalf("the added block waits for %v and has prerequisites %v", block.Dependencies, block.Prerequisites)
			}
			if got := f.capability.applies[len(ranBefore):]; !slices.Equal(got, []string{"addon-nmstate"}) {
				t.Fatalf("the additive apply ran %v", got)
			}
			if !slices.Equal(f.binder.bound[len(f.binder.bound)-1:], []string{addonSecret}) || len(f.binder.released) != 0 {
				t.Fatalf("bound %v released %v, want the added block's Secret bound and no binding released", f.binder.bound, f.binder.released)
			}
			if len(head.Bindings) != 1 || slices.Contains(root.Bindings, head.Bindings[0]) {
				t.Fatalf("the head binds %v beside the root's %v", head.Bindings, root.Bindings)
			}
			if lent := f.capability.material; len(lent) == 0 {
				t.Fatal("the added block was lent no material")
			} else if _, held := lent[len(lent)-1][addonSecret]; !held {
				t.Fatalf("the added block was lent %v", lent[len(lent)-1])
			}
			again, err := apply(f.harness)
			if err != nil || !again.Settled || len(f.capability.applies) != len(ranBefore)+1 {
				t.Fatalf("a repeated apply = %+v (%v) after %v", again, err, f.capability.applies)
			}
			owned, err := f.service.Ownership(context.Background(), testContextName)
			if err != nil || len(owned) < 2 {
				t.Fatalf("ownership = %v (%v), want both operations' objects", owned, err)
			}
		})
	}
}

func TestADiffOverAnAdditiveLineageReportsTheInputSame(t *testing.T) {
	f := newAdditiveFixture(t)
	f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	if _, err := apply(f.harness); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Diff(context.Background(), DiffRequest{ContextName: testContextName})
	if err != nil || result.Plan.State != "same" || result.Input != "unchanged" {
		t.Fatalf("diff = %+v (%v)", result, err)
	}
}

func TestAPlanOverACompletedApplyPreviewsTheAdditiveApplyAndAnApplyPresentsIt(t *testing.T) {
	f := newAdditiveFixture(t)
	root := f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	preview, err := f.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Verb != "apply" || preview.AddsTo != root.ID || preview.Continuation || preview.Receipt.Next != "apply" || len(preview.Steps) != 1 || preview.Steps[0].ID != "addon-nmstate" {
		t.Fatalf("the preview is %+v", preview)
	}
	if _, err := apply(f.harness); err != nil {
		t.Fatal(err)
	}
	if shown := f.presenter.presented[len(f.presenter.presented)-1]; shown.AddsTo != root.ID || len(shown.Authorizations) != 0 {
		t.Fatalf("the apply presented %+v", shown)
	}
}

func TestAnInterruptedAdditiveApplyContinuesWithoutRerunningTheLineage(t *testing.T) {
	f := newAdditiveFixture(t)
	root := f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	f.capability.errorFor = map[string]error{"addon-nmstate": errors.New("seeded failure")}
	if _, err := apply(f.harness); err == nil {
		t.Fatal("the seeded failure did not fire")
	}
	failed, _ := durableOperation(t, f.harness)
	if failed.State != reconciliation.OperationFailed || failed.Predecessor != root.ID {
		t.Fatalf("the additive apply is %s beside predecessor %q", failed.State, failed.Predecessor)
	}
	f.capability.errorFor = nil
	if result, err := apply(f.harness); err != nil || result.Receipt.State != "done" || result.Receipt.Operation != failed.ID {
		t.Fatalf("continuation = %+v (%v)", result, diagnostics.Of(err))
	}
	if got := f.capability.applies; !slices.Equal(got, []string{"producer-a", "addon-nmstate", "addon-nmstate"}) {
		t.Fatalf("the applies ran %v", got)
	}
}

func TestAContinuationRefusesWhenAPrerequisiteNoLongerReadsDone(t *testing.T) {
	f := newAdditiveFixture(t)
	root := f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	f.capability.errorFor = map[string]error{"addon-nmstate": errors.New("seeded failure")}
	_, _ = apply(f.harness)
	f.capability.errorFor = nil
	lose(f.harness, root.ID+"/blocks/producer-a")
	_, err := apply(f.harness)
	reported := diagnostics.Of(err)
	if len(reported) == 0 || reported[0].Code != "lifecycle.state" || !strings.Contains(reported[0].Message, "prerequisite producer-a") || !strings.Contains(reported[0].Message, "pending") {
		t.Fatalf("continuation reported %+v (%v)", reported, err)
	}
	if len(f.capability.applies) != 2 {
		t.Fatalf("a refused continuation ran %v", f.capability.applies)
	}
}

func TestAnAdditiveRegistrationCrashedBeforeItsIndexWriteLeavesThePredecessorCurrent(t *testing.T) {
	f := newAdditiveFixture(t)
	root := f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	f.workspace.area.fail["replace index.json"] = errors.New("interrupted")
	if _, err := apply(f.harness); err == nil {
		t.Fatal("an interrupted registration reported success")
	}
	current, _ := durableOperation(t, f.harness)
	if current.ID != root.ID || current.State != reconciliation.OperationDone {
		t.Fatalf("the current operation is %s (%s), want the predecessor", current.ID, current.State)
	}
	if applied := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone); !bytes.Equal(f.workspace.evidence, applied) {
		t.Fatalf("evidence = %q, want %q", f.workspace.evidence, applied)
	}
	if slices.Contains(f.binder.released, root.Bindings[0]) || len(f.binder.released) != 1 {
		t.Fatalf("released %v, want only the binding the interrupted registration created", f.binder.released)
	}
	delete(f.workspace.area.fail, "replace index.json")
	if _, err := apply(f.harness); err != nil {
		t.Fatalf("the repeated apply: %v", diagnostics.Of(err))
	}
	if head, _ := durableOperation(t, f.harness); head.Predecessor != root.ID {
		t.Fatalf("the repeated apply registered %+v", head)
	}
}

func TestAnAdditiveApplyRefusesWhatItDoesNotAddNamingTheBlock(t *testing.T) {
	type refusal struct {
		initial   []api.Object
		next      []api.Object
		fragments []string
		prepare   func(*testing.T, *additiveFixture)
	}
	cases := map[string]refusal{
		"a changed existing instance": {
			initial: []api.Object{configuredBinding("nmstate", "stable")}, next: []api.Object{configuredBinding("nmstate", "next")},
			fragments: []string{"changes ClusterAddonBinding/binding", "spec.packages.nmstate.channel"},
		},
		"a removed member": {
			initial: []api.Object{addonBinding("cluster", "nmstate", "metallb")}, next: []api.Object{addonBinding("cluster", "nmstate")},
			fragments: []string{"changes ClusterAddonBinding/binding", "spec.packages.metallb"},
		},
		"an addition mixed with another change": {
			initial: []api.Object{serverObject("s", "v1")}, next: []api.Object{serverObject("s", "v2"), addonBinding("cluster", "nmstate")},
			fragments: []string{"adds ClusterAddonBinding/binding", "changes ArtifactServer/s"},
		},
		"a last package the existing instance would wait for": {
			initial: []api.Object{addonBinding("cluster", "nmstate")}, next: []api.Object{addonBinding("cluster", "nmstate", "last")},
			fragments: []string{"block addon-nmstate differently", "froze"},
		},
		"a lineage block that records no completion": {
			next: []api.Object{addonBinding("cluster", "nmstate")}, fragments: []string{"records no block completion", "producer-a (pending)"},
			prepare: func(t *testing.T, f *additiveFixture) {
				root, _ := durableOperation(t, f.harness)
				lose(f.harness, root.ID+"/blocks/producer-a")
			},
		},
		"producer material custody that is unproved": {
			next: []api.Object{addonBinding("cluster", "nmstate")}, fragments: []string{"addon-nmstate cannot be added", "unproved"},
			prepare: func(_ *testing.T, f *additiveFixture) { f.binder.unproved = map[string]bool{"producer-a/access": true} },
		},
		"producer material custody that is missing": {
			next: []api.Object{addonBinding("cluster", "nmstate")}, fragments: []string{"addon-nmstate cannot be added", "missing"},
			prepare: func(_ *testing.T, f *additiveFixture) { f.binder.produced = nil },
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAdditiveFixture(t, test.initial...)
			f.root(t)
			ran := len(f.capability.applies)
			f.publish("rev-2", test.next...)
			if test.prepare != nil {
				test.prepare(t, f)
			}
			f.refusedWith(t, test.fragments...)
			if len(f.capability.applies) != ran {
				t.Fatalf("a refused apply ran %v", f.capability.applies[ran:])
			}
		})
	}
}

func TestAnAddedBlockWhoseCapabilityAdmitsNoAdditionRefuses(t *testing.T) {
	f := newAdditiveFixture(t)
	f.root(t)
	f.service.capabilities = testResolver{capability: f.capability.planningCapability}
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	f.refusedWith(t, "addon-nmstate", "admits no addition")
}

func TestAnAddedBlockThatMayReadMaterialOutsideItsApplyRefuses(t *testing.T) {
	f := newAdditiveFixture(t)
	f.root(t)
	f.service.capabilities = testResolver{capability: admitsWithoutTheFact{f.capability.planningCapability}}
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	f.refusedWith(t, "addon-nmstate", "outside its own apply attempts")
}

type admitsWithoutTheFact struct{ *planningCapability }

func (admitsWithoutTheFact) AdmitAdded(reconciliation.Plan, reconciliation.Block) error { return nil }

func TestAnAddedBlockTheCapabilityRefusesNamesTheCapabilitysReason(t *testing.T) {
	f := newAdditiveFixture(t)
	f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	f.capability.planningCapability.testCapability.definitions = append(f.capability.planningCapability.testCapability.definitions, definition("extra"))
	f.refusedWith(t, "the block extra cannot be added", "outside its own apply attempts")
}

func TestADestroyRemovesBothOperationsInOneRemovalInstancesFirstAndListsTheLineage(t *testing.T) {
	f := newAdditiveFixture(t)
	root := f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	if _, err := apply(f.harness); err != nil {
		t.Fatal(err)
	}
	head, _ := durableOperation(t, f.harness)
	if _, err := destroy(f.harness); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if got := f.capability.destroys; !slices.Equal(got, []string{"addon-nmstate", "producer-a"}) {
		t.Fatalf("destroyed %v, want the instance before the cluster's blocks", got)
	}
	if shown := f.presenter.presented[len(f.presenter.presented)-1]; shown.Verb != "destroy" || !slices.Equal(shown.TakesBack, []string{root.ID, head.ID}) {
		t.Fatalf("the destroy presented %+v", shown)
	}
	removal, _ := durableOperation(t, f.harness)
	if removal.Verb != reconciliation.Destroy || removal.Source != head.ID || removal.State != reconciliation.OperationDone {
		t.Fatalf("the removal is %+v", removal)
	}
}

func TestAnUnresolvedAddedInstanceNamesTheContinuationBeforeTheDestroy(t *testing.T) {
	f := newAdditiveFixture(t)
	f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	f.capability.outcomeFor["addon-nmstate"] = Result{Outcome: reconciliation.OutcomeUnknown}
	_, err := apply(f.harness)
	reported := diagnostics.Of(err)
	if len(reported) == 0 || reported[0].Code != "lifecycle.unknown" {
		t.Fatalf("the unknown outcome reported %+v (%v)", reported, err)
	}
	f.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}}
	_, err = apply(f.harness)
	reported = diagnostics.Of(err)
	if len(reported) == 0 || !strings.Contains(reported[0].Remediation, "remove the object, then repeat bootwright apply") || strings.Contains(reported[0].Remediation, "destroy and apply again") {
		t.Fatalf("the resolution reported %+v (%v)", reported, err)
	}
}

func TestAnUnresolvedInstanceOfAPlainApplyKeepsItsRemedy(t *testing.T) {
	f := newAdditiveFixture(t, addonBinding("cluster", "nmstate"))
	f.capability.outcomeFor["addon-nmstate"] = Result{Outcome: reconciliation.OutcomeUnknown}
	_, _ = apply(f.harness)
	f.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}}
	_, err := apply(f.harness)
	reported := diagnostics.Of(err)
	if len(reported) == 0 || !strings.Contains(reported[0].Remediation, "destroy and apply again") {
		t.Fatalf("the resolution reported %+v (%v)", reported, err)
	}
}

func TestAnInterruptedAdditiveApplyIsContinuedByAnotherBuildThatFreezesTheSameBlocks(t *testing.T) {
	f := newAdditiveFixture(t)
	f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	f.capability.errorFor = map[string]error{"addon-nmstate": errors.New("seeded failure")}
	_, _ = apply(f.harness)
	f.capability.errorFor = nil
	f.service.options.Executable = Executable{Version: "v2", Commit: "fedcba9"}
	result, err := apply(f.harness)
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("continuation under another build = %+v (%v)", result, diagnostics.Of(err))
	}
}
