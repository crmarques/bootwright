package converge

import (
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
