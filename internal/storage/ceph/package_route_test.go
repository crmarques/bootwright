package ceph

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPackageRepositoryProxyFreezesOnlyTheSelectedRepositoryRoute(t *testing.T) {
	for _, tc := range []struct {
		name, repository, bypass, want string
	}{
		{"proxied", "https://packages.example.test/ceph", "192.0.2.0/24", "http://proxy.example.test:3128"},
		{"domain bypass", "https://packages.example.test/ceph", ".example.test", ""},
		{"wildcard bypass", "https://packages.example.test/ceph", "*.example.test", ""},
		{"all bypass", "https://packages.example.test/ceph", "*", ""},
		{"IP bypass", "https://192.0.2.20/ceph", "192.0.2.0/24", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := packageCatalog()
			cluster, _ := c.Find(api.CephCluster, "example")
			proxy := api.NewObject(api.Proxy, "egress", api.Value{}, requestMap("management", "external", "connection", requestMap("httpsProxy", "http://proxy.example.test:3128/")))
			cluster = cluster.WithSpec(cluster.Spec().WithPath(requestMap("proxyRef", "egress", "noProxy", api.ListValue(api.StringValue(tc.bypass))), "packageRepositoryProxy"))
			c = api.NewCatalog(append(c.Objects(), proxy))
			got, err := packageRepositoryProxy(c, cluster, tc.repository)
			if err != nil || got != tc.want {
				t.Fatalf("proxy = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestPackageRepositoryProxyDoesNotInheritOtherRoutes(t *testing.T) {
	c := packageCatalog()
	cluster, _ := c.Find(api.CephCluster, "example")
	for _, selection := range []api.Value{api.Value{}, requestMap("direct", api.MapValue())} {
		cluster = cluster.WithSpec(cluster.Spec().WithPath(selection, "packageRepositoryProxy"))
		got, err := packageRepositoryProxy(c, cluster, "https://packages.example.test/ceph")
		if err != nil || got != "" {
			t.Fatalf("omitted/direct proxy = %q, %v", got, err)
		}
	}
	cluster = cluster.WithSpec(cluster.Spec().WithPath(requestMap("proxyRef", "missing"), "packageRepositoryProxy"))
	if _, err := packageRepositoryProxy(c, cluster, "https://packages.example.test/ceph"); err == nil {
		t.Fatal("an undeclared proxy became a direct route")
	}
}
