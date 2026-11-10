package instance

import (
	"context"
	"strings"
	"testing"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func clusterBlock(kind, object string) reconciliation.Block {
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "install-" + object, Kind: kind, Object: object}}
}

func TestAnAdditionIsAdmittedOnlyOnAClusterTheLineageInstalled(t *testing.T) {
	block := fixtureBlock(t, fixtureRequest(t))
	capability := New(nil, fixtureRegistry(t), nil)
	installed := reconciliation.Plan{Blocks: []reconciliation.Block{clusterBlock("OpenShiftCluster", "cluster")}}
	if err := capability.AdmitAdded(installed, block); err != nil {
		t.Fatalf("an instance on an installed cluster: %v", err)
	}
	for name, lineage := range map[string]reconciliation.Plan{
		"another cluster":  {Blocks: []reconciliation.Block{clusterBlock("OpenShiftCluster", "other")}},
		"another kind":     {Blocks: []reconciliation.Block{clusterBlock("Machine", "cluster")}},
		"an empty lineage": {},
	} {
		err := capability.AdmitAdded(lineage, block)
		if err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, "did not install the cluster cluster") {
			t.Errorf("%s: admitted or refused otherwise: %v", name, err)
		}
	}
	bad := block
	bad.Request = []byte(`{}`)
	if err := capability.AdmitAdded(installed, bad); err == nil {
		t.Error("an undecodable request was admitted")
	}
}

func TestAnInstanceReadsBoundSecretsOnlyInItsOwnApplyAttempts(t *testing.T) {
	request := fixtureRequest(t)
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := co.NewPlacementEntry(co.SecretPlacement{Secret: "credentials", Namespace: "fixture-operator", Name: "credentials", Type: "Opaque", Keys: []co.KeyPart{{Key: "password", Part: "password"}}})
	if err != nil {
		t.Fatal(err)
	}
	program.Steps = append(program.Steps, co.Step{Kind: "place", Place: &entry})
	if request.Program, err = co.Freeze(program, co.Options{Namespace: "fixture-operator"}); err != nil {
		t.Fatal(err)
	}
	request.Secrets = []string{"credentials"}
	execution := fixtureExecution(t, request)
	execution.Material = map[string]secrets.Material{"credentials": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PasswordPart: []byte("password")})}
	defer execution.Material["credentials"].Clear()
	runner := &scriptedRunner{}
	capability := New(runner, fixtureRegistry(t), nil)
	if !capability.ReadsMaterialOnlyInApply(execution.Block) {
		t.Fatal("the capability does not state the premise an addition requires")
	}
	ctx := context.Background()
	execution.ReadOnly = true
	_, _ = capability.Observe(ctx, execution)
	_, _ = capability.Destroy(ctx, execution)
	_, _ = capability.ObserveRemoval(ctx, execution)
	_, _ = capability.Quiescent(ctx, lifecycle.Probe{Block: execution.Block, Material: execution.Material})
	if len(runner.calls) != 1 {
		t.Fatalf("the read paths made %d adapter calls, want only the observation", len(runner.calls))
	}
	if _, lent := runner.calls[0].Material["credentials"]; lent || len(runner.calls[0].Material) != 1 {
		t.Fatalf("the observation was lent %v", runner.calls[0].Material)
	}
	execution.ReadOnly = false
	_, _ = capability.Apply(ctx, execution)
	if _, lent := runner.calls[1].Material["credentials"]; !lent {
		t.Fatal("the apply attempt was not lent the placed Secret")
	}
}

func TestAnUnresolvedAddedInstanceNamesTheContinuationNotADestroy(t *testing.T) {
	request := fixtureRequest(t)
	block := fixtureBlock(t, request)
	evidence := fixtureEvidence(t, request, block.RequestDigest)
	evidence.Observations[0].Ownership = co.Foreign
	capability := New(nil, fixtureRegistry(t), nil)
	plain, ok := capability.Unresolved(reconciliation.Apply, block, encoded(t, evidence))
	if !ok || plain.Remedy != "destroy and apply again" {
		t.Fatalf("a plain apply's instance is explained %+v", plain)
	}
	added, ok := capability.UnresolvedAdded(reconciliation.Apply, block, encoded(t, evidence), nil)
	if !ok || added.Reason != plain.Reason || strings.Contains(added.Remedy, "destroy") || added.Remedy == "" {
		t.Fatalf("an added instance is explained %+v beside %+v", added, plain)
	}
	if _, ok := capability.UnresolvedAdded(reconciliation.Destroy, block, encoded(t, evidence), nil); ok {
		t.Fatal("a removal was explained as an addition")
	}
}

func TestAnUnresolvedAddedInstanceNeverInvitesDeletingAnObjectAnotherInstanceDeclares(t *testing.T) {
	request := fixtureRequest(t)
	block := fixtureBlock(t, request)
	evidence := fixtureEvidence(t, request, block.RequestDigest)
	evidence.Observations[0].Ownership = co.Foreign
	capability := New(nil, fixtureRegistry(t), nil)
	sibling := block
	sibling.ID = "addon-sibling"
	otherCluster := fixtureRequest(t)
	otherCluster.Cluster = "elsewhere"
	otherCluster.ProducedInputs = []lifecycle.ProducedInput{{Kind: "OpenShiftCluster", Object: "elsewhere", Output: "kubeconfig"}}
	elsewhere := fixtureBlock(t, otherCluster)
	elsewhere.ID = "addon-elsewhere"
	for name, test := range map[string]struct {
		lineage []lifecycle.OwnedBlock
		holds   string
		deletes bool
	}{
		"a sibling instance of the lineage declares it": {lineage: []lifecycle.OwnedBlock{{Operation: "op-root", Block: sibling}}, holds: "op-root"},
		"the block itself is no owner":                  {lineage: []lifecycle.OwnedBlock{{Operation: "op-self", Block: block}}, deletes: true},
		"an instance of another cluster is no owner":    {lineage: []lifecycle.OwnedBlock{{Operation: "op-other", Block: elsewhere}}, deletes: true},
		"no instance of the lineage declares it":        {deletes: true},
	} {
		unresolved, ok := capability.UnresolvedAdded(reconciliation.Apply, block, encoded(t, evidence), test.lineage)
		if !ok {
			t.Fatalf("%s: nothing explained", name)
		}
		switch {
		case test.deletes && (!strings.Contains(unresolved.Remedy, "if no add-on of this context owns it") || strings.Contains(unresolved.Remedy, "op-")):
			t.Errorf("%s: the remedy reads %q", name, unresolved.Remedy)
		case !test.deletes && (!strings.Contains(unresolved.Remedy, test.holds) || strings.Contains(unresolved.Remedy, "remove") || strings.Contains(unresolved.Remedy, "destroy")):
			t.Errorf("%s: the remedy reads %q", name, unresolved.Remedy)
		}
	}
}

func TestAnUpgradeChainOLMRetentionBrokeIsNamedAndNeverAnInvitationToDestroy(t *testing.T) {
	request := fixtureRequest(t)
	block := fixtureBlock(t, request)
	capability := New(nil, fixtureRegistry(t), nil)
	collected := func(e *Evidence) {
		e.OLM[0].InstalledCSV, e.OLM[0].Gap = "fixture.v2.0.0", &co.ChainGap{Cause: co.PlanCollected, CSV: "fixture.v1.0.0"}
		e.Postcondition, e.Classification = false, "unknown"
		for i := range e.Checks {
			e.Checks[i].Satisfied = false
		}
	}
	unproved := func(e *Evidence) {
		e.OLM[0].InstalledCSV = "fixture.v2.0.0"
		e.Observations[0].Ownership, e.Observations[0].Equal = co.Unknown, false
		e.Postcondition, e.Classification = false, "unknown"
	}
	explain := func(e Evidence) []lifecycle.Unresolved {
		var out []lifecycle.Unresolved
		for _, call := range []func() (lifecycle.Unresolved, bool){
			func() (lifecycle.Unresolved, bool) {
				return capability.Unresolved(reconciliation.Apply, block, encoded(t, e))
			},
			func() (lifecycle.Unresolved, bool) {
				return capability.UnresolvedAdded(reconciliation.Apply, block, encoded(t, e), nil)
			},
		} {
			unresolved, ok := call()
			if !ok {
				t.Fatalf("no reason for %+v", e.OLM)
			}
			out = append(out, unresolved)
		}
		return out
	}
	for name, test := range map[string]struct {
		change func(*Evidence)
		reason string
	}{
		"the pinned plan was collected": {collected, "Subscription/fixture-operator/fixture reports CSV fixture.v2.0.0 in place of the pinned CSV fixture.v1.0.0, and its upgrade chain cannot be proved: the InstallPlan that installed the pinned CSV fixture.v1.0.0 no longer exists"},
		"a later plan was collected": {func(e *Evidence) {
			collected(e)
			e.OLM[0].Gap.CSV = "fixture.v1.5.0"
		}, "no InstallPlan that installed CSV fixture.v1.5.0 remains"},
		"two plans install one CSV": {func(e *Evidence) {
			collected(e)
			e.OLM[0].Gap = &co.ChainGap{Cause: co.DuplicatePlans, CSV: "fixture.v2.0.0", Plans: []string{"install-a", "install-b"}}
		}, "InstallPlans install-a, install-b each install CSV fixture.v2.0.0"},
		"more than sixteen CSVs": {func(e *Evidence) {
			collected(e)
			e.OLM[0].Gap = &co.ChainGap{Cause: co.ChainTooLong}
		}, "more than sixteen CSVs lie between CSV fixture.v1.0.0 and CSV fixture.v2.0.0"},
	} {
		e := fixtureEvidence(t, request, block.RequestDigest)
		test.change(&e)
		if effect, err := effectOf(encoded(t, e), request, block.RequestDigest); err != nil || effect != reconciliation.EffectUnknown {
			t.Errorf("%s: effect %s %v", name, effect, err)
		}
		for _, unresolved := range explain(e) {
			if !strings.Contains(unresolved.Reason, test.reason) || unresolved.Remedy != unprovedChain || strings.Contains(unresolved.Remedy, "destroy") {
				t.Errorf("%s: %+v", name, unresolved)
			}
		}
	}
	for name, change := range map[string]func(*Evidence){
		"an Automatic, other-source or replaces-mismatched link": unproved,
		"a gap beside a recorded chain": func(e *Evidence) {
			collected(e)
			e.OLM[0].Chain = []co.ChainLink{{Plan: "install-a", CSV: "fixture.v1.0.0"}, {Plan: "install-b", CSV: "fixture.v2.0.0"}}
		},
		"a gap on the pinned CSV": func(e *Evidence) {
			collected(e)
			e.OLM[0].InstalledCSV = "fixture.v1.0.0"
		},
		"a collected gap naming no CSV": func(e *Evidence) {
			collected(e)
			e.OLM[0].Gap.CSV = ""
		},
		"a duplicate gap naming one plan": func(e *Evidence) {
			collected(e)
			e.OLM[0].Gap = &co.ChainGap{Cause: co.DuplicatePlans, CSV: "fixture.v2.0.0", Plans: []string{"install-a"}}
		},
	} {
		e := fixtureEvidence(t, request, block.RequestDigest)
		change(&e)
		reasons := explain(e)
		if reasons[0].Remedy != destroyAndApply || strings.Contains(reasons[0].Reason, "cannot be proved") {
			t.Errorf("%s: %+v", name, reasons[0])
		}
	}
	e := fixtureEvidence(t, request, block.RequestDigest)
	collected(&e)
	e.Checks[0].Unknown = true
	reasons := explain(e)
	if reasons[0].Remedy != destroyAndApply || !strings.HasPrefix(reasons[0].Reason, "add-on ") || !strings.Contains(reasons[0].Reason, "; also Subscription/fixture-operator/fixture reports CSV fixture.v2.0.0 in place of the pinned CSV fixture.v1.0.0") {
		t.Errorf("a gap beside an unknown check: %+v", reasons[0])
	}
	e = fixtureEvidence(t, request, block.RequestDigest)
	collected(&e)
	e.OLM[0].Gap.Cause = "planDeleted"
	if _, ok := capability.Unresolved(reconciliation.Apply, block, encoded(t, e)); ok {
		t.Error("a gap outside the closed set of causes was read")
	}
}
