package clusteraccess

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// Selection is the resolved answer to "which resources does this command act
// on", computed once from the CLI selection axes (the resolved scope target
// plus the raw --clusters value) so command handlers stop re-deriving the
// render set, the work set, the resolved root names, and the readiness
// sub-scopes independently and inconsistently.
//
// The organizing rule is RENDER against the render set, MUTATE only the work
// set:
//
//   - RenderState is the scoped desired state to render and plan over. It keeps
//     render-reference StorageClusters — a managed cluster pulled in only by a
//     selected container cluster's data-foundation attachment — so the
//     attachment renders. apply, destroy, check, render, and status all read it.
//   - The work set (WorkMachines / WorkStorageClusters, from ApplyWorkObjects)
//     never follows the data-foundation attachment edge, so a render reference
//     is never in it. apply gates provisioning by it and destroy gates teardown
//     by it; because both verbs read the SAME work set the render reference can
//     be rendered against but is provisioned or torn down by neither. This is
//     what makes the apply/destroy work-object handling symmetric and what keeps
//     a scoped container-cluster destroy from tearing down the external Ceph it
//     merely attaches to.
//
// Selection deliberately does NOT carry the host-trust connected-machine set
// (workflow.ApplyTaskConnectedMachines): that genuinely depends on the planned
// task graph, which only exists after planning, and lives the wrong side of the
// clusteraccess→converge boundary. WorkMachines is the upstream input that makes
// that later set correct.
type Selection struct {
	// Target is the resolved scope name the selection was computed for
	// (infra/clusters/container-cluster/storage-cluster/all/fabric/... or a
	// synthetic through-<phase>); Scope is the raw --clusters value.
	Target string
	Scope  string

	// Active reports whether a --clusters narrowing is in force. When false the
	// run is whole-target: the root/work fields are empty and RenderState is the
	// full input state.
	Active bool

	// ContainerRoots and StorageRoots are the cluster roots named DIRECTLY in
	// --clusters (never render-reference pull-ins). AllRoots is the two
	// concatenated, the value the destroy executor gate (ownership cleanup
	// scope) consumes.
	ContainerRoots []string
	StorageRoots   []string
	AllRoots       []string

	// RenderState is the scoped desired state to render and plan over
	// (render-inclusive; keeps render-reference storage).
	RenderState v1alpha1.State

	// WorkMachines and WorkStorageClusters are the genuine work objects (from
	// ApplyWorkObjects, which does not follow the data-foundation edge): the
	// Machines a phase SSHes/provisions and the StorageClusters a phase
	// provisions or tears down. Both are nil when !Active (no narrowing).
	WorkMachines        map[string]bool
	WorkStorageClusters map[string]bool
}

// Resolve computes every "what resources to consider" representation once. It
// resolves the --clusters value to cluster roots (per the scope target's name
// space), filters the render state with the apply-variant filter (so the
// work/render-reference split is honored uniformly), and derives the work set.
// It returns an error only for an unknown cluster name or a --clusters value
// the target does not support — the same errors the individual resolvers
// raised before. Shared-service conflict validation stays a handler concern so
// each verb keeps its own message.
//
// state must be the FULL effective desired state; Resolve derives the scoped
// views from it (scoped-validation still loads full state for exactly this
// reason).
func Resolve(state v1alpha1.State, target, scope string) (Selection, error) {
	sel := Selection{Target: target, Scope: scope}
	if strings.TrimSpace(scope) == "" {
		sel.RenderState = state
		return sel, nil
	}
	containerRoots, storageRoots, err := resolveSelectionRoots(state, target, scope)
	if err != nil {
		return Selection{}, err
	}
	renderState, err := ScopeStateForApply(state, target, scope)
	if err != nil {
		return Selection{}, err
	}
	workMachines, workStorageClusters := ApplyWorkObjects(state, containerRoots, storageRoots)
	sel.Active = true
	sel.ContainerRoots = containerRoots
	sel.StorageRoots = storageRoots
	sel.AllRoots = append(append([]string{}, containerRoots...), storageRoots...)
	sel.RenderState = renderState
	sel.WorkMachines = workMachines
	sel.WorkStorageClusters = workStorageClusters
	return sel, nil
}

// resolveSelectionRoots partitions a non-empty --clusters value into directly
// named ContainerCluster and StorageCluster roots, dispatching on the scope
// target's name space exactly as ScopeState does: the single-kind targets use
// their own validators, the cluster-root families use the partitioning
// resolver, and an unsupported target rejects --clusters.
func resolveSelectionRoots(state v1alpha1.State, target, scope string) (container, storage []string, err error) {
	switch target {
	case "container-cluster", "add-ons":
		names, err := ClusterNamesForTarget(state, scope)
		if err != nil {
			return nil, nil, err
		}
		return names, nil, nil
	case "storage-cluster":
		names, err := StorageClusterNamesForTarget(state, scope)
		if err != nil {
			return nil, nil, err
		}
		return nil, names, nil
	}
	if isClusterRootScopeTarget(target) {
		return ClusterRootNamesForTarget(state, scope)
	}
	// Mirror ScopeState's unsupported-target error so Resolve is a drop-in.
	return nil, nil, unsupportedClusterScopeError(target)
}

// StorageWorkNames returns the StorageCluster names this selection genuinely
// provisions or tears down, sorted. It is the single source for both apply's
// applyTarget.StorageClusterNames and destroy's storage-teardown restriction:
// directly-named storage roots, never render-reference pull-ins. Empty (but
// non-nil) when a --clusters selection names no storage cluster, so a
// container-only selection provisions/tears down zero storage clusters; nil
// when no --clusters selection is in force (provision/tear down all).
func (s Selection) StorageWorkNames() []string {
	if !s.Active {
		return nil
	}
	out := append([]string{}, s.StorageRoots...)
	sort.Strings(out)
	return out
}

// IsStorageWorkObject reports whether a StorageCluster name is a genuine
// provisioning/teardown target rather than a render reference. With no
// --clusters selection every declared cluster is a work object.
func (s Selection) IsStorageWorkObject(name string) bool {
	if !s.Active {
		return true
	}
	return s.WorkStorageClusters[name]
}
