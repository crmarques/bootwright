package render

import "github.com/crmarques/bootwright/api/v1alpha1"

func providersVars(state v1alpha1.State) []any {
	out := make([]any, 0, len(state.InfraProviders))
	for _, p := range state.InfraProviders {
		out = append(out, map[string]any{
			"name":         p.Metadata.Name,
			"capabilities": providerCapabilityVars(p),
		})
	}
	return out
}

func providerCapabilityVars(p v1alpha1.InfraProvider) map[string]any {
	out := map[string]any{}
	if len(p.Spec.MachineProfiles) > 0 {
		profiles := make([]any, 0, len(p.Spec.MachineProfiles))
		for _, mp := range p.Spec.MachineProfiles {
			profiles = append(profiles, map[string]any{
				"name":        mp.Name,
				"cpu":         mp.CPU,
				"memoryMiB":   mp.MemoryMiB,
				"diskGiB":     mp.DiskGiB,
				"provisioner": machineProfileProvisionerVars(mp),
			})
		}
		out["machineProfiles"] = profiles
	}
	if len(p.Spec.Machines) > 0 {
		machines := make([]any, 0, len(p.Spec.Machines))
		for _, m := range p.Spec.Machines {
			machines = append(machines, map[string]any{
				"name":        m.Name,
				"interfaces":  providerInterfacesVars(m),
				"provisioner": machineProvisionerVars(m),
			})
		}
		out["machines"] = machines
	}
	if len(p.Spec.LoadBalancers) > 0 {
		lbs := make([]any, 0, len(p.Spec.LoadBalancers))
		for _, lb := range p.Spec.LoadBalancers {
			entry := map[string]any{"name": lb.Name}
			if lb.HAProxy != nil {
				entry["haProxy"] = map[string]any{"hostRef": lb.HAProxy.HostRef.Name}
			}
			lbs = append(lbs, entry)
		}
		out["loadBalancers"] = lbs
	}
	if len(p.Spec.ArtifactPublishers) > 0 {
		artifacts := make([]any, 0, len(p.Spec.ArtifactPublishers))
		for _, publisher := range p.Spec.ArtifactPublishers {
			entry := map[string]any{"name": publisher.Name}
			if publisher.HTTP != nil {
				http := map[string]any{
					"hostRef": publisher.HTTP.HostRef.Name,
					"port":    artifactHTTPPort(publisher.HTTP),
				}
				if routes := artifactRoutesVars(publisher.HTTP.Routes); len(routes) > 0 {
					http["routes"] = routes
				}
				entry["http"] = http
			}
			artifacts = append(artifacts, entry)
		}
		out["artifactPublishers"] = artifacts
	}
	if len(p.Spec.Proxies) > 0 {
		proxies := make([]any, 0, len(p.Spec.Proxies))
		for _, pr := range p.Spec.Proxies {
			entry := map[string]any{"name": pr.Name}
			if pr.Squid != nil {
				entry["squid"] = map[string]any{"hostRef": pr.Squid.HostRef.Name}
			}
			proxies = append(proxies, entry)
		}
		out["proxies"] = proxies
	}
	if len(p.Spec.DNS) > 0 {
		dnss := make([]any, 0, len(p.Spec.DNS))
		for _, d := range p.Spec.DNS {
			entry := map[string]any{"name": d.Name}
			if d.Dnsmasq != nil {
				entry["dnsmasq"] = map[string]any{"hostRef": d.Dnsmasq.HostRef.Name}
			}
			dnss = append(dnss, entry)
		}
		out["dns"] = dnss
	}
	if len(p.Spec.Registries) > 0 {
		regs := make([]any, 0, len(p.Spec.Registries))
		for _, r := range p.Spec.Registries {
			entry := map[string]any{"name": r.Name}
			if r.MirrorRegistry != nil {
				entry["mirrorRegistry"] = map[string]any{"hostRef": r.MirrorRegistry.HostRef.Name}
			}
			regs = append(regs, entry)
		}
		out["registries"] = regs
	}
	return out
}

func artifactRoutesVars(routes v1alpha1.ArtifactHTTPRoutes) map[string]any {
	out := map[string]any{}
	if routes.RedfishVirtualMedia.AddressName != "" {
		out["redfishVirtualMedia"] = map[string]any{"addressName": routes.RedfishVirtualMedia.AddressName}
	}
	if routes.ClusterInstall.AddressName != "" {
		out["clusterInstall"] = map[string]any{"addressName": routes.ClusterInstall.AddressName}
	}
	return out
}

func machineProfileProvisionerVars(p v1alpha1.MachineProfileCapability) map[string]any {
	switch {
	case p.Libvirt != nil:
		out := map[string]any{
			"kind":    v1alpha1.ProvisionerLibvirt,
			"hostRef": p.Libvirt.HostRef.Name,
			"uri":     p.Libvirt.URI,
		}
		if d := p.Libvirt.BMCEmulationDefaults; d != nil {
			out["bmcEmulationDefaults"] = bmcEmulationDefaultsVars(d)
		}
		return out
	case p.VSphere != nil:
		out := map[string]any{"kind": v1alpha1.ProvisionerVSphere}
		if len(p.VSphere.VCenters) > 0 {
			out["vcenters"] = vSphereVCentersVars(p.VSphere.VCenters)
		}
		if len(p.VSphere.FailureDomains) > 0 {
			out["failureDomains"] = vSphereFailureDomainsVars(p.VSphere.FailureDomains)
		}
		if p.VSphere.NodeNetworking != nil {
			out["nodeNetworking"] = vSphereNodeNetworkingConfig(p.VSphere.NodeNetworking)
		}
		if p.VSphere.Template != "" {
			out["template"] = p.VSphere.Template
		}
		return out
	case p.KubeVirt != nil:
		out := map[string]any{
			"kind":       v1alpha1.ProvisionerKubeVirt,
			"clusterRef": p.KubeVirt.ClusterRef.Name,
			"namespace":  p.KubeVirt.Namespace,
		}
		if p.KubeVirt.StorageClassRef != nil {
			out["storageClassRef"] = p.KubeVirt.StorageClassRef.Name
		}
		return out
	default:
		return map[string]any{}
	}
}

func vSphereVCentersVars(items []v1alpha1.VSphereVCenter) []any {
	out := make([]any, 0, len(items))
	for _, vc := range items {
		entry := map[string]any{
			"server":         vc.Server,
			"datacenters":    stringSliceAny(vc.Datacenters),
			"credentialsRef": vc.CredentialsRef.Name,
		}
		if vc.Port != 0 {
			entry["port"] = vc.Port
		}
		out = append(out, entry)
	}
	return out
}

func vSphereFailureDomainsVars(items []v1alpha1.VSphereFailureDomain) []any {
	out := make([]any, 0, len(items))
	for _, fd := range items {
		out = append(out, map[string]any{
			"name":     fd.Name,
			"region":   fd.Region,
			"zone":     fd.Zone,
			"server":   fd.Server,
			"topology": vSphereFailureTopologyVars(fd.Topology),
		})
	}
	return out
}

func vSphereFailureTopologyVars(t v1alpha1.VSphereFailureTopology) map[string]any {
	return map[string]any{
		"datacenter":     t.Datacenter,
		"computeCluster": t.ComputeCluster,
		"datastore":      t.Datastore,
		"folder":         t.Folder,
		"resourcePool":   t.ResourcePool,
		"networks":       stringSliceAny(t.Networks),
	}
}

func bmcEmulationDefaultsVars(d *v1alpha1.BMCEmulationDefaults) map[string]any {
	out := map[string]any{}
	if d.Enabled != nil {
		out["enabled"] = *d.Enabled
	}
	if d.Protocol != "" {
		out["protocol"] = d.Protocol
	}
	if d.Emulator != "" {
		out["emulator"] = d.Emulator
	}
	if d.BindAddress != "" {
		out["bindAddress"] = d.BindAddress
	}
	if d.Port != 0 {
		out["port"] = d.Port
	}
	if d.VMediaPort != 0 {
		out["vmediaPort"] = d.VMediaPort
	}
	if d.Auth != nil {
		out["credentialRef"] = d.Auth.CredentialRef.Name
	}
	if d.DisableCertificateVerification != nil {
		out["disableCertificateVerification"] = *d.DisableCertificateVerification
	}
	return out
}

func machineProvisionerVars(m v1alpha1.MachineCapability) map[string]any {
	if m.BareMetal == nil {
		return map[string]any{}
	}
	bmc := m.BareMetal.BMC
	out := map[string]any{
		"kind":           v1alpha1.ProvisionerBareMetal,
		"bootMACAddress": m.BareMetal.BootMACAddress,
		"bmc": map[string]any{
			"address":                        bmc.Address,
			"protocol":                       bmc.Protocol,
			"credentialsRef":                 bmc.CredentialsRef.Name,
			"disableCertificateVerification": bmc.DisableCertificateVerification,
		},
	}
	if m.BareMetal.RootDeviceHints != nil {
		out["rootDeviceHints"] = rootDeviceHintsConfig(m.BareMetal.RootDeviceHints)
	}
	return out
}

func providerInterfacesVars(m v1alpha1.MachineCapability) []any {
	if m.BareMetal == nil {
		return nil
	}
	return machineInterfaceVars(m.BareMetal.Interfaces)
}

func machineInterfaceVars(interfaces []v1alpha1.MachineInterface) []any {
	out := make([]any, 0, len(interfaces))
	for _, i := range interfaces {
		out = append(out, map[string]any{
			"name":       i.Name,
			"macAddress": i.MACAddress,
		})
	}
	return out
}

func clusterMachineNetworkConfigVars(n v1alpha1.ClusterMachineNetworkConfig) map[string]any {
	out := map[string]any{}
	if n.Ref.Name != "" {
		out["ref"] = n.Ref.Name
	}
	if len(n.Addresses) > 0 {
		out["addresses"] = networkConfigAddressesVars(n.Addresses)
	}
	return out
}

func networkConfigAddressesVars(items []v1alpha1.NetworkConfigAddress) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{"interface": item.Interface}
		if len(item.IPv4) > 0 {
			entry["ipv4"] = networkIPVars(item.IPv4)
		}
		if len(item.IPv6) > 0 {
			entry["ipv6"] = networkIPVars(item.IPv6)
		}
		out = append(out, entry)
	}
	return out
}

func networkIPVars(items []v1alpha1.NetworkIPAddress) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"ip":            item.IP,
			"prefixLength":  item.PrefixLength,
			"prefix-length": item.PrefixLength,
		})
	}
	return out
}

func stringSliceAny(in []string) []any {
	out := make([]any, 0, len(in))
	for _, item := range in {
		out = append(out, item)
	}
	return out
}
