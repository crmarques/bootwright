package desiredstate

import (
	"fmt"
	"sort"
	"strings"

	"dario.cat/mergo"
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

func validateMachineOS(prefix string, machine v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile) []string {
	var errs []string
	if machine.Spec.OS.Provided == nil {
		errs = append(errs, prefix+".provided is required")
		return errs
	}
	if *machine.Spec.OS.Provided {
		if machine.Spec.OS.ProfileRef.Name != "" {
			errs = append(errs, prefix+".profileRef.name must be empty when provided=true")
		}
		if !machine.Spec.OS.Install.IsZero() {
			errs = append(errs, prefix+".install must be empty when provided=true")
		}
		if !machine.Spec.Network.Config.IsZero() {
			errs = append(errs, "Machine/"+machine.Metadata.Name+" spec.network.config must be empty when os.provided=true")
		}
		return errs
	}
	if machine.Spec.OS.ProfileRef.Name != "" {
		if _, ok := installProfiles[machine.Spec.OS.ProfileRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.profileRef.name %q does not match any MachineInstallProfile", prefix, machine.Spec.OS.ProfileRef.Name))
		}
	}
	return errs
}

func validateMachineAccess(prefix string, machine v1alpha1.Machine) []string {
	if machine.Spec.OS.Provided == nil {
		if machine.Spec.Access.SSH == nil {
			return nil
		}
		return validateMachineSSH(prefix, machine)
	}
	if *machine.Spec.OS.Provided || machine.Spec.OS.ProfileRef.Name != "" {
		if machine.Spec.Access.SSH == nil {
			return []string{prefix + ".ssh is required when os.provided=true or os.profileRef.name is set"}
		}
	}
	if machine.Spec.Access.SSH == nil {
		return nil
	}
	return validateMachineSSH(prefix, machine)
}

func validateMachineSSH(prefix string, machine v1alpha1.Machine) []string {
	var errs []string
	ssh := machine.Spec.Access.SSH
	if ssh.AddressRef.Name == "" {
		errs = append(errs, prefix+".ssh.addressRef.name is required")
	} else if _, ok := v1alpha1.MachineAddressByName(machine, ssh.AddressRef.Name); !ok {
		errs = append(errs, fmt.Sprintf("%s.ssh.addressRef.name %q does not resolve to spec.addresses[].name", prefix, ssh.AddressRef.Name))
	}
	if ssh.KeyRef.Name == "" {
		errs = append(errs, prefix+".ssh.keyRef.name is required")
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
	}
	return errs
}

func validateMachineSubstrate(prefix string, machine v1alpha1.Machine, provider v1alpha1.InfraProvider, providerOK bool) []string {
	var errs []string
	providerName := machine.Spec.Substrate.ProviderRef.Name
	if v1alpha1.MachineRequiresSubstrate(machine) && providerName == "" {
		errs = append(errs, prefix+".providerRef.name is required when os.provided=false")
		return errs
	}
	if providerName == "" {
		if machine.Spec.Substrate.ProfileRef.Name != "" {
			errs = append(errs, prefix+".profileRef.name requires providerRef.name")
		}
		return errs
	}
	if !providerOK {
		errs = append(errs, fmt.Sprintf("%s.providerRef.name %q does not match any InfraProvider", prefix, providerName))
		return errs
	}
	switch provider.Spec.Type {
	case v1alpha1.ProvisionerBareMetal:
		if machine.Spec.Substrate.ProfileRef.Name != "" {
			errs = append(errs, prefix+".profileRef.name must be empty for baremetal providers")
		}
	case v1alpha1.ProvisionerLibvirt, v1alpha1.ProvisionerVSphere, v1alpha1.ProvisionerKubeVirt:
		if v1alpha1.MachineRequiresSubstrate(machine) && machine.Spec.Substrate.ProfileRef.Name == "" {
			errs = append(errs, prefix+".profileRef.name is required for "+provider.Spec.Type+" providers when os.provided=false")
		}
		if machine.Spec.Substrate.ProfileRef.Name != "" {
			if _, ok := lookupMachineProfile(provider, machine.Spec.Substrate.ProfileRef.Name); !ok {
				errs = append(errs, fmt.Sprintf("%s.profileRef.name %q does not match any profile on InfraProvider/%s", prefix, machine.Spec.Substrate.ProfileRef.Name, provider.Metadata.Name))
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
		if nic.MACAddress != "" {
			if seenMAC[nic.MACAddress] {
				errs = append(errs, fmt.Sprintf("%s.macAddress %q is duplicated", owner, nic.MACAddress))
			}
			seenMAC[nic.MACAddress] = true
		}
	}
	if machine.Spec.Hardware.Boot.NICRef.Name != "" {
		if _, ok := nicsByName[machine.Spec.Hardware.Boot.NICRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.boot.nicRef.name %q does not resolve to spec.hardware.nics[].name", prefix, machine.Spec.Hardware.Boot.NICRef.Name))
		}
	}
	bmc := machine.Spec.Hardware.Management.BMC
	requireBareMetalBoot := providerOK && provider.Spec.Type == v1alpha1.ProvisionerBareMetal && v1alpha1.MachineRequiresSubstrate(machine)
	if requireBareMetalBoot {
		if len(machine.Spec.Hardware.NICs) == 0 {
			errs = append(errs, prefix+".nics is required for baremetal machines when os.provided=false")
		}
		if machine.Spec.Hardware.Boot.NICRef.Name == "" {
			errs = append(errs, prefix+".boot.nicRef.name is required for baremetal machines when os.provided=false")
		}
		for i, nic := range machine.Spec.Hardware.NICs {
			if nic.MACAddress == "" {
				errs = append(errs, fmt.Sprintf("%s.nics[%d].macAddress is required for baremetal machines when os.provided=false", prefix, i))
			}
		}
	}
	validateBMC := requireBareMetalBoot || bmc.Address != "" || bmc.Protocol != "" || bmc.CredentialsRef.Name != "" || bmc.DisableCertificateVerification
	if validateBMC && bmc.Address == "" {
		errs = append(errs, prefix+".management.bmc.address is required")
	}
	if bmc.Protocol != "" && bmc.Protocol != v1alpha1.DefaultBMCProtocol {
		errs = append(errs, fmt.Sprintf("%s.management.bmc.protocol %q is not supported yet; only %q is implemented", prefix, bmc.Protocol, v1alpha1.DefaultBMCProtocol))
	}
	if validateBMC && bmc.CredentialsRef.Name == "" {
		errs = append(errs, prefix+".management.bmc.credentialsRef.name is required")
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
		errs = append(errs, prefix+".config.attachmentRef.name is required when networkConfigRef is set on a provider-backed Machine")
	}
	var effective map[string]any
	if config.NetworkConfigRef.Name != "" {
		if n, ok := networks[config.NetworkConfigRef.Name]; ok {
			errs = append(errs, machineNetworkOverrideShapeErrors(prefix+".config.overrides", n.Spec.Template.NetworkConfig, config.Overrides)...)
			effective = effectiveMachineNetworkConfig(n.Spec.Template.NetworkConfig, config.Overrides)
		} else {
			errs = append(errs, fmt.Sprintf("%s.config.networkConfigRef.name %q does not match any NetworkConfig", prefix, config.NetworkConfigRef.Name))
		}
	}
	if config.Spec != nil {
		errs = append(errs, validateNetworkConfigSpec(prefix+".config.spec", *config.Spec, map[string]bool{})...)
		effective = effectiveMachineNetworkConfig(config.Spec.Template.NetworkConfig, nil)
	}
	errs = append(errs, validateMachineInterfaceBindings(prefix+".interfaceBinding", machine, networkInterfaceNames(effective), provider)...)
	if effective != nil {
		errs = append(errs, validateMachineNetworkStaticAddresses(prefix+".config", machine, effective)...)
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
			errs = append(errs, owner+".nicRef.name is required")
		} else if !nics[binding.NICRef.Name] {
			errs = append(errs, fmt.Sprintf("%s.nicRef.name %q does not resolve to spec.hardware.nics[].name", owner, binding.NICRef.Name))
		} else if boundNICs[binding.NICRef.Name] {
			errs = append(errs, fmt.Sprintf("%s.nicRef.name %q is duplicated", owner, binding.NICRef.Name))
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

func effectiveMachineNetworkConfig(template, overrides map[string]any) map[string]any {
	out := cloneMachineNetworkConfig(template)
	if len(overrides) > 0 {
		mergeMachineNetworkConfigOverrides(out, cloneMachineNetworkConfig(overrides))
	}
	return out
}

// machineNetworkOverrideShapeErrors reports per-machine NMState override list
// shapes that the deterministic template+override merge cannot apply, naming the
// owning field instead of letting the merge silently drop the override. It
// tracks exactly the merge's structured-sequence path: a list override is only
// checked where both the template and the override hold a list at the same key,
// and is rejected unless every override entry is a named map (merge by name) or
// both lists are positional maps (merge by index).
func machineNetworkOverrideShapeErrors(prefix string, base, patch map[string]any) []string {
	if len(patch) == 0 {
		return nil
	}
	keys := make([]string, 0, len(patch))
	for key := range patch {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var errs []string
	for _, key := range keys {
		path := prefix + "." + key
		patchValue := patch[key]
		if patchMap, ok := patchValue.(map[string]any); ok {
			if baseMap, ok := base[key].(map[string]any); ok {
				errs = append(errs, machineNetworkOverrideShapeErrors(path, baseMap, patchMap)...)
			}
			continue
		}
		patchSlice, patchIsSlice := patchValue.([]any)
		baseSlice, baseIsSlice := base[key].([]any)
		if !patchIsSlice || !baseIsSlice {
			continue
		}
		errs = append(errs, machineNetworkOverrideSequenceShapeErrors(path, baseSlice, patchSlice)...)
	}
	return errs
}

func machineNetworkOverrideSequenceShapeErrors(path string, base, patch []any) []string {
	if len(patch) == 0 {
		return nil
	}
	if sequenceUsesName(patch) {
		index := map[string]map[string]any{}
		for _, item := range base {
			if entry, ok := item.(map[string]any); ok {
				if name, _ := entry["name"].(string); name != "" {
					index[name] = entry
				}
			}
		}
		var errs []string
		for _, item := range patch {
			entry := item.(map[string]any)
			name, _ := entry["name"].(string)
			baseEntry := index[name]
			if baseEntry == nil {
				baseEntry = map[string]any{}
			}
			errs = append(errs, machineNetworkOverrideShapeErrors(path+"["+name+"]", baseEntry, entry)...)
		}
		return errs
	}
	if sequenceUsesMaps(base) && sequenceUsesMaps(patch) {
		var errs []string
		for i, item := range patch {
			entry := item.(map[string]any)
			baseEntry := map[string]any{}
			if i < len(base) {
				if existing, ok := base[i].(map[string]any); ok {
					baseEntry = existing
				}
			}
			errs = append(errs, machineNetworkOverrideShapeErrors(fmt.Sprintf("%s[%d]", path, i), baseEntry, entry)...)
		}
		return errs
	}
	return []string{path + " override list cannot be merged into the NetworkConfig template; entries must be all named maps or a positional list of maps, not scalars or mixed named and unnamed entries"}
}

func mergeMachineNetworkConfigOverrides(base map[string]any, patch map[string]any) {
	for key, patchValue := range patch {
		baseMap, baseIsMap := base[key].(map[string]any)
		patchMap, patchIsMap := patchValue.(map[string]any)
		if baseIsMap && patchIsMap {
			mergeMachineNetworkConfigOverrides(baseMap, patchMap)
			continue
		}
		baseSlice, baseIsSlice := base[key].([]any)
		patchSlice, patchIsSlice := patchValue.([]any)
		if !baseIsSlice || !patchIsSlice {
			continue
		}
		merged, ok := mergeMachineNetworkConfigSequence(baseSlice, patchSlice)
		if !ok {
			continue
		}
		base[key] = merged
		delete(patch, key)
	}
	_ = mergo.Merge(&base, patch, mergo.WithOverride)
}

func mergeMachineNetworkConfigSequence(base []any, patch []any) ([]any, bool) {
	if len(patch) == 0 {
		return cloneMachineNetworkConfigValue(base).([]any), true
	}
	if sequenceUsesName(patch) {
		return mergeMachineNetworkConfigNamedSequence(base, patch), true
	}
	if sequenceUsesMaps(base) && sequenceUsesMaps(patch) {
		return mergeMachineNetworkConfigPositionalSequence(base, patch), true
	}
	return nil, false
}

func mergeMachineNetworkConfigNamedSequence(base []any, patch []any) []any {
	out := cloneMachineNetworkConfigValue(base).([]any)
	index := map[string]map[string]any{}
	for _, item := range out {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name != "" {
			index[name] = entry
		}
	}
	for _, item := range patch {
		entry := item.(map[string]any)
		name := entry["name"].(string)
		if baseEntry, ok := index[name]; ok {
			mergeMachineNetworkConfigOverrides(baseEntry, entry)
			continue
		}
		out = append(out, cloneMachineNetworkConfig(entry))
	}
	return out
}

func mergeMachineNetworkConfigPositionalSequence(base []any, patch []any) []any {
	out := cloneMachineNetworkConfigValue(base).([]any)
	for i, item := range patch {
		entry := item.(map[string]any)
		if i < len(out) {
			baseEntry, ok := out[i].(map[string]any)
			if ok {
				mergeMachineNetworkConfigOverrides(baseEntry, entry)
				continue
			}
		}
		out = append(out, cloneMachineNetworkConfig(entry))
	}
	return out
}

func sequenceUsesName(items []any) bool {
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return false
		}
		name, _ := entry["name"].(string)
		if name == "" {
			return false
		}
	}
	return true
}

func sequenceUsesMaps(items []any) bool {
	for _, item := range items {
		if _, ok := item.(map[string]any); !ok {
			return false
		}
	}
	return true
}

func cloneMachineNetworkConfig(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneMachineNetworkConfigValue(value)
	}
	return out
}

func cloneMachineNetworkConfigValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMachineNetworkConfig(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneMachineNetworkConfigValue(item))
		}
		return out
	default:
		return typed
	}
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
		mediaType := machineImageMediaType(image)
		if image.Spec.MediaType != "" && mediaType == "" {
			errs = append(errs, fmt.Sprintf("%s.mediaType %q must be one of: %s, %s", prefix, image.Spec.MediaType, v1alpha1.MachineImageMediaTypeDVD, v1alpha1.MachineImageMediaTypeBoot))
		}
		if image.Spec.URL == "" {
			errs = append(errs, prefix+".url is required")
		} else if err := media.ValidateISOReference(image.Spec.URL); err != nil {
			errs = append(errs, fmt.Sprintf("%s.url %s", prefix, err))
		}
		errs = append(errs, validateMachineImageInstallSource(prefix, mediaType, image.Spec.InstallSource)...)
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
		errs = append(errs, validateMachineInstallRepositories(prefix+".installer.anaconda.repositories", profile.Spec.Installer.Anaconda.Repositories)...)
		customizations := profile.Spec.Customizations
		if source := customizations.Hostname.Source; source != "" && source != v1alpha1.MachineInstallHostnameMachineName {
			errs = append(errs, fmt.Sprintf("%s.customizations.hostname.source %q must be %q", prefix, source, v1alpha1.MachineInstallHostnameMachineName))
		}
		if source := customizations.Storage.RootDevice.Source; source != "" && source != v1alpha1.MachineInstallRootDeviceMachine {
			errs = append(errs, fmt.Sprintf("%s.customizations.storage.rootDevice.source %q must be %q", prefix, source, v1alpha1.MachineInstallRootDeviceMachine))
		}
	}
	return errs
}

func machineImageMediaType(image v1alpha1.MachineImage) string {
	switch image.Spec.MediaType {
	case "", v1alpha1.MachineImageMediaTypeDVD:
		if image.Spec.MediaType == "" && strings.HasSuffix(strings.ToLower(image.Spec.URL), "boot.iso") {
			return v1alpha1.MachineImageMediaTypeBoot
		}
		return v1alpha1.MachineImageMediaTypeDVD
	case v1alpha1.MachineImageMediaTypeBoot:
		return v1alpha1.MachineImageMediaTypeBoot
	default:
		return ""
	}
}

func validateMachineImageInstallSource(prefix, mediaType string, installSource v1alpha1.MachineImageInstallSource) []string {
	var errs []string
	sourceType := machineImageInstallSourceType(installSource)
	if installSource.Type != "" && sourceType == "" {
		return []string{fmt.Sprintf("%s.installSource.type %q must be one of: %s, %s", prefix, installSource.Type, v1alpha1.MachineImageInstallSourceTypeURL, v1alpha1.MachineImageInstallSourceTypeRHSM)}
	}
	if mediaType == v1alpha1.MachineImageMediaTypeBoot && sourceType == "" {
		errs = append(errs, prefix+".installSource is required when mediaType is boot")
	}
	switch sourceType {
	case "":
		return errs
	case v1alpha1.MachineImageInstallSourceTypeURL:
		if installSource.EntitlementRef.Name != "" {
			errs = append(errs, prefix+".installSource.entitlementRef.name must be empty when installSource.type is url")
		}
		if installSource.URL == "" && len(installSource.Repositories) == 0 {
			errs = append(errs, prefix+".installSource.url or installSource.repositories is required when installSource.type is url")
		}
		if installSource.URL != "" && !httpURL(installSource.URL) {
			errs = append(errs, prefix+".installSource.url must be http:// or https://")
		}
		errs = append(errs, validateMachineInstallRepositories(prefix+".installSource.repositories", installSource.Repositories)...)
		for i, repo := range installSource.Repositories {
			if repo.BaseURL != "" && !httpURL(repo.BaseURL) {
				errs = append(errs, fmt.Sprintf("%s.installSource.repositories[%d].baseURL must be http:// or https://", prefix, i))
			}
		}
	case v1alpha1.MachineImageInstallSourceTypeRHSM:
		if installSource.URL != "" {
			errs = append(errs, prefix+".installSource.url must be empty when installSource.type is redhatCDN")
		}
		if len(installSource.Repositories) > 0 {
			errs = append(errs, prefix+".installSource.repositories must be empty when installSource.type is redhatCDN")
		}
		if installSource.EntitlementRef.Name == "" {
			errs = append(errs, prefix+".installSource.entitlementRef.name is required when installSource.type is redhatCDN")
		}
	}
	return errs
}

func machineImageInstallSourceType(installSource v1alpha1.MachineImageInstallSource) string {
	switch installSource.Type {
	case v1alpha1.MachineImageInstallSourceTypeURL, v1alpha1.MachineImageInstallSourceTypeRHSM:
		return installSource.Type
	case "":
		if installSource.EntitlementRef.Name != "" {
			return v1alpha1.MachineImageInstallSourceTypeRHSM
		}
		if installSource.URL != "" || len(installSource.Repositories) > 0 {
			return v1alpha1.MachineImageInstallSourceTypeURL
		}
	}
	return ""
}

func httpURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func validateMachineInstallRepositories(prefix string, repos []v1alpha1.MachineInstallRepository) []string {
	var errs []string
	for i, repo := range repos {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if repo.ID == "" {
			errs = append(errs, owner+".id is required")
		}
		if repo.BaseURL == "" {
			errs = append(errs, owner+".baseURL is required")
		}
	}
	return errs
}
