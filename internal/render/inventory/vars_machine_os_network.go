package inventory

import (
	"fmt"
	"net"
	"sort"
	"strconv"
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
	if iface := kickstartStaticNetworkStanza(kickstartPrimaryInterfaceEntry(config)); len(iface) > 0 {
		for k, v := range iface {
			network[k] = v
		}
	}
	ifaces := kickstartNetworkInterfaces(config)
	if len(ifaces) > 0 {
		network["interfaces"] = ifaces
	}
	if gateway := nmstate.NetworkConfigGateway(config); gateway != "" {
		network["gateway"] = gateway
	}
	if dns := nmstate.NetworkConfigDNSServers(config); len(dns) > 0 {
		network["dnsServers"] = dns
	}
	return network
}

func machineInstallDesiredNetwork(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, clusterName string) map[string]any {
	config := installer.AgentNetworkConfig(state, ci, m, clusterName)
	markEthernetMACIdentity(config)
	return config
}

func markEthernetMACIdentity(config map[string]any) {
	interfaces, ok := config["interfaces"].([]any)
	if !ok {
		return
	}
	for _, item := range interfaces {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := entry["type"].(string); typ != "ethernet" {
			continue
		}
		if mac, _ := entry["mac-address"].(string); mac == "" {
			continue
		}
		entry["identifier"] = "mac-address"
	}
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
	switch typ, _ := primary["type"].(string); typ {
	case "vlan":
		vlan, ok := primary["vlan"].(map[string]any)
		if !ok {
			break
		}
		base, _ := vlan["base-iface"].(string)
		id := intFromYAML(vlan["id"])
		if base == "" || id <= 0 {
			break
		}
		stanza["vlanID"] = id
		stanza["device"] = base
		if name, _ := primary["name"].(string); name != "" && name != fmt.Sprintf("%s.%d", base, id) {
			stanza["interfaceName"] = name
		}
		if baseEntry := interfaces[base]; baseEntry != nil {
			addBondFields(stanza, baseEntry)
			if baseType, _ := baseEntry["type"].(string); baseType == "ethernet" {
				if mac, _ := baseEntry["mac-address"].(string); mac != "" {
					stanza["device"] = mac
				}
			}
		}
	case "bond":
		addBondFields(stanza, primary)
	}
	return []map[string]any{stanza}
}

func kickstartStaticNetworkStanza(entry map[string]any) map[string]any {
	name, _ := entry["name"].(string)
	mac, _ := entry["mac-address"].(string)
	ip, prefix := nmstate.NetworkConfigFamilyIPPrefix(entry, "ipv4")
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
	routed := nmstate.NetworkConfigDefaultRouteInterface(config)
	var first map[string]any
	for _, entry := range nmstate.NetworkConfigInterfaceEntries(config) {
		ip, _ := nmstate.NetworkConfigFamilyIPPrefix(entry, "ipv4")
		if ip == "" {
			continue
		}
		if name, _ := entry["name"].(string); routed != "" && name == routed {
			return entry
		}
		if first == nil {
			first = entry
		}
	}
	return first
}

func networkConfigInterfacesByName(config map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, entry := range nmstate.NetworkConfigInterfaceEntries(config) {
		if name, _ := entry["name"].(string); name != "" {
			out[name] = entry
		}
	}
	return out
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
	case string:
		out, _ := strconv.Atoi(v)
		return out
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
