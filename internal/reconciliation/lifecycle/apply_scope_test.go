package lifecycle

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestControllerApplyAuthorizesOnlyReachableDestructiveEffects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	controller := stagedDefinition("tools", reconciliation.StageController)
	firewall := stagedDefinition("firewall", reconciliation.StageController)
	controller.Dependencies = []string{firewall.ID}
	install := destructive("install")
	install.Stage = reconciliation.StageMachines
	install.Dependencies = []string{controller.ID}
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{install, controller, firewall})
	request := ApplyRequest{ContextName: testContextName, Stages: []string{"controller"}, SkipConfirmation: true, Unattended: true}
	plan, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Stages: request.Stages})
	if err != nil || plan.Startable != 1 || plan.Later != 1 || plan.Excluded != 1 || plan.Waves != 2 || len(plan.Authorizations) != 0 {
		t.Fatalf("controller preview = %+v, %v", plan, err)
	}
	request.Authorizations = []string{"data-loss"}
	if _, err := h.service.Apply(ctx, request); firstCode(err) != "authorize.unneeded" || len(h.capability.applies) != 0 {
		t.Fatalf("extra token = %v, effects = %v", err, h.capability.applies)
	}
	request.Authorizations = nil
	result, err := h.service.Apply(ctx, request)
	if err != nil || result.Receipt.State != "paused" || !slices.Equal(h.capability.applies, []string{"firewall", "tools"}) {
		t.Fatalf("controller apply = %+v, %v, effects = %v", result, err, h.capability.applies)
	}
	request.Stages = []string{"machines"}
	if _, err := h.service.Apply(ctx, request); firstCode(err) != "authorize.missing" {
		t.Fatalf("wider apply without authorization = %v", err)
	}
	plan, err = h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Stages: request.Stages})
	if err != nil || len(plan.Authorizations) != 1 || !slices.Equal(plan.AuthorizationSteps, []int{3}) {
		t.Fatalf("installation preview = %+v, %v", plan, err)
	}
	request.Authorizations = plan.Authorizations
	result, err = h.service.Apply(ctx, request)
	if err != nil || result.Receipt.State != "done" || !slices.Equal(h.capability.applies, []string{"firewall", "tools", "install"}) {
		t.Fatalf("authorized continuation = %+v, %v, effects = %v", result, err, h.capability.applies)
	}
}

func TestReachableAuthorizationIncludesLaterControllerPrecursorAndRetries(t *testing.T) {
	t.Parallel()
	firewall := stagedDefinition("firewall", reconciliation.StageInfrastructureServices)
	install := destructive("install")
	install.Stage = reconciliation.StageMachines
	install.Dependencies = []string{firewall.ID}
	controller := stagedDefinition("tools", reconciliation.StageController, install.ID)
	unrelated := destructive("unused")
	unrelated.Stage = reconciliation.StageMachines
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{controller, install, unrelated, firewall})
	if err != nil {
		t.Fatal(err)
	}
	selection, _ := reconciliation.ParseSelection([]string{"controller"}, "")
	selection, _ = selection.Bind(plan)
	for _, state := range []reconciliation.BlockState{reconciliation.BlockPending, reconciliation.BlockFailed, reconciliation.BlockUnknown} {
		states := map[string]reconciliation.BlockState{firewall.ID: reconciliation.BlockDone, install.ID: state, unrelated.ID: reconciliation.BlockUnknown}
		scoped := authorizationPlan(plan, states, selection)
		want := []string{reconciliation.AuthorizationDataLoss}
		if !slices.Equal(requiredTokens(scoped), want) || len(scoped.Blocks) != 2 || scoped.Blocks[0].ID != install.ID {
			t.Fatalf("reachable effects for %s: %+v", state, scoped.Blocks)
		}
	}
	states := map[string]reconciliation.BlockState{install.ID: reconciliation.BlockDone}
	if got := requiredTokens(authorizationPlan(plan, states, selection)); len(got) != 0 {
		t.Fatalf("completed installation consumes %v", got)
	}
}

func TestMachineTargetsKeepAuthorizationAndRecoveryWithinTheFrozenGraph(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := stagedDefinition("shared", reconciliation.StageSubstrates)
	first, second := destructive("first"), destructive("second")
	first.Stage, second.Stage = reconciliation.StageMachines, reconciliation.StageMachines
	first.Dependencies, second.Dependencies = []string{base.ID}, []string{base.ID}
	definitions := []reconciliation.BlockDefinition{base, first, second}
	for i := range definitions {
		definitions[i].Kind = "Machine"
	}
	h := newPlannedHarness(t, definitions)
	h.service.capabilities = testResolver{capability: h.capability,
		bindings: []CapabilityBinding{{Kind: "Machine", Implementation: base.Implementation}}}
	full, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName})
	if err != nil || len(full.Authorizations) != 1 {
		t.Fatalf("full preview = %+v, %v", full, err)
	}
	if _, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Machines: []string{"first"}, Stages: []string{"machines"}}); firstCode(err) != "apply.stage-empty" ||
		!strings.Contains(diagnostics.Of(err)[0].Remediation, "--stage substrates,machines --machines first") {
		t.Fatalf("blocked target remedy = %+v", diagnostics.Of(err))
	}
	request := ApplyRequest{ContextName: testContextName, Machines: []string{"first"}, SkipConfirmation: true, Unattended: true, Authorizations: full.Authorizations}
	if _, err := h.service.Apply(ctx, request); firstCode(err) != "authorize.stale" || len(h.capability.applies) != 0 {
		t.Fatalf("full grant passed a narrower invocation: %v, effects %v", err, h.capability.applies)
	}
	preview := func(target string) []string {
		plan, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Machines: []string{target}})
		if err != nil || len(plan.AuthorizationSteps) != 1 || len(plan.Authorizations) != 1 {
			t.Fatalf("preview for %s = %+v, %v", target, plan, err)
		}
		return plan.Authorizations
	}
	request.Authorizations = preview("first")
	h.capability.outcomeFor = map[string]Result{"first": {Outcome: reconciliation.OutcomeFailed}}
	result, err := h.service.Apply(ctx, request)
	want := "bootwright apply --dry-run --context lab --machines first"
	if err == nil || result.Receipt.State != "failed" || result.NextCommand != want {
		t.Fatalf("scoped failure = %+v, %v", result, err)
	}
	for _, diagnostic := range diagnostics.Of(err) {
		if strings.Contains(diagnostic.Remediation, "--authorize") {
			t.Fatalf("a scoped failure proposed an executable grant: %+v", diagnostic)
		}
	}
	if _, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Machines: []string{"second"}}); firstCode(err) != "apply.stage-excludes-retry" {
		t.Fatalf("preview skipped an unrelated failed attempt: %+v", diagnostics.Of(err))
	}
	h.capability.outcomeFor = nil
	if result, err = h.service.Apply(ctx, request); err != nil || result.Receipt.State != "paused" {
		t.Fatalf("scoped retry = %+v, %v", result, err)
	}
	request.Machines = []string{"second"}
	request.Authorizations = preview("second")
	if result, err = h.service.Apply(ctx, request); err != nil || result.Receipt.State != "done" {
		t.Fatalf("widened continuation = %+v, %v", result, err)
	}
	if !slices.Equal(h.capability.applies, []string{"shared", "first", "first", "second"}) {
		t.Fatalf("effects = %v", h.capability.applies)
	}
	request.Machines = []string{"unknown"}
	if _, err := h.service.Apply(ctx, request); firstCode(err) != "apply.stage-empty" {
		t.Fatalf("unknown target in a settled context = %v", err)
	}
}

func TestAClusterSelectionRequiresFrozenOwnershipAndACompatibleAddOnBoundary(t *testing.T) {
	t.Parallel()
	host := stagedDefinition("host", reconciliation.StageClusters)
	host.Kind = "OpenShiftCluster"
	side := stagedDefinition("addon", reconciliation.StageAddOns, host.ID)
	side.Kind, side.Object = "ClusterAddonBinding", "peer.gitops"
	selection, _ := reconciliation.ParseSelection(nil, side.Object)
	selection, _ = selection.WithTargets([]string{"host"}, nil)
	build := func() reconciliation.Plan {
		plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{host, side})
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	if _, err := bindSelection(testContextName, build(), selection); firstCode(err) != "apply.stage-empty" {
		t.Fatalf("legacy ownership was accepted: %+v", diagnostics.Of(err))
	}
	side.ClusterOwners = []string{"peer"}
	if _, err := bindSelection(testContextName, build(), selection); firstCode(err) != "apply.until-unknown" {
		t.Fatalf("unrelated add-on was accepted: %+v", diagnostics.Of(err))
	}
}

func TestASelectedTargetThePlanHoldsNoBlockForRefusesNamingIt(t *testing.T) {
	t.Parallel()
	host := stagedDefinition("host", reconciliation.StageClusters)
	host.Kind = "OpenShiftCluster"
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{host})
	if err != nil {
		t.Fatal(err)
	}
	selection, _ := reconciliation.ParseSelection(nil, "")
	selection, _ = selection.WithTargets([]string{"absent"}, nil)
	_, err = bindSelection(testContextName, plan, selection)
	reported := diagnostics.Of(err)
	if firstCode(err) != "apply.stage-empty" || !strings.Contains(reported[0].Message, "absent") {
		t.Fatalf("a target no block realizes was accepted: %+v", reported)
	}
}
