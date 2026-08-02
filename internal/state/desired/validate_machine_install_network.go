package desiredstate

import (
	"fmt"
	"net"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

const machineInstallNetworkProvidedRemedy = "or set spec.os.provided: true to hand this machine to your own OS install"

var anacondaNetworkInterfaceTypes = map[string]bool{
	"":         true,
	"ethernet": true,
	"vlan":     true,
	"bond":     true,
}

type installNetworkInterface struct {
	name  string
	kind  string
	v4    []string
	v6    []string
	dhcp4 bool
}

func validateMachineInstallNetwork(owner string, machine v1alpha1.Machine, config map[string]any) []string {
	if !v1alpha1.MachineInstallsOS(machine) {
		return nil
	}
	var addressed, unreachable []installNetworkInterface
	dhcp4 := false
	for _, iface := range installNetworkInterfaces(config) {
		switch {
		case len(iface.v4) > 0:
			addressed = append(addressed, iface)
		case iface.dhcp4:
			dhcp4 = true
		case len(iface.v6) > 0:
			unreachable = append(unreachable, iface)
		}
	}
	if len(addressed) == 0 {
		if dhcp4 || len(unreachable) == 0 {
			return nil
		}
		return []string{fmt.Sprintf("%s carries no IPv4 address; interface %q is addressed %s only, and an Anaconda network directive expresses IPv4 static addressing or DHCP and nothing else, so the install would run `network --device=link --bootproto=dhcp` and the machine would never answer at its authored address once its disk had been wiped. Give the install interface an IPv4 address, %s",
			owner, unreachable[0].name, strings.Join(unreachable[0].v6, ", "), machineInstallNetworkProvidedRemedy)}
	}
	primary := installNetworkPrimaryInterface(config, addressed)
	if !anacondaNetworkInterfaceTypes[primary.kind] {
		return []string{fmt.Sprintf("%s makes interface %q the install primary, but its type %q is not one an Anaconda network directive can express (ethernet, vlan, bond); the install would never create it and the machine would never answer at its authored address once its disk had been wiped. Carry the install address on an ethernet, vlan, or bond interface, %s",
			owner, primary.name, primary.kind, machineInstallNetworkProvidedRemedy)}
	}
	if len(addressed) < 2 {
		return nil
	}
	reach := v1alpha1.MachineSSHAddress(machine)
	if net.ParseIP(reach) == nil {
		return nil
	}
	if machineInstallNetworkCarries(primary, reach) {
		return nil
	}
	return []string{fmt.Sprintf("%s addresses %d interfaces (%s) but an Anaconda network directive brings up one, and the install primary %q does not carry %q, the address Bootwright reaches this machine at after the install; the install would wipe the disk of a machine that then never answers. Move that address onto %q, or give its interface the default route so the install brings that interface up, %s",
		owner, len(addressed), strings.Join(installNetworkInterfaceNames(addressed), ", "), primary.name, reach, primary.name, machineInstallNetworkProvidedRemedy)}
}

func machineInstallNetworkCarries(iface installNetworkInterface, address string) bool {
	for _, ip := range iface.v4 {
		if ip == address {
			return true
		}
	}
	return false
}

func installNetworkInterfaceNames(interfaces []installNetworkInterface) []string {
	out := make([]string, 0, len(interfaces))
	for _, iface := range interfaces {
		out = append(out, iface.name)
	}
	return out
}

func installNetworkInterfaces(config map[string]any) []installNetworkInterface {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	var out []installNetworkInterface
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name == "" {
			continue
		}
		kind, _ := entry["type"].(string)
		out = append(out, installNetworkInterface{
			name:  name,
			kind:  kind,
			v4:    networkConfigFamilyAddresses(entry, "ipv4"),
			v6:    networkConfigFamilyAddresses(entry, "ipv6"),
			dhcp4: networkConfigFamilyUsesDHCP(entry, "ipv4"),
		})
	}
	return out
}

func networkConfigFamilyUsesDHCP(entry map[string]any, family string) bool {
	familyConfig, ok := entry[family].(map[string]any)
	if !ok {
		return false
	}
	if enabled, ok := familyConfig["enabled"].(bool); ok && !enabled {
		return false
	}
	dhcp, _ := familyConfig["dhcp"].(bool)
	return dhcp
}

func networkConfigFamilyAddresses(entry map[string]any, family string) []string {
	familyConfig, ok := entry[family].(map[string]any)
	if !ok {
		return nil
	}
	rawAddresses, ok := familyConfig["address"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, raw := range rawAddresses {
		address, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if ip, _ := address["ip"].(string); ip != "" {
			out = append(out, ip)
		}
	}
	return out
}

func installNetworkPrimaryInterface(config map[string]any, addressed []installNetworkInterface) installNetworkInterface {
	routed := networkConfigDefaultRouteInterfaceName(config)
	if routed != "" {
		for _, iface := range addressed {
			if iface.name == routed {
				return iface
			}
		}
	}
	return addressed[0]
}

func networkConfigDefaultRouteInterfaceName(config map[string]any) string {
	routes, ok := config["routes"].(map[string]any)
	if !ok {
		return ""
	}
	rawConfig, ok := routes["config"].([]any)
	if !ok {
		return ""
	}
	for _, item := range rawConfig {
		route, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if destination, _ := route["destination"].(string); destination != "0.0.0.0/0" {
			continue
		}
		if iface, _ := route["next-hop-interface"].(string); iface != "" {
			return iface
		}
	}
	return ""
}
