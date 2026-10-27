package cephcluster

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func withMDSService(t *testing.T, catalog api.Catalog, service api.Value) (api.Object, api.Catalog) {
	t.Helper()
	cluster := find(t, catalog, api.CephCluster, "storage")
	services := []api.Value{}
	for _, existing := range cluster.Spec().Get("services").Items() {
		if textOf(existing, "service_type") != "mds" {
			services = append(services, existing)
		}
	}
	services = append(services, service)
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	return changed, replace(catalog, changed)
}

func mdsService(id ...string) api.Value {
	service := m("service_type", "mds", "placement", m("label", "mgr", "count", 2))
	if len(id) == 1 {
		service = service.With("service_id", api.StringValue(id[0]))
	}
	return service
}

func issuesAt(issues []api.Issue, field string) []api.Issue {
	found := []api.Issue{}
	for _, issue := range issues {
		if issue.Field == field {
			found = append(found, issue)
		}
	}
	return found
}

func partialMDS(service api.Value) api.Object {
	return object(api.CephCluster, "storage", m("services", api.ListValue(m("service_type", "mon"), m("service_type", "mgr"), m("service_type", "osd", "service_id", "data"), service)))
}

func TestMDSServiceRequiresServiceID(t *testing.T) {
	cluster, catalog := withMDSService(t, fixtureCatalog(), mdsService())
	found := issuesAt(Validate(cluster, catalog), "$.spec.services[3].service_id")
	if len(found) != 1 || found[0].Code != "api.required" {
		t.Fatalf("MDS service without service_id: %#v", found)
	}
	if found := issuesAt(ValidatePartial(partialMDS(mdsService()), catalog), "$.spec.services[3].service_id"); len(found) != 0 {
		t.Fatalf("partial MDS input requires its ID too early: %#v", found)
	}
}

func TestMDSServiceIDCannotStartWithDigit(t *testing.T) {
	const field = "$.spec.services[3].service_id"
	cluster, catalog := withMDSService(t, fixtureCatalog(), mdsService("1files"))
	for _, issues := range [][]api.Issue{Validate(cluster, catalog), ValidatePartial(partialMDS(mdsService("1files")), catalog)} {
		found := issuesAt(issues, field)
		if len(found) != 1 || found[0].Code != "api.invariant" || found[0].Message != "MDS service IDs cannot start with a numeric digit" {
			t.Fatalf("leading-digit MDS ID: %#v", found)
		}
	}
	cluster, catalog = withMDSService(t, fixtureCatalog(), mdsService("files"))
	if found := issuesAt(Validate(cluster, catalog), field); len(found) != 0 {
		t.Fatalf("bound MDS ID refused: %#v", found)
	}
}

func TestServiceIDTableMatchesCephadmRequiresServiceID(t *testing.T) {
	// REQUIRES_SERVICE_ID, https://github.com/ceph/ceph/blob/v20.2.1/src/python-common/ceph/deployment/service_spec.py#L804-L813
	requiresServiceID := []string{"container", "ingress", "iscsi", "mds", "nfs", "nvmeof", "rgw", "smb"}
	core := []string{}
	for _, row := range nativeServiceTypes {
		switch row.serviceID {
		case serviceIDNative:
			if !slices.Contains(requiresServiceID, row.name) {
				t.Fatalf("%s requires a native ID cephadm does not require", row.name)
			}
		case serviceIDBootwright:
			if row.name != "osd" {
				t.Fatalf("%s requires a Bootwright-only ID", row.name)
			}
		case serviceIDForbidden:
			if slices.Contains(requiresServiceID, row.name) || row.name == "osd" {
				t.Fatalf("%s forbids an ID cephadm admits", row.name)
			}
		}
		if row.core {
			core = append(core, row.name)
		}
		if shape := api.StorageNativeServiceSpecSchema(row.name); shape == nil || shape.Open {
			t.Fatalf("%s row lacks a closed native schema", row.name)
		}
	}
	for _, name := range []string{"mon", "mgr", "osd"} {
		row, ok := nativeServiceTypeOf(name)
		if !ok || (name == "osd") != (row.serviceID == serviceIDBootwright) || (name != "osd") != (row.serviceID == serviceIDForbidden) {
			t.Fatalf("%s has the wrong service_id rule", name)
		}
	}
	if !slices.Equal(core, []string{"mon", "mgr", "osd"}) {
		t.Fatalf("core rows: %v", core)
	}
}

func TestUnimplementedServiceRemedyNamesImplementedTypes(t *testing.T) {
	cluster, catalog := withMDSService(t, fixtureCatalog(), m("service_type", "example-family", "placement", m("label", "mgr")))
	found := issuesAt(Validate(cluster, catalog), "$.spec.services[3].service_type")
	if len(found) != 1 || found[0].Remediation != "select an implemented MON, MGR, OSD, MDS, RGW, ingress, NVMe-oF, NFS, Prometheus, Alertmanager, Grafana, node-exporter, ceph-exporter, SNMP gateway, management gateway, oauth2-proxy, Loki, Alloy, Promtail or RBD mirror service, or wait for its owning service slice" {
		t.Fatalf("unimplemented family remedy: %#v", found)
	}
}

func rbdMirrorService(fields ...any) api.Value {
	return m(append([]any{"service_type", "rbd-mirror", "placement", m("label", "mgr", "count", 2)}, fields...)...)
}

func TestRBDMirrorServiceAdmittedWithPlacementOnly(t *testing.T) {
	const field = "$.spec.services[3]"
	for _, service := range []api.Value{rbdMirrorService(), rbdMirrorService("networks", api.StringList("192.0.2.0/24"))} {
		cluster, catalog := withMDSService(t, fixtureCatalog(), service)
		if found := issuesUnder(Validate(cluster, catalog), field); len(found) != 0 {
			t.Fatalf("rbd-mirror with placement refused: %#v", found)
		}
	}
	cluster, catalog := withMDSService(t, fixtureCatalog(), rbdMirrorService("spec", m("mode", "x")))
	if found := issuesUnder(Validate(cluster, catalog), field); len(found) != 1 || found[0].Code != "api.field" || !strings.HasPrefix(found[0].Field, field+".spec") {
		t.Fatalf("rbd-mirror spec member: %#v", found)
	}
	cluster, catalog = withMDSService(t, fixtureCatalog(), rbdMirrorService("service_id", "a"))
	exactIssue(t, Validate(cluster, catalog), "api.invariant", field+".service_id", "native singleton services omit service_id; other IDs use the qualified name grammar", "")
}

func TestCephFSMirrorServiceRefusedAsNotRealized(t *testing.T) {
	const field = "$.spec.services[3]"
	cluster, catalog := withMDSService(t, fixtureCatalog(), m("service_type", "cephfs-mirror", "placement", m("label", "mgr")))
	issues := issuesUnder(Validate(cluster, catalog), field)
	exactIssue(t, issues, "api.invariant", field+".service_type", "Bootwright does not realize CephFS snapshot mirroring", "remove this cephfs-mirror service from spec.services")
	if len(issues) != 1 || strings.ContainsAny(issues[0].Message+issues[0].Remediation, "0123456789") || strings.Contains(issues[0].Message+issues[0].Remediation, "Preview") || strings.Contains(issues[0].Message+issues[0].Remediation, "IBM") {
		t.Fatalf("cephfs-mirror refusal: %#v", issues)
	}
}

func TestMirroringManagerModuleRefused(t *testing.T) {
	cluster := find(t, fixtureCatalog(), api.CephCluster, "storage")
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringList("mirroring"), "mgrModules"))
	issues := Validate(cluster, replace(fixtureCatalog(), cluster))
	exactIssue(t, issues, "api.invariant", "$.spec.mgrModules[0]", "manager module mirroring serves CephFS snapshot mirroring, which Bootwright does not realize", "remove mirroring from spec.mgrModules")
	if found := issuesAt(issues, "$.spec.mgrModules"); len(found) != 0 {
		t.Fatalf("generic module refusal: %#v", found)
	}
}

func TestUnboundMDSServiceRefuses(t *testing.T) {
	const field = "$.spec.services[3].service_id"
	const message = "MDS service is not bound by a CephFilesystem of this cluster"
	cluster, catalog := withMDSService(t, fixtureCatalog(), mdsService("files"))
	if found := issuesAt(Validate(cluster, catalog), field); len(found) != 0 {
		t.Fatalf("bound MDS service refused: %#v", found)
	}
	cluster, catalog = withMDSService(t, fixtureCatalog(), mdsService("other"))
	found := issuesAt(Validate(cluster, catalog), field)
	if len(found) != 1 || found[0].Message != message || strings.Contains(found[0].Message+found[0].Remediation, "other") {
		t.Fatalf("unbound MDS service: %#v", found)
	}
	filesystem := find(t, catalog, api.CephFilesystem, "files")
	moved := replace(fixtureCatalog(), filesystem.WithSpec(filesystem.Spec().With("clusterRef", api.StringValue("elsewhere"))))
	cluster, catalog = withMDSService(t, moved, mdsService("files"))
	if found := issuesAt(Validate(cluster, catalog), field); len(found) != 1 || found[0].Message != message {
		t.Fatalf("filesystem of another cluster bound the MDS service: %#v", found)
	}
	for _, issue := range ValidatePartial(partialMDS(mdsService("other")), catalog) {
		if issue.Message == message {
			t.Fatalf("partial input checked the binding: %#v", issue)
		}
	}
}

func TestNativeServiceResolvesQualifiedReference(t *testing.T) {
	cluster, _ := withMDSService(t, fixtureCatalog(), mdsService("files"))
	for ref, want := range map[string]bool{"osd.data": true, "mds.files": true, "mon": true, "mds": false, "mds.other": false, "osd": false} {
		service, ok := NativeService(cluster, ref)
		if ok != want || ok && NativeServiceName(service) != ref {
			t.Fatalf("NativeService(%q) = %v, %v", ref, service, ok)
		}
	}
	if _, ok := NativeService(find(t, fixtureCatalog(), api.CephCluster, "storage"), "mds.files"); ok {
		t.Fatal("an undeclared MDS service resolved")
	}
}
