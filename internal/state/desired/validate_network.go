package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateNetworkConfigs(state v1alpha1.State) []Finding {
	var errs []Finding
	nameResolutionNames := environmentNameResolutionNames(state)
	seen := map[string]bool{}
	for _, n := range state.NetworkConfigs {
		if e := validateName(v1alpha1.KindNetworkConfig, n.Metadata.Name); e != "" {
			errs = append(errs, note(e))
			continue
		}
		object := v1alpha1.KindNetworkConfig + "/" + n.Metadata.Name
		if seen[n.Metadata.Name] {
			errs = append(errs, diag(object, "", fmt.Sprintf("duplicate NetworkConfig %q", n.Metadata.Name)))
		}
		seen[n.Metadata.Name] = true
		errs = append(errs, notes(validateNetworkConfigSpec(object+" spec", n.Spec, nameResolutionNames))...)
	}
	return errs
}

func environmentNameResolutionNames(state v1alpha1.State) map[string]bool {
	names := map[string]bool{}
	if env := primaryEnvironment(&state); env != nil {
		for _, entry := range env.Spec.InfraComponents.NameResolution {
			names[entry.Name] = true
		}
	}
	return names
}

func validateNetworkConfigSpec(owner string, spec v1alpha1.NetworkConfigSpec, nameResolutionNames map[string]bool) []string {
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
	} else if _, ok := spec.Template.NetworkConfig["nameResolutionRefs"]; ok {
		errs = append(errs, fmt.Sprintf("%s.template.networkConfig.nameResolutionRefs is not valid NMState; use spec.nameResolutionRefs instead", owner))
	}
	seenRefs := map[string]bool{}
	for i, ref := range spec.NameResolutionRefs {
		field := fmt.Sprintf("%s.nameResolutionRefs[%d]", owner, i)
		if ref.Name == "" {
			errs = append(errs, field+" must not be empty")
			continue
		}
		if seenRefs[ref.Name] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", field, ref.Name))
			continue
		}
		seenRefs[ref.Name] = true
		if !nameResolutionNames[ref.Name] {
			errs = append(errs, fmt.Sprintf("%s %q does not match any Environment spec.infraComponents.nameResolution[].name", field, ref.Name))
		}
	}
	return errs
}
