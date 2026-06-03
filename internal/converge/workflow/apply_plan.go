package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/graph"
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
	hostServiceTasks, hostServiceTaskIDs := planHostServiceTasks(state, phaseSet)
	tasks = append(tasks, hostServiceTasks...)
	storageInfraDepsByCluster := map[string][]string{}
	if phaseSet[ApplyPhaseStorageInfra] {
		for _, cluster := range state.StorageClusters {
			if !storageClusterManaged(cluster) {
				continue
			}
			if !storageClusterSelectedForTarget(target, cluster.Metadata.Name) {
				continue
			}
			taskID := "storageinfra." + cluster.Metadata.Name
			storageInfraDepsByCluster[cluster.Metadata.Name] = []string{taskID}
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindStorageInfra,
					Label:        "storage infra " + cluster.Metadata.Name,
					Cluster:      cluster.Metadata.Name,
					ClusterKind:  ApplyClusterKindStorage,
					Status:       TaskStatusPending,
					Dependencies: append([]string(nil), hostServiceTaskIDs...),
					ResourceKeys: []string{"storage:" + cluster.Metadata.Name},
				},
				Playbook:      applyStoragePlaybook,
				Limit:         render.StorageSeedHostName(cluster.Metadata.Name),
				ExtraVarPairs: []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name, "bootwright_task_storage_prereqs_only=true"},
				State:         storageTaskState(state, cluster.Metadata.Name),
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
			deps := append([]string(nil), hostServiceTaskIDs...)
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
		if phaseSet[ApplyPhaseClusterInfra] {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			infraHosts := render.HostGroupMembers(clusterState)[render.GroupInfraHosts]
			deps := append([]string(nil), hostServiceTaskIDs...)
			deps = append(deps, kubeVirtDepsByCluster[name]...)
			resourceKeys := kubeVirtResourceKeys(state, name)
			if len(infraHosts) == 0 {
				taskID := "infra." + name
				infraDepsByCluster[name] = append(infraDepsByCluster[name], taskID)
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindClusterInfra,
						Label:        "infra " + name,
						Cluster:      name,
						ClusterKind:  ApplyClusterKindContainer,
						Status:       TaskStatusPending,
						Dependencies: deps,
						ResourceKeys: resourceKeys,
					},
					Playbook: applyClusterInfraPlaybook,
					Limit:    render.GroupInfraHosts,
					State:    clusterState,
				})
				continue
			}
			for _, host := range infraHosts {
				taskID := "infra." + name + "." + host
				infraDepsByCluster[name] = append(infraDepsByCluster[name], taskID)
				tasks = append(tasks, ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindClusterInfra,
						Label:        "infra " + name + " on " + host,
						Cluster:      name,
						ClusterKind:  ApplyClusterKindContainer,
						Host:         host,
						ResourceKeys: append([]string{hostMutationResource(host)}, resourceKeys...),
						Status:       TaskStatusPending,
						Dependencies: deps,
					},
					Playbook: applyClusterInfraPlaybook,
					Limit:    host,
					Forks:    1,
					State:    clusterState,
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
	clusterInfraName := ""
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name != clusterName {
			continue
		}
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name == machineName {
				clusterInfraName = node.MachineRef.ClusterInfra
				break
			}
		}
		break
	}
	for _, infra := range state.ClusterInfras {
		if infra.Metadata.Name != clusterInfraName {
			continue
		}
		for _, machine := range infra.Spec.Components.Machines {
			if machine.Name != machineName || machine.From.Name == "" {
				continue
			}
			for _, provider := range state.InfraProviders {
				if provider.Metadata.Name != machine.From.Provider {
					continue
				}
				for _, providerMachine := range provider.Spec.Machines {
					if providerMachine.Name == machine.From.Name && providerMachine.BareMetal != nil && providerMachine.BareMetal.BMC.Address != "" {
						return "redfish:" + providerMachine.BareMetal.BMC.Address
					}
				}
			}
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
