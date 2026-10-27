package lifecycle

import (
	"context"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// claimResolver offers each binding its own capability, so two bindings can
// plan what each selects independently.
type claimResolver map[CapabilityBinding]Capability

func (r claimResolver) Bindings() []CapabilityBinding {
	return []CapabilityBinding{
		{Kind: "Machine", Implementation: "machine-first-v1", Family: "substrate-machine"},
		{Kind: "Machine", Implementation: "machine-second-v1", Family: "substrate-machine"},
		{Kind: "Machine", Implementation: "os-install-v1", Family: "os-install"},
	}
}

func (r claimResolver) Resolve(kind, implementation string) (Capability, bool) {
	for binding, capability := range r {
		if binding.Kind == kind && binding.Implementation == implementation {
			return capability, true
		}
	}
	return nil, false
}

func claimingCapability(implementation string, machines ...string) *testCapability {
	var definitions []reconciliation.BlockDefinition
	for _, machine := range machines {
		block := definition(implementation + "-" + machine)
		block.Kind, block.Object, block.Implementation = "Machine", machine, implementation
		definitions = append(definitions, block)
	}
	return &testCapability{definitions: definitions}
}

// Two implementations of one capability that both select an object refuse it
// before registration, naming both, while implementations of different
// capabilities planning blocks for one object, and alternatives selecting
// disjoint objects, are no conflict.
func TestAnObjectTwoImplementationsClaimRefusesBeforeRegistration(t *testing.T) {
	bindings := claimResolver{}.Bindings()
	h := newHarness(t)
	h.service.capabilities = claimResolver{
		bindings[0]: claimingCapability("machine-first-v1", "node-a", "node-b"),
		bindings[1]: claimingCapability("machine-second-v1", "node-b", "node-c"),
		bindings[2]: claimingCapability("os-install-v1", "node-a", "node-b", "node-c"),
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	want := []diagnostics.Diagnostic{{
		Severity: "error", Code: "lifecycle.unsupported",
		Message: "two implementations of this executable claim Machine/node-b: machine-first-v1 and machine-second-v1",
		Remediation: "remove Machine/node-b from the selected Environment, or use an example within the supported shape such as " + supportedExample +
			"; each object is realized by exactly one implementation",
		Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: "Machine", Name: "node-b"},
	}}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) {
		t.Fatalf("double claim refusal = %+v, want %+v", reported, want)
	}
	if h.workspace.mutations != 0 {
		t.Fatal("a refused double claim mutated the context")
	}
	h.service.capabilities = claimResolver{
		bindings[0]: claimingCapability("machine-first-v1", "node-a"),
		bindings[1]: claimingCapability("machine-second-v1", "node-b"),
		bindings[2]: claimingCapability("os-install-v1", "node-a", "node-b"),
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("disjoint alternatives refuse: %v", diagnostics.Of(err))
	}
}

// The engine stamps each block with its binding's family before it resolves
// requirements, so a requirement on another family's claim resolves to that
// family's block, and a capability that names a family other than its own
// binding's refuses before registration.
func TestABlockCarriesItsBindingsFamilyAndNoOther(t *testing.T) {
	bindings := claimResolver{}.Bindings()
	install := claimingCapability("os-install-v1", "node-a")
	install.definitions[0].Requires = []reconciliation.ObjectRef{{Kind: "Machine", Object: "node-a", Family: "substrate-machine"}}
	h := newHarness(t)
	h.service.capabilities = claimResolver{
		bindings[0]: claimingCapability("machine-first-v1", "node-a"),
		bindings[1]: claimingCapability("machine-second-v1"),
		bindings[2]: install,
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("a requirement on the substrate family's claim refused: %v", diagnostics.Of(err))
	}
	foreign := claimingCapability("os-install-v1", "node-a")
	foreign.definitions[0].Family = "substrate-machine"
	h = newHarness(t)
	h.service.capabilities = claimResolver{
		bindings[0]: claimingCapability("machine-first-v1", "node-a"),
		bindings[1]: claimingCapability("machine-second-v1"),
		bindings[2]: foreign,
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the os-install-v1 capability planned a block of another family" || h.workspace.mutations != 0 {
		t.Fatalf("a block of another family = %+v, mutations %d", reported, h.workspace.mutations)
	}
}
