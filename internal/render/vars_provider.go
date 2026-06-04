package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

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
	out := map[string]any{"type": p.Spec.Type}
	if profiles := machineProfilesVars(p); len(profiles) > 0 {
		out["machineProfiles"] = profiles
	}
	if p.Spec.Libvirt != nil {
		out["libvirt"] = libvirtProviderVars(p.Spec.Libvirt)
	}
	if p.Spec.BareMetal != nil {
		out["bareMetal"] = bareMetalProviderVars(p.Spec.BareMetal)
	}
	if p.Spec.VSphere != nil {
		out["vsphere"] = vSphereProviderVars(p.Spec.VSphere)
	}
	if p.Spec.KubeVirt != nil {
		out["kubevirt"] = kubeVirtProviderVars(p.Spec.KubeVirt)
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

func machineProfilesVars(provider v1alpha1.InfraProvider) []any {
	profiles := stateview.ProviderMachineProfiles(provider)
	out := make([]any, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, map[string]any{
			"name":             profile.Name,
			"cpu":              profile.CPU,
			"memoryMiB":        profile.MemoryMiB,
			"diskGiB":          profile.DiskGiB,
			"template":         profile.Template,
			"failureDomainRef": profile.FailureDomainRef.Name,
			"dataDisks":        machineProfileDisksVars(profile.DataDisks),
		})
	}
	return out
}

func machineProfileDisksVars(disks []v1alpha1.MachineProfileDisk) []any {
	out := make([]any, 0, len(disks))
	for _, disk := range disks {
		out = append(out, map[string]any{"name": disk.Name, "sizeGiB": disk.SizeGiB})
	}
	return out
}

func machineProfileProvisionerVars(provider v1alpha1.InfraProvider, profile v1alpha1.MachineProfile) map[string]any {
	out := map[string]any{"kind": provider.Spec.Type}
	if profile.Template != "" {
		out["template"] = profile.Template
	}
	if profile.FailureDomainRef.Name != "" {
		out["failureDomainRef"] = profile.FailureDomainRef.Name
	}
	return out
}

func libvirtProviderVars(l *v1alpha1.InfraProviderLibvirt) map[string]any {
	out := map[string]any{
		"machineRef": l.MachineRef.Name,
		"uri":        l.URI,
	}
	if d := l.BMCEmulationDefaults; d != nil {
		out["bmcEmulationDefaults"] = bmcEmulationDefaultsVars(d)
	}
	return out
}

func bareMetalProviderVars(b *v1alpha1.InfraProviderBareMetal) map[string]any {
	out := map[string]any{}
	if b.Boot.Method != "" {
		out["boot"] = map[string]any{"method": b.Boot.Method}
	}
	if b.Defaults.BMC != nil {
		out["defaults"] = map[string]any{"bmc": map[string]any{
			"credentialsRef":                 b.Defaults.BMC.CredentialsRef.Name,
			"disableCertificateVerification": b.Defaults.BMC.DisableCertificateVerification,
		}}
	}
	return out
}

func vSphereProviderVars(v *v1alpha1.InfraProviderVSphere) map[string]any {
	out := map[string]any{}
	if len(v.VCenters) > 0 {
		out["vcenters"] = vSphereVCentersVars(v.VCenters)
	}
	if len(v.FailureDomains) > 0 {
		out["failureDomains"] = vSphereFailureDomainsVars(v.FailureDomains)
	}
	if v.NodeNetworking != nil {
		out["nodeNetworking"] = vSphereNodeNetworkingConfig(v.NodeNetworking)
	}
	return out
}

func kubeVirtProviderVars(k *v1alpha1.InfraProviderKubeVirt) map[string]any {
	out := map[string]any{"namespace": k.Namespace}
	if k.HostClusterRef != nil {
		out["hostClusterRef"] = k.HostClusterRef.Name
	}
	if k.KubeconfigRef != nil {
		out["kubeconfigRef"] = k.KubeconfigRef.Name
	}
	if k.StorageClassRef != nil {
		out["storageClassRef"] = k.StorageClassRef.Name
	}
	return out
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

func providerInterfacesVars(m v1alpha1.Machine) []any {
	return machineInterfaceVars(m.Spec.Hardware.NICs)
}

func machineInterfaceVars(interfaces []v1alpha1.MachineNIC) []any {
	out := make([]any, 0, len(interfaces))
	for _, i := range interfaces {
		entry := map[string]any{"name": i.Name}
		if i.MACAddress != "" {
			entry["macAddress"] = i.MACAddress
		}
		out = append(out, entry)
	}
	return out
}

func clusterMachineNetworkConfigVars(n v1alpha1.MachineNetworkConfig) map[string]any {
	out := map[string]any{}
	if n.NetworkConfigRef.Name != "" {
		out["ref"] = n.NetworkConfigRef.Name
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
