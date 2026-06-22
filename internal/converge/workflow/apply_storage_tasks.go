package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"github.com/crmarques/bootwright/internal/state/graph"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

type StorageAttachmentPlan struct {
	Cluster string
	Binding v1alpha1.ClusterAddonBinding
	Addon   v1alpha1.ClusterAddonBindingAddon
	Input   v1alpha1.ClusterAddonBindingInput
}

func planStorageAttachmentActivities(graph *ActivityGraph, state v1alpha1.State, installPhasePlanned bool, storageDepsByCluster map[string][]string) error {
	exportByName := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		exportByName[export.Metadata.Name] = export
	}
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		cluster := effect.Binding.Spec.ClusterRef.Name
		if !stateHasContainerCluster(state, cluster) {
			continue
		}
		exportRef := addoninputs.LocalObjectReferenceValue(effect.Input.Values, "exportRef")
		export, ok := exportByName[exportRef.Name]
		if !ok {
			continue
		}
		deps := []string{}
		if installPhasePlanned {
			deps = append(deps, "wait."+cluster)
		}
		deps = append(deps, storageDepsByCluster[export.Spec.StorageClusterRef.Name]...)
		deps = append(deps, "addon."+cluster+"."+effect.Addon.AddonRef.Name+".wait")
		id := "storageattachment." + cluster + "." + effect.Addon.AddonRef.Name + "." + effect.Input.Name + ".apply"
		attachmentPlan := StorageAttachmentPlan{Cluster: cluster, Binding: effect.Binding, Addon: effect.Addon, Input: effect.Input}
		if err := graph.Add(Activity{
			ID:                   id,
			Kind:                 ActivityKindStorageAttachmentApply,
			ExplicitDependencies: deps,
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:          id,
					Kind:        ApplyTaskKindStorageAttachmentApply,
					Label:       "storage attachment " + cluster + " " + effect.Addon.AddonRef.Name + "/" + effect.Input.Name + " apply",
					Cluster:     cluster,
					ClusterKind: ApplyClusterKindContainer,
					Status:      TaskStatusPending,
				},
				State:             stategraph.FilterStateToClusters(state, []string{cluster}),
				StorageAttachment: &attachmentPlan,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func storageTaskState(state v1alpha1.State, name string) v1alpha1.State {
	filtered := stategraph.FilterStateToStorageClusters(state, []string{name})
	filtered.ContainerClusters = nil
	return filtered
}

// storageClusterDesiredHashVars projects the desired state hashed for a
// StorageCluster's convergence record: the cluster's own spec, topology, nodes,
// and bound machines — but NOT its pools, filesystems, object gateways, exports, or
// placement policies. Those sub-objects are classified independently (each is its
// own object), so adding or changing one must never flip the StorageCluster itself
// to drift. The task still carries the full State for rendering and for the ceph
// operations loop; only the hash input is projected, mirroring the fabric
// DesiredHashVars pattern.
func storageClusterDesiredHashVars(state v1alpha1.State, name string) v1alpha1.State {
	s := storageTaskState(state, name)
	s.StoragePlacementPolicies = nil
	s.StoragePools = nil
	s.StorageFilesystems = nil
	s.StorageObjectGateways = nil
	s.StorageExports = nil
	return s
}

func managedOSMachineNames(state v1alpha1.State, cluster v1alpha1.StorageCluster) []string {
	if cluster.Spec.Ceph == nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		if node.MachineRef.Name == "" || seen[node.MachineRef.Name] {
			continue
		}
		seen[node.MachineRef.Name] = true
		machine, ok := stateview.Machine(state, node.MachineRef.Name)
		if ok && v1alpha1.MachineInstallsOS(machine) {
			names = append(names, node.MachineRef.Name)
		}
	}
	sort.Strings(names)
	return names
}

func storageClusterNodeCount(cluster v1alpha1.StorageCluster) int {
	if cluster.Spec.Ceph == nil {
		return 1
	}
	if count := len(cluster.Spec.Ceph.Topology.Hosts); count > 0 {
		return count
	}
	return 1
}
