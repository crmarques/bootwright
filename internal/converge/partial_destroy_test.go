package converge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

func storageDestroyResultJSON(t *testing.T, cluster string, completed, skipped []string) string {
	t.Helper()
	zero := 0
	var nodes []workflow.StorageDestroyNodeResult
	for _, node := range completed {
		nodes = append(nodes, workflow.StorageDestroyNodeResult{
			Name: node, Host: "storage__" + cluster + "__" + node, Outcome: "completed",
			ProofVersion: "ceph-lvm-quiet-v2", ScanScope: "all-node-pvs", ScanDigest: strings.Repeat("0", 64),
			ScannedRows: &zero, OwnedSurvivors: &zero, LVMScanRC: &zero, CompletionRC: &zero,
		})
	}
	for _, node := range skipped {
		nodes = append(nodes, workflow.StorageDestroyNodeResult{
			Name: node, Host: "storage__" + cluster + "__" + node, Outcome: "skipped",
			AbsenceClass: "ssh-unreachable", Reason: node + ": connection timed out",
		})
	}
	data, err := json.Marshal(workflow.StorageDestroyResult{
		SchemaVersion: 1,
		Clusters: []workflow.StorageDestroyClusterResult{{
			Name:  cluster,
			FSID:  storageDestroyTestFSID(cluster),
			Nodes: nodes,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func storageDestroyTestFSID(cluster string) string {
	switch cluster {
	case "ceph-a":
		return "11111111-1111-1111-1111-111111111111"
	case "ceph-b":
		return "22222222-2222-2222-2222-222222222222"
	default:
		return "33333333-3333-3333-3333-333333333333"
	}
}

func writeStorageDestroyTaskResult(t *testing.T, runsDir, runID, taskID, content string) string {
	t.Helper()
	runLog := filepath.Join(runsDir, "history", runID, "bootwright.log")
	artifactDir := filepath.Join(runsDir, "history", runID, "tasks", taskID, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if content == "" {
		return runLog
	}
	if err := os.WriteFile(filepath.Join(artifactDir, workflow.StorageDestroyResultFileName), []byte(content), 0o600); err != nil {
		t.Fatalf("write result: %v", err)
	}
	return runLog
}

func writeStorageDestroyResult(t *testing.T, runsDir, runID, content string) string {
	t.Helper()
	return writeStorageDestroyTaskResult(t, runsDir, runID, workflow.DestroyStorageClustersTaskID, content)
}

func recordPartialStorageDestroy(ownershipDir, contextName, runLogPath string, expected map[string][]string, allowSkipped bool) (PartialStorageDestroy, error) {
	seedHosts := map[string]string{}
	for cluster, nodes := range expected {
		if len(nodes) > 0 {
			seedHosts[cluster] = "storage__" + cluster + "__" + nodes[0]
		}
	}
	return RecordPartialStorageDestroy(ownershipDir, contextName, runLogPath, expected, seedHosts, allowSkipped)
}

func TestRecordPartialStorageDestroyStampsOwnershipRecord(t *testing.T) {
	dir := t.TempDir()
	ownershipDir := filepath.Join(dir, "ownership")
	if err := ownership.SaveResource(ownershipDir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: "storage__ceph-a__ceph-02",
		Attributes: map[string]string{"seedHost": "storage__ceph-a__ceph-02", "fsid": "11111111-1111-1111-1111-111111111111"},
	}); err != nil {
		t.Fatalf("seed ownership record: %v", err)
	}
	runLog := writeStorageDestroyResult(t, filepath.Join(dir, "runs"), "destroy-20260101T000000Z",
		storageDestroyResultJSON(t, "ceph-a", nil, []string{"ceph-02"}))

	partial, err := recordPartialStorageDestroy(ownershipDir, "", runLog, map[string][]string{"ceph-a": {"ceph-02"}}, true)
	if err != nil {
		t.Fatalf("RecordPartialStorageDestroy: %v", err)
	}
	if !partial.Found || strings.Join(partial.Recorded, ",") != "ceph-a" || strings.Join(partial.Clusters, ",") != "ceph-a" {
		t.Fatalf("partial result = %+v", partial)
	}
	records, err := ownership.LoadContext(ownershipDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("ownership records = %d, want 1", len(records))
	}
	if got := records[0].Attributes[StorageDestroyStatusAttr]; got != StorageDestroyStatusPartial {
		t.Fatalf("destroyStatus = %q, want %q", got, StorageDestroyStatusPartial)
	}
	if got := records[0].Attributes[StorageDestroySkippedNodesAttr]; got != "ceph-02" {
		t.Fatalf("destroySkippedNodes = %q, want ceph-02", got)
	}
	byName, err := PartiallyDestroyedStorageClusters(ownershipDir, "")
	if err != nil || byName["ceph-a"] != "ceph-02" {
		t.Fatalf("partial map = %v, err = %v", byName, err)
	}
}

func TestRecordPartialStorageDestroyCollectsEveryPerClusterTask(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	runID := "destroy-20260101T000000Z"
	writeStorageDestroyTaskResult(t, runsDir, runID, workflow.DestroyStorageClustersTaskID+".ceph-a",
		storageDestroyResultJSON(t, "ceph-a", nil, []string{"ceph-a1"}))
	runLog := writeStorageDestroyTaskResult(t, runsDir, runID, workflow.DestroyStorageClustersTaskID+".ceph-b",
		storageDestroyResultJSON(t, "ceph-b", nil, []string{"ceph-b1"}))

	partial, err := recordPartialStorageDestroy(filepath.Join(dir, "ownership"), "", runLog,
		map[string][]string{"ceph-a": {"ceph-a1"}, "ceph-b": {"ceph-b1"}}, true)
	if err != nil {
		t.Fatalf("RecordPartialStorageDestroy: %v", err)
	}
	if !partial.Found || strings.Join(partial.Clusters, ",") != "ceph-a,ceph-b" {
		t.Fatalf("partial result = %+v", partial)
	}
	if partial.Skipped != "ceph-a1,ceph-b1" {
		t.Fatalf("skipped nodes = %q", partial.Skipped)
	}
}

func TestRecordPartialStorageDestroyMissingPerClusterReportFailsClosed(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	runID := "destroy-20260101T000000Z"
	writeStorageDestroyTaskResult(t, runsDir, runID, workflow.DestroyStorageClustersTaskID+".ceph-a",
		storageDestroyResultJSON(t, "ceph-a", []string{"ceph-a1"}, nil))
	runLog := writeStorageDestroyTaskResult(t, runsDir, runID, workflow.DestroyStorageClustersTaskID+".ceph-b", "")

	partial, err := recordPartialStorageDestroy(filepath.Join(dir, "ownership"), "", runLog,
		map[string][]string{"ceph-a": {"ceph-a1"}, "ceph-b": {"ceph-b1"}}, false)
	if err == nil || !strings.Contains(err.Error(), "no completion attestation") {
		t.Fatalf("partial = %+v, error = %v", partial, err)
	}
}

func TestRecordPartialStorageDestroyNoExpectedStorageIsClean(t *testing.T) {
	dir := t.TempDir()
	runLog := filepath.Join(dir, "runs", "history", "destroy-x", "bootwright.log")
	partial, err := recordPartialStorageDestroy(filepath.Join(dir, "ownership"), "", runLog, nil, false)
	if err != nil || partial.Found {
		t.Fatalf("partial = %+v, error = %v", partial, err)
	}
}

func TestRecordPartialStorageDestroyMissingExpectedResultFailsClosed(t *testing.T) {
	dir := t.TempDir()
	runLog := filepath.Join(dir, "runs", "history", "destroy-x", "bootwright.log")
	_, err := recordPartialStorageDestroy(filepath.Join(dir, "ownership"), "", runLog,
		map[string][]string{"ceph-a": {"ceph-a1"}}, false)
	if err == nil || !strings.Contains(err.Error(), "no completion attestation") {
		t.Fatalf("error = %v", err)
	}
}

func TestRecordPartialStorageDestroyUnrecordedClusterIsReportedSeparately(t *testing.T) {
	dir := t.TempDir()
	runLog := writeStorageDestroyResult(t, filepath.Join(dir, "runs"), "destroy-20260101T000000Z",
		storageDestroyResultJSON(t, "ceph-a", nil, []string{"ceph-02"}))
	partial, err := recordPartialStorageDestroy(filepath.Join(dir, "ownership"), "", runLog,
		map[string][]string{"ceph-a": {"ceph-02"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Recorded) != 0 || strings.Join(partial.Unrecorded, ",") != "ceph-a" {
		t.Fatalf("partial result = %+v", partial)
	}
}

func TestRecordPartialStorageDestroyNeverReleasesCompletedOwnership(t *testing.T) {
	dir := t.TempDir()
	ownershipDir := filepath.Join(dir, "ownership")
	if err := ownership.SaveResource(ownershipDir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: "storage__ceph-a__a1",
		Attributes: map[string]string{"seedHost": "storage__ceph-a__a1", "fsid": "11111111-1111-1111-1111-111111111111"},
	}); err != nil {
		t.Fatal(err)
	}
	runLog := writeStorageDestroyResult(t, filepath.Join(dir, "runs"), "destroy-failed",
		storageDestroyResultJSON(t, "ceph-a", []string{"a1"}, nil))
	partial, err := recordPartialStorageDestroy(ownershipDir, "", runLog, map[string][]string{"ceph-a": {"a1"}}, false)
	if err != nil {
		t.Fatalf("post-run aggregation: %v", err)
	}
	if !partial.Found || len(partial.Clusters) != 0 {
		t.Fatalf("partial = %+v", partial)
	}
	if records, err := ownership.LoadContext(ownershipDir, ""); err != nil || len(records) != 1 {
		t.Fatalf("post-run aggregation must not release completed ownership without the task-boundary finalizer, records=%v err=%v", records, err)
	}
	if err := FinalizeStorageDestroyCompletion(
		ownershipDir,
		"",
		runLog,
		map[string][]string{"ceph-a": {"a1"}},
		map[string]string{"ceph-a": "storage__ceph-a__a1"},
		false,
	); err == nil || !strings.Contains(err.Error(), "not fully released") {
		t.Fatalf("pre-release finalization error = %v", err)
	}
}

func TestRecordPartialStorageDestroyDoesNotReportAReferenceAsRecorded(t *testing.T) {
	dir := t.TempDir()
	ownershipDir := filepath.Join(dir, "ownership")
	if err := ownership.SaveResource(ownershipDir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Context: "ctx", Role: ownership.RoleReference,
		Attributes: map[string]string{StorageDestroyStatusAttr: StorageDestroyStatusPartial, StorageDestroySkippedNodesAttr: "a1"},
	}); err != nil {
		t.Fatal(err)
	}
	runLog := writeStorageDestroyResult(t, filepath.Join(dir, "runs"), "destroy-partial",
		storageDestroyResultJSON(t, "ceph-a", nil, []string{"a1"}))
	partial, err := recordPartialStorageDestroy(ownershipDir, "ctx", runLog, map[string][]string{"ceph-a": {"a1"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Recorded) != 0 || strings.Join(partial.Unrecorded, ",") != "ceph-a" {
		t.Fatalf("partial = %+v", partial)
	}
	if byName, err := PartiallyDestroyedStorageClusters(ownershipDir, "ctx"); err != nil || len(byName) != 0 {
		t.Fatalf("reference-only status = %v err=%v", byName, err)
	}
}

func TestRecordPartialStorageDestroyRefusesAForeignOwner(t *testing.T) {
	dir := t.TempDir()
	ownershipDir := filepath.Join(dir, "ownership")
	if err := ownership.SaveResource(ownershipDir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Context: "ctx", Owner: "foreign",
	}); err != nil {
		t.Fatal(err)
	}
	runLog := writeStorageDestroyResult(t, filepath.Join(dir, "runs"), "destroy-partial",
		storageDestroyResultJSON(t, "ceph-a", nil, []string{"a1"}))
	_, err := recordPartialStorageDestroy(ownershipDir, "ctx", runLog, map[string][]string{"ceph-a": {"a1"}}, true)
	if err == nil || !strings.Contains(err.Error(), "contradicts") {
		t.Fatalf("error = %v", err)
	}
	if records, loadErr := ownership.LoadContext(ownershipDir, "ctx"); loadErr != nil || len(records) != 1 {
		t.Fatalf("foreign owner must remain, records=%v err=%v", records, loadErr)
	}
}
