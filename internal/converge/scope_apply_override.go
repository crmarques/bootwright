package converge

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// OverrideDriftedStorageSubObjects returns the labels of storage sub-objects that
// --override would DESTRUCTIVELY rebuild — those with structural (immutable-identity)
// drift: a pool type / EC profile, or a CephFS metadata / default-data pool. A
// sub-object whose only drift is reconcilable in place (pool size/quota/compression/
// crush, MDS placement, an added data pool) is excluded, because the ceph set-* / add
// operations reconcile it without data loss. It drives the data-loss warning before
// the confirm.
func OverrideDriftedStorageSubObjects(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if workflow.IsStorageSubObjectKind(o.Kind) && o.HasStructuralDrift() {
			out = append(out, o.Label)
		}
	}
	return out
}

// OverrideDestructiveStorageClusters returns the labels of structurally drifted
// StorageClusters that --override would wipe and rebuild (cephadm rm-cluster
// --zap-osds), so the pre-confirm data-loss warning names the full-cluster OSD
// wipe even when the drift is in the cluster's own identity (seedHost/monIP/network)
// and no pool/filesystem sub-object drifted — the case the sub-object warning
// misses. A reconcilable-in-place OSD-device add is not a wipe and is excluded.
func OverrideDestructiveStorageClusters(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if o.Kind == workflow.ObjectKindStorageCluster && o.HasStructuralDrift() {
			out = append(out, o.Label)
		}
	}
	return out
}

// ReconcilableOnlyStorageClusters returns the bare names of StorageClusters whose
// only drift is a reconcilable-in-place OSD-device edit (no structural/identity
// drift). Under --override the seed role reconciles these via `ceph orch apply`
// instead of wiping them with rm-cluster --zap-osds; the name list is passed to
// Ansible so the zap is suppressed for exactly these clusters.
func ReconcilableOnlyStorageClusters(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if o.Kind == workflow.ObjectKindStorageCluster && o.Reconcilable {
			out = append(out, strings.TrimPrefix(o.Label, "StorageCluster/"))
		}
	}
	return out
}

// ApplyReconcilableOnlyStorageExtraVar threads the reconcilable-only StorageCluster
// names to the seed role so its --override apply-mode gate reconciles those
// clusters in place instead of running the destructive rm-cluster --zap-osds. An
// empty list appends nothing, so a cluster with structural drift (or none) keeps
// the existing override rebuild behavior.
func ApplyReconcilableOnlyStorageExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_ceph_reconcilable_only_clusters="+strings.Join(names, ","))
}

// OwnedStorageClusters returns the bare names of StorageClusters the controller
// records as Bootwright-owned (recorded and not foreign). --reclaim-devices keys on
// this so an in-band device wipe is only ever authorized for a cluster Bootwright
// provisioned — a managed-OS reinstall wipes the on-node ownership markers but the
// controller-side record survives and proves ownership.
func OwnedStorageClusters(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if o.Kind == workflow.ObjectKindStorageCluster && o.Recorded() && !o.HasForeign() {
			out = append(out, strings.TrimPrefix(o.Label, "StorageCluster/"))
		}
	}
	return out
}

// ApplyReclaimDevicesExtraVars threads the operator's --reclaim-devices list and
// the controller-owned StorageCluster names to the seed role so it wipes exactly
// the named devices in-band, gated on the device being a declared OSD device of an
// owned cluster and not mounted or a system disk. An empty device list appends
// nothing (reclaim off).
func ApplyReclaimDevicesExtraVars(plan *WorkflowPlan, devices string, ownedClusters []string) {
	if strings.TrimSpace(devices) == "" {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs,
		"bootwright_ceph_reclaim_devices="+devices,
		"bootwright_ceph_owned_clusters="+strings.Join(ownedClusters, ","))
}
