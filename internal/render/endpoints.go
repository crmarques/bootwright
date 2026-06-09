package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

var standardEndpointNames = []string{
	v1alpha1.EndpointAPI,
	v1alpha1.EndpointAPIInt,
	v1alpha1.EndpointIngress,
}

func containerEndpointRefName(ocp v1alpha1.ContainerCluster, role string) string {
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

func containerEndpoint(ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster, role string) (v1alpha1.Endpoint, bool) {
	name := containerEndpointRefName(ocp, role)
	if name == "" {
		return v1alpha1.Endpoint{}, false
	}
	endpoint, ok := ci.Endpoints[name]
	return endpoint, ok
}

func containerEndpointAddress(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster, role string) string {
	return endpointAddress(state, ci, containerEndpointRefName(ocp, role))
}

func endpointAddress(state v1alpha1.State, ci v1alpha1.ClusterInstall, name string) string {
	return stateview.EndpointAddress(state, ci, name)
}

func endpointNetworkConfig(state v1alpha1.State, ci v1alpha1.ClusterInstall, address string) (v1alpha1.NetworkConfig, bool) {
	return stateview.EndpointNetworkConfig(state, ci, address)
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
