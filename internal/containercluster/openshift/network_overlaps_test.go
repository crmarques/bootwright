package openshift

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func withNetwork(c api.Catalog, name, cidr string) api.Catalog {
	return api.NewCatalog(append(c.Objects(), obj(api.Network, name, m("provided", true, "ipv4", m("cidr", cidr)))))
}

func withNodeAddress(c api.Catalog, name string, assignments ...api.Value) api.Catalog {
	objects := c.Objects()
	for i, object := range objects {
		if object.Kind() == api.Machine && object.Name() == name {
			objects[i] = object.WithSpec(object.Spec().WithPath(list(assignments...), "network", "addresses"))
		}
	}
	return api.NewCatalog(objects)
}

func TestClusterAndServiceNetworksOverlapNoNodeOrSecondarySubnet(t *testing.T) {
	o, c := fixture("libvirt", 3, false)
	o, _ = Normalize(o, c)
	if issues := validateOverlayOverlaps(o, c); len(issues) != 0 {
		t.Fatal("default overlays refused", issues)
	}
	c = withNetwork(c, "node-storage", "10.130.4.0/24")
	c = withNodeAddress(c, "node-1", m("name", "primary", "address", "192.0.2.11", "interface", "eth0"), m("name", "storage", "address", "10.130.4.11", "interface", "eth1"))
	secondary := list(m("name", "tenant", "ipam", m("mode", "Enabled", "subnets", api.StringList("172.30.8.0/24"))))
	o = o.WithSpec(o.Spec().WithPath(secondary, "networking", "secondaryNetworks"))
	issues := Validate(o, c)
	if !hasIssue(issues, "$.spec.networking.clusterNetwork[0].cidr", "the cluster network 10.128.0.0/14 of OpenShiftCluster/cluster overlaps 10.130.4.0/24, the subnet of interface eth1 on Machine/node-1") {
		t.Fatal("a cluster network over a node subnet admitted", issues)
	}
	if !hasIssue(issues, "$.spec.networking.serviceNetwork[0]", "the service network 172.30.0.0/16 of OpenShiftCluster/cluster overlaps 172.30.8.0/24, the IPAM subnet of secondary network tenant") {
		t.Fatal("a service network over a secondary IPAM subnet admitted", issues)
	}
}

func TestEveryNodeThatCanHoldAnInstallerVIPIsOnItsSubnet(t *testing.T) {
	installerOwned := func(spec api.Value) api.Value {
		for _, slot := range []string{"api", "ingress"} {
			spec = spec.WithPath(api.StringValue("installer"), "install", "endpoints", slot, "source", "type")
		}
		return spec
	}
	o, c := roster("libvirt", []string{"controlPlane", "controlPlane", "controlPlane", "worker"}, installerOwned)
	if issues := validateVIPAdjacency(o, c); len(issues) != 0 {
		t.Fatal("nodes on the VIP subnet refused", issues)
	}
	c = withNetwork(c, "off-vip", "198.51.100.0/24")
	worker := withNodeAddress(c, "node-3", m("name", "primary", "address", "198.51.100.13", "interface", "eth0"))
	issues := Validate(o, worker)
	if !hasIssue(issues, "$.spec.install.endpoints.ingress.address", "the ingress VIP 192.0.2.3 lies in no interface subnet of node node-3 (Machine/node-3)") {
		t.Fatal("an ingress holder off the VIP subnet admitted", issues)
	}
	if hasField(issues, "$.spec.install.endpoints.api.address") {
		t.Fatal("a worker refused for the API VIP it never holds", issues)
	}
	master := withNodeAddress(c, "node-0", m("name", "primary", "address", "198.51.100.10", "interface", "eth0"))
	if !hasIssue(Validate(o, master), "$.spec.install.endpoints.api.address", "the api VIP 192.0.2.2 lies in no interface subnet of node node-0 (Machine/node-0)") {
		t.Fatal("an API holder off the VIP subnet admitted")
	}
}

func TestAnEndpointEqualToACephOrSMBClaimRefuses(t *testing.T) {
	o, c := fixture("libvirt", 3, false)
	o, _ = Normalize(o, c)
	for name, holder := range map[string]api.Object{
		"CephCluster/storage": obj(api.CephCluster, "storage", m("services", list(m("service_type", "ingress", "spec", m("virtual_ip", "192.0.2.2/24"))))),
		"CephSMBResource/smb": obj(api.CephSMBResource, "smb", m("native", m("public_addrs", list(m("address", "192.0.2.2/24"))))),
	} {
		t.Run(name, func(t *testing.T) {
			catalog := api.NewCatalog(append(c.Objects(), holder))
			if !hasIssue(Validate(o, catalog), "$.spec.install.endpoints.api.address", "endpoint address 192.0.2.2 is already held by "+name) {
				t.Fatal("an endpoint equal to another claim admitted")
			}
		})
	}
}

func TestClusterAndServiceNetworksOverlapNoDeclaredNetwork(t *testing.T) {
	o, c := fixture("libvirt", 3, false)
	o, _ = Normalize(o, c)
	storage := api.NewObject(api.Network, "storage", api.MapValue(), m("provided", true, "ipv4", m("cidr", "10.130.0.0/16")))
	issues := validateOverlayOverlaps(o, api.NewCatalog(append(c.Objects(), storage)))
	if !hasIssue(issues, "$.spec.networking.clusterNetwork[0].cidr", "the cluster network 10.128.0.0/14 of OpenShiftCluster/cluster overlaps 10.130.0.0/16, the cidr of Network/storage") {
		t.Fatal("a cluster network over a declared Network admitted", issues)
	}
}
