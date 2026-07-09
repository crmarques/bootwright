package nmstate

func NetworkConfigPrimaryIP(config map[string]any) string {
	if ip := networkConfigInterfaceIP(config, "", "ipv4"); ip != "" {
		return ip
	}
	return networkConfigInterfaceIP(config, "", "ipv6")
}

func networkConfigInterfaceIP(config map[string]any, interfaceName string, family string) string {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return ""
	}
	if family == "" {
		family = "ipv4"
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if interfaceName != "" && name != interfaceName {
			continue
		}
		if ip := networkConfigFamilyIP(entry, family); ip != "" {
			return ip
		}
	}
	return ""
}

func networkConfigFamilyIP(entry map[string]any, family string) string {
	familyConfig, ok := entry[family].(map[string]any)
	if !ok {
		return ""
	}
	rawAddresses, ok := familyConfig["address"].([]any)
	if !ok {
		return ""
	}
	for _, raw := range rawAddresses {
		address, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ip, _ := address["ip"].(string)
		if ip != "" {
			return ip
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
