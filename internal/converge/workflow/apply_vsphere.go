package workflow

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func vsphereResourceKey(provider v1alpha1.InfraProvider, machine v1alpha1.Machine) string {
	spec := provider.Spec.VSphere
	server := ""
	if profile, ok := stateview.MachineProfile(provider, machine.Spec.Substrate.ProfileRef.Name); ok {
		if fd, ok := stateview.VSphereProfileFailureDomain(spec, profile); ok {
			server = fd.Server
		}
	}
	if server == "" && spec != nil && len(spec.VCenters) > 0 {
		server = spec.VCenters[0].Server
	}
	return "vsphere:" + server
}
