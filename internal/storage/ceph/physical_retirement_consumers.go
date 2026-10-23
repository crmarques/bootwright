package ceph

import (
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const pendingPhysicalReason = "retirement is delegated to the selected physical erasures and their terminal proof"

func pendingPhysicalClusters(e lifecycle.Execution, clusters, machines []string) ([]string, error) {
	pending := []string{}
	for _, cluster := range clusters {
		var policy *PhysicalRetirementRequest
		physical := false
		for _, proof := range provedFor(e) {
			if proof.Object != cluster {
				continue
			}
			if proof.Implementation == Implementation && proof.Block.ID == ClusterBlockID(cluster) {
				request, err := DecodeRequest(proof.Block.Request)
				if err != nil {
					return nil, err
				}
				physical = physical || physicalHosts(request.Hosts)
			}
			if proof.Implementation == PhysicalRetirementImplementation {
				request, err := DecodePhysicalRetirementRequest(proof.Block.Request)
				if err != nil {
					return nil, err
				}
				policy = &request
			}
		}
		if policy == nil {
			if physical {
				return nil, physicalRemovalRefusal()
			}
			continue
		}
		if err := authorizePhysicalRetirement(e, Request{Cluster: cluster, Hosts: policyHosts(*policy)}); err != nil {
			return nil, err
		}
		for _, host := range policy.Hosts {
			if !slices.Contains(machines, host.Machine) {
				return nil, physicalPolicyFailure()
			}
		}
		pending = append(pending, cluster)
	}
	slices.Sort(pending)
	return slices.Compact(pending), nil
}
