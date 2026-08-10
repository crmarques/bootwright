package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func ocpNodeMachine(name string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityOpenShiftNode},
			OS:           v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
		},
	}
}

func infraValidationCluster() (v1alpha1.ContainerCluster, map[string]v1alpha1.Machine) {
	ocp := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "hub"},
		Spec: v1alpha1.ContainerClusterSpec{
			Nodes: []v1alpha1.OCPNodeSpec{
				{Name: "m1", Role: v1alpha1.NodeRoleMaster, MachineRef: v1alpha1.LocalObjectReference{Name: "m1"}},
				{Name: "w1", Role: v1alpha1.NodeRoleWorker, MachineRef: v1alpha1.LocalObjectReference{Name: "w1"}},
				{Name: "i1", Role: v1alpha1.NodeRoleInfra, MachineRef: v1alpha1.LocalObjectReference{Name: "i1"}},
			},
		},
	}
	machines := map[string]v1alpha1.Machine{"m1": ocpNodeMachine("m1"), "w1": ocpNodeMachine("w1"), "i1": ocpNodeMachine("i1")}
	return ocp, machines
}

func TestValidateNodesAcceptsInfraRole(t *testing.T) {
	ocp, machines := infraValidationCluster()
	if errs := validateNodes(ocp, machines); len(errs) != 0 {
		t.Fatalf("infra role should validate, got: %v", errs)
	}
}

func TestValidateNodesRejectsUnknownRole(t *testing.T) {
	ocp, machines := infraValidationCluster()
	ocp.Spec.Nodes[2].Role = "edge"
	if errs := validateNodes(ocp, machines); !containsSubstring(errs, "must be master, worker, or infra") {
		t.Fatalf("expected role error, got: %v", errs)
	}
}

func TestValidateNodesRejectsProvidedOSMachines(t *testing.T) {
	cases := []struct {
		index   int
		machine string
		want    string
	}{
		{index: 0, machine: "m1", want: `ContainerCluster/hub spec.nodes[0].machineRef "m1" references Machine/m1 spec.os.provided=true; booting the OpenShift agent installer would overwrite that operator-provided OS, so set Machine/m1 spec.os.provided=false or remove this node binding`},
		{index: 1, machine: "w1", want: `ContainerCluster/hub spec.nodes[1].machineRef "w1" references Machine/w1 spec.os.provided=true; booting the OpenShift agent installer would overwrite that operator-provided OS, so set Machine/w1 spec.os.provided=false or remove this node binding`},
		{index: 2, machine: "i1", want: `ContainerCluster/hub spec.nodes[2].machineRef "i1" references Machine/i1 spec.os.provided=true; booting the OpenShift agent installer would overwrite that operator-provided OS, so set Machine/i1 spec.os.provided=false or remove this node binding`},
	}
	for _, tc := range cases {
		t.Run(tc.machine, func(t *testing.T) {
			ocp, machines := infraValidationCluster()
			machine := machines[tc.machine]
			machine.Spec.OS.Provided = v1alpha1.BoolPtr(true)
			machines[tc.machine] = machine
			errs := validateNodes(ocp, machines)
			if len(errs) != 1 || errs[0] != tc.want {
				t.Fatalf("validateNodes error = %v, want [%s]", errs, tc.want)
			}
		})
	}
}

func TestValidateNodesRejectsBadTaintEffect(t *testing.T) {
	ocp, machines := infraValidationCluster()
	ocp.Spec.Nodes[2].Taints = []v1alpha1.OCPNodeTaint{{Key: "dedicated", Effect: "Nope"}}
	if errs := validateNodes(ocp, machines); !containsSubstring(errs, "must be one of") {
		t.Fatalf("expected taint effect error, got: %v", errs)
	}
}

func TestValidateNodesRejectsNonRFC1123Hostname(t *testing.T) {
	ocp, machines := infraValidationCluster()
	ocp.Spec.Nodes[0].Name = "Master_0"
	if errs := validateNodes(ocp, machines); !containsSubstring(errs, "must be a lowercase RFC1123 subdomain") {
		t.Fatalf("expected RFC1123 hostname error, got: %v", errs)
	}

	ocp, machines = infraValidationCluster()
	ocp.Spec.Nodes[0].Name = "master-0.hub.example.com"
	if errs := validateNodes(ocp, machines); len(errs) != 0 {
		t.Fatalf("valid lowercase RFC1123 hostname must pass, got: %v", errs)
	}
}

func TestValidateClusterNetworkIPFamilies(t *testing.T) {
	cluster := func(clusterCIDR, serviceCIDR string) v1alpha1.ContainerCluster {
		return v1alpha1.ContainerCluster{
			Metadata: v1alpha1.Metadata{Name: "hub"},
			Spec: v1alpha1.ContainerClusterSpec{
				Networking: &v1alpha1.OCPNetworkingSpec{
					ClusterNetwork: []v1alpha1.ContainerClusterNetworkCIDR{{CIDR: clusterCIDR, HostPrefix: 23}},
					ServiceNetwork: []string{serviceCIDR},
				},
			},
		}
	}
	check := func(ocp v1alpha1.ContainerCluster) []string {
		return validateClusterSingleAddressFamily(v1alpha1.State{}, ocp, map[string]v1alpha1.NetworkConfig{})
	}
	if errs := check(cluster("10.128.0.0/14", "fd00::/112")); !containsSubstring(errs, "mixes IP address families") {
		t.Fatalf("expected IP-family mismatch finding, got: %v", errs)
	}
	if errs := check(cluster("10.128.0.0/14", "172.30.0.0/16")); len(errs) != 0 {
		t.Fatalf("matching v4 primaries must pass, got: %v", errs)
	}
	if errs := check(cluster("fd01::/48", "fd00::/112")); len(errs) != 0 {
		t.Fatalf("matching v6 primaries must pass, got: %v", errs)
	}
}
