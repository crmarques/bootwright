package network

import (
	"net/netip"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func m(kv ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(kv); i += 2 {
		var v api.Value
		switch x := kv[i+1].(type) {
		case api.Value:
			v = x
		case string:
			v = api.StringValue(x)
		case bool:
			v = api.BoolValue(x)
		case int:
			v = api.IntegerValue(strconv.Itoa(x))
		}
		fields = append(fields, api.FieldValue{Name: kv[i].(string), Value: v})
	}
	return api.MapValue(fields...)
}

func names(values ...string) api.Value {
	items := []api.Value{}
	for _, value := range values {
		items = append(items, api.StringValue(value))
	}
	return api.ListValue(items...)
}

func net(name string, spec api.Value) api.Object {
	return api.NewObject(api.Network, name, api.MapValue(), spec)
}

func validate(o api.Object, others ...api.Object) []api.Issue {
	normalized, _ := Normalize(o, api.Catalog{})
	return Validate(normalized, api.NewCatalog(append([]api.Object{normalized}, others...)))
}

func requireIssue(t *testing.T, issues []api.Issue, field, text string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Field == field && strings.Contains(issue.Message+" "+issue.Remediation, text) {
			return
		}
	}
	t.Fatalf("no issue at %s with %q in %+v", field, text, issues)
}

func requireNone(t *testing.T, issues []api.Issue) {
	t.Helper()
	if len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestAValidNetworkGraphIsAdmitted(t *testing.T) {
	env := api.NewObject(api.Environment, "env", api.MapValue(), m("sites", api.ListValue(m("name", "a"), m("name", "b"))))
	public := net("public", m("provided", true, "vlan", 200, "sites", names("a"), "ipv4", m("cidr", "198.51.100.0/24", "gateway", "198.51.100.1"), "ipv6", m("cidr", "2001:db8:1::/64", "gateway", "2001:db8:1::1")))
	witness := net("witness", m("provided", true, "vlan", 200, "sites", names("b"), "ipv4", m("cidr", "198.51.100.0/25")))
	managed := net("lab", m("provided", false, "ipv4", m("cidr", "192.0.2.0/24"), "libvirt", m("providerRef", "lab", "bridge", "virbr-lab")))
	for _, o := range []api.Object{public, witness, managed} {
		requireNone(t, validate(o, env, public, witness, managed))
	}
}

func TestTheLifecycleSelectsExactlyTheArmsItAllows(t *testing.T) {
	subnet := m("cidr", "192.0.2.0/24")
	requireIssue(t, validate(net("n", m("provided", true, "ipv4", subnet, "libvirt", m("providerRef", "lab", "bridge", "virbr-lab")))), "$.spec.libvirt", "a provided network is realized by nothing")
	requireIssue(t, validate(net("n", m("provided", false, "ipv4", subnet))), "$.spec", "name the substrate that realizes it, spec.libvirt")
}

func TestANetworkDeclaresASubnet(t *testing.T) {
	requireIssue(t, validate(net("n", m("provided", true))), "$.spec", "declares no subnet")
}

func TestACIDRIsCanonicalAndOfItsFamily(t *testing.T) {
	requireIssue(t, validate(net("n", m("provided", true, "ipv4", m("cidr", "192.0.2.1/24")))), "$.spec.ipv4.cidr", "write spec.ipv4.cidr as 192.0.2.0/24")
	requireIssue(t, validate(net("n", m("provided", true, "ipv6", m("cidr", "2001:db8::1/64")))), "$.spec.ipv6.cidr", "write spec.ipv6.cidr as 2001:db8::/64")
	requireIssue(t, validate(net("n", m("provided", true, "ipv4", m("cidr", "2001:db8::/64")))), "$.spec.ipv4.cidr", "is not an IPv4 prefix")
	normalized, _ := Normalize(net("n", m("provided", true, "ipv6", m("cidr", "2001:DB8:0::/64", "gateway", "2001:DB8::1"))), api.Catalog{})
	if got := normalized.Spec().Get("ipv6"); got.Get("cidr").Text() != "2001:db8::/64" || got.Get("gateway").Text() != "2001:db8::1" {
		t.Fatalf("normalized = %v", got)
	}
}

func TestAGatewayIsAHostAddressOfItsSubnet(t *testing.T) {
	for gateway, reason := range map[string]string{
		"198.51.100.1": "lies outside 192.0.2.0/24",
		"192.0.2.0":    "is the network address",
		"192.0.2.255":  "is the broadcast address",
		"2001:db8::1":  "lies outside 192.0.2.0/24",
	} {
		requireIssue(t, validate(net("n", m("provided", true, "ipv4", m("cidr", "192.0.2.0/24", "gateway", gateway)))), "$.spec.ipv4.gateway", reason)
	}
	requireNone(t, validate(net("n", m("provided", true, "ipv4", m("cidr", "192.0.2.0/31", "gateway", "192.0.2.0")))))
	for cidr, gateway := range map[string]string{"2001:db8::/64": "2001:db8::", "2001:db8::/120": "2001:db8::"} {
		requireIssue(t, validate(net("n", m("provided", true, "ipv6", m("cidr", cidr, "gateway", gateway)))), "$.spec.ipv6.gateway", "Subnet-Router anycast")
	}
	for cidr, gateway := range map[string]string{"2001:db8::/64": "2001:db8::fdff:ffff:ffff:ff80", "2001:db8::/120": "2001:db8::ff"} {
		requireIssue(t, validate(net("n", m("provided", true, "ipv6", m("cidr", cidr, "gateway", gateway)))), "$.spec.ipv6.gateway", "reserved subnet anycast")
	}
	requireNone(t, validate(net("n", m("provided", true, "ipv6", m("cidr", "2001:db8::/64", "gateway", "2001:db8::ffff:ffff:ffff:ffff")))))
	for cidr, gateway := range map[string]string{"2001:db8::/121": "2001:db8::7f", "2001:db8::80/121": "2001:db8::81", "2001:db8::100/121": "2001:db8::17e"} {
		requireNone(t, validate(net("n", m("provided", true, "ipv6", m("cidr", cidr, "gateway", gateway)))))
	}
}

func TestAnIPv6NetworkCarriesTheIPv6MinimumMTU(t *testing.T) {
	requireIssue(t, validate(net("n", m("provided", true, "mtu", 1279, "ipv6", m("cidr", "2001:db8::/64")))), "$.spec.mtu", "below 1280, the minimum IPv6 link MTU")
	requireNone(t, validate(net("n", m("provided", true, "mtu", 1280, "ipv6", m("cidr", "2001:db8::/64")))))
	requireNone(t, validate(net("n", m("provided", true, "mtu", 576, "ipv4", m("cidr", "192.0.2.0/24")))))
}

func TestASiteResolvesInTheEnvironment(t *testing.T) {
	env := api.NewObject(api.Environment, "env", api.MapValue(), m("sites", api.ListValue(m("name", "a"))))
	requireIssue(t, validate(net("n", m("provided", true, "sites", names("a", "z"), "ipv4", m("cidr", "192.0.2.0/24"))), env), "$.spec.sites[1]", "site z of Network/n names no site of Environment/env")
}

func TestNetworksOfOneSiteShareNoVLANAndNoAddresses(t *testing.T) {
	first := net("first", m("provided", true, "vlan", 200, "ipv4", m("cidr", "192.0.2.0/24")))
	sameVLAN := net("second", m("provided", true, "vlan", 200, "ipv4", m("cidr", "198.51.100.0/24")))
	requireIssue(t, validate(first, sameVLAN), "$.spec.vlan", "Network/first and Network/second share VLAN 200")
	overlapping := net("second", m("provided", true, "ipv4", m("cidr", "192.0.2.128/25")))
	requireIssue(t, validate(first, overlapping), "$.spec.ipv4.cidr", "Network/first 192.0.2.0/24 overlaps Network/second 192.0.2.128/25")
	apart := net("second", m("provided", true, "vlan", 200, "sites", names("b"), "ipv4", m("cidr", "192.0.2.0/24")))
	requireNone(t, validate(net("first", m("provided", true, "vlan", 200, "sites", names("a"), "ipv4", m("cidr", "192.0.2.0/24"))), apart))
}

func TestTheLookupHelpersReadOneNetwork(t *testing.T) {
	scoped := net("scoped", m("provided", true, "vlan", 300, "sites", names("a"), "ipv4", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1")))
	catalog := api.NewCatalog([]api.Object{scoped})
	held := Containing(catalog, netip.MustParseAddr("192.0.2.9"))
	if len(held) != 1 || held[0].Prefix.String() != "192.0.2.0/24" || !Serves(scoped, "a") || Serves(scoped, "b") || !Serves(scoped, "") {
		t.Fatalf("held = %+v", held)
	}
	if id, ok := VLAN(scoped); !ok || id != 300 {
		t.Fatalf("vlan = %d", id)
	}
	if gateway, ok := Gateway(scoped, "ipv4"); !ok || gateway.String() != "192.0.2.1" {
		t.Fatalf("gateway = %v", gateway)
	}
	if _, ok := Gateway(scoped, "ipv6"); ok {
		t.Fatal("an absent gateway reads as declared")
	}
}

func TestANetworkNeverSpansEveryLoopbackOrMulticastAddress(t *testing.T) {
	for _, tc := range []struct{ family, cidr, reason string }{
		{"ipv4", "0.0.0.0/0", "zero-length prefix that spans every IPv4 address"},
		{"ipv6", "::/0", "zero-length prefix that spans every IPv6 address"},
		{"ipv4", "127.0.0.0/8", "loopback range 127.0.0.0/8"},
		{"ipv4", "127.1.2.0/24", "loopback range 127.0.0.0/8"},
		{"ipv6", "::1/128", "loopback address ::1/128"},
		{"ipv4", "224.0.0.0/8", "multicast range 224.0.0.0/4"},
		{"ipv4", "239.1.0.0/16", "multicast range 224.0.0.0/4"},
		{"ipv6", "ff02::/16", "multicast range ff00::/8"},
		{"ipv4", "128.0.0.0/1", "is a /1, wider than the /8 that bounds one IPv4 network"},
		{"ipv4", "64.0.0.0/2", "is a /2, wider than the /8 that bounds one IPv4 network"},
		{"ipv6", "4000::/2", "is a /2, wider than the /16 that bounds one IPv6 network"},
		{"ipv6", "::/16", "loopback address ::1/128"},
	} {
		issues := validate(net("wide", m("provided", true, tc.family, m("cidr", tc.cidr))))
		found := false
		for _, issue := range issues {
			found = found || issue.Code == "api.value" && issue.Field == "$.spec."+tc.family+".cidr" &&
				strings.Contains(issue.Message, tc.cidr+" of Network/wide ") && strings.Contains(issue.Message, tc.reason) &&
				issue.Remediation == "set spec."+tc.family+".cidr on Network/wide to the subnet its hosts actually share"
		}
		if !found {
			t.Errorf("%s: issues = %+v", tc.cidr, issues)
		}
	}
	for _, cidr := range []string{"10.0.0.0/8", "126.0.0.0/8", "223.255.255.0/24", "240.0.0.0/8"} {
		requireNone(t, validate(net("lan", m("provided", true, "ipv4", m("cidr", cidr)))))
	}
	requireNone(t, validate(net("lan", m("provided", true, "ipv6", m("cidr", "fe80::/64")))))
	requireNone(t, validate(net("lan", m("provided", true, "ipv6", m("cidr", "2001::/16")))))
}
