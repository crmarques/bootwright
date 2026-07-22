package inventory

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func managedMachineOSInstallGroupsVars(state v1alpha1.State, paths PathOptions) []any {
	var out []any
	for _, cluster := range ManagedStorageClusters(state) {
		ci, ok := storageClusterInstall(state, cluster)
		if !ok {
			continue
		}
		var components []any
		for _, m := range ci.Machines {
			machine, ok := stateview.Machine(state, m.Name)
			if !ok || !v1alpha1.MachineInstallsOS(machine) {
				continue
			}
			component := machineComponentVars(state, ci, m, cluster.Metadata.Name, paths.SecretsDir)
			component["machineRef"] = managedOSTaskHost(state, m)
			if osInstall := machineOSInstallVars(state, ci, m, machine, cluster.Metadata.Name, paths); len(osInstall) > 0 {
				component["osInstall"] = osInstall
				boot := machineBootVarsWithISO(state, ci, m, cluster.Metadata.Name, fmt.Sprintf("os-%s-%s.iso", cluster.Metadata.Name, m.Name), machineOSRedfishVirtualMediaEndpoint(state, machine))
				if boot != nil {
					boot["readiness"] = map[string]any{
						"type": "none",
					}
					component["boot"] = boot
				}
				components = append(components, component)
			}
		}
		if len(components) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"name":               cluster.Metadata.Name,
			"storageClusterName": cluster.Metadata.Name,
			"networks":           clusterNetworksVars(state, ci),
			"components":         components,
		})
	}
	return out
}

func storageClusterInstall(state v1alpha1.State, cluster v1alpha1.StorageCluster) (v1alpha1.ClusterInstall, bool) {
	if cluster.Spec.Ceph == nil {
		return v1alpha1.ClusterInstall{}, false
	}
	seen := map[string]bool{}
	var machines []v1alpha1.InstallMachine
	var bindings []v1alpha1.MachineNetworkBinding
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if node.MachineRef.Name == "" || seen[node.MachineRef.Name] {
			continue
		}
		machine, ok := stateview.Machine(state, node.MachineRef.Name)
		if !ok {
			continue
		}
		seen[node.MachineRef.Name] = true
		machines = append(machines, stateview.InstallMachineFromMachine(machine))
		network := machine.Spec.Network.Config
		if network.NetworkConfigRef.Name != "" && network.AttachmentRef.Name != "" && machine.Spec.Substrate.ProviderRef.Name != "" {
			bindings = append(bindings, v1alpha1.MachineNetworkBinding{
				NetworkConfigRef: network.NetworkConfigRef,
				ProviderRef:      machine.Spec.Substrate.ProviderRef,
				AttachmentRef:    network.AttachmentRef,
			})
		}
	}
	sort.SliceStable(machines, func(i, j int) bool { return machines[i].Name < machines[j].Name })
	sort.SliceStable(bindings, func(i, j int) bool {
		if bindings[i].ProviderRef.Name != bindings[j].ProviderRef.Name {
			return bindings[i].ProviderRef.Name < bindings[j].ProviderRef.Name
		}
		return bindings[i].NetworkConfigRef.Name < bindings[j].NetworkConfigRef.Name
	})
	ci := v1alpha1.ClusterInstall{
		Metadata:        v1alpha1.Metadata{Name: cluster.Metadata.Name},
		NetworkBindings: bindings,
		Machines:        machines,
	}
	return ci, len(machines) > 0
}

func machineOSRedfishVirtualMediaEndpoint(state v1alpha1.State, machine v1alpha1.Machine) v1alpha1.ArtifactServerEndpointRef {
	profile, ok := stateview.MachineInstallProfile(state, machine.Spec.OS.InstallProfileRef.Name)
	if !ok || profile.Spec.Installer.Anaconda == nil {
		return v1alpha1.ArtifactServerEndpointRef{}
	}
	return profile.Spec.Installer.Anaconda.RedfishVirtualMedia.ArtifactServerEndpoint
}
