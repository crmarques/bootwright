package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// state-check annotates each drifted resource with whether its drift reconciles in
// place (a reconfigure-only re-apply, a structural-hash-unchanged edit) or needs a
// destructive rebuild, using the same taskDriftReconcilable primitive the apply
// preflight uses — so the report and the gate never disagree.
func TestStateCheckReportsReconcilableDrift(t *testing.T) {
	runsDir := t.TempDir()
	reconfigure := stateCheckTask("addon.demo.x", "clusterAddon", "demo", "container")
	destructive := stateCheckTask("os.demo", "managedMachineOS", "demo", "container")
	saveStateCheckRecord(t, runsDir, reconfigure, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, destructive, "sha256:stale", ConvergeSafetyOwner)

	report, err := StateCheck([]ApplyTask{reconfigure, destructive}, ApplyTarget{}, v1alpha1.State{}, runsDir)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	got := map[string]StateCheckResource{}
	for _, root := range report.Roots {
		for _, r := range root.Resources {
			got[r.Label] = r
		}
	}
	if r := got["addon.demo.x"]; r.Classification != ConvergeSafetyDrift || !r.Reconcilable {
		t.Fatalf("reconfigure-only drift must be reconcilable: %+v", r)
	}
	if r := got["os.demo"]; r.Classification != ConvergeSafetyDrift || r.Reconcilable {
		t.Fatalf("managed-OS drift must be a rebuild (not reconcilable): %+v", r)
	}
}
