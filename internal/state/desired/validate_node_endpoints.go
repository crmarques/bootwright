package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

func validateNodeSourceEndpoints(state v1alpha1.State, ocp v1alpha1.ContainerCluster) []string {
	var errs []string
	singleNode := stateview.IsSingleNodeCluster(ocp)
	resolves := singleNodeMachineResolves(state, ocp)
	addresses := singleNodeInstallAddresses(state, ocp)
	for _, name := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		endpoint, ok := ocp.Spec.Install.Endpoints[name]
		if !ok || endpoint.Source.Type != v1alpha1.EndpointSourceNode {
			continue
		}
		prefix := fmt.Sprintf("ContainerCluster/%s spec.install.endpoints.%s", ocp.Metadata.Name, name)
		if !singleNode {
			errs = append(errs, fmt.Sprintf("%s.source.type=%s is valid only on a cluster with exactly one node; this cluster declares %d, and a multi-node cluster answers at a VIP no single node owns. Use source.type=%s or source.type=%s",
				prefix, v1alpha1.EndpointSourceNode, len(ocp.Spec.Nodes), v1alpha1.EndpointSourceExternal, v1alpha1.EndpointSourceInfraComponent))
			continue
		}
		machineName := ocp.Spec.Nodes[0].MachineRef.Name
		if endpoint.Address != "" && !ocp.DefaultedRefs.NodeEndpointAddress[name] {
			errs = append(errs, fmt.Sprintf("%s.address must be empty when source.type=%s; the node owns the address, and Bootwright resolves it from Machine/%s spec.addresses[] through spec.network.config.interfaceAddresses[]",
				prefix, v1alpha1.EndpointSourceNode, machineName))
			continue
		}
		if !resolves {
			continue
		}
		switch len(addresses) {
		case 1:
		case 0:
			errs = append(errs, fmt.Sprintf("%s.source.type=%s does not resolve to an install address: Machine/%s declares no spec.network.config.interfaceAddresses[] entry pointing at a spec.addresses[] entry. Author the node's install address there, or set address with source.type=%s",
				prefix, v1alpha1.EndpointSourceNode, machineName, v1alpha1.EndpointSourceExternal))
		default:
			errs = append(errs, fmt.Sprintf("%s.source.type=%s resolves to %d install addresses on Machine/%s (%s), so Bootwright cannot tell which one the endpoint answers at. Leave one spec.network.config.interfaceAddresses[] entry, or set address with source.type=%s",
				prefix, v1alpha1.EndpointSourceNode, len(addresses), machineName, strings.Join(addresses, ", "), v1alpha1.EndpointSourceExternal))
		}
	}
	return errs
}
