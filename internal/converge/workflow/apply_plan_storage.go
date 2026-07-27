package workflow

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func planStorageManagedOSInstallActivities(graph *ActivityGraph, state v1alpha1.State, hashState v1alpha1.State, target ApplyTarget, phaseSet map[string]bool, includeStorage bool, machineServiceTaskIDs []string) (map[string][]string, error) {
	managedOSDepsByCluster := map[string][]string{}
	if !(phaseSet[ApplyPhaseMachines] && includeStorage) {
		return managedOSDepsByCluster, nil
	}
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) {
			continue
		}
		if !storageClusterSelectedForTarget(target, cluster.Metadata.Name) {
			continue
		}
		managedOSMachines := managedOSMachineNames(state, cluster)
		if target.MachineScoped() {
			managedOSMachines = selectedMachineNames(managedOSMachines, target)
		}
		if len(managedOSMachines) == 0 {
			continue
		}
		prepareDepsByHost, err := planStorageManagedOSPrepareTasks(graph, state, hashState, cluster.Metadata.Name, managedOSMachines)
		if err != nil {
			return nil, err
		}
		taskID := "osinstall." + cluster.Metadata.Name
		deps := append([]string(nil), machineServiceTaskIDs...)
		seenPrepareDeps := map[string]bool{}
		for _, machineName := range managedOSMachines {
			host := applyMachineHost(state, machineName)
			if prepareID := prepareDepsByHost[host]; prepareID != "" {
				if !seenPrepareDeps[prepareID] {
					deps = append(deps, prepareID)
					seenPrepareDeps[prepareID] = true
				}
			}
		}
		managedOSDepsByCluster[cluster.Metadata.Name] = []string{taskID}
		requires, err := kubeVirtHostClusterApplyCapabilitiesForMachines(state, managedOSMachines)
		if err != nil {
			return nil, err
		}
		provides := make([]CapabilityRef, 0, len(managedOSMachines)*2)
		for _, machineName := range managedOSMachines {
			provides = append(provides, machineInstantiatedCapability(machineName), machineOSReadyCapability(machineName))
		}
		if err := graph.Add(Activity{
			ID:                   taskID,
			Requires:             requires,
			Provides:             provides,
			ExplicitDependencies: deps,
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindManagedMachineOS,
					Label:        "managed OS " + cluster.Metadata.Name + " machines",
					Cluster:      cluster.Metadata.Name,
					ClusterKind:  ApplyClusterKindStorage,
					Status:       TaskStatusPending,
					ResourceKeys: applyManagedOSResourceKeys(state, cluster.Metadata.Name, managedOSMachines),
				},
				Playbook:           applyManagedMachineOSPlaybook,
				Limit:              managedOSInstallLimit(cluster.Metadata.Name, managedOSMachines, target.MachineScoped()),
				ExtraVarPairs:      []string{"bootwright_task_managed_os_group_name=" + cluster.Metadata.Name},
				State:              storageTaskState(state, cluster.Metadata.Name),
				DesiredHashState:   managedOSDesiredHashState(hashState, cluster.Metadata.Name),
				StructuralHashVars: managedMachineOSStructuralHashVars(hashState, cluster.Metadata.Name),
				Forks:              len(managedOSMachines),
				RedfishSlots:       len(managedOSMachines),
			},
		}); err != nil {
			return nil, err
		}
	}
	return managedOSDepsByCluster, nil
}

func planStorageNodeAccessActivities(graph *ActivityGraph, state v1alpha1.State, hashState v1alpha1.State, target ApplyTarget, phaseSet map[string]bool, includeStorage bool, machineServiceTaskIDs []string, managedOSDepsByCluster map[string][]string) (map[string][]string, error) {
	nodeAccessDepsByCluster := map[string][]string{}
	if !(phaseSet[ApplyPhaseMachines] && includeStorage) {
		return nodeAccessDepsByCluster, nil
	}
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil {
			continue
		}
		if !v1alpha1.StorageClusterManagesNodeAccount(cluster) {
			continue
		}
		if !storageClusterSelectedForTarget(target, cluster.Metadata.Name) {
			continue
		}
		limit := render.StorageClusterGroupName(cluster.Metadata.Name)
		forks := storageClusterNodeCount(cluster)
		if target.MachineScoped() {
			hosts := storageRegistrationSelectedHosts(state, target, cluster)
			if len(hosts) == 0 {
				continue
			}
			limit = strings.Join(hosts, ":")
			forks = len(hosts)
		}
		taskID := "nodeaccess." + cluster.Metadata.Name
		nodeAccessDepsByCluster[cluster.Metadata.Name] = []string{taskID}
		deps := append([]string(nil), machineServiceTaskIDs...)
		deps = append(deps, managedOSDepsByCluster[cluster.Metadata.Name]...)
		if err := graph.Add(Activity{
			ID:                   taskID,
			ExplicitDependencies: deps,
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindStorageNodeAccess,
					Label:        "storage node access " + cluster.Metadata.Name,
					Cluster:      cluster.Metadata.Name,
					ClusterKind:  ApplyClusterKindStorage,
					Status:       TaskStatusPending,
					ResourceKeys: []string{"storage:" + cluster.Metadata.Name},
				},
				Playbook:           applyStorageNodeAccessPlaybook,
				Limit:              limit,
				ExtraVarPairs:      []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name},
				State:              storageTaskState(state, cluster.Metadata.Name),
				DesiredHashVars:    storageClusterDesiredHashVars(hashState, cluster.Metadata.Name),
				StructuralHashVars: storageClusterStructuralHashVars(hashState, cluster.Metadata.Name),
				Forks:              forks,
			},
		}); err != nil {
			return nil, err
		}
	}
	return nodeAccessDepsByCluster, nil
}

func planStorageRegistrationActivities(graph *ActivityGraph, state v1alpha1.State, hashState v1alpha1.State, target ApplyTarget, phaseSet map[string]bool, includeStorage bool, machineServiceTaskIDs []string, managedOSDepsByCluster map[string][]string) (map[string][]string, error) {
	registrationDepsByCluster := map[string][]string{}
	if !(phaseSet[ApplyPhaseMachines] && includeStorage) {
		return registrationDepsByCluster, nil
	}
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) {
			continue
		}
		if !storageClusterSelectedForTarget(target, cluster.Metadata.Name) {
			continue
		}
		if !v1alpha1.StorageClusterManagedRegistration(cluster, state) {
			continue
		}
		limit := render.StorageClusterGroupName(cluster.Metadata.Name)
		forks := storageClusterNodeCount(cluster)
		if target.MachineScoped() || storageClusterHasProvidedOSNode(state, cluster) {
			hosts := storageRegistrationSelectedHosts(state, target, cluster)
			if len(hosts) == 0 {
				continue
			}
			limit = strings.Join(hosts, ":")
			forks = len(hosts)
		}
		taskID := "registration." + cluster.Metadata.Name
		registrationDepsByCluster[cluster.Metadata.Name] = []string{taskID}
		deps := append([]string(nil), machineServiceTaskIDs...)
		deps = append(deps, managedOSDepsByCluster[cluster.Metadata.Name]...)
		if err := graph.Add(Activity{
			ID:                   taskID,
			ExplicitDependencies: deps,
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindMachineRegistration,
					Label:        "machine registration " + cluster.Metadata.Name,
					Cluster:      cluster.Metadata.Name,
					ClusterKind:  ApplyClusterKindStorage,
					Status:       TaskStatusPending,
					ResourceKeys: []string{"storage:" + cluster.Metadata.Name},
				},
				Playbook:           applyMachineRegistrationPlaybook,
				Limit:              limit,
				ExtraVarPairs:      []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name},
				State:              storageTaskState(state, cluster.Metadata.Name),
				DesiredHashVars:    storageClusterDesiredHashVars(hashState, cluster.Metadata.Name),
				StructuralHashVars: storageClusterStructuralHashVars(hashState, cluster.Metadata.Name),
				Forks:              forks,
			},
		}); err != nil {
			return nil, err
		}
	}
	return registrationDepsByCluster, nil
}

func managedOSInstallLimit(clusterName string, machineNames []string, scoped bool) string {
	if !scoped {
		return render.ManagedOSGroupName(clusterName)
	}
	hosts := make([]string, 0, len(machineNames))
	for _, machineName := range machineNames {
		hosts = append(hosts, render.ManagedOSHostName(clusterName, machineName))
	}
	return strings.Join(hosts, ":")
}

func storageRegistrationSelectedHosts(state v1alpha1.State, target ApplyTarget, cluster v1alpha1.StorageCluster) []string {
	var hosts []string
	seen := map[string]bool{}
	for _, machineName := range selectedMachineNames(managedOSMachineNames(state, cluster), target) {
		for _, host := range render.MachineInventoryHosts(state, machineName) {
			if host == "" || seen[host] {
				continue
			}
			seen[host] = true
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func planStorageInfraActivities(graph *ActivityGraph, state v1alpha1.State, hashState v1alpha1.State, target ApplyTarget, phaseSet map[string]bool, includeStorage bool, machineServiceTaskIDs []string, managedOSDepsByCluster, registrationDepsByCluster map[string][]string) (map[string][]string, error) {
	storageInfraDepsByCluster := map[string][]string{}
	if !(phaseSet[ApplyPhaseDeps] && includeStorage) {
		return storageInfraDepsByCluster, nil
	}
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) {
			continue
		}
		if !storageClusterSelectedForTarget(target, cluster.Metadata.Name) {
			continue
		}
		taskID := "storageinfra." + cluster.Metadata.Name
		storageInfraDepsByCluster[cluster.Metadata.Name] = []string{taskID}
		deps := append([]string(nil), machineServiceTaskIDs...)
		deps = append(deps, managedOSDepsByCluster[cluster.Metadata.Name]...)
		deps = append(deps, registrationDepsByCluster[cluster.Metadata.Name]...)
		if err := graph.Add(Activity{
			ID:                   taskID,
			Provides:             []CapabilityRef{{Kind: "storage.nodes-ready", Name: cluster.Metadata.Name}},
			ExplicitDependencies: deps,
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindStorageInfra,
					Label:        "storage infra " + cluster.Metadata.Name,
					Cluster:      cluster.Metadata.Name,
					ClusterKind:  ApplyClusterKindStorage,
					Status:       TaskStatusPending,
					ResourceKeys: []string{"storage:" + cluster.Metadata.Name},
				},
				Playbook:           applyStoragePlaybook,
				Limit:              render.StorageClusterGroupName(cluster.Metadata.Name),
				ExtraVarPairs:      []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name, "bootwright_task_storage_prereqs_only=true"},
				State:              storageTaskState(state, cluster.Metadata.Name),
				DesiredHashVars:    storageClusterDesiredHashVars(hashState, cluster.Metadata.Name),
				StructuralHashVars: storageClusterStructuralHashVars(hashState, cluster.Metadata.Name),
				Forks:              storageClusterNodeCount(cluster),
			},
		}); err != nil {
			return nil, err
		}
	}
	return storageInfraDepsByCluster, nil
}

func planStorageClusterActivities(graph *ActivityGraph, state v1alpha1.State, hashState v1alpha1.State, target ApplyTarget, phaseSet map[string]bool, includeStorage bool, machineServiceTaskIDs []string, storageInfraDepsByCluster map[string][]string) (map[string][]string, error) {
	storageDepsByCluster := map[string][]string{}
	if !(phaseSet[ApplyPhaseBase] && includeStorage) {
		return storageDepsByCluster, nil
	}
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) {
			continue
		}
		if !storageClusterSelectedForTarget(target, cluster.Metadata.Name) {
			continue
		}
		taskID := "storage." + cluster.Metadata.Name
		storageDepsByCluster[cluster.Metadata.Name] = []string{taskID}
		deps := append([]string(nil), machineServiceTaskIDs...)
		deps = append(deps, storageInfraDepsByCluster[cluster.Metadata.Name]...)
		if err := graph.Add(Activity{
			ID:                   taskID,
			Provides:             []CapabilityRef{{Kind: "storage.cluster-ready", Name: cluster.Metadata.Name}},
			ExplicitDependencies: deps,
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindStorageCluster,
					Label:        "storage " + cluster.Metadata.Name,
					Cluster:      cluster.Metadata.Name,
					ClusterKind:  ApplyClusterKindStorage,
					Status:       TaskStatusPending,
					ResourceKeys: []string{"storage:" + cluster.Metadata.Name},
				},
				Playbook:           applyStoragePlaybook,
				Limit:              render.StorageClusterGroupName(cluster.Metadata.Name),
				ExtraVarPairs:      []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name, "bootwright_task_storage_skip_prereqs=true"},
				State:              storageTaskState(state, cluster.Metadata.Name),
				DesiredHashVars:    storageClusterDesiredHashVars(hashState, cluster.Metadata.Name),
				StructuralHashVars: storageClusterStructuralHashVars(hashState, cluster.Metadata.Name),
				Forks:              storageClusterNodeCount(cluster),
			},
		}); err != nil {
			return nil, err
		}
	}
	return storageDepsByCluster, nil
}

func planStorageManagedOSPrepareTasks(graph *ActivityGraph, state v1alpha1.State, hashState v1alpha1.State, clusterName string, machineNames []string) (map[string]string, error) {
	out := map[string]string{}
	seen := map[string]bool{}
	for _, machineName := range machineNames {
		host := applyMachineHost(state, machineName)
		if host == "" || seen[host] || !applyMachineNeedsSubstratePrepare(state, machineName) {
			continue
		}
		seen[host] = true
		taskID := "osprepare." + clusterName + "." + host
		out[host] = taskID
		if err := graph.Add(Activity{
			ID:       taskID,
			Requires: []CapabilityRef{providerHostReadyCapability(host)},
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindMachineInfraPrepare,
					Label:        "managed OS prepare " + clusterName + " on " + host,
					Cluster:      clusterName,
					ClusterKind:  ApplyClusterKindStorage,
					Host:         host,
					ResourceKeys: []string{hostMutationResource(host)},
					Status:       TaskStatusPending,
				},
				Playbook:           applyMachineInfraPrepare,
				Limit:              host,
				ExtraVarPairs:      []string{"bootwright_task_cluster_name=" + clusterName, "bootwright_task_provider_host_name=" + host},
				Forks:              1,
				State:              storageTaskState(state, clusterName),
				DesiredHashState:   managedOSDesiredHashState(hashState, clusterName),
				StructuralHashVars: managedMachineOSStructuralHashVars(hashState, clusterName),
			},
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}
