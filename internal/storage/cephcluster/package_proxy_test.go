package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPackageProxyAdmitsOnlyImplementedConnections(t *testing.T) {
	cluster, c := vendorCluster(t)
	for _, tc := range []struct {
		name, endpoint string
		addition       api.Value
		refused        bool
	}{
		{"HTTP CONNECT", "http://proxy.example.test:3128", api.Value{}, false},
		{"root path", "http://proxy.example.test:3128/", api.Value{}, false},
		{"zero port", "http://proxy.example.test:0", api.Value{}, true},
		{"large port", "http://proxy.example.test:65536", api.Value{}, true},
		{"outer HTTPS", "https://proxy.example.test:8443", api.Value{}, true},
		{"credentials in URL", "http://user:password@proxy.example.test:3128", api.Value{}, true},
		{"auth", "http://proxy.example.test:3128", m("auth", m("proxyAuthRef", "credentials")), true},
		{"proxy CA", "http://proxy.example.test:3128", m("trustBundleRef", "ca"), true},
		{"relaxed TLS", "http://proxy.example.test:3128", m("tlsVerification", "none"), true},
	} {
		for _, field := range []string{"packageRepositoryProxy", "registryProxy", "distribution.ibm.callHomeProxy"} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				connection := m("httpsProxy", tc.endpoint)
				for _, addition := range tc.addition.Fields() {
					connection = connection.With(addition.Name, addition.Value)
				}
				proxy := object(api.Proxy, "egress", m("management", "external", "connection", connection))
				spec := cluster.Spec().WithPath(api.StringValue("enabled"), "distribution", "ibm", "callHome")
				authored := cluster.WithSpec(spec.WithPath(m("proxyRef", "egress"), strings.Split(field, ".")...))
				catalog := api.NewCatalog(append(replace(c, authored).Objects(), proxy))
				issues := issuesAt(Validate(authored, catalog), "$.spec."+field)
				if (len(issues) != 0) != tc.refused {
					t.Fatalf("issues = %+v", issues)
				}
			})
		}
	}
}

func TestCallHomeProxyIsRefusedUnlessCallHomeIsEnabled(t *testing.T) {
	cluster, c := vendorCluster(t)
	proxy := object(api.Proxy, "egress", m("management", "external", "connection", m("httpsProxy", "http://proxy.example.test:3128")))
	for _, tc := range []struct {
		callHome  string
		selection api.Value
		refused   bool
	}{
		{"enabled", m("proxyRef", "egress", "noProxy", []api.Value{api.StringValue(".example.test")}), false},
		{"enabled", m("direct", m()), false},
		{"disabled", m("proxyRef", "egress"), true},
		{"disabled", m("direct", m()), true},
	} {
		spec := cluster.Spec().WithPath(api.StringValue(tc.callHome), "distribution", "ibm", "callHome")
		authored := cluster.WithSpec(spec.WithPath(tc.selection, "distribution", "ibm", "callHomeProxy"))
		catalog := api.NewCatalog(append(replace(c, authored).Objects(), proxy))
		issues := issuesAt(Validate(authored, catalog), "$.spec.distribution.ibm.callHomeProxy")
		if (len(issues) != 0) != tc.refused || tc.refused && (issues[0].Code != "api.invariant" || !strings.Contains(issues[0].Remediation, "callHome to enabled")) {
			t.Fatalf("%s %v: issues = %+v", tc.callHome, tc.selection, issues)
		}
	}
}
