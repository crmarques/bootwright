package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageContradictionsNameCorrectiveActions(t *testing.T) {
	for _, test := range []struct {
		spec          api.Value
		field, remedy string
	}{
		{m("replicated", m("size", 2, "minSize", 3)), "$.spec.replicated", "lower minSize or raise size in $.spec.replicated so minSize does not exceed size"},
		{m("autoscale", m("pgNumMin", 20, "pgNumMax", 10)), "$.spec.autoscale", "lower spec.autoscale.pgNumMin or raise spec.autoscale.pgNumMax so the minimum does not exceed the maximum"},
	} {
		issues := ValidatePartial(object(api.CephPool, "pool", test.spec), fixtureCatalog())
		found := issuesAt(issues, test.field)
		if len(found) != 1 || found[0].Remediation != test.remedy {
			t.Fatalf("%s: %#v", test.field, found)
		}
	}
}

func TestCephEntitlementDiagnosticsNameProductAndField(t *testing.T) {
	for _, test := range []struct {
		product       string
		spec          api.Value
		field, action string
	}{
		{"ibmStorageCeph", m("license", m("accept", false)), "$.spec.license.accept", "true"},
		{"redhatCeph", m(), "$.spec.rhsm", "management"},
		{"redhatCeph", m("rhsm", m("management", "external", "organizationRef", "org")), "$.spec.rhsm.organizationRef", "remove"},
		{"redhatCeph", m("rhsm", m("management", "managed")), "$.spec.rhsm.organizationRef", "set"},
		{"redhatCeph", m("rhsm", m("management", "external")), "$.spec.registry", "credentialsRef"},
		{"redhatCeph", m("rhsm", m("management", "external"), "registry", m()), "$.spec.registry.credentialsRef", "set"},
	} {
		obj := object(api.Entitlement, "vendor", test.spec.With("type", api.StringValue(test.product)))
		found := issuesAt(Validate(obj, fixtureCatalog()), test.field)
		if len(found) == 0 {
			t.Fatalf("no issue for %s", test.field)
		}
		for _, issue := range found {
			if !strings.Contains(issue.Message, test.product) || !strings.Contains(issue.Message, test.field) || !strings.Contains(issue.Remediation, test.action) || !strings.Contains(issue.Remediation, test.field) {
				t.Fatalf("product/field/action missing: %#v", issue)
			}
		}
	}
}

func TestStretchLocationRemedyNamesTheAuthoredFailureDomain(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	nodes := cluster.Spec().Get("topology", "nodes").Items()
	nodes[0] = nodes[0].With("site", api.StringValue("east")).With("location", m("rack", "west"))
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(nodes...), "topology", "nodes").WithPath(m("failureDomain", "rack"), "topology", "stretch"))
	found := issuesAt(Validate(changed, replace(catalog, changed)), "$.spec.topology.nodes[0].location")
	if len(found) != 1 || found[0].Remediation != "set location.rack to the topology node's proved site" {
		t.Fatalf("authored failure domain absent from remedy: %#v", found)
	}
}

func TestMonitorLocationRemedyRemovesGeneratedHostBucket(t *testing.T) {
	cluster := object(api.CephCluster, "storage", m("services", api.ListValue(m("service_type", "mon", "spec", m("crush_locations", m("node-1", api.StringList("host=node-1")))))))
	found := issuesAt(ValidatePartial(cluster, api.Catalog{}), "$.spec.services[0].spec.crush_locations")
	if len(found) != 1 || !strings.Contains(found[0].Remediation, "remove the host bucket") {
		t.Fatalf("generated host bucket removal absent from remedy: %#v", found)
	}
}
