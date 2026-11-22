package lifecycle

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// Generated add-on-stage blocks name their owning cluster as a direct requirement.
func clusterOwners(catalog api.Catalog, block reconciliation.BlockDefinition) []string {
	kind := api.Kind(block.Kind)
	if block.Stage == reconciliation.StageAddOns {
		for _, ref := range block.Requires {
			if api.IsClusterKind(api.Kind(ref.Kind)) {
				return []string{ref.Object}
			}
		}
	}
	if !clusterChild(block) {
		return block.ClusterOwners
	}
	object, found := catalog.Find(kind, block.Object)
	if !found {
		return nil
	}
	owners := []string{object.Spec().Get("clusterRef").Text()}
	if peer := object.Spec().Get("peerClusterRef").Text(); peer != "" {
		owners = append(owners, peer)
		slices.Sort(owners)
	}
	return slices.Compact(owners)
}

// clusterMachineOwners identifies the direct membership visible before
// discovery supplies enough evidence to build the installation DAG.
func clusterMachineOwners(catalog api.Catalog) map[string]string {
	owners := map[string]string{}
	for _, object := range catalog.Clusters() {
		for _, machine := range api.ClusterNodeMachines(object) {
			owners[machine] = object.Name()
		}
	}
	return owners
}

func clusterChild(block reconciliation.BlockDefinition) bool {
	kind := api.Kind(block.Kind)
	return block.Stage == reconciliation.StageAddOns ||
		!api.IsClusterKind(kind) && slices.Contains(api.FamilyKinds(), kind)
}
