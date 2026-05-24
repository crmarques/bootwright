package desiredstate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/support"
)

func validateProviders(state v1alpha1.State) []string {
	var errs []string
	hosts := indexHosts(state.Hosts)
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
		errs = append(errs, validateProviderCapabilities(p, hosts)...)
	}
	errs = append(errs, validateLibvirtBMCEmulationHostPorts(state)...)
	return errs
}

func validateProviderCapabilities(p v1alpha1.InfraProvider, hosts map[string]v1alpha1.Host) []string {
	var errs []string
	caps := p.Spec
	errs = append(errs, validateUniqueCapabilityNames(p, "machineProfiles", capabilityNames(caps.MachineProfiles, func(x v1alpha1.MachineProfileCapability) string { return x.Name }))...)
	errs = append(errs, validateUniqueCapabilityNames(p, "machines", capabilityNames(caps.Machines, func(x v1alpha1.MachineCapability) string { return x.Name }))...)
	errs = append(errs, validateUniqueCapabilityNames(p, "loadBalancers", capabilityNames(caps.LoadBalancers, func(x v1alpha1.LoadBalancerCapability) string { return x.Name }))...)
	errs = append(errs, validateUniqueCapabilityNames(p, "artifactPublishers", capabilityNames(caps.ArtifactPublishers, func(x v1alpha1.ArtifactPublisherCapability) string { return x.Name }))...)
	errs = append(errs, validateUniqueCapabilityNames(p, "proxies", capabilityNames(caps.Proxies, func(x v1alpha1.ProxyCapability) string { return x.Name }))...)
	errs = append(errs, validateUniqueCapabilityNames(p, "dns", capabilityNames(caps.DNS, func(x v1alpha1.DNSCapability) string { return x.Name }))...)
	errs = append(errs, validateUniqueCapabilityNames(p, "registries", capabilityNames(caps.Registries, func(x v1alpha1.RegistryCapability) string { return x.Name }))...)

	for _, mp := range caps.MachineProfiles {
		errs = append(errs, validateMachineProfile(p, mp, hosts)...)
	}
	for _, m := range caps.Machines {
		errs = append(errs, validateProviderMachine(p, m)...)
	}
	for _, lb := range caps.LoadBalancers {
		errs = append(errs, validateServiceLoadBalancer(p, lb, hosts)...)
	}
	for _, publisher := range caps.ArtifactPublishers {
		errs = append(errs, validateServiceArtifactPublisher(p, publisher, hosts)...)
	}
	for _, proxy := range caps.Proxies {
		errs = append(errs, validateServiceProxy(p, proxy, hosts)...)
	}
	for _, d := range caps.DNS {
		errs = append(errs, validateServiceDNS(p, d, hosts)...)
	}
	for _, r := range caps.Registries {
		errs = append(errs, validateServiceRegistry(p, r, hosts)...)
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

func validateMachineProfile(p v1alpha1.InfraProvider, mp v1alpha1.MachineProfileCapability, hosts map[string]v1alpha1.Host) []string {
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
		errs = append(errs, validateMachineProfileKubeVirt(prefix, mp.KubeVirt)...)
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

func validateMachineProfileKubeVirt(prefix string, k *v1alpha1.MachineProfileKubeVirtProvisioner) []string {
	var errs []string
	if k.ClusterRef.Name == "" {
		errs = append(errs, fmt.Sprintf("%s.kubevirt.clusterRef.name is required", prefix))
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

func validateProviderMachine(p v1alpha1.InfraProvider, m v1alpha1.MachineCapability) []string {
	var errs []string
	prefix := fmt.Sprintf("InfraProvider/%s spec.machines[%s]", p.Metadata.Name, m.Name)
	set := 0
	if m.BareMetal != nil {
		set++
		errs = append(errs, validateProviderMachineBareMetal(prefix, m.BareMetal)...)
	}
	if set != 1 {
		errs = append(errs, fmt.Sprintf("%s must set exactly one of {baremetal} (got %d)", prefix, set))
	}
	return errs
}

func validateProviderMachineBareMetal(prefix string, b *v1alpha1.MachineBareMetalCapability) []string {
	var errs []string
	if b.BMC.Address == "" {
		errs = append(errs, fmt.Sprintf("%s.baremetal.bmc.address is required", prefix))
	}
	if b.BMC.Protocol != "" && b.BMC.Protocol != v1alpha1.DefaultBMCProtocol {
		errs = append(errs, fmt.Sprintf("%s.baremetal.bmc.protocol %q is not supported yet; only %q is implemented", prefix, b.BMC.Protocol, v1alpha1.DefaultBMCProtocol))
	}
	if b.BMC.CredentialsRef.Name == "" {
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
	if b.BootMACAddress == "" {
		errs = append(errs, fmt.Sprintf("%s.baremetal.bootMACAddress is required", prefix))
	} else if !looksLikeMAC(b.BootMACAddress) {
		errs = append(errs, fmt.Sprintf("%s.baremetal.bootMACAddress %q is not a valid MAC address", prefix, b.BootMACAddress))
	} else if !bootMACKnown {
		errs = append(errs, fmt.Sprintf("%s.baremetal.bootMACAddress %q does not match any baremetal.interfaces[].macAddress", prefix, b.BootMACAddress))
	}
	return errs
}

func validateServiceLoadBalancer(p v1alpha1.InfraProvider, lb v1alpha1.LoadBalancerCapability, hosts map[string]v1alpha1.Host) []string {
	prefix := fmt.Sprintf("InfraProvider/%s spec.loadBalancers[%s]", p.Metadata.Name, lb.Name)
	if lb.HAProxy == nil {
		return []string{fmt.Sprintf("%s must set exactly one of {haProxy}", prefix)}
	}
	return validateServiceHostRef(prefix+".haProxy.hostRef", lb.HAProxy.HostRef, hosts, v1alpha1.ComponentSlotLoadBalancer, "haProxy")
}

func validateServiceArtifactPublisher(p v1alpha1.InfraProvider, publisher v1alpha1.ArtifactPublisherCapability, hosts map[string]v1alpha1.Host) []string {
	prefix := fmt.Sprintf("InfraProvider/%s spec.artifactPublishers[%s]", p.Metadata.Name, publisher.Name)
	if publisher.HTTP == nil {
		return []string{fmt.Sprintf("%s must set exactly one of {http}", prefix)}
	}
	errs := validateServiceHostRef(prefix+".http.hostRef", publisher.HTTP.HostRef, hosts, v1alpha1.ComponentSlotArtifacts, "http")
	if publisher.HTTP.Port < 0 || publisher.HTTP.Port > 65535 {
		errs = append(errs, fmt.Sprintf("%s.http.port %d out of range", prefix, publisher.HTTP.Port))
	}
	host, ok := hosts[publisher.HTTP.HostRef.Name]
	if ok {
		errs = append(errs, validateArtifactRouteAddress(prefix+".http.routes.redfishVirtualMedia", publisher.HTTP.Routes.RedfishVirtualMedia, host)...)
		errs = append(errs, validateArtifactRouteAddress(prefix+".http.routes.clusterInstall", publisher.HTTP.Routes.ClusterInstall, host)...)
	}
	return errs
}

func validateArtifactRouteAddress(prefix string, route v1alpha1.ArtifactRoute, host v1alpha1.Host) []string {
	if route.AddressName == "" {
		return nil
	}
	if _, ok := v1alpha1.HostAddressByName(host, route.AddressName); !ok {
		return []string{fmt.Sprintf("%s.addressName %q does not resolve on Host/%s spec.addresses", prefix, route.AddressName, host.Metadata.Name)}
	}
	return nil
}

func validateServiceProxy(p v1alpha1.InfraProvider, proxy v1alpha1.ProxyCapability, hosts map[string]v1alpha1.Host) []string {
	prefix := fmt.Sprintf("InfraProvider/%s spec.proxies[%s]", p.Metadata.Name, proxy.Name)
	if proxy.Squid == nil {
		return []string{fmt.Sprintf("%s must set exactly one of {squid}", prefix)}
	}
	return validateServiceHostRef(prefix+".squid.hostRef", proxy.Squid.HostRef, hosts, v1alpha1.ComponentSlotProxy, "squid")
}

func validateServiceDNS(p v1alpha1.InfraProvider, d v1alpha1.DNSCapability, hosts map[string]v1alpha1.Host) []string {
	prefix := fmt.Sprintf("InfraProvider/%s spec.dns[%s]", p.Metadata.Name, d.Name)
	if d.Dnsmasq == nil {
		return []string{fmt.Sprintf("%s must set exactly one of {dnsmasq}", prefix)}
	}
	return validateServiceHostRef(prefix+".dnsmasq.hostRef", d.Dnsmasq.HostRef, hosts, v1alpha1.ComponentSlotNameResolution, "dnsmasq")
}

func validateServiceRegistry(p v1alpha1.InfraProvider, r v1alpha1.RegistryCapability, hosts map[string]v1alpha1.Host) []string {
	prefix := fmt.Sprintf("InfraProvider/%s spec.registries[%s]", p.Metadata.Name, r.Name)
	if r.MirrorRegistry == nil {
		return []string{fmt.Sprintf("%s must set exactly one of {mirrorRegistry}", prefix)}
	}
	return validateServiceHostRef(prefix+".mirrorRegistry.hostRef", r.MirrorRegistry.HostRef, hosts, v1alpha1.ComponentSlotRegistry, "mirrorRegistry")
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
