package workflow

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func planTaskHashesByID(t *testing.T, target ApplyTarget, renderState, hashState v1alpha1.State) map[string]string {
	t.Helper()
	tasks, err := PlanApplyTasksCheckedWithHashState(target, renderState, hashState)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	out := map[string]string{}
	for i := range tasks {
		hash, err := ApplyTaskDesiredHash(tasks[i])
		if err != nil {
			t.Fatalf("desired hash for %s: %v", tasks[i].Entry.ID, err)
		}
		out[tasks[i].Entry.Kind+"/"+tasks[i].Entry.ID] = hash
	}
	return out
}

func TestEveryApplyTaskHashIsInvariantUnderClusterScoping(t *testing.T) {
	full, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("load advanced example: %v", err)
	}
	target := ApplyTarget{PhaseNames: []string{ApplyPhaseFabric, ApplyPhaseMachines, ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}}
	whole := planTaskHashesByID(t, target, full, full)
	if len(whole) == 0 {
		t.Fatal("the advanced example planned no apply task")
	}

	roots := []struct {
		name       string
		containers []string
		storage    []string
	}{
		{name: "dc1-metal-ocp", containers: []string{"dc1-metal-ocp"}},
		{name: "dc1-child-ocp", containers: []string{"dc1-child-ocp"}},
		{name: "ceph-storage", storage: []string{"ceph-storage"}},
	}
	for _, root := range roots {
		t.Run(root.name, func(t *testing.T) {
			scoped := stategraph.FilterStateToApplyClusterRoots(full, root.containers, root.storage)
			got := planTaskHashesByID(t, target, scoped, full)
			if len(got) == 0 {
				t.Fatalf("scoping to %s planned no task; the example no longer exercises this scope", root.name)
			}
			var divergent []string
			for id, hash := range got {
				if wholeHash, ok := whole[id]; ok && wholeHash != hash {
					divergent = append(divergent, id)
				}
			}
			sort.Strings(divergent)
			if len(divergent) > 0 {
				t.Errorf("task hash(es) %v depend on the --clusters render scope, so a scoped run after a whole-fleet apply reports false drift and diff --recorded exits 3 on a clean fleet; hash a projection of the unscoped hashState (DesiredHashVars or DesiredHashState) instead of the scoped State", divergent)
			}
		})
	}
}
