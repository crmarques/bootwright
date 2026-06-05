package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/graph"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func PlanApplyTasks(target ApplyTarget, state v1alpha1.State) []ApplyTask {
	tasks, _ := PlanApplyTasksChecked(target, state)
	return tasks
}

func PlanApplyTasksChecked(target ApplyTarget, state v1alpha1.State) ([]ApplyTask, error) {
	phaseSet := map[string]bool{}
	for _, phase := range target.PhaseNames {
		phaseSet[phase] = true
	}
	var tasks []ApplyTask
	kubeVirtDepsByCluster := map[string][]string{}
	if phaseSet[ApplyPhaseContainerCluster] && phaseSet[ApplyPhaseAddons] {
		var err error
		kubeVirtDepsByCluster, err = kubeVirtHostClusterApplyDeps(state)
		if err != nil {
			return nil, err
		}
	}
	machineServiceTasks, machineServiceTaskIDs := planMachineServiceTasks(state, phaseSet)
	tasks = append(tasks, machineServiceTasks...)
	storageInfraDepsByCluster := map[string][]string{}
	if phaseSet[ApplyPhaseStorageInfra] {
		for _, cluster := range state.StorageClusters {
			if !storageClusterManaged(cluster) {
				continue
			}
			if !storageClusterSelectedForTarget(target, cluster.Metadata.Name) {
				continue
			}
			managedOSDeps := []string{}
			managedOSMachines := managedOSMachineNames(state, cluster)
			if len(managedOSMachines) > 0 {
				prepareDepsByHost := planStorageManagedOSPrepareTasks(&tasks, state, cluster.Metadata.Name, managedOSMachines, machineServiceTaskIDs)
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
				managedOSDeps = append(managedOSDeps, taskID)
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindManagedMachineOS,
						Label:        "managed OS " + cluster.Metadata.Name + " machines",
						Cluster:      cluster.Metadata.Name,
						ClusterKind:  ApplyClusterKindStorage,
						Status:       TaskStatusPending,
						Dependencies: deps,
						ResourceKeys: applyManagedOSResourceKeys(state, cluster.Metadata.Name, managedOSMachines),
					},
					Playbook:      applyManagedMachineOSPlaybook,
					Limit:         render.ManagedOSGroupName(cluster.Metadata.Name),
					ExtraVarPairs: []string{"bootwright_task_managed_os_group_name=" + cluster.Metadata.Name},
					State:         storageTaskState(state, cluster.Metadata.Name),
					Forks:         len(managedOSMachines),
					RedfishSlots:  len(managedOSMachines),
				})
			}
			taskID := "storageinfra." + cluster.Metadata.Name
			storageInfraDepsByCluster[cluster.Metadata.Name] = []string{taskID}
			deps := append([]string(nil), machineServiceTaskIDs...)
			deps = append(deps, managedOSDeps...)
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindStorageInfra,
					Label:        "storage infra " + cluster.Metadata.Name,
					Cluster:      cluster.Metadata.Name,
					ClusterKind:  ApplyClusterKindStorage,
					Status:       TaskStatusPending,
					Dependencies: deps,
					ResourceKeys: []string{"storage:" + cluster.Metadata.Name},
				},
				Playbook:      applyStoragePlaybook,
				Limit:         render.StorageClusterGroupName(cluster.Metadata.Name),
				ExtraVarPairs: []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name, "bootwright_task_storage_prereqs_only=true"},
				State:         storageTaskState(state, cluster.Metadata.Name),
				Forks:         storageClusterNodeCount(cluster),
			})
		}
	}
	storageDepsByCluster := map[string][]string{}
	if phaseSet[ApplyPhaseStorageCluster] {
		for _, cluster := range state.StorageClusters {
			if !storageClusterManaged(cluster) {
				continue
			}
			if !storageClusterSelectedForTarget(target, cluster.Metadata.Name) {
				continue
			}
			taskID := "storage." + cluster.Metadata.Name
			storageDepsByCluster[cluster.Metadata.Name] = []string{taskID}
			deps := append([]string(nil), machineServiceTaskIDs...)
			deps = append(deps, storageInfraDepsByCluster[cluster.Metadata.Name]...)
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindStorageCluster,
					Label:        "storage " + cluster.Metadata.Name,
					Cluster:      cluster.Metadata.Name,
					ClusterKind:  ApplyClusterKindStorage,
					Status:       TaskStatusPending,
					Dependencies: deps,
					ResourceKeys: []string{"storage:" + cluster.Metadata.Name},
				},
				Playbook:      applyStoragePlaybook,
				Limit:         render.StorageSeedHostName(cluster.Metadata.Name),
				ExtraVarPairs: []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name, "bootwright_task_storage_skip_prereqs=true"},
				State:         storageTaskState(state, cluster.Metadata.Name),
			})
		}
	}
	infraDepsByCluster := map[string][]string{}
	clusterNames := applyClusterNames(state)
	for _, name := range clusterNames {
		if phaseSet[ApplyPhaseClusterInstall] {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			infraHosts := render.HostGroupMembers(clusterState)[render.GroupInfraHosts]
			baseDeps := append([]string(nil), machineServiceTaskIDs...)
			baseDeps = append(baseDeps, kubeVirtDepsByCluster[name]...)
			prepareDepsByHost := planContainerMachinePrepareTasks(&tasks, state, name, infraHosts, baseDeps)
			machineTaskIDsByHost := map[string][]string{}
			for _, machineName := range applyClusterMachineNames(state, name) {
				host := applyMachineHost(state, machineName)
				if host == "" {
					continue
				}
				taskID := "infra." + name + "." + machineName
				deps := append([]string(nil), baseDeps...)
				if prepareID := prepareDepsByHost[host]; prepareID != "" {
					deps = append(deps, prepareID)
				}
				hostSlotKey := applyMachineHostSlotKey(state, machineName)
				machineTaskIDsByHost[host] = append(machineTaskIDsByHost[host], taskID)
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:            taskID,
						Kind:          ApplyTaskKindClusterInstall,
						Label:         "machine infra " + name + "/" + machineName,
						Cluster:       name,
						ClusterKind:   ApplyClusterKindContainer,
						Node:          machineName,
						Host:          host,
						ResourceKeys:  applyMachineExclusiveResourceKeys(state, name, machineName),
						HostSlotKey:   hostSlotKey,
						HostSlotCount: 1,
						Status:        TaskStatusPending,
						Dependencies:  deps,
					},
					Playbook:      applyClusterInstallPlaybook,
					Limit:         render.MachineInfraHostName(name, machineName),
					ExtraVarPairs: []string{"bootwright_task_cluster_name=" + name, "bootwright_task_machine_name=" + machineName},
					Forks:         1,
					State:         clusterState,
					HostSlotKey:   hostSlotKey,
					HostSlotCount: 1,
				})
			}
			for _, host := range infraHosts {
				taskID := "infrafinalize." + name + "." + host
				deps := append([]string(nil), baseDeps...)
				deps = append(deps, machineTaskIDsByHost[host]...)
				if prepareID := prepareDepsByHost[host]; prepareID != "" && len(machineTaskIDsByHost[host]) == 0 {
					deps = append(deps, prepareID)
				}
				infraDepsByCluster[name] = append(infraDepsByCluster[name], taskID)
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindMachineInfraFinalize,
						Label:        "machine infra finalize " + name + " on " + host,
						Cluster:      name,
						ClusterKind:  ApplyClusterKindContainer,
						Host:         host,
						ResourceKeys: []string{hostMutationResource(host)},
						Status:       TaskStatusPending,
						Dependencies: deps,
					},
					Playbook:      applyMachineInfraFinalize,
					Limit:         host,
					ExtraVarPairs: []string{"bootwright_task_cluster_name=" + name, "bootwright_task_provider_host_name=" + host},
					Forks:         1,
					State:         clusterState,
				})
			}
		}
	}
	for _, name := range clusterNames {
		deps := append([]string(nil), infraDepsByCluster[name]...)
		deps = append(deps, kubeVirtDepsByCluster[name]...)
		if phaseSet[ApplyPhaseContainerCluster] {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			isoTaskID := "iso." + name
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           isoTaskID,
					Kind:         ApplyTaskKindClusterISO,
					Label:        "iso " + name,
					Cluster:      name,
					ClusterKind:  ApplyClusterKindContainer,
					Status:       TaskStatusPending,
					Dependencies: deps,
				},
				Playbook:      applyCreateISOPlaybook,
				Limit:         render.GroupOCPHosts,
				Forks:         1,
				ExtraVarPairs: []string{"bootwright_task_cluster_name=" + name},
				State:         clusterState,
			})
			machineNames := applyClusterMachineNames(state, name)
			bootTaskID := ""
			if len(machineNames) > 0 {
				bootTaskID = "boot." + name
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           bootTaskID,
						Kind:         ApplyTaskKindNodeBoot,
						Label:        "boot " + name + " nodes",
						Cluster:      name,
						ClusterKind:  ApplyClusterKindContainer,
						ResourceKeys: applyNodeBootResourceKeys(state, name, machineNames),
						Status:       TaskStatusPending,
						Dependencies: []string{isoTaskID},
					},
					Playbook:      applyBootMachinePlaybook,
					Limit:         render.AgentNodeGroupName(name),
					ExtraVarPairs: []string{"bootwright_task_cluster_name=" + name},
					State:         clusterState,
					Forks:         len(machineNames),
					RedfishSlots:  len(machineNames),
				})
			}
			waitDeps := []string{}
			if bootTaskID != "" {
				waitDeps = append(waitDeps, bootTaskID)
			} else {
				waitDeps = append(waitDeps, isoTaskID)
			}
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           "wait." + name,
					Kind:         ApplyTaskKindInstallWait,
					Label:        "wait install " + name,
					Cluster:      name,
					ClusterKind:  ApplyClusterKindContainer,
					Status:       TaskStatusPending,
					Dependencies: waitDeps,
				},
				Playbook:      applyWaitInstallPlaybook,
				Limit:         render.GroupOCPHosts,
				Forks:         1,
				ExtraVarPairs: []string{"bootwright_task_cluster_name=" + name},
				State:         clusterState,
			})
		}
	}
	if phaseSet[ApplyPhaseAddons] {
		addonTasks, err := planExtensionTasks(state, phaseSet[ApplyPhaseContainerCluster])
		if err != nil {
			return tasks, err
		}
		tasks = append(tasks, addonTasks...)
		tasks = append(tasks, planStorageAttachmentTasks(state, phaseSet[ApplyPhaseContainerCluster], storageDepsByCluster)...)
	}
	return tasks, nil
}

func planExtensionTasks(state v1alpha1.State, installPhasePlanned bool) ([]ApplyTask, error) {
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return nil, err
	}
	var tasks []ApplyTask
	for _, binding := range plans {
		if !stateHasContainerCluster(state, binding.Cluster) {
			continue
		}
		deps := []string{}
		if installPhasePlanned {
			deps = append(deps, "wait."+binding.Cluster)
		}
		for _, extension := range binding.Addons {
			extension := extension
			applyID := "addon." + binding.Cluster + "." + extension.Name + ".apply"
			waitID := "addon." + binding.Cluster + "." + extension.Name + ".wait"
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           applyID,
					Kind:         ApplyTaskKindClusterAddonApply,
					Label:        "addon " + binding.Cluster + " " + extension.Name + " apply",
					Cluster:      binding.Cluster,
					ClusterKind:  ApplyClusterKindContainer,
					Status:       TaskStatusPending,
					Dependencies: append([]string(nil), deps...),
				},
				State:     stategraph.FilterStateToClusters(state, []string{binding.Cluster}),
				Extension: &extension,
			})
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           waitID,
					Kind:         ApplyTaskKindClusterAddonWait,
					Label:        "addon " + binding.Cluster + " " + extension.Name + " wait",
					Cluster:      binding.Cluster,
					ClusterKind:  ApplyClusterKindContainer,
					Status:       TaskStatusPending,
					Dependencies: []string{applyID},
				},
				State:     stategraph.FilterStateToClusters(state, []string{binding.Cluster}),
				Extension: &extension,
			})
			deps = []string{waitID}
		}
	}
	return tasks, nil
}

func planContainerMachinePrepareTasks(tasks *[]ApplyTask, state v1alpha1.State, clusterName string, hosts []string, deps []string) map[string]string {
	out := map[string]string{}
	clusterState := stategraph.FilterStateToClusters(state, []string{clusterName})
	for _, host := range hosts {
		if !clusterHostNeedsSubstratePrepare(state, clusterName, host) {
			continue
		}
		taskID := "infraprepare." + clusterName + "." + host
		out[host] = taskID
		*tasks = append(*tasks, ApplyTask{
			Entry: TaskLedgerEntry{
				ID:           taskID,
				Kind:         ApplyTaskKindMachineInfraPrepare,
				Label:        "machine infra prepare " + clusterName + " on " + host,
				Cluster:      clusterName,
				ClusterKind:  ApplyClusterKindContainer,
				Host:         host,
				ResourceKeys: []string{hostMutationResource(host)},
				Status:       TaskStatusPending,
				Dependencies: append([]string(nil), deps...),
			},
			Playbook:      applyMachineInfraPrepare,
			Limit:         host,
			ExtraVarPairs: []string{"bootwright_task_cluster_name=" + clusterName, "bootwright_task_provider_host_name=" + host},
			Forks:         1,
			State:         clusterState,
		})
	}
	return out
}

func planStorageManagedOSPrepareTasks(tasks *[]ApplyTask, state v1alpha1.State, clusterName string, machineNames []string, deps []string) map[string]string {
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
		*tasks = append(*tasks, ApplyTask{
			Entry: TaskLedgerEntry{
				ID:           taskID,
				Kind:         ApplyTaskKindMachineInfraPrepare,
				Label:        "managed OS prepare " + clusterName + " on " + host,
				Cluster:      clusterName,
				ClusterKind:  ApplyClusterKindStorage,
				Host:         host,
				ResourceKeys: []string{hostMutationResource(host)},
				Status:       TaskStatusPending,
				Dependencies: append([]string(nil), deps...),
			},
			Playbook:      applyMachineInfraPrepare,
			Limit:         host,
			ExtraVarPairs: []string{"bootwright_task_cluster_name=" + clusterName, "bootwright_task_provider_host_name=" + host},
			Forks:         1,
			State:         storageTaskState(state, clusterName),
		})
	}
	return out
}

func clusterHostNeedsSubstratePrepare(state v1alpha1.State, clusterName, host string) bool {
	cluster, ok := containerClusterByName(state, clusterName)
	if !ok {
		return false
	}
	for _, node := range cluster.Spec.Nodes {
		if node.MachineRef.Name == "" || applyMachineHost(state, node.MachineRef.Name) != host {
			continue
		}
		if applyMachineNeedsSubstratePrepare(state, node.MachineRef.Name) {
			return true
		}
	}
	return false
}

func applyMachineHost(state v1alpha1.State, machineName string) string {
	machine, ok := stateview.Machine(state, machineName)
	if !ok || machine.Spec.Substrate.ProviderRef.Name == "" || machine.Spec.Substrate.ProfileRef.Name == "" {
		return ""
	}
	provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
	if !ok {
		return ""
	}
	switch provider.Spec.Type {
	case v1alpha1.ProvisionerLibvirt:
		if provider.Spec.Libvirt != nil {
			return provider.Spec.Libvirt.MachineRef.Name
		}
	case v1alpha1.ProvisionerKubeVirt:
		return "localhost"
	}
	return ""
}

func applyMachineNeedsSubstratePrepare(state v1alpha1.State, machineName string) bool {
	machine, ok := stateview.Machine(state, machineName)
	if !ok {
		return false
	}
	provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
	return ok && provider.Spec.Type == v1alpha1.ProvisionerLibvirt
}

func applyMachineHostSlotKey(state v1alpha1.State, machineName string) string {
	host := applyMachineHost(state, machineName)
	if host == "" || applyMachineProviderType(state, machineName) != v1alpha1.ProvisionerLibvirt {
		return ""
	}
	return "host:" + host + ":machine"
}

func applyMachineExclusiveResourceKeys(state v1alpha1.State, clusterName, machineName string) []string {
	machine, ok := stateview.Machine(state, machineName)
	if !ok {
		return nil
	}
	provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
	if ok && provider.Spec.Type == v1alpha1.ProvisionerKubeVirt && provider.Spec.KubeVirt != nil {
		return []string{kubeVirtResourceKey(provider.Spec.KubeVirt)}
	}
	if machine.Spec.Hardware.Management.BMC.Address != "" {
		return []string{applyNodeRedfishResource(state, clusterName, machineName)}
	}
	return nil
}

func applyManagedOSResourceKeys(state v1alpha1.State, clusterName string, machineNames []string) []string {
	var out []string
	for _, machineName := range machineNames {
		for _, key := range applyMachineExclusiveResourceKeys(state, clusterName, machineName) {
			out = appendUniqueString(out, key)
		}
		if applyMachineProviderType(state, machineName) == v1alpha1.ProvisionerLibvirt {
			if host := applyMachineHost(state, machineName); host != "" {
				out = appendUniqueString(out, hostMutationResource(host))
			}
		}
	}
	return out
}

func applyMachineProviderType(state v1alpha1.State, machineName string) string {
	machine, ok := stateview.Machine(state, machineName)
	if !ok {
		return ""
	}
	provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
	if !ok {
		return ""
	}
	return provider.Spec.Type
}

func applyClusterNames(state v1alpha1.State) []string {
	names := make([]string, 0, len(state.ContainerClusters))
	for _, cluster := range state.ContainerClusters {
		names = append(names, cluster.Metadata.Name)
	}
	sort.Strings(names)
	return names
}

func storageClusterSelectedForTarget(target ApplyTarget, name string) bool {
	if target.StorageClusterNames == nil {
		return true
	}
	for _, selected := range target.StorageClusterNames {
		if selected == name {
			return true
		}
	}
	return false
}

func applyClusterMachineNames(state v1alpha1.State, clusterName string) []string {
	var names []string
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name != clusterName {
			continue
		}
		seen := map[string]bool{}
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name == "" || seen[node.MachineRef.Name] {
				continue
			}
			seen[node.MachineRef.Name] = true
			names = append(names, node.MachineRef.Name)
		}
		break
	}
	sort.Strings(names)
	return names
}

func applyNodeRedfishResource(state v1alpha1.State, clusterName, machineName string) string {
	for _, machine := range state.Machines {
		if machine.Metadata.Name != machineName {
			continue
		}
		if machine.Spec.Hardware.Management.BMC.Address != "" {
			return "redfish:" + machine.Spec.Hardware.Management.BMC.Address
		}
	}
	return "redfish:" + clusterName + "/" + machineName
}

func hostMutationResource(host string) string {
	if host == "" {
		return ""
	}
	return "host:" + host + ":mutating"
}
