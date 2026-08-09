package steps

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
)

const (
	RefKindStorageExport    = v1alpha1.KindStorageExport
	RefKindStorageCluster   = v1alpha1.KindStorageCluster
	RefKindContainerCluster = v1alpha1.KindContainerCluster
	RefKindMachine          = v1alpha1.KindMachine
)

func SupportedTargetRefKinds() []string {
	return []string{RefKindStorageExport, RefKindStorageCluster, RefKindContainerCluster, RefKindMachine}
}

func TargetClusters(state v1alpha1.State, addon v1alpha1.ClusterAddon, boundCluster string, step v1alpha1.ClusterAddonStep, inputs []v1alpha1.ClusterAddonBindingInput) (containers []string, storage []string) {
	c := newClusterSet()
	s := newClusterSet()
	target := step.Target
	if target.BoundCluster != nil {
		c.add(boundCluster)
	}
	if target.Static != nil {
		for _, name := range target.Static.Clusters {
			classifyCluster(state, name, c, s)
		}
		for _, name := range target.Static.Machines {
			classifyMachineOwners(state, name, c, s)
		}
	}
	if target.FromInput != nil {
		refKind, refName, ok := resolveInputRef(addon, step, inputs, *target.FromInput)
		if ok {
			classifyRef(state, refKind, refName, c, s)
		}
	}
	return c.list(), s.list()
}

func StorageMutationTargets(state v1alpha1.State, addon v1alpha1.ClusterAddon, boundCluster string, step v1alpha1.ClusterAddonStep, inputs []v1alpha1.ClusterAddonBindingInput) ([]string, error) {
	if step.Playbook == "" {
		return nil, nil
	}
	target := step.Target
	modes := 0
	if target.BoundCluster != nil {
		modes++
	}
	if target.FromInput != nil {
		modes++
	}
	if target.Static != nil {
		modes++
	}
	if modes != 1 {
		return nil, fmt.Errorf("target must select exactly one of boundCluster, fromInput, or static")
	}
	storage := newClusterSet()
	if target.BoundCluster != nil {
		if !hasContainerCluster(state, boundCluster) {
			return nil, fmt.Errorf("target.boundCluster references unknown ContainerCluster %q", boundCluster)
		}
	}
	if target.Static != nil {
		if len(target.Static.Clusters) == 0 && len(target.Static.Machines) == 0 {
			return nil, fmt.Errorf("target.static resolves to no clusters or machines")
		}
		for _, name := range target.Static.Clusters {
			switch {
			case hasContainerCluster(state, name):
			case hasStorageCluster(state, name):
				storage.add(name)
			default:
				return nil, fmt.Errorf("target.static references unknown cluster %q", name)
			}
		}
		for _, name := range target.Static.Machines {
			if !hasMachine(state, name) {
				return nil, fmt.Errorf("target.static references unknown Machine %q", name)
			}
			addMachineStorageOwners(state, name, storage)
		}
	}
	if target.FromInput != nil {
		refKind, refName, ok := resolveInputRef(addon, step, inputs, *target.FromInput)
		if !ok {
			return nil, fmt.Errorf("target.fromInput %q does not resolve to a resource", target.FromInput.Input)
		}
		switch refKind {
		case RefKindStorageExport:
			cluster, ok := storageExportCluster(state, refName)
			if !ok {
				return nil, fmt.Errorf("target.fromInput %q references unknown StorageExport %q", target.FromInput.Input, refName)
			}
			if !hasStorageCluster(state, cluster) {
				return nil, fmt.Errorf("StorageExport/%s references unknown StorageCluster %q", refName, cluster)
			}
			storage.add(cluster)
		case RefKindStorageCluster:
			if !hasStorageCluster(state, refName) {
				return nil, fmt.Errorf("target.fromInput %q references unknown StorageCluster %q", target.FromInput.Input, refName)
			}
			storage.add(refName)
		case RefKindContainerCluster:
			if !hasContainerCluster(state, refName) {
				return nil, fmt.Errorf("target.fromInput %q references unknown ContainerCluster %q", target.FromInput.Input, refName)
			}
		case RefKindMachine:
			if !hasMachine(state, refName) {
				return nil, fmt.Errorf("target.fromInput %q references unknown Machine %q", target.FromInput.Input, refName)
			}
			addMachineStorageOwners(state, refName, storage)
		default:
			return nil, fmt.Errorf("target.fromInput %q resolves unsupported kind %q", target.FromInput.Input, refKind)
		}
	}
	out := storage.list()
	sort.Strings(out)
	return out, nil
}

func resolveInputRef(addon v1alpha1.ClusterAddon, step v1alpha1.ClusterAddonStep, inputs []v1alpha1.ClusterAddonBindingInput, from v1alpha1.ClusterAddonStepInputTarget) (refKind, name string, ok bool) {
	accepted, found := acceptedInput(addon, from.Input)
	if !found {
		return "", "", false
	}
	if accepted.ResourceRef == nil {
		return "", "", false
	}
	for _, input := range inputs {
		if input.Name != from.Input {
			continue
		}
		ref := addoninputs.LocalObjectReferenceValue(input)
		if ref.Name == "" {
			return "", "", false
		}
		return accepted.ResourceRef.Kind, ref.Name, true
	}
	return "", "", false
}

func classifyRef(state v1alpha1.State, refKind, name string, containers, storage *clusterSet) {
	switch refKind {
	case RefKindStorageExport:
		for _, export := range state.StorageExports {
			if export.Metadata.Name == name {
				storage.add(export.Spec.StorageClusterRef.Name)
			}
		}
	case RefKindStorageCluster:
		storage.add(name)
	case RefKindContainerCluster:
		containers.add(name)
	case RefKindMachine:
		classifyMachineOwners(state, name, containers, storage)
	}
}

func classifyCluster(state v1alpha1.State, name string, containers, storage *clusterSet) {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			containers.add(name)
			return
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			storage.add(name)
			return
		}
	}
}

func classifyMachineOwners(state v1alpha1.State, machine string, containers, storage *clusterSet) {
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name == machine {
				containers.add(cluster.Metadata.Name)
			}
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name == machine {
				storage.add(cluster.Metadata.Name)
			}
		}
	}
}

func addMachineStorageOwners(state v1alpha1.State, machine string, storage *clusterSet) {
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name == machine {
				storage.add(cluster.Metadata.Name)
				break
			}
		}
	}
}

func hasContainerCluster(state v1alpha1.State, name string) bool {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return true
		}
	}
	return false
}

func hasStorageCluster(state v1alpha1.State, name string) bool {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return true
		}
	}
	return false
}

func hasMachine(state v1alpha1.State, name string) bool {
	for _, machine := range state.Machines {
		if machine.Metadata.Name == name {
			return true
		}
	}
	return false
}

func storageExportCluster(state v1alpha1.State, name string) (string, bool) {
	for _, export := range state.StorageExports {
		if export.Metadata.Name == name {
			return export.Spec.StorageClusterRef.Name, true
		}
	}
	return "", false
}

func acceptedInput(addon v1alpha1.ClusterAddon, name string) (v1alpha1.ClusterAddonAcceptedInput, bool) {
	for _, input := range addon.Spec.Accepts.Inputs {
		if input.Name == name {
			return input, true
		}
	}
	return v1alpha1.ClusterAddonAcceptedInput{}, false
}

type clusterSet struct {
	seen  map[string]bool
	order []string
}

func newClusterSet() *clusterSet { return &clusterSet{seen: map[string]bool{}} }

func (s *clusterSet) add(name string) {
	if name == "" || s.seen[name] {
		return
	}
	s.seen[name] = true
	s.order = append(s.order, name)
}

func (s *clusterSet) list() []string { return s.order }
