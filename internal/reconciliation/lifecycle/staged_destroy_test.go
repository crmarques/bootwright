package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/contextguard"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

func stagedDestroy(t *testing.T, h *harness, floor reconciliation.Stage) *OperationResult {
	t.Helper()
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(floor), SkipConfirmation: true})
	if err != nil {
		t.Fatalf("destroy --stage %s: %v", floor, err)
	}
	return result
}

func currentRecord(t *testing.T, h *harness) (operationstore.Operation, reconciliation.Plan) {
	t.Helper()
	ctx := context.Background()
	store := h.service.store(h.workspace.view())
	index, err := store.Index(ctx)
	if err != nil || index.Current == "" {
		t.Fatalf("the index names %q (%v)", index.Current, err)
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	return operation, plan
}

func planIDs(plan reconciliation.Plan) []string {
	ids := []string{}
	for _, block := range plan.Blocks {
		ids = append(ids, block.ID)
	}
	return ids
}

func trimmedEvidence(t *testing.T) []byte {
	t.Helper()
	data, err := reconciliation.Evidence{Operation: reconciliation.MutationNone, Ownership: reconciliation.OwnershipRetained}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A staged destroy from the clusters stage takes back the cluster step and
// keeps what lies below it: the context rests trimmed, keeping every binding,
// its evidence retained and no binding released. An apply over it refuses, the
// same floor settles, and a destroy without --stage takes back what it kept
// and ends pristine.
func TestAStagedDestroyTrimsTheContextAndAWholeDestroyFinishesIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	applied := completeApply(t, h)
	result := stagedDestroy(t, h, reconciliation.StageClusters)
	if result.Receipt.Verb != "destroy" || result.Receipt.State != "done" || result.Receipt.Next != "none" {
		t.Fatalf("the staged destroy's receipt reads %+v", result.Receipt)
	}
	removal, plan := currentRecord(t, h)
	if removal.Source != applied || removal.Stage == nil || removal.Stage.Floor != reconciliation.StageClusters ||
		!slices.Equal(removal.Stage.TakenBack, []string{"charlie"}) || !slices.Equal(planIDs(plan), []string{"charlie"}) {
		t.Fatalf("the staged destroy recorded %+v over %v", removal, planIDs(plan))
	}
	if !bytes.Equal(h.workspace.evidence, trimmedEvidence(t)) {
		t.Fatalf("the trimmed context's evidence reads %s", h.workspace.evidence)
	}
	if disposition, err := (contextguard.Guard{}).Check(ctx, h.workspace.evidence); err != nil || !disposition.Update || disposition.Dispose {
		t.Fatalf("the context guard grants the trimmed context %+v (%v), want update and no disposal", disposition, err)
	}
	if len(h.binder.released) != 0 {
		t.Fatalf("the staged destroy released %v", h.binder.released)
	}
	if destroyed := h.capability.destroys; !slices.Equal(destroyed, []string{"charlie"}) {
		t.Fatalf("the staged destroy removed %v", destroyed)
	}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); firstCode(err) != applyContextTrimmed.Code {
		t.Fatalf("an apply over the trimmed context returned %v", err)
	}
	settled := stagedDestroy(t, h, reconciliation.StageClusters)
	if !settled.Settled || len(h.binder.released) != 0 {
		t.Fatalf("the same floor over the trimmed context returned %+v and released %v", settled, h.binder.released)
	}
	if owned, err := h.service.Ownership(ctx, testContextName); err != nil || owned[blockIdentity(t, h, "alpha")].Verb != "apply" {
		t.Fatalf("the trimmed context owns %v (%v)", owned, err)
	}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	whole, wholePlan := currentRecord(t, h)
	if whole.Stage != nil || whole.Replaces != removal.ID || whole.Source != applied || !slices.Equal(planIDs(wholePlan), []string{"bravo", "alpha"}) {
		t.Fatalf("the whole destroy recorded %+v over %v", whole, planIDs(wholePlan))
	}
	pristine, err := projection(reconciliation.Destroy, reconciliation.OperationDone)
	if err != nil || !bytes.Equal(h.workspace.evidence, pristine) {
		t.Fatalf("the whole destroy left the evidence %s (%v)", h.workspace.evidence, err)
	}
}

func blockIdentity(t *testing.T, h *harness, id string) string {
	t.Helper()
	for _, definition := range chainedDefinitions() {
		if definition.ID == id {
			return definition.Kind + "/" + definition.Object
		}
	}
	t.Fatalf("no block %s", id)
	return ""
}

func failedStagedRemoval(t *testing.T, h *harness) {
	t.Helper()
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
		t.Fatal("a staged destroy whose block failed reported success")
	}
	h.capability.outcomes = nil
}

type deletingTestCapability struct {
	*testCapability
	deletes map[string]bool
}

func (c *deletingTestCapability) DeletesMachine(block reconciliation.Block) bool {
	return c.deletes[block.ID]
}

func (c *deletingTestCapability) RemovesHost(block reconciliation.Block) bool {
	return c.deletes[block.ID]
}

type anyKindResolver struct{ testResolver }

func (r anyKindResolver) Resolve(string, string) (Capability, bool) { return r.capability, true }

// stagedHarness holds a context with a step at every floor: a host, a Machine
// the host realizes and its own step deletes, a cluster installation on that
// Machine and an add-on on the cluster, beside an infrastructure service.
func stagedHarness(t *testing.T, extra ...reconciliation.BlockDefinition) *harness {
	t.Helper()
	machine := stagedDefinition("machine", reconciliation.StageMachines, "host")
	machine.Kind = "Machine"
	definitions := append([]reconciliation.BlockDefinition{
		stagedDefinition("alpha", reconciliation.StageInfrastructureServices),
		stagedDefinition("host", reconciliation.StageSubstrates, "alpha"),
		machine,
		stagedDefinition("installation", reconciliation.StageClusters, "machine"),
		stagedDefinition("addon", reconciliation.StageAddOns, "installation"),
	}, extra...)
	h := newPlannedHarness(t, definitions)
	h.service.capabilities = anyKindResolver{testResolver{capability: &deletingTestCapability{testCapability: h.capability, deletes: map[string]bool{"machine": true}}}}
	return h
}

// A lower floor over a trimmed context replaces the staged destroy with one
// whose frozen taken-back set holds both scopes, and the whole destroy then
// takes back only what neither took back.
func TestALowerFloorWidensATrimmedContextCumulatively(t *testing.T) {
	t.Parallel()
	h := stagedHarness(t)
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageMachines)
	first, plan := currentRecord(t, h)
	if !slices.Equal(first.Stage.TakenBack, []string{"addon", "installation", "machine"}) || len(plan.Blocks) != 3 {
		t.Fatalf("the machines floor took back %v over %v", first.Stage.TakenBack, planIDs(plan))
	}
	stagedDestroy(t, h, reconciliation.StageSubstrates)
	second, plan := currentRecord(t, h)
	if second.Replaces != first.ID || second.Source != first.Source || second.Stage.Floor != reconciliation.StageSubstrates ||
		!slices.Equal(second.Stage.TakenBack, []string{"addon", "host", "installation", "machine"}) || !slices.Equal(planIDs(plan), []string{"host"}) {
		t.Fatalf("the substrates floor recorded %+v over %v", second, planIDs(plan))
	}
	if settled := stagedDestroy(t, h, reconciliation.StageClusters); !settled.Settled {
		t.Fatalf("a later floor over the trimmed context returned %+v", settled)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	whole, plan := currentRecord(t, h)
	if whole.Replaces != second.ID || whole.Stage != nil || !slices.Equal(planIDs(plan), []string{"alpha"}) {
		t.Fatalf("the whole destroy recorded %+v over %v", whole, planIDs(plan))
	}
	if destroyed := h.capability.destroys; !slices.Equal(destroyed, []string{"addon", "installation", "machine", "host", "alpha"}) {
		t.Fatalf("the destroys removed %v", destroyed)
	}
}

// A scope that would take back a step on a Machine it keeps refuses before it
// registers: the cluster installation runs on the Machine the machines stage
// keeps, so the clusters and add-ons floors name the machines floor.
func TestAStagedDestroyRefusesAStepOnAKeptMachine(t *testing.T) {
	t.Parallel()
	h := stagedHarness(t)
	completeApply(t, h)
	before := untouchedOf(h)
	for _, floor := range []reconciliation.Stage{reconciliation.StageClusters, reconciliation.StageAddOns} {
		_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(floor), SkipConfirmation: true})
		reported := diagnostics.Of(err)
		if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "Machine/machine") || !strings.Contains(reported[0].Remediation, "--stage machines") {
			t.Fatalf("destroy --stage %s returned %v", floor, err)
		}
	}
	before.require(t, h)
}

type boundTestCapability struct {
	*testCapability
	hosts, nodes map[string][]string
}

func (c boundTestCapability) HostMachines(block reconciliation.Block) ([]string, error) {
	return c.hosts[block.ID], nil
}

func (c boundTestCapability) NodeMachines(block reconciliation.Block) ([]string, error) {
	return c.nodes[block.ID], nil
}

// A Machine this context realizes only by an infrastructure-services step, as
// a provided or bootstrap Machine is, is kept like any Machine whose own
// deletion is outside the scope: a scoped step bound to it by its hosts or its
// nodes, with no dependency on it, refuses the staged destroy.
func TestAStagedDestroyRefusesAStepBoundToAProvidedMachine(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ hosts, nodes map[string][]string }{
		"bound by its hosts": {hosts: map[string][]string{"service": {"provided"}}},
		"bound by its nodes": {nodes: map[string][]string{"service": {"provided"}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			enrollment := stagedDefinition("enrollment", reconciliation.StageInfrastructureServices)
			enrollment.Kind, enrollment.Object = "Machine", "provided"
			h := newPlannedHarness(t, []reconciliation.BlockDefinition{enrollment, stagedDefinition("service", reconciliation.StageClusters)})
			h.service.capabilities = anyKindResolver{testResolver{capability: boundTestCapability{testCapability: h.capability, hosts: test.hosts, nodes: test.nodes}}}
			completeApply(t, h)
			before := untouchedOf(h)
			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
			if reported := diagnostics.Of(err); firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step service runs on Machine/provided") {
				t.Fatalf("destroy --stage clusters returned %v", err)
			}
			before.require(t, h)
		})
	}
}

// A step below the floor that depends on what the scope takes back refuses
// the staged destroy and names the lower floor that takes it back too, or the
// whole destroy where that step is an infrastructure service.
func TestAStagedDestroyRefusesADependentBelowTheFloor(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		dependent reconciliation.BlockDefinition
		remedy    string
	}{
		"a substrate on the cluster":              {stagedDefinition("provider", reconciliation.StageSubstrates, "addon"), "--stage substrates"},
		"an infrastructure service on the add-on": {stagedDefinition("mirror", reconciliation.StageInfrastructureServices, "addon"), "destroy --dry-run --context " + testContextName},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := stagedHarness(t, test.dependent)
			completeApply(t, h)
			before := untouchedOf(h)
			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageMachines), SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if firstCode(err) != destroyStageBelowFloor.Code || !strings.Contains(reported[0].Message, test.dependent.ID) || !strings.HasSuffix(reported[0].Remediation, test.remedy) {
				t.Fatalf("destroy --stage machines returned %v %+v", err, reported)
			}
			before.require(t, h)
		})
	}
}

// A staged destroy keeps only finished steps: a kept step an apply left
// failed refuses it, and a scope over the failed step itself takes it back.
func TestAStagedDestroyKeepsOnlyFinishedSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("alpha", reconciliation.StageInfrastructureServices),
		stagedDefinition("charlie", reconciliation.StageClusters),
	})
	applyChained(t, h, map[string]Result{"alpha": {Outcome: reconciliation.OutcomeFailed}})
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); firstCode(err) != destroyStageKeepsUnfinished.Code {
		t.Fatalf("a staged destroy keeping a failed step returned %v", err)
	}
	h = newPlannedHarness(t, chainedDefinitions())
	applyChained(t, h, map[string]Result{"charlie": {Outcome: reconciliation.OutcomeFailed}})
	stagedDestroy(t, h, reconciliation.StageClusters)
	removal, plan := currentRecord(t, h)
	if !trimmed(removal) || !slices.Equal(planIDs(plan), []string{"charlie"}) {
		t.Fatalf("the staged destroy over the failed apply recorded %+v over %v", removal, planIDs(plan))
	}
}

// A floor with nothing the context owns at or above it settles over the apply
// and keeps every binding and the apply itself; an unregistration it
// acknowledges is refused as unneeded, as over a scope that removes nothing.
func TestAnEmptyScopeSettlesKeepingEveryBinding(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, chainedDefinitions())
	applied := completeApply(t, h)
	if settled := stagedDestroy(t, h, reconciliation.StageAddOns); !settled.Settled || settled.Receipt.Operation != applied {
		t.Fatalf("an empty scope returned %+v", settled)
	}
	if current := currentOperation(t, h); current != applied || len(h.binder.released) != 0 {
		t.Fatalf("an empty scope left %s and released %v", current, h.binder.released)
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageAddOns), Unregistered: []string{"charlie"}, SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != authorizeUnneeded.Code || !strings.Contains(reported[0].Remediation, "destroy --context "+testContextName+" --stage add-ons without --unregistered charlie") {
		t.Fatalf("an empty scope acknowledging an unregistration returned %v %+v", err, reported)
	}
	_, err = h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy, Floor: string(reconciliation.StageAddOns), Unregistered: []string{"charlie"}})
	if firstCode(err) != authorizeUnneeded.Code {
		t.Fatalf("an empty scope's preview acknowledging an unregistration returned %v", err)
	}
}

// A failed staged destroy is replaced at its own floor by one that keeps the
// frozen taken-back set and the floor; a destroy without --stage refuses and
// names that command until it completes.
func TestAFailedStagedDestroyIsReplacedAtItsFloor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	failedStagedRemoval(t, h)
	failed, _ := currentRecord(t, h)
	_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != destroyStageChanged.Code || !strings.Contains(reported[0].Remediation, "destroy --context "+testContextName+" --stage clusters") {
		t.Fatalf("a destroy without --stage over the failed staged destroy returned %v", err)
	}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageAddOns), SkipConfirmation: true}); firstCode(err) != destroyStageChanged.Code {
		t.Fatalf("a later floor over the failed staged destroy returned %v", err)
	}
	stagedDestroy(t, h, reconciliation.StageClusters)
	replacement, plan := currentRecord(t, h)
	if !trimmed(replacement) || replacement.Replaces != failed.ID || !slices.Equal(replacement.Stage.TakenBack, failed.Stage.TakenBack) || !slices.Equal(planIDs(plan), []string{"charlie"}) {
		t.Fatalf("the replacement recorded %+v over %v", replacement, planIDs(plan))
	}
	if len(h.binder.released) != 0 || !bytes.Equal(h.workspace.evidence, trimmedEvidence(t)) {
		t.Fatalf("the replacement released %v and left the evidence %s", h.binder.released, h.workspace.evidence)
	}
}

// Completion discards the custody of the producers the staged destroy took
// back and keeps the kept producers'.
func TestAStagedDestroyDiscardsOnlyItsProducersCustody(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, chainedDefinitions())
	h.capability.outcomes = []Result{changedWithKubeconfig(), changedWithKubeconfig(), changedWithKubeconfig()}
	completeApply(t, h)
	h.capability.outcomes = nil
	if held := kept(h); len(held) != 3 {
		t.Fatalf("the apply left custody %v", held)
	}
	stagedDestroy(t, h, reconciliation.StageClusters)
	if held := kept(h); !slices.Equal(held, []string{"alpha/kubeconfig=" + producedCanary, "bravo/kubeconfig=" + producedCanary}) {
		t.Fatalf("the staged destroy left custody %v", held)
	}
}

// Every refusal over a staged destroy names a command that repeats it at its
// floor, so following a remedy verbatim never runs the whole removal: the
// authorization, confirmation, relaxation, unregistration and rebind remedies
// of a requested floor, and the replacement of a failed one at its frozen floor.
func TestEveryRemedyOverAStagedDestroyRepeatsItsFloor(t *testing.T) {
	t.Parallel()
	dataLoss := map[string][]string{"charlie": {reconciliation.AuthorizationDataLoss}}
	for name, test := range map[string]struct {
		consumes map[string][]string
		request  DestroyRequest
		code     string
		failed   bool
		prompt   bool
	}{
		"a missing authorization":       {consumes: dataLoss, code: authorizeMissing.Code},
		"a stale authorization":         {consumes: dataLoss, request: DestroyRequest{Authorizations: []string{"data-loss=0000000000"}}, code: authorizeStale.Code},
		"an unneeded authorization":     {request: DestroyRequest{Authorizations: []string{"data-loss"}}, code: authorizeUnneeded.Code},
		"a confirmation":                {consumes: dataLoss, request: DestroyRequest{Authorizations: []string{"data-loss"}}, code: engineConfirmationRequired.Code, prompt: true},
		"an inapplicable relaxation":    {request: DestroyRequest{Relaxations: map[string]string{"charlie": EraseRetain}}, code: authorizeUnneeded.Code},
		"an unneeded unregistration":    {request: DestroyRequest{Unregistered: []string{"charlie"}}, code: authorizeUnneeded.Code},
		"an invalid rebind":             {request: DestroyRequest{Authorizations: []string{"rebind=Not_A_Name"}}, code: authorizeRebindInvalid.Code},
		"an authorization at its floor": {consumes: dataLoss, code: authorizeMissing.Code, failed: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newPlannedHarness(t, chainedDefinitions())
			if test.failed {
				failedStagedRemoval(t, h)
			} else {
				completeApply(t, h)
			}
			h.capability.consumes = test.consumes
			if test.prompt {
				h.service.options.Confirmer = nil
			}
			request := test.request
			request.ContextName, request.Floor, request.SkipConfirmation = testContextName, string(reconciliation.StageClusters), !test.prompt
			_, err := h.service.Destroy(context.Background(), request)
			reported := diagnostics.Of(err)
			if firstCode(err) != test.code || !strings.Contains(reported[0].Remediation, "destroy --context "+testContextName+" --stage clusters") {
				t.Fatalf("the staged destroy returned %v %+v", err, reported)
			}
		})
	}
}

// A remedy that repeats its invocation's own verb builds its flags through
// invocationFlags, the one place that carries a staged destroy's floor; the
// functions named here pass flags through, only ever repeat an apply, or name
// an abandonment, which no staged destroy admits.
func TestARemedyRepeatingItsVerbCarriesTheFloor(t *testing.T) {
	t.Parallel()
	passing := map[string]bool{"planCommand": true, "preparationCommand": true, "resolutionRemedy": true, "refuseUnattendedAbandonment": true, "abandonExit": true}
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		syntax, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range syntax.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || passing[function.Name.Name] {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !isCall(call, "contextCommand") && !isCall(call, "planCommand") {
					return true
				}
				repeatsRemoval := len(call.Args) > 2 && isDestroy(call.Args[1])
				if isCall(call, "contextCommand") && constantVerb(call.Args[1]) && !repeatsRemoval {
					return true
				}
				if len(call.Args) != 3 || !call.Ellipsis.IsValid() || !isCallTo(call.Args[2], "invocationFlags") {
					t.Errorf("%s: %s repeats its verb or a removal's flags without invocationFlags", source, function.Name.Name)
				}
				return true
			})
		}
	}
}

func isCall(call *ast.CallExpr, name string) bool {
	identifier, ok := call.Fun.(*ast.Ident)
	return ok && identifier.Name == name
}

func isCallTo(expression ast.Expr, name string) bool {
	call, ok := expression.(*ast.CallExpr)
	return ok && isCall(call, name)
}

func isDestroy(expression ast.Expr) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || !isCall(call, "string") {
		return false
	}
	selector, ok := call.Args[0].(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "Destroy"
}

func constantVerb(expression ast.Expr) bool {
	switch verb := expression.(type) {
	case *ast.BasicLit:
		return true
	case *ast.BinaryExpr:
		return constantVerb(verb.X) && constantVerb(verb.Y)
	case *ast.CallExpr:
		selector, ok := verb.Args[0].(*ast.SelectorExpr)
		if !ok || !isCall(verb, "string") {
			return false
		}
		owner, named := selector.X.(*ast.Ident)
		return named && owner.Name == "reconciliation"
	}
	return false
}

// A resolution that proves the last step of an unknown staged destroy gone
// completes it as its own run would: a relaxation the operator adds replaces it,
// whose resolution records it done and discards the custody of what it took
// back, even when the replacement then refuses at its quiescence gate.
func TestAResolutionThatCompletesAStagedDestroyDiscardsItsCustody(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	h.service.capabilities = testResolver{capability: &relaxingTestCapability{testCapability: h.capability, relaxable: map[string]bool{"charlie": true}}}
	h.capability.outcomes = []Result{changedWithKubeconfig(), changedWithKubeconfig(), changedWithKubeconfig()}
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
		t.Fatal("the seeded unknown staged destroy did not fire")
	}
	unknown, _ := currentRecord(t, h)
	h.capability.outcomes, h.capability.observations = nil, []Observation{{Effect: reconciliation.EffectCompleted}}
	h.capability.quiescence = map[string]Quiescence{"charlie": {State: Live, Reason: "a client still uses it"}}
	_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true,
		Relaxations: map[string]string{"charlie": EraseRetain}, Authorizations: []string{reconciliation.AuthorizationDataLoss}})
	if resolved := readOperation(t, h, unknown.ID); !trimmed(resolved) {
		t.Fatalf("the resolution left the staged destroy %s (%v %+v)", resolved.State, err, diagnostics.Of(err))
	}
	if held := kept(h); !slices.Equal(held, []string{"alpha/kubeconfig=" + producedCanary, "bravo/kubeconfig=" + producedCanary}) {
		t.Fatalf("the resolved staged destroy left custody %v", held)
	}
	if !bytes.Equal(h.workspace.evidence, trimmedEvidence(t)) {
		t.Fatalf("the resolved staged destroy left the evidence %s", h.workspace.evidence)
	}
}

type sharedClaimingCapability struct {
	*testCapability
	shared map[string][]infrastructureservices.HostReservation
}

func (c sharedClaimingCapability) SharedClaims(block reconciliation.Block) ([]infrastructureservices.HostReservation, error) {
	return c.shared[block.ID], nil
}

// A trimmed context keeps exactly the claims its kept steps plan: a shared
// claim loses the keys only the taken-back step plans and goes once nothing
// kept plans it, whatever object it names; the claim of the deleted object
// keeps the releasing mark its removal set, and a kept object's claim loses
// one. A releasing mark on a kept claim leaves the completion unfinalized
// until the next invocation clears it, while the deleted object's mark stays.
func TestATrimmedContextKeepsTheClaimsItsKeptStepsPlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	media := func(keys ...string) infrastructureservices.HostReservation {
		return infrastructureservices.HostReservation{Context: testContextName, Kind: "media", Service: "media", Shared: true, Keys: keys}
	}
	use := infrastructureservices.HostReservation{Context: testContextName, Kind: "use", Service: "use", Shared: true, Keys: []string{"bridge-use:br-c"}}
	server := func(object string) infrastructureservices.HostReservation {
		return infrastructureservices.HostReservation{Context: testContextName, Kind: "artifact-server", Service: object, Keys: []string{"bridge:br-" + object}}
	}
	h.capability.reservations = []infrastructureservices.HostReservation{media("media:boot", "media:shared", "media:tree"), use, server("alpha"), server("charlie")}
	h.service.capabilities = testResolver{capability: sharedClaimingCapability{testCapability: h.capability, shared: map[string][]infrastructureservices.HostReservation{
		"bravo":   {media("media:boot", "media:shared")},
		"charlie": {media("media:shared", "media:tree"), use},
	}}}
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	alpha, charlie := server("alpha"), server("charlie")
	charlie.Releasing = true
	want := []infrastructureservices.HostReservation{alpha, charlie, media("media:boot", "media:shared")}
	if !slices.EqualFunc(h.workspace.reservations, want, func(x, y infrastructureservices.HostReservation) bool { return reflect.DeepEqual(x, y) }) {
		t.Fatalf("the trimmed context holds %+v, want %+v", h.workspace.reservations, want)
	}
	h.workspace.reservations[0].Releasing = true
	removal, _ := currentRecord(t, h)
	if finished, err := finalized(ctx, h.workspace.view(), h.service.store(h.workspace.view()), removal); err != nil || finished {
		t.Fatalf("a kept claim's releasing mark reads finalized %v (%v)", finished, err)
	}
	if settled := stagedDestroy(t, h, reconciliation.StageClusters); !settled.Settled {
		t.Fatalf("the same floor over the unfinalized trimmed context returned %+v", settled)
	}
	if !slices.EqualFunc(h.workspace.reservations, want, func(x, y infrastructureservices.HostReservation) bool { return reflect.DeepEqual(x, y) }) {
		t.Fatalf("the finalization left %+v, want %+v", h.workspace.reservations, want)
	}
	if finished, err := finalized(ctx, h.workspace.view(), h.service.store(h.workspace.view()), removal); err != nil || !finished {
		t.Fatalf("the deleted object's releasing mark reads finalized %v (%v)", finished, err)
	}
}

// B1866: every reader of a removal's ownership reads a kept step of a trimmed
// lineage as its holder's: the evidence the machine power realization reads,
// the status realization and a replacement's covered set.
func TestAKeptStepOfATrimmedLineageReadsAsItsHolders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	applied := completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	removal, plan := currentRecord(t, h)
	for id, want := range map[string]string{"alpha": applied, "charlie": removal.ID} {
		found, err := h.service.Evidence(ctx, testContextName, "ArtifactServer", id)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 || found[0].State != reconciliation.BlockDone || found[0].Operation != want {
			t.Errorf("the evidence of %s over the trimmed context reads %+v", id, found)
		}
	}
	store := h.service.store(h.workspace.view())
	states, err := store.BlockStates(ctx, removal.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	realized, err := realizations(ctx, store, removal, plan, states)
	if err != nil {
		t.Fatal(err)
	}
	if realized["ArtifactServer/alpha"] != RealizationDone || realized["ArtifactServer/charlie"] != RealizationPending {
		t.Errorf("the trimmed context's realizations read %v", realized)
	}

	h = newPlannedHarness(t, chainedDefinitions())
	failedStagedRemoval(t, h)
	failed, failedPlan := currentRecord(t, h)
	store = h.service.store(h.workspace.view())
	if states, err = store.BlockStates(ctx, failed.ID, failedPlan); err != nil {
		t.Fatal(err)
	}
	scope, err := replacementOf(ctx, store, failed, failedPlan, states)
	if err != nil || !slices.Equal(planIDs(scope.owned), []string{"charlie"}) {
		t.Fatalf("the replacement covers %v (%v)", planIDs(scope.owned), err)
	}
	kept, err := keptSteps(ctx, store, failed, failedPlan, states)
	if err != nil || !slices.Equal(kept, []string{"alpha", "bravo"}) {
		t.Fatalf("the failed staged destroy keeps %v (%v)", kept, err)
	}
}

// A diff over a trimmed context reads the kept steps' objects as their apply
// realized them and the input as unchanged; an object no step names is not
// read as one the staged destroy took back.
func TestADiffOverATrimmedContextReadsWhatItKeeps(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, chainedDefinitions())
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	diff, err := h.service.Diff(context.Background(), DiffRequest{ContextName: testContextName})
	if err != nil || diff.Input != diffUnchanged || diff.Operation.Verb != "destroy" || diff.Plan.State != "same" {
		t.Fatalf("the diff over the trimmed context reads %+v %+v input %s (%v)", diff.Operation, diff.Plan, diff.Input, err)
	}
	read := map[string]ObjectDiff{}
	for _, object := range diff.Objects {
		read[string(object.Kind)+"/"+object.Name] = object
	}
	environment, alpha, charlie := read["Environment/lab"], read["ArtifactServer/alpha"], read["ArtifactServer/charlie"]
	if alpha.Realization != RealizationDone || charlie.Realization == RealizationDone || strings.Contains(environment.Real.Reason, "took it back") {
		t.Fatalf("the diff over the trimmed context reads %+v", diff.Objects)
	}
}

// A whole destroy over a trimmed context reopens the binding the staged
// destroy rebound, so it presents the rotated credential the kept steps now
// need, and releases that binding beside the lineage's once it completes.
func TestAWholeDestroyOverATrimmedContextReopensTheRebindingItsStagedDestroyMade(t *testing.T) {
	t.Parallel()
	base, cluster := stagedDefinition("base", reconciliation.StageInfrastructureServices), stagedDefinition("cluster", reconciliation.StageClusters, "base")
	base.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"base"}`)
	cluster.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"cluster"}`)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{base, cluster})
	h.capability.secrets = []string{"bmc"}
	h.capability.access = map[string][]string{"base": {"bmc"}, "cluster": {"bmc"}}
	h.binder.material = map[string]secrets.Material{"bmc": password("first")}
	h.binder.versions = map[string]string{"bmc": "ver-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second")}
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	h.binder.versions["bmc"] = "ver-2"
	seen := presentedTo(h)
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters),
		Authorizations: []string{RebindAuthorization + "bmc"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	staged, _ := currentRecord(t, h)
	if !trimmed(staged) || slices.Equal(staged.Bindings, applied.Bindings) || len(h.binder.released) != 0 {
		t.Fatalf("the rebound staged destroy recorded %+v and released %v", staged, h.binder.released)
	}
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy over the trimmed context presented %q: %v", *seen, err)
	}
	if !slices.Equal(*seen, []string{"second", "second"}) {
		t.Fatalf("the removals presented %q, want the rebound version each time", *seen)
	}
	for _, binding := range append(slices.Clone(applied.Bindings), staged.Bindings...) {
		if !slices.Contains(h.binder.released, binding) {
			t.Fatalf("released = %v, want %s released by the whole destroy", h.binder.released, binding)
		}
	}
}
