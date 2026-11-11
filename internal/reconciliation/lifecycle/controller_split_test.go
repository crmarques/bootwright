package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func splitDefinitions() []reconciliation.BlockDefinition {
	definitions := mirrorDefinitions()
	definitions[0].ID = "controller-mirrored-clients"
	return append([]reconciliation.BlockDefinition{stagedDefinition("controller-prerequisites", reconciliation.StageController)}, definitions...)
}

// A stage split around a managed artifact mirror orders the plan as its
// controller-placed mirror needs it: the host packages first, then the
// mirror's server and its fill, then the clients read from it, then every
// other block, which waits for both controller blocks.
func TestASplitControllerStageOrdersTheMirrorBetweenItsBlocks(t *testing.T) {
	resolved, err := reconciliation.ResolveRequirements(splitDefinitions())
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
		"controller-prerequisites":    nil,
		"artifact-server-mirror":      {"controller-prerequisites"},
		"mirror-files-lab":            {"artifact-server-mirror", "controller-prerequisites"},
		"controller-mirrored-clients": {"artifact-server-mirror", "controller-prerequisites", "mirror-files-lab"},
		"artifact-server-lab":         {"controller-mirrored-clients", "controller-prerequisites"},
		"machine-guest":               {"artifact-server-lab", "controller-mirrored-clients", "controller-prerequisites"},
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
	if !slices.Equal(ids[:4], []string{"controller-prerequisites", "artifact-server-mirror", "mirror-files-lab", "controller-mirrored-clients"}) {
		t.Fatalf("plan order = %v", ids)
	}
}

// The second controller block still refuses a block it waits for that needs
// the stage's clients, naming itself and that block.
func TestASplitControllerStageWaitingForWorkThatNeedsItsClientsRefuses(t *testing.T) {
	definitions := splitDefinitions()
	definitions[2].Dependencies = []string{"machine-guest"}
	resolved, err := reconciliation.ResolveRequirements(definitions)
	if err != nil {
		t.Fatal(err)
	}
	_, err = precursorService("artifact-server-mirror", "mirror-files-lab").dependOnController(resolved)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.unsupported" || reported[0].Message !=
		"block controller-mirrored-clients waits for block artifact-server-lab, which runs only after the controller stage it waits for" {
		t.Fatalf("closure refusal = %+v", reported)
	}
}

// splitLocator answers as a split stage does: only the block that holds the
// asked tool locates it, and the other refuses as a closure without it.
type splitLocator struct {
	toolLocator
	holder map[string]string
	asked  []controller.InstalledTool
}

func (l splitLocator) LocateTool(ctx context.Context, view prerequisites.StorageView, block reconciliation.Block, proved BlockEvidence, tool controller.InstalledTool) (string, error) {
	if l.holder[tool.Executable] != block.ID {
		return "", failure("controller.state", "the "+tool.Executable+" of release "+tool.Version+" is not installed on this controller", "run bootwright apply --stage controller")
	}
	return l.toolLocator.LocateTool(ctx, view, block, proved, tool)
}

func (l splitLocator) Apply(ctx context.Context, execution Execution) (Result, error) {
	if execution.Block.Stage != reconciliation.StageController {
		for _, tool := range l.asked {
			located, err := execution.LocateTool(ctx, tool)
			if err != nil {
				located = "refused " + firstCode(err)
			}
			l.answers.Store(tool.Executable, located)
		}
	}
	return l.testCapability.Apply(ctx, execution)
}

// A block locates a client in whichever controller block of a split stage
// published it, and a client neither block holds refuses with the last one's
// refusal.
func TestALookupFindsAToolInEitherControllerBlock(t *testing.T) {
	kubectl := controller.InstalledTool{Kind: "kubectl", Compatibility: "kubernetes", Version: "1.37.2", Executable: "kubectl"}
	absent := controller.InstalledTool{Kind: "helm", Compatibility: "kubernetes", Version: "4.3.1", Executable: "helm"}
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
		stagedDefinition("controller-mirrored-clients", reconciliation.StageController),
		stagedDefinition("artifacts", reconciliation.StageInfrastructureServices),
	})
	locator := splitLocator{toolLocator: toolLocator{testCapability: h.capability, answers: &sync.Map{}},
		holder: map[string]string{installer.Executable: "controller-mirrored-clients", kubectl.Executable: "controller-prerequisites"},
		asked:  []controller.InstalledTool{installer, kubectl, absent}}
	h.service.capabilities = testResolver{capability: locator}
	h.capability.outcomeFor = map[string]Result{
		"controller-prerequisites":    {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"area":"packages"}`)},
		"controller-mirrored-clients": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(stageProof)},
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	for executable, want := range map[string]string{
		installer.Executable: "/located/controller-mirrored-clients/true/done/" + stageProof + "/openshift-install",
		kubectl.Executable:   `/located/controller-prerequisites/true/done/{"area":"packages"}/kubectl`,
		absent.Executable:    "refused controller.state",
	} {
		if located := locator.answer(t, executable); located != want {
			t.Errorf("%s located %q, want %q", executable, located, want)
		}
	}
}

// The controller blocks are taken in the order they wait for each other, not
// the order a capability lists them, so a split stage listed clients first
// still runs its host packages before the mirror and the clients after it.
func TestASplitControllerStageListedInReverseKeepsItsOrder(t *testing.T) {
	definitions := splitDefinitions()
	definitions[1].Dependencies = []string{"controller-prerequisites"}
	definitions[0], definitions[1] = definitions[1], definitions[0]
	resolved, err := reconciliation.ResolveRequirements(definitions)
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
	ids := []string{}
	for _, block := range plan.Blocks {
		ids = append(ids, block.ID)
	}
	if !slices.Equal(ids[:4], []string{"controller-prerequisites", "artifact-server-mirror", "mirror-files-lab", "controller-mirrored-clients"}) {
		t.Fatalf("plan order = %v", ids)
	}
	if waits := dependencies(t, plan)["artifact-server-mirror"]; !slices.Equal(waits, []string{"controller-prerequisites"}) {
		t.Fatalf("the mirror's server waits for %v", waits)
	}
}

// A precursor of a later controller block must wait for an earlier one: a
// mirror's server that the host packages block itself waits for would run
// before the packages it needs, so the plan refuses.
func TestAPrecursorOfALaterControllerBlockMustWaitForAnEarlierOne(t *testing.T) {
	definitions := splitDefinitions()
	definitions[0].Requires = []reconciliation.ObjectRef{{Kind: "ArtifactServer", Object: "mirror", Family: "managed-service"}}
	resolved, err := reconciliation.ResolveRequirements(definitions)
	if err != nil {
		t.Fatal(err)
	}
	_, err = precursorService("artifact-server-mirror", "mirror-files-lab").dependOnController(resolved)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.unsupported" || reported[0].Message !=
		"block controller-mirrored-clients waits for block artifact-server-mirror, which runs only after the controller stage it waits for" {
		t.Fatalf("refusal = %+v", reported)
	}
}
