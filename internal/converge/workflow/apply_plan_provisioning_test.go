package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func provisioningPlaybook(name, stage, timing string, target v1alpha1.ProvisioningPlaybookTarget) v1alpha1.ProvisioningPlaybook {
	return v1alpha1.ProvisioningPlaybook{
		Metadata:   v1alpha1.Metadata{Name: name},
		SourcePath: "input/hooks/" + name + ".yaml",
		Spec: v1alpha1.ProvisioningPlaybookSpec{
			Stage:    stage,
			Timing:   timing,
			Playbook: "playbooks/" + name + ".yml",
			Target:   target,
		},
	}
}

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

func assertOrderingDependsOn(t *testing.T, task ApplyTask, dep string) {
	t.Helper()
	for _, d := range task.Entry.OrderingDependencies {
		if d == dep {
			return
		}
	}
	t.Fatalf("%s orderingDependencies = %v, want to include %q", task.Entry.ID, task.Entry.OrderingDependencies, dep)
}

func TestPlanProvisioningPlaybookAfterBaseWaitsForClusterInstall(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	state.ProvisioningPlaybooks = []v1alpha1.ProvisioningPlaybook{
		provisioningPlaybook("post-install", v1alpha1.ProvisioningStageBase, v1alpha1.ProvisioningPlaybookTimingAfter,
			v1alpha1.ProvisioningPlaybookTarget{Clusters: []string{"sno-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	hook := taskByID(t, tasks, "playbook.post-install")
	assertDependsOn(t, hook, "wait.sno-libvirt")
	assertDependsOn(t, hook, "boot.sno-libvirt")
	if hook.Entry.Cluster != "sno-libvirt" {
		t.Fatalf("hook cluster = %q, want sno-libvirt", hook.Entry.Cluster)
	}
	if !hook.SkipWhenConverged {
		t.Fatal("default run: onChange should set SkipWhenConverged")
	}
	if hook.Limit == "" {
		t.Fatal("hook limit must not be empty (empty --limit targets every host)")
	}
}

func TestPlanProvisioningPlaybookBeforeDepsGatesDepsTasks(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	state.ProvisioningPlaybooks = []v1alpha1.ProvisioningPlaybook{
		provisioningPlaybook("pre-deps", v1alpha1.ProvisioningStageDeps, v1alpha1.ProvisioningPlaybookTimingBefore,
			v1alpha1.ProvisioningPlaybookTarget{Clusters: []string{"sno-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	iso := taskByID(t, tasks, "iso.sno-libvirt")
	assertDependsOn(t, iso, "playbook.pre-deps")
	hook := taskByID(t, tasks, "playbook.pre-deps")
	if len(hook.Entry.Dependencies) == 0 {
		t.Fatal("before: deps hook should depend on the previous machines-stage tasks")
	}
}

func TestPlanProvisioningPlaybookBeforeContinueUsesOrderingDependency(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	hook := provisioningPlaybook("pre-deps", v1alpha1.ProvisioningStageDeps, v1alpha1.ProvisioningPlaybookTimingBefore,
		v1alpha1.ProvisioningPlaybookTarget{Clusters: []string{"sno-libvirt"}})
	hook.Spec.FailureMode = v1alpha1.ProvisioningPlaybookFailureContinue
	state.ProvisioningPlaybooks = []v1alpha1.ProvisioningPlaybook{hook}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	iso := taskByID(t, tasks, "iso.sno-libvirt")
	assertOrderingDependsOn(t, iso, "playbook.pre-deps")
	for _, d := range iso.Entry.Dependencies {
		if d == "playbook.pre-deps" {
			t.Fatal("failureMode: continue should not create a hard dependency on the playbook")
		}
	}
}

func TestPlanProvisioningPlaybookSkippedOutOfStage(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	state.ProvisioningPlaybooks = []v1alpha1.ProvisioningPlaybook{
		provisioningPlaybook("machines-hook", v1alpha1.ProvisioningStageMachines, v1alpha1.ProvisioningPlaybookTimingAfter,
			v1alpha1.ProvisioningPlaybookTarget{Clusters: []string{"sno-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyClustersTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskMissing(t, tasks, "playbook.machines-hook")
}

func TestPlanProvisioningPlaybookSkippedWhenTargetOutOfScope(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	state.ProvisioningPlaybooks = []v1alpha1.ProvisioningPlaybook{
		provisioningPlaybook("ghost", v1alpha1.ProvisioningStageBase, v1alpha1.ProvisioningPlaybookTimingAfter,
			v1alpha1.ProvisioningPlaybookTarget{Clusters: []string{"does-not-exist"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskMissing(t, tasks, "playbook.ghost")
}

func TestPlanProvisioningPlaybookDisabledNotPlanned(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	hook := provisioningPlaybook("disabled", v1alpha1.ProvisioningStageBase, v1alpha1.ProvisioningPlaybookTimingAfter,
		v1alpha1.ProvisioningPlaybookTarget{Clusters: []string{"sno-libvirt"}})
	disabled := false
	hook.Spec.Enabled = &disabled
	state.ProvisioningPlaybooks = []v1alpha1.ProvisioningPlaybook{hook}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskMissing(t, tasks, "playbook.disabled")
}
