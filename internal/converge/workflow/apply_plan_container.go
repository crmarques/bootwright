package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func planContainerMachineInfraActivities(graph *ActivityGraph, state v1alpha1.State, target ApplyTarget, phaseSet map[string]bool, includeContainer bool, clusterNames []string, kubeVirtReqsByCluster map[string][]CapabilityRef, machineServiceTaskIDs []string) (map[string][]string, map[string][]string, error) {
	infraDepsByCluster := map[string][]string{}
	prepareDepsByCluster := map[string][]string{}
	if !(phaseSet[ApplyPhaseMachines] && includeContainer) {
		return infraDepsByCluster, prepareDepsByCluster, nil
	}
	for _, name := range clusterNames {
		machineNames := selectedMachineNames(applyClusterMachineNames(state, name), target)
		if target.MachineScoped() && len(machineNames) == 0 {
			continue
		}
		selectedHosts := map[string]bool{}
		for _, machineName := range machineNames {
			if host := applyMachineHost(state, machineName); host != "" {
				selectedHosts[host] = true
			}
		}
		clusterState := stategraph.FilterStateToClusters(state, []string{name})
		infraHosts := retainSelectedHosts(render.HostGroupMembers(clusterState)[render.GroupInfraHosts], selectedHosts, target.MachineScoped())
		baseDeps := append([]string(nil), machineServiceTaskIDs...)
		prepareDepsByHost, err := planContainerMachinePrepareTasks(graph, state, name, infraHosts)
		if err != nil {
			return nil, nil, err
		}
		for _, host := range infraHosts {
			if prepareID := prepareDepsByHost[host]; prepareID != "" {
				prepareDepsByCluster[name] = append(prepareDepsByCluster[name], prepareID)
			}
		}
		for _, machineName := range machineNames {
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
			infraDepsByCluster[name] = append(infraDepsByCluster[name], taskID)
			if err := graph.Add(Activity{
				ID:                   taskID,
				Requires:             kubeVirtReqsByCluster[name],
				Provides:             []CapabilityRef{machineInstantiatedCapability(machineName)},
				ExplicitDependencies: deps,
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:            taskID,
						Kind:          ApplyTaskKindClusterInstall,
						Label:         "provision machine " + machineName,
						Cluster:       name,
						ClusterKind:   ApplyClusterKindContainer,
						Node:          machineName,
						Host:          host,
						ResourceKeys:  applyMachineExclusiveResourceKeys(state, name, machineName),
						HostSlotKey:   hostSlotKey,
						HostSlotCount: 1,
						Status:        TaskStatusPending,
					},
					Playbook:           applyClusterInstallPlaybook,
					Limit:              render.MachineInfraHostName(name, machineName),
					ExtraVarPairs:      []string{"bootwright_task_cluster_name=" + name, "bootwright_task_machine_name=" + machineName},
					Forks:              1,
					State:              clusterState,
					StructuralHashVars: containerClusterInstallStructuralHashVars(clusterState),
					HostSlotKey:        hostSlotKey,
					HostSlotCount:      1,
				},
			}); err != nil {
				return nil, nil, err
			}
		}
		for _, host := range infraHosts {
			taskID := "infrafinalize." + name + "." + host
			finalizeLabel := "finalize infra " + name
			if host != "localhost" {
				finalizeLabel += " on " + host
			}
			deps := append([]string(nil), baseDeps...)
			if prepareID := prepareDepsByHost[host]; prepareID != "" {
				deps = append(deps, prepareID)
			}
			infraDepsByCluster[name] = append(infraDepsByCluster[name], taskID)
			if err := graph.Add(Activity{
				ID:                   taskID,
				Requires:             kubeVirtReqsByCluster[name],
				ExplicitDependencies: deps,
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindMachineInfraFinalize,
						Label:        finalizeLabel,
						Cluster:      name,
						ClusterKind:  ApplyClusterKindContainer,
						Host:         host,
						ResourceKeys: []string{hostMutationResource(host)},
						Status:       TaskStatusPending,
					},
					Playbook:           applyMachineInfraFinalize,
					Limit:              host,
					ExtraVarPairs:      []string{"bootwright_task_cluster_name=" + name, "bootwright_task_provider_host_name=" + host},
					Forks:              1,
					State:              clusterState,
					StructuralHashVars: containerClusterInstallStructuralHashVars(clusterState),
				},
			}); err != nil {
				return nil, nil, err
			}
		}
	}
	return infraDepsByCluster, prepareDepsByCluster, nil
}

func planContainerMachinePrepareTasks(graph *ActivityGraph, state v1alpha1.State, clusterName string, hosts []string) (map[string]string, error) {
	out := map[string]string{}
	clusterState := stategraph.FilterStateToClusters(state, []string{clusterName})
	for _, host := range hosts {
		if !clusterHostNeedsSubstratePrepare(state, clusterName, host) {
			continue
		}
		taskID := "infraprepare." + clusterName + "." + host
		out[host] = taskID
		if err := graph.Add(Activity{
			ID:       taskID,
			Requires: []CapabilityRef{providerHostReadyCapability(host)},
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindMachineInfraPrepare,
					Label:        "machine infra prepare " + clusterName + " on " + host,
					Cluster:      clusterName,
					ClusterKind:  ApplyClusterKindContainer,
					Host:         host,
					ResourceKeys: []string{hostMutationResource(host)},
					Status:       TaskStatusPending,
				},
				Playbook:           applyMachineInfraPrepare,
				Limit:              host,
				ExtraVarPairs:      []string{"bootwright_task_cluster_name=" + clusterName, "bootwright_task_provider_host_name=" + host},
				Forks:              1,
				State:              clusterState,
				StructuralHashVars: containerClusterInstallStructuralHashVars(clusterState),
			},
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func planHostVirtctlActivities(graph *ActivityGraph, state v1alpha1.State, phaseSet map[string]bool, includeContainer bool, hostVirtctlReadiness map[string][]CapabilityRef) error {
	if !(phaseSet[ApplyPhaseDeps] && includeContainer) {
		return nil
	}
	hostNames := make([]string, 0, len(hostVirtctlReadiness))
	for host := range hostVirtctlReadiness {
		hostNames = append(hostNames, host)
	}
	sort.Strings(hostNames)
	virtctlMirror := render.VirtctlMirrorOverride(state)
	for _, host := range hostNames {
		virtctlID := "virtctl." + host
		extraVars := []string{"bootwright_task_host_cluster_name=" + host}
		if virtctlMirror != "" {
			extraVars = append(extraVars, "bootwright_virtctl_mirror_base="+virtctlMirror)
		}
		if err := graph.Add(Activity{
			ID:       virtctlID,
			Requires: hostVirtctlReadiness[host],
			Provides: []CapabilityRef{virtctlProvisionedCapability(host)},
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:          virtctlID,
					Kind:        ApplyTaskKindHostVirtctl,
					Label:       "provision virtctl for host " + host,
					Cluster:     host,
					ClusterKind: ApplyClusterKindContainer,
					Status:      TaskStatusPending,
				},
				Playbook:        applyHostVirtctlPlaybook,
				Limit:           render.GroupOCPHosts,
				Forks:           1,
				ExtraVarPairs:   extraVars,
				State:           state,
				DesiredHashVars: virtctlDesiredHashVars(host, virtctlMirror),
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func planContainerInstallActivities(graph *ActivityGraph, state v1alpha1.State, phaseSet map[string]bool, includeContainer bool, clusterNames []string, kubeVirtReqsByCluster map[string][]CapabilityRef, infraDepsByCluster, prepareDepsByCluster map[string][]string, machineServiceTaskIDs []string, hostsByContainerCluster map[string][]string) error {
	for _, name := range clusterNames {
		isoTaskID := "iso." + name
		if phaseSet[ApplyPhaseDeps] && includeContainer {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			fabricDeps := appendUniqueStrings(append([]string(nil), machineServiceTaskIDs...), prepareDepsByCluster[name]...)
			if err := graph.Add(Activity{
				ID:                   isoTaskID,
				ExplicitDependencies: fabricDeps,
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:          isoTaskID,
						Kind:        ApplyTaskKindClusterISO,
						Label:       "iso " + name,
						Cluster:     name,
						ClusterKind: ApplyClusterKindContainer,
						Status:      TaskStatusPending,
					},
					Playbook:           applyCreateISOPlaybook,
					Limit:              render.GroupOCPHosts,
					Forks:              1,
					ExtraVarPairs:      []string{"bootwright_task_cluster_name=" + name},
					State:              clusterState,
					StructuralHashVars: containerClusterInstallStructuralHashVars(clusterState),
				},
			}); err != nil {
				return err
			}
		}
		if phaseSet[ApplyPhaseBase] && includeContainer {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			machineNames := applyClusterMachineNames(state, name)
			isoDeps := []string(nil)
			if phaseSet[ApplyPhaseDeps] {
				isoDeps = []string{isoTaskID}
			}
			bootTaskID := ""
			if len(machineNames) > 0 {
				bootTaskID = "boot." + name
				bootRequires := append([]CapabilityRef(nil), kubeVirtReqsByCluster[name]...)
				if phaseSet[ApplyPhaseDeps] {
					for _, host := range hostsByContainerCluster[name] {
						bootRequires = appendUniqueCapability(bootRequires, virtctlProvisionedCapability(host))
					}
				}
				bootDeps := appendUniqueStrings(append([]string(nil), isoDeps...), infraDepsByCluster[name]...)
				if err := graph.Add(Activity{
					ID:                   bootTaskID,
					Requires:             bootRequires,
					ExplicitDependencies: bootDeps,
					Task: ApplyTask{
						Entry: TaskLedgerEntry{
							ID:           bootTaskID,
							Kind:         ApplyTaskKindNodeBoot,
							Label:        "boot " + name + " nodes",
							Cluster:      name,
							ClusterKind:  ApplyClusterKindContainer,
							ResourceKeys: applyNodeBootResourceKeys(state, name, machineNames),
							Status:       TaskStatusPending,
						},
						Playbook:           applyBootMachinePlaybook,
						Limit:              render.AgentNodeGroupName(name),
						ExtraVarPairs:      []string{"bootwright_task_cluster_name=" + name},
						State:              clusterState,
						StructuralHashVars: containerClusterInstallStructuralHashVars(clusterState),
						Forks:              len(machineNames),
						RedfishSlots:       len(machineNames),
					},
				}); err != nil {
					return err
				}
			}
			waitDeps := []string{}
			if bootTaskID != "" {
				waitDeps = append(waitDeps, bootTaskID)
			} else {
				waitDeps = append(waitDeps, isoDeps...)
			}
			bootstrapID := containerBootstrapWaitTaskID(name)
			if err := graph.Add(Activity{
				ID:                   bootstrapID,
				Provides:             []CapabilityRef{clusterBootstrappedCapability(name)},
				ExplicitDependencies: waitDeps,
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:          bootstrapID,
						Kind:        ApplyTaskKindBootstrapWait,
						Label:       "wait bootstrap " + name,
						Cluster:     name,
						ClusterKind: ApplyClusterKindContainer,
						Status:      TaskStatusPending,
					},
					Playbook:           applyWaitInstallPlaybook,
					Limit:              render.GroupOCPHosts,
					Forks:              1,
					ExtraVarPairs:      []string{"bootwright_task_cluster_name=" + name, "bootwright_install_wait_target=bootstrap"},
					State:              clusterState,
					StructuralHashVars: containerClusterInstallStructuralHashVars(clusterState),
				},
			}); err != nil {
				return err
			}
			waitID := "wait." + name
			if err := graph.Add(Activity{
				ID:                   waitID,
				Provides:             []CapabilityRef{clusterInstalledCapability(name)},
				ExplicitDependencies: []string{bootstrapID},
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:          waitID,
						Kind:        ApplyTaskKindInstallWait,
						Label:       "wait install " + name,
						Cluster:     name,
						ClusterKind: ApplyClusterKindContainer,
						Status:      TaskStatusPending,
					},
					Playbook:           applyWaitInstallPlaybook,
					Limit:              render.GroupOCPHosts,
					Forks:              1,
					ExtraVarPairs:      []string{"bootwright_task_cluster_name=" + name, "bootwright_install_wait_target=install"},
					State:              clusterState,
					StructuralHashVars: containerClusterInstallStructuralHashVars(clusterState),
				},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func containerBootstrapWaitTaskID(cluster string) string {
	return "wait-bootstrap." + cluster
}

func clusterBootstrappedCapability(cluster string) CapabilityRef {
	return CapabilityRef{Kind: "cluster.bootstrapped", Name: cluster}
}

func virtctlDesiredHashVars(host, mirror string) map[string]string {
	return map[string]string{
		"hostCluster":   host,
		"virtctlMirror": mirror,
	}
}

func selectedMachineNames(names []string, target ApplyTarget) []string {
	if !target.MachineScoped() {
		return names
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if target.MachineIncluded(name) {
			out = append(out, name)
		}
	}
	return out
}

func retainSelectedHosts(hosts []string, selected map[string]bool, scoped bool) []string {
	if !scoped {
		return hosts
	}
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if selected[host] {
			out = append(out, host)
		}
	}
	return out
}
