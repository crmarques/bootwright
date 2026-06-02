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
			entry := map[string]any{
				"name":        m.Name,
				"interfaces":  providerInterfacesVars(m),
				"provisioner": machineProvisionerVars(m),
			}
			if len(m.Labels) > 0 {
				entry["labels"] = m.Labels
			}
			machines = append(machines, entry)
		}
		out["machines"] = machines
	}
	if len(p.Spec.NetworkAttachments) > 0 {
		attachments := make([]any, 0, len(p.Spec.NetworkAttachments))
		for _, attachment := range p.Spec.NetworkAttachments {
			entry := networkAttachmentVars(attachment)
			entry["name"] = attachment.Name
			attachments = append(attachments, entry)
		}
		out["networkAttachments"] = attachments
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
			"kind":      v1alpha1.ProvisionerKubeVirt,
			"namespace": p.KubeVirt.Namespace,
		}
		if p.KubeVirt.HostContainerClusterRef != nil {
			out["hostContainerClusterRef"] = p.KubeVirt.HostContainerClusterRef.Name
		}
		if p.KubeVirt.KubeconfigRef != nil {
			out["kubeconfigRef"] = p.KubeVirt.KubeconfigRef.Name
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
	if len(n.Overrides) > 0 {
		out["overrides"] = n.Overrides
	}
	if n.Spec != nil {
		out["spec"] = n.Spec
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
