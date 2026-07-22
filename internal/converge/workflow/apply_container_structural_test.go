package workflow

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestContainerClusterDay2EditsAreReconcilable(t *testing.T) {
	now := time.Unix(1700000000, 0)
	mkState := func(labelVal, role string, addons []v1alpha1.ClusterAddon) v1alpha1.State {
		return v1alpha1.State{
			ContainerClusters: []v1alpha1.ContainerCluster{{
				Metadata: v1alpha1.Metadata{Name: "demo"},
				Spec: v1alpha1.ContainerClusterSpec{
					Nodes: []v1alpha1.OCPNodeSpec{{
						Name:       "n1",
						Role:       role,
						MachineRef: v1alpha1.LocalObjectReference{Name: "m1"},
						Labels:     map[string]string{"k": labelVal},
					}},
				},
			}},
			ClusterAddons: addons,
		}
	}
	installTasks := func(state v1alpha1.State) []ApplyTask {
		mk := func(id, kind string) ApplyTask {
			return ApplyTask{
				Entry:              TaskLedgerEntry{ID: id, Kind: kind, Cluster: "demo", ClusterKind: "container"},
				State:              state,
				StructuralHashVars: containerClusterInstallStructuralHashVars(state),
			}
		}
		return []ApplyTask{mk("iso.demo", ApplyTaskKindClusterISO), mk("wait.demo", ApplyTaskKindInstallWait)}
	}
	classify := func(t *testing.T, runsDir string, state v1alpha1.State) ObjectClassification {
		t.Helper()
		objs, err := ClassifyApplyObjects(installTasks(state), runsDir)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		for _, o := range objs {
			if o.ObjectKey == "ContainerCluster/demo" {
				return o
			}
		}
		t.Fatalf("ContainerCluster object not classified")
		return ObjectClassification{}
	}

	runsDir := t.TempDir()
	base := mkState("v1", "worker", nil)
	for _, task := range installTasks(base) {
		if err := MarkApplyTaskConvergeSafety(runsDir, "", "", task, ConvergeSafetyStatusReconciled, now); err != nil {
			t.Fatalf("mark: %v", err)
		}
	}

	day2 := classify(t, runsDir, mkState("v2", "worker", []v1alpha1.ClusterAddon{{Metadata: v1alpha1.Metadata{Name: "x"}}}))
	if day2.Class != ConvergeSafetyDrift {
		t.Fatalf("day-2 edit should DISPLAY as drift, got %q", day2.Class)
	}
	if !day2.HasReconcilableDrift() || day2.HasStructuralDrift() {
		t.Fatalf("day-2 label/add-on edit must be reconcilable, not structural: reconcilable=%v structural=%v", day2.HasReconcilableDrift(), day2.HasStructuralDrift())
	}

	roleEdit := classify(t, runsDir, mkState("v1", "master", nil))
	if !roleEdit.HasStructuralDrift() || roleEdit.HasReconcilableDrift() {
		t.Fatalf("install-role change must be structural: structural=%v reconcilable=%v", roleEdit.HasStructuralDrift(), roleEdit.HasReconcilableDrift())
	}

	legacyDir := t.TempDir()
	for _, task := range installTasks(base) {
		desired, err := ApplyTaskDesiredHash(task)
		if err != nil {
			t.Fatalf("desired hash: %v", err)
		}
		saveStateCheckRecord(t, legacyDir, task, desired, ConvergeSafetyOwner)
	}
	legacy := classify(t, legacyDir, mkState("v2", "worker", nil))
	if !legacy.HasStructuralDrift() || legacy.HasReconcilableDrift() {
		t.Fatalf("legacy record must fall back to structural drift: structural=%v reconcilable=%v", legacy.HasStructuralDrift(), legacy.HasReconcilableDrift())
	}
}
