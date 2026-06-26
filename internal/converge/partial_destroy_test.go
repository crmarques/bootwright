package converge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

func writeStorageDestroyResult(t *testing.T, runsDir, runID, content string) string {
	t.Helper()
	runLog := filepath.Join(runsDir, "history", runID, "bootwright.log")
	artifactDir := filepath.Join(runsDir, "history", runID, "tasks", workflow.DestroyStorageClustersTaskID, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "storage-destroy-result.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("write result: %v", err)
	}
	return runLog
}

func TestRecordPartialStorageDestroyStampsOwnershipRecord(t *testing.T) {
	dir := t.TempDir()
	ownershipDir := filepath.Join(dir, "ownership")
	runsDir := filepath.Join(dir, "runs")

	if err := ownership.SaveResource(ownershipDir, ownership.ResourceRecord{
		Kind:    string(ownership.KindStorageCluster),
		Name:    "ceph-a",
		Cluster: "ceph-a",
	}); err != nil {
		t.Fatalf("seed ownership record: %v", err)
	}

	runLog := writeStorageDestroyResult(t, runsDir, "destroy-20260101T000000Z",
		`{"partialClusters":["ceph-a"],"skippedNodes":["ceph-02"],"skippedHosts":["storage__ceph-a__ceph-02"]}`)

	partial, err := RecordPartialStorageDestroy(ownershipDir, "", runLog)
	if err != nil {
		t.Fatalf("RecordPartialStorageDestroy: %v", err)
	}
	if len(partial) != 1 || partial[0] != "ceph-a" {
		t.Fatalf("partial clusters = %v, want [ceph-a]", partial)
	}

	records, err := ownership.LoadContext(ownershipDir, "")
	if err != nil {
		t.Fatalf("load ownership: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("ownership records = %d, want 1 (record must be kept, not removed)", len(records))
	}
	if got := records[0].Attributes[StorageDestroyStatusAttr]; got != StorageDestroyStatusPartial {
		t.Fatalf("record destroyStatus = %q, want %q", got, StorageDestroyStatusPartial)
	}
	if got := records[0].Attributes[StorageDestroySkippedNodesAttr]; got != "ceph-02" {
		t.Fatalf("record skipped nodes = %q, want ceph-02", got)
	}

	byName, err := PartiallyDestroyedStorageClusters(ownershipDir, "")
	if err != nil {
		t.Fatalf("PartiallyDestroyedStorageClusters: %v", err)
	}
	if byName["ceph-a"] != "ceph-02" {
		t.Fatalf("partial map = %v, want ceph-a->ceph-02", byName)
	}
}

func TestRecordPartialStorageDestroyNoResultIsClean(t *testing.T) {
	dir := t.TempDir()
	runLog := filepath.Join(dir, "runs", "history", "destroy-x", "bootwright.log")
	partial, err := RecordPartialStorageDestroy(filepath.Join(dir, "ownership"), "", runLog)
	if err != nil {
		t.Fatalf("missing result must be clean, got err %v", err)
	}
	if partial != nil {
		t.Fatalf("missing result must report no partial clusters, got %v", partial)
	}
}
