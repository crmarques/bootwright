package lifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A bounded run is never planned, so its refusal of a reserved key ends with
// the command that carries the run again, while a plan's refusal keeps its
// own text, which ends with the plan.
func TestABoundedRefusalNamesAReservedKeyAndItsRetry(t *testing.T) {
	definitions := []reconciliation.BlockDefinition{{Kind: "Machine", Object: "metal", Request: []byte(`{"a":{"__ansible_unsafe":1}}`)}}
	const retry = "bootwright machine stop --context lab --name metal"
	reported := diagnostics.Of(RefuseBoundedTemplateDelimiters("lab", retry, definitions))
	if len(reported) != 1 || reported[0].Code != "api.value" || reported[0].Object == nil || reported[0].Object.Name != "metal" ||
		!strings.Contains(reported[0].Message, `the ansible-core reserved key "__ansible_unsafe" in a`) ||
		!strings.HasSuffix(reported[0].Remediation, ", then run "+retry) {
		t.Fatalf("the bounded refusal reported %+v", reported)
	}
	planned := diagnostics.Of(RefuseTemplateDelimiters("lab", definitions))
	want := `remove every key named "__ansible_type", "__ansible_unsafe" or "__ansible_vault" from the desired-state values Machine/metal is planned from, ` +
		"import them with bootwright context update --name lab --input-dir <dir>, then run bootwright plan --context lab"
	if len(planned) != 1 || planned[0].Message != reported[0].Message || planned[0].Remediation != want {
		t.Fatalf("the plan's refusal reported %+v, want remediation %q", planned, want)
	}
}

// authorizedApply applies the harness's plan with the data-loss token.
func authorizedApply(h *harness) error {
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", Authorizations: []string{"data-loss"}, SkipConfirmation: true})
	return err
}

// An unknown block no resolution has observed is observed by the next apply,
// so status's remedy names that exact apply, with its context and the tokens
// its frozen plan consumes.
func TestAnUnobservedBlockNamesTheCommandThatObservesIt(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("alpha")})
	h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}}
	if err := authorizedApply(h); err == nil {
		t.Fatal("an apply whose block is unknown completed")
	}
	want := "run bootwright apply --context lab --authorize data-loss, which observes it before anything else starts"
	if got := blockUnresolved(t, h, "alpha"); got.Remedy != want {
		t.Fatalf("status names %+v, want remedy %q", got, want)
	}
}

// A resolution that leaves an apply's block unknown names the exact apply that
// observes it again.
func TestAnUnresolvedBlockNamesItsExactRetry(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("alpha")})
	h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}}
	if err := authorizedApply(h); err == nil {
		t.Fatal("an apply whose block is unknown completed")
	}
	h.capability.outcomeFor = nil
	reported := diagnostics.Of(authorizedApply(h))
	if len(reported) == 0 || reported[0].Code != "lifecycle.unknown" ||
		!strings.HasSuffix(reported[0].Remediation, ", then repeat bootwright apply --context lab --authorize data-loss to observe it again") {
		t.Fatalf("the resolution reported %+v", reported)
	}
	// A removal resolves the apply's block by its own check, so the command
	// that observes it again is that removal, with its own plan's tokens.
	h.capability.consumes = map[string][]string{"alpha": {"data-loss"}}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", Authorizations: []string{"data-loss"}, SkipConfirmation: true})
	reported = diagnostics.Of(err)
	if len(reported) == 0 || reported[0].Code != "lifecycle.unknown" ||
		!strings.HasSuffix(reported[0].Remediation, ", then repeat bootwright destroy --context lab --authorize data-loss to observe it again") {
		t.Fatalf("the removal reported %+v", reported)
	}
}

// Only an apply's own attempt carries the command that continues it: a stage
// block's carries the apply and every token its frozen plan consumes, and a
// resolution and a destroy attempt carry none.
func TestAStageAttemptCarriesItsContinuation(t *testing.T) {
	stage := destructive("clients")
	stage.Stage = reconciliation.StageController
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{stage})
	h.capability.outcomeFor = map[string]Result{"clients": {Outcome: reconciliation.OutcomeUnknown}}
	if err := authorizedApply(h); err == nil {
		t.Fatal("an apply whose block is unknown completed")
	}
	h.capability.outcomeFor = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Outcome: reconciliation.OutcomeChanged}}
	if err := authorizedApply(h); err != nil {
		t.Fatalf("the resolving apply = %v", err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("destroy = %+v", diagnostics.Of(err))
	}
	var continuations []string
	for _, execution := range h.capability.executions {
		continuations = append(continuations, execution.Continuation)
	}
	want := []string{"bootwright apply --context lab --authorize data-loss", "", ""}
	if strings.Join(continuations, "|") != strings.Join(want, "|") {
		t.Fatalf("the attempts carried %q, want %q", continuations, want)
	}
}

// The execution foundation can refuse a controller stage block on entry, before
// the stage capability runs, so only the lifecycle can name the stage command
// that settles it, with the context.
func TestAStageBlockRefusedOnEntryNamesTheStageCommand(t *testing.T) {
	failure := &prerequisites.ScopedFailure{Code: "controller.conflict", Message: "a native package transaction prevents coherent dependency execution", Correction: "Restore the qualified host execution foundation", Command: "repeat this command"}
	for _, test := range []struct {
		name  string
		stage reconciliation.Stage
		want  string
	}{
		{"controller stage", reconciliation.StageController, "Restore the qualified host execution foundation, then run bootwright apply --stage controller --context lab."},
		{"another stage", reconciliation.StageMachines, "Restore the qualified host execution foundation, then repeat this command."},
	} {
		block := destructive("clients")
		block.Stage = test.stage
		h := newPlannedHarness(t, []reconciliation.BlockDefinition{block})
		h.service.guard = refusingGuard{failure}
		reported := diagnostics.Of(authorizedApply(h))
		if len(reported) == 0 || reported[0].Code != "controller.conflict" || reported[0].Remediation != test.want {
			t.Errorf("%s: reported %+v, want remediation %q", test.name, reported, test.want)
		}
	}
}

// The foundation can also refuse the entry of the resolution that observes an
// apply's unknown controller stage block, and that refusal names the stage
// command too, because the observation it refused is the apply's own.
func TestAStageResolutionRefusedOnEntryNamesTheStageCommand(t *testing.T) {
	failure := &prerequisites.ScopedFailure{Code: "controller.conflict", Message: "a native package transaction prevents coherent dependency execution", Correction: "Restore the qualified host execution foundation", Command: "repeat this command"}
	block := destructive("clients")
	block.Stage = reconciliation.StageController
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{block})
	h.capability.outcomeFor = map[string]Result{"clients": {Outcome: reconciliation.OutcomeUnknown}}
	if err := authorizedApply(h); err == nil {
		t.Fatal("an apply whose stage block is unknown completed")
	}
	h.capability.outcomeFor = nil
	h.service.guard = refusingGuard{failure}
	reported := diagnostics.Of(authorizedApply(h))
	want := "Restore the qualified host execution foundation, then run bootwright apply --stage controller --context lab."
	if len(reported) == 0 || reported[0].Code != "controller.conflict" || reported[0].Remediation != want {
		t.Fatalf("the resolution reported %+v, want remediation %q", reported, want)
	}
}

// A destroy that meets the foundation's refusal on a controller stage block's
// entry keeps the failure's own remedy: an incomplete destroy refuses the
// stage apply, so naming it would name a command that cannot run.
func TestADestroyRefusedOnAStageBlockEntryKeepsItsOwnRemedy(t *testing.T) {
	failure := &prerequisites.ScopedFailure{Code: "controller.conflict", Message: "a native package transaction prevents coherent dependency execution", Correction: "Restore the qualified host execution foundation", Command: "repeat this command"}
	block := destructive("clients")
	block.Stage = reconciliation.StageController
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{block})
	if err := authorizedApply(h); err != nil {
		t.Fatalf("the stage apply = %+v", diagnostics.Of(err))
	}
	guard := &admittingThenRefusingGuard{admit: 1, err: failure}
	h.service.guard = guard
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	reported := diagnostics.Of(err)
	want := "Restore the qualified host execution foundation, then repeat this command."
	if guard.refused == 0 || len(reported) == 0 || reported[0].Code != "controller.conflict" || reported[0].Remediation != want {
		t.Fatalf("the destroy reported %+v after %d refusals, want remediation %q", reported, guard.refused, want)
	}
}

type admittingThenRefusingGuard struct {
	admit   int
	refused int
	err     error
}

func (g *admittingThenRefusingGuard) WithPython(_ context.Context, _ prerequisites.BundleArea, _ prerequisites.ExecutionRequirement, use func(prerequisites.PythonLaunch, func() error) error) error {
	if g.admit > 0 {
		g.admit--
		return use(prerequisites.PythonLaunch{Loader: "/loader"}, func() error { return nil })
	}
	g.refused++
	return g.err
}
