package installer

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestInstallerNodeRoleCoercesInfraToWorker(t *testing.T) {
	cases := map[string]string{
		v1alpha1.NodeRoleMaster: "master",
		v1alpha1.NodeRoleWorker: "worker",
		v1alpha1.NodeRoleInfra:  "worker",
	}
	for role, want := range cases {
		if got := InstallerNodeRole(role); got != want {
			t.Fatalf("InstallerNodeRole(%q) = %q, want %q", role, got, want)
		}
	}
}

func TestComputeReplicaCountSumsWorkerAndInfra(t *testing.T) {
	ocp := v1alpha1.ContainerCluster{
		Spec: v1alpha1.ContainerClusterSpec{
			Nodes: []v1alpha1.OCPNodeSpec{
				{Role: v1alpha1.NodeRoleMaster},
				{Role: v1alpha1.NodeRoleMaster},
				{Role: v1alpha1.NodeRoleMaster},
				{Role: v1alpha1.NodeRoleWorker},
				{Role: v1alpha1.NodeRoleInfra},
				{Role: v1alpha1.NodeRoleInfra},
				{Role: v1alpha1.NodeRoleInfra},
			},
		},
	}
	if got := computeReplicaCount(ocp); got != 4 {
		t.Fatalf("computeReplicaCount = %d, want 4 (1 worker + 3 infra)", got)
	}
	if got := nodeRoleCount(ocp, v1alpha1.NodeRoleMaster); got != 3 {
		t.Fatalf("master count = %d, want 3", got)
	}
}
