package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func cephPubnetCase(monIP string, prefixLength int, publicCIDRs []string) (v1alpha1.StorageCluster, map[string]v1alpha1.Machine) {
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "srv"},
		Spec: v1alpha1.MachineSpec{
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: monIP}},
			Access:    v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}}},
		},
	}
	if prefixLength > 0 {
		machine.Spec.Network = v1alpha1.MachineNetwork{Config: v1alpha1.MachineNetworkConfig{
			InterfaceAddresses: []v1alpha1.MachineInterfaceAddress{
				{Interface: "bond0.773", AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}, PrefixLength: prefixLength},
			},
		}}
	}
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Cephadm:  v1alpha1.StorageCephadmSpec{Bootstrap: v1alpha1.StorageCephadmBootstrap{Node: "srv"}},
				Networks: v1alpha1.StorageCephNetworks{PublicCIDRs: publicCIDRs},
				Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
					{Name: "srv", MachineRef: v1alpha1.LocalObjectReference{Name: "srv"}},
				}},
			},
		},
	}
	return cluster, map[string]v1alpha1.Machine{"srv": machine}
}

func TestValidateStorageCephBootstrapPublicNetwork(t *testing.T) {
	run := func(monIP string, prefixLength int, publicCIDRs []string) []string {
		cluster, machines := cephPubnetCase(monIP, prefixLength, publicCIDRs)
		return validateStorageCephBootstrapPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines)
	}

	if errs := run("10.7.7.129", 28, []string{"10.7.7.128/28"}); len(errs) != 0 {
		t.Fatalf("consistent /28 subnet rejected: %v", errs)
	}

	if errs := run("10.7.7.1", 25, []string{"10.7.7.0/25"}); len(errs) != 0 {
		t.Fatalf("exact /25 subnet rejected: %v", errs)
	}

	errs := run("10.7.7.1", 24, []string{"10.7.7.0/25"})
	if !containsSubstring(errs, "not configured locally") {
		t.Fatalf("prefix/CIDR mismatch not caught: %v", errs)
	}
	if !containsSubstring(errs, "10.7.7.0/24") {
		t.Fatalf("error omits the interface subnet: %v", errs)
	}

	if errs := run("10.7.7.1", 0, nil); len(errs) != 0 {
		t.Fatalf("empty publicCIDRs rejected: %v", errs)
	}

	if errs := run("10.7.7.1", 0, []string{"10.7.7.0/25"}); len(errs) != 0 {
		t.Fatalf("mon-ip within publicCIDRs (no interface prefix) rejected: %v", errs)
	}

	if errs := run("10.9.9.9", 0, []string{"10.7.7.0/25"}); !containsSubstring(errs, "falls outside every entry") {
		t.Fatalf("mon-ip outside publicCIDRs not caught: %v", errs)
	}
}
