package converge

import (
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestEveryApplyTaskKindMapsToADestroyKind(t *testing.T) {
	kinds := workflow.ApplyTaskKinds()
	if len(kinds) == 0 {
		t.Fatal("no apply task kinds registered; the guard would pass vacuously")
	}
	for _, kind := range kinds {
		if destroyKindForApplyTaskKind(kind) == "" {
			t.Errorf("apply task kind %q maps to no destroy kind: its convergence record would survive the teardown that removes the resource, so the next apply would classify it as already converged and skip re-provisioning it", kind)
		}
	}
}
