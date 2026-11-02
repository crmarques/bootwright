package ceph

import (
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

func requirementsOf(t *testing.T, catalog api.Catalog) []HostPortRequirement {
	t.Helper()
	got, err := HostPortRequirements(catalog)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return got
}

func onHosts(publisher string, networks []string, ports []HostPort, machines ...string) []HostPortRequirement {
	out := []HostPortRequirement{}
	for _, machine := range machines {
		out = append(out, HostPortRequirement{Machine: machine, Publisher: publisher, Networks: networks, Protocols: []string{"vrrp"}, Ports: ports})
	}
	return out
}

func keepaliveNFS() []api.Value {
	return []api.Value{
		requestMap("service_type", "nfs", "service_id", "files", "placement", requestMap("label", "mon", "count", 2), "spec", requestMap("port", 2050)),
		requestMap("service_type", "ingress", "service_id", "nfs.files", "placement", requestMap("label", "mon", "count", 2),
			"spec", requestMap("backend_service", "nfs.files", "virtual_ip", "192.0.2.50/24", "keepalive_only", true, "virtual_interface_networks", api.StringList("198.51.100.0/24"))),
	}
}

func TestNFSIngressPublishesVRRPAndItsFrontendPortOnEveryIngressHost(t *testing.T) {
	got := requirementsOf(t, delegatedCatalog(haproxyProtocolNFS(), nil))
	want := onHosts("ceph-service-example-nfs-files", []string{"192.0.2.50/32"}, []HostPort{{Number: 2049, Transport: "tcp"}}, "node-1", "node-2", "node-3")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the NFS ingress publishes %+v", got)
	}
}

func TestKeepaliveOnlyNFSPublishesTheNFSPortAndItsInterfaceNetworks(t *testing.T) {
	got := requirementsOf(t, delegatedCatalog(keepaliveNFS(), nil))
	want := onHosts("ceph-service-example-nfs-files", []string{"192.0.2.50/32", "198.51.100.0/24"}, []HostPort{{Number: 2050, Transport: "tcp"}}, "node-1", "node-2", "node-3")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the keepalive-only NFS ingress publishes %+v", got)
	}
}

func TestManagementGatewayPublishesItsClientAndInternalPorts(t *testing.T) {
	got := requirementsOf(t, delegatedCatalog(vipGateway("192.0.2.60"), nil))
	if six := requirementsOf(t, delegatedCatalog(vipGateway("2001:DB8::60"), nil)); len(six) == 0 || !reflect.DeepEqual(six[0].Networks, []string{"2001:db8::60/128"}) {
		t.Fatalf("an IPv6 management gateway publishes %+v", six)
	}
	ports := []HostPort{{Number: 9443, Transport: "tcp"}, {Number: cephcluster.MgmtGatewayInternalPort, Transport: "tcp"}}
	want := onHosts("ceph-service-example-mgmt-gateway", []string{"192.0.2.60/32"}, ports, "node-1", "node-2", "node-3")
	if !reflect.DeepEqual(got, want) || cephcluster.MgmtGatewayInternalPort != 29443 {
		t.Fatalf("the management gateway publishes %+v", got)
	}
	unit := gatewayUnitOf(t, delegatedCatalog([]api.Value{mgmtGateway(requestMap("port", 9443))}, nil))
	if unit.IngressPorts != nil {
		t.Fatalf("a gateway without an ingress carries ingress ports %v", unit.IngressPorts)
	}
}

func TestStretchedGatewayPublishesEachSiteIngressOnItsOwnHosts(t *testing.T) {
	got := requirementsOf(t, stretchGatewayCatalog(t, true))
	want := append(onHosts(GatewayBlockID("objects"), []string{"192.0.2.50/32"}, []HostPort{{Number: 443, Transport: "tcp"}}, "node-1", "node-2"),
		onHosts(GatewayBlockID("objects"), []string{"192.0.2.51/32"}, []HostPort{{Number: 443, Transport: "tcp"}}, "node-3", "node-4")...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the stretched gateway publishes %+v", got)
	}
	if got := requirementsOf(t, stretchGatewayCatalog(t, false)); len(got) != 0 {
		t.Fatalf("a direct stretched gateway publishes %+v", got)
	}
}

func TestClusterWithoutIngressPublishesNothing(t *testing.T) {
	if got := requirementsOf(t, delegatedCatalog([]api.Value{nfsService("direct")}, nil)); got == nil || len(got) != 0 {
		t.Fatalf("a cluster without an ingress publishes %#v", got)
	}
	if got := requirementsOf(t, api.NewCatalog(nil)); got == nil || len(got) != 0 {
		t.Fatalf("an empty catalog publishes %#v", got)
	}
}

func TestRequirementsNameTheMachineOfEachIngressHost(t *testing.T) {
	catalog := delegatedCatalog(haproxyProtocolNFS(), nil)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	requests, err := (Capability{kind: api.CephCluster}).objectRequests(catalog, cluster, "")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	for _, r := range requests {
		if r.Service == nil || r.Service.Type != "nfs" {
			continue
		}
		for i := range r.Hosts {
			r.Hosts[i].Machine = "machine-" + r.Hosts[i].Name
		}
		r.Hosts = r.Hosts[1:]
		got := mergeRequirements(ingressRequirements(r, "publisher"))
		want := onHosts("publisher", []string{"192.0.2.50/32"}, []HostPort{{Number: 2049, Transport: "tcp"}}, "machine-node-2", "machine-node-3")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("the requirements name %+v", got)
		}
		return
	}
	t.Fatal("the cluster planned no NFS unit")
}

func TestMergedRequirementsAreSortedWithoutDuplicates(t *testing.T) {
	ports := []HostPort{{Number: 443, Transport: "tcp"}, {Number: 53, Transport: "udp"}, {Number: 80, Transport: "tcp"}}
	got := mergeRequirements([]HostPortRequirement{
		{Machine: "a", Publisher: "p", Networks: []string{"192.0.2.0/24"}, Protocols: []string{"vrrp"}, Ports: ports[:2]},
		{Machine: "a", Publisher: "p", Networks: []string{"192.0.2.0/24", "198.51.100.0/24"}, Protocols: []string{"vrrp"}, Ports: ports},
	})
	want := []HostPortRequirement{{Machine: "a", Publisher: "p", Networks: []string{"192.0.2.0/24", "198.51.100.0/24"}, Protocols: []string{"vrrp"},
		Ports: []HostPort{{Number: 80, Transport: "tcp"}, {Number: 443, Transport: "tcp"}, {Number: 53, Transport: "udp"}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged %+v", got)
	}
}
