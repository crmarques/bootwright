package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// otherBuilds are the three ways a build differs from the one that registered
// an operation: other automation, another version or commit, and a source
// modified since, which no recorded identity tells from the registering build.
func otherBuilds() []struct {
	name     string
	switchTo func(*harness)
} {
	return []struct {
		name     string
		switchTo func(*harness)
	}{
		{"other automation", func(h *harness) { h.service.automation = testAutomation{digest: strings.Repeat("9", 64)} }},
		{"another commit", func(h *harness) { h.service.options.Executable = Executable{Version: "devel", Commit: "1234567"} }},
		{"a modified build", func(h *harness) { h.service.options.Executable.Modified = true }},
	}
}

// Another build continues an incomplete operation whose blocks it freezes the
// same: the verb repeats under the same operation, runs this build's
// automation, and the record keeps naming the build that registered it. The
// proof plans, so it never runs inside the exclusive transaction.
func TestAnotherBuildContinuesAnOperationWhoseBlocksItFreezesTheSame(t *testing.T) {
	for _, build := range otherBuilds() {
		for _, test := range []struct {
			verb      reconciliation.Verb
			start     func(*testing.T, *harness)
			continued func(*harness) []string
			proving   func(*harness) *func()
		}{
			{reconciliation.Apply, failedApply, func(h *harness) []string { return h.capability.applies }, func(h *harness) *func() { return &h.capability.planned }},
			{reconciliation.Destroy, unknownRemoval, func(h *harness) []string { return h.capability.observes }, func(h *harness) *func() { return &h.capability.removed }},
		} {
			t.Run(build.name+" "+string(test.verb), func(t *testing.T) {
				h := newHarness(t, "artifact-server-lab")
				test.start(t, h)
				registered, _ := durableOperation(t, h)
				build.switchTo(h)
				proved := 0
				*test.proving(h) = func() {
					proved++
					if h.workspace.transacting {
						t.Error("the blocks were planned again inside the exclusive transaction")
					}
				}
				h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
				before := len(test.continued(h))
				var result *OperationResult
				var err error
				if test.verb == reconciliation.Destroy {
					result, err = h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
				} else {
					result, err = apply(h)
				}
				if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) || len(test.continued(h)) == before {
					t.Fatalf("the continuation under %s = %+v (%v)", build.name, result, err)
				}
				if proved == 0 {
					t.Fatalf("the continuation under %s proved its blocks without planning them", build.name)
				}
				continued, _ := durableOperation(t, h)
				if continued.Executable != registered.Executable || continued.AutomationDigest != registered.AutomationDigest {
					t.Fatalf("the continuation re-stamped the record: %+v %q, registered %+v %q",
						continued.Executable, continued.AutomationDigest, registered.Executable, registered.AutomationDigest)
				}
				if test.verb == reconciliation.Destroy {
					if ran := resolutionBuild(t, h, registered.ID, "artifact-server-lab"); ran != buildOf(h, h.capability.definitions[0].ContentDigest) {
						t.Fatalf("the removal's resolution under %s names %+v", build.name, ran)
					}
				}
			})
		}
	}
}

// Another build refuses a continuation whose blocks it would freeze
// differently or cannot plan again, naming the block or the reason, with no
// effect and no record changed.
func TestAnotherBuildRefusesToContinueABlockItWouldFreezeDifferently(t *testing.T) {
	const take = "take back what this operation owns under this executable with bootwright destroy --context lab, or install bootwright devel (abcdef1), which registered it"
	const install = "install bootwright devel (abcdef1), which registered this operation, and run bootwright setup"
	const differently = "this executable would freeze the block artifact-server-lab differently from the plan this operation froze"
	const unplannable = "this executable cannot plan this operation again to prove it freezes the same blocks: "
	for _, test := range []struct {
		name        string
		verb        reconciliation.Verb
		start       func(*testing.T, *harness)
		change      func(*harness)
		message     string
		remediation string
	}{
		{"moved content digest", reconciliation.Apply, failedApply, func(h *harness) {
			h.capability.definitions[0].ContentDigest = strings.Repeat("8", 64)
		}, differently, take},
		{"moved request", reconciliation.Apply, failedApply, func(h *harness) {
			h.capability.definitions[0].Request = json.RawMessage(`{"name":"artifact-server-lab","tuned":true}`)
		}, differently, take},
		{"unknown apply under a moved content digest", reconciliation.Apply, applyLeftUnknown, func(h *harness) {
			h.capability.definitions[0].ContentDigest = strings.Repeat("8", 64)
		}, differently, take},
		{"unknown apply under a moved request", reconciliation.Apply, applyLeftUnknown, func(h *harness) {
			h.capability.definitions[0].Request = json.RawMessage(`{"name":"artifact-server-lab","tuned":true}`)
		}, differently, take},
		{"added block", reconciliation.Apply, failedApply, func(h *harness) {
			h.capability.definitions = append(h.capability.definitions, definition("extra"))
		}, "this executable would freeze the block extra differently from the plan this operation froze", take},
		{"plan fails", reconciliation.Apply, failedApply, func(h *harness) {
			h.capability.planErr = failure("lifecycle.state", "the planner refused", "")
		}, unplannable + "the planner refused", take},
		{"removal consumes more", reconciliation.Destroy, unknownRemoval, func(h *harness) {
			h.capability.consumes = map[string][]string{"artifact-server-lab": {reconciliation.AuthorizationDataLoss}}
		}, differently, install},
		{"unreadable request", reconciliation.Destroy, unknownRemoval, func(h *harness) {
			h.capability.removalErr = failure("lifecycle.state", "the frozen request has an unsupported version", "")
		}, unplannable + "the request the block artifact-server-lab froze cannot be read by this executable: the frozen request has an unsupported version", install},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			test.start(t, h)
			h.service.automation = testAutomation{digest: strings.Repeat("9", 64)}
			test.change(h)
			refusedUnchanged(t, h, test.verb, test.message, test.remediation)
		})
	}
}

// Status offers another build the continuation its blocks admit, and where the
// build would freeze a block differently only the removal that supersedes.
func TestStatusOffersAnotherBuildTheContinuationItsBlocksAdmit(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "artifact-server-lab")
	failedApply(t, h)
	same, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	for _, build := range otherBuilds() {
		t.Run(build.name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			failedApply(t, h)
			build.switchTo(h)
			status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil || !slices.Equal(status.NextSteps, same.NextSteps) {
				t.Fatalf("status under %s = %+v (%v), want steps %q", build.name, status.NextSteps, err, same.NextSteps)
			}
			h.capability.definitions[0].ContentDigest = strings.Repeat("8", 64)
			status, err = h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil || !slices.Equal(status.NextSteps, []string{"bootwright destroy --context lab"}) {
				t.Fatalf("status under %s with a moved content digest = %+v (%v)", build.name, status.NextSteps, err)
			}
		})
	}
}

// Only the words a block is presented in may differ between two plans that
// freeze the same effects; every other member names the block that moved.
func TestDifferingBlockIgnoresOnlyPresentationWords(t *testing.T) {
	plan := func(t *testing.T, change func(*reconciliation.BlockDefinition), extra ...reconciliation.BlockDefinition) reconciliation.Plan {
		t.Helper()
		first := definition("a")
		if change != nil {
			change(&first)
		}
		built, err := reconciliation.NewPlan(reconciliation.Apply, append([]reconciliation.BlockDefinition{first}, extra...))
		if err != nil {
			t.Fatal(err)
		}
		return built
	}
	for _, test := range []struct {
		name   string
		change func(*reconciliation.BlockDefinition)
		block  string
	}{
		{"identical", nil, ""},
		{"a description", func(d *reconciliation.BlockDefinition) { d.Description = "serve it differently" }, ""},
		{"impacts", func(d *reconciliation.BlockDefinition) { d.Impacts = []string{"serve-a"} }, ""},
		{"a group description", func(d *reconciliation.BlockDefinition) { d.Groups[0].Description = "fetch the image" }, ""},
		{"no exclusive against empty", func(d *reconciliation.BlockDefinition) { d.Exclusive = []string{} }, ""},
		{"a request", func(d *reconciliation.BlockDefinition) { d.Request = json.RawMessage(`{"name":"a","x":1}`) }, "a"},
		{"a content digest", func(d *reconciliation.BlockDefinition) { d.ContentDigest = strings.Repeat("8", 64) }, "a"},
		{"an implementation", func(d *reconciliation.BlockDefinition) { d.Implementation = "artifact-server-nginx-v2" }, "a"},
		{"an object", func(d *reconciliation.BlockDefinition) { d.Object = "other" }, "a"},
		{"a stage", func(d *reconciliation.BlockDefinition) { d.Stage = reconciliation.StageClusters }, "a"},
		{"an exclusive claim", func(d *reconciliation.BlockDefinition) { d.Exclusive = []string{"port:80"} }, "a"},
		{"a consumed authorization", func(d *reconciliation.BlockDefinition) { d.Consumes = []string{reconciliation.AuthorizationDataLoss} }, "a"},
		{"a group id", func(d *reconciliation.BlockDefinition) { d.Groups[0].ID = "other-group" }, "a"},
		{"a group machine", func(d *reconciliation.BlockDefinition) { d.Groups[0].Machines = []string{"elsewhere"} }, "a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			block, differs := differingBlock(plan(t, nil), plan(t, test.change))
			if differs != (test.block != "") || block != test.block {
				t.Fatalf("differing block = %q, %t, want %q", block, differs, test.block)
			}
		})
	}
	t.Run("a dependency", func(t *testing.T) {
		later := func(requires ...reconciliation.ObjectRef) reconciliation.BlockDefinition {
			d := definition("b")
			d.Requires = requires
			return d
		}
		block, differs := differingBlock(plan(t, nil, later()), plan(t, nil, later(reconciliation.ObjectRef{Kind: "ArtifactServer", Object: "a"})))
		if !differs || block != "b" {
			t.Fatalf("differing block = %q, %t, want b", block, differs)
		}
	})
	t.Run("a block only the fresh plan holds", func(t *testing.T) {
		block, differs := differingBlock(plan(t, nil), plan(t, nil, definition("extra")))
		if !differs || block != "extra" {
			t.Fatalf("differing block = %q, %t, want extra", block, differs)
		}
	})
	t.Run("a block only the frozen plan holds", func(t *testing.T) {
		block, differs := differingBlock(plan(t, nil, definition("missing")), plan(t, nil))
		if !differs || block != "missing" {
			t.Fatalf("differing block = %q, %t, want missing", block, differs)
		}
	})
}

// A continuation under another build that carries no proof refuses, and one
// under the build that registered the operation needs none.
func TestAContinuationWithNoSameBlocksProofRefusesUnderAnotherBuild(t *testing.T) {
	const message = "this executable has not proved it freezes the blocks this operation froze"
	const take = "take back what this operation owns under this executable with bootwright destroy --context lab, or install bootwright devel (abcdef1), which registered it"
	ctx := context.Background()
	verify := func(t *testing.T, h *harness) error {
		t.Helper()
		operation, _ := durableOperation(t, h)
		return h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
			return h.service.verifyContinuation(ctx, view, operation, sameBlocksProof{})
		})
	}
	for _, build := range otherBuilds() {
		t.Run(build.name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			failedApply(t, h)
			build.switchTo(h)
			reported := diagnostics.Of(verify(t, h))
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != message || reported[0].Remediation != take {
				t.Fatalf("an unproved continuation under %s reported %+v", build.name, reported)
			}
		})
	}
	t.Run("the registering build", func(t *testing.T) {
		h := newHarness(t, "artifact-server-lab")
		failedApply(t, h)
		if err := verify(t, h); err != nil {
			t.Fatalf("a continuation under the registering build refused: %v", err)
		}
	})
}

// registeringBuild is the build the harness registers its operations under,
// running a block of contentDigest.
func registeringBuild(contentDigest string) operationstore.Build {
	return operationstore.Build{AutomationDigest: testAutomaton, ContentDigest: contentDigest,
		Executable: operationstore.Executable{Version: "devel", Commit: "abcdef1"}}
}

// buildOf is the build the harness's service now runs a block of
// contentDigest under.
func buildOf(h *harness, contentDigest string) operationstore.Build {
	executable := h.service.options.Executable
	return operationstore.Build{AutomationDigest: h.service.automation.CatalogDigest(), ContentDigest: contentDigest,
		Executable: operationstore.Executable{Version: executable.Version, Commit: executable.Commit}, Modified: executable.Modified}
}

func recordsOf(h *harness) *operationstore.Store {
	return operationstore.New(h.workspace.area, func() time.Time { return time.Unix(0, 0) })
}

// attemptBuilds is the build each attempt of one block names, in order.
func attemptBuilds(t *testing.T, h *harness, id, block string) []operationstore.Build {
	t.Helper()
	ctx := context.Background()
	record, err := recordsOf(h).Block(ctx, id, block)
	if err != nil {
		t.Fatal(err)
	}
	builds := []operationstore.Build{}
	for number := 1; number <= record.Attempts; number++ {
		attempt, err := recordsOf(h).Attempt(ctx, id, block, number)
		if err != nil || attempt.Build == nil {
			t.Fatalf("attempt %d of %s names no build (%v)", number, block, err)
		}
		builds = append(builds, *attempt.Build)
	}
	return builds
}

// resolutionBuild is the build the last resolution of one block's last
// attempt names.
func resolutionBuild(t *testing.T, h *harness, id, block string) operationstore.Build {
	t.Helper()
	attempt, err := recordsOf(h).LastAttempt(context.Background(), id, block)
	if err != nil {
		t.Fatal(err)
	}
	return resolutionBuildOf(t, h, id, block, attempt)
}

// resolutionBuildOf is the build the last resolution of one attempt of a block
// names, so a retry that follows the resolution does not hide it.
func resolutionBuildOf(t *testing.T, h *harness, id, block string, attempt int) operationstore.Build {
	t.Helper()
	resolution, found, err := recordsOf(h).LastResolution(context.Background(), id, block, attempt)
	if err != nil || !found || resolution.Build == nil {
		t.Fatalf("the resolution of %s names no build (%t, %v)", block, found, err)
	}
	return *resolution.Build
}

// stoppedPair applies two blocks, each under its own content digest:
// artifact-server-lab completes and artifact-server-web, which runs after it,
// ends as outcome says.
func stoppedPair(t *testing.T, outcome Result) *harness {
	t.Helper()
	lab, web := definition("artifact-server-lab"), definition("artifact-server-web")
	web.ContentDigest = strings.Repeat("d", 64)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{lab, web})
	h.capability.outcomeFor = map[string]Result{"artifact-server-web": outcome}
	if _, err := apply(h); err == nil {
		t.Fatal("the apply a continuation starts from completed")
	}
	if _, states := durableOperation(t, h); states["artifact-server-lab"] != reconciliation.BlockDone {
		t.Fatalf("the apply a continuation starts from left %v", states)
	}
	h.capability.outcomeFor = nil
	return h
}

// The record of an operation another build continues keeps naming the build
// that registered it, so each attempt names the build it ran under: the retry
// of a failed block names the other build, the attempt before it the
// registering one, and a block that was done gains no attempt.
func TestAnotherBuildRetriesAFailedBlockUnderItsOwnBuild(t *testing.T) {
	for _, build := range otherBuilds() {
		t.Run(build.name, func(t *testing.T) {
			h := stoppedPair(t, Result{Outcome: reconciliation.OutcomeFailed})
			registered, _ := durableOperation(t, h)
			done, stopped := h.capability.definitions[0].ContentDigest, h.capability.definitions[1].ContentDigest
			build.switchTo(h)
			result, err := apply(h)
			if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) {
				t.Fatalf("the continuation under %s = %+v (%v)", build.name, result, err)
			}
			if other := buildOf(h, stopped); other == registeringBuild(stopped) {
				t.Fatalf("%s runs under the registering build", build.name)
			}
			if builds := attemptBuilds(t, h, registered.ID, "artifact-server-web"); !slices.Equal(builds, []operationstore.Build{registeringBuild(stopped), buildOf(h, stopped)}) {
				t.Fatalf("the attempts of the failed block under %s name %+v", build.name, builds)
			}
			if builds := attemptBuilds(t, h, registered.ID, "artifact-server-lab"); !slices.Equal(builds, []operationstore.Build{registeringBuild(done)}) {
				t.Fatalf("the attempts of the done block under %s name %+v", build.name, builds)
			}
			continued, _ := durableOperation(t, h)
			if continued.Executable != registered.Executable || continued.AutomationDigest != registered.AutomationDigest {
				t.Fatalf("the continuation re-stamped the record: %+v %q", continued.Executable, continued.AutomationDigest)
			}
		})
	}
}

// Another build resolves an unknown apply first and names itself in the
// resolution. A resolution that proves the block done lets the same
// invocation complete the operation; one that proves no effect is followed by
// that invocation's retry, which names that build too.
func TestAnotherBuildResolvesAnUnknownApplyThenRetriesIt(t *testing.T) {
	for _, build := range otherBuilds() {
		t.Run(build.name+" completed", func(t *testing.T) {
			h := stoppedPair(t, Result{Outcome: reconciliation.OutcomeUnknown})
			registered, _ := durableOperation(t, h)
			stopped := h.capability.definitions[1].ContentDigest
			build.switchTo(h)
			h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
			result, err := apply(h)
			if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) {
				t.Fatalf("the continuation under %s = %+v (%v)", build.name, result, err)
			}
			if ran := resolutionBuild(t, h, registered.ID, "artifact-server-web"); ran != buildOf(h, stopped) {
				t.Fatalf("the resolution under %s names %+v", build.name, ran)
			}
			if builds := attemptBuilds(t, h, registered.ID, "artifact-server-web"); !slices.Equal(builds, []operationstore.Build{registeringBuild(stopped)}) {
				t.Fatalf("a block its resolution proved done was attempted again: %+v", builds)
			}
		})
		t.Run(build.name+" no effect", func(t *testing.T) {
			h := stoppedPair(t, Result{Outcome: reconciliation.OutcomeUnknown})
			registered, _ := durableOperation(t, h)
			stopped := h.capability.definitions[1].ContentDigest
			build.switchTo(h)
			h.capability.observations = []Observation{{Effect: reconciliation.EffectNoEffect}}
			unproved, err := recordsOf(h).LastAttempt(context.Background(), registered.ID, "artifact-server-web")
			if err != nil {
				t.Fatal(err)
			}
			result, err := apply(h)
			if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) {
				t.Fatalf("the continuation under %s = %+v (%v)", build.name, result, err)
			}
			if ran := resolutionBuildOf(t, h, registered.ID, "artifact-server-web", unproved); ran != buildOf(h, stopped) {
				t.Fatalf("the resolution under %s names %+v", build.name, ran)
			}
			if builds := attemptBuilds(t, h, registered.ID, "artifact-server-web"); !slices.Equal(builds, []operationstore.Build{registeringBuild(stopped), buildOf(h, stopped)}) {
				t.Fatalf("the attempts of the resolved block under %s name %+v", build.name, builds)
			}
		})
	}
}
