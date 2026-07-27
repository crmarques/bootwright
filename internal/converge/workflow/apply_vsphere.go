package workflow

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func vsphereMachinePlacement(provider v1alpha1.InfraProvider, machine v1alpha1.Machine) (server, datacenter string) {
	spec := provider.Spec.VSphere
	if profile, ok := stateview.MachineProfile(provider, machine.Spec.Substrate.ProfileRef.Name); ok {
		if fd, ok := stateview.VSphereProfileFailureDomain(spec, profile); ok {
			return fd.Server, fd.Topology.Datacenter
		}
	}
	if spec != nil && len(spec.VCenters) > 0 {
		vcenter := spec.VCenters[0]
		if len(vcenter.Datacenters) > 0 {
			return vcenter.Server, vcenter.Datacenters[0]
		}
		return vcenter.Server, ""
	}
	return "", ""
}

func vsphereResourceKey(provider v1alpha1.InfraProvider, machine v1alpha1.Machine, clusterName string) string {
	server, datacenter := vsphereMachinePlacement(provider, machine)
	return "vsphere:" + server + ":" + datacenter + ":vm:" + clusterName + "-" + machine.Metadata.Name
}

func vsphereHostSlotKey(provider v1alpha1.InfraProvider, machine v1alpha1.Machine) string {
	server, _ := vsphereMachinePlacement(provider, machine)
	if server == "" {
		return ""
	}
	return "vcenter:" + server
}
