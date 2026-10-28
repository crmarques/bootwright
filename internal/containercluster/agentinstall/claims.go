package agentinstall

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func (MediaCapability) Claimed(catalog api.Catalog) []string { return installable(catalog) }

func (InstallCapability) Claimed(catalog api.Catalog) []string { return installable(catalog) }

func installable(catalog api.Catalog) []string {
	refused := lifecycle.Identities(Refusals(catalog))
	var claimed []string
	for _, cluster := range catalog.OfKind(api.OpenShiftCluster) {
		if !slices.Contains(refused, cluster.Identity()) {
			claimed = append(claimed, cluster.Identity())
		}
	}
	return claimed
}
