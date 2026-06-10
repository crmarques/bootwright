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
