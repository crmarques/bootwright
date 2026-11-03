package preflight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

const grantCorpus = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/ingress_firewall_grants.json"

type grantCase struct {
	Case      string
	Family    string
	Protocols []string
	Ports     [][2]any
	Zone      []string
	Missing   []string
}

type selectionCase struct {
	Case       string
	Addresses  [][3]any
	Networks   []string
	Interfaces []string
}

type grantTable struct {
	Services  map[string][]string
	Cases     []grantCase
	Selection []selectionCase
}

func readGrants(t *testing.T) grantTable {
	t.Helper()
	data, err := os.ReadFile(grantCorpus)
	if err != nil {
		t.Fatal(err)
	}
	var table grantTable
	if err := json.Unmarshal(data, &table); err != nil || len(table.Cases) == 0 {
		t.Fatalf("the grant corpus is %+v (%v)", table, err)
	}
	return table
}

func lines(text []string) string { return strings.Join(text, "\n") + "\n" }

func portsOf(t *testing.T, raw [][2]any) []HostPort {
	t.Helper()
	out := []HostPort{}
	for _, entry := range raw {
		number, ok := entry[0].(float64)
		transport, named := entry[1].(string)
		if !ok || !named {
			t.Fatalf("the corpus port %v is malformed", entry)
		}
		out = append(out, HostPort{Number: int(number), Transport: transport})
	}
	return out
}

func TestZoneGrantsFollowTheSharedCorpus(t *testing.T) {
	table := readGrants(t)
	for _, c := range table.Cases {
		t.Run(c.Case, func(t *testing.T) {
			read := []string{}
			missing, err := zoneMissing(lines(c.Zone), func(name string) (string, error) {
				read = append(read, name)
				service, found := table.Services[name]
				if !found {
					return "", fmt.Errorf("no service %s", name)
				}
				return lines(service), nil
			}, c.Family, c.Protocols, portsOf(t, c.Ports))
			if err != nil || !slices.Equal(missing, c.Missing) {
				t.Fatalf("missing %q (%v), want %q", missing, err, c.Missing)
			}
		})
	}
}

func TestInterfaceSelectionFollowsTheSharedCorpus(t *testing.T) {
	table := readGrants(t)
	if len(table.Selection) == 0 {
		t.Fatal("the corpus has no selection cases")
	}
	for _, c := range table.Selection {
		t.Run(c.Case, func(t *testing.T) {
			rows := []map[string]any{}
			for _, entry := range c.Addresses {
				address := map[string]any{"family": "inet", "local": entry[1], "prefixlen": entry[2]}
				if strings.Contains(entry[1].(string), ":") {
					address["family"] = "inet6"
				}
				rows = append(rows, map[string]any{"ifname": entry[0], "addr_info": []any{address}})
			}
			data, err := json.Marshal(rows)
			if err != nil {
				t.Fatal(err)
			}
			addresses, err := interfaceAddresses(data)
			if err != nil {
				t.Fatal(err)
			}
			names, _, err := carrierInterfaces(addresses, c.Networks)
			if err != nil || !slices.Equal(names, c.Interfaces) {
				t.Fatalf("selected %q (%v), want %q", names, err, c.Interfaces)
			}
		})
	}
}

func TestActiveZonesMapBoundInterfacesOnly(t *testing.T) {
	zones, err := parseActiveZones("public (default)\n  interfaces: eth0 eth2\ninternal\n  interfaces: eth1\n  sources: 192.0.2.0/24\ndmz\n  sources: 203.0.113.0/24\n")
	if err != nil || !maps.Equal(zones, map[string]string{"eth0": "public", "eth2": "public", "eth1": "internal"}) {
		t.Fatalf("%v %v", zones, err)
	}
	for _, text := range []string{"  interfaces: eth0\n", "public\n  interfaces: eth0\ninternal\n  interfaces: eth0\n", "public\n  bogus: x\n", "bad/zone\n"} {
		if _, err := parseActiveZones(text); !errors.Is(err, errFirewallOutput) {
			t.Fatalf("%q read %v", text, err)
		}
	}
}

func TestZoneWithoutTargetIsUnreadable(t *testing.T) {
	if _, err := zoneMissing("public\n  services: ssh\n", nil, "ipv4", []string{"vrrp"}, nil); !errors.Is(err, errFirewallOutput) {
		t.Fatalf("a zone without a target read %v", err)
	}
}

type scriptedHost struct {
	state     string
	addresses string
	zones     map[string]string
	infos     map[string]string
	services  map[string]string
	fail      map[string]bool
	argv      [][]string
}

func (h *scriptedHost) read(argv []string) ([]byte, error) {
	h.argv = append(h.argv, argv)
	key := strings.Join(argv, " ")
	if h.fail[key] {
		return nil, errors.New("the command exited nonzero")
	}
	switch {
	case key == "/usr/bin/systemctl show --property=ActiveState --value firewalld.service":
		return []byte(h.state + "\n"), nil
	case key == "/usr/sbin/ip -json address show":
		return []byte(h.addresses), nil
	case key == firewallCmd+" --get-default-zone":
		return []byte("public\n"), nil
	case key == firewallCmd+" --get-active-zones":
		text := ""
		for _, name := range slices.Sorted(maps.Keys(h.zones)) {
			text += h.zones[name] + "\n  interfaces: " + name + "\n"
		}
		return []byte(text), nil
	}
	option, value, _ := strings.Cut(argv[len(argv)-1], "=")
	switch {
	case len(argv) != 2 || argv[0] != firewallCmd:
	case option == "--info-zone":
		return []byte(h.infos[value]), nil
	case option == "--info-service":
		return []byte(h.services[value]), nil
	}
	return nil, fmt.Errorf("the probe ran %v", argv)
}

const scriptedAddresses = `[{"ifname":"lo","addr_info":[{"family":"inet","local":"127.0.0.1","prefixlen":8}]},` +
	`{"ifname":"eth0","addr_info":[{"family":"inet","local":"192.0.2.12","prefixlen":24},{"family":"inet6","local":"fe80::1","prefixlen":64}]},` +
	`{"ifname":"eth1","addr_info":[{"family":"inet","local":"198.51.100.12","prefixlen":24}]}]`

func managementRequirement() HostPortRequirement {
	return HostPortRequirement{Machine: "guest", Publisher: "ceph-service-example-mgmt-gateway", Networks: []string{"192.0.2.60/32"}, Protocols: []string{"vrrp"},
		Ports: []HostPort{{Number: 9443, Transport: "tcp"}, {Number: 29443, Transport: "tcp"}}}
}

func zoneInfo(table grantTable, name string) string {
	for _, c := range table.Cases {
		if c.Case == name {
			return lines(c.Zone)
		}
	}
	return ""
}

func servicesOf(table grantTable) map[string]string {
	out := map[string]string{}
	for name, text := range table.Services {
		out[name] = lines(text)
	}
	return out
}

func TestFirewallProbeReportsWhatTheZoneDoesNotPermit(t *testing.T) {
	table := readGrants(t)
	host := &scriptedHost{state: "active", addresses: scriptedAddresses, zones: map[string]string{"eth0": "public"},
		infos: map[string]string{"public": zoneInfo(table, "a management ingress without the internal port")}, services: servicesOf(table)}
	check := firewallCheck(context.Background(), host.read, "guest", []HostPortRequirement{managementRequirement()})
	if check.Status != capacity.Failed || check.ID != "host.firewall" ||
		check.Reason != "interface eth0 in firewalld zone public does not permit 29443/tcp that ceph-service-example-mgmt-gateway publishes" ||
		!strings.Contains(check.Next, "/usr/bin/firewall-cmd --permanent --zone=public --add-port=29443/tcp and /usr/bin/firewall-cmd --reload") ||
		!strings.Contains(check.Next, "customizations.security.firewall.enabled: true") {
		t.Fatalf("the probe reported %+v", check)
	}
	host.infos["public"] = zoneInfo(table, "nothing permitted")
	check = firewallCheck(context.Background(), host.read, "guest", []HostPortRequirement{managementRequirement()})
	if check.Status != capacity.Failed || !strings.Contains(check.Next, "--add-protocol=vrrp --add-port=9443/tcp --add-port=29443/tcp") {
		t.Fatalf("the probe reported %+v", check)
	}
	host.infos["public"] = zoneInfo(table, "a named service grants a management ingress")
	if check := firewallCheck(context.Background(), host.read, "guest", []HostPortRequirement{managementRequirement()}); check.Status != capacity.Passed {
		t.Fatalf("a named service did not grant: %+v", check)
	}
	for _, argv := range host.argv {
		joined := strings.Join(argv, " ")
		for _, word := range []string{"--permanent", "--add", "--remove", "--reload", "--set", "--change"} {
			if strings.Contains(joined, word) {
				t.Fatalf("the probe ran %v", argv)
			}
		}
	}
}

func TestFirewallProbePassesAnInactiveFirewalldAndAHostWithoutTheNetwork(t *testing.T) {
	for _, state := range []string{"inactive", "failed"} {
		host := &scriptedHost{state: state}
		if check := firewallCheck(context.Background(), host.read, "guest", []HostPortRequirement{managementRequirement()}); check.Status != capacity.Passed || len(host.argv) != 1 {
			t.Fatalf("an %s firewalld reported %+v after %v", state, check, host.argv)
		}
	}
	host := &scriptedHost{state: "activating"}
	if check := firewallCheck(context.Background(), host.read, "guest", []HostPortRequirement{managementRequirement()}); check.Status != capacity.Failed || check.Reason != "firewalld.service is activating" {
		t.Fatalf("an unsettled firewalld reported %+v", check)
	}
	elsewhere := managementRequirement()
	elsewhere.Networks = []string{"203.0.113.50/32"}
	host = &scriptedHost{state: "active", addresses: scriptedAddresses}
	if check := firewallCheck(context.Background(), host.read, "guest", []HostPortRequirement{elsewhere}); check.Status != capacity.Passed || len(host.argv) != 2 {
		t.Fatalf("a host without the network reported %+v after %v", check, host.argv)
	}
}

func TestFirewallProbeFollowsInterfaceNetworksAndTheDefaultZone(t *testing.T) {
	table := readGrants(t)
	requirement := HostPortRequirement{Machine: "guest", Publisher: "ceph-service-example-nfs-files", Networks: []string{"203.0.113.50/32", "198.51.100.0/24"},
		Protocols: []string{"vrrp"}, Ports: []HostPort{{Number: 2049, Transport: "tcp"}}}
	host := &scriptedHost{state: "active", addresses: scriptedAddresses, zones: map[string]string{}, infos: map[string]string{"public": zoneInfo(table, "nothing permitted")}, services: servicesOf(table)}
	check := firewallCheck(context.Background(), host.read, "guest", []HostPortRequirement{requirement})
	if check.Status != capacity.Failed || !strings.HasPrefix(check.Reason, "interface eth1 in firewalld zone public does not permit VRRP (IP protocol 112) and 2049/tcp") {
		t.Fatalf("the probe reported %+v", check)
	}
	if !slices.ContainsFunc(host.argv, func(argv []string) bool { return slices.Equal(argv, []string{firewallCmd, "--get-default-zone"}) }) {
		t.Fatalf("an unbound interface did not read the default zone: %v", host.argv)
	}
}

func TestFirewallProbeIsUnknownWhenAReadFails(t *testing.T) {
	for _, failing := range []string{"/usr/bin/systemctl show --property=ActiveState --value firewalld.service", "/usr/sbin/ip -json address show", firewallCmd + " --get-active-zones", firewallCmd + " --info-zone=public"} {
		host := &scriptedHost{state: "active", addresses: scriptedAddresses, zones: map[string]string{"eth0": "public"}, fail: map[string]bool{failing: true}}
		if check := firewallCheck(context.Background(), host.read, "guest", []HostPortRequirement{managementRequirement()}); check.Status != capacity.Unknown {
			t.Fatalf("a failed %s reported %+v", failing, check)
		}
	}
	host := &scriptedHost{state: "active", addresses: `[{"ifname":"eth0","addr_info":[{"family":"inet","local":"192.0.2.12"}]}]`}
	if check := firewallCheck(context.Background(), host.read, "guest", []HostPortRequirement{managementRequirement()}); check.Status != capacity.Unknown {
		t.Fatalf("an unexpected address form reported %+v", check)
	}
}

func TestGuestChecksAddTheFirewallProbeForAnIngressHost(t *testing.T) {
	table := readGrants(t)
	host := &scriptedHost{state: "active", addresses: scriptedAddresses, zones: map[string]string{"eth0": "public"},
		infos: map[string]string{"public": zoneInfo(table, "a management ingress with both ports")}, services: servicesOf(table)}
	machine := api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue())
	checker := GuestChecks{Commands: CommandFunc(func(_ context.Context, request CommandRequest) ([]byte, error) {
		if request.Machine != "guest" || request.ContextName != "synthetic" {
			t.Fatal(request)
		}
		if strings.HasPrefix(request.Argv[0], "/usr/bin/systemctl") || strings.HasPrefix(request.Argv[0], "/usr/sbin/ip") || request.Argv[0] == firewallCmd {
			return host.read(request.Argv)
		}
		return []byte("guest\n"), nil
	})}
	rows, err := checker.CheckGuest(context.Background(), "synthetic", machine, []HostPortRequirement{managementRequirement()}, PreflightRequest{})
	if err != nil || len(rows) != 8 || rows[7].ID != "host.firewall" || rows[7].Status != capacity.Passed {
		t.Fatalf("%+v %v", rows, err)
	}
	rows, err = checker.CheckGuest(context.Background(), "synthetic", machine, nil, PreflightRequest{})
	if err != nil || len(rows) != 7 {
		t.Fatalf("a host without requirements probed %+v %v", rows, err)
	}
}

func TestGuestFirewallReadsRunAsRootBecauseFirewalldRefusesZoneReadsToAUser(t *testing.T) {
	table := readGrants(t)
	for _, c := range []struct {
		name       string
		root, sudo bool
		privilege  string
		firewall   string
	}{
		{"a root access user reads bare", true, false, capacity.Passed, capacity.Passed},
		{"a user access user reads through sudo -n", false, true, capacity.Passed, capacity.Passed},
		{"a user without sudo leaves the row unknown", false, false, capacity.Unknown, capacity.Unknown},
	} {
		t.Run(c.name, func(t *testing.T) {
			host := &scriptedHost{state: "active", addresses: scriptedAddresses, zones: map[string]string{"eth0": "public"},
				infos: map[string]string{"public": zoneInfo(table, "a management ingress with both ports")}, services: servicesOf(table)}
			refusesUser := func(argv []string) ([]byte, error) {
				if argv[0] == firewallCmd {
					return nil, errors.New("NotAuthorizedException: Not Authorized(uid): org.fedoraproject.FirewallD1.info")
				}
				return host.read(argv)
			}
			if check := firewallCheck(context.Background(), refusesUser, "guest", []HostPortRequirement{managementRequirement()}); check.Status != capacity.Unknown {
				t.Fatalf("a refused zone read reported %+v", check)
			}
			bare := [][]string{}
			checker := GuestChecks{Commands: CommandFunc(func(_ context.Context, request CommandRequest) ([]byte, error) {
				argv, elevated := request.Argv, false
				if len(argv) > 2 && argv[0] == sudoCmd && argv[1] == "-n" {
					if !c.sudo {
						return nil, errors.New("sudo: a password is required")
					}
					argv, elevated = argv[2:], true
				}
				switch {
				case argv[0] == "/usr/bin/id":
					if c.root || elevated {
						return []byte("0\n"), nil
					}
					return []byte("1000\n"), nil
				case argv[0] == firewallCmd && !c.root && !elevated:
					bare = append(bare, argv)
					return refusesUser(argv)
				case argv[0] == firewallCmd || argv[0] == "/usr/bin/systemctl" || argv[0] == "/usr/sbin/ip":
					if !elevated {
						bare = append(bare, argv)
					}
					return host.read(argv)
				}
				return []byte("guest\n"), nil
			})}
			rows, err := checker.CheckGuest(context.Background(), "synthetic", api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue()), []HostPortRequirement{managementRequirement()}, PreflightRequest{})
			if err != nil || len(rows) != 8 || rows[5].ID != "host.privilege" || rows[5].Status != c.privilege || rows[7].ID != "host.firewall" || rows[7].Status != c.firewall {
				t.Fatalf("%+v %v", rows, err)
			}
			if c.sudo && slices.ContainsFunc(bare, func(argv []string) bool { return argv[0] == firewallCmd }) {
				t.Fatalf("a user access user ran a bare firewall read: %v", bare)
			}
			if c.root && !slices.ContainsFunc(bare, func(argv []string) bool { return strings.HasPrefix(argv[len(argv)-1], "--info-zone=") }) {
				t.Fatalf("a root access user did not read its zone bare: %v", bare)
			}
		})
	}
}

func TestServicePassesEachHostOnlyItsOwnRequirements(t *testing.T) {
	service := testService()
	service.Ownership = ownedStub{"Machine/guest": {Verb: "apply", State: "done"}}
	other := managementRequirement()
	other.Machine = "elsewhere"
	service.HostPorts = func(api.Catalog) ([]HostPortRequirement, error) {
		return []HostPortRequirement{managementRequirement(), other}, nil
	}
	seen := [][]HostPortRequirement{}
	service.Guests = guestRequirements(func(ports []HostPortRequirement) { seen = append(seen, ports) })
	if _, err := service.Check(context.Background(), PreflightRequest{ContextName: "synthetic"}); err != nil && !strings.Contains(err.Error(), "storage readiness") {
		t.Fatal(err)
	}
	if len(seen) != 1 || len(seen[0]) != 1 || seen[0][0].Machine != "guest" {
		t.Fatalf("the guest received %+v", seen)
	}
	service.HostPorts = func(api.Catalog) ([]HostPortRequirement, error) { return nil, errors.New("refused") }
	if _, err := service.Check(context.Background(), PreflightRequest{ContextName: "synthetic"}); err == nil {
		t.Fatal("a refused publication passed")
	}
}

type guestRequirements func([]HostPortRequirement)

func (f guestRequirements) CheckGuest(_ context.Context, _ string, host api.Object, ports []HostPortRequirement, _ PreflightRequest) ([]Check, error) {
	f(ports)
	return []Check{{ID: "host.identity", Target: host.Name(), Status: capacity.Passed}}, nil
}
