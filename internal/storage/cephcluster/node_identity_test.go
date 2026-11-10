package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func nodeIdentityCatalog(nodes ...api.Value) api.Catalog {
	return api.NewCatalog([]api.Object{
		object(api.Environment, "environment", m("domains", m("base", "example.test"), "sites", []api.Value{m("name", "site-a"), m("name", "site-b")})),
		object(api.Machine, "machine-1", m("placement", m("site", "site-b"), "os", m("provided", false, "installProfileRef", "rhel"),
			"network", m("addresses", []api.Value{m("name", "fqdn", "address", "machine-1.example.test")}))),
		object(api.Machine, "machine-2", m("os", m("provided", true))),
		object(api.CephCluster, "storage", m("topology", m("nodes", nodes))),
	})
}

func TestATopologyNodeTakesItsMachineFQDNAndSite(t *testing.T) {
	catalog := nodeIdentityCatalog(m("name", "node-1", "machineRef", "machine-1"), m("name", "node-2", "machineRef", "machine-2"))
	normalized, _ := Normalize(find(t, catalog, api.CephCluster, "storage"), catalog)
	nodes := normalized.Spec().Get("topology", "nodes").Items()
	if got := textOf(nodes[0], "fqdn"); got != "machine-1.example.test" {
		t.Fatalf("a node over a Machine with an fqdn contact registers as %q", got)
	}
	if got := textOf(nodes[0], "site"); got != "site-b" {
		t.Fatalf("a node takes site %q, not its Machine's placement.site", got)
	}
	if got := textOf(nodes[1], "fqdn"); got != "node-2.storage.example.test" {
		t.Fatalf("a node over a Machine without an fqdn contact registers as %q", got)
	}
	if nodes[1].Has("site") {
		t.Fatal("a node over a Machine without placement.site gained a site")
	}
}

func TestAnAuthoredTopologyNodeSiteRefuses(t *testing.T) {
	catalog := nodeIdentityCatalog(m("name", "node-1", "machineRef", "machine-1"), m("name", "node-2", "machineRef", "machine-2", "site", "site-a"))
	issues := ValidateAuthored(find(t, catalog, api.CephCluster, "storage"), catalog)
	if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != "$.spec.topology.nodes[1].site" || !strings.Contains(issues[0].Remediation, "Machine/machine-2") {
		t.Fatalf("authored node site: %+v", issues)
	}
}

func TestAnAuthoredNodeFQDNDifferingFromItsInstalledMachineRefuses(t *testing.T) {
	for _, test := range []struct {
		name, fqdn, machine string
		refused             bool
	}{
		{"differs on an installed Machine", "node-1.storage.example.test", "machine-1", true},
		{"matches an installed Machine", "machine-1.example.test", "machine-1", false},
		{"omitted", "", "machine-1", false},
		{"on a provided Machine", "elsewhere.example.test", "machine-2", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := m("name", "node-1", "machineRef", test.machine)
			if test.fqdn != "" {
				node = node.With("fqdn", api.StringValue(test.fqdn))
			}
			catalog := nodeIdentityCatalog(node)
			cluster, _ := Normalize(find(t, catalog, api.CephCluster, "storage"), catalog)
			a := admission{object: cluster, catalog: replace(catalog, cluster)}
			a.topology(cluster.Spec())
			var refusal *api.Issue
			for i := range a.issues {
				if a.issues[i].Field == "$.spec.topology.nodes[0].fqdn" {
					refusal = &a.issues[i]
				}
			}
			if !test.refused {
				if refusal != nil {
					t.Fatalf("refused %+v", *refusal)
				}
				return
			}
			if refusal == nil || refusal.Code != "api.invariant" || !strings.Contains(refusal.Message, test.fqdn) || !strings.Contains(refusal.Message, "machine-1.example.test") {
				t.Fatalf("mismatch refusal %+v in %+v", refusal, a.issues)
			}
		})
	}
}
