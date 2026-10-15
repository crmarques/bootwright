package storage

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPartialValidationIssuesAlsoRefuseTheCompleteObject(t *testing.T) {
	catalog := fixtureCatalog()
	edit := func(kind api.Kind, name string, change func(api.Value) api.Value) api.Object {
		t.Helper()
		object := find(t, catalog, kind, name)
		return object.WithSpec(change(object.Spec()))
	}
	service := func(spec api.Value, index int, change func(api.Value) api.Value) api.Value {
		services := spec.Get("ceph", "services").Items()
		services[index] = change(services[index])
		return spec.WithPath(api.ListValue(services...), "ceph", "services")
	}
	appendService := func(added api.Value) api.Object {
		return edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return s.WithPath(api.ListValue(append(s.Get("ceph", "services").Items(), added)...), "ceph", "services")
		})
	}
	rgw := m("service_type", "rgw", "service_id", "objects", "placement", m("label", "storage"))
	nfs := m("service_type", "nfs", "service_id", "files", "placement", m("label", "storage"))
	ingress := m("service_type", "ingress", "service_id", "rgw.objects", "placement", m("label", "storage"), "spec", m("backend_service", "rgw.objects", "virtual_ip", "192.0.2.50/24"))
	entitlement := object(api.Entitlement, "ibm", m("type", "ibm-storage-ceph", "registry", m("credentialsRef", "registry"), "license", m("accept", false)))
	for _, test := range []struct {
		name   string
		object api.Object
	}{
		{"pool compression bounds", edit(api.StoragePool, "block", func(s api.Value) api.Value {
			return s.With("compression", m("minBlobSize", 20, "maxBlobSize", 10))
		})},
		{"pool PG bounds", edit(api.StoragePool, "block", func(s api.Value) api.Value { return s.With("autoscale", m("pgNumMin", 20, "pgNumMax", 10)) })},
		{"pool autoscale targets", edit(api.StoragePool, "block", func(s api.Value) api.Value {
			return s.With("autoscale", m("targetSizeRatio", api.NumberValue("0.5"), "targetSizeBytes", "1Gi"))
		})},
		{"policy-owned protection", edit(api.StoragePool, "metadata", func(s api.Value) api.Value { return s.With("replicated", m("size", 2)) })},
		{"erasure with replicas", edit(api.StoragePool, "block", func(s api.Value) api.Value {
			return s.With("type", api.StringValue("erasure")).With("replicated", m("size", 2))
		})},
		{"policy replica bounds", edit(api.StoragePlacementPolicy, "policy", func(s api.Value) api.Value {
			return s.With("replicated", m("size", 2, "minSize", 3))
		})},
		{"oss vendor field", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return s.WithPath(api.StringValue("ibm"), "ceph", "entitlementRef").WithPath(api.StringValue("oss"), "ceph", "distribution")
		})},
		{"native count conflict", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return service(s, 0, func(v api.Value) api.Value {
				return v.With("placement", m("label", "mon", "count", 3, "count_per_host", 1))
			})
		})},
		{"native count_per_host selector", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return service(s, 2, func(v api.Value) api.Value { return v.With("placement", m("count_per_host", 1)) })
		})},
		{"native decorated count_per_host", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return service(s, 2, func(v api.Value) api.Value {
				return v.With("placement", m("hosts", api.StringList("node-1=a"), "count_per_host", 1))
			})
		})},
		{"native singleton identity", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return service(s, 0, func(v api.Value) api.Value { return v.With("service_id", api.StringValue("extra")) })
		})},
		{"native unmanaged", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return service(s, 1, func(v api.Value) api.Value { return v.With("unmanaged", api.BoolValue(true)) })
		})},
		{"native effect option", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return service(s, 1, func(v api.Value) api.Value { return v.With("extra_container_args", api.StringList("--privileged")) })
		})},
		{"native typed config", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return service(s, 1, func(v api.Value) api.Value { return v.With("config", m("public_network", "192.0.2.0/24")) })
		})},
		{"native OSD encryption", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return service(s, 2, func(v api.Value) api.Value {
				return v.WithPath(api.BoolValue(true), "spec", "tpm2").WithPath(api.BoolValue(false), "spec", "encrypted")
			})
		})},
		{"native binding paths", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			return s.WithPath(api.ListValue(m("serviceRef", "mgr", "secrets", api.ListValue(m("path", "/spec/key", "secretRef", "a"), m("path", "/spec/key/child", "secretRef", "b")))), "ceph", "serviceBindings")
		})},
		{"native RGW legacy certificate", appendService(rgw.With("spec", m("rgw_frontend_ssl_certificate", "material")))},
		{"native RGW signed certificate", appendService(rgw.With("spec", m("ssl", true, "certificate_source", "cephadm-signed")))},
		{"native RGW colocated daemons", appendService(rgw.With("placement", m("label", "storage", "count_per_host", 2)))},
		{"native NFS version 3", appendService(nfs.With("spec", m("enable_nfsv3", true)))},
		{"native NFS colocated daemons", appendService(nfs.With("placement", m("label", "storage", "count_per_host", 1)))},
		{"native NFS prefixed VIP", appendService(nfs.With("spec", m("virtual_ip", "192.0.2.60/24")))},
		{"native ingress password", appendService(ingress.WithPath(api.StringValue("material"), "spec", "monitor_password"))},
		{"native ingress interval", appendService(ingress.WithPath(api.StringValue("5x"), "spec", "health_check_interval"))},
		{"native ingress bare VIP", appendService(ingress.WithPath(api.StringValue("192.0.2.60"), "spec", "virtual_ip"))},
		{"native ingress modes", appendService(ingress.WithPath(api.BoolValue(true), "spec", "keepalive_only").WithPath(api.BoolValue(true), "spec", "enable_haproxy_protocol"))},
		{"native ingress host bits", appendService(ingress.WithPath(api.StringList("192.0.2.5/24"), "spec", "virtual_interface_networks"))},
		{"native ingress signed certificate", appendService(ingress.WithPath(api.StringValue("cephadm-signed"), "spec", "certificate_source"))},
		{"native NFS binding", edit(api.StorageCluster, "storage", func(s api.Value) api.Value {
			s = s.WithPath(api.ListValue(append(s.Get("ceph", "services").Items(), nfs)...), "ceph", "services")
			return s.WithPath(api.ListValue(m("serviceRef", "nfs.files", "secrets", api.ListValue(m("path", "/spec/ssl_cert", "secretRef", "a")))), "ceph", "serviceBindings")
		})},
		{"gateway plain TLS", edit(api.StorageObjectGateway, "objects", func(s api.Value) api.Value {
			return s.WithPath(m("certificateRef", "certificate"), "endpoint", "tls")
		})},
		{"NFS ingress backend port", edit(api.StorageNFSExport, "nfs", func(s api.Value) api.Value {
			return s.With("ingresses", api.ListValue(m("name", "nfs-ingress", "address", "192.0.2.60", "prefixLength", 24))).With("port", api.IntegerValue("2049"))
		})},
		{"NFS export backing", edit(api.StorageNFSExport, "nfs", func(s api.Value) api.Value {
			return s.With("exports", api.ListValue(m("pseudo", "/files", "filesystemRef", "files", "bucket", "also")))
		})},
		{"export modes", edit(api.StorageExport, "export", func(s api.Value) api.Value { return s.With("externalDetails", m("fromSecretRef", "details")) })},
		{"IBM license", entitlement},
		{"external RHSM", object(api.Entitlement, "rhsm", m("type", "redhat-ceph", "registry", m("credentialsRef", "registry"), "rhsm", m("management", "external", "organizationRef", "org")))},
	} {
		t.Run(test.name, func(t *testing.T) {
			partial := ValidatePartial(test.object, catalog)
			if len(partial) == 0 {
				t.Fatal("partial validation admitted the contradiction")
			}
			full := Validate(test.object, replace(catalog, test.object))
			for _, issue := range partial {
				if !slices.ContainsFunc(full, func(other api.Issue) bool {
					return other.Code == issue.Code && other.Field == issue.Field && other.Message == issue.Message
				}) {
					t.Errorf("partial issue %+v is absent from full validation %+v", issue, full)
				}
			}
		})
	}
}

func TestStretchSitesResolveAgainstTheEnvironment(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	for _, test := range []struct {
		name, field string
		stretch     api.Value
	}{
		{"undeclared data site", "$.spec.ceph.topology.stretch.dataSites[1]", m("dataSites", api.StringList("site-a", "site-z"), "tiebreaker", m("site", "site-b"))},
		{"undeclared tiebreaker site", "$.spec.ceph.topology.stretch.tiebreaker.site", m("dataSites", api.StringList("site-a", "site-b"), "tiebreaker", m("site", "site-z"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := cluster.WithSpec(cluster.Spec().WithPath(test.stretch, "ceph", "topology", "stretch"))
			issues := Validate(changed, replace(catalog, changed))
			requireIssue(t, issues, test.field)
			for _, issue := range issues {
				if issue.Field == test.field && issue.Message != "storage site must resolve to a declared Environment site" {
					t.Fatalf("unexpected site refusal: %+v", issue)
				}
			}
		})
	}
	declared := cluster.WithSpec(cluster.Spec().WithPath(m("dataSites", api.StringList("site-a", "site-b"), "tiebreaker", m("site", "site-b")), "ceph", "topology", "stretch"))
	for _, issue := range Validate(declared, replace(catalog, declared)) {
		if issue.Message == "storage site must resolve to a declared Environment site" {
			t.Fatalf("declared sites were refused: %+v", issue)
		}
	}
}
