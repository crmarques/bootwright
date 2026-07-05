package workflow

import (
	"encoding/json"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/graph"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func PlanApplyTasksChecked(target ApplyTarget, state v1alpha1.State) ([]ApplyTask, error) {
	phaseSet := map[string]bool{}
	for _, phase := range target.PhaseNames {
		phaseSet[phase] = true
	}
	// deps and base are shared by storage and container clusters; ClusterKind
	// lets a single-kind scope plan only its kind so the unified gates do not
	// pull in the other kind's tasks.
	includeStorage := target.ClusterKind != ApplyClusterKindContainer
	includeContainer := target.ClusterKind != ApplyClusterKindStorage
	graph := NewActivityGraph()
	addAvailableMachineOSCapabilities(graph, state)
	kubeVirtReqsByCluster := map[string][]CapabilityRef{}
	if phaseSet[ApplyPhaseMachines] || phaseSet[ApplyPhaseDeps] || phaseSet[ApplyPhaseBase] {
		var err error
		kubeVirtReqsByCluster, err = kubeVirtHostClusterApplyCapabilities(state)
		if err != nil {
			return nil, err
		}
	}
	machineServiceTaskIDs, err := planMachineServiceActivities(graph, state, phaseSet)
	if err != nil {
		return nil, err
	}
	// machines (infra): managed-OS install on storage nodes — the storage-side
	// twin of clusterInstall (provides machine.instantiated + machine.os-ready),
	// so it lives in the infra family, not with the cephadm prereqs.
	managedOSDepsByCluster := map[string][]string{}
	if phaseSet[ApplyPhaseMachines] && includeStorage {
		for _, cluster := range state.StorageClusters {
			if !v1alpha1.StorageClusterManaged(cluster) {
				continue
			}
			if !storageClusterSelectedForTarget(target, cluster.Metadata.Name) {
				continue
			}
			managedOSMachines := managedOSMachineNames(state, cluster)
			if len(managedOSMachines) == 0 {
				continue
			}
			prepareDepsByHost, err := planStorageManagedOSPrepareTasks(graph, state, cluster.Metadata.Name, managedOSMachines, machineServiceTaskIDs)
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
					Playbook:      applyManagedMachineOSPlaybook,
					Limit:         render.ManagedOSGroupName(cluster.Metadata.Name),
					ExtraVarPairs: []string{"bootwright_task_managed_os_group_name=" + cluster.Metadata.Name},
					State:         storageTaskState(state, cluster.Metadata.Name),
					// A pool/topology/OSD-device/BMC edit changes the full state hash but
					// not the OS-install identity: classify it reconcilable in place so
					// apply does not refuse it as a machine-disk wipe reinstall.
					StructuralHashVars: managedMachineOSStructuralHashVars(state, cluster.Metadata.Name),
					Forks:              len(managedOSMachines),
					RedfishSlots:       len(managedOSMachines),
				},
			}); err != nil {
				return nil, err
			}
		}
	}
	// deps (clusters): cephadm + dependencies on storage nodes, before bootstrap.
	// Depends on the managed-OS install (machines family) via managedOSDepsByCluster
	// when both phases are planned together; otherwise the os-ready capability is
	// satisfied by a prior run (addAvailableMachineOSCapabilities / provided OS).
	storageInfraDepsByCluster := map[string][]string{}
	if phaseSet[ApplyPhaseDeps] && includeStorage {
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
	}
	storageDepsByCluster := map[string][]string{}
	if phaseSet[ApplyPhaseBase] && includeStorage {
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
					Limit:              render.StorageSeedHostName(cluster),
					ExtraVarPairs:      []string{"bootwright_task_storage_cluster_name=" + cluster.Metadata.Name, "bootwright_task_storage_skip_prereqs=true"},
					State:              storageTaskState(state, cluster.Metadata.Name),
					DesiredHashVars:    storageClusterDesiredHashVars(state, cluster.Metadata.Name),
					StructuralHashVars: storageClusterStructuralHashVars(state, cluster.Metadata.Name),
				},
			}); err != nil {
				return nil, err
			}
		}
	}
	infraDepsByCluster := map[string][]string{}
	clusterNames := applyClusterNames(state)
	for _, name := range clusterNames {
		if phaseSet[ApplyPhaseMachines] && includeContainer {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			infraHosts := render.HostGroupMembers(clusterState)[render.GroupInfraHosts]
			baseDeps := append([]string(nil), machineServiceTaskIDs...)
			prepareDepsByHost, err := planContainerMachinePrepareTasks(graph, state, name, infraHosts, baseDeps)
			if err != nil {
				return nil, err
			}
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
					return nil, err
				}
			}
			for _, host := range infraHosts {
				taskID := "infrafinalize." + name + "." + host
				// host is "localhost" for KubeVirt/vSphere (the finalize runs on
				// the controller); only name the host when it is a real, distinct
				// provider machine (libvirt) where it disambiguates per-host tasks.
				finalizeLabel := "finalize infra " + name
				if host != "localhost" {
					finalizeLabel += " on " + host
				}
				deps := append([]string(nil), baseDeps...)
				deps = append(deps, machineTaskIDsByHost[host]...)
				if prepareID := prepareDepsByHost[host]; prepareID != "" && len(machineTaskIDsByHost[host]) == 0 {
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
						Playbook:      applyMachineInfraFinalize,
						Limit:         host,
						ExtraVarPairs: []string{"bootwright_task_cluster_name=" + name, "bootwright_task_provider_host_name=" + host},
						Forks:         1,
						State:         clusterState,
					},
				}); err != nil {
					return nil, err
				}
			}
		}
	}
	// deps (clusters): provision a version-matched virtctl on the controller for
	// each distinct KubeVirt host cluster, before any child boots. virtctl runs on
	// the controller (the agent-node layer connects locally), so one provision per
	// host suffices; each child's boot task waits on its host's provision.
	hostVirtctlReadiness, hostsByContainerCluster, err := kubeVirtHostClusterReadiness(state)
	if err != nil {
		return nil, err
	}
	if phaseSet[ApplyPhaseDeps] && includeContainer {
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
				return nil, err
			}
		}
	}
	for _, name := range clusterNames {
		infraDeps := append([]string(nil), infraDepsByCluster[name]...)
		isoTaskID := "iso." + name
		// deps (clusters): build + publish the agent install ISO — the OCP/OKD
		// pre-bringup asset, the container-side twin of the cephadm prereqs.
		if phaseSet[ApplyPhaseDeps] && includeContainer {
			clusterState := stategraph.FilterStateToClusters(state, []string{name})
			if err := graph.Add(Activity{
				ID:                   isoTaskID,
				Requires:             kubeVirtReqsByCluster[name],
				ExplicitDependencies: infraDeps,
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
				return nil, err
			}
		}
		// base (clusters): boot nodes then wait for openshift-install to converge.
		// boot/wait depend on the ISO task only when deps is also in scope (the
		// iso activity exists in this graph). When only base is selected, the dep
		// is omitted so the run reuses the ISO a prior deps run published, instead
		// of blocking on a task that was never planned. Same conditional-omit
		// pattern the storage/addons extension activities use (installPhasePlanned).
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
				if err := graph.Add(Activity{
					ID:                   bootTaskID,
					Requires:             bootRequires,
					ExplicitDependencies: isoDeps,
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
					return nil, err
				}
			}
			waitDeps := []string{}
			if bootTaskID != "" {
				waitDeps = append(waitDeps, bootTaskID)
			} else {
				// No machines to boot: wait orders behind the ISO task when deps
				// is in scope, else nothing (isoDeps is empty) so a base-only run
				// reuses a prior deps run's ISO instead of blocking on it.
				waitDeps = append(waitDeps, isoDeps...)
			}
			waitID := "wait." + name
			if err := graph.Add(Activity{
				ID:                   waitID,
				Provides:             []CapabilityRef{clusterInstalledCapability(name)},
				ExplicitDependencies: waitDeps,
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
					ExtraVarPairs:      []string{"bootwright_task_cluster_name=" + name},
					State:              clusterState,
					StructuralHashVars: containerClusterInstallStructuralHashVars(clusterState),
				},
			}); err != nil {
				return nil, err
			}
		}
	}
	if phaseSet[ApplyPhaseAddons] {
		if err := planExtensionActivities(graph, state, phaseSet[ApplyPhaseBase]); err != nil {
			return nil, err
		}
		if err := planStorageAttachmentActivities(graph, state, phaseSet[ApplyPhaseBase], storageDepsByCluster); err != nil {
			return nil, err
		}
		if err := planNodeConfigActivities(graph, state, phaseSet[ApplyPhaseBase]); err != nil {
			return nil, err
		}
	}
	// ProvisioningPlaybooks anchor to any of the five phases, so they plan after
	// every core activity is added — the phase index they wire against reads the
	// completed graph snapshot.
	if err := planProvisioningPlaybookActivities(graph, state, phaseSet, target); err != nil {
		return nil, err
	}
	return graph.Lower()
}

// virtctlDesiredHashVars projects the desired-state inputs the per-host virtctl
// provision actually depends on — the KubeVirt host cluster identity and the
// optional virtctl mirror override — so the task's convergence hash is stable
// regardless of the --clusters scope a run was planned with. The task still
// carries the full planning State for execution, but that State is the
// --clusters-filtered set on a scoped run; hashing it flipped an unscoped
// state-check to drift after a scoped apply (and so fail-closed the next
// reconcile). Mirrors the fabric/storage DesiredHashVars projection pattern; a
// map[string]string marshals with sorted keys, so the hash input is
// order-stable.
func virtctlDesiredHashVars(host, mirror string) map[string]string {
	return map[string]string{
		"hostCluster":   host,
		"virtctlMirror": mirror,
	}
}

func planExtensionActivities(graph *ActivityGraph, state v1alpha1.State, installPhasePlanned bool) error {
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return err
	}
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
			// One task per addon installs (oc apply) and then waits for the addon
			// to report ready; it provides the capabilities downstream tasks need.
			id := "addon." + binding.Cluster + "." + extension.Name
			provides := addonProvidedCapabilities(binding.Cluster, extension.Extension)
			if err := graph.Add(Activity{
				ID:                   id,
				Provides:             provides,
				ExplicitDependencies: append([]string(nil), deps...),
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:          id,
						Kind:        ApplyTaskKindClusterAddon,
						Label:       "addon " + extension.Name,
						Cluster:     binding.Cluster,
						ClusterKind: ApplyClusterKindContainer,
						Status:      TaskStatusPending,
					},
					State:     stategraph.FilterStateToClusters(state, []string{binding.Cluster}),
					Extension: &extension,
				},
			}); err != nil {
				return err
			}
			deps = []string{id}
		}
	}
	return nil
}

func addAvailableMachineOSCapabilities(graph *ActivityGraph, state v1alpha1.State) {
	for _, machine := range state.Machines {
		if v1alpha1.MachineOSProvided(machine) {
			graph.AddAvailable(machineOSReadyCapability(machine.Metadata.Name))
		}
	}
}

func addonProvidedCapabilities(cluster string, extension v1alpha1.ClusterAddon) []CapabilityRef {
	out := make([]CapabilityRef, 0, len(extension.Spec.Provides))
	for _, capability := range extension.Spec.Provides {
		if capability == "" {
			continue
		}
		out = append(out, addonProvidesCapability(cluster, capability))
	}
	return out
}

func planContainerMachinePrepareTasks(graph *ActivityGraph, state v1alpha1.State, clusterName string, hosts []string, deps []string) (map[string]string, error) {
	out := map[string]string{}
	clusterState := stategraph.FilterStateToClusters(state, []string{clusterName})
	for _, host := range hosts {
		if !clusterHostNeedsSubstratePrepare(state, clusterName, host) {
			continue
		}
		taskID := "infraprepare." + clusterName + "." + host
		out[host] = taskID
		if err := graph.Add(Activity{
			ID:                   taskID,
			Requires:             []CapabilityRef{providerHostReadyCapability(host)},
			ExplicitDependencies: append([]string(nil), deps...),
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
				Playbook:      applyMachineInfraPrepare,
				Limit:         host,
				ExtraVarPairs: []string{"bootwright_task_cluster_name=" + clusterName, "bootwright_task_provider_host_name=" + host},
				Forks:         1,
				State:         clusterState,
			},
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
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
				Playbook:      applyMachineInfraPrepare,
				Limit:         host,
				ExtraVarPairs: []string{"bootwright_task_cluster_name=" + clusterName, "bootwright_task_provider_host_name=" + host},
				Forks:         1,
				State:         storageTaskState(state, clusterName),
			},
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func clusterHostNeedsSubstratePrepare(state v1alpha1.State, clusterName, host string) bool {
	cluster, ok := containerClusterByName(state, clusterName)
	if !ok {
		return false
	}
	for _, node := range cluster.Spec.Hosts {
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
	case v1alpha1.ProvisionerKubeVirt, v1alpha1.ProvisionerVSphere:
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
	if ok && provider.Spec.Type == v1alpha1.ProvisionerVSphere && provider.Spec.VSphere != nil {
		return []string{vsphereResourceKey(provider, machine)}
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
		for _, node := range cluster.Spec.Hosts {
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

// containerClusterInstallStructuralHashVars projects the DESTRUCTIVE-IDENTITY subset of
// a ContainerCluster's install intent: the cluster-filtered state with the day-2-owned
// intent cleared. Cluster add-ons and per-node labels/taints are applied AFTER install
// by the add-on and node-config tasks (their own reconfigure-only, non-destructive
// re-apply), so editing them must not flip the install object to a destructive reinstall.
// When the full desired hash drifts but this structural hash is unchanged, the only
// change is day-2 intent — reconcilable in place, so continue proceeds and --override
// does not reinstall. A change to install-config / agent-config identity (networks,
// platform, release, endpoints, host machineRefs, roles, FIPS) moves this hash and stays
// a destructive rebuild; the install-state reconcile gate (clusterInstallDesiredHashForContext)
// is the precise second backstop that still refuses regenerating install inputs for an
// installed cluster. The day-2 fields are cleared on a JSON deep copy so the shared
// render state is never mutated. Mirrors storageClusterStructuralHashVars.
func containerClusterInstallStructuralHashVars(clusterState v1alpha1.State) v1alpha1.State {
	var clone v1alpha1.State
	data, err := json.Marshal(clusterState)
	if err != nil {
		return clusterState
	}
	if err := json.Unmarshal(data, &clone); err != nil {
		return clusterState
	}
	clone.ClusterAddons = nil
	clone.ClusterAddonBindings = nil
	clone.ClusterAddonProfiles = nil
	for i := range clone.ContainerClusters {
		for j := range clone.ContainerClusters[i].Spec.Hosts {
			clone.ContainerClusters[i].Spec.Hosts[j].Labels = nil
			clone.ContainerClusters[i].Spec.Hosts[j].Taints = nil
		}
	}
	return clone
}
