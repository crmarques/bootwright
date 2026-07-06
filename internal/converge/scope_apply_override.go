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

// RebuildAuthorizedStorageClusters returns the bare names of the StorageClusters
// whose --override rebuild the controller POSITIVELY authorizes to run the
// destructive cephadm rm-cluster --zap-osds — exactly the structurally drifted
// owned clusters of OverrideDestructiveStorageClusters, with the label prefix
// stripped. It is the single source of truth shared by the pre-confirm data-loss
// warning (which lists the same clusters) and the Ansible zap gate (which requires
// the cluster be named here before it wipes), so the operator can never be warned
// about one set and wiped on another.
//
// The list is a POSITIVE authorization token, not an opt-out: the seed role's zap
// runs only for a cluster named here, so a healthy match (no drift) and a
// reconcilable-only OSD-device add — neither of which appears in this list — are
// never wiped under --override, they reconcile idempotently in place. An absent or
// empty token therefore fails safe: no cluster is authorized, so a stale bundle or
// a Go layer that forgot to thread it can only under-authorize (skip a rebuild),
// never over-authorize (wipe a cluster that should have been left alone).
func RebuildAuthorizedStorageClusters(objects []workflow.ObjectClassification) []string {
	labels := OverrideDestructiveStorageClusters(objects)
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		names = append(names, strings.TrimPrefix(label, "StorageCluster/"))
	}
	return names
}

// ApplyRebuildAuthorizedStorageExtraVar threads the positive rebuild-authorization
// token to the seed role so its --override apply-mode gate wipes ONLY the named
// structurally drifted clusters. An empty list appends nothing; because the gate
// requires membership in this list, an absent var authorizes no wipe at all — the
// fail-safe default that makes a healthy owned cluster the untouched case.
func ApplyRebuildAuthorizedStorageExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_ceph_rebuild_authorized_clusters="+strings.Join(names, ","))
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

// ApplyOCPFirstInstallClustersExtraVar threads the names of the ContainerClusters a
// run will boot-and-install onto bare-metal hosts for the FIRST time (no controller
// record) to the boot role, so its pre-boot Redfish occupancy guard runs for exactly
// those clusters: it refuses to disk-wipe a host that already reports a running OS
// when bootwright has never provisioned the cluster (a mis-pointed BMC, a host
// repurposed from another cluster, or a re-apply after lost records). An owned
// cluster is absent from the list and never occupancy-checked, so a legitimate
// re-provision is unaffected. Empty appends nothing (the guard never runs).
func ApplyOCPFirstInstallClustersExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_ocp_first_install_clusters="+strings.Join(names, ","))
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
