package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func cephClusternetCase(ifaceAddrs []v1alpha1.MachineInterfaceAddress, addresses []v1alpha1.MachineAddress, clusterCIDRs, roles []string) (v1alpha1.StorageCluster, map[string]v1alpha1.Machine) {
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "srv"},
		Spec: v1alpha1.MachineSpec{
			Addresses: addresses,
			Network: v1alpha1.MachineNetwork{Config: v1alpha1.MachineNetworkConfig{
				InterfaceAddresses: ifaceAddrs,
			}},
		},
	}
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Networks: v1alpha1.StorageCephNetworks{ClusterCIDRs: clusterCIDRs},
				Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{
					{Hostname: "srv", MachineRef: v1alpha1.LocalObjectReference{Name: "srv"}, Roles: roles},
				}},
			},
		},
	}
	return cluster, map[string]v1alpha1.Machine{"srv": machine}
}

func TestValidateStorageCephClusterNetwork(t *testing.T) {
	ssh := v1alpha1.MachineAddress{Name: "ssh", Address: "10.7.7.1"}
	clusterAddr := v1alpha1.MachineAddress{Name: "cluster", Address: "10.200.255.1"}
	osdRoles := []string{v1alpha1.StorageCephRoleOSD}

	run := func(ifaceAddrs []v1alpha1.MachineInterfaceAddress, addresses []v1alpha1.MachineAddress, cidrs, roles []string) []string {
		cluster, machines := cephClusternetCase(ifaceAddrs, addresses, cidrs, roles)
		return validateStorageCephClusterNetwork("StorageCluster/ceph spec.ceph", cluster, machines)
	}

	ok := []v1alpha1.MachineInterfaceAddress{
		{Interface: "bond0.771", AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}, PrefixLength: 25},
		{Interface: "bond0.798", AddressRef: v1alpha1.LocalObjectReference{Name: "cluster"}, PrefixLength: 24},
	}
	if errs := run(ok, []v1alpha1.MachineAddress{ssh, clusterAddr}, []string{"10.200.255.0/24"}, osdRoles); len(errs) != 0 {
		t.Fatalf("node placed on the cluster network rejected: %v", errs)
	}

	misref := []v1alpha1.MachineInterfaceAddress{
		{Interface: "bond0.771", AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}, PrefixLength: 25},
		{Interface: "bond0.798", AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}, PrefixLength: 24},
	}
	errs := run(misref, []v1alpha1.MachineAddress{ssh, clusterAddr}, []string{"10.200.255.0/24"}, osdRoles)
	if !containsSubstring(errs, "unable to find any IP address in network(s)") {
		t.Fatalf("cluster VLAN bound to the public address not caught: %v", errs)
	}
	if !containsSubstring(errs, "10.200.255.0/24") {
		t.Fatalf("error omits the declared cluster network: %v", errs)
	}

	if errs := run(ok, []v1alpha1.MachineAddress{ssh, clusterAddr}, nil, osdRoles); len(errs) != 0 {
		t.Fatalf("empty clusterCIDRs rejected: %v", errs)
	}

	if errs := run(nil, []v1alpha1.MachineAddress{ssh}, []string{"10.200.255.0/24"}, osdRoles); len(errs) != 0 {
		t.Fatalf("node without interfaceAddresses rejected: %v", errs)
	}

	single := []v1alpha1.MachineInterfaceAddress{
		{Interface: "primary", AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}, PrefixLength: 24},
	}
	if errs := run(single, []v1alpha1.MachineAddress{{Name: "ssh", Address: "192.168.140.21"}}, []string{"192.168.140.0/24"}, osdRoles); len(errs) != 0 {
		t.Fatalf("single-network node rejected: %v", errs)
	}

	if errs := run(misref, []v1alpha1.MachineAddress{ssh, clusterAddr}, []string{"10.200.255.0/24"}, []string{"mon"}); len(errs) != 0 {
		t.Fatalf("non-OSD host off the cluster network rejected: %v", errs)
	}
}
