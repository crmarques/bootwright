package cephcluster

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func ProvidedTiebreakerUser(catalog api.Catalog, cluster api.Object, machineName string) (string, bool) {
	spec := cluster.Spec()
	tiebreaker := textOf(spec, "topology", "stretch", "tiebreaker", "node")
	if tiebreaker == "" || textOf(spec, "management") == "external" {
		return "", false
	}
	node, found := lookupNode(cluster, tiebreaker)
	if !found || textOf(node, "machineRef") != machineName {
		return "", false
	}
	host, found := catalog.Find(api.Machine, machineName)
	if !found || !host.Spec().Get("os", "provided").Bool() {
		return "", false
	}
	return textOf(spec, "cephadm", "clusterSSH", "user"), true
}
