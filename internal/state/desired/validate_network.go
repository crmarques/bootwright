package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateNetworkConfigs(state v1alpha1.State) []string {
	var errs []string
	dnsRefs := networkConfigDNSRefs(state)
	seen := map[string]bool{}
	for _, n := range state.NetworkConfigs {
		if e := validateName(v1alpha1.KindNetworkConfig, n.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[n.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate NetworkConfig %q", n.Metadata.Name))
		}
		seen[n.Metadata.Name] = true
		errs = append(errs, validateNetworkConfigSpec(fmt.Sprintf("NetworkConfig/%s spec", n.Metadata.Name), n.Spec, dnsRefs)...)
	}
	return errs
}

func networkConfigDNSRefs(state v1alpha1.State) map[string]bool {
	dnsRefs := map[string]bool{}
	if env := primaryEnvironment(&state); env != nil {
		for _, entry := range env.Spec.InfraComponents.NameResolution {
			dnsRefs[entry.Name] = true
		}
	}
	return dnsRefs
}

func validateNetworkConfigSpec(owner string, spec v1alpha1.NetworkConfigSpec, dnsRefs map[string]bool) []string {
	var errs []string
	if len(spec.MachineNetwork) == 0 {
		errs = append(errs, fmt.Sprintf("%s.machineNetwork is required (at least one cidr)", owner))
	}
	seenCIDRs := map[string]bool{}
	for i, mn := range spec.MachineNetwork {
		field := fmt.Sprintf("%s.machineNetwork[%d]", owner, i)
		if mn.CIDR == "" {
			errs = append(errs, fmt.Sprintf("%s.cidr is required", field))
			continue
		}
		if _, _, err := net.ParseCIDR(mn.CIDR); err != nil {
			errs = append(errs, fmt.Sprintf("%s.cidr %q invalid: %v", field, mn.CIDR, err))
			continue
		}
		if seenCIDRs[mn.CIDR] {
			errs = append(errs, fmt.Sprintf("%s.cidr %q is duplicated inside this NetworkConfig", field, mn.CIDR))
		}
		seenCIDRs[mn.CIDR] = true
	}
	if spec.Template.NetworkConfig == nil {
		errs = append(errs, fmt.Sprintf("%s.template.networkConfig is required", owner))
	} else if _, ok := spec.Template.NetworkConfig["dnsRefs"]; ok {
		errs = append(errs, fmt.Sprintf("%s.template.networkConfig.dnsRefs is not valid NMState; use spec.dnsRefs instead", owner))
	}
	seenDNSRefs := map[string]bool{}
	for i, ref := range spec.DNSRefs {
		field := fmt.Sprintf("%s.dnsRefs[%d]", owner, i)
		if ref == "" {
			errs = append(errs, field+" must not be empty")
			continue
		}
		if seenDNSRefs[ref] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", field, ref))
			continue
		}
		seenDNSRefs[ref] = true
		if !dnsRefs[ref] {
			errs = append(errs, fmt.Sprintf("%s %q does not match any Environment spec.infraComponents.nameResolution[].name", field, ref))
		}
	}
	return errs
}
