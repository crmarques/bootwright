package agentinstall

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster"
)

func projectedVIPs(catalog api.Catalog, cluster api.Object) []VIP {
	controller := ""
	for _, environment := range catalog.OfKind(api.Environment) {
		controller = environment.Spec().Get("controller", "machineRef").Text()
	}
	result := []VIP{}
	for _, ip := range containercluster.VirtualIPs(catalog, cluster, controller) {
		result = append(result, VIP{Slot: ip.Slot, Address: ip.Address, Bridge: ip.Bridge})
	}
	return result
}
