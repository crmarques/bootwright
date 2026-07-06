package workflow

import (
	"encoding/json"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"github.com/crmarques/bootwright/internal/state/graph"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

type StorageAttachmentPlan struct {
	Cluster string
	Binding v1alpha1.ClusterAddonBinding
	Addon   v1alpha1.ClusterAddonBindingAddon
	Input   v1alpha1.ClusterAddonBindingInput
}

func planStorageAttachmentActivities(graph *ActivityGraph, state v1alpha1.State, installPhasePlanned bool, storageDepsByCluster map[string][]string) error {
	for _, effect := range addoninputs.StorageExportAttachments(state) {
		cluster := effect.Binding.Spec.ClusterRef.Name
		if !stateHasContainerCluster(state, cluster) {
			continue
		}
		export := effect.Export
		deps := []string{}
		if installPhasePlanned {
			deps = append(deps, "wait."+cluster)
		}
		deps = append(deps, storageDepsByCluster[export.Spec.StorageClusterRef.Name]...)
		deps = append(deps, "addon."+cluster+"."+effect.Addon.AddonRef.Name)
		id := "storageattachment." + cluster + "." + effect.Addon.AddonRef.Name + "." + effect.Input.Name + ".apply"
		attachmentPlan := StorageAttachmentPlan{Cluster: cluster, Binding: effect.Binding, Addon: effect.Addon, Input: effect.Input}
		if err := graph.Add(Activity{
			ID:                   id,
			ExplicitDependencies: deps,
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:          id,
					Kind:        ApplyTaskKindStorageAttachmentApply,
					Label:       "storage attachment " + cluster + " " + effect.Addon.AddonRef.Name + "/" + effect.Input.Name + " apply",
					Cluster:     cluster,
					ClusterKind: ApplyClusterKindContainer,
					Status:      TaskStatusPending,
				},
				State:             stategraph.FilterStateToClusters(state, []string{cluster}),
				StorageAttachment: &attachmentPlan,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func storageTaskState(state v1alpha1.State, name string) v1alpha1.State {
	filtered := stategraph.FilterStateToStorageClusters(state, []string{name})
	filtered.ContainerClusters = nil
	return filtered
}

// storageClusterDesiredHashVars projects the desired state hashed for a
// StorageCluster's convergence record: the cluster's own spec, topology, nodes,
// and bound machines — but NOT its pools, filesystems, object gateways, NFS
// exports, exports, or placement policies. Those sub-objects are classified
// independently (each is its own object), so adding or changing one must never
// flip the StorageCluster itself to drift. The task still carries the full State
// for rendering and for the ceph operations loop; only the hash input is
// projected, mirroring the fabric DesiredHashVars pattern.
func storageClusterDesiredHashVars(state v1alpha1.State, name string) v1alpha1.State {
	s := storageTaskState(state, name)
	s.StoragePlacementPolicies = nil
	s.StoragePools = nil
	s.StorageFilesystems = nil
	s.StorageObjectGateways = nil
	s.StorageNFSExports = nil
	s.StorageExports = nil
	return s
}

// storageClusterStructuralHashVars projects the DESTRUCTIVE-IDENTITY subset of a
// StorageCluster's desired state: the full desired-hash projection with the entire
// host topology (the host set, per-host roles/site/labels, the OSD device
// selection, and topology.osdDrivegroups) cleared. Everything cephadm reconciles in
// place via `ceph orch host add` + `ceph orch apply` — scaling out a node, adding a
// device, rebalancing a mon/mgr/mds daemon by editing host roles — keeps this hash
// stable and classifies reconcilable, so apply proceeds in place and --override does
// not wipe. Only a change to cluster/bootstrap IDENTITY (the bootstrap host, fsid
// seed, cluster/public networks, cluster name, distribution) moves this hash and
// stays a destructive rebuild.
//
// Making a host REMOVAL reconcilable here is safe because it is never a silent OSD
// wipe: a host still in the inventory whose declared devices shrank is caught by the
// device-removal gate (install.yml, "Refuse to drop an OSD device that still hosts an
// OSD"), and a host dropped from the topology entirely leaves the inventory group so
// bootwright never touches its OSDs — it lingers in the cluster until explicitly
// drained. The topology is cleared on a JSON deep copy so the shared render state is
// never mutated.
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
	// Shared fabric (provider BMC defaults, artifact-server/proxy/registry infra
	// components) is reconfigure-only and re-applied by its own task, so editing it
	// must not flip a StorageCluster to a destructive rebuild. cephadm reaches the
	// nodes over SSH, not the BMC, so none of it is cluster identity.
	clone.InfraProviders = nil
	clone.InfraComponents = nil
	for i := range clone.StorageClusters {
		ceph := clone.StorageClusters[i].Spec.Ceph
		if ceph == nil {
			continue
		}
		ceph.Topology.Hosts = nil
		ceph.Topology.OSDDrivegroups = nil
	}
	return clone
}

// managedMachineOSStructuralHashVars projects the DESTRUCTIVE-IDENTITY (disk-wipe
// reinstall) subset of a storage cluster's managed-OS install intent. The
// managedMachineOS task otherwise hashes the whole storage-filtered State, so ANY
// edit to the cluster — a pool size change, an OSD-device add, a machine's BMC
// endpoint — flipped the install object to structural drift and apply refused it
// as "would wipe the machine disks", even though none of those touch the installed
// OS. This projection clears exactly the fields the on-host install marker
// (machineOSInstallMarkerVars, which hashes only osInstall) ALSO excludes, so the
// Go classification and the Ansible probe agree: a change to one of them is
// reconcilable in place (the install is skipped, the storage/day-2 task applies the
// edit), never a reinstall. It deliberately does NOT touch the marker hash itself —
// re-hashing it would false-drift every already-installed host on upgrade.
//
// Cleared (not OS-install identity, absent from the marker): the storage
// sub-objects (pools/filesystems/gateways/NFS/exports/placement, each classified
// independently), the OSD device selection, and each machine's substrate (the
// BMC/Redfish endpoint, which is how bootwright reaches the host, applied each boot,
// not baked into the OS). Kept structural (present in the marker, a real reinstall
// trigger): the machine OS spec, hardware/disk layout, network, and access.
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
	// Provider BMC defaults and infra components are how bootwright reaches the host,
	// not part of the installed OS, so a fabric edit is not a reinstall (mirrors the
	// per-machine substrate clear below).
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
