package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuccessfulInputSnapshotIsImmutableAndRunBound(t *testing.T) {
	const (
		runID      = "apply-1"
		resourceID = "ContainerCluster/install.demo"
		taskID     = "wait.demo"
		taskKind   = ApplyTaskKindInstallWait
	)
	runsDir := t.TempDir()
	input := []byte(`{"desired":"v1"}`)
	if err := saveSuccessfulInputSnapshot(runsDir, runID, resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-1, input); err != nil {
		t.Fatalf("saveSuccessfulInputSnapshot: %v", err)
	}
	if err := saveSuccessfulInputSnapshot(runsDir, runID, resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-1, input); err != nil {
		t.Fatalf("idempotent snapshot write: %v", err)
	}
	if err := saveSuccessfulInputSnapshot(runsDir, runID, resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-1, []byte(`{"desired":"v2"}`)); err == nil || !strings.Contains(err.Error(), "refusing to replace immutable run evidence") {
		t.Fatalf("changed snapshot write = %v, want immutable refusal", err)
	}
	ledger := RunLedger{
		RunID:  runID,
		Status: RunStatusOK,
		Tasks:  []TaskLedgerEntry{{ID: taskID, Kind: taskKind, Status: TaskStatusOK}},
	}
	if err := ArchiveRunLedger(runsDir, ledger); err != nil {
		t.Fatalf("ArchiveRunLedger: %v", err)
	}
	matched, err := successfulInputSnapshotMatches(runsDir, runID, resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-1, input)
	if err != nil || !matched {
		t.Fatalf("exact snapshot match = %v, %v, want true", matched, err)
	}
	matched, err = successfulInputSnapshotMatches(runsDir, runID, resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-1, []byte(`{"desired":"v2"}`))
	if err != nil || matched {
		t.Fatalf("changed input match = %v, %v, want drift without evidence error", matched, err)
	}
}

func TestSuccessfulInputSnapshotMissingOrAmbiguousEvidenceFailsClosed(t *testing.T) {
	const (
		runID      = "apply-1"
		resourceID = "StoragePool/demo.data"
		taskID     = "storage.demo"
		taskKind   = ApplyTaskKindStorageCluster
	)
	input := []byte(`{"desired":"v1"}`)
	assertError := func(t *testing.T, runsDir string, want string) {
		t.Helper()
		matched, err := successfulInputSnapshotMatches(runsDir, runID, resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-1, input)
		if err == nil || matched || !strings.Contains(err.Error(), want) {
			t.Fatalf("match = %v, error = %v, want fail-closed error containing %q", matched, err, want)
		}
	}

	assertError(t, t.TempDir(), "is missing")

	missingLedgerDir := t.TempDir()
	if err := saveSuccessfulInputSnapshot(missingLedgerDir, runID, resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-1, input); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	assertError(t, missingLedgerDir, "no archived run ledger")

	failedRunDir := t.TempDir()
	if err := saveSuccessfulInputSnapshot(failedRunDir, runID, resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-1, input); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if err := ArchiveRunLedger(failedRunDir, RunLedger{RunID: runID, Status: RunStatusFailed, Tasks: []TaskLedgerEntry{{ID: taskID, Kind: taskKind, Status: TaskStatusOK}}}); err != nil {
		t.Fatalf("archive failed ledger: %v", err)
	}
	assertError(t, failedRunDir, "not bound to an archived successful run")

	duplicateDir := t.TempDir()
	if err := saveSuccessfulInputSnapshot(duplicateDir, runID, resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-1, input); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	duplicate := TaskLedgerEntry{ID: taskID, Kind: taskKind, Status: TaskStatusOK}
	if err := ArchiveRunLedger(duplicateDir, RunLedger{RunID: runID, Status: RunStatusOK, Tasks: []TaskLedgerEntry{duplicate, duplicate}}); err != nil {
		t.Fatalf("archive duplicate ledger: %v", err)
	}
	assertError(t, duplicateDir, "found 2 tasks")

	if _, err := successfulInputSnapshotMatches(t.TempDir(), "", resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-1, input); err == nil {
		t.Fatal("missing run identity must fail closed")
	}
	if _, err := successfulInputSnapshotMatches(t.TempDir(), runID, resourceID, taskID, taskKind, TaskStatusOK, ConvergeHashSchema-2, input); err == nil {
		t.Fatal("unexpected schema must fail closed")
	}

	brokenDir := t.TempDir()
	path := successfulInputSnapshotPath(brokenDir, runID, resourceID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write broken snapshot: %v", err)
	}
	assertError(t, brokenDir, "decode successful input snapshot")
}

func TestLegacyClassificationWithoutSnapshotIsUnknown(t *testing.T) {
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "wait.demo", Kind: ApplyTaskKindInstallWait, Cluster: "demo"}}
	desiredHash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatalf("ApplyTaskDesiredHash: %v", err)
	}
	record := ConvergeSafetyRecord{
		ResourceID:  applyTaskSafetyResourceID(task),
		TaskID:      task.Entry.ID,
		TaskKind:    task.Entry.Kind,
		DesiredHash: desiredHash,
		HashSchema:  ConvergeHashSchema - 1,
		Owner:       ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner},
		Status:      ConvergeSafetyStatusReconciled,
		RunID:       "missing-run",
	}
	class, err := classifyApplyTaskWithRecord(task, t.TempDir(), record, desiredHash)
	if class != ConvergeSafetyUnknown || err == nil {
		t.Fatalf("classification = %q, %v, want unknown fail-closed", class, err)
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		t.Fatalf("operator error should carry contextual safety guidance, got raw path error: %v", err)
	}
}
