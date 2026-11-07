package main

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
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
	refusal = requireRefusal(t, editedExample(t, "lab-networks", subnets("203.0.113.0/24")), "api.invariant", "OpenShiftCluster/networks", "$.spec.networking.secondaryNetworks[0].ipam.excludeSubnets")
	if !strings.Contains(refusal.Message, "pods can take 203.0.113.1, held by the provider host's address on managed Network/lab-secondary") || !strings.Contains(refusal.Remediation, "add 203.0.113.1/32") {
		t.Fatalf("refusal = %+v", refusal)
	}
	excluded := exampleEdit{networksCluster, "        ipam:\n          mode: Disabled\n", "        ipam:\n          mode: Enabled\n\n          subnets:\n            - 203.0.113.0/24\n\n          excludeSubnets:\n            - 203.0.113.1/32\n"}
	compileAcceptance(t, editedExample(t, "lab-networks", excluded))
}

const machineSecondary = "\n      - name: machine\n        topology: Localnet\n\n        bridgeMapping:\n          bridge: br-ex\n\n        ipam:\n          mode: Enabled\n\n          subnets:\n            - 198.51.100.0/24\n"

// Pod IPAM over a whole Network leaves out every address a host, gateway or
// endpoint holds on it.
func TestPodIPAMExcludesTheAddressesHeldOnItsNetwork(t *testing.T) {
	network := func(exclude string) exampleEdit {
		return exampleEdit{networksCluster, "            network.example.test/workloads: enabled\n", "            network.example.test/workloads: enabled\n" + machineSecondary + exclude + "\n        namespaceSelector:\n          matchLabels:\n            network.example.test/machine: enabled\n"}
	}
	_, _, err := wireCompiler().Compile(context.Background(), editedExample(t, "lab-networks", network("\n          excludeSubnets:\n            - 198.51.100.0/28\n            - 198.51.100.128/29\n")))
	held := []string{}
	for _, d := range diagnostics.Of(err) {
		if d.Field == "$.spec.networking.secondaryNetworks[1].ipam.excludeSubnets" {
			held = append(held, d.Message)
		}
	}
	if len(held) != 5 || !strings.Contains(held[0], "IPAM subnet 198.51.100.0/24 of secondary network machine spans Network/lab-guests, so pods can take 198.51.100.21, held by node master-0 (Machine/networks-01)") {
		t.Fatalf("refusals = %q in %v", held, diagnostics.Of(err))
	}
	_, _, err = wireCompiler().Compile(context.Background(), editedExample(t, "lab-networks", network("")))
	want := []string{"198.51.100.1, held by the gateway of Network/lab-guests", "198.51.100.10, held by the api endpoint of OpenShiftCluster/networks", "198.51.100.11, held by the ingress endpoint of OpenShiftCluster/networks"}
	for _, d := range diagnostics.Of(err) {
		for i, w := range want {
			if strings.Contains(d.Message, w) {
				want[i] = ""
			}
		}
	}
	if strings.Join(want, "") != "" {
		t.Fatalf("missing %q in %v", want, diagnostics.Of(err))
	}
	compileAcceptance(t, editedExample(t, "lab-networks", network("\n          excludeSubnets:\n            - 198.51.100.0/27\n            - 198.51.100.128/29\n")))
}
