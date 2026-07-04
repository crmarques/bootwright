package workflow

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// A ContainerCluster edit confined to day-2-owned intent (a node label; an added
// cluster add-on) classifies as RECONCILABLE drift on the install object, so continue
// proceeds and --override does not reinstall. An install-affecting edit (a node's
// install role) stays STRUCTURAL. A record written before the structural projection
// existed falls back to structural (fail-safe: an upgrade never turns a real reinstall
// into a no-op).
func TestContainerClusterDay2EditsAreReconcilable(t *testing.T) {
	now := time.Unix(1700000000, 0)
	mkState := func(labelVal, role string, addons []v1alpha1.ClusterAddon) v1alpha1.State {
		return v1alpha1.State{
			ContainerClusters: []v1alpha1.ContainerCluster{{
				Metadata: v1alpha1.Metadata{Name: "demo"},
				Spec: v1alpha1.ContainerClusterSpec{
					Hosts: []v1alpha1.OCPHostSpec{{
						Hostname:   "n1",
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

	// Day-2 edits: a changed node label and an added add-on. Reconcilable, not structural.
	day2 := classify(t, runsDir, mkState("v2", "worker", []v1alpha1.ClusterAddon{{Metadata: v1alpha1.Metadata{Name: "x"}}}))
	if day2.Class != ConvergeSafetyDrift {
		t.Fatalf("day-2 edit should DISPLAY as drift, got %q", day2.Class)
	}
	if !day2.HasReconcilableDrift() || day2.HasStructuralDrift() {
		t.Fatalf("day-2 label/add-on edit must be reconcilable, not structural: reconcilable=%v structural=%v", day2.HasReconcilableDrift(), day2.HasStructuralDrift())
	}

	// Install-affecting edit: a node's install role moves the structural hash.
	roleEdit := classify(t, runsDir, mkState("v1", "master", nil))
	if !roleEdit.HasStructuralDrift() || roleEdit.HasReconcilableDrift() {
		t.Fatalf("install-role change must be structural: structural=%v reconcilable=%v", roleEdit.HasStructuralDrift(), roleEdit.HasReconcilableDrift())
	}

	// Legacy record (no structural hash): a day-2 edit falls back to structural.
	legacyDir := t.TempDir()
	for _, task := range installTasks(base) {
		desired, err := ApplyTaskDesiredHash(task)
		if err != nil {
			t.Fatalf("desired hash: %v", err)
		}
		saveStateCheckRecord(t, legacyDir, task, desired, ConvergeSafetyOwner) // no structural hash
	}
	legacy := classify(t, legacyDir, mkState("v2", "worker", nil))
	if !legacy.HasStructuralDrift() || legacy.HasReconcilableDrift() {
		t.Fatalf("legacy record must fall back to structural drift: structural=%v reconcilable=%v", legacy.HasStructuralDrift(), legacy.HasReconcilableDrift())
	}
}
