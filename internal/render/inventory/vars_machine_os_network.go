package inventory

import (
	"net"

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

func prefixNetmask(prefix int) string {
	if prefix <= 0 || prefix > 32 {
		return ""
	}
	return net.IP(net.CIDRMask(prefix, 32)).String()
}
