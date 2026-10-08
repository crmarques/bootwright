package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// presentedRefusal is a verb run without --yes over records whose proof
// refuses it: its refusal, and how many plans it presented and how many times
// it asked before refusing.
type presentedRefusal struct {
	Refusal   verbReport `json:"refusal"`
	Presented int        `json:"presented"`
	Asked     int        `json:"asked"`
}

// refusedUnconfirmed runs verb without --yes and reports its refusal and what
// it presented and asked first. The refusal leaves everything as it was.
func refusedUnconfirmed(t *testing.T, h *harness, verb reconciliation.Verb) presentedRefusal {
	t.Helper()
	ctx := context.Background()
	before, presented, asked := untouchedOf(h), len(h.presenter.presented), h.confirmer.asked
	var err error
	if verb == reconciliation.Apply {
		_, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName})
	} else {
		_, err = h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
	}
	if err == nil {
		t.Fatalf("the %s went on", verb)
	}
	before.require(t, h)
	return presentedRefusal{Refusal: refusalReport(t, err), Presented: len(h.presenter.presented) - presented, Asked: h.confirmer.asked - asked}
}

// A continuation or resolution re-proves what it depends on from the records
// it decided from, before its plan is presented or confirmed, so a refused one
// presents nothing and asks nothing, and refuses with the re-proof's own
// diagnostic.
func TestARefusedContinuationPresentsNoPlan(t *testing.T) {
	for _, row := range refusedContinuations() {
		t.Run(row.name, func(t *testing.T) {
			report := refusedUnconfirmed(t, row.arrange(t), row.verb)
			if report.Presented != 0 || report.Asked != 0 {
				t.Fatalf("the refused %s presented %d plans and asked %d times", row.verb, report.Presented, report.Asked)
			}
			if report.Refusal.Code != row.code || report.Refusal.Message != row.message || len(report.Refusal.Causes) != 0 {
				t.Fatalf("the refused %s reported %+v, want %s %q", row.verb, report.Refusal, row.code, row.message)
			}
		})
	}
}

// An apply over an incomplete removal names that removal's continuation, or
// the replacement of a failed one, only where its proof admits it. Where that
// proof refuses, the apply names the exit the refusal names, followed by that
// refusal.
func TestAnApplyOverARefusedRemovalNamesTheRefusalsExit(t *testing.T) {
	ctx := context.Background()
	rows := []refusedContinuation{}
	for _, row := range refusedContinuations() {
		if row.verb == reconciliation.Destroy {
			rows = append(rows, row)
		}
	}
	rows = append(rows, refusedContinuation{
		name: "a failed removal on another host", verb: reconciliation.Destroy,
		arrange: func(t *testing.T) *harness {
			h := newPlannedHarness(t, chainedDefinitions())
			failedChainedRemoval(t, h)
			anotherHost(t, h)
			return h
		},
		code: "controller.identity", message: "this host is not the host the context is bound to",
	})
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			_, err := row.arrange(t).service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			refusal := diagnostics.Of(err)
			if len(refusal) != 1 || refusal[0].Code != row.code || refusal[0].Message != row.message {
				t.Fatalf("the removal's own refusal = %+v (%v)", refusal, err)
			}
			h := row.arrange(t)
			before := untouchedOf(h)
			_, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			want := []diagnostics.Diagnostic{
				{Severity: "error", Code: "lifecycle.state", Message: "an incomplete destroy must be continued before another operation", Remediation: refusal[0].Remediation},
				refusal[0],
			}
			if got := diagnostics.Of(err); !slices.Equal(got, want) {
				t.Fatalf("the apply reported %+v, want %+v", got, want)
			}
			before.require(t, h)
		})
	}
	t.Run("an unknown removal whose re-proof passes", func(t *testing.T) {
		h := newHarness(t, "alpha")
		unknownRemoval(t, h)
		_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
		want := []diagnostics.Diagnostic{{Severity: "error", Code: "lifecycle.state",
			Message: "an incomplete destroy must be continued before another operation", Remediation: "continue it with bootwright destroy --context lab"}}
		if got := diagnostics.Of(err); !slices.Equal(got, want) {
			t.Fatalf("the apply reported %+v, want %+v", got, want)
		}
	})
}

// withoutDefinition keeps the retained setup complete while its receipt holds
// no execution definition, which every block and registration needs.
func withoutDefinition(arrange func(*testing.T, *harness)) func(*testing.T) *harness {
	return func(t *testing.T) *harness {
		h := newHarness(t, "alpha")
		arrange(t, h)
		h.workspace.controller.State.Receipt.Definition = nil
		return h
	}
}

// Over an incomplete operation whose retained setup receipt holds no execution
// definition, status offers setup alone in place of every step that would run
// a block or register, the removal beside an incomplete apply included, and
// each verb it replaces refuses naming setup.
func TestStatusOffersSetupWhenTheReceiptHoldsNoExecutionDefinition(t *testing.T) {
	for name, row := range map[string]struct {
		arrange func(*testing.T) *harness
		gated   []reconciliation.Verb
	}{
		"an unknown removal": {arrange: withoutDefinition(unknownRemoval), gated: []reconciliation.Verb{reconciliation.Destroy}},
		"a failed apply":     {arrange: withoutDefinition(failedApply), gated: []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy}},
	} {
		t.Run(name, func(t *testing.T) {
			status, err := row.arrange(t).service.Status(context.Background(), StatusRequest{ContextName: testContextName})
			if err != nil || !slices.Equal(status.NextSteps, []string{"bootwright setup"}) {
				t.Fatalf("status = %+v (%v), want only bootwright setup", status, err)
			}
			for _, verb := range row.gated {
				requireSetupRefuses(t, row.arrange(t), verb)
			}
		})
	}
}

// What each edge of a continuation's proofs reports has a golden: the apply's
// refusal over a removal whose re-proof refuses, the refused continuation and
// the refused fresh removal that present nothing, and status over a receipt
// that holds no execution definition.
func TestContinuationProofsMatchTheirGolden(t *testing.T) {
	unboundRemoval := refusedContinuations()[1]
	unboundApply := refusedContinuations()[0]
	onAnotherHost := func(t *testing.T) *harness {
		h := newHarness(t, "alpha")
		completeApply(t, h)
		anotherHost(t, h)
		return h
	}
	reports := map[string]any{
		"the apply over an unbound unknown removal":                   verbOver(t, unboundRemoval.arrange(t), reconciliation.Apply),
		"the continuation of an unbound failed apply":                 refusedUnconfirmed(t, unboundApply.arrange(t), reconciliation.Apply),
		"the fresh destroy on another host":                           refusedUnconfirmed(t, onAnotherHost(t), reconciliation.Destroy),
		"status over a failed apply with no execution definition":     statusOf(t, withoutDefinition(failedApply)(t)),
		"status over an unknown removal with no execution definition": statusOf(t, withoutDefinition(unknownRemoval)(t)),
	}
	data, err := json.Marshal(reports)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "lifecycle-continuation-proofs", withoutIdentities(data), false)
}
