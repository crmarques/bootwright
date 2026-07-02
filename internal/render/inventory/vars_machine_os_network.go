package inventory

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/nmstate"
	"github.com/crmarques/bootwright/internal/render/installer"
)

func machineInstallNetworkVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, clusterName string) map[string]any {
	config := installer.AgentNetworkConfig(state, ci, m, clusterName)
	network := map[string]any{
		"bootproto": "dhcp",
		"device":    "link",
	}
	if len(config) == 0 {
		return network
	}
	if iface := kickstartPrimaryInterface(config); len(iface) > 0 {
		for k, v := range iface {
			network[k] = v
		}
	}
	ifaces := kickstartNetworkInterfaces(config)
	if len(ifaces) > 0 {
		network["interfaces"] = ifaces
	}
	if gateway := networkConfigGatewayFromMap(config); gateway != "" {
		network["gateway"] = gateway
	}
	if dns := nmstate.NetworkConfigDNSServers(config); len(dns) > 0 {
		network["dnsServers"] = dns
	}
	return network
}

func kickstartPrimaryInterface(config map[string]any) map[string]any {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		mac, _ := entry["mac-address"].(string)
		ip, prefix := networkConfigFamilyIPPrefix(entry, "ipv4")
		if ip == "" {
			continue
		}
		out := map[string]any{
			"bootproto": "static",
			"ip":        ip,
			"prefix":    prefix,
			"netmask":   prefixNetmask(prefix),
		}
		if mac != "" {
			out["device"] = mac
		} else if name != "" {
			out["device"] = name
		}
		return out
	}
	return nil
}

func kickstartNetworkInterfaces(config map[string]any) []map[string]any {
	interfaces := networkConfigInterfacesByName(config)
	primary := kickstartPrimaryInterfaceEntry(config)
	if primary == nil {
		return nil
	}
	stanza := kickstartStaticNetworkStanza(primary)
	if len(stanza) == 0 {
		return nil
	}
	stanza["hostname"] = true
	var out []map[string]any
	if typ, _ := primary["type"].(string); typ == "vlan" {
		if vlan, ok := primary["vlan"].(map[string]any); ok {
			base, _ := vlan["base-iface"].(string)
			if id := intFromYAML(vlan["id"]); id > 0 {
				stanza["vlanID"] = id
			}
			if name, _ := primary["name"].(string); name != "" {
				stanza["interfaceName"] = name
			}
			if baseEntry := interfaces[base]; baseEntry != nil {
				if baseStanza := kickstartInterfaceDependencyStanza(baseEntry); len(baseStanza) > 0 {
					out = append(out, baseStanza)
				}
			}
		}
	} else if typ == "bond" {
		addBondFields(stanza, primary)
	}
	out = append(out, stanza)
	return out
}

func kickstartInterfaceDependencyStanza(entry map[string]any) map[string]any {
	typ, _ := entry["type"].(string)
	if typ != "bond" {
		return nil
	}
	name, _ := entry["name"].(string)
	if name == "" {
		return nil
	}
	out := map[string]any{
		"device":    name,
		"bootproto": "none",
	}
	addBondFields(out, entry)
	return out
}

func kickstartStaticNetworkStanza(entry map[string]any) map[string]any {
	name, _ := entry["name"].(string)
	mac, _ := entry["mac-address"].(string)
	ip, prefix := networkConfigFamilyIPPrefix(entry, "ipv4")
	if ip == "" {
		return nil
	}
	out := map[string]any{
		"bootproto": "static",
		"ip":        ip,
		"prefix":    prefix,
		"netmask":   prefixNetmask(prefix),
	}
	if mac != "" {
		out["device"] = mac
	} else if name != "" {
		out["device"] = name
	}
	return out
}

func addBondFields(out, entry map[string]any) {
	aggregation, ok := entry["link-aggregation"].(map[string]any)
	if !ok {
		return
	}
	if ports := stringSliceFromYAML(aggregation["port"]); len(ports) > 0 {
		out["bondSlaves"] = ports
	}
	if opts := kickstartBondOptions(aggregation); opts != "" {
		out["bondOptions"] = opts
	}
}

func kickstartBondOptions(aggregation map[string]any) string {
	var parts []string
	if mode, _ := aggregation["mode"].(string); mode != "" {
		parts = append(parts, "mode="+mode)
	}
	options, _ := aggregation["options"].(map[string]any)
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := fmt.Sprint(options[key])
		if value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	return strings.Join(parts, ",")
}

func kickstartPrimaryInterfaceEntry(config map[string]any) map[string]any {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ip, _ := networkConfigFamilyIPPrefix(entry, "ipv4")
		if ip != "" {
			return entry
		}
	}
	return nil
}

func networkConfigInterfacesByName(config map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return out
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name != "" {
			out[name] = entry
		}
	}
	return out
}

func networkConfigFamilyIPPrefix(entry map[string]any, family string) (string, int) {
	familyConfig, ok := entry[family].(map[string]any)
	if !ok {
		return "", 0
	}
	rawAddresses, ok := familyConfig["address"].([]any)
	if !ok {
		return "", 0
	}
	for _, raw := range rawAddresses {
		address, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ip, _ := address["ip"].(string)
		if ip == "" {
			continue
		}
		return ip, intFromYAML(address["prefix-length"])
	}
	return "", 0
}

func networkConfigGatewayFromMap(config map[string]any) string {
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
		destination, _ := route["destination"].(string)
		if destination != "0.0.0.0/0" && destination != "::/0" {
			continue
		}
		nextHop, _ := route["next-hop-address"].(string)
		if nextHop != "" {
			return nextHop
		}
	}
	return ""
}

func intFromYAML(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func stringSliceFromYAML(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		value, _ := item.(string)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func prefixNetmask(prefix int) string {
	if prefix <= 0 || prefix > 32 {
		return ""
	}
	return net.IP(net.CIDRMask(prefix, 32)).String()
}
