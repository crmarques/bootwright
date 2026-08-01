package converge

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func OverrideDriftedStorageSubObjects(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if workflow.IsStorageSubObjectKind(o.Kind) && o.HasStructuralDrift() {
			out = append(out, o.Label)
		}
	}
	return out
}

func storageSubObjectRebuildKey(o workflow.ObjectClassification) string {
	name := strings.TrimPrefix(o.Label, o.Kind+"/")
	name = strings.TrimPrefix(name, o.Cluster+".")
	return o.Cluster + "/" + name
}

func SubObjectRebuildAuthorizedKeys(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if workflow.IsStorageSubObjectKind(o.Kind) && o.HasStructuralDrift() {
			out = append(out, storageSubObjectRebuildKey(o))
		}
	}
	return out
}

func AllStorageSubObjectRebuildKeys(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if workflow.IsStorageSubObjectKind(o.Kind) {
			out = append(out, storageSubObjectRebuildKey(o))
		}
	}
	return out
}

func ApplySubObjectRebuildAuthorizedExtraVar(plan *WorkflowPlan, keys []string) {
	if len(keys) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_ceph_subobject_rebuild_authorized="+strings.Join(keys, ","))
}

func OverrideDestructiveStorageClusters(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if o.Kind == workflow.ObjectKindStorageCluster && o.HasStructuralDrift() {
			out = append(out, o.Label)
		}
	}
	return out
}

func RebuildAuthorizedStorageClusters(objects []workflow.ObjectClassification) []string {
	labels := OverrideDestructiveStorageClusters(objects)
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		names = append(names, strings.TrimPrefix(label, "StorageCluster/"))
	}
	return names
}

func ApplyRebuildAuthorizedStorageExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_ceph_rebuild_authorized_clusters="+strings.Join(names, ","))
}

func ReconcilableOnlyStorageClusters(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if o.Kind == workflow.ObjectKindStorageCluster && o.Reconcilable {
			out = append(out, strings.TrimPrefix(o.Label, "StorageCluster/"))
		}
	}
	return out
}

func ApplyReconcilableOnlyStorageExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_ceph_reconcilable_only_clusters="+strings.Join(names, ","))
}

func ApplySubstrateResetExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_substrate_reset_clusters="+strings.Join(names, ","))
}

func ApplySubstrateResetMachinesExtraVar(plan *WorkflowPlan, pairs []string) {
	if len(pairs) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_substrate_reset_machines="+strings.Join(pairs, ","))
}

func ApplyOCPReinstallClustersExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_ocp_reinstall_clusters="+strings.Join(names, ","))
}

func ApplyOCPRebuildAuthorizedClustersExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_ocp_rebuild_authorized_clusters="+strings.Join(names, ","))
}

func OwnedStorageClusters(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if o.Kind == workflow.ObjectKindStorageCluster && o.Recorded() && !o.HasForeign() {
			out = append(out, strings.TrimPrefix(o.Label, "StorageCluster/"))
		}
	}
	return out
}

func ApplyFilterReclaimAuthorizedExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, "bootwright_ceph_filter_reclaim_clusters="+strings.Join(names, ","))
}

func ApplyCephUnownedDeviceAuthorizationExtraVar(plan *WorkflowPlan, authorized bool) {
	if !authorized {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, CephAuthorizeUnownedDevicesExtraVar+"=true")
}

func ApplyCephForeignDaemonAuthorizationExtraVar(plan *WorkflowPlan, authorized bool) {
	if !authorized {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, CephAuthorizeForeignDaemonsExtraVar+"=true")
}

func ApplyReclaimDevicesExtraVars(plan *WorkflowPlan, devices string, ownedClusters []string) {
	if strings.TrimSpace(devices) == "" {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs,
		"bootwright_ceph_reclaim_devices="+devices,
		"bootwright_ceph_owned_clusters="+strings.Join(ownedClusters, ","))
}
