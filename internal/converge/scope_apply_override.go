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

// OverrideDestructiveStorageClusters returns the labels of drifted StorageClusters
// that --override would wipe and rebuild (cephadm rm-cluster --zap-osds), so the
// pre-confirm data-loss warning names the full-cluster OSD wipe even when the drift
// is in the cluster's own topology (seedHost/monIP/network or OSD device selection)
// and no pool/filesystem sub-object drifted — the case the sub-object warning misses.
func OverrideDestructiveStorageClusters(objects []workflow.ObjectClassification) []string {
	var out []string
	for _, o := range objects {
		if o.Kind == workflow.ApplyTaskKindStorageCluster && o.HasDrift() {
			out = append(out, o.Label)
		}
	}
	return out
}
