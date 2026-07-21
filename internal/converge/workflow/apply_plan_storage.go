package workflow

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func planStorageManagedOSInstallActivities(graph *ActivityGraph, state v1alpha1.State, target ApplyTarget, phaseSet map[string]bool, includeStorage bool, machineServiceTaskIDs []string) (map[string][]string, error) {
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
		prepareDepsByHost, err := planStorageManagedOSPrepareTasks(graph, state, cluster.Metadata.Name, managedOSMachines, machineServiceTaskIDs)
		if err != nil {
			return nil, err
		}
		if target.MachineScoped() {
			ids, err := planStorageManagedOSPerMachineTasks(graph, state, cluster.Metadata.Name, managedOSMachines, machineServiceTaskIDs, prepareDepsByHost)
			if err != nil {
				return nil, err
			}
			managedOSDepsByCluster[cluster.Metadata.Name] = ids
			continue
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
				Limit:              render.ManagedOSGroupName(cluster.Metadata.Name),
				ExtraVarPairs:      []string{"bootwright_task_managed_os_group_name=" + cluster.Metadata.Name},
				State:              storageTaskState(state, cluster.Metadata.Name),
				StructuralHashVars: managedMachineOSStructuralHashVars(state, cluster.Metadata.Name),
				Forks:              len(managedOSMachines),
				RedfishSlots:       len(managedOSMachines),
			},
		}); err != nil {
			return nil, err
		}
	}
	return managedOSDepsByCluster, nil
}

func planStorageManagedOSPerMachineTasks(graph *ActivityGraph, state v1alpha1.State, clusterName string, machineNames, machineServiceTaskIDs []string, prepareDepsByHost map[string]string) ([]string, error) {
	var ids []string
	for _, machineName := range machineNames {
		requires, err := kubeVirtHostClusterApplyCapabilitiesForMachines(state, []string{machineName})
		if err != nil {
			return nil, err
		}
		host := applyMachineHost(state, machineName)
		taskID := "osinstall." + clusterName + "." + machineName
		ids = append(ids, taskID)
		deps := append([]string(nil), machineServiceTaskIDs...)
		if prepareID := prepareDepsByHost[host]; prepareID != "" {
			deps = append(deps, prepareID)
		}
		if err := graph.Add(Activity{
			ID:                   taskID,
			Requires:             requires,
			Provides:             []CapabilityRef{machineInstantiatedCapability(machineName), machineOSReadyCapability(machineName)},
			ExplicitDependencies: deps,
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindManagedMachineOS,
					Label:        "managed OS " + clusterName + " machine " + machineName,
					Cluster:      clusterName,
					ClusterKind:  ApplyClusterKindStorage,
					Node:         machineName,
					Host:         host,
					Status:       TaskStatusPending,
					ResourceKeys: applyManagedOSResourceKeys(state, clusterName, []string{machineName}),
				},
				Playbook:           applyManagedMachineOSPlaybook,
				Limit:              render.ManagedOSHostName(clusterName, machineName),
				ExtraVarPairs:      []string{"bootwright_task_managed_os_group_name=" + clusterName, "bootwright_task_machine_name=" + machineName},
				State:              storageTaskState(state, clusterName),
				StructuralHashVars: managedMachineOSStructuralHashVars(state, clusterName),
				Forks:              1,
				RedfishSlots:       1,
			},
		}); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func planStorageRegistrationActivities(graph *ActivityGraph, state v1alpha1.State, target ApplyTarget, phaseSet map[string]bool, includeStorage bool, machineServiceTaskIDs []string, managedOSDepsByCluster map[string][]string) (map[string][]string, error) {
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
		if target.MachineScoped() {
			ids, err := planStorageRegistrationPerMachineTasks(graph, state, target, cluster, machineServiceTaskIDs, managedOSDepsByCluster[cluster.Metadata.Name])
			if err != nil {
				return nil, err
			}
			if len(ids) > 0 {
				registrationDepsByCluster[cluster.Metadata.Name] = ids
			}
			continue
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
				Limit:              render.StorageClusterGroupName(cluster.Metadata.Name),
				ExtraVarPairs:      []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name},
				State:              storageTaskState(state, cluster.Metadata.Name),
				DesiredHashVars:    storageClusterDesiredHashVars(state, cluster.Metadata.Name),
				StructuralHashVars: storageClusterStructuralHashVars(state, cluster.Metadata.Name),
				Forks:              storageClusterNodeCount(cluster),
			},
		}); err != nil {
			return nil, err
		}
	}
	return registrationDepsByCluster, nil
}

func planStorageRegistrationPerMachineTasks(graph *ActivityGraph, state v1alpha1.State, target ApplyTarget, cluster v1alpha1.StorageCluster, machineServiceTaskIDs, managedOSDeps []string) ([]string, error) {
	var ids []string
	for _, machineName := range selectedMachineNames(managedOSMachineNames(state, cluster), target) {
		hosts := render.MachineInventoryHosts(state, machineName)
		if len(hosts) == 0 {
			continue
		}
		taskID := "registration." + cluster.Metadata.Name + "." + machineName
		ids = append(ids, taskID)
		deps := append([]string(nil), machineServiceTaskIDs...)
		deps = append(deps, managedOSDeps...)
		if err := graph.Add(Activity{
			ID:                   taskID,
			ExplicitDependencies: deps,
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindMachineRegistration,
					Label:        "machine registration " + cluster.Metadata.Name + " machine " + machineName,
					Cluster:      cluster.Metadata.Name,
					ClusterKind:  ApplyClusterKindStorage,
					Node:         machineName,
					Status:       TaskStatusPending,
					ResourceKeys: []string{"storage:" + cluster.Metadata.Name + ":" + machineName},
				},
				Playbook:           applyMachineRegistrationPlaybook,
				Limit:              strings.Join(hosts, ":"),
				ExtraVarPairs:      []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name, "bootwright_task_machine_name=" + machineName},
				State:              storageTaskState(state, cluster.Metadata.Name),
				DesiredHashVars:    storageClusterDesiredHashVars(state, cluster.Metadata.Name),
				StructuralHashVars: storageClusterStructuralHashVars(state, cluster.Metadata.Name),
				Forks:              1,
			},
		}); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func planStorageInfraActivities(graph *ActivityGraph, state v1alpha1.State, target ApplyTarget, phaseSet map[string]bool, includeStorage bool, machineServiceTaskIDs []string, managedOSDepsByCluster, registrationDepsByCluster map[string][]string) (map[string][]string, error) {
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
				DesiredHashVars:    storageClusterDesiredHashVars(state, cluster.Metadata.Name),
				StructuralHashVars: storageClusterStructuralHashVars(state, cluster.Metadata.Name),
				Forks:              storageClusterNodeCount(cluster),
			},
		}); err != nil {
			return nil, err
		}
	}
	return storageInfraDepsByCluster, nil
}

func planStorageClusterActivities(graph *ActivityGraph, state v1alpha1.State, target ApplyTarget, phaseSet map[string]bool, includeStorage bool, machineServiceTaskIDs []string, storageInfraDepsByCluster map[string][]string) (map[string][]string, error) {
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
				DesiredHashVars:    storageClusterDesiredHashVars(state, cluster.Metadata.Name),
				StructuralHashVars: storageClusterStructuralHashVars(state, cluster.Metadata.Name),
				Forks:              storageClusterNodeCount(cluster),
			},
		}); err != nil {
			return nil, err
		}
	}
	return storageDepsByCluster, nil
}

func planStorageManagedOSPrepareTasks(graph *ActivityGraph, state v1alpha1.State, clusterName string, machineNames []string, deps []string) (map[string]string, error) {
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
			ID:                   taskID,
			Requires:             []CapabilityRef{providerHostReadyCapability(host)},
			ExplicitDependencies: append([]string(nil), deps...),
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
				StructuralHashVars: managedMachineOSStructuralHashVars(state, clusterName),
			},
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}
