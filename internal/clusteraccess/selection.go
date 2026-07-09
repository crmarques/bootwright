package clusteraccess

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type Selection struct {
	Target string
	Scope  string

	Active bool

	ContainerRoots []string
	StorageRoots   []string
	AllRoots       []string

	RenderState v1alpha1.State

	WorkMachines        map[string]bool
	WorkStorageClusters map[string]bool
}

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
	return nil, nil, unsupportedClusterScopeError(target)
}

func (s Selection) StorageWorkNames() []string {
	if !s.Active {
		return nil
	}
	out := append([]string{}, s.StorageRoots...)
	sort.Strings(out)
	return out
}
