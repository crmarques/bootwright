package openshift

import (
	"reflect"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestInspectionShowsNativeEndpointsAndNodeTopology(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		cluster, catalog := fixture("libvirt", 5, ipv6)
		nodes := cluster.Spec().Get("nodes").Items()
		nodes[4] = nodes[4].With("role", api.StringValue("infra")).With("fqdn", api.StringValue("infra.example.test"))
		slices.Reverse(nodes)
		cluster = cluster.WithSpec(cluster.Spec().With("nodes", list(nodes...)).With("security", m("fips", m("enabled", true))))
		cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("misleading.example.test"), "install", "endpoints", "api", "dnsName"))
		cluster = cluster.WithSpec(cluster.Spec().With("networking", m("networkType", "OVNKubernetes")))
		effective, issues := Normalize(cluster, catalog)
		if len(issues) != 0 {
			t.Fatal(issues)
		}
		got := Inspect(catalog, effective)
		apiAddress, ingressAddress, nodeAddress, machineNetwork := "192.0.2.2", "192.0.2.3", "192.0.2.14", "192.0.2.0/24"
		if ipv6 {
			apiAddress, ingressAddress, nodeAddress, machineNetwork = "2001:db8::2", "2001:db8::3", "2001:db8::14", "2001:db8::/64"
		}
		wantEndpoints := []InspectionEndpoint{
			{Service: "api", URL: "https://api.cluster.clusters.example.test:6443", Address: apiAddress, Source: "external"},
			{Service: "api-int", URL: "https://api-int.cluster.clusters.example.test:6443", Address: apiAddress, Source: "external"},
			{Service: "console", URL: "https://console-openshift-console.apps.cluster.clusters.example.test", Address: ingressAddress, Source: "external"},
		}
		if !reflect.DeepEqual(got.Endpoints, wantEndpoints) {
			t.Fatalf("IPv6=%v endpoints = %+v", ipv6, got.Endpoints)
		}
		if got.Distribution != "openshift" || got.Release != "4.21.15" || got.Platform != "none" || got.InstallMode != "connected" || !got.FIPS ||
			got.BaseDomain != "clusters.example.test" || got.IngressDomain != "apps.cluster.clusters.example.test" || got.NetworkType != "OVNKubernetes" {
			t.Fatalf("settings = %+v", got)
		}
		if len(got.Nodes) != 5 || got.Nodes[0].Name != "node-0" || got.Nodes[0].Role != "controlPlane" || got.Nodes[3].Role != "worker" ||
			got.Nodes[4] != (InspectionNode{Name: "node-4", Role: "infra", Machine: "node-4", FQDN: "infra.example.test", Address: nodeAddress}) {
			t.Fatalf("nodes = %+v", got.Nodes)
		}
		if !slices.Equal(got.MachineNetworks, []string{machineNetwork}) || len(got.ClusterNetworks) == 0 || len(got.ServiceNetworks) == 0 {
			t.Fatalf("networks = %+v", got)
		}
		nodes = effective.Spec().Get("nodes").Items()
		slices.Reverse(nodes)
		effective = effective.WithSpec(effective.Spec().With("nodes", list(nodes...)))
		if reordered := Inspect(catalog, effective); !reflect.DeepEqual(reordered, got) {
			t.Fatal("inspection changed with node declaration order")
		}
	}
}

func TestInspectionSingleNodeEndpointsUseTheInstallAddress(t *testing.T) {
	cluster, catalog := fixture("libvirt", 1, false)
	effective, issues := Normalize(cluster, catalog)
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	got := Inspect(catalog, effective)
	if len(got.Endpoints) != 3 || len(got.Nodes) != 1 {
		t.Fatalf("inspection = %+v", got)
	}
	for _, endpoint := range got.Endpoints {
		if endpoint.Address != got.Nodes[0].Address || endpoint.Source != "node" {
			t.Fatalf("endpoint = %+v, node = %+v", endpoint, got.Nodes[0])
		}
	}
}
