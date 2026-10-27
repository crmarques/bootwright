package cephcluster

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func bindingCatalog() api.Catalog {
	catalog := fixtureCatalog()
	for name, kind := range map[string]string{"tls-a": "tlsCertificate", "tls-b": "tlsCertificate", "opaque-a": "opaque"} {
		catalog = replace(catalog, object(api.Secret, name, m("type", kind)))
	}
	return catalog
}

func withBoundService(t *testing.T, service api.Value, bindings ...api.Value) (api.Object, api.Catalog) {
	t.Helper()
	cluster, catalog := withMDSService(t, bindingCatalog(), service)
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(bindings...), "serviceBindings"))
	return changed, replace(catalog, changed)
}

func leaves(ref string, pairs ...string) api.Value {
	secrets := []api.Value{}
	for i := 0; i < len(pairs); i += 2 {
		secrets = append(secrets, m("path", pairs[i], "secretRef", pairs[i+1]))
	}
	return m("serviceRef", ref, "secrets", api.ListValue(secrets...))
}

func oneIssueAt(t *testing.T, issues []api.Issue, field, message string) {
	t.Helper()
	found := issuesAt(issues, field)
	if len(found) != 1 || found[0].Code != "api.invariant" || found[0].Message != message {
		t.Fatalf("issues at %s: %#v (all %#v)", field, found, issues)
	}
}

func TestRGWTLSRequiresBothBindingsOfOneSecret(t *testing.T) {
	const binding = "$.spec.serviceBindings[0]"
	tls := rgwService(qualifiedRGWSpec().With("ssl", api.BoolValue(true)))
	cluster, catalog := withBoundService(t, tls, leaves("rgw.objects", "/spec/ssl_cert", "tls-a", "/spec/ssl_key", "tls-a"))
	issues := Validate(cluster, catalog)
	if found := append(issuesUnder(issues, "$.spec.services[3]"), issuesUnder(issues, "$.spec.serviceBindings")...); len(found) != 0 {
		t.Fatalf("one tlsCertificate Secret bound to both leaves refused: %#v", found)
	}
	const missing = "TLS needs the /spec/ssl_cert and /spec/ssl_key bindings of one tlsCertificate Secret"
	cluster, catalog = withBoundService(t, tls)
	oneIssueAt(t, Validate(cluster, catalog), "$.spec.services[3].spec.ssl", missing)
	cluster, catalog = withBoundService(t, tls, leaves("rgw.objects", "/spec/ssl_cert", "tls-a"))
	issues = Validate(cluster, catalog)
	oneIssueAt(t, issues, "$.spec.services[3].spec.ssl", missing)
	oneIssueAt(t, issues, binding+".secrets[0].path", "ssl_cert and ssl_key are bound together")
	cluster, catalog = withBoundService(t, tls, leaves("rgw.objects", "/spec/ssl_cert", "tls-a", "/spec/ssl_key", "tls-b"))
	issues = Validate(cluster, catalog)
	oneIssueAt(t, issues, binding+".secrets[1].secretRef", "ssl_cert and ssl_key must bind the certificate and private key of one tlsCertificate Secret")
	if found := issuesAt(issues, binding+".secrets[0].secretRef"); len(found) != 0 {
		t.Fatalf("split pair refused at the certificate leaf: %#v", found)
	}
	cluster, catalog = withBoundService(t, rgwService(qualifiedRGWSpec()), leaves("rgw.objects", "/spec/ssl_cert", "tls-a", "/spec/ssl_key", "tls-a"))
	issues = Validate(cluster, catalog)
	for _, leaf := range []string{".secrets[0].path", ".secrets[1].path"} {
		oneIssueAt(t, issues, binding+leaf, "this TLS binding takes effect only when its service sets ssl: true")
	}
	cluster, catalog = withBoundService(t, rgwService(qualifiedRGWSpec().With("certificate_source", api.StringValue("inline"))))
	oneIssueAt(t, Validate(cluster, catalog), "$.spec.services[3].spec.certificate_source", "cephadm ignores certificate_source unless ssl is true")
}

func TestBindingRegistryRefusesUnqualifiedLeaf(t *testing.T) {
	const unqualified = "secret leaf is not qualified for this service schema"
	const leaf = "$.spec.serviceBindings[0].secrets[0]"
	tls := rgwService(qualifiedRGWSpec().With("ssl", api.BoolValue(true)))
	cluster, catalog := withBoundService(t, tls, leaves("rgw.objects", "/spec/rgw_frontend_port", "tls-a"))
	oneIssueAt(t, Validate(cluster, catalog), leaf+".path", unqualified)
	cluster, catalog = withBoundService(t, tls, leaves("rgw.objects", "/spec/ssl_cert", "opaque-a", "/spec/ssl_key", "tls-a"))
	issues := Validate(cluster, catalog)
	oneIssueAt(t, issues, leaf+".secretRef", "this binding leaf accepts only tlsCertificate Secrets")
	if found := issuesAt(issues, leaf+".secretRef"); found[0].Remediation != "bind a tlsCertificate Secret at this path" {
		t.Fatalf("secret type remedy: %q", found[0].Remediation)
	}
	ingress := m("service_type", "ingress", "service_id", "rgw.objects", "placement", m("label", "storage"), "spec", m("backend_service", "rgw.objects", "virtual_ip", "192.0.2.50/24"))
	cluster, catalog = withBoundService(t, ingress, leaves("ingress.rgw.objects", "/spec/monitor_password", "opaque-a"))
	oneIssueAt(t, Validate(cluster, catalog), leaf+".path", unqualified)
	cluster, catalog = withBoundService(t, mdsService("files"), leaves("mgr", "/spec/key", "opaque-a"))
	oneIssueAt(t, Validate(cluster, catalog), leaf+".path", unqualified)
}
