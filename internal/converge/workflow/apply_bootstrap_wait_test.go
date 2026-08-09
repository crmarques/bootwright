package workflow

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPlanApplySplitsClusterWaitAtBootstrapBoundary(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")

	tasks, err := PlanApplyTasksChecked(applyContainerClusterTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	bootstrap := assertTaskPresent(t, tasks, "wait-bootstrap.sno-libvirt")
	if bootstrap.Entry.Kind != ApplyTaskKindBootstrapWait {
		t.Fatalf("bootstrap wait kind = %q, want %q", bootstrap.Entry.Kind, ApplyTaskKindBootstrapWait)
	}
	if bootstrap.Entry.Cluster != "sno-libvirt" || bootstrap.Entry.ClusterKind != ApplyClusterKindContainer {
		t.Fatalf("bootstrap wait cluster = %q/%q", bootstrap.Entry.Cluster, bootstrap.Entry.ClusterKind)
	}
	if !reflect.DeepEqual(bootstrap.ExtraVarPairs, []string{"bootwright_task_cluster_name=sno-libvirt", "bootwright_install_wait_target=bootstrap"}) {
		t.Fatalf("bootstrap wait extra vars = %v", bootstrap.ExtraVarPairs)
	}

	wait := assertTaskPresent(t, tasks, "wait.sno-libvirt")
	if wait.Entry.Kind != ApplyTaskKindInstallWait {
		t.Fatalf("install wait kind = %q, want %q", wait.Entry.Kind, ApplyTaskKindInstallWait)
	}
	if !reflect.DeepEqual(wait.ExtraVarPairs, []string{"bootwright_task_cluster_name=sno-libvirt", "bootwright_install_wait_target=install"}) {
		t.Fatalf("install wait extra vars = %v", wait.ExtraVarPairs)
	}
	if bootstrap.Playbook != wait.Playbook {
		t.Fatalf("bootstrap wait playbook = %q, want the install wait playbook %q", bootstrap.Playbook, wait.Playbook)
	}

	assertTaskDeps(t, tasks, "wait-bootstrap.sno-libvirt", "boot.sno-libvirt")
	assertTaskDeps(t, tasks, "wait.sno-libvirt", "wait-bootstrap.sno-libvirt")
}

func TestBootstrapGateNeverPrecedesHardwareOrAddonWork(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	state.ContainerClusters[0].Spec.Nodes[0].Labels = map[string]string{"bootwright.io/demo": "yes"}

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskDeps(t, tasks, "nodeconfig.sno-libvirt.apply", "wait.sno-libvirt")
	for _, task := range tasks {
		if task.Entry.ID == "wait.sno-libvirt" {
			continue
		}
		for _, dep := range task.Entry.Dependencies {
			if dep == "wait-bootstrap.sno-libvirt" {
				t.Fatalf("%s depends on the bootstrap gate directly; only the install wait may consume it", task.Entry.ID)
			}
		}
	}
	for _, id := range []string{"boot.sno-libvirt", "iso.sno-libvirt", "infra.sno-libvirt.master-0"} {
		assertNoTaskPath(t, tasks, id, "wait-bootstrap.sno-libvirt")
	}
}

func TestAddonsStayBehindInstallCompleteNotBootstrap(t *testing.T) {
	state := extensionPlanningState()

	tasks, err := PlanApplyTasksChecked(applyClustersTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	for _, id := range []string{"addon.demo.a", "addon.demo.b"} {
		assertTaskDeps(t, tasks, id, "wait.demo")
	}
}

func TestKubeVirtChildMachineTasksStillWaitForFullAddonCompletion(t *testing.T) {
	state := kubeVirtChildPlanningState(true)

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskHasDeps(t, tasks, "infra.child-ocp.localhost", "wait.metal-ocp", "addon.metal-ocp.openshift-virtualization")
	assertTaskDeps(t, tasks, "addon.metal-ocp.openshift-virtualization", "wait.metal-ocp")

	byID := map[string]ApplyTask{}
	for _, task := range tasks {
		byID[task.Entry.ID] = task
	}
	for _, dep := range byID["infra.child-ocp.localhost"].Entry.Dependencies {
		if dep == "wait-bootstrap.metal-ocp" {
			t.Fatal("child machine task depends on the parent bootstrap gate; storageClassRef/networkRef objects are only guaranteed after the parent add-on completes")
		}
	}
}

func TestBootstrapWaitAdditionLeavesInstalledClusterConverged(t *testing.T) {
	now := time.Unix(1700000000, 0)
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	tasks, err := PlanApplyTasksChecked(applyContainerClusterTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	runsDir := t.TempDir()
	for _, task := range tasks {
		if task.Entry.Kind == ApplyTaskKindBootstrapWait {
			continue
		}
		if err := MarkApplyTaskConvergeSafety(runsDir, "", "", task, ConvergeSafetyStatusReconciled, now); err != nil {
			t.Fatalf("mark: %v", err)
		}
	}

	objects, err := ClassifyApplyObjects(tasks, runsDir)
	if err != nil {
		t.Fatalf("ClassifyApplyObjects: %v", err)
	}
	var cluster, bootstrap ObjectClassification
	for _, object := range objects {
		switch object.ObjectKey {
		case "ContainerCluster/sno-libvirt":
			cluster = object
		case ApplyTaskKindBootstrapWait + "/wait-bootstrap.sno-libvirt":
			bootstrap = object
		}
	}
	if cluster.ObjectKey == "" || bootstrap.ObjectKey == "" {
		t.Fatalf("classified objects = %+v", objects)
	}
	if cluster.Class != ConvergeSafetyMatch {
		t.Fatalf("pre-upgrade ContainerCluster classifies as %q, want %q: adding the bootstrap wait must not reclassify an installed cluster", cluster.Class, ConvergeSafetyMatch)
	}
	if cluster.HasStructuralDrift() || cluster.HasForeign() {
		t.Fatalf("pre-upgrade ContainerCluster drift = structural:%v foreign:%v, want neither", cluster.HasStructuralDrift(), cluster.HasForeign())
	}
	if bootstrap.Class != ConvergeSafetyMissing || bootstrap.Recorded() {
		t.Fatalf("unrecorded bootstrap wait classifies as %q recorded=%v, want missing and unrecorded", bootstrap.Class, bootstrap.Recorded())
	}

	if err := EvaluateApplyModePreflight(ApplyModeReconcile, objects); err != nil {
		t.Fatalf("continue-mode preflight on a pre-upgrade installed cluster refused: %v", err)
	}
	if err := EvaluateApplyModePreflight(ApplyModeRebuild, objects); err != nil {
		t.Fatalf("converge-drifted preflight on a pre-upgrade installed cluster refused: %v", err)
	}
	err = EvaluateApplyModePreflight(ApplyModeCreate, objects)
	if err == nil || !strings.Contains(err.Error(), "ContainerCluster/sno-libvirt") {
		t.Fatalf("expect-new preflight error = %v, want the existing cluster still refused", err)
	}
}

func TestInstalledClusterSkipsBootstrapWaitTaskKind(t *testing.T) {
	if !isClusterInstallTaskKind(ApplyTaskKindBootstrapWait) {
		t.Fatal("bootstrap wait must count as a cluster install task kind so an already-installed cluster skips it before Ansible")
	}
	kinds := map[string]bool{}
	for _, kind := range allClusterInstallTaskKinds() {
		kinds[kind] = true
	}
	if !kinds[ApplyTaskKindBootstrapWait] {
		t.Fatalf("allClusterInstallTaskKinds = %v, want the bootstrap wait", allClusterInstallTaskKinds())
	}
	if phase, ok := clusterInstallTaskStartPhase(ApplyTaskKindBootstrapWait); !ok || phase != ClusterInstallPhaseWaitingBootstrap {
		t.Fatalf("bootstrap wait start phase = %q found=%v, want %q", phase, ok, ClusterInstallPhaseWaitingBootstrap)
	}
	if phase, ok := clusterInstallTaskSuccessPhase(ApplyTaskKindBootstrapWait); !ok || phase != ClusterInstallPhaseBootstrapComplete {
		t.Fatalf("bootstrap wait success phase = %q found=%v, want %q", phase, ok, ClusterInstallPhaseBootstrapComplete)
	}
	if SubstrateReleaseClearKind(ApplyTaskKindBootstrapWait) {
		t.Fatal("bootstrap wait must not consume a substrate release; install-complete owns that round trip")
	}
}

func TestNodeConfigStaysBehindInstallComplete(t *testing.T) {
	state := v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "demo"},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{
			Name:   "n1",
			Role:   v1alpha1.NodeRoleInfra,
			Labels: map[string]string{"k": "v"},
		}}},
	}}}
	graph := NewActivityGraph()
	if err := planNodeConfigActivities(graph, state, true); err != nil {
		t.Fatalf("planNodeConfigActivities: %v", err)
	}
	for _, activity := range graph.ActivitySnapshot() {
		if activity.ID != "nodeconfig.demo.apply" {
			continue
		}
		if !reflect.DeepEqual(activity.ExplicitDependencies, []string{"wait.demo"}) {
			t.Fatalf("node config deps = %v, want the install-complete wait", activity.ExplicitDependencies)
		}
		return
	}
	t.Fatal("node config activity not planned")
}
