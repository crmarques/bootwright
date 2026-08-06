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

func ocpWithHosts(hosts ...v1alpha1.OCPNodeSpec) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "hub"},
		Spec:     v1alpha1.ContainerClusterSpec{Nodes: hosts},
	}
}

func TestClusterNeedsNodeConfig(t *testing.T) {
	plain := ocpWithHosts(
		v1alpha1.OCPNodeSpec{Name: "m1", Role: v1alpha1.NodeRoleMaster},
		v1alpha1.OCPNodeSpec{Name: "w1", Role: v1alpha1.NodeRoleWorker},
	)
	if !clusterNeedsNodeConfig(plain) {
		t.Fatal("every cluster with nodes reconciles node config so removals propagate")
	}
	if clusterNeedsNodeConfig(ocpWithHosts()) {
		t.Fatal("a cluster with no nodes has nothing to reconcile")
	}
	if !clusterNeedsNodeConfig(ocpWithHosts(v1alpha1.OCPNodeSpec{Name: "i1", Role: v1alpha1.NodeRoleInfra})) {
		t.Fatal("an infra cluster needs node config")
	}
}

func TestNodeConfigManifestsInfra(t *testing.T) {
	ocp := ocpWithHosts(
		v1alpha1.OCPNodeSpec{Name: "master-01", Role: v1alpha1.NodeRoleMaster},
		v1alpha1.OCPNodeSpec{Name: "infra-01", Role: v1alpha1.NodeRoleInfra},
	)
	out, err := nodeConfigManifests(ocp, nil)
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
	ocp := ocpWithHosts(v1alpha1.OCPNodeSpec{
		Name: "w1", Role: v1alpha1.NodeRoleWorker,
		Labels: map[string]string{"team": "data"},
	})
	out, err := nodeConfigManifests(ocp, nil)
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
		v1alpha1.OCPNodeSpec{Name: "m1", Role: v1alpha1.NodeRoleMaster},
		v1alpha1.OCPNodeSpec{Name: "infra-01", Role: v1alpha1.NodeRoleInfra},
		v1alpha1.OCPNodeSpec{Name: "w1", Role: v1alpha1.NodeRoleWorker, Labels: map[string]string{"team": "data"}},
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
		v1alpha1.OCPNodeSpec{Name: "m1", Role: v1alpha1.NodeRoleMaster},
		v1alpha1.OCPNodeSpec{Name: "w1", Role: v1alpha1.NodeRoleWorker},
	)
	out, err := nodeConfigManifests(ocp, nil)
	if err != nil {
		t.Fatalf("nodeConfigManifests: %v", err)
	}
	if out != nil {
		t.Fatalf("expected no manifests, got:\n%s", string(out))
	}
}

type fakeManagedFieldsRunner struct {
	managers map[string]string
	missing  map[string]bool
}

func (f *fakeManagedFieldsRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	name := args[2]
	if f.missing[name] {
		return nil, fmt.Errorf("nodes %q not found", name)
	}
	return []byte(f.managers[name]), nil
}

func TestNodeConfigApplySetCoversOnlyManagedNodes(t *testing.T) {
	ocp := ocpWithHosts(
		v1alpha1.OCPNodeSpec{Name: "infra-01", Role: v1alpha1.NodeRoleInfra},
		v1alpha1.OCPNodeSpec{Name: "demoted-01", Role: v1alpha1.NodeRoleWorker},
		v1alpha1.OCPNodeSpec{Name: "untouched-01", Role: v1alpha1.NodeRoleWorker},
		v1alpha1.OCPNodeSpec{Name: "never-joined", Role: v1alpha1.NodeRoleWorker},
	)
	runner := &fakeManagedFieldsRunner{
		managers: map[string]string{
			"demoted-01":   "kubelet bootwright kube-controller-manager",
			"untouched-01": "kubelet kube-controller-manager",
		},
		missing: map[string]bool{"never-joined": true},
	}
	set := nodeConfigApplySet(context.Background(), runner, "kc", ocp, nodeConfigNodeNames(ocp))
	if !set["infra-01"] {
		t.Fatal("a node carrying declared config is always applied")
	}
	if !set["demoted-01"] {
		t.Fatal("a node bootwright still manages must be re-applied so its stale fields are relinquished")
	}
	if set["untouched-01"] {
		t.Fatal("a node bootwright never configured must not be written at all")
	}
	if set["never-joined"] {
		t.Fatal("an unregistered node must not be applied")
	}
}

type fakeMCPRunner struct {
	getOut  string
	getErr  error
	deleted bool
}

func (f *fakeMCPRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	if args[0] == "delete" {
		f.deleted = true
		return nil, nil
	}
	return []byte(f.getOut), f.getErr
}

func TestNodeConfigManifestsDedupesAuthoredInfraTaint(t *testing.T) {
	ocp := ocpWithHosts(v1alpha1.OCPNodeSpec{
		Name: "infra-01",
		Role: v1alpha1.NodeRoleInfra,
		Taints: []v1alpha1.OCPNodeTaint{
			{Key: v1alpha1.InfraNodeRoleLabel, Effect: v1alpha1.TaintEffectNoSchedule},
		},
	})
	out, err := nodeConfigManifests(ocp, nil)
	if err != nil {
		t.Fatalf("nodeConfigManifests: %v", err)
	}
	if got := strings.Count(string(out), "key: "+v1alpha1.InfraNodeRoleLabel); got != 1 {
		t.Fatalf("expected the authored infra taint to collapse to one entry, got %d:\n%s", got, string(out))
	}
}

func TestNodeConfigManifestsDedupesTaintKeyEffectPairAcrossValues(t *testing.T) {
	ocp := ocpWithHosts(v1alpha1.OCPNodeSpec{
		Name: "infra-01",
		Role: v1alpha1.NodeRoleInfra,
		Taints: []v1alpha1.OCPNodeTaint{
			{Key: v1alpha1.InfraNodeRoleLabel, Value: "reserved", Effect: v1alpha1.TaintEffectNoSchedule},
		},
	})
	out, err := nodeConfigManifests(ocp, nil)
	if err != nil {
		t.Fatalf("nodeConfigManifests: %v", err)
	}
	s := string(out)
	if got := strings.Count(s, "key: "+v1alpha1.InfraNodeRoleLabel); got != 1 {
		t.Fatalf("kubernetes dedupes taints by key+effect, so one entry must survive, got %d:\n%s", got, s)
	}
	if !strings.Contains(s, "value: reserved") {
		t.Fatalf("the authored taint must win over the synthesized one:\n%s", s)
	}
}

func TestNodeConfigManifestsRelinquishesClearedNode(t *testing.T) {
	ocp := ocpWithHosts(
		v1alpha1.OCPNodeSpec{Name: "infra-01", Role: v1alpha1.NodeRoleInfra},
		v1alpha1.OCPNodeSpec{Name: "demoted-01", Role: v1alpha1.NodeRoleWorker},
	)
	out, err := nodeConfigManifests(ocp, map[string]bool{"demoted-01": true})
	if err != nil {
		t.Fatalf("nodeConfigManifests: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "name: demoted-01") {
		t.Fatalf("a live node with no declared config needs a bare doc so server-side apply drops what bootwright owned:\n%s", s)
	}
	if strings.Contains(s, "demoted-01\n  labels") {
		t.Fatalf("the relinquish doc must carry no labels:\n%s", s)
	}
}

func TestNodeConfigManifestsSkipsAbsentUnconfiguredNode(t *testing.T) {
	ocp := ocpWithHosts(
		v1alpha1.OCPNodeSpec{Name: "infra-01", Role: v1alpha1.NodeRoleInfra},
		v1alpha1.OCPNodeSpec{Name: "never-joined", Role: v1alpha1.NodeRoleWorker},
	)
	out, err := nodeConfigManifests(ocp, map[string]bool{})
	if err != nil {
		t.Fatalf("nodeConfigManifests: %v", err)
	}
	if strings.Contains(string(out), "never-joined") {
		t.Fatalf("an unregistered node must not be applied, or oc would create a phantom Node:\n%s", string(out))
	}
}

func TestInfraMachineConfigPoolCarriesManagedByLabel(t *testing.T) {
	ocp := ocpWithHosts(v1alpha1.OCPNodeSpec{Name: "infra-01", Role: v1alpha1.NodeRoleInfra})
	out, err := nodeConfigManifests(ocp, nil)
	if err != nil {
		t.Fatalf("nodeConfigManifests: %v", err)
	}
	if !strings.Contains(string(out), v1alpha1.ManagedByLabel+": "+v1alpha1.ManagedByLabelValue) {
		t.Fatalf("the pool must be labelled so pruning can tell it from an operator's own pool:\n%s", string(out))
	}
}

func TestPruneInfraMachineConfigPoolDeletesOwnPool(t *testing.T) {
	runner := &fakeMCPRunner{getOut: "machineconfigpool.machineconfiguration.openshift.io/infra\n"}
	pruned, err := pruneInfraMachineConfigPool(context.Background(), runner, "kc")
	if err != nil {
		t.Fatalf("pruneInfraMachineConfigPool: %v", err)
	}
	if !pruned || !runner.deleted {
		t.Fatal("a bootwright-owned pool must be deleted once the last infra node is gone")
	}
}

func TestPruneInfraMachineConfigPoolLeavesForeignPool(t *testing.T) {
	runner := &fakeMCPRunner{getOut: "\n"}
	pruned, err := pruneInfraMachineConfigPool(context.Background(), runner, "kc")
	if err != nil {
		t.Fatalf("pruneInfraMachineConfigPool: %v", err)
	}
	if pruned || runner.deleted {
		t.Fatal("a pool bootwright does not own must be left alone")
	}
}

func TestPruneInfraMachineConfigPoolToleratesMissingKind(t *testing.T) {
	runner := &fakeMCPRunner{getErr: fmt.Errorf("the server doesn't have a resource type \"machineconfigpool\"")}
	pruned, err := pruneInfraMachineConfigPool(context.Background(), runner, "kc")
	if err != nil {
		t.Fatalf("a cluster without the MachineConfigPool kind must not fail the task: %v", err)
	}
	if pruned || runner.deleted {
		t.Fatal("nothing to prune when the kind is absent")
	}
}
