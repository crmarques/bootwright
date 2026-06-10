package desiredstate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

func validateProviders(state v1alpha1.State) []string {
	var errs []string
	machines := indexMachines(state.Machines)
	clusters := indexContainerClusters(state.ContainerClusters)
	seen := map[string]bool{}
	for _, provider := range state.InfraProviders {
		if e := validateName(v1alpha1.KindInfraProvider, provider.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[provider.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate InfraProvider %q", provider.Metadata.Name))
		}
		seen[provider.Metadata.Name] = true
		errs = append(errs, validateProviderSpec(provider, machines, clusters)...)
	}
	errs = append(errs, validateLibvirtBMCEmulationHostPorts(state)...)
	return errs
}

func validateProviderSpec(provider v1alpha1.InfraProvider, machines map[string]v1alpha1.Machine, clusters map[string]v1alpha1.ContainerCluster) []string {
	var errs []string
	prefix := fmt.Sprintf("InfraProvider/%s spec", provider.Metadata.Name)
	switch provider.Spec.Type {
	case v1alpha1.ProvisionerLibvirt:
		if provider.Spec.Libvirt == nil {
			errs = append(errs, prefix+".libvirt is required when type=libvirt")
		} else {
			errs = append(errs, validateProviderLibvirt(prefix+".libvirt", provider.Spec.Libvirt, machines)...)
		}
		errs = append(errs, rejectProviderArms(prefix, provider, "libvirt")...)
	case v1alpha1.ProvisionerBareMetal:
		if provider.Spec.BareMetal == nil {
			errs = append(errs, prefix+".bareMetal is required when type=baremetal")
		}
		errs = append(errs, rejectProviderArms(prefix, provider, "bareMetal")...)
	case v1alpha1.ProvisionerVSphere:
		if provider.Spec.VSphere == nil {
			errs = append(errs, prefix+".vsphere is required when type=vsphere")
		} else {
			errs = append(errs, validateProviderVSphere(prefix+".vsphere", provider.Spec.VSphere)...)
		}
		errs = append(errs, rejectProviderArms(prefix, provider, "vsphere")...)
	case v1alpha1.ProvisionerKubeVirt:
		if provider.Spec.KubeVirt == nil {
			errs = append(errs, prefix+".kubevirt is required when type=kubevirt")
		} else {
			errs = append(errs, validateProviderKubeVirt(prefix+".kubevirt", provider.Spec.KubeVirt, clusters)...)
		}
		errs = append(errs, rejectProviderArms(prefix, provider, "kubevirt")...)
	case "":
		errs = append(errs, prefix+".type is required")
	default:
		errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s, %s, %s}",
			prefix, provider.Spec.Type, v1alpha1.ProvisionerLibvirt, v1alpha1.ProvisionerBareMetal, v1alpha1.ProvisionerVSphere, v1alpha1.ProvisionerKubeVirt))
	}
	if providerArtifactAccessSet(provider.Spec.ArtifactAccess) {
		errs = append(errs, prefix+".artifactAccess is not valid on InfraProvider; use Environment.spec.defaults.artifactAccess or ContainerCluster.spec.install.artifactAccess")
	}
	errs = append(errs, validateUniqueCapabilityNames(provider, "networkAttachments", capabilityNames(provider.Spec.NetworkAttachments, func(x v1alpha1.NetworkAttachmentCapability) string { return x.Name }))...)
	for _, attachment := range provider.Spec.NetworkAttachments {
		errs = append(errs, validateProviderNetworkAttachment(provider, attachment)...)
	}
	return errs
}

func providerArtifactAccessSet(access v1alpha1.ProviderArtifactAccess) bool {
	return access.ServerRef.Name != "" ||
		access.RedfishVirtualMedia.EndpointRef.Name != "" ||
		access.MachineBoot.EndpointRef.Name != "" ||
		access.OSInstall.EndpointRef.Name != ""
}

func rejectProviderArms(prefix string, provider v1alpha1.InfraProvider, keep string) []string {
	arms := map[string]bool{
		"libvirt":   provider.Spec.Libvirt != nil,
		"bareMetal": provider.Spec.BareMetal != nil,
		"vsphere":   provider.Spec.VSphere != nil,
		"kubevirt":  provider.Spec.KubeVirt != nil,
	}
	var errs []string
	for name, set := range arms {
		if set && name != keep {
			errs = append(errs, fmt.Sprintf("%s.%s must be empty when type=%s", prefix, name, provider.Spec.Type))
		}
	}
	return errs
}

func validateProviderLibvirt(prefix string, spec *v1alpha1.InfraProviderLibvirt, machines map[string]v1alpha1.Machine) []string {
	var errs []string
	if spec.MachineRef.Name == "" {
		errs = append(errs, prefix+".machineRef is required")
	} else if machine, ok := machines[spec.MachineRef.Name]; !ok {
		errs = append(errs, fmt.Sprintf("%s.machineRef %q does not match any Machine", prefix, spec.MachineRef.Name))
	} else if !machineHasCapability(machine, v1alpha1.MachineCapabilityLibvirt) {
		errs = append(errs, fmt.Sprintf("%s.machineRef %q lacks capability %q", prefix, spec.MachineRef.Name, v1alpha1.MachineCapabilityLibvirt))
	}
	if spec.URI == "" {
		errs = append(errs, prefix+".uri is required")
	}
	if spec.BMCEmulationDefaults != nil {
		errs = append(errs, validateBMCEmulationDefaults(prefix+".bmcEmulationDefaults", spec.BMCEmulationDefaults)...)
	} else {
		errs = append(errs, prefix+".bmcEmulationDefaults is required for current libvirt apply support")
	}
	errs = append(errs, validateMachineProfiles(prefix+".machineProfiles", v1alpha1.ProvisionerLibvirt, spec.MachineProfiles, nil)...)
	return errs
}

func validateProviderVSphere(prefix string, spec *v1alpha1.InfraProviderVSphere) []string {
	var errs []string
	if len(spec.VCenters) == 0 {
		errs = append(errs, prefix+".vcenters is required")
	}
	vcenterServers := map[string]bool{}
	for i, vc := range spec.VCenters {
		owner := fmt.Sprintf("%s.vcenters[%d]", prefix, i)
		if vc.Server == "" {
			errs = append(errs, owner+".server is required")
		} else {
			vcenterServers[vc.Server] = true
		}
		if vc.Port < 0 || vc.Port > 65535 {
			errs = append(errs, fmt.Sprintf("%s.port %d out of range", owner, vc.Port))
		}
		if len(vc.Datacenters) == 0 {
			errs = append(errs, owner+".datacenters is required")
		}
		if vc.CredentialsRef.Name == "" {
			errs = append(errs, owner+".credentialsRef is required")
		}
	}
	if len(spec.FailureDomains) == 0 {
		errs = append(errs, prefix+".failureDomains is required")
	}
	failureDomains := map[string]bool{}
	for i, fd := range spec.FailureDomains {
		owner := fmt.Sprintf("%s.failureDomains[%d]", prefix, i)
		if fd.Name == "" {
			errs = append(errs, owner+".name is required")
		} else {
			failureDomains[fd.Name] = true
		}
		if fd.Region == "" {
			errs = append(errs, owner+".region is required")
		}
		if fd.Zone == "" {
			errs = append(errs, owner+".zone is required")
		}
		if fd.Server == "" {
			errs = append(errs, owner+".server is required")
		} else if !vcenterServers[fd.Server] {
			errs = append(errs, fmt.Sprintf("%s.server %q does not match any vcenters[].server", owner, fd.Server))
		}
		if fd.Topology.Datacenter == "" {
			errs = append(errs, owner+".topology.datacenter is required")
		}
		if fd.Topology.ComputeCluster == "" {
			errs = append(errs, owner+".topology.computeCluster is required")
		}
		if fd.Topology.Datastore == "" {
			errs = append(errs, owner+".topology.datastore is required")
		}
		if len(fd.Topology.Networks) == 0 {
			errs = append(errs, owner+".topology.networks is required")
		}
		if len(fd.Topology.Networks) > 1 && spec.NodeNetworking == nil {
			errs = append(errs, fmt.Sprintf("%s.topology.networks declares multiple vSphere topology networks; spec.vsphere.nodeNetworking is required", owner))
		}
	}
	errs = append(errs, validateMachineProfiles(prefix+".machineProfiles", v1alpha1.ProvisionerVSphere, spec.MachineProfiles, failureDomains)...)
	return errs
}

func validateProviderKubeVirt(prefix string, spec *v1alpha1.InfraProviderKubeVirt, clusters map[string]v1alpha1.ContainerCluster) []string {
	var errs []string
	hasHostClusterRef := spec.HostClusterRef != nil && spec.HostClusterRef.Name != ""
	hasKubeconfigRef := spec.KubeconfigRef != nil && spec.KubeconfigRef.Name != ""
	if hasHostClusterRef == hasKubeconfigRef {
		errs = append(errs, prefix+" must set exactly one of {hostClusterRef, kubeconfigRef}")
	}
	if spec.HostClusterRef != nil {
		if spec.HostClusterRef.Name == "" {
			errs = append(errs, prefix+".hostClusterRef is required when hostClusterRef is set")
		} else if _, ok := clusters[spec.HostClusterRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.hostClusterRef %q does not match any ContainerCluster", prefix, spec.HostClusterRef.Name))
		}
	}
	if spec.KubeconfigRef != nil && spec.KubeconfigRef.Name == "" {
		errs = append(errs, prefix+".kubeconfigRef is required when kubeconfigRef is set")
	}
	if spec.Namespace == "" {
		errs = append(errs, prefix+".namespace is required")
	} else if !IsDNSLabel(spec.Namespace) {
		errs = append(errs, fmt.Sprintf("%s.namespace %q is not a DNS label", prefix, spec.Namespace))
	}
	if spec.StorageClassRef != nil && spec.StorageClassRef.Name == "" {
		errs = append(errs, prefix+".storageClassRef is required when storageClassRef is set")
	}
	errs = append(errs, validateMachineProfiles(prefix+".machineProfiles", v1alpha1.ProvisionerKubeVirt, spec.MachineProfiles, nil)...)
	return errs
}

// validateMachineProfiles validates the shared MachineProfile shape and
// rejects fields the selected provider's adapter ignores, instead of
// accepting state that diverges from what the operator authored (the
// MachinePoolSpec precedent): template and failureDomainRef drive only the
// vSphere adapter, and dataDisks are provisioned only by the libvirt adapter.
// failureDomains carries spec.vsphere.failureDomains[].name for vSphere
// providers so failureDomainRef resolves like every other reference.
func validateMachineProfiles(prefix, providerType string, profiles []v1alpha1.MachineProfile, failureDomains map[string]bool) []string {
	var errs []string
	seen := map[string]bool{}
	for i, profile := range profiles {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if profile.Name == "" {
			errs = append(errs, owner+".name is required")
		} else if seen[profile.Name] {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, profile.Name))
		}
		seen[profile.Name] = true
		if profile.CPU < 0 || profile.MemoryMiB < 0 || profile.DiskGiB < 0 {
			errs = append(errs, owner+" cpu/memoryMiB/diskGiB must be non-negative")
		}
		if providerType == v1alpha1.ProvisionerVSphere {
			if profile.FailureDomainRef.Name != "" && !failureDomains[profile.FailureDomainRef.Name] {
				errs = append(errs, fmt.Sprintf("%s.failureDomainRef %q does not match any failureDomains[].name", owner, profile.FailureDomainRef.Name))
			}
		} else {
			if profile.Template != "" {
				errs = append(errs, fmt.Sprintf("%s.template is not supported when type=%s; only the vsphere adapter clones machines from a template", owner, providerType))
			}
			if profile.FailureDomainRef.Name != "" {
				errs = append(errs, fmt.Sprintf("%s.failureDomainRef is not supported when type=%s; failure domains exist only on vsphere providers", owner, providerType))
			}
		}
		if providerType != v1alpha1.ProvisionerLibvirt && len(profile.DataDisks) > 0 {
			errs = append(errs, fmt.Sprintf("%s.dataDisks is not supported when type=%s; only the libvirt adapter provisions data disks", owner, providerType))
		}
		for j, disk := range profile.DataDisks {
			diskOwner := fmt.Sprintf("%s.dataDisks[%d]", owner, j)
			if disk.Name == "" {
				errs = append(errs, diskOwner+".name is required")
			}
			if disk.SizeGiB <= 0 {
				errs = append(errs, diskOwner+".sizeGiB must be greater than zero")
			}
		}
	}
	return errs
}

func validateBMCEmulationDefaults(prefix string, defaults *v1alpha1.BMCEmulationDefaults) []string {
	var errs []string
	if defaults.Protocol != "" && defaults.Protocol != v1alpha1.DefaultBMCProtocol {
		errs = append(errs, fmt.Sprintf("%s.protocol %q is not supported yet; only %q is implemented", prefix, defaults.Protocol, v1alpha1.DefaultBMCProtocol))
	}
	enabled := defaults.Enabled == nil || *defaults.Enabled
	if !enabled {
		errs = append(errs, prefix+".enabled=false is not supported; current libvirt apply requires emulated Redfish BMC")
	}
	if enabled && (defaults.Auth == nil || defaults.Auth.CredentialsRef.Name == "") {
		errs = append(errs, prefix+".auth.credentialsRef is required when BMC emulation is enabled")
	}
	port := effectiveBMCEmulationPort(defaults)
	vmediaPort := effectiveBMCEmulationVMediaPort(defaults)
	if port < 1 || port > 65535 {
		errs = append(errs, fmt.Sprintf("%s.port %d out of range", prefix, port))
	}
	if vmediaPort < 1 || vmediaPort > 65535 {
		errs = append(errs, fmt.Sprintf("%s.vmediaPort %d out of range", prefix, vmediaPort))
	}
	if port == vmediaPort {
		errs = append(errs, fmt.Sprintf("%s.port and vmediaPort must be different (both %d)", prefix, port))
	}
	return errs
}

func validateProviderNetworkAttachment(provider v1alpha1.InfraProvider, attachment v1alpha1.NetworkAttachmentCapability) []string {
	var errs []string
	prefix := fmt.Sprintf("InfraProvider/%s spec.networkAttachments[%s]", provider.Metadata.Name, attachment.Name)
	set := 0
	if attachment.Libvirt != nil {
		set++
		if attachment.Libvirt.Bridge == "" {
			errs = append(errs, prefix+".libvirt.bridge is required")
		}
	}
	if attachment.VSphere != nil {
		set++
		if attachment.VSphere.Portgroup == "" {
			errs = append(errs, prefix+".vsphere.portgroup is required")
		}
	}
	if attachment.KubeVirt != nil {
		set++
		if attachment.KubeVirt.NADRef.Name == "" {
			errs = append(errs, prefix+".kubevirt.nadRef is required")
		}
		if attachment.KubeVirt.NADRef.Namespace == "" {
			errs = append(errs, prefix+".kubevirt.nadRefspace is required")
		} else if !IsDNSLabel(attachment.KubeVirt.NADRef.Namespace) {
			errs = append(errs, fmt.Sprintf("%s.kubevirt.nadRefspace %q is not a DNS label", prefix, attachment.KubeVirt.NADRef.Namespace))
		}
	}
	if attachment.BareMetal != nil {
		set++
		if vlan := attachment.BareMetal.VLAN; vlan < 0 || vlan > 4094 {
			errs = append(errs, fmt.Sprintf("%s.bareMetal.vlan %d must be 0..4094", prefix, vlan))
		}
	}
	if set != 1 {
		errs = append(errs, fmt.Sprintf("%s must set exactly one of {libvirt, vsphere, kubevirt, bareMetal} (got %d)", prefix, set))
	}
	if kind := v1alpha1.NetworkAttachmentKind(attachment); kind != "" && provider.Spec.Type != "" && kind != provider.Spec.Type {
		errs = append(errs, fmt.Sprintf("%s.%s must be empty when InfraProvider/%s spec.type=%s", prefix, networkAttachmentArmField(kind), provider.Metadata.Name, provider.Spec.Type))
	}
	return errs
}

func networkAttachmentArmField(kind string) string {
	switch kind {
	case v1alpha1.ProvisionerBareMetal:
		return "bareMetal"
	default:
		return kind
	}
}

func capabilityNames[T any](items []T, name func(T) string) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = name(item)
	}
	return out
}

func validateUniqueCapabilityNames(provider v1alpha1.InfraProvider, kind string, names []string) []string {
	var errs []string
	seen := map[string]bool{}
	for i, name := range names {
		if name == "" {
			errs = append(errs, fmt.Sprintf("InfraProvider/%s spec.%s[%d].name is required", provider.Metadata.Name, kind, i))
			continue
		}
		if seen[name] {
			errs = append(errs, fmt.Sprintf("InfraProvider/%s spec.%s has duplicate name %q", provider.Metadata.Name, kind, name))
		}
		seen[name] = true
	}
	return errs
}

func validateMachineLabels(prefix string, labels map[string]string) []string {
	if len(labels) == 0 {
		return nil
	}
	var errs []string
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		field := fmt.Sprintf("%s.labels[%q]", prefix, key)
		if !isLabelKey(key) {
			errs = append(errs, fmt.Sprintf("%s %q is not a valid label key", field, key))
		}
		if value := labels[key]; !isLabelValue(value) {
			errs = append(errs, fmt.Sprintf("%s value %q is not a valid label value", field, value))
		}
	}
	return errs
}

var macRegex = (func() func(string) bool {
	return func(s string) bool {
		if len(s) != 17 {
			return false
		}
		for i, ch := range s {
			if (i+1)%3 == 0 {
				if ch != ':' && ch != '-' {
					return false
				}
				continue
			}
			if !isHex(byte(ch)) {
				return false
			}
		}
		return true
	}
})()

func looksLikeMAC(s string) bool { return macRegex(s) }

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func joinSortedNames(names []string) string {
	out := append([]string(nil), names...)
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func providerProfiles(provider v1alpha1.InfraProvider) []v1alpha1.MachineProfile {
	return stateview.ProviderMachineProfiles(provider)
}
