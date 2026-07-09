package hooks

import (
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

func TargetClusters(state v1alpha1.State, addon v1alpha1.ClusterAddon, boundCluster string, hook v1alpha1.ClusterAddonHook, inputs []v1alpha1.ClusterAddonBindingInput) (containers []string, storage []string) {
	c := newClusterSet()
	s := newClusterSet()
	target := hook.Target
	if target.BoundCluster {
		c.add(boundCluster)
	}
	for _, name := range target.Clusters {
		classifyCluster(state, name, c, s)
	}
	for _, name := range target.Machines {
		classifyMachineOwners(state, name, c, s)
	}
	if target.FromInput != nil {
		refKind, refName, ok := resolveInputRef(addon, hook, inputs, *target.FromInput)
		if ok {
			classifyRef(state, refKind, refName, c, s)
		}
	}
	return c.list(), s.list()
}

func resolveInputRef(addon v1alpha1.ClusterAddon, hook v1alpha1.ClusterAddonHook, inputs []v1alpha1.ClusterAddonBindingInput, from v1alpha1.ClusterAddonHookInputTarget) (refKind, name string, ok bool) {
	accepted, found := acceptedInput(addon, from.Input)
	if !found {
		return "", "", false
	}
	property, found := accepted.Schema.Properties[from.Property]
	if !found || property.RefKind == "" {
		return "", "", false
	}
	for _, input := range inputs {
		if input.Name != from.Input {
			continue
		}
		ref := addoninputs.LocalObjectReferenceValue(input.Values, from.Property)
		if ref.Name == "" {
			return "", "", false
		}
		return property.RefKind, ref.Name, true
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
		for _, node := range cluster.Spec.Hosts {
			if node.MachineRef.Name == machine {
				containers.add(cluster.Metadata.Name)
			}
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			if node.MachineRef.Name == machine {
				storage.add(cluster.Metadata.Name)
			}
		}
	}
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
