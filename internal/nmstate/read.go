package nmstate

import "strconv"

func NetworkConfigInterfaceEntries(config map[string]any) []map[string]any {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			out = append(out, entry)
		}
	}
	return out
}

func NetworkConfigPrimaryIP(config map[string]any) string {
	if ip := networkConfigFirstFamilyIP(config, "ipv4"); ip != "" {
		return ip
	}
	return networkConfigFirstFamilyIP(config, "ipv6")
}

func networkConfigFirstFamilyIP(config map[string]any, family string) string {
	for _, entry := range NetworkConfigInterfaceEntries(config) {
		if ip := networkConfigFamilyIP(entry, family); ip != "" {
			return ip
		}
	}
	return ""
}

func networkConfigFamilyIP(entry map[string]any, family string) string {
	addresses := NetworkConfigFamilyAddresses(entry, family)
	if len(addresses) == 0 {
		return ""
	}
	return addresses[0]
}

func NetworkConfigFamilyAddresses(entry map[string]any, family string) []string {
	var out []string
	for _, address := range networkConfigFamilyAddressEntries(entry, family) {
		if ip, _ := address["ip"].(string); ip != "" {
			out = append(out, ip)
		}
	}
	return out
}

func NetworkConfigFamilyIPPrefix(entry map[string]any, family string) (string, int) {
	for _, address := range networkConfigFamilyAddressEntries(entry, family) {
		ip, _ := address["ip"].(string)
		if ip == "" {
			continue
		}
		return ip, intFromYAML(address["prefix-length"])
	}
	return "", 0
}

func NetworkConfigFamilyUsesDHCP(entry map[string]any, family string) bool {
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

func networkConfigFamilyAddressEntries(entry map[string]any, family string) []map[string]any {
	familyConfig, ok := entry[family].(map[string]any)
	if !ok {
		return nil
	}
	rawAddresses, ok := familyConfig["address"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(rawAddresses))
	for _, raw := range rawAddresses {
		if address, ok := raw.(map[string]any); ok {
			out = append(out, address)
		}
	}
	return out
}

type RoutedInterface struct {
	Name string
	Type string
	IPv4 string
}

func NetworkConfigRoutedInterface(config map[string]any) (RoutedInterface, bool) {
	name := NetworkConfigDefaultRouteInterface(config)
	if name == "" {
		return RoutedInterface{}, false
	}
	for _, entry := range NetworkConfigInterfaceEntries(config) {
		if got, _ := entry["name"].(string); got != name {
			continue
		}
		kind, _ := entry["type"].(string)
		return RoutedInterface{Name: name, Type: kind, IPv4: networkConfigFamilyIP(entry, "ipv4")}, true
	}
	return RoutedInterface{Name: name}, true
}

func NetworkConfigDefaultRouteInterface(config map[string]any) string {
	return networkConfigDefaultRouteField(config, "next-hop-interface")
}

func NetworkConfigGateway(config map[string]any) string {
	return networkConfigDefaultRouteField(config, "next-hop-address")
}

func networkConfigDefaultRouteField(config map[string]any, field string) string {
	routes, ok := config["routes"].(map[string]any)
	if !ok {
		return ""
	}
	entries, ok := routes["config"].([]any)
	if !ok {
		return ""
	}
	for _, item := range entries {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if destination, _ := entry["destination"].(string); destination != "0.0.0.0/0" {
			continue
		}
		if value, _ := entry[field].(string); value != "" {
			return value
		}
	}
	return ""
}

func NetworkConfigDNSServers(config map[string]any) []string {
	rawResolver, ok := config["dns-resolver"].(map[string]any)
	if !ok {
		return nil
	}
	rawConfig, ok := rawResolver["config"].(map[string]any)
	if !ok {
		return nil
	}
	rawServers, ok := rawConfig["server"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rawServers))
	for _, raw := range rawServers {
		if server, ok := raw.(string); ok && server != "" {
			out = append(out, server)
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
