package lifecycle

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// toolLocator answers as the controller stage's capability does, naming the
// stage block, the setup evidence and the stage's proof it was asked with.
// Every other block asks it from inside its own call, as an install does,
// because the operation's records are open only for that long, and each
// answer is kept under the call and the block that asked.
type toolLocator struct {
	*testCapability
	answers *sync.Map
}

func (toolLocator) LocateTool(_ context.Context, view prerequisites.StorageView, block reconciliation.Block, proved BlockEvidence, tool controller.InstalledTool) (string, error) {
	return "/located/" + block.ID + "/" + view.State.Receipt.CatalogDigest + "/" + string(proved.State) + "/" + string(proved.Evidence) + "/" + tool.Executable, nil
}

func (l toolLocator) ask(ctx context.Context, call string, execution Execution) {
	if execution.Block.Stage == reconciliation.StageController {
		return
	}
	located, err := execution.LocateTool(ctx, installer)
	if err != nil {
		located = "refused " + firstCode(err)
	}
	l.answers.Store(call+":"+execution.Block.ID, located)
}

func (l toolLocator) Apply(ctx context.Context, execution Execution) (Result, error) {
	l.ask(ctx, "apply", execution)
	return l.testCapability.Apply(ctx, execution)
}

func (l toolLocator) Observe(ctx context.Context, execution Execution) (Observation, error) {
	l.ask(ctx, "observe", execution)
	return l.testCapability.Observe(ctx, execution)
}

func (l toolLocator) Destroy(ctx context.Context, execution Execution) (Result, error) {
	l.ask(ctx, "destroy", execution)
	return l.testCapability.Destroy(ctx, execution)
}

func (l toolLocator) answer(t *testing.T, key string) string {
	t.Helper()
	located, asked := l.answers.Load(key)
	if !asked {
		t.Fatalf("%s never asked where its clients are", key)
	}
	return located.(string)
}

var installer = controller.InstalledTool{Kind: "openshift-install", Compatibility: "openshift", Version: "4.21.15", Executable: "openshift-install"}

// stageProof is what the harness's controller stage proves when it is applied,
// so an answer shows which record the lookup was handed.
const stageProof = `{"area":"applied"}`

var locatedFromTheStageProof = "/located/controller-prerequisites/" + strings.Repeat("b", 64) + "/done/" + stageProof + "/openshift-install"

func stagedToolHarness(t *testing.T) (*harness, toolLocator) {
	t.Helper()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
		stagedDefinition("artifacts", reconciliation.StageInfraComponents),
	})
	locator := toolLocator{testCapability: h.capability, answers: &sync.Map{}}
	h.service.capabilities = testResolver{capability: locator}
	h.capability.outcomeFor = map[string]Result{
		"controller-prerequisites": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(stageProof)},
	}
	return h, locator
}

func executionOf(t *testing.T, h *harness, block string, resolution bool) Execution {
	t.Helper()
	for _, execution := range h.capability.executions {
		if execution.Block.ID == block && (execution.Resolution != 0) == resolution {
			return execution
		}
	}
	t.Fatalf("%s was never executed (resolution %t)", block, resolution)
	return Execution{}
}

// A block locates the clients it runs through the controller stage block its
// plan froze, answered by that block's capability over this attempt's setup
// evidence and what that block proved, so it runs what its own context's stage
// installed.
func TestAnAttemptLocatesItsClientsThroughTheControllerStage(t *testing.T) {
	h, locator := stagedToolHarness(t)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if located := locator.answer(t, "apply:artifacts"); located != locatedFromTheStageProof {
		t.Fatalf("located %q, want %q", located, locatedFromTheStageProof)
	}
}

// A resolution is handed the same lookup, because an observation runs the
// same clients its attempt ran.
func TestAResolutionLocatesItsClientsThroughTheControllerStage(t *testing.T) {
	h, locator := stagedToolHarness(t)
	h.capability.outcomeFor["artifacts"] = Result{Outcome: reconciliation.OutcomeUnknown}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown attempt reported success")
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if located := locator.answer(t, "observe:artifacts"); located != locatedFromTheStageProof {
		t.Fatalf("located %q, want %q", located, locatedFromTheStageProof)
	}
}

// A removal runs the clients its apply's stage installed, so it is answered
// from what that block proved in the apply it takes back, never from the
// removal's own record of the stage, which retains and proves no closure.
func TestARemovalLocatesItsClientsThroughWhatTheStageProvedInItsApply(t *testing.T) {
	h, locator := stagedToolHarness(t)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if located := locator.answer(t, "destroy:artifacts"); located != locatedFromTheStageProof {
		t.Fatalf("located %q, want %q", located, locatedFromTheStageProof)
	}
}

// A plan without a controller stage installed nothing to run, and a stage
// whose capability cannot answer is not a place to search instead.
func TestALookupWithoutAnAnsweringControllerStageRefuses(t *testing.T) {
	cases := map[string]struct {
		harness     func(*testing.T) *harness
		code        string
		remediation string
	}{
		"no controller stage": {
			harness:     func(t *testing.T) *harness { return newHarness(t, "artifacts") },
			code:        "controller.state",
			remediation: "run bootwright apply --stage controller",
		},
		"a stage that cannot locate": {
			harness: func(t *testing.T) *harness {
				return newPlannedHarness(t, []reconciliation.BlockDefinition{
					stagedDefinition("controller-prerequisites", reconciliation.StageController),
					stagedDefinition("artifacts", reconciliation.StageInfraComponents),
				})
			},
			code:        "lifecycle.state",
			remediation: "install the executable that registered this operation",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := tc.harness(t)
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			located, err := executionOf(t, h, "artifacts", false).LocateTool(context.Background(), installer)
			if err == nil {
				t.Fatalf("a lookup answered %q", located)
			}
			if code := firstCode(err); code != tc.code {
				t.Fatalf("refusal code = %q, want %q", code, tc.code)
			}
			if remediation := diagnostics.Of(err)[0].Remediation; remediation != tc.remediation {
				t.Fatalf("remediation = %q, want %q", remediation, tc.remediation)
			}
		})
	}
}
