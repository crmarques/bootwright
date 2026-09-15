package lifecycle

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// effectKinds are the API kinds whose selected objects need a lifecycle
// capability before an operation can realize them. A kind no capability claims
// refuses the whole operation rather than silently leaving its objects out.
func effectKinds() []api.Kind {
	return []api.Kind{
		api.InfraProvider,
		api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer,
		api.ContainerCluster, api.StorageCluster, api.StoragePlacementPolicy, api.StoragePool,
		api.StorageFilesystem, api.StorageObjectGateway, api.StorageNFSExport, api.StorageExport,
		api.ClusterAddon, api.ClusterAddonProfile, api.ClusterAddonBinding,
	}
}

// Unrealizable lists every selected object of an unclaimed effect-bearing kind,
// in canonical order. External services and declaration-only objects are inputs
// and never appear here.
func Unrealizable(catalog api.Catalog, claimed []string) []string {
	var found []string
	for _, kind := range effectKinds() {
		if slices.Contains(claimed, string(kind)) {
			continue
		}
		for _, object := range catalog.OfKind(kind) {
			if isService(kind) && object.Spec().Get("management").Text() != "managed" {
				continue
			}
			found = append(found, object.Identity())
		}
	}
	if !slices.Contains(claimed, string(api.Machine)) {
		for _, machine := range catalog.OfKind(api.Machine) {
			if provided := machine.Spec().Get("os", "provided"); provided.Type() == api.Boolean && !provided.Bool() {
				found = append(found, machine.Identity())
			}
		}
	}
	if !slices.Contains(claimed, string(api.CustomPlaybook)) {
		for _, playbook := range catalog.OfKind(api.CustomPlaybook) {
			if enabled := playbook.Spec().Get("enabled"); enabled.Type() == api.Boolean && enabled.Bool() {
				found = append(found, playbook.Identity())
			}
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

func isService(kind api.Kind) bool {
	return slices.Contains([]api.Kind{api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer}, kind)
}
