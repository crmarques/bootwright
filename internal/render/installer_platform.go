package render

import "github.com/crmarques/bootwright/api/v1alpha1"

func clusterPlatformKind(ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) string {
	if isSingleNodeCluster(ocp) && ci.Spec.Platform.Type != v1alpha1.PlatformTypeExternal {
		return v1alpha1.PlatformTypeNone
	}
	if ci.Spec.Platform.Type == "" {
		return v1alpha1.PlatformTypeNone
	}
	return ci.Spec.Platform.Type
}

func isSingleNodeCluster(ocp v1alpha1.ContainerCluster) bool {
	return len(ocp.Spec.Nodes) == 1 && ocp.Spec.Nodes[0].Role == v1alpha1.NodeRoleMaster
}

func platformConfig(state v1alpha1.State, kind string, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) map[string]any {
	apiVIPs, ingressVIPs := vipsFromEndpoints(state, ci, ocp)
	userManaged := !allContainerEndpointsOpenShift(ci, ocp)
	switch kind {
	case v1alpha1.PlatformTypeVSphere:
		out := map[string]any{
			"apiVIPs":     apiVIPs,
			"ingressVIPs": ingressVIPs,
		}
		for key, value := range vSphereProviderPlatformConfig(state, ci) {
			out[key] = value
		}
		if ci.Spec.Platform.VSphere != nil && ci.Spec.Platform.VSphere.NodeNetworking != nil {
			out["nodeNetworking"] = vSphereNodeNetworkingConfig(ci.Spec.Platform.VSphere.NodeNetworking)
		}
		return map[string]any{"vsphere": out}
	case v1alpha1.PlatformTypeBareMetal:
		out := map[string]any{
			"apiVIPs":     apiVIPs,
			"ingressVIPs": ingressVIPs,
		}
		if ci.Spec.Platform.BareMetal != nil && ci.Spec.Platform.BareMetal.ProvisioningNetwork != "" {
			out["provisioningNetwork"] = installerProvisioningNetwork(ci.Spec.Platform.BareMetal.ProvisioningNetwork)
		}
		if userManaged {
			out["loadBalancer"] = map[string]any{"type": "UserManaged"}
		}
		return map[string]any{"baremetal": out}
	case v1alpha1.PlatformTypeExternal:
		if ci.Spec.Platform.External != nil {
			return map[string]any{"external": cloneYAMLMap(ci.Spec.Platform.External)}
		}
		return map[string]any{"external": map[string]any{}}
	default:
		return map[string]any{"none": map[string]any{}}
	}
}

func installerProvisioningNetwork(value string) string {
	switch value {
	case v1alpha1.ProvisioningNetworkDisabled:
		return "Disabled"
	case v1alpha1.ProvisioningNetworkManaged:
		return "Managed"
	case v1alpha1.ProvisioningNetworkUnmanaged:
		return "Unmanaged"
	default:
		return value
	}
}

func vSphereProviderPlatformConfig(state v1alpha1.State, ci v1alpha1.ClusterInfra) map[string]any {
	vcenters := []any{}
	failureDomains := []any{}
	seenVCenters := map[string]bool{}
	seenFailureDomains := map[string]bool{}
	for _, machine := range ci.Spec.Components.Nodes {
		if machine.Source.ProfileRef.Name == "" {
			continue
		}
		provider, ok := findProvider(state, machine.Source.ProviderRef.Name)
		if !ok {
			continue
		}
		profile, ok := findProfile(provider, machine.Source.ProfileRef.Name)
		if !ok || profile.VSphere == nil {
			continue
		}
		for _, vc := range profile.VSphere.VCenters {
			key := vc.Server
			if seenVCenters[key] {
				continue
			}
			seenVCenters[key] = true
			vcenters = append(vcenters, vSphereVCenterConfig(vc))
		}
		for _, fd := range profile.VSphere.FailureDomains {
			key := fd.Name
			if seenFailureDomains[key] {
				continue
			}
			seenFailureDomains[key] = true
			failureDomains = append(failureDomains, vSphereFailureDomainConfig(fd))
		}
	}
	out := map[string]any{}
	if len(vcenters) > 0 {
		out["vcenters"] = vcenters
	}
	if len(failureDomains) > 0 {
		out["failureDomains"] = failureDomains
	}
	if ci.Spec.Platform.VSphere == nil || ci.Spec.Platform.VSphere.NodeNetworking == nil {
		if nn := firstVSphereProfileNodeNetworking(state, ci); nn != nil {
			out["nodeNetworking"] = vSphereNodeNetworkingConfig(nn)
		}
	}
	return out
}

func firstVSphereProfileNodeNetworking(state v1alpha1.State, ci v1alpha1.ClusterInfra) *v1alpha1.VSphereNodeNetworking {
	for _, machine := range ci.Spec.Components.Nodes {
		if machine.Source.ProfileRef.Name == "" {
			continue
		}
		provider, ok := findProvider(state, machine.Source.ProviderRef.Name)
		if !ok {
			continue
		}
		profile, ok := findProfile(provider, machine.Source.ProfileRef.Name)
		if ok && profile.VSphere != nil && profile.VSphere.NodeNetworking != nil {
			return profile.VSphere.NodeNetworking
		}
	}
	return nil
}

func vSphereVCenterConfig(vc v1alpha1.VSphereVCenter) map[string]any {
	out := map[string]any{"server": vc.Server}
	if vc.Port > 0 {
		out["port"] = vc.Port
	}
	if len(vc.Datacenters) > 0 {
		items := make([]any, 0, len(vc.Datacenters))
		for _, dc := range vc.Datacenters {
			items = append(items, dc)
		}
		out["datacenters"] = items
	}
	if vc.CredentialsRef.Name != "" {
		out["user"] = secretRefPlaceholder("vsphere-user", vc.CredentialsRef.Name)
		out["password"] = secretRefPlaceholder("vsphere-password", vc.CredentialsRef.Name)
	}
	return out
}

func vSphereFailureDomainConfig(fd v1alpha1.VSphereFailureDomain) map[string]any {
	out := map[string]any{
		"name":     fd.Name,
		"server":   fd.Server,
		"topology": vSphereFailureTopologyConfig(fd.Topology),
	}
	if fd.Region != "" {
		out["region"] = fd.Region
	}
	if fd.Zone != "" {
		out["zone"] = fd.Zone
	}
	return out
}

func vSphereFailureTopologyConfig(t v1alpha1.VSphereFailureTopology) map[string]any {
	out := map[string]any{
		"datacenter":     t.Datacenter,
		"computeCluster": t.ComputeCluster,
	}
	if t.Datastore != "" {
		out["datastore"] = t.Datastore
	}
	if t.Folder != "" {
		out["folder"] = t.Folder
	}
	if t.ResourcePool != "" {
		out["resourcePool"] = t.ResourcePool
	}
	if len(t.Networks) > 0 {
		items := make([]any, 0, len(t.Networks))
		for _, network := range t.Networks {
			items = append(items, network)
		}
		out["networks"] = items
	}
	return out
}

func vSphereNodeNetworkingConfig(n *v1alpha1.VSphereNodeNetworking) map[string]any {
	out := map[string]any{}
	if n.External != nil {
		out["external"] = vSphereNetworkSubnetConfig(n.External)
	}
	if n.Internal != nil {
		out["internal"] = vSphereNetworkSubnetConfig(n.Internal)
	}
	return out
}

func vSphereNetworkSubnetConfig(n *v1alpha1.VSphereNetworkSubnet) map[string]any {
	out := map[string]any{}
	if len(n.NetworkSubnetCIDR) > 0 {
		values := make([]any, 0, len(n.NetworkSubnetCIDR))
		for _, cidr := range n.NetworkSubnetCIDR {
			values = append(values, cidr)
		}
		out["networkSubnetCidr"] = values
	}
	return out
}

func vipsFromEndpoints(state v1alpha1.State, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) (api []any, ingress []any) {
	if vip := containerEndpointAddress(state, ci, ocp, v1alpha1.EndpointAPI); vip != "" {
		api = append(api, vip)
	}
	if vip := containerEndpointAddress(state, ci, ocp, v1alpha1.EndpointIngress); vip != "" {
		ingress = append(ingress, vip)
	}
	if api == nil {
		api = []any{}
	}
	if ingress == nil {
		ingress = []any{}
	}
	return api, ingress
}

func allContainerEndpointsOpenShift(ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) bool {
	for _, name := range standardEndpointNames {
		endpoint, ok := containerEndpoint(ci, ocp, name)
		if !ok || endpointSourceType(endpoint, v1alpha1.EndpointSourceOpenShift) != v1alpha1.EndpointSourceOpenShift {
			return false
		}
	}
	return true
}
