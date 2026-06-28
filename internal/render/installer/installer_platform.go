package installer

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

var StandardEndpointNames = []string{
	v1alpha1.EndpointAPI,
	v1alpha1.EndpointAPIInt,
	v1alpha1.EndpointIngress,
}

func clusterPlatformKind(ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster) string {
	if stateview.IsSingleNodeCluster(ocp) && ci.Platform.Type != v1alpha1.PlatformTypeExternal {
		return v1alpha1.PlatformTypeNone
	}
	if ci.Platform.Type == "" {
		return v1alpha1.PlatformTypeNone
	}
	return ci.Platform.Type
}

func platformConfig(state v1alpha1.State, kind string, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster, secrets InstallerSecrets) map[string]any {
	apiVIPs, ingressVIPs := vipsFromEndpoints(state, ci, ocp)
	userManaged := !allContainerEndpointsOpenShift(ci, ocp)
	switch kind {
	case v1alpha1.PlatformTypeVSphere:
		out := map[string]any{
			"apiVIPs":     apiVIPs,
			"ingressVIPs": ingressVIPs,
		}
		for key, value := range vSphereProviderPlatformConfig(state, ci, secrets) {
			out[key] = value
		}
		if ci.Platform.VSphere != nil && ci.Platform.VSphere.NodeNetworking != nil {
			out["nodeNetworking"] = VSphereNodeNetworkingConfig(ci.Platform.VSphere.NodeNetworking)
		}
		return map[string]any{"vsphere": out}
	case v1alpha1.PlatformTypeBareMetal:
		out := map[string]any{
			"apiVIPs":     apiVIPs,
			"ingressVIPs": ingressVIPs,
		}
		if ci.Platform.BareMetal != nil && ci.Platform.BareMetal.ProvisioningNetwork != "" {
			out["provisioningNetwork"] = installerProvisioningNetwork(ci.Platform.BareMetal.ProvisioningNetwork)
		}
		if userManaged {
			out["loadBalancer"] = map[string]any{"type": "UserManaged"}
		}
		return map[string]any{"baremetal": out}
	case v1alpha1.PlatformTypeExternal:
		if ci.Platform.External != nil {
			return map[string]any{"external": cloneYAMLMap(ci.Platform.External)}
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

func vSphereProviderPlatformConfig(state v1alpha1.State, ci v1alpha1.ClusterInstall, secrets InstallerSecrets) map[string]any {
	vcenters := []any{}
	failureDomains := []any{}
	seenVCenters := map[string]bool{}
	seenFailureDomains := map[string]bool{}
	for _, machine := range ci.Machines {
		if machine.Source.ProfileRef.Name == "" {
			continue
		}
		provider, ok := stateview.Provider(state, machine.Source.ProviderRef.Name)
		if !ok || provider.Spec.Type != v1alpha1.ProvisionerVSphere || provider.Spec.VSphere == nil {
			continue
		}
		if _, ok := stateview.MachineProfile(provider, machine.Source.ProfileRef.Name); !ok {
			continue
		}
		for _, vc := range provider.Spec.VSphere.VCenters {
			key := vc.Server
			if seenVCenters[key] {
				continue
			}
			seenVCenters[key] = true
			vcenters = append(vcenters, vSphereVCenterConfig(vc, secrets))
		}
		for _, fd := range provider.Spec.VSphere.FailureDomains {
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
	if ci.Platform.VSphere == nil || ci.Platform.VSphere.NodeNetworking == nil {
		if nn := firstVSphereProfileNodeNetworking(state, ci); nn != nil {
			out["nodeNetworking"] = VSphereNodeNetworkingConfig(nn)
		}
	}
	return out
}

func firstVSphereProfileNodeNetworking(state v1alpha1.State, ci v1alpha1.ClusterInstall) *v1alpha1.VSphereNodeNetworking {
	for _, machine := range ci.Machines {
		if machine.Source.ProfileRef.Name == "" {
			continue
		}
		provider, ok := stateview.Provider(state, machine.Source.ProviderRef.Name)
		if !ok || provider.Spec.Type != v1alpha1.ProvisionerVSphere || provider.Spec.VSphere == nil {
			continue
		}
		if _, ok := stateview.MachineProfile(provider, machine.Source.ProfileRef.Name); !ok {
			continue
		}
		if provider.Spec.VSphere.NodeNetworking != nil {
			return provider.Spec.VSphere.NodeNetworking
		}
	}
	return nil
}

func vSphereVCenterConfig(vc v1alpha1.VSphereVCenter, secrets InstallerSecrets) map[string]any {
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
		if creds, ok := secrets.VSphereCredentials[vc.CredentialsRef.Name]; ok {
			out["user"] = creds.Username
			out["password"] = creds.Password
		} else {
			out["user"] = secretRefPlaceholder("vsphere-user", vc.CredentialsRef.Name)
			out["password"] = secretRefPlaceholder("vsphere-password", vc.CredentialsRef.Name)
		}
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

func VSphereNodeNetworkingConfig(n *v1alpha1.VSphereNodeNetworking) map[string]any {
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

func vipsFromEndpoints(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster) (api []any, ingress []any) {
	if vip := stateview.ContainerEndpointAddress(state, ci, ocp, v1alpha1.EndpointAPI); vip != "" {
		api = append(api, vip)
	}
	if vip := stateview.ContainerEndpointAddress(state, ci, ocp, v1alpha1.EndpointIngress); vip != "" {
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

func allContainerEndpointsOpenShift(ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster) bool {
	for _, name := range StandardEndpointNames {
		endpoint, ok := stateview.ContainerEndpoint(ci, ocp, name)
		if !ok || endpoint.Source.Type != v1alpha1.EndpointSourceOpenShift {
			return false
		}
	}
	return true
}
