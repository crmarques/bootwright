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

// A hostname with uppercase or an underscore is not a valid RFC1123 subdomain:
// openshift-install rejects it at ISO creation and it can never match the
// lowercase kubelet-registered Node name, so validate must reject it up front.
// A lowercase dotted name stays valid.
func TestValidateNodesRejectsNonRFC1123Hostname(t *testing.T) {
	ocp, machines := infraValidationCluster()
	ocp.Spec.Hosts[0].Hostname = "Master_0"
	if errs := validateNodes(ocp, machines); !containsSubstring(errs, "must be a lowercase RFC1123 subdomain") {
		t.Fatalf("expected RFC1123 hostname error, got: %v", errs)
	}

	ocp, machines = infraValidationCluster()
	ocp.Spec.Hosts[0].Hostname = "master-0.hub.example.com"
	if errs := validateNodes(ocp, machines); len(errs) != 0 {
		t.Fatalf("valid lowercase RFC1123 hostname must pass, got: %v", errs)
	}
}

// OpenShift requires the primary (first-entry) IP family of clusterNetwork and
// serviceNetwork to match; a v4-primary clusterNetwork with a v6-primary
// serviceNetwork validates clean today and fails only inside openshift-install,
// so validate must reject the mismatch and pass a consistent family.
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
	if errs := validateClusterNetworkIPFamilies(cluster("10.128.0.0/14", "fd00::/112")); !containsSubstring(errs, "primary IP family mismatch") {
		t.Fatalf("expected IP-family mismatch finding, got: %v", errs)
	}
	if errs := validateClusterNetworkIPFamilies(cluster("10.128.0.0/14", "172.30.0.0/16")); len(errs) != 0 {
		t.Fatalf("matching v4 primaries must pass, got: %v", errs)
	}
	// A consistent v6-primary dual-stack head also passes (both lists v6 first).
	if errs := validateClusterNetworkIPFamilies(cluster("fd01::/48", "fd00::/112")); len(errs) != 0 {
		t.Fatalf("matching v6 primaries must pass, got: %v", errs)
	}
}
