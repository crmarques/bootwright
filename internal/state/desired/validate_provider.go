package desiredstate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/support"
)

func validateProviders(state v1alpha1.State) []string {
	var errs []string
	hosts := indexHosts(state.Hosts)
	clusters := indexContainerClusters(state.ContainerClusters)
	containerMachines := providerMachinesUsedByContainerClusters(state)
	seen := map[string]bool{}
	for _, p := range state.InfraProviders {
		if e := validateName(v1alpha1.KindInfraProvider, p.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[p.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate InfraProvider %q", p.Metadata.Name))
		}
		seen[p.Metadata.Name] = true
		errs = append(errs, validateProviderCapabilities(p, hosts, clusters, containerMachines[p.Metadata.Name])...)
	}
	errs = append(errs, validateLibvirtBMCEmulationHostPorts(state)...)
	return errs
}

func validateProviderCapabilities(p v1alpha1.InfraProvider, hosts map[string]v1alpha1.Host, clusters map[string]v1alpha1.ContainerCluster, containerMachines map[string]bool) []string {
	var errs []string
	caps := p.Spec
	errs = append(errs, validateUniqueCapabilityNames(p, "machineProfiles", capabilityNames(caps.MachineProfiles, func(x v1alpha1.MachineProfileCapability) string { return x.Name }))...)
	errs = append(errs, validateUniqueCapabilityNames(p, "machines", capabilityNames(caps.Machines, func(x v1alpha1.MachineCapability) string { return x.Name }))...)

	for _, mp := range caps.MachineProfiles {
		errs = append(errs, validateMachineProfile(p, mp, hosts, clusters)...)
	}
	for _, m := range caps.Machines {
		errs = append(errs, validateProviderMachine(p, m, containerMachines[m.Name])...)
	}
	return errs
}

func capabilityNames[T any](items []T, name func(T) string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = name(it)
	}
	return out
}

func validateUniqueCapabilityNames(p v1alpha1.InfraProvider, kind string, names []string) []string {
	var errs []string
	seen := map[string]bool{}
	for i, name := range names {
		if name == "" {
			errs = append(errs, fmt.Sprintf("InfraProvider/%s spec.%s[%d].name is required", p.Metadata.Name, kind, i))
			continue
		}
		if seen[name] {
			errs = append(errs, fmt.Sprintf("InfraProvider/%s spec.%s has duplicate name %q (names are unique per kind)", p.Metadata.Name, kind, name))
		}
		seen[name] = true
	}
	return errs
}

func validateMachineProfile(p v1alpha1.InfraProvider, mp v1alpha1.MachineProfileCapability, hosts map[string]v1alpha1.Host, clusters map[string]v1alpha1.ContainerCluster) []string {
	var errs []string
	prefix := fmt.Sprintf("InfraProvider/%s spec.machineProfiles[%s]", p.Metadata.Name, mp.Name)
	if mp.CPU < 0 || mp.MemoryMiB < 0 || mp.DiskGiB < 0 {
		errs = append(errs, fmt.Sprintf("%s cpu/memoryMiB/diskGiB must be non-negative", prefix))
	}
	set := 0
	if mp.Libvirt != nil {
		set++
		errs = append(errs, validateMachineProfileLibvirt(prefix, mp.Libvirt, hosts)...)
	}
	if mp.VSphere != nil {
		set++
		errs = append(errs, validateMachineProfileVSphere(prefix, mp.VSphere)...)
	}
	if mp.KubeVirt != nil {
		set++
		errs = append(errs, validateMachineProfileKubeVirt(prefix, mp.KubeVirt, clusters)...)
	}
	if set != 1 {
		errs = append(errs, fmt.Sprintf("%s must set exactly one of {libvirt, vsphere, kubevirt} (got %d)", prefix, set))
	}
	return errs
}

func validateMachineProfileLibvirt(prefix string, l *v1alpha1.MachineProfileLibvirtProvisioner, hosts map[string]v1alpha1.Host) []string {
	var errs []string
	if l.HostRef.Name == "" {
		errs = append(errs, fmt.Sprintf("%s.libvirt.hostRef.name is required", prefix))
	} else {
		host, ok := hosts[l.HostRef.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.libvirt.hostRef %q does not match any Host", prefix, l.HostRef.Name))
		} else if !hostHasCapability(host, v1alpha1.HostCapabilityLibvirt) {
			errs = append(errs, fmt.Sprintf("%s.libvirt.hostRef %q lacks capability %q", prefix, l.HostRef.Name, v1alpha1.HostCapabilityLibvirt))
		}
	}
	if l.URI == "" {
		errs = append(errs, fmt.Sprintf("%s.libvirt.uri is required (e.g. qemu:///system)", prefix))
	}
	d := l.BMCEmulationDefaults
	if d == nil {
		errs = append(errs, fmt.Sprintf("%s.libvirt.bmcEmulationDefaults is required for current libvirt apply support", prefix))
		return errs
	}
	if d.Protocol != "" && d.Protocol != v1alpha1.DefaultBMCProtocol {
		errs = append(errs, fmt.Sprintf("%s.libvirt.bmcEmulationDefaults.protocol %q is not supported yet; only %q is implemented", prefix, d.Protocol, v1alpha1.DefaultBMCProtocol))
	}
	enabled := d.Enabled == nil || *d.Enabled
	if !enabled {
		errs = append(errs, fmt.Sprintf("%s.libvirt.bmcEmulationDefaults.enabled=false is not supported; current libvirt apply requires emulated Redfish BMC", prefix))
	}
	if d.Auth == nil || d.Auth.CredentialRef.Name == "" {
		errs = append(errs, fmt.Sprintf("%s.libvirt.bmcEmulationDefaults.auth.credentialRef.name is required when bmcEmulationDefaults is enabled", prefix))
	}
	port := effectiveBMCEmulationPort(d)
	vmediaPort := effectiveBMCEmulationVMediaPort(d)
	if port < 1 || port > 65535 {
		errs = append(errs, fmt.Sprintf("%s.libvirt.bmcEmulationDefaults.port %d out of range", prefix, port))
	}
	if vmediaPort < 1 || vmediaPort > 65535 {
		errs = append(errs, fmt.Sprintf("%s.libvirt.bmcEmulationDefaults.vmediaPort %d out of range", prefix, vmediaPort))
	}
	if port == vmediaPort {
		errs = append(errs, fmt.Sprintf("%s.libvirt.bmcEmulationDefaults.port and vmediaPort must be different (both %d)", prefix, port))
	}
	return errs
}

func validateMachineProfileVSphere(prefix string, v *v1alpha1.MachineProfileVSphereProvisioner) []string {
	var errs []string
	if len(v.VCenters) == 0 {
		errs = append(errs, fmt.Sprintf("%s.vsphere.vcenters is required (at least one)", prefix))
	}
	for i, vc := range v.VCenters {
		owner := fmt.Sprintf("%s.vsphere.vcenters[%d]", prefix, i)
		if vc.Server == "" {
			errs = append(errs, fmt.Sprintf("%s.server is required", owner))
		}
		if vc.Port < 0 || vc.Port > 65535 {
			errs = append(errs, fmt.Sprintf("%s.port %d out of range", owner, vc.Port))
		}
		if len(vc.Datacenters) == 0 {
			errs = append(errs, fmt.Sprintf("%s.datacenters is required", owner))
		}
		if vc.CredentialsRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.credentialsRef.name is required", owner))
		}
	}
	if len(v.FailureDomains) == 0 {
		errs = append(errs, fmt.Sprintf("%s.vsphere.failureDomains is required (at least one)", prefix))
	}
	for i, fd := range v.FailureDomains {
		owner := fmt.Sprintf("%s.vsphere.failureDomains[%d]", prefix, i)
		if fd.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.name is required", owner))
		}
		if fd.Region == "" {
			errs = append(errs, fmt.Sprintf("%s.region is required", owner))
		}
		if fd.Zone == "" {
			errs = append(errs, fmt.Sprintf("%s.zone is required", owner))
		}
		if fd.Server == "" {
			errs = append(errs, fmt.Sprintf("%s.server is required", owner))
		}
		if fd.Topology.Datacenter == "" {
			errs = append(errs, fmt.Sprintf("%s.topology.datacenter is required", owner))
		}
		if fd.Topology.ComputeCluster == "" {
			errs = append(errs, fmt.Sprintf("%s.topology.computeCluster is required", owner))
		}
		if fd.Topology.Datastore == "" {
			errs = append(errs, fmt.Sprintf("%s.topology.datastore is required", owner))
		}
		if len(fd.Topology.Networks) == 0 {
			errs = append(errs, fmt.Sprintf("%s.topology.networks is required", owner))
		}
	}
	return errs
}

func validateMachineProfileKubeVirt(prefix string, k *v1alpha1.MachineProfileKubeVirtProvisioner, clusters map[string]v1alpha1.ContainerCluster) []string {
	var errs []string
	hasHostContainerClusterRef := k.HostContainerClusterRef != nil && k.HostContainerClusterRef.Name != ""
	hasKubeconfigRef := k.KubeconfigRef != nil && k.KubeconfigRef.Name != ""
	if hasHostContainerClusterRef == hasKubeconfigRef {
		errs = append(errs, fmt.Sprintf("%s.kubevirt must set exactly one of {hostContainerClusterRef, kubeconfigRef}", prefix))
	}
	if k.HostContainerClusterRef != nil {
		if k.HostContainerClusterRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.kubevirt.hostContainerClusterRef.name is required when hostContainerClusterRef is set", prefix))
		} else if _, ok := clusters[k.HostContainerClusterRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.kubevirt.hostContainerClusterRef.name %q does not match any ContainerCluster", prefix, k.HostContainerClusterRef.Name))
		}
	}
	if k.KubeconfigRef != nil && k.KubeconfigRef.Name == "" {
		errs = append(errs, fmt.Sprintf("%s.kubevirt.kubeconfigRef.name is required when kubeconfigRef is set", prefix))
	}
	if k.Namespace == "" {
		errs = append(errs, fmt.Sprintf("%s.kubevirt.namespace is required", prefix))
	} else if !IsDNSLabel(k.Namespace) {
		errs = append(errs, fmt.Sprintf("%s.kubevirt.namespace %q is not a DNS label", prefix, k.Namespace))
	}
	if k.StorageClassRef != nil && k.StorageClassRef.Name == "" {
		errs = append(errs, fmt.Sprintf("%s.kubevirt.storageClassRef.name is required when storageClassRef is set", prefix))
	}
	return errs
}

func validateProviderMachine(p v1alpha1.InfraProvider, m v1alpha1.MachineCapability, requireBoot bool) []string {
	var errs []string
	prefix := fmt.Sprintf("InfraProvider/%s spec.machines[%s]", p.Metadata.Name, m.Name)
	set := 0
	if m.BareMetal != nil {
		set++
		errs = append(errs, validateProviderMachineBareMetal(prefix, m.BareMetal, requireBoot)...)
	}
	if set != 1 {
		errs = append(errs, fmt.Sprintf("%s must set exactly one of {baremetal} (got %d)", prefix, set))
	}
	return errs
}

func validateProviderMachineBareMetal(prefix string, b *v1alpha1.MachineBareMetalCapability, requireBoot bool) []string {
	var errs []string
	validateBMC := requireBoot || b.BMC.Address != "" || b.BMC.Protocol != "" || b.BMC.CredentialsRef.Name != "" || b.BMC.DisableCertificateVerification
	if validateBMC && b.BMC.Address == "" {
		errs = append(errs, fmt.Sprintf("%s.baremetal.bmc.address is required", prefix))
	}
	if b.BMC.Protocol != "" && b.BMC.Protocol != v1alpha1.DefaultBMCProtocol {
		errs = append(errs, fmt.Sprintf("%s.baremetal.bmc.protocol %q is not supported yet; only %q is implemented", prefix, b.BMC.Protocol, v1alpha1.DefaultBMCProtocol))
	}
	if validateBMC && b.BMC.CredentialsRef.Name == "" {
		errs = append(errs, fmt.Sprintf("%s.baremetal.bmc.credentialsRef.name is required", prefix))
	}
	if len(b.Interfaces) == 0 {
		errs = append(errs, fmt.Sprintf("%s.baremetal.interfaces is required (at least one)", prefix))
		return errs
	}
	seen := map[string]bool{}
	bootMACKnown := b.BootMACAddress == ""
	for i, iface := range b.Interfaces {
		ifacePrefix := fmt.Sprintf("%s.baremetal.interfaces[%d]", prefix, i)
		if iface.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.name is required", ifacePrefix))
		} else if seen[iface.Name] {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", ifacePrefix, iface.Name))
		}
		seen[iface.Name] = true
		if iface.MACAddress == "" {
			errs = append(errs, fmt.Sprintf("%s.macAddress is required (immutable NIC fact)", ifacePrefix))
		} else if !looksLikeMAC(iface.MACAddress) {
			errs = append(errs, fmt.Sprintf("%s.macAddress %q is not a valid MAC address", ifacePrefix, iface.MACAddress))
		}
		if iface.MACAddress != "" && strings.EqualFold(iface.MACAddress, b.BootMACAddress) {
			bootMACKnown = true
		}
	}
	if requireBoot && b.BootMACAddress == "" {
		errs = append(errs, fmt.Sprintf("%s.baremetal.bootMACAddress is required", prefix))
	} else if !looksLikeMAC(b.BootMACAddress) {
		if b.BootMACAddress != "" {
			errs = append(errs, fmt.Sprintf("%s.baremetal.bootMACAddress %q is not a valid MAC address", prefix, b.BootMACAddress))
		}
	} else if !bootMACKnown {
		errs = append(errs, fmt.Sprintf("%s.baremetal.bootMACAddress %q does not match any baremetal.interfaces[].macAddress", prefix, b.BootMACAddress))
	}
	return errs
}

func providerMachinesUsedByContainerClusters(state v1alpha1.State) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	infras := indexClusterInfras(state.ClusterInfras)
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			infra, ok := infras[node.MachineRef.ClusterInfra]
			if !ok {
				continue
			}
			for _, machine := range infra.Spec.Components.Machines {
				if machine.Name != node.MachineRef.Name || machine.From.Name == "" || machine.From.Provider == "" {
					continue
				}
				if out[machine.From.Provider] == nil {
					out[machine.From.Provider] = map[string]bool{}
				}
				out[machine.From.Provider][machine.From.Name] = true
			}
		}
	}
	return out
}

func validateServiceHostRef(owner string, ref v1alpha1.LocalObjectReference, hosts map[string]v1alpha1.Host, kind, realisation string) []string {
	var errs []string
	capabilities := support.ServiceHostCapabilities(kind, realisation)
	if len(capabilities) == 0 {
		return validateHostRefCapability(owner, ref, hosts, "")
	}
	for _, capability := range capabilities {
		errs = append(errs, validateHostRefCapability(owner, ref, hosts, capability)...)
	}
	return errs
}

func validateHostRefCapability(owner string, ref v1alpha1.LocalObjectReference, hosts map[string]v1alpha1.Host, want string) []string {
	if ref.Name == "" {
		return []string{fmt.Sprintf("%s.name is required", owner)}
	}
	host, ok := hosts[ref.Name]
	if !ok {
		return []string{fmt.Sprintf("%s %q does not match any Host", owner, ref.Name)}
	}
	if want != "" && !hostHasCapability(host, want) {
		return []string{fmt.Sprintf("%s %q lacks capability %q (Host.spec.capabilities)", owner, ref.Name, want)}
	}
	return nil
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
