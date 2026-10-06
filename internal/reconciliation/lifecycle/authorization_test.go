package lifecycle

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// boundResolver offers one capability per kind-and-implementation pair, which
// is what a build with two implementations of one kind looks like.
type boundResolver map[CapabilityBinding]Capability

func (r boundResolver) Bindings() []CapabilityBinding {
	bindings := make([]CapabilityBinding, 0, len(r))
	for binding := range r {
		bindings = append(bindings, binding)
	}
	slices.SortFunc(bindings, func(x, y CapabilityBinding) int {
		if order := strings.Compare(x.Kind, y.Kind); order != 0 {
			return order
		}
		return strings.Compare(x.Implementation, y.Implementation)
	})
	return bindings
}

func (r boundResolver) Resolve(kind, implementation string) (Capability, bool) {
	capability, ok := r[CapabilityBinding{Kind: kind, Implementation: implementation}]
	return capability, ok
}

func destructive(id string) reconciliation.BlockDefinition {
	block := definition(id)
	block.Consumes = []string{reconciliation.AuthorizationDataLoss}
	return block
}

// An irreversible consequence is acknowledged before the operation registers.
// The plan is presented first, so a plan that consumes an authorization the
// invocation did not supply refuses naming the steps the operator has just
// read and the exact command that passes, before the prompt and before
// anything is written.
func TestAPlanThatConsumesAnAuthorizationRefusesWithoutIt(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("artifacts")})
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab"})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.authorization" ||
		reported[0].Message != "this plan has data-loss consequences that are not authorized: step 1 (serve artifacts)" ||
		reported[0].Remediation != "repeat it with the authorization: bootwright apply --context lab --authorize data-loss" {
		t.Fatalf("missing authorization refusal = %+v (%v)", reported, err)
	}
	if len(h.presenter.presented) != 1 || !slices.Equal(h.presenter.presented[0].Steps[0].Consumes, []string{"data-loss"}) {
		t.Fatalf("the refusal did not follow the presented plan: %+v", h.presenter.presented)
	}
	if h.confirmer.asked != 0 || h.workspace.mutations != 0 || len(h.capability.applies) != 0 {
		t.Fatal("a refused request asked for confirmation, mutated or ran a block")
	}
	result, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", Authorizations: []string{"data-loss"}, SkipConfirmation: true,
	})
	if err != nil || result == nil || result.Receipt.State != "done" {
		t.Fatalf("authorized apply = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.applies, []string{"artifacts"}) {
		t.Fatalf("applied %v", h.capability.applies)
	}
}

// Every presentation marks the steps an authorization acknowledges, and the
// whole frozen plan decides what is required: a preview and the apply that
// presents it carry the same marks, a token the plan consumes nowhere refuses
// naming the command without it, and a selection is kept in the command a
// refusal names.
func TestAPlanMarksTheStepsAnAuthorizationAcknowledges(t *testing.T) {
	network := stagedDefinition("network", reconciliation.StageSubstrates, "artifacts")
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("artifacts"), network})
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	consumes := map[string][]string{}
	for _, step := range preview.Steps {
		consumes[step.ID] = step.Consumes
	}
	if !slices.Equal(consumes["artifacts"], []string{"data-loss"}) || len(consumes["network"]) != 0 {
		t.Fatalf("the preview marked %v", consumes)
	}
	_, err = h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", Stages: []string{"substrates", "infra-components"}})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Remediation != "repeat it with the authorization: bootwright apply --context lab --stage infra-components,substrates --authorize data-loss" {
		t.Fatalf("the staged refusal = %+v (%v)", reported, err)
	}
	if len(h.presenter.presented) != 1 || !slices.EqualFunc(h.presenter.presented[0].Steps, preview.Steps, func(x, y PlanStep) bool {
		return x.ID == y.ID && slices.Equal(x.Consumes, y.Consumes)
	}) {
		t.Fatalf("the apply presented %+v, the preview showed %+v", h.presenter.presented, preview.Steps)
	}
	plain := newHarness(t, "artifacts")
	_, err = plain.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", Authorizations: []string{"data-loss"}})
	reported = diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Message != "this plan requires no data-loss authorization" ||
		reported[0].Remediation != "repeat bootwright apply --context lab without --authorize data-loss" {
		t.Fatalf("the superfluous token refusal = %+v (%v)", reported, err)
	}
	if len(plain.presenter.presented) != 1 || plain.confirmer.asked != 0 || plain.workspace.mutations != 0 {
		t.Fatal("the superfluous token refused before presenting, or asked or mutated")
	}
}

// Authorization is per mutation, not per context: the destroy of a completed
// apply consumes the token again, because it is the destroy's own consequence
// the operator is acknowledging.
func TestEachMutationOfADestructivePlanIsAuthorizedOnItsOwn(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("artifacts"), definition("network")})
	// Removing this block is destructive in its own right, which is what its
	// capability says when the removal is planned from the frozen block.
	h.capability.consumes = map[string][]string{"artifacts": {reconciliation.AuthorizationDataLoss}}
	_, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", Authorizations: []string{"data-loss"}, SkipConfirmation: true,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); firstCode(err) != "lifecycle.authorization" {
		t.Fatalf("unauthorized destroy = %v", err)
	}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{
		ContextName: "lab", Authorizations: []string{"data-loss"}, SkipConfirmation: true,
	})
	if err != nil || result == nil || result.Receipt.State != "done" {
		t.Fatalf("authorized destroy = %+v (%v)", result, err)
	}
}

// A block freezes the implementation that planned it, so a build offering two
// implementations of one kind runs each block against its own capability and
// never against the other.
func TestBlocksRunAgainstTheImplementationThatPlannedThem(t *testing.T) {
	primary := definition("primary")
	secondary := definition("secondary")
	secondary.Implementation = "artifact-server-caddy-v1"
	h := newPlannedHarness(t, nil)
	nginx := &testCapability{definitions: []reconciliation.BlockDefinition{primary}, secrets: []string{"artifact-server-tls"}}
	caddy := &testCapability{definitions: []reconciliation.BlockDefinition{secondary}, secrets: []string{"artifact-server-tls"}}
	h.service.capabilities = boundResolver{
		{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}: nginx,
		{Kind: "ArtifactServer", Implementation: "artifact-server-caddy-v1"}: caddy,
	}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || result == nil || result.Receipt.State != "done" {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	if !slices.Equal(nginx.applies, []string{"primary"}) || !slices.Equal(caddy.applies, []string{"secondary"}) {
		t.Fatalf("nginx applied %v and caddy applied %v", nginx.applies, caddy.applies)
	}
}

// A kind this build binds no implementation for is named before registration,
// so an operation never applies part of a graph it cannot realize.
func TestAKindWithNoBindingIsNamedBeforeRegistration(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("artifacts")})
	h.service.capabilities = boundResolver{}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.state" {
		t.Fatalf("empty binding set refusal = %q", code)
	}
	if h.workspace.mutations != 0 {
		t.Fatal("a build with no capability mutated the context")
	}
}
