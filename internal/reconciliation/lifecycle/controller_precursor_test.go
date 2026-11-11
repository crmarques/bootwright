package lifecycle

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type precursorCapability struct {
	*testCapability
	precedes []string
}

func (c precursorCapability) PrecedesController(definition reconciliation.BlockDefinition) bool {
	return slices.Contains(c.precedes, definition.ID)
}

func precursorService(precedes ...string) Service {
	return Service{capabilities: testResolver{capability: precursorCapability{testCapability: &testCapability{}, precedes: precedes}}}
}

func mirrorDefinitions() []reconciliation.BlockDefinition {
	controller := stagedDefinition("controller-prerequisites", reconciliation.StageController)
	controller.Requires = []reconciliation.ObjectRef{{Kind: "ArtifactServer", Object: "mirror"}}
	server := stagedDefinition("artifact-server-mirror", reconciliation.StageInfrastructureServices)
	server.Object, server.Family = "mirror", "managed-service"
	fill := stagedDefinition("mirror-files-lab", reconciliation.StageInfrastructureServices)
	fill.Object, fill.Family = "mirror", "mirror-fill"
	fill.Requires = []reconciliation.ObjectRef{{Kind: "ArtifactServer", Object: "mirror", Family: "managed-service"}}
	return []reconciliation.BlockDefinition{controller, server, fill, stagedDefinition("artifact-server-lab", reconciliation.StageInfrastructureServices),
		stagedDefinition("machine-guest", reconciliation.StageMachines, "artifact-server-lab")}
}

func dependencies(t *testing.T, plan reconciliation.Plan) map[string][]string {
	t.Helper()
	found := map[string][]string{}
	for _, block := range plan.Blocks {
		found[block.ID] = block.Dependencies
	}
	return found
}

// The controller block waits for the mirror its stage reads, so the engine's
// own edge reaches only the blocks outside what the controller block waits
// for, and the plan holds no cycle: the mirror's server and its fill first,
// the controller stage after them and every other block after it.
func TestTheControllerBlockWaitsForTheMirrorItReadsAndEveryOtherBlockForIt(t *testing.T) {
	resolved, err := reconciliation.ResolveRequirements(mirrorDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := precursorService("artifact-server-mirror", "mirror-files-lab").dependOnController(resolved)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, ordered)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	want := map[string][]string{
		"artifact-server-mirror":   nil,
		"mirror-files-lab":         {"artifact-server-mirror"},
		"controller-prerequisites": {"artifact-server-mirror", "mirror-files-lab"},
		"artifact-server-lab":      {"controller-prerequisites"},
		"machine-guest":            {"artifact-server-lab", "controller-prerequisites"},
	}
	for id, dependencies := range dependencies(t, plan) {
		if !slices.Equal(dependencies, want[id]) && (len(dependencies) != 0 || len(want[id]) != 0) {
			t.Errorf("%s waits for %v, want %v", id, dependencies, want[id])
		}
	}
	ids := []string{}
	for _, block := range plan.Blocks {
		ids = append(ids, block.ID)
	}
	if !slices.Equal(ids[:3], []string{"artifact-server-mirror", "mirror-files-lab", "controller-prerequisites"}) {
		t.Fatalf("plan order = %v", ids)
	}
}

// A block the controller block waits for, directly or through another, must
// be one its capability runs without the stage's clients; any other refuses
// the plan before it exists rather than running before what it needs.
func TestAControllerBlockWaitingForWorkThatNeedsItsClientsRefuses(t *testing.T) {
	definitions := mirrorDefinitions()
	definitions[1].Dependencies = []string{"machine-guest"}
	resolved, err := reconciliation.ResolveRequirements(definitions)
	if err != nil {
		t.Fatal(err)
	}
	_, err = precursorService("artifact-server-mirror", "mirror-files-lab").dependOnController(resolved)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.unsupported" {
		t.Fatalf("closure refusal = %+v", reported)
	}
	for _, named := range []string{"controller-prerequisites", "waits for block artifact-server-lab"} {
		if !slices.ContainsFunc(reported, func(diagnostic diagnostics.Diagnostic) bool { return strings.Contains(diagnostic.Message, named) }) {
			t.Errorf("the refusal %q does not name %q", reported[0].Message, named)
		}
	}
}

// Without a controller block the engine adds no edge at all.
func TestAPlanWithoutAControllerBlockKeepsItsResolvedDependencies(t *testing.T) {
	definitions := mirrorDefinitions()[1:]
	resolved, err := reconciliation.ResolveRequirements(definitions)
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := precursorService().dependOnController(slices.Clone(resolved))
	if err != nil || !slices.EqualFunc(ordered, resolved, func(x, y reconciliation.BlockDefinition) bool { return slices.Equal(x.Dependencies, y.Dependencies) }) {
		t.Fatalf("a plan without a controller block gained dependencies: %v", err)
	}
}
