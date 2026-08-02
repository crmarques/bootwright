package desiredstate

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

func materializeNodeEndpointAddresses(state *v1alpha1.State) {
	for i := range state.ContainerClusters {
		ocp := &state.ContainerClusters[i]
		addresses := singleNodeInstallAddresses(*state, *ocp)
		if len(addresses) != 1 {
			continue
		}
		for _, name := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
			endpoint, ok := ocp.Spec.Install.Endpoints[name]
			if !ok || endpoint.Source.Type != v1alpha1.EndpointSourceNode || endpoint.Address != "" {
				continue
			}
			endpoint.Address = addresses[0]
			ocp.Spec.Install.Endpoints[name] = endpoint
			if ocp.DefaultedRefs.NodeEndpointAddress == nil {
				ocp.DefaultedRefs.NodeEndpointAddress = map[string]bool{}
			}
			ocp.DefaultedRefs.NodeEndpointAddress[name] = true
		}
	}
}

func singleNodeInstallAddresses(state v1alpha1.State, ocp v1alpha1.ContainerCluster) []string {
	if !stateview.IsSingleNodeCluster(ocp) {
		return nil
	}
	machine, ok := stateview.Machine(state, ocp.Spec.Nodes[0].MachineRef.Name)
	if !ok {
		return nil
	}
	return stateview.InstallMachineAddresses(stateview.InstallMachineFromMachine(machine))
}

func singleNodeMachineResolves(state v1alpha1.State, ocp v1alpha1.ContainerCluster) bool {
	if !stateview.IsSingleNodeCluster(ocp) {
		return false
	}
	_, ok := stateview.Machine(state, ocp.Spec.Nodes[0].MachineRef.Name)
	return ok
}
