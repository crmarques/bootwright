package main

import (
	"strings"
	"testing"
)

const workloadsMapping = "        bridgeMapping:\n          bridge: br-workloads\n          interface: enp2s0\n"

// The renamed uplink is an unknown field, and a node's bridge interface names
// a declared secondary network.
func TestASecondaryNetworkMapsItsBridge(t *testing.T) {
	refusal := requireRefusal(t, editedExample(t, "lab-networks", exampleEdit{networksCluster, "        bridgeMapping:\n", "        uplink:\n"}), "api.field", "OpenShiftCluster/networks", "$.spec.networking.secondaryNetworks[0].uplink")
	if !strings.Contains(refusal.Message+refusal.Remediation, "bridgeMapping") {
		t.Fatalf("refusal = %+v", refusal)
	}
	override := exampleEdit{networksCluster, "      machineRef: networks-04\n", "      machineRef: networks-04\n\n      bridgeInterfaces:\n        - networkRef: tenants\n          interface: enp2s0\n"}
	requireRefusal(t, editedExample(t, "lab-networks", override), "api.reference", "OpenShiftCluster/networks", "$.spec.nodes[3].bridgeInterfaces[0].networkRef")
}

// A secondary network's native vlan and IPAM subnets are checked against the
// Network the bridge interface's segment carries.
func TestASecondaryNetworkMatchesTheNetworkItsSegmentCarries(t *testing.T) {
	tagged := exampleEdit{networksCluster, workloadsMapping, workloadsMapping + "\n        vlan: 140\n"}
	refusal := requireRefusal(t, editedExample(t, "lab-networks", tagged), "api.invariant", "OpenShiftCluster/networks", "$.spec.networking.secondaryNetworks[0].vlan")
	for _, want := range []string{"secondary network workloads reaches interface enp2s0 of node master-0", "carries no tagged Network with vlan 140"} {
		if !strings.Contains(refusal.Message, want) {
			t.Fatalf("refusal = %+v", refusal)
		}
	}
	subnets := func(cidr string) exampleEdit {
		return exampleEdit{networksCluster, "        ipam:\n          mode: Disabled\n", "        ipam:\n          mode: Enabled\n\n          subnets:\n            - " + cidr + "\n"}
	}
	refusal = requireRefusal(t, editedExample(t, "lab-networks", subnets("203.0.113.0/25")), "api.invariant", "OpenShiftCluster/networks", "$.spec.networking.secondaryNetworks[0].ipam.subnets[0]")
	if !strings.Contains(refusal.Message, "is not the ipv4 cidr of Network/lab-secondary") || !strings.Contains(refusal.Remediation, "203.0.113.0/24") {
		t.Fatalf("refusal = %+v", refusal)
	}
	compileAcceptance(t, editedExample(t, "lab-networks", subnets("203.0.113.0/24")))
}
