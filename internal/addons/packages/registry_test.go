package packages

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func config(pools ...api.Value) api.Value {
	return api.MapValue(api.FieldValue{Name: "subscription", Value: api.MapValue(api.FieldValue{Name: "channel", Value: api.StringValue("stable")}, api.FieldValue{Name: "startingCSV", Value: api.StringValue("operator.v1")})}, api.FieldValue{Name: "pools", Value: api.ListValue(pools...)})
}
func pool(name string, addresses ...string) api.Value {
	return api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(name)}, api.FieldValue{Name: "addresses", Value: api.StringList(addresses...)}, api.FieldValue{Name: "autoAssign", Value: api.BoolValue(true)})
}
func facts() addons.ClusterFacts {
	return addons.ClusterFacts{Name: "cluster", Family: "ipv4", MachineNetworks: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, ClusterNetworks: []netip.Prefix{netip.MustParsePrefix("10.128.0.0/14")}, ServiceNetworks: []netip.Prefix{netip.MustParsePrefix("172.30.0.0/16")}, NodeNetworks: []addons.NodeNetwork{{Machine: "node", DefaultRouteInterface: "eth0", Interfaces: []addons.NodeInterface{{Name: "eth0", Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.10/24")}}}, NextHops: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}}}
}
func TestPoolAdmission(t *testing.T) {
	for _, tt := range []struct {
		name     string
		entries  []api.Value
		contains string
	}{
		{"valid", []api.Value{pool("services", "192.0.2.80-192.0.2.90")}, ""},
		{"host bits", []api.Value{pool("services", "192.0.2.81/28")}, "canonical"},
		{"reverse", []api.Value{pool("services", "192.0.2.90-192.0.2.80")}, "ends before"},
		{"family", []api.Value{pool("services", "2001:db8::/120")}, "family"},
		{"node", []api.Value{pool("services", "192.0.2.10-192.0.2.15")}, "Machine/node"},
		{"route", []api.Value{pool("services", "192.0.2.1-192.0.2.5")}, "next hop"},
		{"network", []api.Value{pool("services", "192.0.2.0/24")}, "broadcast"},
		{"outside", []api.Value{pool("services", "198.51.100.0/28")}, "outside"},
		{"overlap", []api.Value{pool("a", "192.0.2.80-192.0.2.90"), pool("b", "192.0.2.90-192.0.2.95")}, "overlaps pool a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			issues := admitPools(addons.Instance{Config: config(tt.entries...)}, facts())
			if tt.contains == "" {
				if len(issues) > 0 {
					t.Fatal(issues)
				}
				return
			}
			found := false
			for _, issue := range issues {
				if strings.Contains(issue.Message, tt.contains) {
					found = true
					if !strings.HasPrefix(issue.Field, ".pools[") || issue.Remediation == "" {
						t.Fatal(issue)
					}
				}
			}
			if !found {
				t.Fatalf("want %s: %+v", tt.contains, issues)
			}
		})
	}
}
func TestSecondaryInterfaceAdmissionAndReservedAddresses(t *testing.T) {
	f := facts()
	f.NodeNetworks[0].Interfaces = append(f.NodeNetworks[0].Interfaces, addons.NodeInterface{Name: "eth1", Addresses: []netip.Prefix{netip.MustParsePrefix("198.51.100.10/24")}})
	p := pool("secondary", "198.51.100.80-198.51.100.90").With("l2", api.MapValue(api.FieldValue{Name: "interface", Value: api.StringValue("eth1")}))
	i := addons.Instance{Config: config(p)}
	if len(admitPools(i, f)) != 2 {
		t.Fatal("missing forwarding and host routing were not refused")
	}
	f.Gateway = addons.OVNGateway{RoutingViaHost: true, IPForwarding: "Global"}
	if issues := admitPools(i, f); len(issues) > 0 {
		t.Fatal(issues)
	}
	f.Reserved = []addons.AddressHolder{{Addr: netip.MustParseAddr("198.51.100.85"), Holder: "DNSServer/resolver"}}
	if issues := admitPools(i, f); len(issues) != 1 || !strings.Contains(issues[0].Message, "DNSServer/resolver") {
		t.Fatal(issues)
	}
	f.Reserved = nil
	f.SecondaryNetworks = []addons.SecondaryNetwork{{Name: "local", UplinkInterface: "eth1"}}
	if issues := admitPools(i, f); len(issues) != 1 || !strings.Contains(issues[0].Message, "uplink") {
		t.Fatal(issues)
	}
}
func TestShippedPackagesRenderPurePrograms(t *testing.T) {
	r := Builtin()
	for _, p := range r.Packages() {
		t.Run(p.Name, func(t *testing.T) {
			v := config().Without("pools")
			if p.Name == "metallb" {
				v = config(pool("services", "192.0.2.80-192.0.2.90"))
			}
			i := addons.Instance{Cluster: "cluster", Binding: "binding", Config: v}
			a, err := p.Render(i)
			if err != nil {
				t.Fatal(err)
			}
			b, err := p.Render(i)
			if err != nil || !reflect.DeepEqual(a, b) {
				t.Fatal("render not deterministic")
			}
			if len(a.After) == 0 || len(a.After[0].Gate) == 0 || a.After[0].Gate[0].Condition.Type != "Established" {
				t.Fatal("operand has no CRD gate")
			}
			for _, phase := range a.After {
				if _, err := co.CompilePhase(phase); err != nil {
					t.Fatal(err)
				}
			}
			if len(a.Checks) == 0 {
				t.Fatal("no readiness")
			}
		})
	}
	entries := r.Packages()
	entries[0].OLM.Namespace = "changed"
	entries[0].Provides[0] = "changed"
	again := r.Packages()
	if again[0].OLM.Namespace == "changed" || again[0].Provides[0] == "changed" {
		t.Fatal("registry exposes mutable state")
	}
}
func TestIPv6IntervalsAreBounded(t *testing.T) {
	r, _, err := parseInterval("2001:db8::/32")
	if err != nil || r.last.String() != "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff" {
		t.Fatal(r, err)
	}
	if !r.contains(netip.MustParseAddr("2001:db8::1")) {
		t.Fatal("range membership")
	}
}

func TestSchemaMembersMatchRegistry(t *testing.T) {
	field, ok := api.Schema(api.ClusterAddonBinding).Field("packages")
	if !ok {
		t.Fatal("packages schema missing")
	}
	r := Builtin()
	if len(field.Shape.Fields) != len(r.Packages()) {
		t.Fatal("schema and registry differ")
	}
	for _, member := range field.Shape.Fields {
		if _, ok := r.Find(member.Name); !ok {
			t.Fatal(member.Name)
		}
	}
}

func TestMetalLBReadyBeforePoolCreation(t *testing.T) {
	rendered, err := renderMetalLB(addons.Instance{Config: config(pool("services", "192.0.2.80-192.0.2.90"))})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.After) != 2 {
		t.Fatal(rendered)
	}
	gate := rendered.After[1].Gate[0].Condition
	if gate == nil || gate.Kind != "MetalLB" || gate.Name != "metallb" || gate.Namespace != "metallb-system" || gate.Type != "Available" || gate.Status != "True" {
		t.Fatal("pool objects precede singleton readiness", gate)
	}
}

func TestPoolInterfaceRequiresFactsForEveryNode(t *testing.T) {
	f := facts()
	f.Nodes = []string{"node", "missing"}
	f.Gateway = addons.OVNGateway{RoutingViaHost: true, IPForwarding: "Global"}
	f.NodeNetworks[0].Interfaces = append(f.NodeNetworks[0].Interfaces, addons.NodeInterface{Name: "eth1", Addresses: []netip.Prefix{netip.MustParsePrefix("198.51.100.10/24")}})
	p := pool("secondary", "198.51.100.80-198.51.100.90").With("l2", api.MapValue(api.FieldValue{Name: "interface", Value: api.StringValue("eth1")}))
	issues := admitPools(addons.Instance{Config: config(p)}, f)
	if len(issues) != 1 || issues[0].Field != ".pools[0].l2.interface" || !strings.Contains(issues[0].Message, "node missing") {
		t.Fatal(issues)
	}
}

func TestPoolInterfaceAndNetworkRefusalFields(t *testing.T) {
	for _, test := range []struct {
		name           string
		change         func(*addons.ClusterFacts, *api.Value)
		message, field string
	}{
		{"missing interface", func(f *addons.ClusterFacts, _ *api.Value) {
			f.NodeNetworks[0].Interfaces = f.NodeNetworks[0].Interfaces[:1]
		}, "not declared on node node", ".l2.interface"},
		{"addressless interface", func(f *addons.ClusterFacts, _ *api.Value) { f.NodeNetworks[0].Interfaces[1].Addresses = nil }, "carries no ipv4 address", ".l2.interface"},
		{"different subnets", func(f *addons.ClusterFacts, _ *api.Value) {
			f.NodeNetworks = append(f.NodeNetworks, addons.NodeNetwork{Machine: "other", Interfaces: []addons.NodeInterface{{Name: "eth1", Addresses: []netip.Prefix{netip.MustParsePrefix("203.0.113.10/24")}}}})
		}, "is on 198.51.100.0/24 on node node and on 203.0.113.0/24 on node other", ".l2.interface"},
		{"outside interface subnet", func(_ *addons.ClusterFacts, p *api.Value) {
			*p = p.With("addresses", api.StringList("203.0.113.80-203.0.113.90"))
		}, "lies outside 198.51.100.0/24, the subnet of eth1", ".addresses[0]"},
		{"default interface", func(f *addons.ClusterFacts, _ *api.Value) { f.NodeNetworks[0].DefaultRouteInterface = "eth1" }, "default-route interface", ".l2.interface"},
		{"secondary bridge", func(f *addons.ClusterFacts, _ *api.Value) {
			f.SecondaryNetworks = []addons.SecondaryNetwork{{Name: "tenant", UplinkBridge: "eth1"}}
		}, "uplink of secondary network tenant", ".l2.interface"},
		{"host routing", func(f *addons.ClusterFacts, _ *api.Value) { f.Gateway.RoutingViaHost = false }, "needs gatewayConfig.routingViaHost: true", ".l2.interface"},
		{"forwarding", func(f *addons.ClusterFacts, _ *api.Value) { f.Gateway.IPForwarding = "Restricted" }, "needs gatewayConfig.ipForwarding: Global", ".l2.interface"},
		{"endpoint", func(f *addons.ClusterFacts, _ *api.Value) {
			f.Endpoints = []addons.AddressHolder{{Addr: netip.MustParseAddr("198.51.100.85"), Holder: "the api endpoint"}}
		}, "held by the api endpoint", ".addresses[0]"},
		{"cluster network", func(f *addons.ClusterFacts, _ *api.Value) {
			f.ClusterNetworks = []netip.Prefix{netip.MustParsePrefix("198.51.100.64/26")}
		}, "the cluster network", ".addresses[0]"},
		{"service network", func(f *addons.ClusterFacts, _ *api.Value) {
			f.ServiceNetworks = []netip.Prefix{netip.MustParsePrefix("198.51.100.64/26")}
		}, "the service network", ".addresses[0]"},
		{"secondary subnet", func(f *addons.ClusterFacts, _ *api.Value) {
			f.SecondaryNetworks = []addons.SecondaryNetwork{{Name: "tenant", Subnets: []netip.Prefix{netip.MustParsePrefix("198.51.100.64/26")}}}
		}, "the secondary network tenant", ".addresses[0]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := facts()
			f.Gateway = addons.OVNGateway{RoutingViaHost: true, IPForwarding: "Global"}
			f.NodeNetworks[0].Interfaces = append(f.NodeNetworks[0].Interfaces, addons.NodeInterface{Name: "eth1", Addresses: []netip.Prefix{netip.MustParsePrefix("198.51.100.10/24")}})
			p := pool("secondary", "198.51.100.80-198.51.100.90").With("l2", api.MapValue(api.FieldValue{Name: "interface", Value: api.StringValue("eth1")}))
			if issues := admitPools(addons.Instance{Config: config(p)}, f); len(issues) != 0 {
				t.Fatalf("passing twin: %+v", issues)
			}
			test.change(&f, &p)
			issues := admitPools(addons.Instance{Config: config(p)}, f)
			for _, issue := range issues {
				if strings.Contains(issue.Message, test.message) {
					if issue.Code != "api.invariant" || issue.Field != ".pools[0]"+test.field || issue.Remediation == "" {
						t.Fatal(issue)
					}
					return
				}
			}
			t.Fatal(issues)
		})
	}
}
