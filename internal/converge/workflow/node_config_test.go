package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type fakeNodeRunner struct {
	registered map[string]bool
	calls      map[string]int
	readyAfter map[string]int
}

func (f *fakeNodeRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	name := args[2]
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[name]++
	if after, ok := f.readyAfter[name]; ok && f.calls[name] >= after {
		return []byte("node/" + name), nil
	}
	if f.registered[name] {
		return []byte("node/" + name), nil
	}
	return nil, fmt.Errorf("nodes %q not found", name)
}

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

func TestNodeConfigNodeNames(t *testing.T) {
	ocp := ocpWithHosts(
		v1alpha1.OCPHostSpec{Hostname: "m1", Role: v1alpha1.NodeRoleMaster},
		v1alpha1.OCPHostSpec{Hostname: "infra-01", Role: v1alpha1.NodeRoleInfra},
		v1alpha1.OCPHostSpec{Hostname: "w1", Role: v1alpha1.NodeRoleWorker, Labels: map[string]string{"team": "data"}},
	)
	names := nodeConfigNodeNames(ocp)
	if len(names) != 2 || names[0] != "infra-01" || names[1] != "w1" {
		t.Fatalf("expected [infra-01 w1], got %v", names)
	}
}

func TestWaitNodesRegisteredMissingNodeFails(t *testing.T) {
	runner := &fakeNodeRunner{registered: map[string]bool{"infra-01": true}}
	err := waitNodesRegistered(context.Background(), runner, "kc", "hub", []string{"infra-01", "typo-02"}, 1, 0)
	if err == nil {
		t.Fatal("expected failure for the unregistered node")
	}
	if !strings.Contains(err.Error(), "typo-02") || !strings.Contains(err.Error(), "hub") {
		t.Fatalf("error should name the missing node and cluster: %v", err)
	}
}

func TestWaitNodesRegisteredAllPresent(t *testing.T) {
	runner := &fakeNodeRunner{registered: map[string]bool{"a": true, "b": true}}
	if err := waitNodesRegistered(context.Background(), runner, "kc", "hub", []string{"a", "b"}, 1, 0); err != nil {
		t.Fatalf("all nodes present should pass: %v", err)
	}
}

func TestWaitNodesRegisteredLateJoinRetries(t *testing.T) {
	runner := &fakeNodeRunner{readyAfter: map[string]int{"late": 3}}
	if err := waitNodesRegistered(context.Background(), runner, "kc", "hub", []string{"late"}, 5, 0); err != nil {
		t.Fatalf("a node that registers on the third probe should pass: %v", err)
	}
	if runner.calls["late"] != 3 {
		t.Fatalf("expected 3 probes for the late node, got %d", runner.calls["late"])
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
