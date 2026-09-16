package lifecycle

import (
	"context"
	"slices"
	"strings"
	"testing"

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

// An irreversible consequence is acknowledged before the operation registers,
// so a plan that consumes an authorization the invocation did not supply
// refuses without presenting a plan or writing anything.
func TestAPlanThatConsumesAnAuthorizationRefusesWithoutIt(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("artifacts")})
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.authorization" {
		t.Fatalf("missing authorization refusal = %q", code)
	}
	if len(h.presenter.presented) != 0 || h.workspace.mutations != 0 || len(h.capability.applies) != 0 {
		t.Fatal("a refused request presented a plan, mutated or ran a block")
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
