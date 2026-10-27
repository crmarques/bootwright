package lifecycle

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// anotherBuildPreview is a failed apply of four blocks: alpha done, bravo
// failed, charlie and delta pending behind bravo in a later stage.
func anotherBuildPreview(t *testing.T) *harness {
	t.Helper()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("alpha", reconciliation.StageInfrastructureServices),
		stagedDefinition("bravo", reconciliation.StageInfrastructureServices, "alpha"),
		stagedDefinition("charlie", reconciliation.StageClusters, "bravo"),
		stagedDefinition("delta", reconciliation.StageClusters, "bravo"),
	})
	applyChained(t, h, map[string]Result{"bravo": {Outcome: reconciliation.OutcomeFailed}})
	return h
}

// confirmationOf is the plan an apply presents before it asks. The harness's
// confirmer declines, so nothing runs.
func confirmationOf(t *testing.T, h *harness, stages ...string) PlanResult {
	t.Helper()
	presented := len(h.presenter.presented)
	h.confirmer.decline = true
	defer func() { h.confirmer.decline = false }()
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, Stages: stages}); err == nil {
		t.Fatal("a declined apply went on")
	}
	if len(h.presenter.presented) != presented+1 {
		t.Fatalf("the apply presented %d plans", len(h.presenter.presented)-presented)
	}
	return h.presenter.presented[presented]
}

func namesBuild(got *AnotherBuild, registeredBy string, steps ...int) bool {
	return got != nil && got.RegisteredBy == registeredBy && slices.Equal(got.Steps, steps)
}

// A continuation another build proved it freezes the same blocks previews the
// build that registered it and the plan positions of the steps this build
// runs: those not done, and with a stage selection those it does not leave
// out. The apply's confirmation shows the very same.
func TestAContinuationUnderAnotherBuildNamesTheStepsThisBuildRuns(t *testing.T) {
	ctx := context.Background()
	for _, build := range otherBuilds() {
		for _, test := range []struct {
			name   string
			stages []string
			steps  []int
		}{
			{"every stage", nil, []int{2, 3, 4}},
			{"a stage selection", []string{"infrastructure-services"}, []int{2}},
		} {
			t.Run(build.name+" "+test.name, func(t *testing.T) {
				h := anotherBuildPreview(t)
				build.switchTo(h)
				preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Stages: test.stages})
				if err != nil || !preview.Continuation || preview.Finalizes {
					t.Fatalf("the preview = %+v (%v)", preview, err)
				}
				if !namesBuild(preview.AnotherBuild, "devel (abcdef1)", test.steps...) {
					t.Fatalf("the preview named %+v, want steps %v (%+v)", preview.AnotherBuild, test.steps, preview.Steps)
				}
				if shown := confirmationOf(t, h, test.stages...); !namesBuild(shown.AnotherBuild, "devel (abcdef1)", test.steps...) {
					t.Fatalf("the confirmation named %+v, want steps %v", shown.AnotherBuild, test.steps)
				}
			})
		}
	}
}

// destroyConfirmationOf is the plan a destroy presents before it asks. The
// harness's confirmer declines, so nothing runs.
func destroyConfirmationOf(t *testing.T, h *harness) PlanResult {
	t.Helper()
	presented := len(h.presenter.presented)
	h.confirmer.decline = true
	defer func() { h.confirmer.decline = false }()
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName}); err == nil {
		t.Fatal("a declined destroy went on")
	}
	if len(h.presenter.presented) != presented+1 {
		t.Fatalf("the destroy presented %d plans", len(h.presenter.presented)-presented)
	}
	return h.presenter.presented[presented]
}

// A removal another build continues names the build that registered it in
// its preview and in its confirmation, as an apply's continuation does; a
// fresh removal that supersedes an incomplete apply begins under this build
// and names none.
func TestARemovalUnderAnotherBuildNamesTheBuildThatRegisteredIt(t *testing.T) {
	for _, build := range otherBuilds() {
		t.Run(build.name+" continuation", func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			unknownRemoval(t, h)
			build.switchTo(h)
			preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
			if err != nil || preview.Verb != string(reconciliation.Destroy) || !preview.Continuation || !namesBuild(preview.AnotherBuild, "devel (abcdef1)", 1) {
				t.Fatalf("the removal's preview under %s = %+v (%v)", build.name, preview, err)
			}
			if shown := destroyConfirmationOf(t, h); shown.Verb != string(reconciliation.Destroy) || !shown.Continuation || !namesBuild(shown.AnotherBuild, "devel (abcdef1)", 1) {
				t.Fatalf("the removal's confirmation under %s = %+v", build.name, shown)
			}
		})
		t.Run(build.name+" fresh removal of an incomplete apply", func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			failedApply(t, h)
			build.switchTo(h)
			if shown := destroyConfirmationOf(t, h); shown.Verb != string(reconciliation.Destroy) || shown.Continuation || shown.AnotherBuild != nil {
				t.Fatalf("the fresh removal under %s confirmed %+v", build.name, shown)
			}
		})
	}
}

// Only a continuation another build proved says so: not one under the build
// that registered it, not a preview whose proof refuses, not one that only
// completes a finalization, and not a plan that begins an operation.
func TestAPreviewNamesNoOtherBuildWhereNoContinuationRunsUnderOne(t *testing.T) {
	ctx := context.Background()
	preview := func(t *testing.T, h *harness) *PlanResult {
		t.Helper()
		result, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	t.Run("the registering build", func(t *testing.T) {
		h := anotherBuildPreview(t)
		if got := preview(t, h); !got.Continuation || got.AnotherBuild != nil {
			t.Fatalf("the registering build previewed %+v", got.AnotherBuild)
		}
		if shown := confirmationOf(t, h); !shown.Continuation || shown.AnotherBuild != nil {
			t.Fatalf("the registering build confirmed %+v", shown.AnotherBuild)
		}
	})
	t.Run("a refused proof", func(t *testing.T) {
		for _, build := range otherBuilds() {
			h := anotherBuildPreview(t)
			build.switchTo(h)
			h.capability.definitions[0].ContentDigest = "8888888888888888888888888888888888888888888888888888888888888888"
			if got := preview(t, h); !got.Continuation || got.AnotherBuild != nil {
				t.Fatalf("the preview under %s whose proof refuses named %+v", build.name, got.AnotherBuild)
			}
		}
	})
	t.Run("a finalization only", func(t *testing.T) {
		for _, build := range otherBuilds() {
			h, _ := laggingApply(t)
			build.switchTo(h)
			if got := preview(t, h); !got.Finalizes || got.AnotherBuild != nil {
				t.Fatalf("the finalization preview under %s named %+v", build.name, got.AnotherBuild)
			}
		}
	})
	t.Run("a fresh apply", func(t *testing.T) {
		for _, build := range otherBuilds() {
			h := newHarness(t, "alpha")
			build.switchTo(h)
			if got := preview(t, h); got.Continuation || got.AnotherBuild != nil {
				t.Fatalf("the fresh plan under %s named %+v", build.name, got.AnotherBuild)
			}
		}
	})
	t.Run("a fresh destroy", func(t *testing.T) {
		for _, build := range otherBuilds() {
			h := newHarness(t, "alpha")
			if _, err := apply(h); err != nil {
				t.Fatal(err)
			}
			build.switchTo(h)
			got := preview(t, h)
			if got.Verb != string(reconciliation.Destroy) || got.Continuation || got.AnotherBuild != nil {
				t.Fatalf("the fresh removal under %s previewed %+v", build.name, got)
			}
		}
	})
}

// A continuation whose steps all are done or left out by the stage selection
// runs nothing under the build in hand, so it names none.
func TestAPreviewNamesNoOtherBuildWhenNoStepRunsUnderIt(t *testing.T) {
	steps := []PlanStep{{State: string(reconciliation.BlockDone)}, {State: string(reconciliation.BlockPending), Selection: StepNotSelected}}
	if got := anotherBuildOf(operationstore.Operation{}, steps); got != nil {
		t.Fatalf("a continuation with nothing to run named %+v", got)
	}
}

// Status says whether the build in hand is another build than the one that
// registered the operation.
func TestStatusSaysWhetherAnotherBuildRegisteredTheOperation(t *testing.T) {
	ctx := context.Background()
	statusOf := func(t *testing.T, h *harness) *LifecycleSummary {
		t.Helper()
		status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
		if err != nil || status.Lifecycle == nil {
			t.Fatalf("status = %+v (%v)", status, err)
		}
		return status.Lifecycle
	}
	t.Run("the registering build", func(t *testing.T) {
		if got := statusOf(t, anotherBuildPreview(t)); got.AnotherBuild {
			t.Fatal("status under the registering build reported another build")
		}
	})
	for _, build := range otherBuilds() {
		t.Run(build.name, func(t *testing.T) {
			h := anotherBuildPreview(t)
			build.switchTo(h)
			if got := statusOf(t, h); !got.AnotherBuild || got.Executable != "devel (abcdef1)" {
				t.Fatalf("status under %s reported %+v", build.name, got)
			}
		})
	}
}
