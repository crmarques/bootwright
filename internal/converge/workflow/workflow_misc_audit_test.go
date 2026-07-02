package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

// TestVirtctlTaskDesiredHashIsScopeIndependent is the M8 regression guard: the
// per-host virtctl provision task's convergence hash must be identical whether it
// was planned from the full state or a --clusters-filtered state, so an unscoped
// state-check after a scoped apply does not misreport drift (and fail-close the
// next reconcile).
func TestVirtctlTaskDesiredHashIsScopeIndependent(t *testing.T) {
	full := kubeVirtChildPlanningState(true) // child-ocp + its KubeVirt host cluster metal-ocp
	target := applyClustersTarget()

	fullTasks, err := PlanApplyTasksChecked(target, full)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked(full): %v", err)
	}
	// A `--clusters child-ocp` run plans over the filtered state, which drops the
	// KubeVirt host cluster metal-ocp from ContainerClusters — exactly what scoped
	// apply / state-check feed the planner.
	scoped := stategraph.FilterStateToClusters(full, []string{"child-ocp"})
	scopedTasks, err := PlanApplyTasksChecked(target, scoped)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked(scoped): %v", err)
	}

	fullVirtctl := assertTaskPresent(t, fullTasks, "virtctl.metal-ocp")
	scopedVirtctl := assertTaskPresent(t, scopedTasks, "virtctl.metal-ocp")

	// Guard that the carried State genuinely differs across scope, so this test
	// would fail if the task reverted to hashing the whole (scope-varying) State.
	if len(fullVirtctl.State.ContainerClusters) == len(scopedVirtctl.State.ContainerClusters) {
		t.Fatalf("expected filtered State to carry fewer container clusters (full=%d scoped=%d); test no longer exercises scope divergence",
			len(fullVirtctl.State.ContainerClusters), len(scopedVirtctl.State.ContainerClusters))
	}

	fullHash, err := ApplyTaskDesiredHash(fullVirtctl)
	if err != nil {
		t.Fatalf("ApplyTaskDesiredHash(full): %v", err)
	}
	scopedHash, err := ApplyTaskDesiredHash(scopedVirtctl)
	if err != nil {
		t.Fatalf("ApplyTaskDesiredHash(scoped): %v", err)
	}
	if fullHash != scopedHash {
		t.Fatalf("virtctl desired hash is scope-dependent: full=%s scoped=%s", fullHash, scopedHash)
	}
}

// TestStateCheckDegradesOnCorruptConvergeSafetyRecord is the L3 regression guard:
// a single corrupt file under runs/safety/ must not brick the read-only
// state-check report. The offending record is skipped and its file named in
// LoadWarnings instead of aborting with an error.
func TestStateCheckDegradesOnCorruptConvergeSafetyRecord(t *testing.T) {
	state := kubeVirtChildPlanningState(false)
	target := applyClustersTarget()
	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	runsDir := t.TempDir()
	// Corrupt the converge-safety record for one planned task.
	corrupt := assertTaskPresent(t, tasks, "iso.child-ocp")
	corruptPath := ConvergeSafetyRecordPath(runsDir, applyTaskSafetyResourceID(corrupt))
	if err := os.MkdirAll(filepath.Dir(corruptPath), 0o755); err != nil {
		t.Fatalf("mkdir safety dir: %v", err)
	}
	if err := os.WriteFile(corruptPath, []byte("{ this is not valid json"), 0o600); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}

	report, err := StateCheck(tasks, target, state, runsDir)
	if err != nil {
		t.Fatalf("StateCheck aborted on a single corrupt record, want a degraded report: %v", err)
	}

	found := false
	for _, w := range report.LoadWarnings {
		if strings.Contains(w, corruptPath) {
			found = true
		}
	}
	if !found {
		t.Fatalf("LoadWarnings = %v, want a warning naming %s", report.LoadWarnings, corruptPath)
	}
}
