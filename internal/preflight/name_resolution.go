package preflight

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

const checkGroupNameResolution = "Name resolution"

const nameResolutionLookupTimeout = 3 * time.Second

func DefaultLookupHost(name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nameResolutionLookupTimeout)
	defer cancel()
	return net.DefaultResolver.LookupHost(ctx, name)
}

func nameResolutionChecks(state v1alpha1.State, selected []Phase, deps Deps, scope map[string]bool) []Check {
	if !anyPhaseInScope([]string{"fabric", "machines", "deps", "base", "add-ons"}, selected) {
		return nil
	}
	if deps.LookupHost == nil {
		return nil
	}
	var lookups []nameResolutionLookup
	for _, machine := range state.Machines {
		if len(scope) > 0 && !scope[machine.Metadata.Name] {
			continue
		}
		fqdn := v1alpha1.MachineFQDNAddress(machine)
		if fqdn == "" || !machineNameResolutionChecked(state, machine) {
			continue
		}
		expected := v1alpha1.MachineSSHAddress(machine)
		if expected == "" {
			continue
		}
		managed := machineNameResolutionManaged(state, machine)
		lookups = append(lookups, nameResolutionLookup{
			name:        "Machine/" + machine.Metadata.Name + " fqdn",
			lookup:      fqdn,
			expected:    expected,
			managed:     managed,
			remediation: fmt.Sprintf("create an A record %s -> %s on the DNS server provided by your infrastructure", fqdn, expected),
		})
		if hostname, ok := stateview.NodeHostname(state, machine.Metadata.Name); ok && hostname != "" && hostname != fqdn {
			lookups = append(lookups, nameResolutionLookup{
				name:        "node " + hostname,
				lookup:      hostname,
				expected:    expected,
				managed:     managed,
				remediation: fmt.Sprintf("create a CNAME record %s -> %s (or an A record %s -> %s) on the DNS server provided by your infrastructure", hostname, fqdn, hostname, expected),
			})
		}
	}
	if len(lookups) == 0 {
		return nil
	}
	checks := make([]Check, len(lookups))
	forEachBounded(len(lookups), nameResolutionParallelism, func(i int) {
		l := lookups[i]
		checks[i] = resolutionCheck(deps, l.name, l.lookup, l.expected, l.managed, l.remediation)
	})
	return checks
}

type nameResolutionLookup struct {
	name        string
	lookup      string
	expected    string
	managed     bool
	remediation string
}

func resolutionCheck(deps Deps, name, lookup, expected string, managed bool, externalRemediation string) Check {
	addresses, err := deps.LookupHost(lookup)
	if err != nil {
		if managed {
			return warnResolutionCheck(name,
				lookup+" does not resolve yet",
				"A fabric or machines apply treats this as provisional: it proves the managed DNS service and controller route before the first machines-phase SSH or mutation; a later-only range assumes that earlier barrier already succeeded",
				"include fabric or machines when establishing this service; if that apply refuses at its controller name-resolution barrier, repair what it reports and re-run the exact command it prints")
		}
		return failCheck(checkGroupNameResolution, name,
			lookup+" does not resolve",
			"Bootwright connects to this machine through its DNS name; SSH and cluster provisioning fail until the record exists",
			externalRemediation)
	}
	expectedIP := net.ParseIP(expected)
	if expectedIP == nil {
		return okCheck(checkGroupNameResolution, name, lookup+" -> "+strings.Join(addresses, ","))
	}
	for _, address := range addresses {
		if ip := net.ParseIP(address); ip != nil && ip.Equal(expectedIP) {
			return okCheck(checkGroupNameResolution, name, lookup+" -> "+address)
		}
	}
	if managed {
		return warnResolutionCheck(name,
			fmt.Sprintf("%s resolves to %s, want %s", lookup, strings.Join(addresses, ","), expected),
			"A fabric or machines apply reconverges this managed answer and refuses before the first machines-phase SSH or mutation unless the exact address succeeds; a later-only range assumes that earlier barrier already succeeded",
			"include fabric or machines when reconciling this service; if that apply refuses at its controller name-resolution barrier, repair what it reports and re-run the exact command it prints")
	}
	return failCheck(checkGroupNameResolution, name,
		fmt.Sprintf("%s resolves to %s, want %s", lookup, strings.Join(addresses, ","), expected),
		"The DNS record points at a different host, so Bootwright would drive the wrong machine",
		externalRemediation)
}

func warnResolutionCheck(name, evidence, impact, remediation string) Check {
	return Check{
		Group:       checkGroupNameResolution,
		Name:        name,
		Status:      StatusWarn,
		Evidence:    evidence,
		Impact:      impact,
		Remediation: remediation,
	}
}

func machineNameResolutionChecked(state v1alpha1.State, machine v1alpha1.Machine) bool {
	if stateview.MachineReferencesNameResolution(state, machine) {
		return true
	}
	return machineInheritsEnvironmentNameResolution(state, machine)
}

func machineInheritsEnvironmentNameResolution(state v1alpha1.State, machine v1alpha1.Machine) bool {
	if !v1alpha1.MachineOSProvided(machine) {
		return false
	}
	if hostname, ok := stateview.NodeHostname(state, machine.Metadata.Name); !ok || hostname == "" {
		return false
	}
	env := stateview.Environment(state)
	return env != nil && len(env.Spec.InfraComponents.NameResolution) > 0
}

func machineNameResolutionManaged(state v1alpha1.State, machine v1alpha1.Machine) bool {
	refs := machineNameResolutionRefNames(state, machine)
	if len(refs) == 0 {
		return false
	}
	env := stateview.Environment(state)
	if env == nil {
		return false
	}
	for _, entry := range env.Spec.InfraComponents.NameResolution {
		if refs[entry.Name] && entry.Management == v1alpha1.EnvironmentComponentManaged {
			return true
		}
	}
	return false
}

func machineNameResolutionRefNames(state v1alpha1.State, machine v1alpha1.Machine) map[string]bool {
	refs := map[string]bool{}
	config := machine.Spec.Network.Config
	if config.Spec != nil {
		for _, ref := range config.Spec.NameResolutionRefs {
			if ref.Name != "" {
				refs[ref.Name] = true
			}
		}
	}
	if config.NetworkConfigRef.Name != "" {
		if network, ok := stateview.NetworkConfig(state, config.NetworkConfigRef.Name); ok {
			for _, ref := range network.Spec.NameResolutionRefs {
				if ref.Name != "" {
					refs[ref.Name] = true
				}
			}
		}
	}
	return refs
}
