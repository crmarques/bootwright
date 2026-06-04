package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/media"
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
	seen := map[string]bool{}
	for _, machine := range state.Machines {
		if e := validateName(v1alpha1.KindMachine, machine.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[machine.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate Machine %q", machine.Metadata.Name))
		}
		seen[machine.Metadata.Name] = true
		prefix := fmt.Sprintf("Machine/%s spec", machine.Metadata.Name)
		errs = append(errs, validateMachineLabels(prefix, machine.Metadata.Labels)...)
		errs = append(errs, validateMachineCapabilitySet(prefix+".capabilities", machine.Spec.Capabilities)...)
		errs = append(errs, validateMachineOS(prefix+".os", machine, networks, installProfiles)...)
		errs = append(errs, validateMachineSubstrate(prefix+".substrate", machine, providers)...)
	}
	errs = append(errs, validateMachineImages(state)...)
	errs = append(errs, validateMachineInstallProfiles(state)...)
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

func validateMachineOS(prefix string, machine v1alpha1.Machine, networks map[string]v1alpha1.NetworkConfig, installProfiles map[string]v1alpha1.MachineInstallProfile) []string {
	var errs []string
	switch machine.Spec.OS.Mode {
	case v1alpha1.MachineOSModeRaw:
		if machine.Spec.OS.Install.ProfileRef.Name != "" {
			errs = append(errs, prefix+".install.profileRef.name must be empty when mode=raw")
		}
	case v1alpha1.MachineOSModeExternal:
		if machine.Spec.OS.Install.ProfileRef.Name != "" {
			errs = append(errs, prefix+".install.profileRef.name must be empty when mode=external")
		}
		errs = append(errs, validateMachineSSH(prefix, machine)...)
	case v1alpha1.MachineOSModeManaged:
		if machine.Spec.OS.Install.ProfileRef.Name == "" {
			errs = append(errs, prefix+".install.profileRef.name is required when mode=managed")
		} else if _, ok := installProfiles[machine.Spec.OS.Install.ProfileRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.install.profileRef.name %q does not match any MachineInstallProfile", prefix, machine.Spec.OS.Install.ProfileRef.Name))
		}
		errs = append(errs, validateMachineSSH(prefix, machine)...)
	case "":
		errs = append(errs, prefix+".mode is required")
	default:
		errs = append(errs, fmt.Sprintf("%s.mode %q must be one of {%s, %s, %s}",
			prefix, machine.Spec.OS.Mode, v1alpha1.MachineOSModeRaw, v1alpha1.MachineOSModeExternal, v1alpha1.MachineOSModeManaged))
	}
	errs = append(errs, validateMachineCapabilitySet(prefix+".capabilities", machine.Spec.OS.Capabilities)...)
	errs = append(errs, validateMachineAddresses(prefix, machine)...)
	errs = append(errs, validateMachineNetwork(prefix+".install.network", machine, machine.Spec.OS.Install.Network, networks)...)
	return errs
}

func validateMachineSSH(prefix string, machine v1alpha1.Machine) []string {
	if machine.Spec.OS.SSH == nil {
		return []string{prefix + ".ssh is required when mode is external or managed"}
	}
	var errs []string
	ssh := machine.Spec.OS.SSH
	if ssh.AddressName == "" {
		errs = append(errs, prefix+".ssh.addressName is required")
	} else if _, ok := v1alpha1.MachineAddressByName(machine, ssh.AddressName); !ok {
		errs = append(errs, fmt.Sprintf("%s.ssh.addressName %q does not resolve to spec.os.addresses[].name", prefix, ssh.AddressName))
	}
	if ssh.KeyRef.Name == "" {
		errs = append(errs, prefix+".ssh.keyRef.name is required")
	}
	return errs
}

func validateMachineAddresses(prefix string, machine v1alpha1.Machine) []string {
	var errs []string
	seen := map[string]bool{}
	for i, address := range machine.Spec.OS.Addresses {
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
	}
	return errs
}

func validateMachineNetwork(prefix string, machine v1alpha1.Machine, network v1alpha1.MachineNetwork, networks map[string]v1alpha1.NetworkConfig) []string {
	var errs []string
	if network.NetworkConfigRef.Name != "" && network.Spec != nil {
		errs = append(errs, prefix+" must set only one of networkConfigRef or spec")
	}
	if len(network.Overrides) > 0 && network.NetworkConfigRef.Name == "" {
		errs = append(errs, prefix+".overrides is only valid with "+prefix+".networkConfigRef")
	}
	if network.NetworkConfigRef.Name != "" && machine.Spec.Substrate.ProviderRef.Name != "" && network.AttachmentRef.Name == "" {
		errs = append(errs, prefix+".attachmentRef.name is required when networkConfigRef is set on a provider-backed Machine")
	}
	if network.NetworkConfigRef.Name != "" {
		if n, ok := networks[network.NetworkConfigRef.Name]; ok {
			errs = append(errs, validateMachineBareMetalNetworkInterfaces(prefix+".networkConfigRef", machine, network.NetworkConfigRef.Name, n.Spec.Template.NetworkConfig, network.Overrides)...)
		} else {
			errs = append(errs, fmt.Sprintf("%s.networkConfigRef.name %q does not match any NetworkConfig", prefix, network.NetworkConfigRef.Name))
		}
	}
	if network.Spec != nil {
		errs = append(errs, validateNetworkConfigSpec(prefix+".spec", *network.Spec, map[string]bool{})...)
		errs = append(errs, validateMachineBareMetalNetworkInterfaces(prefix+".spec", machine, "inline", network.Spec.Template.NetworkConfig, nil)...)
	}
	return errs
}

func validateMachineBareMetalNetworkInterfaces(prefix string, machine v1alpha1.Machine, source string, template, overrides map[string]any) []string {
	bareMetal := machine.Spec.Substrate.BareMetal
	if bareMetal == nil {
		return nil
	}
	declared := map[string]bool{}
	for _, iface := range bareMetal.Interfaces {
		if iface.Name != "" {
			declared[iface.Name] = true
		}
	}
	want := append(templateBareMetalInterfaceNames(v1alpha1.NetworkConfig{
		Spec: v1alpha1.NetworkConfigSpec{
			Template: v1alpha1.NetworkConfigTemplate{
				NetworkConfig: template,
			},
		},
	}), overrideBareMetalInterfaceNames(overrides)...)
	var errs []string
	seen := map[string]bool{}
	for _, name := range want {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if !declared[name] {
			errs = append(errs, fmt.Sprintf("%s %q requires bareMetal interface %q but Machine/%s spec.substrate.bareMetal.interfaces does not declare it", prefix, source, name, machine.Metadata.Name))
		}
	}
	return errs
}

func validateMachineSubstrate(prefix string, machine v1alpha1.Machine, providers map[string]v1alpha1.InfraProvider) []string {
	var errs []string
	set := 0
	if machine.Spec.Substrate.BareMetal != nil {
		set++
	}
	if machine.Spec.Substrate.Libvirt != nil {
		set++
	}
	if machine.Spec.Substrate.VSphere != nil {
		set++
	}
	if machine.Spec.Substrate.KubeVirt != nil {
		set++
	}
	if set == 0 && machine.Spec.OS.Mode == v1alpha1.MachineOSModeExternal && machine.Spec.Substrate.ProviderRef.Name == "" {
		return errs
	}
	if set != 1 {
		errs = append(errs, fmt.Sprintf("%s must set exactly one of {bareMetal, libvirt, vsphere, kubevirt} (got %d)", prefix, set))
	}
	provider, providerOK := providers[machine.Spec.Substrate.ProviderRef.Name]
	if set > 0 && machine.Spec.Substrate.ProviderRef.Name == "" {
		errs = append(errs, prefix+".providerRef.name is required")
	} else if !providerOK {
		errs = append(errs, fmt.Sprintf("%s.providerRef.name %q does not match any InfraProvider", prefix, machine.Spec.Substrate.ProviderRef.Name))
	}
	switch {
	case machine.Spec.Substrate.BareMetal != nil:
		if providerOK && provider.Spec.Type != v1alpha1.ProvisionerBareMetal {
			errs = append(errs, fmt.Sprintf("%s.providerRef.name %q must reference type=%s for bareMetal machines", prefix, provider.Metadata.Name, v1alpha1.ProvisionerBareMetal))
		}
		errs = append(errs, validateMachineBareMetal(prefix+".bareMetal", machine.Spec.Substrate.BareMetal, machine.Spec.OS.Mode == v1alpha1.MachineOSModeRaw)...)
	case machine.Spec.Substrate.Libvirt != nil:
		errs = append(errs, validateProfiledMachineSubstrate(prefix+".libvirt", provider, providerOK, machine.Spec.Substrate.Libvirt, v1alpha1.ProvisionerLibvirt)...)
	case machine.Spec.Substrate.VSphere != nil:
		errs = append(errs, validateProfiledMachineSubstrate(prefix+".vsphere", provider, providerOK, machine.Spec.Substrate.VSphere, v1alpha1.ProvisionerVSphere)...)
	case machine.Spec.Substrate.KubeVirt != nil:
		errs = append(errs, validateProfiledMachineSubstrate(prefix+".kubevirt", provider, providerOK, machine.Spec.Substrate.KubeVirt, v1alpha1.ProvisionerKubeVirt)...)
	}
	return errs
}

func validateProfiledMachineSubstrate(prefix string, provider v1alpha1.InfraProvider, providerOK bool, substrate *v1alpha1.MachineProfiledSubstrate, wantType string) []string {
	var errs []string
	if providerOK && provider.Spec.Type != wantType {
		errs = append(errs, fmt.Sprintf("%s provider type %q must be %q", prefix, provider.Spec.Type, wantType))
	}
	if substrate.ProfileRef.Name == "" {
		errs = append(errs, prefix+".profileRef.name is required")
	} else if providerOK {
		if _, ok := lookupMachineProfile(provider, substrate.ProfileRef.Name); !ok {
			errs = append(errs, fmt.Sprintf("%s.profileRef.name %q does not match any profile on InfraProvider/%s", prefix, substrate.ProfileRef.Name, provider.Metadata.Name))
		}
	}
	return errs
}

func validateMachineBareMetal(prefix string, b *v1alpha1.MachineBareMetalSubstrate, requireBoot bool) []string {
	var errs []string
	validateBMC := requireBoot || b.BMC.Address != "" || b.BMC.Protocol != "" || b.BMC.CredentialsRef.Name != "" || b.BMC.DisableCertificateVerification
	if validateBMC && b.BMC.Address == "" {
		errs = append(errs, prefix+".bmc.address is required")
	}
	if b.BMC.Protocol != "" && b.BMC.Protocol != v1alpha1.DefaultBMCProtocol {
		errs = append(errs, fmt.Sprintf("%s.bmc.protocol %q is not supported yet; only %q is implemented", prefix, b.BMC.Protocol, v1alpha1.DefaultBMCProtocol))
	}
	if validateBMC && b.BMC.CredentialsRef.Name == "" {
		errs = append(errs, prefix+".bmc.credentialsRef.name is required")
	}
	if len(b.Interfaces) == 0 {
		errs = append(errs, prefix+".interfaces is required (at least one)")
		return errs
	}
	seen := map[string]bool{}
	bootMACKnown := b.BootMACAddress == ""
	for i, iface := range b.Interfaces {
		owner := fmt.Sprintf("%s.interfaces[%d]", prefix, i)
		if iface.Name == "" {
			errs = append(errs, owner+".name is required")
		} else if seen[iface.Name] {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, iface.Name))
		}
		seen[iface.Name] = true
		if iface.MACAddress == "" {
			errs = append(errs, owner+".macAddress is required")
		} else if !looksLikeMAC(iface.MACAddress) {
			errs = append(errs, fmt.Sprintf("%s.macAddress %q is not a valid MAC address", owner, iface.MACAddress))
		}
		if strings.EqualFold(iface.MACAddress, b.BootMACAddress) {
			bootMACKnown = true
		}
	}
	if requireBoot && b.BootMACAddress == "" {
		errs = append(errs, prefix+".bootMACAddress is required for raw bare-metal machines")
	} else if b.BootMACAddress != "" && !looksLikeMAC(b.BootMACAddress) {
		errs = append(errs, fmt.Sprintf("%s.bootMACAddress %q is not a valid MAC address", prefix, b.BootMACAddress))
	} else if !bootMACKnown {
		errs = append(errs, fmt.Sprintf("%s.bootMACAddress %q does not match any interfaces[].macAddress", prefix, b.BootMACAddress))
	}
	return errs
}

func validateMachineImages(state v1alpha1.State) []string {
	var errs []string
	seen := map[string]bool{}
	for _, image := range state.MachineImages {
		if e := validateName(v1alpha1.KindMachineImage, image.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[image.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate MachineImage %q", image.Metadata.Name))
		}
		seen[image.Metadata.Name] = true
		prefix := fmt.Sprintf("MachineImage/%s spec", image.Metadata.Name)
		if image.Spec.Type != v1alpha1.MachineImageTypeISO {
			errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, image.Spec.Type, v1alpha1.MachineImageTypeISO))
		}
		if image.Spec.URL == "" {
			errs = append(errs, prefix+".url is required")
		} else if err := media.ValidateISOReference(image.Spec.URL); err != nil {
			errs = append(errs, fmt.Sprintf("%s.url %s", prefix, err))
		}
		if _, err := media.NormalizeSHA256(image.Spec.Checksum); err != nil {
			errs = append(errs, fmt.Sprintf("%s.checksum %s", prefix, err))
		}
	}
	return errs
}

func validateMachineInstallProfiles(state v1alpha1.State) []string {
	var errs []string
	images := indexMachineImages(state.MachineImages)
	seen := map[string]bool{}
	for _, profile := range state.MachineInstallProfiles {
		if e := validateName(v1alpha1.KindMachineInstallProfile, profile.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[profile.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate MachineInstallProfile %q", profile.Metadata.Name))
		}
		seen[profile.Metadata.Name] = true
		prefix := fmt.Sprintf("MachineInstallProfile/%s spec", profile.Metadata.Name)
		if profile.Spec.OS.Family == "" {
			errs = append(errs, prefix+".os.family is required")
		}
		if profile.Spec.OS.Version == "" {
			errs = append(errs, prefix+".os.version is required")
		}
		if profile.Spec.OS.Architecture == "" {
			errs = append(errs, prefix+".os.architecture is required")
		}
		if profile.Spec.Installer.Type != v1alpha1.MachineInstallProfileTypeAnaconda {
			errs = append(errs, fmt.Sprintf("%s.installer.type %q must be %q", prefix, profile.Spec.Installer.Type, v1alpha1.MachineInstallProfileTypeAnaconda))
		}
		if profile.Spec.Installer.Anaconda == nil {
			errs = append(errs, prefix+".installer.anaconda is required")
			continue
		}
		imageRef := profile.Spec.Installer.Anaconda.ImageRef.Name
		if imageRef == "" {
			errs = append(errs, prefix+".installer.anaconda.imageRef.name is required")
		} else if _, ok := images[imageRef]; !ok {
			errs = append(errs, fmt.Sprintf("%s.installer.anaconda.imageRef.name %q does not match any MachineImage", prefix, imageRef))
		}
	}
	return errs
}
