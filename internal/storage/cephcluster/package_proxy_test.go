package cephcluster

import (
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
		for _, field := range []string{"packageRepositoryProxy", "registryProxy"} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				connection := m("httpsProxy", tc.endpoint)
				for _, addition := range tc.addition.Fields() {
					connection = connection.With(addition.Name, addition.Value)
				}
				proxy := object(api.Proxy, "egress", m("management", "external", "connection", connection))
				authored := cluster.WithSpec(cluster.Spec().WithPath(m("proxyRef", "egress"), field))
				catalog := api.NewCatalog(append(replace(c, authored).Objects(), proxy))
				issues := issuesAt(Validate(authored, catalog), "$.spec."+field)
				if (len(issues) != 0) != tc.refused {
					t.Fatalf("issues = %+v", issues)
				}
			})
		}
	}
}
