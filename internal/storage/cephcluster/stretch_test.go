package cephcluster

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// stretchCatalog is the reference stretch shape: three OSD hosts at each data
// site and a provided tiebreaker at a third site on its own routed subnet.
func stretchCatalog(t *testing.T, change func(api.Value) api.Value) (api.Object, api.Catalog) {
	t.Helper()
	catalog := replace(fixtureCatalog(), object(api.Environment, "environment", m("domains", m("base", "example.test"), "remoteMachinesAccessKey", m("keyRef", "fleet-key"),
		"sites", []api.Value{m("name", "site-a"), m("name", "site-b"), m("name", "site-c")})))
	nodes := []api.Value{}
	for i, name := range []string{"a1", "a2", "a3", "b1", "b2", "b3", "tie"} {
		site, address := "site-"+name[:1], fmt.Sprintf("192.0.2.%d/24", 10+i)
		labels := api.StringList("storage")
		if name == "tie" {
			site, address, labels = "site-c", "198.51.100.10/24", api.StringList()
		}
		nodes = append(nodes, m("name", name, "machineRef", "machine-"+name, "labels", labels))
		catalog = replace(catalog, object(api.Machine, "machine-"+name, m("placement", m("site", site), "hardware", m("rootDevice", m("deviceName", "/dev/vda")), "os", m("provided", true),
			"network", m("addresses", []api.Value{m("name", "ssh", "address", address)}), "access", m("ssh", m("addressRef", "ssh")))))
	}
	services := api.ListValue(m("service_type", "mon", "placement", m("hosts", fqdns("a1", "a2", "b1", "b2", "tie"))), m("service_type", "mgr", "placement", m("hosts", fqdns("a1", "b1"))),
		m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "spec", m("data_devices", m("paths", api.StringList("/dev/vdb")))))
	cluster := find(t, catalog, api.CephCluster, "storage")
	spec := cluster.Spec().WithPath(api.ListValue(nodes...), "topology", "nodes").With("services", services).WithPath(m("nodeRef", "a1"), "cephadm", "bootstrap").
		WithPath(api.StringList("192.0.2.0/24", "198.51.100.0/24"), "networks", "publicCIDRs").Without("config").
		WithPath(m("failureDomain", "datacenter", "tiebreaker", m("node", "tie")), "topology", "stretch")
	if change != nil {
		spec = change(spec)
	}
	cluster = cluster.WithSpec(spec)
	catalog = replace(catalog, cluster)
	catalog = normalizeCatalog(catalog)
	return find(t, catalog, api.CephCluster, "storage"), catalog
}

func fqdns(names ...string) api.Value {
	hosts := []string{}
	for _, name := range names {
		hosts = append(hosts, name+".storage.example.test")
	}
	return api.StringList(hosts...)
}

func withItem(list api.Value, index int, value api.Value) api.Value {
	items := list.Items()
	items[index] = value
	return api.ListValue(items...)
}

func locate(spec api.Value, index int, location api.Value) api.Value {
	nodes := spec.Get("topology", "nodes")
	return spec.WithPath(withItem(nodes, index, nodes.Items()[index].With("location", location)), "topology", "nodes")
}

func clusterIssuesAt(issues []api.Issue, field string) []api.Issue {
	found := []api.Issue{}
	for _, issue := range issues {
		if issue.Field == field {
			found = append(found, issue)
		}
	}
	return found
}

func requireOnlyIssue(t *testing.T, issues []api.Issue, field, message string) {
	t.Helper()
	if len(issues) != 1 || issues[0].Field != field || !strings.Contains(issues[0].Message, message) {
		t.Fatalf("want one issue at %s containing %q, got %#v", field, message, issues)
	}
}

func TestReferenceStretchTopologyAdmitsAProvidedTiebreakerOnASecondPublicCIDR(t *testing.T) {
	cluster, catalog := stretchCatalog(t, nil)
	if issues := Validate(cluster, catalog); len(issues) != 0 {
		t.Fatalf("reference stretch refused: %#v", issues)
	}
	stretch := cluster.Spec().Get("topology", "stretch")
	if textOf(stretch, "tiebreaker", "site") != "site-c" || !stretch.Get("dataSites").Equal(api.StringList("site-a", "site-b")) || textOf(stretch, "ruleName") != "stretch-rule" {
		t.Fatalf("stretch normalization = %v", stretch)
	}
	fips, catalog := stretchCatalog(t, func(spec api.Value) api.Value {
		return spec.WithPath(api.BoolValue(true), "security", "fips", "enabled").WithPath(m("keyType", "aes"), "security", "cephx")
	})
	if issues := clusterIssuesAt(Validate(fips, catalog), "$.spec.topology.nodes[6].machineRef"); len(issues) != 0 {
		t.Fatalf("a FIPS cluster refused its provided tiebreaker at admission: %#v", issues)
	}
}

func TestStretchTopologyRefusesEachBrokenInput(t *testing.T) {
	for _, test := range []struct {
		name, field, message string
		change               func(api.Value) api.Value
	}{
		{"failure domain below a site", "$.spec.topology.stretch.failureDomain", "not a native default CRUSH bucket type above host", func(s api.Value) api.Value {
			return s.WithPath(api.StringValue("host"), "topology", "stretch", "failureDomain")
		}},
		{"no tiebreaker", "$.spec.topology.stretch.tiebreaker.node", "needs a tiebreaker MON", func(s api.Value) api.Value {
			return s.WithPath(api.MapValue(), "topology", "stretch", "tiebreaker")
		}},
		{"tiebreaker outside the MON service", "$.spec.topology.stretch.tiebreaker.node", "eligible for the declared MON service", func(s api.Value) api.Value {
			return s.With("services", withItem(s.Get("services"), 0, m("service_type", "mon", "placement", m("hosts", fqdns("a1", "a2", "b1", "b2")))))
		}},
		{"tiebreaker bootstraps", "$.spec.cephadm.bootstrap.nodeRef", "runs a MON alone", func(s api.Value) api.Value {
			return s.WithPath(api.StringValue("tie"), "cephadm", "bootstrap", "nodeRef")
		}},
		{"OSD service on the tiebreaker", "$.spec.services[2].placement", "the osd service places on the stretch tiebreaker tie", func(s api.Value) api.Value {
			return s.With("services", withItem(s.Get("services"), 2, m("service_type", "osd", "service_id", "data", "placement", m("host_pattern", "*"), "spec", m("data_devices", m("paths", api.StringList("/dev/vdb"))))))
		}},
		{"one OSD host at a data site", "$.spec.topology.stretch.dataSites", "stretch data site site-b needs a MON and at least two OSD hosts", func(s api.Value) api.Value {
			nodes := s.Get("topology", "nodes")
			for i := 4; i <= 5; i++ {
				nodes = withItem(nodes, i, nodes.Items()[i].With("labels", api.StringList()))
			}
			return s.WithPath(nodes, "topology", "nodes")
		}},
		{"a data-site location naming another level", "$.spec.topology.nodes[0].location.rack", "names only the stretch failure domain datacenter", func(s api.Value) api.Value {
			return locate(s, 0, m("rack", "r1"))
		}},
		{"a data-site location naming another site", "$.spec.topology.nodes[0].location", "must agree with the proved site", func(s api.Value) api.Value {
			return locate(s, 0, m("datacenter", "site-b"))
		}},
		{"a located tiebreaker", "$.spec.topology.nodes[6].location.datacenter", "the tiebreaker holds no OSD", func(s api.Value) api.Value {
			return locate(s, 6, m("datacenter", "site-c"))
		}},
		{"a MON outside every public CIDR", "$.spec.topology.nodes[6].machineRef", "MON host tie.storage.example.test address 198.51.100.10 lies inside no public CIDR 192.0.2.0/24", func(s api.Value) api.Value {
			return s.WithPath(api.StringList("192.0.2.0/24"), "networks", "publicCIDRs")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cluster, catalog := stretchCatalog(t, test.change)
			issues := Validate(cluster, catalog)
			if test.name == "no tiebreaker" {
				issues = clusterIssuesAt(issues, test.field)
			}
			requireOnlyIssue(t, issues, test.field, test.message)
		})
	}
}

func TestStretchNodesOccupyADataSiteOrTheTiebreakerSite(t *testing.T) {
	_, catalog := stretchCatalog(t, nil)
	moved := find(t, catalog, api.Machine, "machine-b3")
	catalog = replace(catalog, moved.WithSpec(moved.Spec().WithPath(api.StringValue("site-c"), "placement", "site")))
	cluster := find(t, catalog, api.CephCluster, "storage")
	nodes := cluster.Spec().Get("topology", "nodes")
	cluster = cluster.WithSpec(cluster.Spec().WithPath(withItem(nodes, 5, nodes.Items()[5].With("site", api.StringValue("site-c"))), "topology", "nodes"))
	requireIssue(t, Validate(cluster, replace(catalog, cluster)), "$.spec.topology.nodes[5].site")
}

func TestHostLocationRefusesOutsideStretch(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	cluster = cluster.WithSpec(locate(cluster.Spec(), 0, m("datacenter", "site-a")))
	requireOnlyIssue(t, Validate(cluster, replace(catalog, cluster)), "$.spec.topology.nodes[0].location.datacenter", "admitted only on a stretch topology")
}

func TestAStretchClusterPlacesEveryPoolWithItsStretchRule(t *testing.T) {
	_, catalog := stretchCatalog(t, nil)
	policy := find(t, catalog, api.CephPlacementPolicy, "policy")
	requireOnlyIssue(t, Validate(policy, catalog), "$.spec.clusterRef", "places every pool with its own stretch rule stretch-rule, so no placement policy applies")
	metadata := find(t, catalog, api.CephPool, "metadata")
	requireIssue(t, Validate(metadata, catalog), "$.spec.placementPolicyRef")
	implicit := object(api.CephPool, "implicit", m("clusterRef", "storage", "role", "rbd"))
	if issues := Validate(implicit, replace(catalog, implicit)); len(issues) != 0 {
		t.Fatalf("a stretch pool without protection took no implicit 4/2: %#v", issues)
	}
	explicit := object(api.CephPool, "explicit", m("clusterRef", "storage", "role", "rbd", "replicated", m("size", 3, "minSize", 2)))
	requireIssue(t, Validate(explicit, replace(catalog, explicit)), "$.spec.replicated")
}

func addServices(extra ...api.Value) func(api.Value) api.Value {
	return func(spec api.Value) api.Value {
		return spec.With("services", api.ListValue(append(spec.Get("services").Items(), extra...)...))
	}
}

func stretchRGW(placement api.Value) api.Value {
	return m("service_type", "rgw", "service_id", "objects", "placement", placement)
}

func siteIngress(id, vip string, hosts api.Value, spec ...any) api.Value {
	base := m("backend_service", "rgw.objects", "virtual_ip", vip, "frontend_port", 8080, "monitor_port", 8999)
	for i := 0; i+1 < len(spec); i += 2 {
		base = base.With(spec[i].(string), v(spec[i+1]))
	}
	return m("service_type", "ingress", "service_id", id, "placement", m("hosts", hosts), "spec", base)
}

func siteIngresses(changes ...any) []api.Value {
	return []api.Value{siteIngress("objects-a", "192.0.2.100/24", fqdns("a1", "a2")), siteIngress("objects-b", "192.0.2.101/24", fqdns("b1", "b2"), append([]any{"first_virtual_router_id", 60}, changes...)...)}
}

func withPlacement(service api.Value, placement api.Value) api.Value {
	return service.With("placement", placement)
}

func TestStretchHAServicesReachBothDataSites(t *testing.T) {
	mgr := m("service_type", "mgr", "placement", m("hosts", fqdns("a1", "b1")))
	for _, test := range []struct {
		name, field, message string
		service              api.Value
	}{
		{"an mgr at one data site", "$.spec.services[1].placement", "places no host at data site site-b", withPlacement(mgr, m("hosts", fqdns("a1", "a2")))},
		{"an mgr count one site can hold", "$.spec.services[1].placement.count", "lets cephadm place every daemon at one data site, which holds 3 of its hosts", withPlacement(mgr, m("label", "storage", "count", 3))},
		{"an mds at one data site by label", "$.spec.services[3].placement", "the mds service of a stretch cluster places no host at data site site-a", m("service_type", "mds", "service_id", "files", "placement", m("hosts", fqdns("b1", "b2")))},
		{"a direct rgw on fewer daemons than hosts", "$.spec.services[3].placement.count", "reached at every host's address", stretchRGW(m("hosts", fqdns("a1", "a2", "b1", "b2"), "count", 3))},
	} {
		t.Run(test.name, func(t *testing.T) {
			cluster, catalog := stretchCatalog(t, func(spec api.Value) api.Value {
				services := spec.Get("services")
				if textOf(test.service, "service_type") == "mgr" {
					return spec.With("services", withItem(services, 1, test.service))
				}
				return addServices(test.service)(spec)
			})
			requireOnlyIssue(t, Validate(cluster, catalog), test.field, test.message)
		})
	}
	for _, service := range []api.Value{
		withPlacement(mgr, m("label", "storage", "count", 4)),
		withPlacement(mgr, m("label", "storage", "count_per_host", 1)),
		withPlacement(mgr, m("label", "storage")),
		m("service_type", "mds", "service_id", "files", "placement", m("hosts", fqdns("a1", "b1"), "count", 2)),
		stretchRGW(m("hosts", fqdns("a1", "b1"), "count", 2)),
	} {
		cluster, catalog := stretchCatalog(t, func(spec api.Value) api.Value {
			if textOf(service, "service_type") == "mgr" {
				return spec.With("services", withItem(spec.Get("services"), 1, service))
			}
			return addServices(service)(spec)
		})
		if issues := Validate(cluster, catalog); len(issues) != 0 {
			t.Fatalf("a placement reaching both data sites refused (%v): %#v", service, issues)
		}
	}
}

func TestStretchRGWFrontDoorsArePerSite(t *testing.T) {
	rgw := stretchRGW(m("hosts", fqdns("a1", "a2", "b1", "b2")))
	ingresses := siteIngresses()
	cluster, catalog := stretchCatalog(t, addServices(append([]api.Value{rgw}, ingresses...)...))
	if issues := Validate(cluster, catalog); len(issues) != 0 {
		t.Fatalf("one ingress per data site refused: %#v", issues)
	}
	for _, test := range []struct {
		name, field, message string
		services             []api.Value
	}{
		{"a stretched VIP", "$.spec.services[4].placement", "an ingress whose hosts span both data sites", []api.Value{rgw, siteIngress("objects-a", "192.0.2.100/24", fqdns("a1", "b1")), siteIngress("objects-b", "192.0.2.101/24", fqdns("b2", "b3"), "first_virtual_router_id", 60)}},
		{"a single site-local ingress", "$.spec.services[4].spec.backend_service", "has 1 ingresses, not one per data site", []api.Value{rgw, ingresses[0]}},
		{"two ingresses at one site", "$.spec.services[5].placement", "placed one per data site", []api.Value{rgw, ingresses[0], siteIngress("objects-b", "192.0.2.101/24", fqdns("a2", "a3"), "first_virtual_router_id", 60)}},
		{"per-site ingresses on two ports", "$.spec.services[5].spec.frontend_port", "they share spec.frontend_port", append([]api.Value{rgw}, siteIngresses("frontend_port", 8081)...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			cluster, catalog := stretchCatalog(t, addServices(test.services...))
			requireOnlyIssue(t, clusterIssuesAt(Validate(cluster, catalog), test.field), test.field, test.message)
		})
	}
	plain := find(t, fixtureCatalog(), api.CephCluster, "storage")
	if slices.ContainsFunc(Validate(plain, fixtureCatalog()), func(issue api.Issue) bool { return strings.Contains(issue.Message, "data site") }) {
		t.Fatal("a cluster without stretch took a stretch placement rule")
	}
}

func TestStretchPerSiteIngressesBindOneSecret(t *testing.T) {
	bind := func(service, secret string) api.Value {
		return m("serviceRef", service, "secrets", []api.Value{m("path", "/spec/ssl_cert", "secretRef", secret), m("path", "/spec/ssl_key", "secretRef", secret)})
	}
	tls := func(secretB string) func(api.Value) api.Value {
		return func(spec api.Value) api.Value {
			ingresses := siteIngresses("ssl", true)
			ingresses[0] = ingresses[0].WithPath(api.BoolValue(true), "spec", "ssl")
			spec = addServices(append([]api.Value{stretchRGW(m("hosts", fqdns("a1", "a2", "b1", "b2")))}, ingresses...)...)(spec)
			return spec.With("serviceBindings", api.ListValue(bind("ingress.objects-a", "gateway-tls"), bind("ingress.objects-b", secretB)))
		}
	}
	cluster, catalog := stretchCatalog(t, tls("gateway-tls"))
	same := clusterIssuesAt(Validate(cluster, catalog), "$.spec.services[5].spec.ssl")
	cluster, catalog = stretchCatalog(t, tls("other-tls"))
	requireOnlyIssue(t, clusterIssuesAt(Validate(cluster, catalog), "$.spec.services[5].spec.ssl"), "$.spec.services[5].spec.ssl", "bind the same Secret")
	if len(same) != 0 {
		t.Fatalf("per-site ingresses binding one Secret refused: %#v", same)
	}
}

func TestStretchRefusesNVMeoFNFSAndSMB(t *testing.T) {
	cluster, catalog := stretchCatalog(t, addServices(m("service_type", "nfs", "service_id", "files", "placement", m("hosts", fqdns("a1", "b1")))))
	requireIssue(t, Validate(cluster, catalog), "$.spec.services[3].service_type")
	for _, kind := range []api.Kind{api.CephNFSExport, api.CephSMBResource} {
		child := object(kind, "child", m("clusterRef", "storage"))
		requireOnlyIssue(t, Validate(child, replace(catalog, child)), "$.spec.clusterRef", "NFS and SMB on a stretch cluster have no qualified profile")
	}
	if _, _, refused := stretchedChildRefusal(api.CephObjectGateway); refused {
		t.Fatal("a gateway of a stretch cluster took the NFS and SMB refusal")
	}
}
