package installation

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestARequestRefusesANetworkThatDoesNotCompose(t *testing.T) {
	metal := metalOn("enp1s0", hardwareNIC("enp1s0", "52:54:00:9a:1b:01"), hardwareNIC("nic-b", "52:54:00:9a:1b:0b"))
	metal = metal.WithSpec(metal.Spec().WithPath(api.ListValue(
		api.MapValue(text("interfaceName", "enp2s0"), text("nicRef", "nic-b")),
	), "network", "interfaceBinding"))
	catalog := metalCatalog(metal, metalNetwork(declaredInterface("enp1s0", "ethernet"), declaredInterface("enp2s0", "ethernet")))
	requests, _, err := Requests(catalog, "controller", testContext)
	reported := diagnostics.Of(err)
	if len(requests) != 0 || len(reported) != 1 || reported[0].Code != "api.value" ||
		reported[0].Message != "the network configuration of Machine/metal-01 does not compose" ||
		reported[0].Remediation != "correct spec.network on Machine/metal-01" {
		t.Fatalf("requests = %d, refusal = %#v", len(requests), reported)
	}
}

func documentOf(t *testing.T, catalog api.Catalog, object string) map[string]any {
	t.Helper()
	return requestOf(t, catalog, object).Network.Document
}

func encodedAt(t *testing.T, document map[string]any, path ...string) string {
	t.Helper()
	var value any = document
	for _, name := range path {
		mapping, _ := value.(map[string]any)
		value = mapping[name]
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func documentInterface(document map[string]any, name string) map[string]any {
	interfaces, _ := document["interfaces"].([]any)
	for _, item := range interfaces {
		if iface, _ := item.(map[string]any); iface["name"] == name {
			return iface
		}
	}
	return nil
}

func lineOption(line, option string) string {
	for _, word := range strings.Fields(line) {
		if value, found := strings.CutPrefix(word, "--"+option+"="); found {
			return value
		}
	}
	return ""
}

func withResolver(config api.Object, resolver api.Value) api.Object {
	return config.WithSpec(config.Spec().WithPath(resolver, "nmstate", "dns-resolver"))
}

func refusedWith(t *testing.T, catalog api.Catalog, message, remediation string) {
	t.Helper()
	requests, _, err := Requests(catalog, "controller", testContext)
	reported := diagnostics.Of(err)
	if len(requests) != 0 || len(reported) != 1 || reported[0].Code != "api.value" || reported[0].Message != message || reported[0].Remediation != remediation {
		t.Fatalf("requests = %d, refusal = %#v", len(requests), reported)
	}
}

func TestTheNetworkDocumentInjectsAddressesAndResolvers(t *testing.T) {
	request := requestOf(t, labCatalog(), "rhel-01")
	if got := encodedAt(t, request.Network.Document, "interfaces"); !strings.Contains(got, `"ipv4":{"address":[{"ip":"198.51.100.11","prefix-length":24}],"dhcp":false,"enabled":true}`) {
		t.Fatalf("interfaces = %s", got)
	}
	servers := strings.Split(lineOption(networkLineOf(t, request), "nameserver"), ",")
	if want, _ := json.Marshal(servers); encodedAt(t, request.Network.Document, "dns-resolver", "config", "server") != string(want) || servers[0] == "" {
		t.Fatalf("dns-resolver = %s, want server %s", encodedAt(t, request.Network.Document, "dns-resolver"), want)
	}
	searched := withResolver(networkConfig(), api.MapValue(field("config", api.MapValue(field("search", api.StringList("lab.example.test"))))))
	if got := encodedAt(t, documentOf(t, labCatalog(searched), "rhel-01"), "dns-resolver"); got != `{"config":{"search":["lab.example.test"],"server":["192.0.2.1"]}}` {
		t.Fatalf("dns-resolver = %s", got)
	}
	unselected := networkConfig()
	unselected = withResolver(unselected.WithSpec(unselected.Spec().Without("dns")), api.MapValue(field("config", api.MapValue(field("server", api.StringList("192.0.2.53"))))))
	if got := encodedAt(t, documentOf(t, labCatalog(unselected), "rhel-01"), "dns-resolver"); got != `{"config":{"server":["192.0.2.53"]}}` {
		t.Fatalf("dns-resolver = %s", got)
	}
}

func TestTheNetworkDocumentDropsInjectedMACsOnly(t *testing.T) {
	metal := metalOn("eno1", hardwareNIC("eno1", "52:54:00:9a:1b:01"))
	implied := documentInterface(documentOf(t, metalCatalog(metal, metalNetwork(declaredInterface("eno1", "ethernet"))), "metal-01"), "eno1")
	if _, kept := implied["mac-address"]; implied == nil || kept {
		t.Fatalf("eno1 = %v", implied)
	}
	authored := metalNetwork(declaredInterface("eno1", "ethernet", text("mac-address", "52:54:00:9a:1b:01")))
	if iface := documentInterface(documentOf(t, metalCatalog(metal, authored), "metal-01"), "eno1"); iface["mac-address"] != "52:54:00:9a:1b:01" {
		t.Fatalf("eno1 = %v", iface)
	}
}

func TestTheNetworkDocumentPurgesRoutesOfDeclaredInterfaces(t *testing.T) {
	interfaces := vlanOverBond(declaredInterface("enp3s0", "ethernet"), declaredInterface("enp4s0", "ethernet", text("state", "absent")))
	purge := `{"next-hop-interface":"enp1s0","state":"absent"},{"next-hop-interface":"enp2s0","state":"absent"},` +
		`{"next-hop-interface":"bond0","state":"absent"},{"next-hop-interface":"bond0.200","state":"absent"},{"next-hop-interface":"enp3s0","state":"absent"}`
	routed := withRoutes(networkWith(interfaces...),
		api.MapValue(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1"), text("next-hop-interface", "bond0.200")),
		api.MapValue(text("destination", "203.0.113.0/24"), text("next-hop-address", "198.51.100.254"), text("next-hop-interface", "enp3s0")))
	want := "[" + purge + `,{"destination":"0.0.0.0/0","next-hop-address":"198.51.100.1","next-hop-interface":"bond0.200"},` +
		`{"destination":"203.0.113.0/24","next-hop-address":"198.51.100.254","next-hop-interface":"enp3s0"}]`
	if got := encodedAt(t, documentOf(t, labCatalog(externalProvider(), installingOn("bond0.200"), routed), "rhel-01"), "routes", "config"); got != want {
		t.Fatalf("routes = %s\nwant %s", got, want)
	}
	unrouted := networkWith(interfaces...)
	unrouted = unrouted.WithSpec(unrouted.Spec().WithPath(api.MapValue(), "nmstate", "routes"))
	if got := encodedAt(t, documentOf(t, labCatalog(externalProvider(), installingOn("bond0.200"), unrouted), "rhel-01"), "routes", "config"); got != "["+purge+"]" {
		t.Fatalf("routes = %s", got)
	}
}

func TestTheNetworkDocumentKeepsTheInstallPath(t *testing.T) {
	vlan := []api.Value{declaredInterface("enp1s0", "ethernet"), vlanInterface("enp1s0.200", "enp1s0", "200")}
	for name, test := range map[string]struct {
		iface   string
		catalog api.Catalog
	}{
		"ethernet":       {"enp1s0", labCatalog()},
		"bond":           {"bond0", bondedGuest(activeBackup("bond0", "enp1s0", "enp2s0"))},
		"vlan":           {"enp1s0.200", labCatalog(externalProvider(), installingOn("enp1s0.200"), networkWith(vlan...))},
		"vlan-over-bond": {"bond0.200", labCatalog(externalProvider(), installingOn("bond0.200"), networkWith(vlanOverBond()...))},
	} {
		t.Run(name, func(t *testing.T) {
			request := requestOf(t, test.catalog, "rhel-01")
			install := documentInterface(request.Network.Document, test.iface)
			if state, stated := install["state"]; install == nil || stated && state != "up" ||
				!strings.Contains(encodedAt(t, install, "ipv4", "address"), `{"ip":"198.51.100.11","prefix-length":24}`) {
				t.Fatalf("%s = %v", test.iface, install)
			}
			gateway := lineOption(networkLineOf(t, request), "gateway")
			routes, _ := request.Network.Document["routes"].(map[string]any)["config"].([]any)
			index := slices.IndexFunc(routes, func(item any) bool {
				route, _ := item.(map[string]any)
				hop, named := route["next-hop-interface"]
				return route["destination"] == "0.0.0.0/0" && route["state"] != "absent" && (!named || hop == test.iface)
			})
			if gateway == "" || index < 0 || routes[index].(map[string]any)["next-hop-address"] != gateway {
				t.Fatalf("gateway %q, routes = %v", gateway, routes)
			}
		})
	}
}

func TestAnOversizedNetworkDocumentRefuses(t *testing.T) {
	var routes []api.Value
	for host := range 256 {
		for _, metric := range []string{"100", "200"} {
			routes = append(routes, api.MapValue(text("destination", fmt.Sprintf("203.0.113.%d/32", host)), number("metric", metric),
				text("next-hop-address", "198.51.100.1"), text("next-hop-interface", "enp1s0")))
		}
	}
	refusedWith(t, labCatalog(withRoutes(networkConfig(), routes...)),
		"the composed network of Machine/rhel-01 exceeds 32 KiB, which its installation does not carry", "reduce the network configuration of Machine/rhel-01")
}

func TestTheNetworkDocumentSurvivesItsFrozenRoundTrip(t *testing.T) {
	route := func(field api.FieldValue) api.Value {
		return api.MapValue(text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1"), field)
	}
	exact := withRoutes(networkWith(declaredInterface("enp1s0", "ethernet", number("mtu", "9000"))), route(number("table-id", "254")))
	canonical, err := pinnedRequest(t, labCatalog(exact)).Canonical()
	if err != nil {
		t.Fatalf("freezing: %v", diagnostics.Of(err))
	}
	if !strings.Contains(string(canonical), `"mtu":9000`) || !strings.Contains(string(canonical), `"table-id":254`) {
		t.Fatalf("canonical = %s", canonical)
	}
	if _, err := DecodeRequest(canonical); err != nil {
		t.Fatalf("decoding: %v", diagnostics.Of(err))
	}
	refusedWith(t, labCatalog(withRoutes(networkConfig(), route(number("metric", "9007199254740993")))),
		"the composed network of Machine/rhel-01 holds a number its frozen installation cannot carry exactly",
		"write each number in the network of Machine/rhel-01 as an integer of at most 15 digits, or as text")
}

func packageLines(kickstart string) []string {
	_, section, _ := strings.Cut(kickstart, "\n%packages")
	section, _, _ = strings.Cut(section, "\n%end")
	return strings.Split(section, "\n")
}

func TestEveryInstallationInstallsNmstate(t *testing.T) {
	sshPlaced := labCatalog(servicesHost(), artifactServer(text("machineRef", "services"), text("bindAddress", "192.0.2.2")))
	checksummed := api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "local-media:rhel-9.8-x86_64-boot.iso"), text("checksum", "SHA256:"+strings.Repeat("0123456789ABCDEF", 4))))
	kickstarts := map[string]string{
		"metal": requestOf(t, metalCatalog(metalOn("enp1s0", hardwareNIC("enp1s0", "52:54:00:9a:1b:01")), metalNetwork(declaredInterface("enp1s0", "ethernet"))), "metal-01").Kickstart,
	}
	for name, catalog := range map[string]api.Catalog{
		"lab-rhel": labCatalog(), "lab-rhel-checksummed": labCatalog(checksummed), "lab-rhel-ssh-placed": sshPlaced, "lab-rhel-site-services": siteServicesCatalog(),
	} {
		kickstarts[name] = pinnedRequest(t, catalog).Kickstart
	}
	for name, kickstart := range kickstarts {
		lines := packageLines(kickstart)
		if count := len(slices.DeleteFunc(lines, func(line string) bool { return line != "nmstate" })); count != 1 {
			t.Errorf("%s installs nmstate %d times:\n%s", name, count, kickstart)
		}
	}
}
