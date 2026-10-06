package lifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// Every result of an operation that did not complete names the exact command
// its records call for, bound to the context it ran in and carrying the tokens
// that command's decision requires, and it is the command the terminal
// diagnostic names; a completed operation names none.
func TestEveryIncompleteResultNamesTheCommandItsRecordsCallFor(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		run   func(t *testing.T) (*OperationResult, error)
		state string
		next  string
		// remedy is the terminal diagnostic's remediation around the command,
		// or empty for a result that reports no diagnostic.
		remedy string
	}{
		{
			name: "a paused apply", state: "paused", next: "bootwright apply --context lab",
			run: func(t *testing.T) (*OperationResult, error) {
				h := newPlannedHarness(t, nestedDefinitions())
				return h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Stages: []string{"infra-components"}, SkipConfirmation: true})
			},
		},
		{
			name: "a failed apply that consumes an authorization", state: "failed", next: "bootwright apply --context lab --authorize data-loss",
			remedy: "continue it with bootwright apply --context lab --authorize data-loss",
			run: func(t *testing.T) (*OperationResult, error) {
				h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("alpha"), definition("bravo")})
				h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeFailed}}
				return h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: dataLoss(), SkipConfirmation: true})
			},
		},
		{
			name: "an unknown apply", state: "unknown", next: "bootwright apply --context lab",
			remedy: "resolve it with bootwright apply --context lab, which observes that effect before anything else starts, or take back what it started with bootwright destroy --context lab",
			run: func(t *testing.T) (*OperationResult, error) {
				h := newHarness(t, "alpha", "bravo")
				h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}}
				return h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			},
		},
		{
			name: "an interrupted apply", state: "running", next: "bootwright apply --context lab",
			remedy: "continue it with bootwright apply --context lab",
			run: func(t *testing.T) (*OperationResult, error) {
				h := newHarness(t, "alpha", "bravo")
				interrupted, cancel := context.WithCancel(ctx)
				h.capability.hold = func(string) { cancel() }
				return h.service.Apply(interrupted, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			},
		},
		{
			name: "a failed destroy", state: "failed", next: "bootwright destroy --context lab",
			remedy: "repeat bootwright destroy --context lab, which replaces it with a fresh removal of what it has not proved gone",
			run: func(t *testing.T) (*OperationResult, error) {
				h := newHarness(t, "alpha", "bravo")
				completeApply(t, h)
				h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
				return h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			},
		},
		{
			name: "a failed apply in another context", state: "failed", next: "bootwright apply --context lab-b",
			remedy: "continue it with bootwright apply --context lab-b",
			run: func(t *testing.T) (*OperationResult, error) {
				h := newHarness(t, "alpha")
				renamed(h, "lab-b")
				h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeFailed}}
				return h.service.Apply(ctx, ApplyRequest{ContextName: "lab-b", SkipConfirmation: true})
			},
		},
		{
			name: "a completed apply", state: "done",
			run: func(t *testing.T) (*OperationResult, error) {
				return newHarness(t, "alpha").service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.run(t)
			if result == nil || result.Receipt.State != tc.state || result.NextCommand != tc.next {
				t.Fatalf("result = %+v (%v), want state %s naming %q", result, err, tc.state, tc.next)
			}
			reported := diagnostics.Of(err)
			if tc.remedy == "" {
				if len(reported) != 0 {
					t.Fatalf("the %s reported %+v", tc.state, reported)
				}
				return
			}
			last := reported[len(reported)-1]
			if last.Remediation != tc.remedy || !strings.Contains(last.Remediation, tc.next) {
				t.Fatalf("the terminal diagnostic names %q, want %q", last.Remediation, tc.remedy)
			}
		})
	}
}
