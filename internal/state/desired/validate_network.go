package desiredstate

import (
	"fmt"
	"net"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateNetworkConfigs(state v1alpha1.State) []string {
	var errs []string
	dnsRefs := map[string]bool{}
	if env := primaryEnvironment(&state); env != nil {
		for _, entry := range env.Spec.InfraComponents.NameResolution {
			dnsRefs[entry.Name] = true
		}
	}
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
		errs = append(errs, validateNetworkConfigSpec(n, dnsRefs)...)
	}
	errs = append(errs, validateNetworkConfigCIDRSharing(state.NetworkConfigs)...)
	return errs
}

func validateNetworkConfigSpec(n v1alpha1.NetworkConfig, dnsRefs map[string]bool) []string {
	var errs []string
	if len(n.Spec.MachineNetwork) == 0 {
		errs = append(errs, fmt.Sprintf("NetworkConfig/%s spec.machineNetwork is required (at least one cidr)", n.Metadata.Name))
	}
	seenCIDRs := map[string]bool{}
	for i, mn := range n.Spec.MachineNetwork {
		owner := fmt.Sprintf("NetworkConfig/%s spec.machineNetwork[%d]", n.Metadata.Name, i)
		if mn.CIDR == "" {
			errs = append(errs, fmt.Sprintf("%s.cidr is required", owner))
			continue
		}
		if _, _, err := net.ParseCIDR(mn.CIDR); err != nil {
			errs = append(errs, fmt.Sprintf("%s.cidr %q invalid: %v", owner, mn.CIDR, err))
			continue
		}
		if seenCIDRs[mn.CIDR] {
			errs = append(errs, fmt.Sprintf("%s.cidr %q is duplicated inside this NetworkConfig", owner, mn.CIDR))
		}
		seenCIDRs[mn.CIDR] = true
	}
	if n.Spec.Template.NetworkConfig == nil {
		errs = append(errs, fmt.Sprintf("NetworkConfig/%s spec.template.networkConfig is required", n.Metadata.Name))
	} else if _, ok := n.Spec.Template.NetworkConfig["dnsRefs"]; ok {
		errs = append(errs, fmt.Sprintf("NetworkConfig/%s spec.template.networkConfig.dnsRefs is not valid NMState; use spec.dnsRefs instead", n.Metadata.Name))
	}
	seenDNSRefs := map[string]bool{}
	for i, ref := range n.Spec.DNSRefs {
		owner := fmt.Sprintf("NetworkConfig/%s spec.dnsRefs[%d]", n.Metadata.Name, i)
		if ref == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if seenDNSRefs[ref] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, ref))
			continue
		}
		seenDNSRefs[ref] = true
		if !dnsRefs[ref] {
			errs = append(errs, fmt.Sprintf("%s %q does not match any Environment spec.infraComponents.nameResolution[].name", owner, ref))
		}
	}
	return errs
}

func validateNetworkConfigCIDRSharing(nets []v1alpha1.NetworkConfig) []string {
	if len(nets) < 2 {
		return nil
	}
	byCIDR := map[string][]string{}
	for _, n := range nets {
		for _, mn := range n.Spec.MachineNetwork {
			if mn.CIDR == "" {
				continue
			}
			byCIDR[mn.CIDR] = append(byCIDR[mn.CIDR], n.Metadata.Name)
		}
	}
	var errs []string
	for cidr, names := range byCIDR {
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		errs = append(errs, fmt.Sprintf("NetworkConfig machineNetwork cidr %q appears on multiple NetworkConfigs (%s); each CIDR must have exactly one owning NetworkConfig", cidr, joinSortedNames(names)))
	}
	sort.Strings(errs)
	return errs
}
