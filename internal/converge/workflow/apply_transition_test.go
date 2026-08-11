package workflow

import "testing"

func TestClassifyApplyTransitions(t *testing.T) {
	runsDir := t.TempDir()
	create := classifyTask("addon.new", ApplyTaskKindClusterAddon, "demo")
	match := classifyTask("addon.match", ApplyTaskKindClusterAddon, "demo")
	recon := classifyTask("addon.drift", ApplyTaskKindClusterAddon, "demo")
	foreign := classifyTask("addon.foreign", ApplyTaskKindClusterAddon, "demo")
	osMachine := classifyTask("os.demo", ApplyTaskKindManagedMachineOS, "demo")

	matchHash, err := ApplyTaskDesiredHash(match)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, match, matchHash, ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, recon, "sha256:stale", ConvergeSafetyOwner)
	saveStateCheckRecord(t, runsDir, foreign, "sha256:stale", "someone-else")
	saveStateCheckRecord(t, runsDir, osMachine, "sha256:stale", ConvergeSafetyOwner)

	objs, err := ClassifyApplyObjects([]ApplyTask{create, match, recon, foreign, osMachine}, runsDir, "test")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	actions := func(mode ApplyMode) map[string]ApplyTransitionAction {
		got := map[string]ApplyTransitionAction{}
		for _, tr := range ClassifyApplyTransitions(objs, mode) {
			got[tr.Label] = tr.Action
		}
		return got
	}

	check := func(mode ApplyMode, want map[string]ApplyTransitionAction) {
		got := actions(mode)
		for label, wantAction := range want {
			if got[label] != wantAction {
				t.Errorf("mode %s: %s => %q, want %q", mode, label, got[label], wantAction)
			}
		}
	}

	check(ApplyModeReconcile, map[string]ApplyTransitionAction{
		"addon.new":     ApplyTransitionCreate,
		"addon.match":   ApplyTransitionUnchanged,
		"addon.drift":   ApplyTransitionReconcile,
		"addon.foreign": ApplyTransitionRefuse,
		"os.demo":       ApplyTransitionRefuse,
	})
	check(ApplyModeRebuild, map[string]ApplyTransitionAction{
		"addon.drift":   ApplyTransitionReconcile,
		"addon.foreign": ApplyTransitionRefuse,
		"os.demo":       ApplyTransitionRebuild,
		"addon.new":     ApplyTransitionCreate,
		"addon.match":   ApplyTransitionUnchanged,
	})
	check(ApplyModeCreate, map[string]ApplyTransitionAction{
		"addon.new":   ApplyTransitionCreate,
		"addon.match": ApplyTransitionRefuse,
	})
}
