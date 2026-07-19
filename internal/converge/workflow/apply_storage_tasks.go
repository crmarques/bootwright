package workflow

import (
	"encoding/json"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/graph"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func storageTaskState(state v1alpha1.State, name string) v1alpha1.State {
	filtered := stategraph.FilterStateToStorageClusters(state, []string{name})
	filtered.ContainerClusters = nil
	return filtered
}

func storageClusterDesiredHashVars(state v1alpha1.State, name string) v1alpha1.State {
	s := hashScopedState(storageTaskState(state, name))
	s.StoragePlacementPolicies = nil
	s.StoragePools = nil
	s.StorageFilesystems = nil
	s.StorageObjectGateways = nil
	s.StorageNFSExports = nil
	s.StorageExports = nil
	return s
}

func storageClusterStructuralHashVars(state v1alpha1.State, name string) v1alpha1.State {
	base := storageClusterDesiredHashVars(state, name)
	var clone v1alpha1.State
	data, err := json.Marshal(base)
	if err != nil {
		return base
	}
	if err := json.Unmarshal(data, &clone); err != nil {
		return base
	}
	clone.InfraProviders = nil
	clone.InfraComponents = nil
	clone.Environments = nil
	clone.Machines = nil
	clone.Secrets = nil
	clone.Entitlements = nil
	clone.MachineImages = nil
	clone.MachineInstallProfiles = nil
	clone.NetworkConfigs = nil
	clone.ProvisioningPlaybooks = nil
	for i := range clone.StorageClusters {
		ceph := clone.StorageClusters[i].Spec.Ceph
		if ceph == nil {
			continue
		}
		ceph.Topology.Hosts = nil
		ceph.Topology.OSDDrivegroups = nil
		ceph.Management = nil
		ceph.Services = nil
		ceph.Config = nil
		ceph.MgrModules = nil
		ceph.Monitoring = nil
		if ceph.IBM != nil {
			ceph.IBM.CallHome = ""
		}
	}
	return clone
}

func managedMachineOSStructuralHashVars(state v1alpha1.State, name string) v1alpha1.State {
	base := storageClusterDesiredHashVars(state, name)
	var clone v1alpha1.State
	data, err := json.Marshal(base)
	if err != nil {
		return base
	}
	if err := json.Unmarshal(data, &clone); err != nil {
		return base
	}
	clone.InfraProviders = nil
	clone.InfraComponents = nil
	for i := range clone.StorageClusters {
		if ceph := clone.StorageClusters[i].Spec.Ceph; ceph != nil {
			for j := range ceph.Topology.Hosts {
				ceph.Topology.Hosts[j].Devices = nil
				ceph.Topology.Hosts[j].OSD = nil
			}
			ceph.Topology.OSDDrivegroups = nil
		}
	}
	for i := range clone.Machines {
		clone.Machines[i].Spec.Substrate = v1alpha1.MachineSubstrate{}
		clone.Machines[i].Spec.Hardware.Management = v1alpha1.MachineHardwareManagement{}
	}
	return clone
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
