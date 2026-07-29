package workflow

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func provisioningPlaybook(name, anchor, anchorKey string, target v1alpha1.CustomPlaybookTarget) v1alpha1.CustomPlaybook {
	spec := v1alpha1.CustomPlaybookSpec{
		Playbook: "playbooks/" + name + ".yml",
		Target:   target,
	}
	if anchorKey == anchorKeyGates {
		spec.Gates = anchor
	} else {
		spec.Follows = anchor
	}
	return v1alpha1.CustomPlaybook{
		Metadata:   v1alpha1.Metadata{Name: name},
		SourcePath: "input/playbooks/" + name + ".yaml",
		Spec:       spec,
	}
}

const (
	anchorKeyGates   = "gates"
	anchorKeyFollows = "follows"
)

func taskByID(t *testing.T, tasks []ApplyTask, id string) ApplyTask {
	t.Helper()
	return assertTaskPresent(t, tasks, id)
}

func assertDependsOn(t *testing.T, task ApplyTask, dep string) {
	t.Helper()
	for _, d := range task.Entry.Dependencies {
		if d == dep {
			return
		}
	}
	t.Fatalf("%s dependencies = %v, want to include %q", task.Entry.ID, task.Entry.Dependencies, dep)
}

func TestPlanPlaybookAfterBaseWaitsForClusterInstall(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{
		provisioningPlaybook("post-install", v1alpha1.CustomPlaybookAnchorBase, anchorKeyFollows,
			v1alpha1.CustomPlaybookTarget{Clusters: []string{"sno-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	step := taskByID(t, tasks, "playbook.post-install")
	assertDependsOn(t, step, "wait.sno-libvirt")
	assertDependsOn(t, step, "boot.sno-libvirt")
	if step.Entry.Cluster != "sno-libvirt" {
		t.Fatalf("step cluster = %q, want sno-libvirt", step.Entry.Cluster)
	}
	if !step.SkipWhenConverged {
		t.Fatal("default run: onChange should set SkipWhenConverged")
	}
	if step.Limit == "" {
		t.Fatal("step limit must not be empty (empty --limit targets every host)")
	}
}

func TestPlanPlaybookBeforeDepsGatesDepsTasks(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{
		provisioningPlaybook("pre-deps", v1alpha1.CustomPlaybookAnchorDeps, anchorKeyGates,
			v1alpha1.CustomPlaybookTarget{Clusters: []string{"sno-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	iso := taskByID(t, tasks, "iso.sno-libvirt")
	assertDependsOn(t, iso, "playbook.pre-deps")
	step := taskByID(t, tasks, "playbook.pre-deps")
	if len(step.Entry.Dependencies) == 0 {
		t.Fatal("before: deps step should depend on the previous machines-stage tasks")
	}
}

func TestPlanPlaybookGatesAlwaysHardDependsOnTheStage(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	step := provisioningPlaybook("pre-deps", v1alpha1.CustomPlaybookAnchorDeps, anchorKeyGates,
		v1alpha1.CustomPlaybookTarget{Clusters: []string{"sno-libvirt"}})
	step.Spec.OnFailure = v1alpha1.PlaybookFailureFail
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{step}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	iso := taskByID(t, tasks, "iso.sno-libvirt")
	assertDependsOn(t, iso, "playbook.pre-deps")
	if len(iso.Entry.OrderingDependencies) > 0 {
		for _, d := range iso.Entry.OrderingDependencies {
			if d == "playbook.pre-deps" {
				t.Fatal("gates must be a hard dependency, never ordering-only")
			}
		}
	}
}

func TestPlanPlaybookSkippedOutOfStage(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{
		provisioningPlaybook("machines-step", v1alpha1.CustomPlaybookAnchorMachines, anchorKeyFollows,
			v1alpha1.CustomPlaybookTarget{Clusters: []string{"sno-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyClustersTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskMissing(t, tasks, "playbook.machines-step")
}

func TestPlanPlaybookErrorsWhenTargetResolvesToNoHosts(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{
		provisioningPlaybook("ghost", v1alpha1.CustomPlaybookAnchorBase, anchorKeyFollows,
			v1alpha1.CustomPlaybookTarget{Clusters: []string{"does-not-exist"}}),
	}
	_, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err == nil {
		t.Fatal("expected an error rather than a silently dropped playbook")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error should name the playbook, got: %v", err)
	}
}

func TestResolveProvisioningTargetDefersOutOfScopeStorageCluster(t *testing.T) {
	playbook := provisioningPlaybook("ceph-step", v1alpha1.CustomPlaybookAnchorDeps, anchorKeyGates,
		v1alpha1.CustomPlaybookTarget{Clusters: []string{"ceph-a"}})
	target := ApplyTarget{Name: "all", StorageClusterNames: []string{"ceph-b"}}

	limit, _, _, inScope, err := resolveProvisioningTarget(v1alpha1.State{}, target, playbook,
		map[string]bool{}, map[string]bool{"ceph-a": true})
	if err != nil {
		t.Fatalf("a storage cluster outside the run scope must not be an error: %v", err)
	}
	if inScope {
		t.Fatalf("expected the playbook to be skipped, got limit %q", limit)
	}
}

func TestResolveProvisioningTargetErrorsOnMachineWithoutInventoryHost(t *testing.T) {
	playbook := provisioningPlaybook("bastion-step", v1alpha1.CustomPlaybookAnchorMachines, anchorKeyFollows,
		v1alpha1.CustomPlaybookTarget{Machines: []string{"bastion-01"}})

	_, _, _, _, err := resolveProvisioningTarget(v1alpha1.State{}, applyAllTarget(), playbook,
		map[string]bool{}, map[string]bool{})
	if err == nil {
		t.Fatal("expected an error for a machine with no inventory host")
	}
	if !strings.Contains(err.Error(), "bastion-01") || !strings.Contains(err.Error(), "hostGroups") {
		t.Fatalf("error should name the machine and the remedy, got: %v", err)
	}
}

func TestPlanPlaybookDisabledNotPlanned(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	step := provisioningPlaybook("disabled", v1alpha1.CustomPlaybookAnchorBase, anchorKeyFollows,
		v1alpha1.CustomPlaybookTarget{Clusters: []string{"sno-libvirt"}})
	disabled := false
	step.Spec.Enabled = &disabled
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{step}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskMissing(t, tasks, "playbook.disabled")
}
