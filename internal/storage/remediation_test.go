package storage

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
		issues := ValidatePartial(object(api.StoragePool, "pool", test.spec), fixtureCatalog())
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
		{"ibm-storage-ceph", m("rhsm", m("management", "external")), "$.spec.rhsm", "remove"},
		{"ibm-storage-ceph", m("license", m("accept", false)), "$.spec.license.accept", "true"},
		{"redhat-ceph", m(), "$.spec.rhsm", "management"},
		{"redhat-ceph", m("rhsm", m("management", "external", "organizationRef", "org")), "$.spec.rhsm.organizationRef", "remove"},
		{"redhat-ceph", m("rhsm", m("management", "managed")), "$.spec.rhsm.organizationRef", "set"},
		{"redhat-ceph", m("rhsm", m("management", "external")), "$.spec.registry", "credentialsRef"},
		{"redhat-ceph", m("rhsm", m("management", "external"), "registry", m()), "$.spec.registry.credentialsRef", "set"},
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
	cluster := find(t, catalog, api.StorageCluster, "storage")
	nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
	nodes[0] = nodes[0].With("site", api.StringValue("east")).With("location", m("rack", "west"))
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(nodes...), "ceph", "topology", "nodes").WithPath(m("failureDomain", "rack"), "ceph", "topology", "stretch"))
	found := issuesAt(Validate(changed, replace(catalog, changed)), "$.spec.ceph.topology.nodes[0].location")
	if len(found) != 1 || found[0].Remediation != "set location.rack to the topology node's proved site" {
		t.Fatalf("authored failure domain absent from remedy: %#v", found)
	}
}

func TestMonitorLocationRemedyRemovesGeneratedHostBucket(t *testing.T) {
	cluster := object(api.StorageCluster, "storage", m("ceph", m("services", api.ListValue(m("service_type", "mon", "spec", m("crush_locations", m("node-1", api.StringList("host=node-1"))))))))
	found := issuesAt(ValidatePartial(cluster, api.Catalog{}), "$.spec.ceph.services[0].spec.crush_locations")
	if len(found) != 1 || !strings.Contains(found[0].Remediation, "remove the host bucket") {
		t.Fatalf("generated host bucket removal absent from remedy: %#v", found)
	}
}
