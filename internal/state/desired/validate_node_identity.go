package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateAuthoredNodeNames(state v1alpha1.State) []string {
	var errs []string
	for _, cluster := range state.ContainerClusters {
		for i, node := range cluster.Spec.Nodes {
			prefix := fmt.Sprintf("ContainerCluster/%s spec.nodes[%d]", cluster.Metadata.Name, i)
			errs = append(errs, validateAuthoredNodeIdentity(prefix, node.Name, node.FQDN)...)
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for i, node := range cluster.Spec.Ceph.Topology.Nodes {
			prefix := fmt.Sprintf("StorageCluster/%s spec.ceph.topology.nodes[%d]", cluster.Metadata.Name, i)
			errs = append(errs, validateAuthoredNodeIdentity(prefix, node.Name, node.FQDN)...)
		}
	}
	return errs
}

func validateAuthoredNodeIdentity(prefix, name, fqdn string) []string {
	var errs []string
	if name == "" {
		errs = append(errs, prefix+".name is required")
	} else if !IsDNSLabel(name) {
		errs = append(errs, fmt.Sprintf("%s.name %q is not a valid DNS label; the node FQDN is composed as <name>.<cluster>.<domain>, so set the sibling fqdn field to pin a name outside that zone", prefix, name))
	}
	if fqdn != "" && !isDNSSubdomainName(fqdn) {
		errs = append(errs, fmt.Sprintf("%s.fqdn %q is not a valid DNS subdomain", prefix, fqdn))
	}
	return errs
}
