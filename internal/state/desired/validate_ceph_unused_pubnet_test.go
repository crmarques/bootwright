package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateCephUnusedPubnet(publicCIDRs []string, nodes ...cephMonPubnetNode) []string {
	cluster, machines := cephMonPubnetCase(publicCIDRs, nodes...)
	return validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines, v1alpha1.State{})
}

func cephNeighborCluster(publicCIDRs ...string) v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph-neighbor"},
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Networks: v1alpha1.StorageCephNetworks{PublicCIDRs: publicCIDRs},
			},
		},
	}
}

func TestValidateStorageCephUnusedPublicNetworkAdmitsServedEntries(t *testing.T) {
	mon := []string{v1alpha1.StorageCephRoleMON}

	if errs := validateCephUnusedPubnet([]string{"10.7.7.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
	); len(errs) != 0 {
		t.Fatalf("a single served entry rejected: %v", errs)
	}

	if errs := validateCephUnusedPubnet([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
		cephMonPubnetNode{name: "node-07", address: "10.22.254.5", prefixLength: 25, roles: mon},
	); len(errs) != 0 {
		t.Fatalf("both entries are stood on, yet one was reported: %v", errs)
	}

	if errs := validateCephUnusedPubnet([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", roles: mon},
		cephMonPubnetNode{name: "node-07", address: "10.22.254.5", roles: mon},
	); len(errs) != 0 {
		t.Fatalf("provided machines standing on both entries still reported: %v", errs)
	}
}

func TestValidateStorageCephUnusedPublicNetworkCatchesADeadEntry(t *testing.T) {
	mon := []string{v1alpha1.StorageCephRoleMON}

	errs := validateCephUnusedPubnet([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
		cephMonPubnetNode{name: "node-02", address: "10.7.7.2", prefixLength: 25, roles: mon},
	)
	if len(errs) != 1 {
		t.Fatalf("expected exactly the unserved entry to be reported: %v", errs)
	}
	if !containsSubstring(errs, "10.22.254.0/25") {
		t.Fatalf("error omits the unserved entry: %v", errs)
	}
	if containsSubstring(errs, "10.7.7.0/25") {
		t.Fatalf("error wrongly names the served entry: %v", errs)
	}
}

func TestValidateStorageCephUnusedPublicNetworkAdmitsAStandbyArbiterSubnet(t *testing.T) {
	mon := []string{v1alpha1.StorageCephRoleMON}

	cluster, machines := cephMonPubnetCase([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
		cephMonPubnetNode{name: "node-07", address: "10.7.7.5", prefixLength: 25, roles: mon},
	)
	standby := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "arb-standby"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityCephNode, v1alpha1.MachineCapabilityCephArbiter},
			Addresses:    []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.22.254.5"}},
			Access:       v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}}},
		},
	}
	machines["arb-standby"] = standby

	if errs := validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines, v1alpha1.State{}); len(errs) != 0 {
		t.Fatalf("a subnet held for a declared arbiter candidate was reported as unserved: %v", errs)
	}

	delete(machines, "arb-standby")
	standby.Spec.Capabilities = []string{v1alpha1.MachineCapabilityCephNode}
	machines["arb-standby"] = standby
	if errs := validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines, v1alpha1.State{}); len(errs) != 1 {
		t.Fatalf("without the arbiter capability the same subnet must still be reported: %v", errs)
	}
}

func TestValidateStorageCephUnusedPublicNetworkIgnoresAnotherClustersArbiterCandidate(t *testing.T) {
	mon := []string{v1alpha1.StorageCephRoleMON}

	cluster, machines := cephMonPubnetCase([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
		cephMonPubnetNode{name: "node-02", address: "10.7.7.2", prefixLength: 25, roles: mon},
	)
	machines["arb-elsewhere"] = v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "arb-elsewhere"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityCephNode, v1alpha1.MachineCapabilityCephArbiter},
			Addresses:    []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.22.254.5"}},
			Access:       v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}}},
		},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster, cephNeighborCluster("10.9.9.0/25", "10.22.254.0/25")}}

	errs := validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines, state)
	if len(errs) != 1 {
		t.Fatalf("a candidate standing on a subnet another cluster also declares still held this cluster's entry: %v", errs)
	}
	if !containsSubstring(errs, "10.22.254.0/25") {
		t.Fatalf("error omits the unserved entry: %v", errs)
	}
}

func TestValidateStorageCephUnusedPublicNetworkAdmitsAnAdjacentArbiterCandidate(t *testing.T) {
	mon := []string{v1alpha1.StorageCephRoleMON}

	cluster, machines := cephMonPubnetCase([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
		cephMonPubnetNode{name: "node-02", address: "10.7.7.2", prefixLength: 25, roles: mon},
	)
	machines["arb-adjacent"] = v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "arb-adjacent"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityCephNode, v1alpha1.MachineCapabilityCephArbiter},
			Addresses: []v1alpha1.MachineAddress{
				{Name: "ssh", Address: "10.7.7.9"},
				{Name: "arbiter", Address: "10.22.254.5"},
			},
			Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}}},
			Network: v1alpha1.MachineNetwork{Config: v1alpha1.MachineNetworkConfig{
				InterfaceAddresses: []v1alpha1.MachineInterfaceAddress{
					{Interface: "bond0.773", AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}, PrefixLength: 25},
					{Interface: "bond0.254", AddressRef: v1alpha1.LocalObjectReference{Name: "arbiter"}, PrefixLength: 25},
				},
			}},
		},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster, cephNeighborCluster("10.9.9.0/25", "10.22.254.0/25")}}

	if errs := validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines, state); len(errs) != 0 {
		t.Fatalf("a candidate sharing a network with this cluster's declared nodes was denied its entry: %v", errs)
	}
}

func TestValidateStorageCephUnusedPublicNetworkStaysSilentWhenUnprovable(t *testing.T) {
	mon := []string{v1alpha1.StorageCephRoleMON}

	if errs := validateCephUnusedPubnet([]string{"10.7.7.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
	); len(errs) != 0 {
		t.Fatalf("a lone entry must never be reported, it is the network the cluster serves: %v", errs)
	}

	cluster, machines := cephMonPubnetCase([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
		cephMonPubnetNode{name: "node-07", address: "", roles: mon},
	)
	if errs := validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines, v1alpha1.State{}); len(errs) != 0 {
		t.Fatalf("an unresolvable host must silence the check rather than risk a false report: %v", errs)
	}

	cluster, machines = cephMonPubnetCase([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
	)
	delete(machines, "node-01")
	if errs := validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines, v1alpha1.State{}); len(errs) != 0 {
		t.Fatalf("a missing Machine must silence the check: %v", errs)
	}
}
