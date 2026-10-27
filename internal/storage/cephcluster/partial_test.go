package cephcluster

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
		services := spec.Get("services").Items()
		services[index] = change(services[index])
		return spec.WithPath(api.ListValue(services...), "services")
	}
	appendService := func(added api.Value) api.Object {
		return edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return s.WithPath(api.ListValue(append(s.Get("services").Items(), added)...), "services")
		})
	}
	rgw := m("service_type", "rgw", "service_id", "objects", "placement", m("label", "storage"))
	nfs := m("service_type", "nfs", "service_id", "files", "placement", m("label", "storage"))
	ingress := m("service_type", "ingress", "service_id", "rgw.objects", "placement", m("label", "storage"), "spec", m("backend_service", "rgw.objects", "virtual_ip", "192.0.2.50/24"))
	entitlement := object(api.Entitlement, "ibm", m("type", "ibmStorageCeph", "registry", m("credentialsRef", "registry"), "license", m("accept", false)))
	for _, test := range []struct {
		name   string
		object api.Object
	}{
		{"pool compression bounds", edit(api.CephPool, "block", func(s api.Value) api.Value {
			return s.With("compression", m("minBlobSize", 20, "maxBlobSize", 10))
		})},
		{"pool PG bounds", edit(api.CephPool, "block", func(s api.Value) api.Value { return s.With("autoscale", m("pgNumMin", 20, "pgNumMax", 10)) })},
		{"pool autoscale targets", edit(api.CephPool, "block", func(s api.Value) api.Value {
			return s.With("autoscale", m("targetSizeRatio", api.NumberValue("0.5"), "targetSizeBytes", "1Gi"))
		})},
		{"policy-owned protection", edit(api.CephPool, "metadata", func(s api.Value) api.Value { return s.With("replicated", m("size", 2)) })},
		{"erasure with replicas", edit(api.CephPool, "block", func(s api.Value) api.Value {
			return s.With("erasure", m("dataChunks", 2, "codingChunks", 1)).With("replicated", m("size", 2))
		})},
		{"policy replica bounds", edit(api.CephPlacementPolicy, "policy", func(s api.Value) api.Value {
			return s.With("replicated", m("size", 2, "minSize", 3))
		})},
		{"auxiliary image version lexical", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return s.WithPath(m("base", "registry.example.test/haproxy", "version", "19.2\u00851"), "images", "haproxy")
		})},
		{"oss vendor field", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return s.WithPath(api.StringValue("ibm"), "entitlementRef").With("distribution", m("oss", m()))
		})},
		{"native count conflict", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return service(s, 0, func(v api.Value) api.Value {
				return v.With("placement", m("label", "mon", "count", 3, "count_per_host", 1))
			})
		})},
		{"native count_per_host selector", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return service(s, 2, func(v api.Value) api.Value { return v.With("placement", m("count_per_host", 1)) })
		})},
		{"native decorated count_per_host", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return service(s, 2, func(v api.Value) api.Value {
				return v.With("placement", m("hosts", api.StringList("node-1=a"), "count_per_host", 1))
			})
		})},
		{"native singleton identity", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return service(s, 0, func(v api.Value) api.Value { return v.With("service_id", api.StringValue("extra")) })
		})},
		{"native unmanaged", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return service(s, 1, func(v api.Value) api.Value { return v.With("unmanaged", api.BoolValue(true)) })
		})},
		{"native effect option", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return service(s, 1, func(v api.Value) api.Value { return v.With("extra_container_args", api.StringList("--privileged")) })
		})},
		{"native typed config", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return service(s, 1, func(v api.Value) api.Value { return v.With("config", m("public_network", "192.0.2.0/24")) })
		})},
		{"native OSD encryption", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return service(s, 2, func(v api.Value) api.Value {
				return v.WithPath(api.BoolValue(true), "spec", "tpm2").WithPath(api.BoolValue(false), "spec", "encrypted")
			})
		})},
		{"native binding paths", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return s.WithPath(api.ListValue(m("serviceRef", "mgr", "secrets", api.ListValue(m("path", "/spec/key", "secretRef", "a"), m("path", "/spec/key/child", "secretRef", "b")))), "serviceBindings")
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
		{"native gateway plaintext", appendService(m("service_type", "mgmt-gateway", "placement", m("label", "mgr"), "spec", m("ssl", false, "port", 9443)))},
		{"native gateway prefixed VIP", appendService(m("service_type", "mgmt-gateway", "placement", m("label", "mgr"), "spec", m("port", 9443, "virtual_ip", "192.0.2.50/24")))},
		{"native oauth2-proxy port", appendService(m("service_type", "oauth2-proxy", "placement", m("label", "mgr"), "spec", m("provider_display_name", "IdP", "client_id", "ceph", "oidc_issuer_url", "https://idp.example.test/realms/ceph", "https_address", "0.0.0.0:4181")))},
		{"native grafana inline password", appendService(m("service_type", "grafana", "placement", m("label", "mgr"), "spec", m("initial_admin_password", "material")))},
		{"native tracing service", appendService(m("service_type", "jaeger-agent", "placement", m("label", "mgr")))},
		{"native authored crash", appendService(m("service_type", "crash", "placement", m("label", "mgr")))},
		{"native iSCSI gateway", appendService(m("service_type", "iscsi", "service_id", "gw", "placement", m("label", "mgr")))},
		{"native SMB generated service", appendService(m("service_type", "smb", "service_id", "files", "placement", m("label", "mgr")))},
		{"native alertmanager webhook key", appendService(m("service_type", "alertmanager", "placement", m("label", "mgr"), "spec", m("user_data", m("webhook_urls", api.StringList("https://hooks.example.test/a")))))},
		{"native prometheus retention", appendService(m("service_type", "prometheus", "placement", m("label", "mgr"), "spec", m("retention_time", "14days")))},
		{"native retired workarounds", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			return s.WithPath(api.StringList("mgmt-gateway-spec-dependency-recording"), "cephadm", "workarounds")
		})},
		{"native NFS binding", edit(api.CephCluster, "storage", func(s api.Value) api.Value {
			s = s.WithPath(api.ListValue(append(s.Get("services").Items(), nfs)...), "services")
			return s.WithPath(api.ListValue(m("serviceRef", "nfs.files", "secrets", api.ListValue(m("path", "/spec/ssl_cert", "secretRef", "a")))), "serviceBindings")
		})},
		{"gateway pool roles share one pool", edit(api.CephObjectGateway, "objects", func(s api.Value) api.Value {
			return s.With("pools", m("indexPoolRef", "block", "dataPoolRef", "block"))
		})},
		{"NFS export backing", edit(api.CephNFSExport, "nfs", func(s api.Value) api.Value { return s.With("subvolumeRef", api.StringValue("docs")) })},
		{"NFS export UDP transport", edit(api.CephNFSExport, "nfs", func(s api.Value) api.Value {
			return s.WithPath(api.StringList("UDP"), "native", "transports")
		})},
		{"NFS export generated ID", edit(api.CephNFSExport, "nfs", func(s api.Value) api.Value { return s.WithPath(api.IntegerValue("7"), "native", "export_id") })},
		{"export modes", edit(api.CephConsumer, "export", func(s api.Value) api.Value { return s.With("externalDetails", m("fromSecretRef", "details")) })},
		{"IBM license", entitlement},
		{"external RHSM", object(api.Entitlement, "rhsm", m("type", "redhatCeph", "registry", m("credentialsRef", "registry"), "rhsm", m("management", "external", "organizationRef", "org")))},
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
	cluster := find(t, catalog, api.CephCluster, "storage")
	for _, test := range []struct {
		name, field string
		stretch     api.Value
	}{
		{"undeclared data site", "$.spec.topology.stretch.dataSites[1]", m("dataSites", api.StringList("site-a", "site-z"), "tiebreaker", m("site", "site-b"))},
		{"undeclared tiebreaker site", "$.spec.topology.stretch.tiebreaker.site", m("dataSites", api.StringList("site-a", "site-b"), "tiebreaker", m("site", "site-z"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := cluster.WithSpec(cluster.Spec().WithPath(test.stretch, "topology", "stretch"))
			issues := Validate(changed, replace(catalog, changed))
			requireIssue(t, issues, test.field)
			for _, issue := range issues {
				if issue.Field == test.field && issue.Message != "storage site must resolve to a declared Environment site" {
					t.Fatalf("unexpected site refusal: %+v", issue)
				}
			}
		})
	}
	declared := cluster.WithSpec(cluster.Spec().WithPath(m("dataSites", api.StringList("site-a", "site-b"), "tiebreaker", m("site", "site-b")), "topology", "stretch"))
	for _, issue := range Validate(declared, replace(catalog, declared)) {
		if issue.Message == "storage site must resolve to a declared Environment site" {
			t.Fatalf("declared sites were refused: %+v", issue)
		}
	}
}
