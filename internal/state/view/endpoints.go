package stateview

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
)

func ContainerEndpointRefName(ocp v1alpha1.ContainerCluster, role string) string {
	switch role {
	case v1alpha1.EndpointAPI:
		return role
	case v1alpha1.EndpointAPIInt:
		return role
	case v1alpha1.EndpointIngress:
		return role
	default:
		return ""
	}
}

func ContainerEndpoint(ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster, role string) (v1alpha1.Endpoint, bool) {
	name := ContainerEndpointRefName(ocp, role)
	if name == "" {
		return v1alpha1.Endpoint{}, false
	}
	endpoint, ok := ci.Endpoints[name]
	return endpoint, ok
}

func ContainerEndpointAddress(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster, role string) string {
	return EndpointAddress(state, ci, ContainerEndpointRefName(ocp, role))
}

func ClusterIngressBaseDomain(clusterName, baseDomain string) string {
	return "apps." + clusterName + "." + baseDomain
}

func ClusterConsoleHostname(clusterName, baseDomain string) string {
	return "console-openshift-console." + ClusterIngressBaseDomain(clusterName, baseDomain)
}

func ClusterAPIURL(clusterName, baseDomain string) string {
	return "https://api." + clusterName + "." + baseDomain + ":6443"
}

func ClusterConsoleURL(clusterName, baseDomain string) string {
	return "https://" + ClusterConsoleHostname(clusterName, baseDomain)
}
