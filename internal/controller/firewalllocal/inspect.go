package firewalllocal

import (
	"context"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/controller/firewall"
)

type Interface struct {
	Name      string
	Addresses []netip.Addr
}

type Inspector struct {
	Read       func(context.Context, []string) ([]byte, error)
	Interfaces func() ([]Interface, error)
}

func (i Inspector) Inspect(ctx context.Context, socket firewall.Socket) (firewall.Observation, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	read := i.Read
	if read == nil {
		read = command
	}
	interfaces := i.Interfaces
	if interfaces == nil {
		interfaces = localInterfaces
	}
	unknown := func(reason string) (firewall.Observation, error) {
		return firewall.Observation{Status: "unknown", Reason: reason}, nil
	}
	links, err := interfaces()
	if err != nil {
		return unknown("controller interface addresses could not be read")
	}
	address, err := netip.ParseAddr(socket.Address)
	if err != nil {
		return unknown("the listener has no exact interface address")
	}
	link := ""
	for _, candidate := range links {
		for _, assigned := range candidate.Addresses {
			if assigned.Unmap() == address.Unmap() {
				if link != "" && link != candidate.Name {
					return unknown("the listener address belongs to multiple interfaces")
				}
				link = candidate.Name
			}
		}
	}
	if link == "" {
		return unknown("the listener address is not assigned to a controller interface")
	}
	query := func(args ...string) (string, error) {
		data, err := read(ctx, args)
		return strings.TrimSpace(string(data)), err
	}
	if state, err := query("--state"); err != nil || state != "running" {
		return unknown("firewalld is unavailable; another firewall cannot be inferred safe")
	}
	if panic, err := query("--query-panic"); err != nil || panic != "no" {
		return unknown("firewalld panic mode is enabled or could not be excluded")
	}
	active, err := query("--get-active-zones")
	if err != nil {
		return unknown("active firewalld zones could not be read")
	}
	for _, line := range strings.Split(active, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "sources:") && strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "sources:")) != "" {
			return unknown("source-selected zones make interface-only admission ambiguous")
		}
	}
	if !harmlessPolicies(query) {
		return unknown("active policy inspection is unavailable or a policy can affect TCP/UDP admission")
	}
	for _, args := range [][]string{{"--direct", "--get-all-rules"}, {"--direct", "--get-all-passthroughs"}} {
		value, err := query(args...)
		if err != nil || value != "" {
			return unknown("active policies or direct rules prevent bounded interface-zone inspection")
		}
	}
	return inspectZone(query, link, socket)
}

func inspectZone(query func(...string) (string, error), link string, socket firewall.Socket) (firewall.Observation, error) {
	unknown := func(reason string) (firewall.Observation, error) {
		return firewall.Observation{Status: "unknown", Reason: reason}, nil
	}
	zone, err := query("--get-zone-of-interface=" + link)
	if err == nil && (zone == "" || zone == "no zone") {
		zone, err = query("--get-default-zone")
	}
	if err != nil || !safeName(zone) {
		return unknown("the listener interface has no unambiguous runtime zone")
	}
	result := firewall.Observation{Interface: link, Zone: zone, Status: "unknown"}
	rich, err := query("--zone="+zone, "--list-rich-rules")
	if err != nil || rich != "" {
		result.Reason = "rich rules prevent bounded interface-zone inspection"
		return result, nil
	}
	config, err := query("--zone="+zone, "--list-all")
	if err != nil {
		result.Reason = "runtime zone configuration could not be read"
		return result, nil
	}
	fields, valid := zoneFields(config, zone)
	if !valid {
		result.Reason = "runtime zone output contains an unqualified rule shape"
		return result, nil
	}
	for _, key := range []string{"target", "sources", "services", "ports", "protocols", "forward-ports", "source-ports", "rich rules"} {
		if _, present := fields[key]; !present {
			result.Reason = "runtime zone output omits a required rule field"
			return result, nil
		}
	}
	target, hasTarget := fields["target"]
	ports, hasPorts := fields["ports"]
	services, hasServices := fields["services"]
	if !hasTarget || !hasPorts || !hasServices || fields["forward-ports"] != "" || fields["source-ports"] != "" || fields["protocols"] != "" || fields["rich rules"] != "" || fields["sources"] != "" {
		result.Reason = "runtime zone uses an unqualified or incomplete rule shape"
		return result, nil
	}
	allowed := target == "ACCEPT"
	if target != "ACCEPT" && target != "default" && target != "DROP" && target != "REJECT" && target != "%%REJECT%%" {
		result.Reason = "runtime zone target is unqualified"
		return result, nil
	}
	for _, entry := range strings.Fields(ports) {
		match, valid := admits(entry, socket.Port, socket.Protocol)
		if !valid {
			result.Reason = "runtime zone port grammar is unqualified"
			return result, nil
		}
		allowed = allowed || match
	}
	if allowed {
		result.Status, result.Reason = "open", "runtime interface zone explicitly admits this socket"
	} else if services != "" {
		result.Reason = "service expansion has not qualified admission for this socket"
	} else {
		result.Status, result.Reason = "closed", "runtime interface zone does not admit this socket"
	}
	return result, nil
}

func safeName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func admits(entry string, port int, protocol string) (bool, bool) {
	interval, transport, ok := strings.Cut(entry, "/")
	if !ok || (transport != "tcp" && transport != "udp" && transport != "sctp" && transport != "dccp") {
		return false, false
	}
	low, high, ranged := strings.Cut(interval, "-")
	first, err := strconv.Atoi(low)
	if err != nil || first < 1 || first > 65535 {
		return false, false
	}
	last := first
	if ranged {
		last, err = strconv.Atoi(high)
		if err != nil || last < first || last > 65535 {
			return false, false
		}
	}
	return transport == protocol && port >= first && port <= last, true
}

func zoneFields(config, zone string) (map[string]string, bool) {
	fields := map[string]string{}
	known := map[string]bool{"target": true, "ingress-priority": true, "egress-priority": true, "icmp-block-inversion": true, "interfaces": true, "sources": true, "services": true, "ports": true, "protocols": true, "forward": true, "masquerade": true, "forward-ports": true, "source-ports": true, "icmp-blocks": true, "rich rules": true}
	for index, line := range strings.Split(config, "\n") {
		if index == 0 && len(strings.Fields(line)) != 0 && strings.Fields(line)[0] == zone {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || !known[key] {
			return nil, false
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, false
		}
		fields[key] = strings.TrimSpace(value)
	}
	return fields, true
}
