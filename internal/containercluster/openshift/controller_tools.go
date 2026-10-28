package openshift

import api "github.com/crmarques/bootwright/api/v1alpha1"

type ControllerTool struct{ Kind, Version, Compatibility, Mirror string }

func ControllerTools(cluster api.Object) ([]ControllerTool, *api.Issue) {
	distribution := cluster.Spec().Get("distribution")
	kind, version := distribution.Get("type").Text(), distribution.Get("release", "version").Text()
	if (kind != "openshift" && kind != "okd") || !api.ValidLexical("cluster-release", version) {
		return nil, &api.Issue{Code: "controller.unsupported", Field: "$.spec.distribution.release", Message: "target tools require an exact supported release version; declare release.version for a cluster pinned to a release image"}
	}
	installer := "openshift-install"
	if cluster.Spec().Get("security", "fips", "enabled").Bool() {
		if kind != "openshift" {
			return nil, &api.Issue{Code: "controller.unsupported", Field: "$.spec.security.fips", Message: "FIPS installer acquisition requires the OpenShift publisher"}
		}
		installer = "openshift-install-fips"
	}
	return []ControllerTool{
		{Kind: "openshift-clients", Version: version, Compatibility: kind, Mirror: "openshiftClientsMirror"},
		{Kind: installer, Version: version, Compatibility: kind, Mirror: "openshiftClientsMirror"},
		{Kind: "helm", Mirror: "helmMirror"},
	}, nil
}

type NodeName struct{ Name, Machine string }

func NodeNames(cluster api.Object) []NodeName {
	var names []NodeName
	for _, node := range cluster.Spec().Get("nodes").Items() {
		names = append(names, NodeName{Name: node.Get("fqdn").Text(), Machine: node.Get("machineRef").Text()})
	}
	return names
}
