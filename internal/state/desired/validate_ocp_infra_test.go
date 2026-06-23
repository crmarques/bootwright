package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func ocpNodeMachine(name string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec:     v1alpha1.MachineSpec{Capabilities: []string{v1alpha1.MachineCapabilityOpenShiftNode}},
	}
}

func infraValidationCluster() (v1alpha1.ContainerCluster, map[string]v1alpha1.Machine) {
	ocp := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "hub"},
		Spec: v1alpha1.ContainerClusterSpec{
			Hosts: []v1alpha1.OCPHostSpec{
				{Hostname: "m1", Role: v1alpha1.NodeRoleMaster, MachineRef: v1alpha1.LocalObjectReference{Name: "m1"}},
				{Hostname: "w1", Role: v1alpha1.NodeRoleWorker, MachineRef: v1alpha1.LocalObjectReference{Name: "w1"}},
				{Hostname: "i1", Role: v1alpha1.NodeRoleInfra, MachineRef: v1alpha1.LocalObjectReference{Name: "i1"}},
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

func TestValidateNodesComputeReplicasIncludeInfra(t *testing.T) {
	ocp, machines := infraValidationCluster()
	ocp.Spec.Compute = []v1alpha1.MachinePoolSpec{{Replicas: 2}} // worker(1) + infra(1)
	if errs := validateNodes(ocp, machines); len(errs) != 0 {
		t.Fatalf("compute replicas should count worker+infra, got: %v", errs)
	}
	ocp.Spec.Compute = []v1alpha1.MachinePoolSpec{{Replicas: 1}}
	if errs := validateNodes(ocp, machines); !containsSubstring(errs, "worker+infra node count") {
		t.Fatalf("expected worker+infra replica mismatch, got: %v", errs)
	}
}

func TestValidateNodesRejectsUnknownRole(t *testing.T) {
	ocp, machines := infraValidationCluster()
	ocp.Spec.Hosts[2].Role = "edge"
	if errs := validateNodes(ocp, machines); !containsSubstring(errs, "must be master, worker, or infra") {
		t.Fatalf("expected role error, got: %v", errs)
	}
}

func TestValidateNodesRejectsBadTaintEffect(t *testing.T) {
	ocp, machines := infraValidationCluster()
	ocp.Spec.Hosts[2].Taints = []v1alpha1.OCPNodeTaint{{Key: "dedicated", Effect: "Nope"}}
	if errs := validateNodes(ocp, machines); !containsSubstring(errs, "must be one of") {
		t.Fatalf("expected taint effect error, got: %v", errs)
	}
}
