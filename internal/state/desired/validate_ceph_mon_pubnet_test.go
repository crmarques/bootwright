package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type cephMonPubnetNode struct {
	name         string
	address      string
	prefixLength int
	roles        []string
}

func cephMonPubnetCase(publicCIDRs []string, nodes ...cephMonPubnetNode) (v1alpha1.StorageCluster, map[string]v1alpha1.Machine) {
	machines := map[string]v1alpha1.Machine{}
	var topologyNodes []v1alpha1.StorageCephNode
	for _, node := range nodes {
		machine := v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: node.name},
			Spec: v1alpha1.MachineSpec{
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: node.address}},
				Access:    v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}}},
			},
		}
		if node.prefixLength > 0 {
			machine.Spec.Network = v1alpha1.MachineNetwork{Config: v1alpha1.MachineNetworkConfig{
				InterfaceAddresses: []v1alpha1.MachineInterfaceAddress{
					{Interface: "bond0.773", AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}, PrefixLength: node.prefixLength},
				},
			}}
		}
		machines[node.name] = machine
		topologyNodes = append(topologyNodes, v1alpha1.StorageCephNode{
			Name:       node.name,
			MachineRef: v1alpha1.LocalObjectReference{Name: node.name},
			Roles:      node.roles,
		})
	}
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Networks: v1alpha1.StorageCephNetworks{PublicCIDRs: publicCIDRs},
				Topology: v1alpha1.StorageCephTopology{Nodes: topologyNodes},
			},
		},
	}
	return cluster, machines
}

func validateCephMonPubnet(publicCIDRs []string, nodes ...cephMonPubnetNode) []string {
	cluster, machines := cephMonPubnetCase(publicCIDRs, nodes...)
	return validateStorageCephMonPublicNetwork("StorageCluster/ceph spec.ceph", cluster, machines)
}

func TestValidateStorageCephMonPublicNetworkAdmitsMatchingHosts(t *testing.T) {
	mon := []string{v1alpha1.StorageCephRoleMON}

	if errs := validateCephMonPubnet([]string{"10.7.7.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
	); len(errs) != 0 {
		t.Fatalf("mon on the declared public network rejected: %v", errs)
	}

	if errs := validateCephMonPubnet([]string{"10.7.7.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 24, roles: mon},
	); len(errs) != 0 {
		t.Fatalf("mon whose wider interface subnet overlaps the declared entry rejected: %v", errs)
	}

	if errs := validateCephMonPubnet([]string{"10.7.7.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", roles: mon},
	); len(errs) != 0 {
		t.Fatalf("mon address inside the declared entry rejected without an interface prefix: %v", errs)
	}

	if errs := validateCephMonPubnet(nil,
		cephMonPubnetNode{name: "node-07", address: "10.22.254.5", prefixLength: 25, roles: mon},
	); len(errs) != 0 {
		t.Fatalf("undeclared publicCIDRs rejected: %v", errs)
	}

	if errs := validateCephMonPubnet([]string{"10.7.7.0/25"},
		cephMonPubnetNode{name: "node-03", address: "10.22.254.5", prefixLength: 25, roles: []string{v1alpha1.StorageCephRoleOSD}},
	); len(errs) != 0 {
		t.Fatalf("non-mon host off the public network rejected: %v", errs)
	}
}

func TestValidateStorageCephMonPublicNetworkCatchesFilteredArbiter(t *testing.T) {
	mon := []string{v1alpha1.StorageCephRoleMON}

	errs := validateCephMonPubnet([]string{"10.7.7.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
		cephMonPubnetNode{name: "node-07", address: "10.22.254.5", prefixLength: 25, roles: mon},
	)
	if len(errs) != 1 {
		t.Fatalf("expected exactly the arbiter to be reported: %v", errs)
	}
	if !containsSubstring(errs, "node-07") {
		t.Fatalf("error omits the filtered mon host: %v", errs)
	}
	if !containsSubstring(errs, "10.22.254.0/25") {
		t.Fatalf("error omits the host subnet: %v", errs)
	}
	if !containsSubstring(errs, "does not belong to mon public_network(s)") {
		t.Fatalf("error omits the cephadm log line the operator will grep for: %v", errs)
	}

	if errs := validateCephMonPubnet([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-01", address: "10.7.7.1", prefixLength: 25, roles: mon},
		cephMonPubnetNode{name: "node-07", address: "10.22.254.5", prefixLength: 25, roles: mon},
	); len(errs) != 0 {
		t.Fatalf("arbiter subnet declared alongside the data-site network still rejected: %v", errs)
	}
}

func TestValidateStorageCephMonPublicNetworkCatchesProvidedArbiter(t *testing.T) {
	mon := []string{v1alpha1.StorageCephRoleMON}

	errs := validateCephMonPubnet([]string{"10.7.7.0/25"},
		cephMonPubnetNode{name: "node-07", address: "10.22.254.5", roles: mon},
	)
	if !containsSubstring(errs, "answers at 10.22.254.5") {
		t.Fatalf("arbiter without declared interface addresses not caught: %v", errs)
	}

	if errs := validateCephMonPubnet([]string{"10.7.7.0/25", "10.22.254.0/25"},
		cephMonPubnetNode{name: "node-07", address: "10.22.254.5", roles: mon},
	); len(errs) != 0 {
		t.Fatalf("arbiter subnet declared for a provided machine still rejected: %v", errs)
	}
}
