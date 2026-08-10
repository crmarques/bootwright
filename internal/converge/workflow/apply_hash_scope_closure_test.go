package workflow

import (
	"path/filepath"
	"sort"
	"strings"
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

func TestFabricTaskHashesTrackEffectiveRuntimeProxy(t *testing.T) {
	load := func(t *testing.T, proxyURL string) v1alpha1.State {
		t.Helper()
		state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "state", "desired", "testdata", "good", "001-sno-libvirt")})
		if err != nil {
			t.Fatalf("load libvirt fixture: %v", err)
		}
		state.Environments[0].Spec.InfraComponents.Proxies = append(state.Environments[0].Spec.InfraComponents.Proxies, v1alpha1.EnvironmentProxyComponent{
			Name:       "runtime",
			Management: v1alpha1.EnvironmentComponentExternal,
			Connection: &v1alpha1.EnvironmentProxyConnection{
				HTTPProxy: proxyURL,
				NoProxy:   []string{"storage.example.test"},
			},
		})
		state.Environments[0].Spec.ProxyFor.Bootwright = "runtime"
		return state
	}
	base := load(t, "http://proxy-a.example.test:3128")
	edited := load(t, "http://proxy-b.example.test:3128")
	target := ApplyTarget{PhaseNames: []string{ApplyPhaseFabric}}
	baseHashes := planTaskHashesByID(t, target, base, base)
	editedHashes := planTaskHashesByID(t, target, edited, edited)

	for _, taskClass := range []struct {
		kind     string
		idPrefix string
	}{
		{kind: ApplyTaskKindProvider, idPrefix: "provider."},
		{kind: ApplyTaskKindInfraComponentServices, idPrefix: "infra-component."},
	} {
		seen := 0
		for key, baseHash := range baseHashes {
			if !strings.HasPrefix(key, taskClass.kind+"/"+taskClass.idPrefix) {
				continue
			}
			seen++
			editedHash, ok := editedHashes[key]
			if !ok {
				t.Errorf("%s task %s disappeared after a runtime proxy edit", taskClass.kind, key)
				continue
			}
			if editedHash == baseHash {
				t.Errorf("%s hash %s did not move with the effective runtime proxy", taskClass.kind, key)
			}
		}
		if seen == 0 {
			t.Errorf("libvirt fixture planned no %s task with ID prefix %s", taskClass.kind, taskClass.idPrefix)
		}
	}
}
