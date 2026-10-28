package ceph

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

func (c Capability) Claimed(catalog api.Catalog) []string {
	refused := lifecycle.Identities(c.Unsupported(compilation.NewState(catalog, catalog, nil)))
	var claimed []string
	for _, object := range catalog.OfKind(c.kind) {
		if !slices.Contains(refused, object.Identity()) {
			claimed = append(claimed, object.Identity())
		}
	}
	return claimed
}

func (ImageCapability) Claimed(catalog api.Catalog) []string {
	return New(api.CephCluster, nil).Claimed(catalog)
}

func (HostCapability) Claimed(catalog api.Catalog) []string {
	return New(api.CephCluster, nil).Claimed(catalog)
}

func (PhysicalRetirementCapability) Claimed(catalog api.Catalog) []string {
	var claimed []string
	for _, identity := range New(api.CephCluster, nil).Claimed(catalog) {
		cluster, _ := catalog.Find(api.CephCluster, identity[len(api.CephCluster)+1:])
		if physicalCluster(catalog, cluster) {
			claimed = append(claimed, identity)
		}
	}
	return claimed
}

func physicalCluster(catalog api.Catalog, cluster api.Object) bool {
	return slices.ContainsFunc(cluster.Spec().Get("topology", "nodes").Items(), func(node api.Value) bool {
		machine, found := catalog.Find(api.Machine, node.Get("machineRef").Text())
		if !found {
			return false
		}
		provider, found := catalog.Find(api.InfraProvider, machine.Spec().Get("substrate", "providerRef").Text())
		return found && substrate.RealizesPhysicalNICs(provider)
	})
}

func (ConsumerCapability) Claimed(catalog api.Catalog) []string {
	return ofKind(catalog, api.CephConsumer)
}

func (SMBCapability) Claimed(catalog api.Catalog) []string {
	return ofKind(catalog, api.CephSMBResource)
}

func (ReplicationCapability) Claimed(catalog api.Catalog) []string {
	return ofKind(catalog, api.CephReplication)
}

func ofKind(catalog api.Catalog, kind api.Kind) []string {
	var claimed []string
	for _, object := range catalog.OfKind(kind) {
		claimed = append(claimed, object.Identity())
	}
	return claimed
}
