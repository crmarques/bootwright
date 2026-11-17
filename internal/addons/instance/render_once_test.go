package instance

import (
	"context"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestAPlanRendersEachInstanceOnceAndFreezesWhatItsRenderGives(t *testing.T) {
	registry := fixtureRegistry(t)
	catalog := authoredCatalog([]string{"widget"}, executableAuthoredAddOn(t, "widget"))
	asked := 0
	facts := func(c api.Catalog, cluster string) addons.ClusterFacts { asked++; return authoredFacts(c, cluster) }
	plan, err := New(nil, registry, facts).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	if asked != 1 {
		t.Fatalf("one instance's plan rendered it %d times", asked)
	}
	selected, _ := addons.Instances(catalog, registry)
	request, _, err := Render(selected[0], authoredFacts(catalog, "cluster"), "controller")
	if err != nil {
		t.Fatal(err)
	}
	if data, err := request.Canonical(); err != nil || !reflect.DeepEqual([]byte(plan.Definitions[0].Request), data) {
		t.Fatalf("the plan froze %s, its render gives %s (%v)", plan.Definitions[0].Request, data, err)
	}
}
