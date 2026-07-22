package desiredstate

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/nmstate"
)

var validMachineCapabilities = map[string]bool{
	v1alpha1.MachineCapabilityOpenShiftNode:    true,
	v1alpha1.MachineCapabilityLibvirt:          true,
	v1alpha1.MachineCapabilityContainerRuntime: true,
	v1alpha1.MachineCapabilityArtifactServer:   true,
	v1alpha1.MachineCapabilityLoadBalancer:     true,
	v1alpha1.MachineCapabilityProxy:            true,
	v1alpha1.MachineCapabilityNameResolution:   true,
	v1alpha1.MachineCapabilityNTP:              true,
	v1alpha1.MachineCapabilityRegistry:         true,
	v1alpha1.MachineCapabilityCephAdmin:        true,
	v1alpha1.MachineCapabilityCephNode:         true,
}

func validateMachines(state v1alpha1.State) []string {
	var errs []string
	providers := indexProviders(state.InfraProviders)
	networks := indexNetworkConfigs(state.NetworkConfigs)
	installProfiles := indexMachineInstallProfiles(state.MachineInstallProfiles)
	for _, machine := range state.Machines {
		if e := validateName(v1alpha1.KindMachine, machine.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("Machine/%s spec", machine.Metadata.Name)
		provider, providerOK := providers[machine.Spec.Substrate.ProviderRef.Name]
		errs = append(errs, validateMachineLabels(prefix, machine.Metadata.Labels)...)
		errs = append(errs, validateMachineCapabilitySet(prefix+".capabilities", machine.Spec.Capabilities)...)
		errs = append(errs, validateMachineOS(prefix+".os", machine, installProfiles)...)
		errs = append(errs, validateMachineSubstrate(prefix+".substrate", machine, provider, providerOK)...)
		errs = append(errs, validateMachineHardware(prefix+".hardware", machine, provider, providerOK)...)
		errs = append(errs, validateMachineNetwork(prefix+".network", machine, networks, provider)...)
		errs = append(errs, validateMachineAddresses(prefix, machine)...)
		errs = append(errs, validateMachineAccess(prefix+".access", machine)...)
	}
	errs = append(errs, validateMachineRootLogin(state)...)
	errs = append(errs, validateMachineImages(state)...)
	errs = append(errs, validateMachineInstallProfiles(state)...)
	errs = append(errs, validateUniqueMachineInstallIPs(state)...)
	errs = append(errs, validateUniqueMachineNICMACs(state)...)
	return errs
}

func validateUniqueMachineInstallIPs(state v1alpha1.State) []string {
	byIP := map[string][]string{}
	for _, machine := range state.Machines {
		addresses := map[string]string{}
		for _, a := range machine.Spec.Addresses {
			if a.Name != "" {
				addresses[a.Name] = a.Address
			}
		}
		seen := map[string]bool{}
		for _, ia := range machine.Spec.Network.Config.InterfaceAddresses {
			ip := strings.TrimSpace(addresses[ia.AddressRef.Name])
			if ip == "" || seen[ip] {
				continue
			}
			seen[ip] = true
			byIP[ip] = append(byIP[ip], machine.Metadata.Name)
		}
	}
	ips := make([]string, 0, len(byIP))
	for ip := range byIP {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	var errs []string
	for _, ip := range ips {
		names := byIP[ip]
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		errs = append(errs, fmt.Sprintf("install IP %q is assigned by interfaceAddresses on more than one Machine (%s); a static install address identifies one node, so a shared value boots two nodes into an IP conflict that stalls the install. Give each Machine a unique address", ip, strings.Join(names, ", ")))
	}
	return errs
}

func validateUniqueMachineNICMACs(state v1alpha1.State) []string {
	byMAC := map[string][]string{}
	for _, machine := range state.Machines {
		seen := map[string]bool{}
		for _, nic := range machine.Spec.Hardware.NICs {
			mac := strings.ToLower(strings.TrimSpace(nic.MACAddress))
			if mac == "" || seen[mac] {
				continue
			}
			seen[mac] = true
			byMAC[mac] = append(byMAC[mac], machine.Metadata.Name)
		}
	}
	macs := make([]string, 0, len(byMAC))
	for mac := range byMAC {
		macs = append(macs, mac)
	}
	sort.Strings(macs)
	var errs []string
	for _, mac := range macs {
		names := byMAC[mac]
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		errs = append(errs, fmt.Sprintf("NIC MAC %q is declared by more than one Machine (%s); the agent installer matches hosts by MAC, so a shared address makes two nodes contend for one host entry. Give each hardware.nics[].macAddress a unique value", mac, strings.Join(names, ", ")))
	}
	return errs
}

func validateMachineCapabilitySet(owner string, capabilities []string) []string {
	var errs []string
	seen := map[string]bool{}
	for _, capability := range capabilities {
		if capability == "" {
			errs = append(errs, owner+" contains an empty capability")
			continue
		}
		if seen[capability] {
			errs = append(errs, fmt.Sprintf("%s contains duplicate capability %q", owner, capability))
		}
		seen[capability] = true
		if !validMachineCapabilities[capability] {
			errs = append(errs, fmt.Sprintf("%s %q is not in the canonical Machine capability set", owner, capability))
		}
	}
	return errs
}

func validateMachineOS(prefix string, machine v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile) []string {
	var errs []string
	if machine.Spec.OS.Provided == nil {
		errs = append(errs, prefix+".provided is required")
		return errs
	}
	if *machine.Spec.OS.Provided {
		if machine.Spec.OS.InstallProfileRef.Name != "" {
			errs = append(errs, prefix+".installProfileRef must be empty when provided=true")
		}
		if !machine.Spec.OS.Install.IsZero() {
			errs = append(errs, prefix+".install must be empty when provided=true")
		}
		if !machine.Spec.Network.Config.IsZero() {
			errs = append(errs, "Machine/"+machine.Metadata.Name+" spec.network.config must be empty when os.provided=true")
		}
		return errs
	}
	if machine.Spec.OS.InstallProfileRef.Name != "" {
		profile, ok := installProfiles[machine.Spec.OS.InstallProfileRef.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.installProfileRef %q does not match any MachineInstallProfile", prefix, machine.Spec.OS.InstallProfileRef.Name))
		} else if !machineInstallStringListContains(profile.Spec.Customizations.Services.Enabled, "sshd") {
			errs = append(errs, fmt.Sprintf("%s.installProfileRef %q references MachineInstallProfile/%s without customizations.services.enabled containing sshd", prefix, machine.Spec.OS.InstallProfileRef.Name, profile.Metadata.Name))
		}
	}
	return errs
}

func validateMachineAccess(prefix string, machine v1alpha1.Machine) []string {
	if machine.Spec.OS.InstallProfileRef.Name != "" && machine.Spec.Access.SSH == nil {
		return []string{prefix + ".ssh is required when os.installProfileRef is set"}
	}
	if machine.Spec.Access.SSH == nil {
		return nil
	}
	return validateMachineSSH(prefix, machine)
}

func validateMachineRootLogin(state v1alpha1.State) []string {
	var errs []string
	replacing := map[string]string{}
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil {
			continue
		}
		user := v1alpha1.StorageClusterCephadmSSHUser(cluster)
		if user == v1alpha1.RootSSHUser {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			replacing[node.MachineRef.Name] = cluster.Metadata.Name
		}
	}
	for _, machine := range state.Machines {
		prefix := fmt.Sprintf("Machine/%s spec.access.rootLogin", machine.Metadata.Name)
		switch machine.Spec.Access.RootLogin {
		case v1alpha1.MachineRootLoginKeep, v1alpha1.MachineRootLoginRevoke, "":
		default:
			errs = append(errs, fmt.Sprintf("%s %q must be one of {%s}", prefix, machine.Spec.Access.RootLogin, strings.Join(v1alpha1.MachineRootLoginValues(), ", ")))
			continue
		}
		if !v1alpha1.MachineRevokesRootLogin(machine) {
			continue
		}
		if machine.Spec.Access.SSH == nil {
			errs = append(errs, fmt.Sprintf("%s is %q but spec.access.ssh is not declared; Bootwright would have no identity to reach the machine with before or after the revoke", prefix, v1alpha1.MachineRootLoginRevoke))
			continue
		}
		if _, ok := replacing[machine.Metadata.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s is %q but no managed Ceph StorageCluster lists this Machine in spec.ceph.topology.nodes with a non-root spec.ceph.cephadm.clusterSSH.user; revoking root SSH would leave no account to reach it. Add the Machine to such a cluster, or set %s back to %q", prefix, v1alpha1.MachineRootLoginRevoke, prefix, v1alpha1.MachineRootLoginKeep))
		}
	}
	return errs
}

func validateMachineSSH(prefix string, machine v1alpha1.Machine) []string {
	var errs []string
	ssh := machine.Spec.Access.SSH
	if ssh.AddressRef.Name == "" {
		errs = append(errs, prefix+".ssh.addressRef is required")
	} else if _, ok := v1alpha1.MachineAddressByName(machine, ssh.AddressRef.Name); !ok {
		errs = append(errs, fmt.Sprintf("%s.ssh.addressRef %q does not resolve to spec.addresses[].name", prefix, ssh.AddressRef.Name))
	}
	if ssh.KeyRef.Name == "" {
		errs = append(errs, prefix+".ssh.keyRef is required")
	}
	return errs
}

func validateMachineAddresses(prefix string, machine v1alpha1.Machine) []string {
	var errs []string
	seen := map[string]bool{}
	for i, address := range machine.Spec.Addresses {
		owner := fmt.Sprintf("%s.addresses[%d]", prefix, i)
		if address.Name == "" {
			errs = append(errs, owner+".name is required")
		} else if seen[address.Name] {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, address.Name))
		}
		seen[address.Name] = true
		if address.Address == "" {
			errs = append(errs, owner+".address is required")
		}
		if address.Name == v1alpha1.MachineAddressFQDN && address.Address != "" && !isDNSSubdomainName(address.Address) {
			errs = append(errs, fmt.Sprintf("%s.address %q is not a DNS subdomain; %q names the machine's canonical DNS entry and must be a resolvable fully-qualified name", owner, address.Address, v1alpha1.MachineAddressFQDN))
		}
	}
	return errs
}

func validateMachineSubstrate(prefix string, machine v1alpha1.Machine, provider v1alpha1.InfraProvider, providerOK bool) []string {
	var errs []string
	providerName := machine.Spec.Substrate.ProviderRef.Name
	if v1alpha1.MachineRequiresSubstrate(machine) && providerName == "" {
		errs = append(errs, prefix+".providerRef is required when os.provided=false")
		return errs
	}
	if providerName == "" {
		if machine.Spec.Substrate.ProfileRef.Name != "" {
			errs = append(errs, prefix+".profileRef requires providerRef")
		}
		return errs
	}
	if !providerOK {
		errs = append(errs, fmt.Sprintf("%s.providerRef %q does not match any InfraProvider", prefix, providerName))
		return errs
	}
	switch provider.Spec.Type {
	case v1alpha1.ProvisionerBareMetal:
		if machine.Spec.Substrate.ProfileRef.Name != "" {
			errs = append(errs, prefix+".profileRef must be empty for baremetal providers")
		}
	case v1alpha1.ProvisionerLibvirt, v1alpha1.ProvisionerVSphere, v1alpha1.ProvisionerKubeVirt:
		if v1alpha1.MachineRequiresSubstrate(machine) && machine.Spec.Substrate.ProfileRef.Name == "" {
			errs = append(errs, prefix+".profileRef is required for "+provider.Spec.Type+" providers when os.provided=false")
		}
		if machine.Spec.Substrate.ProfileRef.Name != "" {
			if _, ok := lookupMachineProfile(provider, machine.Spec.Substrate.ProfileRef.Name); !ok {
				errs = append(errs, fmt.Sprintf("%s.profileRef %q does not match any profile on InfraProvider/%s", prefix, machine.Spec.Substrate.ProfileRef.Name, provider.Metadata.Name))
			}
		}
	}
	return errs
}

func validateMachineHardware(prefix string, machine v1alpha1.Machine, provider v1alpha1.InfraProvider, providerOK bool) []string {
	var errs []string
	nicsByName := map[string]v1alpha1.MachineNIC{}
	seenMAC := map[string]bool{}
	for i, nic := range machine.Spec.Hardware.NICs {
		owner := fmt.Sprintf("%s.nics[%d]", prefix, i)
		if nic.Name == "" {
			errs = append(errs, owner+".name is required")
		} else if _, ok := nicsByName[nic.Name]; ok {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, nic.Name))
		}
		nicsByName[nic.Name] = nic
		if nic.MACAddress != "" && !looksLikeMAC(nic.MACAddress) {
			errs = append(errs, fmt.Sprintf("%s.macAddress %q is not a valid MAC address", owner, nic.MACAddress))
		}
		if nic.MACAddress != "" && looksLikeMAC(nic.MACAddress) && providerOK &&
			provider.Spec.Type == v1alpha1.ProvisionerVSphere && !macInVSphereManualRange(nic.MACAddress) {
			errs = append(errs, fmt.Sprintf("%s.macAddress %q is outside the vCenter manually-assigned MAC range 00:50:56:00:00:00-00:50:56:3f:ff:ff; vCenter rejects a VM create whose NIC carries a manual MAC outside it. Use an address in that range, or omit macAddress to let bootwright generate one", owner, nic.MACAddress))
		}
		if nic.MACAddress != "" {
			if seenMAC[nic.MACAddress] {
				errs = append(errs, fmt.Sprintf("%s.macAddress %q is duplicated", owner, nic.MACAddress))
			}
			seenMAC[nic.MACAddress] = true
		}
	}
	if machine.Spec.Hardware.Boot.NICRef.Name != "" {
		if _, ok := nicsByName[machine.Spec.Hardware.Boot.NICRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.boot.nicRef %q does not resolve to spec.hardware.nics[].name", prefix, machine.Spec.Hardware.Boot.NICRef.Name))
		}
	}
	bmc := machine.Spec.Hardware.Management.BMC
	requireBareMetalBoot := providerOK && provider.Spec.Type == v1alpha1.ProvisionerBareMetal && v1alpha1.MachineRequiresSubstrate(machine)
	if requireBareMetalBoot {
		if len(machine.Spec.Hardware.NICs) == 0 {
			errs = append(errs, prefix+".nics is required for baremetal machines when os.provided=false")
		}
		if machine.Spec.Hardware.Boot.NICRef.Name == "" {
			errs = append(errs, prefix+".boot.nicRef is required for baremetal machines when os.provided=false")
		}
		for i, nic := range machine.Spec.Hardware.NICs {
			if nic.MACAddress == "" {
				errs = append(errs, fmt.Sprintf("%s.nics[%d].macAddress is required for baremetal machines when os.provided=false", prefix, i))
			}
		}
	}
	validateBMC := requireBareMetalBoot || bmc.Address != "" || bmc.Protocol != "" || bmc.CredentialsRef.Name != "" || bmc.TLS != nil || bmc.VirtualMedia != nil
	if validateBMC && bmc.Address == "" {
		errs = append(errs, prefix+".management.bmc.address is required")
	}
	if bmc.Protocol != "" && bmc.Protocol != v1alpha1.DefaultBMCProtocol {
		errs = append(errs, fmt.Sprintf("%s.management.bmc.protocol %q is not supported yet; only %q is implemented", prefix, bmc.Protocol, v1alpha1.DefaultBMCProtocol))
	}
	if validateBMC && bmc.CredentialsRef.Name == "" {
		errs = append(errs, prefix+".management.bmc.credentialsRef is required")
	}
	if vm := bmc.VirtualMedia; vm != nil && vm.TLS != nil {
		errs = append(errs, validateBMCVirtualMediaTLS(prefix+".management.bmc.virtualMedia.tls", vm.TLS)...)
	}
	return errs
}

func validateBMCVirtualMediaTLS(prefix string, tls *v1alpha1.BMCVirtualMediaTLS) []string {
	var errs []string
	switch tls.Trust {
	case "", v1alpha1.BMCVirtualMediaTrustDisableVerification, v1alpha1.BMCVirtualMediaTrustImportCertificate, v1alpha1.BMCVirtualMediaTrustEstablished:
	default:
		errs = append(errs, fmt.Sprintf("%s.trust %q is not supported; use %q, %q or %q",
			prefix, tls.Trust,
			v1alpha1.BMCVirtualMediaTrustDisableVerification,
			v1alpha1.BMCVirtualMediaTrustImportCertificate,
			v1alpha1.BMCVirtualMediaTrustEstablished))
		return errs
	}
	if tls.RestoreVerificationAfterBoot != nil && tls.TrustMode() != v1alpha1.BMCVirtualMediaTrustDisableVerification {
		errs = append(errs, fmt.Sprintf("%s.restoreVerificationAfterBoot is only valid with trust %q", prefix, v1alpha1.BMCVirtualMediaTrustDisableVerification))
	}
	if tls.RemoveCertificateAfterBoot && tls.TrustMode() != v1alpha1.BMCVirtualMediaTrustImportCertificate {
		errs = append(errs, fmt.Sprintf("%s.removeCertificateAfterBoot is only valid with trust %q", prefix, v1alpha1.BMCVirtualMediaTrustImportCertificate))
	}
	if tls.Trust == "" && tls.RestoreVerificationAfterBoot == nil && !tls.RemoveCertificateAfterBoot {
		errs = append(errs, prefix+" sets no option; set trust (disable-verification, import-certificate or established)")
	}
	return errs
}

func validateMachineNetwork(prefix string, machine v1alpha1.Machine, networks map[string]v1alpha1.NetworkConfig, provider v1alpha1.InfraProvider) []string {
	var errs []string
	config := machine.Spec.Network.Config
	if config.NetworkConfigRef.Name != "" && config.Spec != nil {
		errs = append(errs, prefix+".config must set only one of networkConfigRef or spec")
	}
	if len(config.Overrides) > 0 && config.NetworkConfigRef.Name == "" {
		errs = append(errs, prefix+".config.overrides is only valid with "+prefix+".config.networkConfigRef")
	}
	if config.NetworkConfigRef.Name != "" && machine.Spec.Substrate.ProviderRef.Name != "" && config.AttachmentRef.Name == "" {
		errs = append(errs, prefix+".config.attachmentRef is required when networkConfigRef is set on a provider-backed Machine")
	}
	if machine.DefaultedRefs.AttachmentRef {
		if candidates := providerNetworkAttachmentNames(provider); len(candidates) > 1 {
			errs = append(errs, fmt.Sprintf("%s.config.attachmentRef was defaulted from networkConfigRef %q, but InfraProvider/%s declares multiple networkAttachments {%s}; author attachmentRef to pick one",
				prefix, config.NetworkConfigRef.Name, provider.Metadata.Name, strings.Join(candidates, ", ")))
		}
	}
	errs = append(errs, validateMachineInterfaceAddresses(prefix+".config.interfaceAddresses", machine, config)...)
	errs = append(errs, validateInterfaceAddressesResolveInterface(prefix+".config.interfaceAddresses", config, machineNetworkTemplateInterfaceNames(config, networks))...)
	errs = append(errs, validateMachineInstallIPInNetwork(prefix+".config.interfaceAddresses", machine, config, networks)...)
	injects := machineConfigInterfaceAddresses(machine, config)
	var effective map[string]any
	if config.NetworkConfigRef.Name != "" {
		if n, ok := networks[config.NetworkConfigRef.Name]; ok {
			errs = append(errs, nmstate.ShapeErrors(prefix+".config.overrides", n.Spec.Template.NetworkConfig, config.Overrides)...)
			effective = nmstate.EffectiveConfig(n.Spec.Template.NetworkConfig, config.Overrides, injects)
		} else {
			errs = append(errs, fmt.Sprintf("%s.config.networkConfigRef %q does not match any NetworkConfig", prefix, config.NetworkConfigRef.Name))
		}
	}
	if config.Spec != nil {
		errs = append(errs, validateNetworkConfigSpec(prefix+".config.spec", *config.Spec, map[string]bool{})...)
		effective = nmstate.EffectiveConfig(config.Spec.Template.NetworkConfig, nil, injects)
	}
	errs = append(errs, validateMachineInterfaceBindings(prefix+".interfaceBinding", machine, networkInterfaceNames(effective), provider)...)
	if effective != nil {
		errs = append(errs, validateMachineNetworkStaticAddresses(prefix+".config", machine, effective)...)
	}
	return errs
}

func validateMachineInterfaceAddresses(prefix string, machine v1alpha1.Machine, config v1alpha1.MachineNetworkConfig) []string {
	if len(config.InterfaceAddresses) == 0 {
		return nil
	}
	var errs []string
	if config.NetworkConfigRef.Name == "" && config.Spec == nil {
		errs = append(errs, prefix+" is only valid with config.networkConfigRef or config.spec")
	}
	addrByName := map[string]string{}
	for _, address := range machine.Spec.Addresses {
		if address.Name != "" {
			addrByName[address.Name] = address.Address
		}
	}
	seen := map[string]bool{}
	for i, ia := range config.InterfaceAddresses {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if ia.Interface == "" {
			errs = append(errs, owner+".interface is required")
		} else {
			key := ia.Interface + "/" + ia.Family
			if seen[key] {
				errs = append(errs, fmt.Sprintf("%s.interface %q is duplicated for the same address family", owner, ia.Interface))
			}
			seen[key] = true
			if nmstate.InterfaceHasStaticIP(config.Overrides, ia.Interface) {
				errs = append(errs, fmt.Sprintf("%s.interface %q install IP is owned by interfaceAddresses; remove the static address from config.overrides", owner, ia.Interface))
			}
		}
		switch ia.Family {
		case "", "ipv4", "ipv6":
		default:
			errs = append(errs, fmt.Sprintf("%s.family %q must be ipv4 or ipv6", owner, ia.Family))
		}
		if ia.AddressRef.Name == "" {
			errs = append(errs, owner+".addressRef is required")
		} else if address, ok := addrByName[ia.AddressRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.addressRef %q does not resolve to spec.addresses[].name", owner, ia.AddressRef.Name))
		} else if ip := net.ParseIP(strings.TrimSpace(address)); ip != nil {
			if wantIPv4 := ia.Family == "" || ia.Family == "ipv4"; (ip.To4() != nil) != wantIPv4 {
				errs = append(errs, fmt.Sprintf("%s.addressRef %q resolves to %s, which is not an %s address; set family to match the literal", owner, ia.AddressRef.Name, strings.TrimSpace(address), interfaceAddressFamilyLabel(ia.Family)))
			}
		}
		switch {
		case ia.PrefixLength < 1 || ia.PrefixLength > 128:
			errs = append(errs, fmt.Sprintf("%s.prefixLength %d out of range", owner, ia.PrefixLength))
		case (ia.Family == "" || ia.Family == "ipv4") && ia.PrefixLength > 32:
			errs = append(errs, fmt.Sprintf("%s.prefixLength %d out of IPv4 range", owner, ia.PrefixLength))
		}
	}
	return errs
}

func validateInterfaceAddressesResolveInterface(prefix string, config v1alpha1.MachineNetworkConfig, templateInterfaces map[string]bool) []string {
	if len(config.InterfaceAddresses) == 0 || templateInterfaces == nil {
		return nil
	}
	var errs []string
	for i, ia := range config.InterfaceAddresses {
		if ia.Interface == "" || templateInterfaces[ia.Interface] {
			continue
		}
		errs = append(errs, fmt.Sprintf("%s[%d].interface %q does not match any interface in the effective NetworkConfig; interfaceAddresses can only assign an install IP to an interface the template declares", prefix, i, ia.Interface))
	}
	return errs
}

func interfaceAddressFamilyLabel(family string) string {
	if family == "ipv6" {
		return "ipv6"
	}
	return "ipv4"
}

func machineNetworkTemplateInterfaceNames(config v1alpha1.MachineNetworkConfig, networks map[string]v1alpha1.NetworkConfig) map[string]bool {
	var template map[string]any
	switch {
	case config.NetworkConfigRef.Name != "":
		n, ok := networks[config.NetworkConfigRef.Name]
		if !ok {
			return nil
		}
		template = nmstate.EffectiveConfig(n.Spec.Template.NetworkConfig, config.Overrides, nil)
	case config.Spec != nil:
		template = nmstate.EffectiveConfig(config.Spec.Template.NetworkConfig, nil, nil)
	default:
		return nil
	}
	names := map[string]bool{}
	raw, _ := template["interfaces"].([]any)
	for _, item := range raw {
		iface, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := iface["name"].(string); name != "" {
			names[name] = true
		}
	}
	return names
}

func macInVSphereManualRange(mac string) bool {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return false
	}
	return hw[0] == 0x00 && hw[1] == 0x50 && hw[2] == 0x56 && hw[3] <= 0x3f
}

func validateMachineInstallIPInNetwork(prefix string, machine v1alpha1.Machine, config v1alpha1.MachineNetworkConfig, networks map[string]v1alpha1.NetworkConfig) []string {
	cidrs := selectedMachineNetworkCIDRs(config, networks)
	if len(cidrs) == 0 {
		return nil
	}
	var errs []string
	for i, ia := range config.InterfaceAddresses {
		if ia.AddressRef.Name == "" {
			continue
		}
		address, ok := v1alpha1.MachineAddressByName(machine, ia.AddressRef.Name)
		if !ok || address == "" {
			continue
		}
		if !addressInAnyCIDR(cidrs, address) {
			errs = append(errs, fmt.Sprintf("%s[%d].addressRef %q resolves to %s, which is outside the selected NetworkConfig machine networks {%s}",
				prefix, i, ia.AddressRef.Name, address, strings.Join(cidrs, ", ")))
		}
	}
	return errs
}

func validateMachineInterfaceBindings(prefix string, machine v1alpha1.Machine, interfaceNames []string, provider v1alpha1.InfraProvider) []string {
	var errs []string
	nics := map[string]bool{}
	for _, nic := range machine.Spec.Hardware.NICs {
		if nic.Name != "" {
			nics[nic.Name] = true
		}
	}
	wantInterfaces := map[string]bool{}
	for _, name := range interfaceNames {
		wantInterfaces[name] = true
	}
	boundInterfaces := map[string]bool{}
	boundNICs := map[string]bool{}
	for i, binding := range machine.Spec.Network.InterfaceBinding {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if binding.NICRef.Name == "" {
			errs = append(errs, owner+".nicRef is required")
		} else if !nics[binding.NICRef.Name] {
			errs = append(errs, fmt.Sprintf("%s.nicRef %q does not resolve to spec.hardware.nics[].name", owner, binding.NICRef.Name))
		} else if boundNICs[binding.NICRef.Name] {
			errs = append(errs, fmt.Sprintf("%s.nicRef %q is duplicated", owner, binding.NICRef.Name))
		}
		boundNICs[binding.NICRef.Name] = true
		if binding.InterfaceName == "" {
			errs = append(errs, owner+".interfaceName is required")
		} else if boundInterfaces[binding.InterfaceName] {
			errs = append(errs, fmt.Sprintf("%s.interfaceName %q is duplicated", owner, binding.InterfaceName))
		} else if len(wantInterfaces) > 0 && !wantInterfaces[binding.InterfaceName] {
			errs = append(errs, fmt.Sprintf("%s.interfaceName %q does not match any interface in the effective NetworkConfig", owner, binding.InterfaceName))
		}
		boundInterfaces[binding.InterfaceName] = true
	}
	if provider.Spec.Type == v1alpha1.ProvisionerBareMetal && v1alpha1.MachineRequiresSubstrate(machine) && len(wantInterfaces) > 0 {
		for _, name := range interfaceNames {
			if !boundInterfaces[name] {
				errs = append(errs, fmt.Sprintf("%s must bind NetworkConfig interface %q to a hardware NIC for baremetal machines", prefix, name))
			}
		}
	}
	return errs
}

func validateMachineNetworkStaticAddresses(prefix string, machine v1alpha1.Machine, config map[string]any) []string {
	known := map[string]bool{}
	for _, address := range machine.Spec.Addresses {
		if address.Address != "" {
			known[address.Address] = true
		}
	}
	var errs []string
	for _, ip := range networkConfigStaticIPs(config) {
		if !known[ip] {
			errs = append(errs, fmt.Sprintf("%s static IP %q does not match any spec.addresses[].address", prefix, ip))
		}
	}
	return errs
}

func networkConfigStaticIPs(config map[string]any) []string {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, family := range []string{"ipv4", "ipv6"} {
			familyConfig, ok := entry[family].(map[string]any)
			if !ok {
				continue
			}
			rawAddresses, ok := familyConfig["address"].([]any)
			if !ok {
				continue
			}
			for _, raw := range rawAddresses {
				address, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				ip, _ := address["ip"].(string)
				if ip != "" {
					seen[ip] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for ip := range seen {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

func machineConfigInterfaceAddresses(machine v1alpha1.Machine, config v1alpha1.MachineNetworkConfig) []nmstate.InterfaceAddress {
	if len(config.InterfaceAddresses) == 0 {
		return nil
	}
	addresses := map[string]string{}
	for _, address := range machine.Spec.Addresses {
		if address.Name != "" {
			addresses[address.Name] = address.Address
		}
	}
	var out []nmstate.InterfaceAddress
	for _, ia := range config.InterfaceAddresses {
		ip := addresses[ia.AddressRef.Name]
		if ip == "" || ia.Interface == "" {
			continue
		}
		out = append(out, nmstate.InterfaceAddress{
			Interface:    ia.Interface,
			Family:       ia.Family,
			IP:           ip,
			PrefixLength: ia.PrefixLength,
		})
	}
	return out
}
