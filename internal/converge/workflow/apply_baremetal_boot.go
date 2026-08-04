package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func BareMetalFirstInstallClusters(bootProven []string, tasks []ApplyTask, state v1alpha1.State) []string {
	proven := map[string]bool{}
	for _, name := range bootProven {
		proven[name] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, task := range tasks {
		if task.Entry.Kind != ApplyTaskKindNodeBoot {
			continue
		}
		cluster := task.Entry.Cluster
		if proven[cluster] || seen[cluster] || !clusterHasBareMetalBoot(state, cluster) {
			continue
		}
		seen[cluster] = true
		out = append(out, cluster)
	}
	sort.Strings(out)
	return out
}

func FirstInstallManagedOSBareMetalClusters(objects []ObjectClassification, tasks []ApplyTask) []string {
	unrecorded := map[string]bool{}
	for _, o := range objects {
		if o.Kind == ApplyTaskKindManagedMachineOS && !o.Recorded() && o.Cluster != "" {
			unrecorded[o.Cluster] = true
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, task := range tasks {
		if task.Entry.Kind != ApplyTaskKindManagedMachineOS {
			continue
		}
		cluster := task.Entry.Cluster
		if !unrecorded[cluster] || seen[cluster] || !storageClusterHasBareMetalManagedOS(task.State, cluster) {
			continue
		}
		seen[cluster] = true
		out = append(out, cluster)
	}
	sort.Strings(out)
	return out
}

func storageClusterHasBareMetalManagedOS(state v1alpha1.State, clusterName string) bool {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name != clusterName {
			continue
		}
		for _, name := range managedOSMachineNames(state, cluster) {
			if applyMachineProviderType(state, name) == v1alpha1.ProvisionerBareMetal {
				return true
			}
		}
	}
	return false
}

func clusterHasBareMetalBoot(state v1alpha1.State, clusterName string) bool {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name != clusterName {
			continue
		}
		for _, host := range cluster.Spec.Nodes {
			switch applyMachineProviderType(state, host.MachineRef.Name) {
			case v1alpha1.ProvisionerKubeVirt, v1alpha1.ProvisionerVSphere:
				continue
			default:
				return true
			}
		}
	}
	return false
}
