package workflow

import (
	"testing"
	"time"
)

func stateCheckTask(id, kind, cluster, clusterKind string) ApplyTask {
	return ApplyTask{Entry: TaskLedgerEntry{ID: id, Kind: kind, Label: id, Cluster: cluster, ClusterKind: clusterKind}}
}

func saveStateCheckRecord(t *testing.T, runsDir string, task ApplyTask, hash, owner string) {
	t.Helper()
	record := ConvergeSafetyRecord{
		APIVersion:  ConvergeSafetyAPIVersion,
		ResourceID:  applyTaskSafetyResourceID(task),
		DesiredHash: hash,
		Owner:       ConvergeSafetyOwnerIdentity{Manager: owner},
		Status:      ConvergeSafetyStatusReconciled,
		UpdatedAt:   time.Unix(0, 0).UTC(),
	}
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatalf("save converge safety record: %v", err)
	}
}

func TestStateCheckClassifiesDriftAbsenceAndMatch(t *testing.T) {
	runsDir := t.TempDir()
	match := stateCheckTask("container-cluster.demo", "containerCluster", "demo", "container")
	drift := stateCheckTask("container-cluster.drifted", "containerCluster", "drifted", "container")
	absent := stateCheckTask("storage.gone", "storageCluster", "gone", "storage")

	matchHash, err := ApplyTaskDesiredHash(match)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, match, matchHash, ConvergeSafetyOwner)      // applied with current desired state
	saveStateCheckRecord(t, runsDir, drift, "sha256:stale", ConvergeSafetyOwner) // desired state changed since apply
	// absent: no record at all -> never applied

	report, err := StateCheck([]ApplyTask{match, drift, absent}, runsDir)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	if report.InSync {
		t.Fatal("expected drift, report claims in sync")
	}

	byName := map[string]StateCheckRoot{}
	for _, root := range report.Roots {
		byName[root.Name] = root
	}

	if r := byName["demo"]; r.Absent || len(r.Resources) != 0 || r.Matched != 1 {
		t.Fatalf("demo should be in sync, got %+v", r)
	}
	if r := byName["drifted"]; r.Absent || len(r.Resources) != 1 || r.Resources[0].Classification != ConvergeSafetyDrift {
		t.Fatalf("drifted should report one drift, got %+v", r)
	}
	if r := byName["gone"]; !r.Absent || len(r.Resources) != 0 {
		t.Fatalf("gone should be a single absence, got %+v", r)
	}
}

func TestStateCheckForeignOwner(t *testing.T) {
	runsDir := t.TempDir()
	task := stateCheckTask("container-cluster.demo", "containerCluster", "demo", "container")
	hash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	saveStateCheckRecord(t, runsDir, task, hash, "someone-else")

	report, err := StateCheck([]ApplyTask{task}, runsDir)
	if err != nil {
		t.Fatalf("StateCheck: %v", err)
	}
	if report.InSync {
		t.Fatal("foreign ownership should not be in sync")
	}
	root := report.Roots[0]
	if len(root.Resources) != 1 || root.Resources[0].Classification != ConvergeSafetyForeign {
		t.Fatalf("expected foreign classification, got %+v", root)
	}
}
