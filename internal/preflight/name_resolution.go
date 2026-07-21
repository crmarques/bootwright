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

func nameResolutionChecks(state v1alpha1.State, selected []Phase, deps Deps) []Check {
	if !anyPhaseInScope([]string{"fabric", "machines", "deps", "base", "add-ons"}, selected) {
		return nil
	}
	if deps.LookupHost == nil {
		return nil
	}
	var checks []Check
	for _, machine := range state.Machines {
		dnsEntry := v1alpha1.MachineDNSEntryAddress(machine)
		if dnsEntry == "" || !stateview.MachineReferencesNameResolution(state, machine) {
			continue
		}
		expected := v1alpha1.MachineSSHAddress(machine)
		if expected == "" {
			continue
		}
		managed := machineNameResolutionManaged(state, machine)
		checks = append(checks, resolutionCheck(deps, "Machine/"+machine.Metadata.Name+" dnsEntry", dnsEntry, expected, managed,
			fmt.Sprintf("create an A record %s -> %s on the DNS server provided by your infrastructure", dnsEntry, expected)))
		if hostname, ok := stateview.NodeHostname(state, machine.Metadata.Name); ok && hostname != "" && hostname != dnsEntry {
			checks = append(checks, resolutionCheck(deps, "node "+hostname, hostname, expected, managed,
				fmt.Sprintf("create a CNAME record %s -> %s (or an A record %s -> %s) on the DNS server provided by your infrastructure", hostname, dnsEntry, hostname, expected)))
		}
	}
	return checks
}

func resolutionCheck(deps Deps, name, lookup, expected string, managed bool, externalRemediation string) Check {
	addresses, err := deps.LookupHost(lookup)
	if err != nil {
		if managed {
			return Check{
				Group:       checkGroupNameResolution,
				Name:        name,
				Status:      StatusWarn,
				Evidence:    lookup + " does not resolve yet",
				Impact:      "Bootwright connects to this machine through its DNS name; the managed name-resolution component publishes this record once the infra stage converges",
				Remediation: "bootwright apply converges the managed name-resolution component; if this persists after apply, check that the controller uses the managed resolver",
			}
		}
		return failCheck(checkGroupNameResolution, name,
			lookup+" does not resolve",
			"Bootwright connects to this machine through its DNS name; SSH and cluster provisioning fail until the record exists",
			externalRemediation)
	}
	for _, address := range addresses {
		if address == expected {
			return okCheck(checkGroupNameResolution, name, lookup+" -> "+address)
		}
	}
	return failCheck(checkGroupNameResolution, name,
		fmt.Sprintf("%s resolves to %s, want %s", lookup, strings.Join(addresses, ","), expected),
		"The DNS record points at a different host, so Bootwright would drive the wrong machine",
		externalRemediation)
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
