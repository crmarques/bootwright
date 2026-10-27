package clients

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// An earlier build admitted a mirror ending in a bare '?' or '#' and froze it
// in the stage request. Recovering that block still proves it left nothing, so
// its operation can continue or be destroyed under this build.
func TestAMirrorAnEarlierBuildFrozeStillObservesAsNoEffect(t *testing.T) {
	for _, mirror := range []string{"https://mirror.example.test/helm?", "https://mirror.example.test/helm#", "https://mirror.example.test/helm?#"} {
		capability := stage(&fakeTools{}, newDnf(), &fakeSetup{}, &fakeInstaller{})
		capability.tools = bundlelocal.NewToolCatalog()
		downloads := field("downloads", api.MapValue(text("helmMirror", mirror)))
		block := planBlock(t, capability, stateOf(environment(downloads), machine(), cluster()), reconciliation.Apply)
		frozen := false
		for _, tool := range stageRequest(t, block).Tools {
			frozen = frozen || tool.Kind == "helm" && tool.Mirror == mirror
		}
		if !frozen {
			t.Fatalf("mirror %q: the stage request does not freeze it", mirror)
		}
		observation, err := capability.Observe(context.Background(), newRecorder(&fakeArea{sealed: true}).execution(t, block, hostState()))
		if err != nil || observation.Effect != reconciliation.EffectNoEffect {
			t.Errorf("mirror %q: observation = %q (%v), want %q", mirror, observation.Effect, err, reconciliation.EffectNoEffect)
		}
	}
}
