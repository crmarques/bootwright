package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func pullSecretMergeAddonState() v1alpha1.State {
	state := extensionPlanningState()
	for i := range state.ClusterAddons {
		state.ClusterAddons[i].Spec.Accepts = v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
			Name:      "entitlement",
			SecretRef: &v1alpha1.ClusterAddonInputSecret{},
			Effects: []v1alpha1.ClusterAddonInputEffect{{
				GlobalPullSecretMerge: &v1alpha1.ClusterAddonGlobalPullSecretMergeEffect{Registry: "cp.icr.io", Username: "cp"},
			}},
		}}}
	}
	state.ClusterAddonBindings[0].Spec.AddonConfigs = []v1alpha1.ClusterAddonBindingAddonConfig{
		{
			AddonRef: v1alpha1.LocalObjectReference{Name: "a"},
			Inputs:   []v1alpha1.ClusterAddonBindingInput{{Name: "entitlement", Value: "entitlement-key"}},
		},
		{
			AddonRef: v1alpha1.LocalObjectReference{Name: "b"},
			Inputs:   []v1alpha1.ClusterAddonBindingInput{{Name: "entitlement", Value: "entitlement-key"}},
		},
	}
	return state
}

func preUpgradeAddonRecordHash(t *testing.T, task ApplyTask) string {
	t.Helper()
	legacy := task
	legacy.hashes = nil
	legacy.Entry.ResourceKeys = nil
	hash, err := ApplyTaskDesiredHash(legacy)
	if err != nil {
		t.Fatalf("pre-upgrade desired hash: %v", err)
	}
	return hash
}

func TestAddonPullSecretResourceKeyUpgradeIsNotARefusal(t *testing.T) {
	state := pullSecretMergeAddonState()
	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "add-ons", PhaseNames: []string{ApplyPhaseAddons}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	runsDir := t.TempDir()
	addonTasks := 0
	for i := range tasks {
		if tasks[i].Entry.Kind != ApplyTaskKindClusterAddon {
			continue
		}
		addonTasks++
		if len(tasks[i].Entry.ResourceKeys) == 0 {
			t.Fatalf("%s must carry the pull-secret resource key for this scenario", tasks[i].Entry.ID)
		}
		current, err := ApplyTaskDesiredHash(tasks[i])
		if err != nil {
			t.Fatalf("desired hash: %v", err)
		}
		stored := preUpgradeAddonRecordHash(t, tasks[i])
		if stored == current {
			t.Fatalf("%s: adding the pull-secret resource key must move the desired hash, otherwise this scenario proves nothing", tasks[i].Entry.ID)
		}
		saveStateCheckRecord(t, runsDir, tasks[i], stored, ConvergeSafetyOwner)
	}
	if addonTasks == 0 {
		t.Fatal("fixture planned no add-on tasks")
	}

	objects, err := ClassifyApplyObjects(tasks, runsDir)
	if err != nil {
		t.Fatalf("ClassifyApplyObjects: %v", err)
	}
	seen := 0
	for _, o := range objects {
		if o.Kind != ApplyTaskKindClusterAddon {
			continue
		}
		seen++
		if o.Class != ConvergeSafetyDrift {
			t.Fatalf("%s: a stored pre-upgrade record must classify as drift, got %q", o.Label, o.Class)
		}
		if !o.Reconcilable {
			t.Fatalf("%s: add-on drift caused only by the added pull-secret resource key must be reconcilable, not a rebuild", o.Label)
		}
		if o.HasStructuralDrift() {
			t.Fatalf("%s: add-on drift must never present as structural drift; apply would refuse until the operator passes --converge-drifted", o.Label)
		}
	}
	if seen == 0 {
		t.Fatal("no add-on objects classified")
	}
	if err := EvaluateApplyModePreflight(ApplyModeContinue, objects); err != nil {
		t.Fatalf("plain apply must not refuse an installed fleet whose add-on records predate the pull-secret resource key: %v", err)
	}
	if labels := OverrideDestructiveDriftedObjects(objects); len(labels) != 0 {
		t.Fatalf("add-on drift must never count as destructive drift, got %v", labels)
	}
}

func TestAddonPullSecretResourceKeyUpgradeStateCheckReportsReconcilable(t *testing.T) {
	state := pullSecretMergeAddonState()
	target := ApplyTarget{Name: "add-ons", PhaseNames: []string{ApplyPhaseAddons}}
	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	runsDir := t.TempDir()
	for i := range tasks {
		if tasks[i].Entry.Kind != ApplyTaskKindClusterAddon {
			continue
		}
		saveStateCheckRecord(t, runsDir, tasks[i], preUpgradeAddonRecordHash(t, tasks[i]), ConvergeSafetyOwner)
	}
	report, err := StateCheck(tasks, target, state, runsDir)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	found := 0
	for _, root := range report.Roots {
		for _, resource := range root.Resources {
			if resource.Kind != ApplyTaskKindClusterAddon {
				continue
			}
			found++
			if resource.Classification != ConvergeSafetyDrift || !resource.Reconcilable {
				t.Fatalf("%s: bootwright diff must show the add-on as reconcilable drift, got %q reconcilable=%v", resource.ResourceID, resource.Classification, resource.Reconcilable)
			}
		}
	}
	if found == 0 {
		t.Fatal("state check reported no add-on resources")
	}
}
