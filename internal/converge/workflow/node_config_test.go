package workflow

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func ocpWithHosts(hosts ...v1alpha1.OCPHostSpec) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "hub"},
		Spec:     v1alpha1.ContainerClusterSpec{Hosts: hosts},
	}
}

func TestClusterNeedsNodeConfig(t *testing.T) {
	plain := ocpWithHosts(
		v1alpha1.OCPHostSpec{Hostname: "m1", Role: v1alpha1.NodeRoleMaster},
		v1alpha1.OCPHostSpec{Hostname: "w1", Role: v1alpha1.NodeRoleWorker},
	)
	if clusterNeedsNodeConfig(plain) {
		t.Fatal("a master/worker-only cluster needs no day-2 node config")
	}
	if !clusterNeedsNodeConfig(ocpWithHosts(v1alpha1.OCPHostSpec{Hostname: "i1", Role: v1alpha1.NodeRoleInfra})) {
		t.Fatal("an infra cluster needs node config")
	}
	labelled := ocpWithHosts(v1alpha1.OCPHostSpec{Hostname: "w1", Role: v1alpha1.NodeRoleWorker, Labels: map[string]string{"team": "x"}})
	if !clusterNeedsNodeConfig(labelled) {
		t.Fatal("a labelled worker needs node config")
	}
}

func TestNodeConfigManifestsInfra(t *testing.T) {
	ocp := ocpWithHosts(
		v1alpha1.OCPHostSpec{Hostname: "master-01", Role: v1alpha1.NodeRoleMaster},
		v1alpha1.OCPHostSpec{Hostname: "infra-01", Role: v1alpha1.NodeRoleInfra},
	)
	out, err := nodeConfigManifests(ocp)
	if err != nil {
		t.Fatalf("nodeConfigManifests: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"kind: Node", "name: infra-01", "node-role.kubernetes.io/infra",
		"effect: NoSchedule", "kind: MachineConfigPool", "name: infra",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("manifests missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "master-01") {
		t.Fatalf("master node must not get a day-2 patch:\n%s", s)
	}
}

func TestNodeConfigManifestsLabelledWorkerNoMCP(t *testing.T) {
	ocp := ocpWithHosts(v1alpha1.OCPHostSpec{
		Hostname: "w1", Role: v1alpha1.NodeRoleWorker,
		Labels: map[string]string{"team": "data"},
	})
	out, err := nodeConfigManifests(ocp)
	if err != nil {
		t.Fatalf("nodeConfigManifests: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "name: w1") || !strings.Contains(s, "team") {
		t.Fatalf("expected a Node patch for the labelled worker:\n%s", s)
	}
	if strings.Contains(s, "MachineConfigPool") {
		t.Fatalf("no infra host, so no MachineConfigPool expected:\n%s", s)
	}
}

func TestNodeConfigManifestsEmptyWhenNothingToDo(t *testing.T) {
	ocp := ocpWithHosts(
		v1alpha1.OCPHostSpec{Hostname: "m1", Role: v1alpha1.NodeRoleMaster},
		v1alpha1.OCPHostSpec{Hostname: "w1", Role: v1alpha1.NodeRoleWorker},
	)
	out, err := nodeConfigManifests(ocp)
	if err != nil {
		t.Fatalf("nodeConfigManifests: %v", err)
	}
	if out != nil {
		t.Fatalf("expected no manifests, got:\n%s", string(out))
	}
}
