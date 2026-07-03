package converge

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// OverrideDriftedStorageSubObjects returns the labels of storage sub-objects that
// --override would rebuild (those that drifted from their recorded desired state), in
// the classifier's stable order. It drives the data-loss warning before the confirm.
func OverrideDriftedStorageSubObjects(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if workflow.IsStorageSubObjectKind(o.Kind) && o.HasDrift() {
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
		if o.Kind == workflow.ApplyTaskKindStorageCluster && o.HasStructuralDrift() {
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
		if o.Kind == workflow.ApplyTaskKindStorageCluster && o.Reconcilable {
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
