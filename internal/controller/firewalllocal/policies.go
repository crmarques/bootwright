package firewalllocal

import (
	"strconv"
	"strings"
)

func harmlessPolicies(query func(...string) (string, error)) bool {
	active, err := query("--get-active-policies")
	if err != nil {
		return false
	}
	names, valid := activePolicies(active)
	if !valid {
		return false
	}
	seen := map[string]bool{}
	for _, name := range names {
		if !safeName(name) || seen[name] {
			return false
		}
		seen[name] = true
		policy, err := query("--info-policy=" + name)
		if err != nil || !harmlessPolicy(policy, name) {
			return false
		}
	}
	return true
}

func harmlessPolicy(data, name string) bool {
	known := map[string]bool{"priority": true, "disable": true, "target": true, "ingress-zones": true, "egress-zones": true, "services": true, "ports": true, "protocols": true, "masquerade": true, "forward-ports": true, "source-ports": true, "icmp-blocks": true, "rich rules": true}
	fields := map[string]string{}
	rules := false
	hasICMPRules := false
	for index, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if index == 0 {
			if line != name && line != name+" (active)" {
				return false
			}
			continue
		}
		if rules {
			if !icmpAcceptRule(line) {
				return false
			}
			hasICMPRules = true
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || !known[key] {
			return false
		}
		if _, duplicate := fields[key]; duplicate {
			return false
		}
		fields[key] = strings.TrimSpace(value)
		if key == "rich rules" {
			if fields[key] != "" {
				return false
			}
			rules = true
		}
	}
	for key := range known {
		if _, found := fields[key]; !found && key != "disable" {
			return false
		}
	}
	if fields["masquerade"] != "no" || fields["disable"] != "" && fields["disable"] != "no" {
		return false
	}
	if _, err := strconv.ParseInt(fields["priority"], 10, 32); err != nil {
		return false
	}
	for _, key := range []string{"services", "ports", "protocols", "forward-ports", "source-ports", "icmp-blocks"} {
		if fields[key] != "" {
			return false
		}
	}
	for _, key := range []string{"ingress-zones", "egress-zones"} {
		if !validPolicyZones(fields[key]) {
			return false
		}
	}
	if fields["target"] == "CONTINUE" {
		return true
	}
	return fields["target"] == "ACCEPT" && !hasICMPRules && fields["ingress-zones"] != "HOST" && fields["egress-zones"] != "HOST"
}

func validPolicyZones(value string) bool {
	zones := strings.Fields(value)
	if len(zones) == 0 || len(zones) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, zone := range zones {
		if !safeName(zone) || seen[zone] || (zone == "ANY" || zone == "HOST") && len(zones) != 1 {
			return false
		}
		seen[zone] = true
	}
	return true
}

func icmpAcceptRule(line string) bool {
	parts := strings.Fields(line)
	if len(parts) != 5 || parts[0] != "rule" || parts[1] != `family="ipv6"` || parts[2] != "icmp-type" || parts[4] != "accept" {
		return false
	}
	name := strings.TrimPrefix(parts[3], `name="`)
	if name == parts[3] || !strings.HasSuffix(name, `"`) {
		return false
	}
	return safeName(strings.TrimSuffix(name, `"`))
}

func activePolicies(data string) ([]string, bool) {
	var names []string
	fields := map[string]bool{}
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if raw == line {
			if len(names) != 0 && len(fields) != 2 {
				return nil, false
			}
			if !safeName(line) || len(names) >= 64 {
				return nil, false
			}
			names = append(names, line)
			fields = map[string]bool{}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if len(names) == 0 || !ok || (key != "ingress-zones" && key != "egress-zones") || fields[key] {
			return nil, false
		}
		if !validPolicyZones(value) {
			return nil, false
		}
		fields[key] = true
	}
	return names, len(names) == 0 || len(fields) == 2
}
