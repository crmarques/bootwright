package installation

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// declaredInterface is one native NMState interface of a network fixture.
func declaredInterface(name, kind string, fields ...api.FieldValue) api.Value {
	return api.MapValue(append([]api.FieldValue{text("name", name), text("type", kind)}, fields...)...)
}

func bondPort(name, bond string) api.Value {
	return declaredInterface(name, "ethernet", text("controller", bond))
}

// bondInterface is a bond aggregating the named ports, with a mode and
// options unless they are absent.
func bondInterface(name string, mode, options api.Value, ports ...string) api.Value {
	aggregation := []api.FieldValue{field("port", api.StringList(ports...))}
	if mode.Present() {
		aggregation = append(aggregation, field("mode", mode))
	}
	if options.Present() {
		aggregation = append(aggregation, field("options", options))
	}
	return declaredInterface(name, "bond", field("link-aggregation", api.MapValue(aggregation...)))
}

func activeBackup(name string, ports ...string) api.Value {
	return bondInterface(name, api.StringValue("active-backup"), api.Value{}, ports...)
}

func vlanInterface(name, base, id string, fields ...api.FieldValue) api.Value {
	return declaredInterface(name, "vlan", field("vlan", api.MapValue(append([]api.FieldValue{text("base-iface", base), number("id", id)}, fields...)...)))
}

// installingOn is the lab guest with its install address on iface.
func installingOn(iface string) api.Object {
	return guest(field("network", guest().Spec().Get("network").With("addresses", api.ListValue(
		api.MapValue(text("name", "fqdn"), text("address", "rhel-01.lab.example.test")),
		api.MapValue(text("name", "ip"), text("address", "198.51.100.11/24"), text("interface", iface)),
	))))
}

// externalProvider is the lab provider attached to a bridge its host owns,
// which carries the VLAN a VLAN install interface is tagged with.
func externalProvider() api.Object {
	lab := provider()
	return lab.WithSpec(lab.Spec().With("networkAttachments", api.ListValue(api.MapValue(
		text("name", "lab-guests"), field("libvirt", api.MapValue(text("bridge", "br-lab"), text("management", "external"))),
	))))
}

func hardwareNIC(name, mac string) api.Value {
	return api.MapValue(text("name", name), text("macAddress", mac))
}

// metalOn is the bare-metal Machine on its own network configuration, with
// its install address on iface and the hardware NICs given.
func metalOn(iface string, nics ...api.Value) api.Object {
	metal := server(api.MapValue(text("deviceName", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4")))
	return metal.WithSpec(metal.Spec().
		WithPath(api.ListValue(nics...), "hardware", "nics").
		WithPath(api.StringValue("metal-net"), "network", "configRef").
		WithPath(api.ListValue(
			api.MapValue(text("name", "fqdn"), text("address", "metal-01.lab.example.test")),
			api.MapValue(text("name", "ip"), text("address", "198.51.100.41/24"), text("interface", iface)),
		), "network", "addresses"))
}

func metalNetwork(interfaces ...api.Value) api.Object {
	return api.NewObject(api.NetworkConfig, "metal-net", api.Value{}, networkWith(interfaces...).Spec())
}

func metalCatalog(metal, network api.Object) api.Catalog {
	return labCatalog(metalProvider(), metal, network, sshKeySecret(api.MapValue(field("generated", api.MapValue()))))
}

func requestOf(t *testing.T, catalog api.Catalog, object string) Request {
	t.Helper()
	requests, _, err := Requests(catalog, "controller", testContext)
	if err != nil {
		t.Fatalf("requests: %v", diagnostics.Of(err))
	}
	for _, request := range requests {
		if request.Identity.Object == object {
			return request
		}
	}
	t.Fatalf("no request for %s", object)
	return Request{}
}

func networkLines(kickstart string) []string {
	var lines []string
	for _, line := range strings.Split(kickstart, "\n") {
		if strings.HasPrefix(line, "network ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func networkLineOf(t *testing.T, request Request) string {
	t.Helper()
	lines := networkLines(request.Kickstart)
	if len(lines) != 1 {
		t.Fatalf("the Kickstart has %d network lines:\n%s", len(lines), request.Kickstart)
	}
	return lines[0]
}

func expectNetworkRefusal(t *testing.T, catalog api.Catalog, reason, remediation string) {
	t.Helper()
	want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01", Reason: reason, Remediation: remediation}}
	if refused := Refusals(catalog); !slices.Equal(refused, want) {
		t.Fatalf("refusals = %+v, want %+v", refused, want)
	}
}

func expectNoRefusal(t *testing.T, catalog api.Catalog) {
	t.Helper()
	if refused := Refusals(catalog); len(refused) != 0 {
		t.Fatalf("refusals = %+v", refused)
	}
}

func bondedGuest(bond api.Value) api.Catalog {
	return labCatalog(installingOn("bond0"), networkWith(bondPort("enp1s0", "bond0"), bondPort("enp2s0", "bond0"), bond))
}

func TestABondInstallInterfaceRendersItsPortsAndMode(t *testing.T) {
	catalog := bondedGuest(bondInterface("bond0", api.StringValue("active-backup"), api.MapValue(number("miimon", "100")), "enp1s0", "enp2s0"))
	expectNoRefusal(t, catalog)
	const want = "network --bootproto=static --device=bond0 --bondslaves=enp1s0,enp2s0 --bondopts=mode=active-backup,miimon=100 --ip=198.51.100.11 " +
		"--netmask=255.255.255.0 --hostname=rhel-01.lab.example.test --onboot=on --activate --gateway=198.51.100.1 --nameserver=192.0.2.1"
	if line := networkLineOf(t, requestOf(t, catalog, "rhel-01")); line != want {
		t.Fatalf("network line = %q, want %q", line, want)
	}
}

func TestAVLANInstallInterfaceRendersOverItsBase(t *testing.T) {
	network := []api.Value{declaredInterface("enp1s0", "ethernet"), vlanInterface("enp1s0.200", "enp1s0", "200")}
	catalog := labCatalog(externalProvider(), installingOn("enp1s0.200"), networkWith(network...))
	expectNoRefusal(t, catalog)
	if line := networkLineOf(t, requestOf(t, catalog, "rhel-01")); !strings.HasPrefix(line, "network --bootproto=static --device=enp1s0 --vlanid=200 --interfacename=enp1s0.200 --ip=198.51.100.11 ") {
		t.Fatalf("network line = %q", line)
	}
	metal := metalCatalog(metalOn("enp1s0.200", hardwareNIC("enp1s0", "52:54:00:9a:1b:01")), metalNetwork(network...))
	if line := networkLineOf(t, requestOf(t, metal, "metal-01")); !strings.HasPrefix(line, "network --bootproto=static --device=enp1s0 --vlanid=200 --interfacename=enp1s0.200 --ip=198.51.100.41 ") {
		t.Fatalf("physical network line = %q", line)
	}
}

func vlanOverBond(extra ...api.Value) []api.Value {
	return append([]api.Value{bondPort("enp1s0", "bond0"), bondPort("enp2s0", "bond0"), activeBackup("bond0", "enp1s0", "enp2s0"),
		vlanInterface("bond0.200", "bond0", "200")}, extra...)
}

func TestAVLANOverBondIsOneLine(t *testing.T) {
	catalog := labCatalog(externalProvider(), installingOn("bond0.200"), networkWith(vlanOverBond()...))
	expectNoRefusal(t, catalog)
	line := networkLineOf(t, requestOf(t, catalog, "rhel-01"))
	if !strings.HasPrefix(line, "network --bootproto=static --device=bond0 --bondslaves=enp1s0,enp2s0 --bondopts=mode=active-backup --vlanid=200 --interfacename=bond0.200 --ip=") {
		t.Fatalf("network line = %q", line)
	}
}

func TestBondOptionsUseASemicolonWhenAValueHasAComma(t *testing.T) {
	options := api.MapValue(number("miimon", "100"), text("arp_ip_target", "192.0.2.1,192.0.2.2"))
	catalog := bondedGuest(bondInterface("bond0", api.StringValue("active-backup"), options, "enp1s0", "enp2s0"))
	expectNoRefusal(t, catalog)
	if line := networkLineOf(t, requestOf(t, catalog, "rhel-01")); !strings.Contains(line, " --bondopts=mode=active-backup;arp_ip_target=192.0.2.1,192.0.2.2;miimon=100 ") {
		t.Fatalf("network line = %q", line)
	}
}

func TestTheInstallLineCarriesNoMTU(t *testing.T) {
	jumbo := number("mtu", "9000")
	network := networkWith(declaredInterface("enp1s0", "ethernet", text("controller", "bond0"), jumbo), declaredInterface("enp2s0", "ethernet", text("controller", "bond0"), jumbo),
		bondInterface("bond0", api.StringValue("active-backup"), api.Value{}, "enp1s0", "enp2s0").With("mtu", api.IntegerValue("9000")),
		vlanInterface("bond0.200", "bond0", "200").With("mtu", api.IntegerValue("9000")))
	request := requestOf(t, labCatalog(externalProvider(), installingOn("bond0.200"), network), "rhel-01")
	if strings.Contains(request.Kickstart, "--mtu") {
		t.Fatalf("the Kickstart carries an MTU:\n%s", request.Kickstart)
	}
}

func TestTheGatewayComesOnlyFromTheInstallInterfacesDefaultRoute(t *testing.T) {
	two := networkWith(declaredInterface("enp1s0", "ethernet"), declaredInterface("enp2s0", "ethernet"))
	route := func(hop, iface string, fields ...api.FieldValue) api.Value {
		return api.MapValue(append([]api.FieldValue{text("destination", "0.0.0.0/0"), text("next-hop-address", hop), text("next-hop-interface", iface)}, fields...)...)
	}
	lone := networkLineOf(t, requestOf(t, labCatalog(withRoutes(two, route("198.51.100.1", "enp2s0"))), "rhel-01"))
	if strings.Contains(lone, "--gateway=") {
		t.Fatalf("a default route through another interface became the gateway: %q", lone)
	}
	later := withRoutes(two, route("198.51.100.9", "enp1s0", text("state", "absent")), route("198.51.100.1", "enp2s0"), route("198.51.100.254", "enp1s0"))
	if line := networkLineOf(t, requestOf(t, labCatalog(later), "rhel-01")); !strings.HasSuffix(line, " --gateway=198.51.100.254 --nameserver=192.0.2.1") {
		t.Fatalf("network line = %q", line)
	}
}

func TestEveryEthernetInterfaceIsPinnedByItsMAC(t *testing.T) {
	network := networkWith(bondPort("enp1s0", "bond0"), bondPort("enp2s0", "bond0"), activeBackup("bond0", "enp1s0", "enp2s0"),
		declaredInterface("enp3s0", "ethernet"), declaredInterface("enp4s0", "ethernet", text("state", "absent")))
	request := requestOf(t, labCatalog(installingOn("bond0"), network), "rhel-01")
	var want []string
	for _, name := range []string{"enp1s0", "enp2s0", "enp3s0"} {
		want = append(want, "ifname="+name+":"+substrate.InterfaceMAC(testContext, "rhel-01", name))
	}
	if !slices.Equal(request.InstallerArguments, want) {
		t.Fatalf("installer arguments = %q, want %q", request.InstallerArguments, want)
	}
	metal := metalOn("enp1s0", hardwareNIC("enp1s0", "52:54:00:9A:1B:01"), hardwareNIC("nic-b", "52:54:00:9a:1b:0b"), hardwareNIC("enp2s0", "52:54:00:9a:1b:0c"))
	metal = metal.WithSpec(metal.Spec().WithPath(api.ListValue(
		api.MapValue(text("interfaceName", "enp1s0"), text("nicRef", "enp1s0")),
		api.MapValue(text("interfaceName", "enp2s0"), text("nicRef", "nic-b")),
	), "network", "interfaceBinding"))
	physical := requestOf(t, metalCatalog(metal, metalNetwork(declaredInterface("enp1s0", "ethernet"), declaredInterface("enp2s0", "ethernet"))), "metal-01")
	if want := []string{"ifname=enp1s0:52:54:00:9a:1b:01", "ifname=enp2s0:52:54:00:9a:1b:0b"}; !slices.Equal(physical.InstallerArguments, want) {
		t.Fatalf("physical installer arguments = %q, want %q", physical.InstallerArguments, want)
	}
}

func TestTheDeviceIsTheDeclaredNameOnEverySubstrate(t *testing.T) {
	rendered, err := RenderKickstart(physicalInstallation())
	if err != nil {
		t.Fatal(err)
	}
	if line := networkLines(rendered); len(line) != 1 || !strings.HasPrefix(line[0], "network --bootproto=static --device=eno1 --ip=") || strings.Contains(line[0], "52:54:00") {
		t.Fatalf("network lines = %q", line)
	}
	if line := networkLineOf(t, requestOf(t, labCatalog(), "rhel-01")); !strings.HasPrefix(line, "network --bootproto=static --device=enp1s0 --ip=") {
		t.Fatalf("network line = %q", line)
	}
}

func kernelReason(name string) (string, string) {
	return "a managed-OS installation names each ethernet interface after its hardware address, and a kernel-style name such as " + name + " can collide with the names the kernel assigns",
		"rename interface " + name + " in the network of Machine/rhel-01 to a name outside the kernel's ethN scheme, such as its predictable name"
}

func TestKernelStyleNamesRefuse(t *testing.T) {
	for _, name := range []string{"eth0", "eth12"} {
		reason, remediation := kernelReason(name)
		expectNetworkRefusal(t, labCatalog(networkWith(declaredInterface("enp1s0", "ethernet"), declaredInterface(name, "ethernet"))), reason, remediation)
	}
	for _, name := range []string{"eth", "ethernet0", "eth0a"} {
		expectNoRefusal(t, labCatalog(networkWith(declaredInterface("enp1s0", "ethernet"), declaredInterface(name, "ethernet"))))
	}
	expectNoRefusal(t, labCatalog(networkWith(declaredInterface("enp1s0", "ethernet"), declaredInterface("eth0", "ethernet", text("state", "absent")))))
}

func TestABareMetalEthernetInterfaceWithoutAHardwareNICRefuses(t *testing.T) {
	unbound := "a managed-OS installation names each ethernet interface after its hardware address, and interface enp2s0 of Machine/metal-01 resolves to no declared hardware NIC, or to one whose address differs from its mac-address"
	remedy := "bind interface enp2s0 to its hardware NIC in spec.network.interfaceBinding of Machine/metal-01, or mark it absent in its network"
	two := metalNetwork(declaredInterface("enp1s0", "ethernet"), declaredInterface("enp2s0", "ethernet"))
	_, _, err := Requests(metalCatalog(metalOn("enp1s0", hardwareNIC("enp1s0", "52:54:00:9a:1b:01")), two), "controller", testContext)
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" || reported[0].Message != unbound || reported[0].Remediation != remedy {
		t.Fatalf("refusal = %#v", reported)
	}
	nics := []api.Value{hardwareNIC("enp1s0", "52:54:00:9a:1b:01"), hardwareNIC("enp2s0", "52:54:00:9a:1b:02")}
	differing := metalNetwork(declaredInterface("enp1s0", "ethernet"), declaredInterface("enp2s0", "ethernet", text("mac-address", "AA-BB-CC-DD-EE-FF")))
	_, _, err = Requests(metalCatalog(metalOn("enp1s0", nics...), differing), "controller", testContext)
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != unbound {
		t.Fatalf("refusal = %#v", reported)
	}
	same := metalNetwork(declaredInterface("enp1s0", "ethernet"), declaredInterface("enp2s0", "ethernet", text("mac-address", "52-54-00-9A-1B-02")))
	request := requestOf(t, metalCatalog(metalOn("enp1s0", nics...), same), "metal-01")
	if !slices.Equal(request.InstallerArguments, []string{"ifname=enp1s0:52:54:00:9a:1b:01", "ifname=enp2s0:52:54:00:9a:1b:02"}) {
		t.Fatalf("installer arguments = %q", request.InstallerArguments)
	}
	malformed := metalCatalog(metalOn("enp1s0", hardwareNIC("enp1s0", "52:54:00:9a:1b:01"), hardwareNIC("enp2s0", "52:54:00:9a:1b")), two)
	want := []lifecycle.Refusal{{Kind: "Machine", Name: "metal-01", Reason: unbound, Remediation: remedy}}
	if refused := Refusals(malformed); !slices.Equal(refused, want) {
		t.Fatalf("refusals = %+v", refused)
	}
}

func bondRefusalOf(row, bond, detail string) (string, string) {
	switch row {
	case "ports":
		return "the installer configures a bonded install interface from its named ports, and bond " + bond + " of Machine/rhel-01 names no port, or a port that is not an available ethernet interface aggregated by it",
			"list the ethernet interfaces bond " + bond + " aggregates in its link-aggregation.port in the network of Machine/rhel-01"
	case "mode":
		return "the installer configures a bond only by its full mode name, and bond " + bond + " of Machine/rhel-01 declares " + detail,
			"set link-aggregation.mode of " + bond + " to one of balance-rr, active-backup, balance-xor, broadcast, 802.3ad, balance-tlb or balance-alb in the network of Machine/rhel-01"
	case "libvirt":
		return "a libvirt bridge qualifies only active-backup bonding, and bond " + bond + " of Machine/rhel-01 declares mode " + detail,
			"set link-aggregation.mode of " + bond + " on Machine/rhel-01 to active-backup; the other modes are qualified on physical hosts (B78)"
	}
	return "the installer receives bond options as one option list, and link-aggregation.options." + detail + " of " + bond + " on Machine/rhel-01 cannot be written into it",
		"remove link-aggregation.options." + detail + " from " + bond + " in the network of Machine/rhel-01, or give it a plain value"
}

func TestBondInstallRefusals(t *testing.T) {
	mode := api.StringValue("active-backup")
	options := func(value api.Value) api.Value { return bondInterface("bond0", mode, value, "enp1s0", "enp2s0") }
	withAggregation := func(aggregation ...api.FieldValue) api.Value {
		return declaredInterface("bond0", "bond", field("link-aggregation", api.MapValue(aggregation...)))
	}
	for name, test := range map[string]struct {
		extra       []api.Value
		row, detail string
	}{
		"no port":                {[]api.Value{withAggregation(field("mode", mode))}, "ports", ""},
		"an empty port list":     {[]api.Value{withAggregation(field("mode", mode), field("port", api.ListValue()))}, "ports", ""},
		"a non-ethernet port":    {[]api.Value{bondInterface("bond0", mode, api.Value{}, "enp1s0", "bond1"), activeBackup("bond1")}, "ports", ""},
		"an absent port":         {[]api.Value{bondInterface("bond0", mode, api.Value{}, "enp1s0", "enp3s0"), declaredInterface("enp3s0", "ethernet", text("state", "absent"))}, "ports", ""},
		"another bond's port":    {[]api.Value{bondInterface("bond0", mode, api.Value{}, "enp1s0", "enp3s0"), bondPort("enp3s0", "bond1")}, "ports", ""},
		"no port and no mode":    {[]api.Value{withAggregation()}, "ports", ""},
		"no mode":                {[]api.Value{bondInterface("bond0", api.Value{}, api.Value{}, "enp1s0", "enp2s0")}, "mode", "no mode"},
		"a numeric mode":         {[]api.Value{bondInterface("bond0", api.IntegerValue("1"), api.Value{}, "enp1s0", "enp2s0")}, "mode", "mode 1"},
		"a short mode name":      {[]api.Value{bondInterface("bond0", api.StringValue("lacp"), api.MapValue(text("Miimon", "1")), "enp1s0", "enp2s0")}, "mode", "mode lacp"},
		"an uppercase option":    {[]api.Value{options(api.MapValue(number("Miimon", "100")))}, "option", "Miimon"},
		"a mode option":          {[]api.Value{options(api.MapValue(text("mode", "balance-rr")))}, "option", "mode"},
		"a list value":           {[]api.Value{options(api.MapValue(field("arp_ip_target", api.StringList("192.0.2.1"))))}, "option", "arp_ip_target"},
		"a semicolon":            {[]api.Value{options(api.MapValue(text("primary", "a;b"), text("zz", "x y")))}, "option", "primary"},
		"a space":                {[]api.Value{options(api.MapValue(text("primary", "a b")))}, "option", "primary"},
		"an equals sign":         {[]api.Value{options(api.MapValue(text("primary", "a=b")))}, "option", "primary"},
		"a quote":                {[]api.Value{options(api.MapValue(text("primary", `a"b`)))}, "option", "primary"},
		"a comment":              {[]api.Value{options(api.MapValue(text("primary", "a#b")))}, "option", "primary"},
		"options not a mapping":  {[]api.Value{options(api.StringValue("miimon=100"))}, "option", "*"},
		"the first option named": {[]api.Value{options(api.MapValue(text("zz", "a b"), text("aa", "a;b")))}, "option", "aa"},
	} {
		t.Run(name, func(t *testing.T) {
			interfaces := append([]api.Value{bondPort("enp1s0", "bond0"), bondPort("enp2s0", "bond0")}, test.extra...)
			reason, remediation := bondRefusalOf(test.row, "bond0", test.detail)
			expectNetworkRefusal(t, labCatalog(installingOn("bond0"), networkWith(interfaces...)), reason, remediation)
		})
	}
	expectNoRefusal(t, bondedGuest(options(api.MapValue(number("miimon", "100"), field("use_carrier", api.BoolValue(true)), api.FieldValue{Name: "updelay", Value: api.NumberValue("1.5")}))))
}

func vlanRefusalOf(iface string) (string, string) {
	return "the installer configures a VLAN install interface only as an 802.1Q VLAN with an id from 1 to 4094 over an available ethernet or bond interface, named vlan and its id, a prefix, a dot and its id, or a name with no dot that does not start with vlan, and " + iface + " of Machine/rhel-01 is not one",
		"declare " + iface + " in the network of Machine/rhel-01 with vlan.base-iface an available ethernet or bond interface, vlan.id from 1 to 4094, no vlan.protocol other than 802.1q, and a name of one of those forms"
}

func TestVLANInstallRefusals(t *testing.T) {
	ethernet := []api.Value{declaredInterface("enp1s0", "ethernet"), declaredInterface("enp2s0", "ethernet", text("state", "absent"))}
	refused := map[string]api.Value{
		"id 0":             vlanInterface("enp1s0.0", "enp1s0", "0"),
		"id 4095":          vlanInterface("enp1s0.4095", "enp1s0", "4095"),
		"a string id":      declaredInterface("enp1s0.8", "vlan", field("vlan", api.MapValue(text("base-iface", "enp1s0"), text("id", "8")))),
		"802.1ad":          vlanInterface("enp1s0.8", "enp1s0", "8", text("protocol", "802.1ad")),
		"QinQ":             vlanInterface("outer.8", "enp1s0.7", "8"),
		"an absent base":   vlanInterface("enp2s0.8", "enp2s0", "8"),
		"another id":       vlanInterface("foo.7", "enp1s0", "8"),
		"another vlan id":  vlanInterface("vlan9", "enp1s0", "8"),
		"two dots":         vlanInterface("a.b.8", "enp1s0", "8"),
		"a dotted vlan":    vlanInterface("vlan.8", "enp1s0", "8"),
		"an undeclared id": declaredInterface("enp1s0.8", "vlan", field("vlan", api.MapValue(text("base-iface", "enp1s0")))),
	}
	for name, declared := range refused {
		t.Run(name, func(t *testing.T) {
			iface := declared.Get("name").Text()
			interfaces := append(slices.Clone(ethernet), declared, vlanInterface("enp1s0.7", "enp1s0", "7"))
			reason, remediation := vlanRefusalOf(iface)
			expectNetworkRefusal(t, labCatalog(externalProvider(), installingOn(iface), networkWith(interfaces...)), reason, remediation)
		})
	}
	for name, declared := range map[string]api.Value{
		"over a bond": vlanInterface("bond0.8", "bond0", "8"), "vlan8": vlanInterface("vlan8", "enp1s0", "8"),
		"a prefix": vlanInterface("uplink.8", "enp1s0", "8"), "no dot": vlanInterface("public", "enp1s0", "8", text("protocol", "802.1q")),
	} {
		t.Run(name, func(t *testing.T) {
			interfaces := []api.Value{bondPort("enp1s0", "bond0"), bondPort("enp2s0", "bond0"), activeBackup("bond0", "enp1s0", "enp2s0"), declared}
			if declared.Get("vlan", "base-iface").Text() != "bond0" {
				interfaces = []api.Value{declaredInterface("enp1s0", "ethernet"), declared}
			}
			catalog := labCatalog(externalProvider(), installingOn(declared.Get("name").Text()), networkWith(interfaces...))
			expectNoRefusal(t, catalog)
			networkLineOf(t, requestOf(t, catalog, "rhel-01"))
		})
	}
}

func TestAVLANInstallOnAManagedLibvirtNetworkRefuses(t *testing.T) {
	network := []api.Value{declaredInterface("enp1s0", "ethernet"), vlanInterface("enp1s0.100", "enp1s0", "100")}
	expectNetworkRefusal(t, labCatalog(installingOn("enp1s0.100"), networkWith(network...)),
		"a managed libvirt network gives its host an untagged address on virbr-lab, so the controller cannot reach the VLAN install address of Machine/rhel-01",
		"attach Machine/rhel-01 to an external bridge whose host carries VLAN 100, or install over an untagged interface")
	expectNoRefusal(t, labCatalog(externalProvider(), installingOn("enp1s0.100"), networkWith(network...)))
	metal := metalCatalog(metalOn("enp1s0.100", hardwareNIC("enp1s0", "52:54:00:9a:1b:01")), metalNetwork(network...))
	expectNoRefusal(t, metal)
}

func TestTwoOwnersOfTheNameServersRefuse(t *testing.T) {
	servers := func(config api.Object, listed ...string) api.Object {
		return config.WithSpec(config.Spec().WithPath(api.MapValue(field("config", api.MapValue(field("server", api.StringList(listed...))))), "nmstate", "dns-resolver"))
	}
	reason := "the installed name servers of Machine/rhel-01 are its network's DNS server selections, and its native dns-resolver.config.server declares them a second time"
	remedy := "remove dns-resolver.config.server from the network of Machine/rhel-01, or remove its DNS server selections"
	expectNetworkRefusal(t, labCatalog(servers(networkConfig(), "192.0.2.53")), reason, remedy)
	expectNetworkRefusal(t, labCatalog(servers(networkConfig())), reason, remedy)
	unselected := networkConfig()
	for _, config := range []api.Object{unselected.WithSpec(unselected.Spec().With("dns", api.ListValue())), unselected.WithSpec(unselected.Spec().Without("dns"))} {
		catalog := labCatalog(servers(config, "192.0.2.53"))
		expectNoRefusal(t, catalog)
		if line := networkLineOf(t, requestOf(t, catalog, "rhel-01")); strings.Contains(line, "--nameserver") {
			t.Fatalf("network line = %q", line)
		}
	}
}

func TestALibvirtBondRefusesEveryModeButActiveBackup(t *testing.T) {
	for _, mode := range bondModes {
		catalog := bondedGuest(bondInterface("bond0", api.StringValue(mode), api.Value{}, "enp1s0", "enp2s0"))
		if mode == "active-backup" {
			expectNoRefusal(t, catalog)
			continue
		}
		reason, remediation := bondRefusalOf("libvirt", "bond0", mode)
		expectNetworkRefusal(t, catalog, reason, remediation)
	}
}

func TestABareMetalBondAdmitsEveryOneOfTheSevenModes(t *testing.T) {
	if len(bondModes) != 7 {
		t.Fatalf("bond modes = %q", bondModes)
	}
	for _, mode := range bondModes {
		metal := metalOn("bond0", hardwareNIC("enp1s0", "52:54:00:9a:1b:01"), hardwareNIC("enp2s0", "52:54:00:9a:1b:02"))
		catalog := metalCatalog(metal, metalNetwork(bondPort("enp1s0", "bond0"), bondPort("enp2s0", "bond0"), bondInterface("bond0", api.StringValue(mode), api.Value{}, "enp1s0", "enp2s0")))
		expectNoRefusal(t, catalog)
		if line := networkLineOf(t, requestOf(t, catalog, "metal-01")); !strings.Contains(line, " --bondslaves=enp1s0,enp2s0 --bondopts=mode="+mode+" ") {
			t.Fatalf("network line = %q", line)
		}
	}
}

func TestTheLibvirtBondRefusalNamesMachineInterfaceAndMode(t *testing.T) {
	reason, remediation := bondRefusalOf("libvirt", "bond0", "balance-xor")
	if !strings.Contains(reason, "Machine/rhel-01") || !strings.Contains(reason, "bond bond0") || !strings.HasSuffix(reason, "mode balance-xor") {
		t.Fatalf("reason = %q", reason)
	}
	network := []api.Value{bondPort("enp1s0", "bond0"), bondPort("enp2s0", "bond0"), bondInterface("bond0", api.StringValue("balance-xor"), api.Value{}, "enp1s0", "enp2s0")}
	expectNetworkRefusal(t, labCatalog(installingOn("bond0"), networkWith(network...)), reason, remediation)
	overBond := append(network, vlanInterface("bond0.200", "bond0", "200"))
	expectNetworkRefusal(t, labCatalog(externalProvider(), installingOn("bond0.200"), networkWith(overBond...)), reason, remediation)
}

func TestSecondaryAddressesMTUAndRoutesNoLongerRefuse(t *testing.T) {
	expectNoRefusal(t, labCatalog(externalProvider(), installingOn("bond0.200"), networkWith(vlanOverBond()...)))
	extra := vlanOverBond(declaredInterface("enp3s0", "ethernet"), activeBackup("bond1", "enp4s0"), declaredInterface("enp4s0", "ethernet", text("controller", "bond1")),
		vlanInterface("enp3s0.300", "enp3s0", "300"))
	extra[2] = extra[2].With("mtu", api.IntegerValue("9000"))
	secondary := installingOn("bond0.200")
	secondary = secondary.WithSpec(secondary.Spec().WithPath(api.ListValue(
		api.MapValue(text("name", "fqdn"), text("address", "rhel-01.lab.example.test")),
		api.MapValue(text("name", "ip"), text("address", "198.51.100.11/24"), text("interface", "bond0.200")),
		api.MapValue(text("name", "cluster"), text("address", "192.0.2.50/24"), text("interface", "enp3s0")),
	), "network", "addresses"))
	routes := withRoutes(networkWith(extra...), api.MapValue(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1")),
		api.MapValue(text("destination", "203.0.113.0/24"), text("next-hop-address", "198.51.100.254")))
	catalog := labCatalog(externalProvider(), secondary, routes)
	expectNoRefusal(t, catalog)
	document := documentOf(t, catalog, "rhel-01")
	for _, name := range []string{"bond1", "enp3s0.300"} {
		if documentInterface(document, name) == nil {
			t.Errorf("the network document has no interface %s: %v", name, document["interfaces"])
		}
	}
	if mtu := encodedAt(t, documentInterface(document, "bond0"), "mtu"); mtu != "9000" {
		t.Errorf("bond0 mtu = %s", mtu)
	}
	if address := encodedAt(t, documentInterface(document, "enp3s0"), "ipv4", "address"); !strings.Contains(address, `{"ip":"192.0.2.50","prefix-length":24}`) {
		t.Errorf("enp3s0 addresses = %s", address)
	}
	if config := encodedAt(t, document, "routes", "config"); !strings.Contains(config, `{"destination":"203.0.113.0/24","next-hop-address":"198.51.100.254"}`) {
		t.Errorf("routes = %s", config)
	}
}

func TestTheRequestBuilderRefusesWhatUnsupportedRefuses(t *testing.T) {
	dns := networkConfig()
	dns = dns.WithSpec(dns.Spec().WithPath(api.MapValue(field("config", api.MapValue(field("server", api.StringList("192.0.2.53"))))), "nmstate", "dns-resolver"))
	for name, catalog := range map[string]api.Catalog{
		"a kernel-style name": labCatalog(networkWith(declaredInterface("enp1s0", "ethernet"), declaredInterface("eth0", "ethernet"))),
		"a bond without ports": labCatalog(installingOn("bond0"), networkWith(bondPort("enp1s0", "bond0"),
			declaredInterface("bond0", "bond", field("link-aggregation", api.MapValue(text("mode", "active-backup")))))),
		"a VLAN it cannot carry": labCatalog(externalProvider(), installingOn("enp1s0.4095"), networkWith(declaredInterface("enp1s0", "ethernet"), vlanInterface("enp1s0.4095", "enp1s0", "4095"))),
		"two name server owners": labCatalog(dns),
	} {
		t.Run(name, func(t *testing.T) {
			refused := Refusals(catalog)
			_, _, err := Requests(catalog, "controller", testContext)
			reported := diagnostics.Of(err)
			if len(refused) != 1 || len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" ||
				reported[0].Message != refused[0].Reason || reported[0].Remediation != refused[0].Remediation {
				t.Fatalf("refusals = %+v, builder = %#v", refused, reported)
			}
		})
	}
}

func TestTheRendererRefusesAnInstallInterfaceItCannotName(t *testing.T) {
	incomplete := "the install interface eno1 carries an incomplete bond or VLAN"
	for name, test := range map[string]struct {
		set     func(*Installation)
		message string
	}{
		"no type":               {func(i *Installation) { i.InterfaceType = "" }, "the install interface type  is not ethernet, bond or vlan"},
		"a bridge":              {func(i *Installation) { i.InterfaceType = "linux-bridge" }, "the install interface type linux-bridge is not ethernet, bond or vlan"},
		"a VLAN without an id":  {func(i *Installation) { i.InterfaceType, i.VLANBase = "vlan", "eno1" }, incomplete},
		"a VLAN without a base": {func(i *Installation) { i.InterfaceType, i.VLANID = "vlan", 8 }, incomplete},
		"a bond without ports":  {func(i *Installation) { i.InterfaceType, i.BondOptions = "bond", []string{"mode=active-backup"} }, incomplete},
		"a bond without its mode first": {func(i *Installation) {
			i.InterfaceType, i.BondPorts, i.BondOptions = "bond", []string{"eno1"}, []string{"miimon=100", "mode=active-backup"}
		}, incomplete},
	} {
		t.Run(name, func(t *testing.T) {
			input := physicalInstallation()
			test.set(&input)
			rendered, err := RenderKickstart(input)
			reported := diagnostics.Of(err)
			if rendered != "" || len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != test.message ||
				reported[0].Remediation != "plan the installation again with this release" {
				t.Fatalf("rendered %q with %#v", rendered, reported)
			}
		})
	}
}

func TestAnInterfaceNameTheKernelCannotPinRefuses(t *testing.T) {
	catalog := labCatalog(networkWith(declaredInterface("enp1s0", "ethernet"), declaredInterface("uplink-to-the-core", "ethernet")))
	_, _, err := Requests(catalog, "controller", testContext)
	argument := "ifname=uplink-to-the-core:" + substrate.InterfaceMAC(testContext, "rhel-01", "uplink-to-the-core")
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "the installer argument "+argument+" is not an interface name pinned to a hardware address" ||
		reported[0].Remediation != "correct the interface names in the network of Machine/rhel-01" {
		t.Fatalf("refusal = %#v", reported)
	}
}
