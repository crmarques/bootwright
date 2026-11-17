package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type recordingPlanner struct {
	decisionPlanner
	planned, admitted []*reconciliation.PreparationScope
	refuse            error
}

func (p *recordingPlanner) planFrom(ctx context.Context, view View, state *compilation.State, scope *reconciliation.PreparationScope) (reconciliation.Plan, capabilityBinding, error) {
	p.planned = append(p.planned, scope)
	return p.decisionPlanner.planFrom(ctx, view, state, scope)
}

func (p *recordingPlanner) refuseUnsupported(state *compilation.State, scope *reconciliation.PreparationScope) error {
	p.admitted = append(p.admitted, scope)
	if p.refuse != nil {
		return p.refuse
	}
	return p.decisionPlanner.refuseUnsupported(state, scope)
}

func decideThrough(t *testing.T, h *harness, d decider) (operationPlan, error) {
	t.Helper()
	ctx := context.Background()
	var decided operationPlan
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) (err error) {
		decided, err = d.decide(ctx, view, reconciliation.Apply, reconciliation.Selection{})
		return err
	})
	return decided, err
}

func TestTheDecisionHandsItsPreparationScopeToEveryPlanningPort(t *testing.T) {
	t.Parallel()
	h := prepareHarness(t, "inventory")
	scope, err := h.service.requestedPreparation(requestPreparation())
	if err != nil {
		t.Fatal(err)
	}
	for name, given := range map[string]*reconciliation.PreparationScope{"an ordinary apply": nil, "a preparation": scope} {
		t.Run(name, func(t *testing.T) {
			planner := &recordingPlanner{decisionPlanner: h.service}
			d := h.service.decider(given)
			d.planner = planner
			decided, err := decideThrough(t, h, d)
			if err != nil || !decided.fresh {
				t.Fatalf("decision = %+v (%v), want a fresh apply", decided, err)
			}
			if len(planner.planned) != 1 || planner.planned[0] != given || len(planner.admitted) != 1 || planner.admitted[0] != given {
				t.Fatalf("the planner saw scopes %v and %v, want %v once each", planner.planned, planner.admitted, given)
			}
			if !reflect.DeepEqual(decided.plan.Preparation, given) {
				t.Fatalf("the frozen plan records preparation %+v, want %+v", decided.plan.Preparation, given)
			}
		})
	}
}

func TestTheDecisionRefusesWhatItsPlanningPortRefusesBeforePlanning(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "machine")
	refused := errors.New("the planning port refuses")
	planner := &recordingPlanner{decisionPlanner: h.service, refuse: refused}
	d := h.service.decider(nil)
	d.planner = planner
	if decided, err := decideThrough(t, h, d); !errors.Is(err, refused) || decided.fresh {
		t.Fatalf("decision = %+v (%v), want the planning port's refusal", decided, err)
	}
	if len(planner.planned) != 0 {
		t.Fatalf("the decision planned %d times after its refusal", len(planner.planned))
	}
}
