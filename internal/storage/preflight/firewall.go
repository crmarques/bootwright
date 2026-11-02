package preflight

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type HostPort struct {
	Number    int
	Transport string
}

type HostPortRequirement struct {
	Machine   string
	Publisher string
	Networks  []string
	Protocols []string
	Ports     []HostPort
}

const (
	firewallUnit  = "firewalld.service"
	firewallCmd   = "/usr/bin/firewall-cmd"
	maxInterfaces = 256
	maxAddresses  = 64
	maxCandidates = 8
	maxServices   = 64
)

var (
	errFirewallOutput = errors.New("the firewall read returned an unexpected form")
	unitState         = regexp.MustCompile(`^[a-z-]{1,32}$`)
	interfaceName     = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)
	zoneName          = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,64}$`)
	portToken         = regexp.MustCompile(`^([0-9]{1,5})(?:-([0-9]{1,5}))?/(tcp|udp)$`)
	richProtocol      = regexp.MustCompile(`^rule (?:family="(ipv4|ipv6)" )?protocol value="([a-z0-9-]{1,32})" accept$`)
	richPort          = regexp.MustCompile(`^rule (?:family="(ipv4|ipv6)" )?port port="([0-9]{1,5})(?:-([0-9]{1,5}))?" protocol="(tcp|udp)" accept$`)
	protocolNumbers   = map[string]string{"vrrp": "112"}
	protocolLabels    = map[string]string{"vrrp": "VRRP (IP protocol 112)"}
)

type firewallFields struct {
	fields map[string]string
	rules  []string
}

func parseFirewallFields(text string) firewallFields {
	out := firewallFields{fields: map[string]string{}}
	lines := strings.Split(text, "\n")
	rich := false
	for _, line := range lines[min(1, len(lines)):] {
		if strings.HasPrefix(line, "\t") {
			if rich {
				out.rules = append(out.rules, strings.TrimSpace(line[1:]))
			}
			continue
		}
		rich = false
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.Contains(line, ":") {
			key, value, _ := strings.Cut(line[2:], ":")
			out.fields[key] = strings.TrimSpace(value)
			rich = key == "rich rules"
		}
	}
	return out
}

func protocolGranted(tokens []string, protocol string) bool {
	return slices.Contains(tokens, protocol) || protocolNumbers[protocol] != "" && slices.Contains(tokens, protocolNumbers[protocol])
}

func inRange(port int, low, high string) bool {
	if high == "" {
		high = low
	}
	from, errFrom := strconv.Atoi(low)
	to, errTo := strconv.Atoi(high)
	return errFrom == nil && errTo == nil && from <= port && port <= to
}

func familyMatches(family, wanted string) bool { return family == "" || family == wanted }

func grantsProtocol(f firewallFields, family, protocol string) bool {
	if protocolGranted(strings.Fields(f.fields["protocols"]), protocol) {
		return true
	}
	for _, rule := range f.rules {
		if found := richProtocol.FindStringSubmatch(rule); found != nil && familyMatches(found[1], family) && protocolGranted([]string{found[2]}, protocol) {
			return true
		}
	}
	return false
}

func grantsPort(f firewallFields, family string, port HostPort) bool {
	for _, token := range strings.Fields(f.fields["ports"]) {
		if found := portToken.FindStringSubmatch(token); found != nil && found[3] == port.Transport && inRange(port.Number, found[1], found[2]) {
			return true
		}
	}
	for _, rule := range f.rules {
		if found := richPort.FindStringSubmatch(rule); found != nil && familyMatches(found[1], family) && found[4] == port.Transport && inRange(port.Number, found[2], found[3]) {
			return true
		}
	}
	return false
}

func zoneMissing(zone string, service func(string) (string, error), family string, protocols []string, ports []HostPort) ([]string, error) {
	f := parseFirewallFields(zone)
	target, found := f.fields["target"]
	if !found {
		return nil, errFirewallOutput
	}
	if target == "ACCEPT" {
		return []string{}, nil
	}
	protocolMet := make([]bool, len(protocols))
	portMet := make([]bool, len(ports))
	for i, protocol := range protocols {
		protocolMet[i] = grantsProtocol(f, family, protocol)
	}
	for i, port := range ports {
		portMet[i] = grantsPort(f, family, port)
	}
	unmet := func() bool { return slices.Contains(protocolMet, false) || slices.Contains(portMet, false) }
	queue, seen, read := strings.Fields(f.fields["services"]), map[string]bool{}, 0
	for len(queue) > 0 && unmet() {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		if !zoneName.MatchString(name) || read >= maxServices {
			return nil, errFirewallOutput
		}
		read++
		text, err := service(name)
		if err != nil {
			return nil, err
		}
		s := parseFirewallFields(text)
		if s.fields["destination"] == "" {
			granted := firewallFields{fields: s.fields}
			for i, protocol := range protocols {
				protocolMet[i] = protocolMet[i] || grantsProtocol(granted, family, protocol)
			}
			for i, port := range ports {
				portMet[i] = portMet[i] || grantsPort(granted, "", port)
			}
		}
		queue = append(queue, strings.Fields(s.fields["includes"])...)
	}
	missing := []string{}
	for i, protocol := range protocols {
		if !protocolMet[i] {
			label := protocolLabels[protocol]
			if label == "" {
				label = "IP protocol " + protocol
			}
			missing = append(missing, label)
		}
	}
	for i, port := range ports {
		if !portMet[i] {
			missing = append(missing, strconv.Itoa(port.Number)+"/"+port.Transport)
		}
	}
	return missing, nil
}

type interfaceAddress struct {
	name   string
	prefix netip.Prefix
}

func interfaceAddresses(data []byte) ([]interfaceAddress, error) {
	var rows []struct {
		Name      *string `json:"ifname"`
		Addresses []struct {
			Family string  `json:"family"`
			Local  *string `json:"local"`
			Prefix *int    `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) > maxInterfaces {
		return nil, errFirewallOutput
	}
	out := []interfaceAddress{}
	for _, row := range rows {
		if row.Name == nil || row.Addresses == nil || len(row.Addresses) > maxAddresses {
			return nil, errFirewallOutput
		}
		for _, address := range row.Addresses {
			if address.Family != "inet" && address.Family != "inet6" || address.Local == nil || address.Prefix == nil {
				return nil, errFirewallOutput
			}
			parsed, err := netip.ParseAddr(*address.Local)
			if err != nil {
				return nil, errFirewallOutput
			}
			prefix, err := parsed.WithZone("").Prefix(*address.Prefix)
			if err != nil {
				return nil, errFirewallOutput
			}
			out = append(out, interfaceAddress{name: *row.Name, prefix: prefix})
		}
	}
	return out, nil
}

func carrierInterfaces(addresses []interfaceAddress, networks []string) ([]string, netip.Prefix, error) {
	parsed := []netip.Prefix{}
	for _, network := range networks {
		prefix, err := netip.ParsePrefix(network)
		if err != nil {
			return nil, netip.Prefix{}, errFirewallOutput
		}
		parsed = append(parsed, prefix.Masked())
	}
	if len(parsed) == 0 {
		return nil, netip.Prefix{}, errFirewallOutput
	}
	pick := func(match func(netip.Prefix) bool) []string {
		names := []string{}
		for _, address := range addresses {
			if match(address.prefix.Masked()) && !slices.Contains(names, address.name) {
				names = append(names, address.name)
			}
		}
		return names
	}
	first := parsed[0]
	names := pick(func(network netip.Prefix) bool { return network.Contains(first.Addr()) })
	if len(names) == 0 {
		names = pick(func(network netip.Prefix) bool { return slices.Contains(parsed[1:], network) })
	}
	slices.Sort(names)
	if len(names) > maxCandidates || slices.ContainsFunc(names, func(name string) bool { return !interfaceName.MatchString(name) }) {
		return nil, netip.Prefix{}, errFirewallOutput
	}
	return names, first, nil
}

type firewallReader func([]string) ([]byte, error)

func (read firewallReader) text(argv ...string) (string, error) {
	data, err := read(argv)
	text := string(data)
	clear(data)
	return text, err
}

func firewallCheck(ctx context.Context, read firewallReader, target string, requirements []HostPortRequirement) Check {
	check := Check{ID: "host.firewall", Target: target, Status: capacity.Unknown, Reason: "the bounded firewall read did not complete",
		Next: "prove that this host's firewalld permits what its Ceph ingress publishes"}
	clauses, commands, err := firewallClauses(ctx, read, requirements)
	switch {
	case err != nil:
		return check
	case len(clauses) == 0:
		check.Status, check.Reason, check.Next = capacity.Passed, "the host's firewall permits every published ingress requirement", ""
	default:
		check.Status = capacity.Failed
		check.Reason = strings.Join(clauses, "; ")
		check.Next = "either permit them, for example with " + strings.Join(commands, " and ") + " and " + firewallCmd + " --reload, " +
			"or let Bootwright realize them by installing this Machine with customizations.security.firewall.enabled: true, then repeat preflight storage-cluster"
	}
	return check
}

func firewallClauses(ctx context.Context, read firewallReader, requirements []HostPortRequirement) ([]string, []string, error) {
	state, err := read.text("/usr/bin/systemctl", "show", "--property=ActiveState", "--value", firewallUnit)
	if err != nil {
		return nil, nil, err
	}
	switch state = strings.TrimSpace(state); {
	case state == "inactive" || state == "failed":
		return nil, nil, nil
	case state != "active" && state != "reloading":
		shown := "unknown"
		if unitState.MatchString(state) {
			shown = state
		}
		return []string{firewallUnit + " is " + shown}, []string{"systemctl start " + firewallUnit}, nil
	}
	data, err := read([]string{"/usr/sbin/ip", "-json", "address", "show"})
	if err != nil {
		return nil, nil, err
	}
	addresses, err := interfaceAddresses(data)
	clear(data)
	if err != nil {
		return nil, nil, err
	}
	var zones map[string]string
	infos, services := map[string]string{}, map[string]string{}
	clauses, commands := []string{}, []string{}
	for _, requirement := range requirements {
		names, network, err := carrierInterfaces(addresses, requirement.Networks)
		if err != nil {
			return nil, nil, err
		}
		family := "ipv4"
		if network.Addr().Is6() {
			family = "ipv6"
		}
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if zones == nil {
				if zones, err = interfaceZones(read); err != nil {
					return nil, nil, err
				}
			}
			zone := zones[name]
			if zone == "" {
				if zone, err = read.text(firewallCmd, "--get-default-zone"); err != nil {
					return nil, nil, err
				}
				if zone = strings.TrimSpace(zone); !zoneName.MatchString(zone) {
					return nil, nil, errFirewallOutput
				}
				zones[name] = zone
			}
			info, known := infos[zone]
			if !known {
				if info, err = read.text(firewallCmd, "--info-zone="+zone); err != nil {
					return nil, nil, err
				}
				infos[zone] = info
			}
			missing, err := zoneMissing(info, func(service string) (string, error) {
				if text, known := services[service]; known {
					return text, nil
				}
				text, err := read.text(firewallCmd, "--info-service="+service)
				services[service] = text
				return text, err
			}, family, requirement.Protocols, requirement.Ports)
			if err != nil {
				return nil, nil, err
			}
			if len(missing) == 0 {
				continue
			}
			clauses = append(clauses, "interface "+name+" in firewalld zone "+zone+" does not permit "+strings.Join(missing, " and ")+" that "+requirement.Publisher+" publishes")
			commands = append(commands, remedyCommand(zone, requirement, missing))
		}
	}
	return clauses, slices.Compact(commands), nil
}

func interfaceZones(read firewallReader) (map[string]string, error) {
	text, err := read.text(firewallCmd, "--get-active-zones")
	if err != nil {
		return nil, err
	}
	return parseActiveZones(text)
}

func parseActiveZones(text string) (map[string]string, error) {
	zones, zone := map[string]string{}, ""
	for _, line := range strings.Split(text, "\n") {
		switch trimmed := strings.TrimSpace(line); {
		case trimmed == "":
		case !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t"):
			zone = strings.Fields(trimmed)[0]
			if !zoneName.MatchString(zone) {
				return nil, errFirewallOutput
			}
		case zone == "":
			return nil, errFirewallOutput
		case strings.HasPrefix(trimmed, "interfaces:"):
			for _, name := range strings.Fields(strings.TrimPrefix(trimmed, "interfaces:")) {
				if !interfaceName.MatchString(name) || zones[name] != "" {
					return nil, errFirewallOutput
				}
				zones[name] = zone
			}
		case strings.HasPrefix(trimmed, "sources:"):
		default:
			return nil, errFirewallOutput
		}
	}
	return zones, nil
}

func remedyCommand(zone string, requirement HostPortRequirement, missing []string) string {
	command := firewallCmd + " --permanent --zone=" + zone
	for _, protocol := range requirement.Protocols {
		label := protocolLabels[protocol]
		if label == "" {
			label = "IP protocol " + protocol
		}
		if slices.Contains(missing, label) {
			command += " --add-protocol=" + protocol
		}
	}
	for _, port := range requirement.Ports {
		if token := strconv.Itoa(port.Number) + "/" + port.Transport; slices.Contains(missing, token) {
			command += " --add-port=" + token
		}
	}
	return command
}
