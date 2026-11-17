package installation

import (
	"context"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestCapabilitiesSharingOnePlanFreezeWhatEachFreezesAlone(t *testing.T) {
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		alone := planInput(verb)
		shared := planInput(verb)
		shared.Derived = lifecycle.NewDerivations()
		planners := map[string]func(lifecycle.PlanInput) (lifecycle.CapabilityPlan, error){
			"installation": func(input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
				return New(nil).WithMedia(labMedia()).Plan(context.Background(), input)
			},
			"package tree": func(input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
				return NewTree(nil).WithMedia(labMedia()).Plan(context.Background(), input)
			},
		}
		for round := range 2 {
			for name, plan := range planners {
				want, wantErr := plan(alone)
				got, err := plan(shared)
				if !reflect.DeepEqual(got, want) || (err == nil) != (wantErr == nil) {
					t.Fatalf("%s %s round %d: the shared plan = %+v (%v), alone %+v (%v)", verb, name, round, got, err, want, wantErr)
				}
			}
		}
	}
}
