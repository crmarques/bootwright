package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateCephUnusedPubnet(publicCIDRs []string, nodes ...cephMonPubnetNode) []string {
	cluster, machines := cephMonPubnetCase(publicCIDRs, nodes...)
	return validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines)
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

	if errs := validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines); len(errs) != 0 {
		t.Fatalf("a subnet held for a declared arbiter candidate was reported as unserved: %v", errs)
	}

	delete(machines, "arb-standby")
	standby.Spec.Capabilities = []string{v1alpha1.MachineCapabilityCephNode}
	machines["arb-standby"] = standby
	if errs := validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines); len(errs) != 1 {
		t.Fatalf("without the arbiter capability the same subnet must still be reported: %v", errs)
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
	if errs := validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines); len(errs) != 0 {
		t.Fatalf("an unresolvable host must silence the check rather than risk a false report: %v", errs)
	}

	cluster, machines = cephMonPubnetCase([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
	)
	delete(machines, "node-01")
	if errs := validateStorageCephUnusedPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines); len(errs) != 0 {
		t.Fatalf("a missing Machine must silence the check: %v", errs)
	}
}
