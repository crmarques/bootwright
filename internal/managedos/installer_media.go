package managedos

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func InstallerMediaHosts(catalog api.Catalog) []string {
	var hosts []string
	for _, machine := range catalog.OfKind(api.Machine) {
		provided := machine.Spec().Get("os", "provided")
		if provided.Type() != api.Boolean || provided.Bool() {
			continue
		}
		profile, found := catalog.Find(api.MachineInstallProfile, machine.Spec().Get("os", "installProfileRef").Text())
		if !found || !profile.Spec().Has("installer", "anaconda") {
			continue
		}
		endpoint := profile.Spec().Get("installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint")
		if server, found := catalog.Find(api.ArtifactServer, endpoint.Get("serverRef").Text()); found {
			hosts = append(hosts, infrastructureservices.Placement(server))
		}
	}
	slices.Sort(hosts)
	return slices.Compact(hosts)
}
