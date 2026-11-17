package agentinstall

import (
	"context"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/material"
)

func TestTheMediaAndInstallPlansShareOneDerivationOfTheirRequests(t *testing.T) {
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		alone := planInput(verb)
		shared := planInput(verb)
		shared.Derived = lifecycle.NewDerivations()
		planners := []func(lifecycle.PlanInput) (lifecycle.CapabilityPlan, error){
			func(input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
				return NewMedia(nil, material.AuthorizedKey).Plan(context.Background(), input)
			},
			func(input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
				return NewInstall(nil).Plan(context.Background(), input)
			},
		}
		for round := range 2 {
			for index, plan := range planners {
				want, wantErr := plan(alone)
				got, err := plan(shared)
				if wantErr != nil || err != nil || len(want.Definitions) == 0 || !reflect.DeepEqual(got, want) {
					t.Fatalf("%s planner %d round %d: the shared plan = %+v (%v), alone %+v (%v)", verb, index, round, got, err, want, wantErr)
				}
			}
		}
		derived := 0
		if _, err := lifecycle.Derived(shared, "containercluster/agentinstall.requests", func() (derivedRequests, error) { derived++; return derivedRequests{}, nil }); err != nil || derived != 0 {
			t.Fatalf("the plans left no shared derivation of their requests: derived again %d times (%v)", derived, err)
		}
	}
}
