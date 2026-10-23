package storage

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func hasIssue(issues []api.Issue, want api.Issue) bool {
	for _, issue := range issues {
		if issue == want {
			return true
		}
	}
	return false
}

func TestStorageExportManagedAdmission(t *testing.T) {
	catalog := fixtureCatalog()
	export := find(t, catalog, api.StorageExport, "export")
	for _, issue := range Validate(export, catalog) {
		if strings.Contains(issue.Remediation, "B86") || strings.Contains(issue.Remediation, "B489") {
			t.Fatalf("stale deferral: %+v", issue)
		}
	}
	external := object(api.StorageCluster, "external", m("type", "ceph", "management", "external"))
	ext := object(api.StorageExport, "external-export", m("clusterRef", "external", "externalDetails", m("fromSecretRef", "details")))
	want := api.Issue{Code: "api.invariant", Field: "$.spec", Message: "this storage resource family requires its later qualified service slice", Remediation: "remove this StorageExport from the input; parked item B107 delivers it"}
	if got := Validate(ext, api.NewCatalog([]api.Object{external, ext})); !hasIssue(got, want) {
		t.Fatalf("missing external refusal: %+v", got)
	}
}

func validExportCatalog(t *testing.T) api.Catalog {
	t.Helper()
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	services := append(cluster.Spec().Get("ceph", "services").Items(), m("service_type", "prometheus", "placement", m("hosts", api.StringList("node-1"))))
	catalog = replace(catalog, cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services")))
	fs := find(t, catalog, api.StorageFilesystem, "files")
	catalog = replace(catalog, fs.WithSpec(fs.Spec().With("subvolumeGroups", api.ListValue(m("name", "csi")))))
	gateway := find(t, catalog, api.StorageObjectGateway, "objects")
	return replace(catalog, gateway.WithSpec(gateway.Spec().With("endpoint", m("fqdn", "objects.example.test"))))
}
func TestManagedExportAdmissionRulesAndPassingTwin(t *testing.T) {
	catalog := validExportCatalog(t)
	export := find(t, catalog, api.StorageExport, "export")
	if issues := Validate(export, catalog); len(issues) != 0 {
		t.Fatalf("passing export refused: %+v", issues)
	}
	cases := []struct {
		name, field string
		change      func(api.Catalog) api.Catalog
	}{
		{"filesystem group", "$.spec.dataFoundation.filesystemRef", func(c api.Catalog) api.Catalog {
			fs := find(t, c, api.StorageFilesystem, "files")
			return replace(c, fs.WithSpec(fs.Spec().With("subvolumeGroups", api.ListValue(m("name", "other")))))
		}},
		{"gateway endpoint", "$.spec.dataFoundation.objectGatewayRef", func(c api.Catalog) api.Catalog {
			g := find(t, c, api.StorageObjectGateway, "objects")
			return replace(c, g.WithSpec(g.Spec().With("endpoint", m("fqdn", ""))))
		}},
		{"monitoring", "$.spec.clusterRef", func(c api.Catalog) api.Catalog {
			cl := find(t, c, api.StorageCluster, "storage")
			return replace(c, cl.WithSpec(cl.Spec().WithPath(api.ListValue(), "ceph", "services")))
		}},
		{"replication", "$.spec.dataFoundation.rbdPoolRef", func(c api.Catalog) api.Catalog {
			pool := find(t, c, api.StoragePool, "block")
			return replace(c, pool.WithSpec(pool.Spec().With("type", api.StringValue("erasure"))))
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			issues := Validate(export, tt.change(catalog))
			found := false
			for _, issue := range issues {
				if issue.Field == tt.field && issue.Remediation != "" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing refusal: %+v", issues)
			}
		})
	}
	normalized, issues := Normalize(export, catalog)
	if len(issues) != 0 || normalized.Spec().Get("dataFoundation", "cephxKeyType").Text() != "aes" {
		t.Fatalf("key type not normalized: %+v", issues)
	}
	facts := AddonExportFacts(catalog)
	if len(facts) != 1 || facts[0].Name != "export" || !facts[0].Gateway || facts[0].TLS {
		t.Fatalf("export facts: %+v", facts)
	}
}
