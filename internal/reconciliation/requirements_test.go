package reconciliation

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func realizing(id, kind, object string, requires ...ObjectRef) BlockDefinition {
	block := definition(id)
	block.Kind, block.Object, block.Requires = kind, object, requires
	return block
}

// A capability names the API object its block waits for, never another
// capability's block identity, so the engine is the only thing that has to know
// which block realizes what.
func TestRequirementsResolveIntoDependenciesOnTheRealizingBlocks(t *testing.T) {
	plan, err := NewPlan(Apply, []BlockDefinition{
		realizing("install-rhel-01", "Machine", "rhel-01", ObjectRef{Kind: "InfraProvider", Object: "lab"}, ObjectRef{Kind: "ArtifactServer", Object: "files"}),
		realizing("provider-lab", "InfraProvider", "lab"),
		realizing("artifact-server-files", "ArtifactServer", "files"),
	})
	if err != nil {
		t.Fatal(err)
	}
	block, found := plan.Block("install-rhel-01")
	if !found {
		t.Fatal("the requiring block is missing from the plan")
	}
	if !slices.Equal(block.Dependencies, []string{"artifact-server-files", "provider-lab"}) {
		t.Fatalf("dependencies = %v", block.Dependencies)
	}
	order := []string{}
	for _, block := range plan.Blocks {
		order = append(order, block.ID)
	}
	if order[len(order)-1] != "install-rhel-01" {
		t.Fatalf("order = %v", order)
	}
	// Requirements are resolved before the plan freezes, so a frozen block
	// carries only the block identities it waits for.
	if block.Requires != nil {
		t.Fatalf("a frozen block still carries its requirements: %v", block.Requires)
	}
}

// Several blocks may realize one object, and a block that realizes the object
// it requires must not wait for itself.
func TestRequirementsCoverEveryRealizingBlockAndNeverTheBlockItself(t *testing.T) {
	plan, err := NewPlan(Apply, []BlockDefinition{
		realizing("consumer", "Machine", "guest", ObjectRef{Kind: "InfraProvider", Object: "lab"}),
		realizing("provider-host", "InfraProvider", "lab"),
		realizing("provider-network", "InfraProvider", "lab", ObjectRef{Kind: "InfraProvider", Object: "lab"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	consumer, _ := plan.Block("consumer")
	if !slices.Equal(consumer.Dependencies, []string{"provider-host", "provider-network"}) {
		t.Fatalf("dependencies = %v", consumer.Dependencies)
	}
	network, _ := plan.Block("provider-network")
	if !slices.Equal(network.Dependencies, []string{"provider-host"}) {
		t.Fatalf("a block waited for itself: %v", network.Dependencies)
	}
}

// A requirement no block realizes is a planning defect, not a runtime surprise:
// it refuses before the plan exists rather than registering an operation whose
// block can never start.
func TestARequirementNoBlockRealizesRefusesBeforeThePlanExists(t *testing.T) {
	_, err := NewPlan(Apply, []BlockDefinition{
		realizing("install-rhel-01", "Machine", "rhel-01", ObjectRef{Kind: "InfraProvider", Object: "absent"}),
	})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || !strings.Contains(reported[0].Message, "InfraProvider/absent") {
		t.Fatalf("refusal = %#v", reported)
	}
}

// Only a destroy that removes data an operator cannot recover consumes an
// authorization, and the token set is closed.
func TestConsumedAuthorizationsAreClosedUniqueAndOrdered(t *testing.T) {
	destructive := definition("destroy-guest")
	destructive.Consumes = []string{AuthorizationDataLoss}
	plan, err := NewPlan(Destroy, []BlockDefinition{destructive})
	if err != nil {
		t.Fatal(err)
	}
	block, _ := plan.Block("destroy-guest")
	if !slices.Equal(block.Consumes, []string{"data-loss"}) {
		t.Fatalf("consumes = %v", block.Consumes)
	}
	for name, tokens := range map[string][]string{
		"unrecognized": {"force"},
		"repeated":     {AuthorizationDataLoss, AuthorizationDataLoss},
		"unordered":    {AuthorizationDataLoss, "a-data-loss"},
	} {
		t.Run(name, func(t *testing.T) {
			refused := definition("destroy-guest")
			refused.Consumes = tokens
			if _, err := NewPlan(Destroy, []BlockDefinition{refused}); err == nil {
				t.Fatalf("%v was accepted", tokens)
			}
		})
	}
	if ValidAuthorization("") || ValidAuthorization("DATA-LOSS") || !ValidAuthorization(AuthorizationDataLoss) {
		t.Fatal("the authorization token set is not closed")
	}
}

// The inverse of a completed apply carries each block's own authorization, so
// the destroy an operator confirms is the one the plan froze.
func TestTheInversePreservesWhatEachBlockConsumes(t *testing.T) {
	destructive := definition("guest")
	destructive.Consumes = []string{AuthorizationDataLoss}
	plan, err := NewPlan(Apply, []BlockDefinition{destructive, definition("network")})
	if err != nil {
		t.Fatal(err)
	}
	inverse := plan.Inverse()
	if len(inverse.Blocks) != 2 {
		t.Fatalf("inverse = %+v", inverse.Blocks)
	}
	guest, found := inverse.Block("guest")
	if !found || !slices.Equal(guest.Consumes, []string{"data-loss"}) {
		t.Fatalf("inverse guest = %+v", guest)
	}
	network, _ := inverse.Block("network")
	if len(network.Consumes) != 0 {
		t.Fatalf("a block acquired an authorization through the inverse: %v", network.Consumes)
	}
}
