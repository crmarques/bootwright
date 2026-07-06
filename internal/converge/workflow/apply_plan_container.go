package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

// The container-cluster apply chain, driving openshift-install agent:
//
//	machine infra prepare/provision/finalize (machines) -> agent ISO (deps)
//	  -> boot nodes -> wait for install-complete (base)
//
// with a per-KubeVirt-host virtctl provision (deps) that every child boot waits
// on. Machine-infra returns the per-cluster finalize task IDs the ISO stage
// depends on; the install stage reads the shared virtctl readiness the caller
// computed once.

// planContainerMachineInfraActivities plans per-machine provisioning and the
// per-host infra finalize for every container cluster. It returns the finalize
// task IDs (infraDepsByCluster) the agent-ISO stage depends on.
func planContainerMachineInfraActivities(graph *ActivityGraph, state v1alpha1.State, phaseSet map[string]bool, includeContainer bool, clusterNames []string, kubeVirtReqsByCluster map[string][]CapabilityRef, machineServiceTaskIDs []string) (map[string][]string, error) {
	infraDepsByCluster := map[string][]string{}
	if !(phaseSet[ApplyPhaseMachines] && includeContainer) {
		return infraDepsByCluster, nil
	}
	for _, name := range clusterNames {
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
					// Finalize is a post-provision reconcile step, not a disk-wipe
					// reinstall; without a structural projection any day-2 edit
					// (a host label, a ClusterAddon) flipped it to structural drift
					// and continue refused with a false "would reinstall the machine
					// — its disks wiped". Share the install task's projection so
					// only a genuine install-identity change stays a rebuild.
					StructuralHashVars: containerClusterInstallStructuralHashVars(clusterState),
				},
			}); err != nil {
				return nil, err
			}
		}
	}
	return infraDepsByCluster, nil
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
				// Prepare is an idempotent pre-provision step, not a disk-wipe
				// reinstall; share the install task's structural projection so a
				// reconcilable day-2 edit (label/addon/pool) does not falsely flip it
				// to structural drift and refuse the run as a machine re-image.
				StructuralHashVars: containerClusterInstallStructuralHashVars(clusterState),
			},
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// planHostVirtctlActivities provisions a version-matched virtctl on the controller
// for each distinct KubeVirt host cluster, before any child boots. virtctl runs on
// the controller (the agent-node layer connects locally), so one provision per
// host suffices; each child's boot task waits on its host's provision. The caller
// passes the shared readiness map it computed once.
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

// planContainerInstallActivities builds and publishes the agent install ISO
// (deps), then boots the declared nodes and waits for openshift-install to
// converge (base). boot/wait depend on the ISO task only when deps is also in
// scope (the iso activity exists in this graph); when only base is selected the
// dep is omitted so the run reuses the ISO a prior deps run published, instead of
// blocking on a task that was never planned — the same conditional-omit pattern
// the storage/addons extension activities use (installPhasePlanned).
func planContainerInstallActivities(graph *ActivityGraph, state v1alpha1.State, phaseSet map[string]bool, includeContainer bool, clusterNames []string, kubeVirtReqsByCluster map[string][]CapabilityRef, infraDepsByCluster map[string][]string, hostsByContainerCluster map[string][]string) error {
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
				return err
			}
		}
		// base (clusters): boot nodes then wait for openshift-install to converge.
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
					return err
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
				return err
			}
		}
	}
	return nil
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
