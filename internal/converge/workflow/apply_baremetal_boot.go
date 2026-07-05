package workflow

import "sort"

// BareMetalFirstInstallClusters returns the names of ContainerClusters that a run
// will boot-and-install onto BARE-METAL hosts for the first time — a nodeBoot
// (Redfish virtual-media) task is planned for the cluster AND no convergence-safety
// record backs it (an unrecorded, first-apply cluster). A nodeBoot task is emitted
// only for a Redfish/bare-metal cluster (KubeVirt and vSphere clusters boot a VM,
// not a physical host), so this is exactly the set whose first apply drives
// coreos-installer to DISK-WIPE a physical machine.
//
// It exists so the CLI can surface an irreversible-disk-wipe warning before the
// confirm: bootwright has no pre-boot occupancy probe on the bare-metal OCP path
// (a deferred, hardware-dependent driver), so a first apply onto a host already
// running production — a mis-pointed BMC, or a re-apply after the controller
// records were lost — would silently re-image it. Naming the hosts at confirm time
// is the interim guard: the operator sees which physical machines are about to be
// wiped and can abort. An already-recorded (owned) cluster is excluded — its
// install-state gate and healthy-skip already protect a healthy in-sync cluster.
func BareMetalFirstInstallClusters(objects []ObjectClassification, tasks []ApplyTask) []string {
	recorded := map[string]bool{}
	for _, o := range objects {
		if o.Kind == ObjectKindContainerCluster {
			recorded[o.Cluster] = o.Recorded()
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, task := range tasks {
		if task.Entry.Kind != ApplyTaskKindNodeBoot {
			continue
		}
		cluster := task.Entry.Cluster
		if recorded[cluster] || seen[cluster] {
			continue
		}
		seen[cluster] = true
		out = append(out, cluster)
	}
	sort.Strings(out)
	return out
}
