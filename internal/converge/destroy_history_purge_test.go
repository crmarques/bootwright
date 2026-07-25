package converge

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func mustArchiveLedger(t *testing.T, runsDir, runID string, tasks []workflow.TaskLedgerEntry) {
	t.Helper()
	ledger := workflow.RunLedger{RunID: runID, Target: "destroy", Status: workflow.RunStatusOK, Tasks: tasks}
	if err := workflow.ArchiveRunLedger(runsDir, ledger); err != nil {
		t.Fatalf("archive ledger %s: %v", runID, err)
	}
}

func touchTaskDir(t *testing.T, runsDir, runID, taskID string) {
	t.Helper()
	path := workflow.TaskLogPath(runsDir, runID, taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("log\n"), 0o600); err != nil {
		t.Fatalf("write task log: %v", err)
	}
}

func touchClusterLog(t *testing.T, runsDir, runID, cluster string) {
	t.Helper()
	path := workflow.ApplyClusterLogPath(runsDir, runID, cluster)
	if err := os.WriteFile(path, []byte("log\n"), 0o600); err != nil {
		t.Fatalf("write cluster log: %v", err)
	}
}

func runDirExists(runsDir, runID string) bool {
	_, err := os.Stat(filepath.Join(runsDir, "history", runID))
	return err == nil
}

func TestPurgeRunHistoryForComponentsRemovesFullyMatchedRun(t *testing.T) {
	runsDir := t.TempDir()
	mustArchiveLedger(t, runsDir, "run-1", []workflow.TaskLedgerEntry{
		{ID: "clusters.ocp.install", Kind: "cluster-install", Cluster: "ocp"},
	})
	touchTaskDir(t, runsDir, "run-1", "clusters.ocp.install")
	touchClusterLog(t, runsDir, "run-1", "ocp")

	if err := purgeRunHistoryForComponents(runsDir, []string{"ocp"}, nil); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if runDirExists(runsDir, "run-1") {
		t.Fatal("a run whose entire task set matched the purge scope must be removed outright")
	}
}

func TestPurgeRunHistoryForComponentsKeepsMixedRunButPrunesMatchedTasks(t *testing.T) {
	runsDir := t.TempDir()
	mustArchiveLedger(t, runsDir, "run-1", []workflow.TaskLedgerEntry{
		{ID: "clusters.ocp.install", Kind: "cluster-install", Cluster: "ocp"},
		{ID: "clusters.other.install", Kind: "cluster-install", Cluster: "other"},
	})
	touchTaskDir(t, runsDir, "run-1", "clusters.ocp.install")
	touchTaskDir(t, runsDir, "run-1", "clusters.other.install")
	touchClusterLog(t, runsDir, "run-1", "ocp")
	touchClusterLog(t, runsDir, "run-1", "other")

	if err := purgeRunHistoryForComponents(runsDir, []string{"ocp"}, nil); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !runDirExists(runsDir, "run-1") {
		t.Fatal("a run that also covers a still-live component must keep its run directory (ledger, shared log)")
	}
	if _, err := os.Stat(filepath.Join(runsDir, "history", "run-1", "ledger.json")); err != nil {
		t.Fatalf("ledger.json must survive a mixed-scope purge: %v", err)
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", "clusters.ocp.install")); !os.IsNotExist(err) {
		t.Fatalf("purged component's task dir must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(workflow.ApplyClusterLogPath(runsDir, "run-1", "ocp")); !os.IsNotExist(err) {
		t.Fatalf("purged component's cluster log must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", "clusters.other.install")); err != nil {
		t.Fatalf("still-live component's task dir must survive: %v", err)
	}
	if _, err := os.Stat(workflow.ApplyClusterLogPath(runsDir, "run-1", "other")); err != nil {
		t.Fatalf("still-live component's cluster log must survive: %v", err)
	}
}

func TestPurgeRunHistoryForComponentsSkipsUnrelatedRuns(t *testing.T) {
	runsDir := t.TempDir()
	mustArchiveLedger(t, runsDir, "run-1", []workflow.TaskLedgerEntry{
		{ID: "clusters.other.install", Kind: "cluster-install", Cluster: "other"},
	})
	touchTaskDir(t, runsDir, "run-1", "clusters.other.install")

	if err := purgeRunHistoryForComponents(runsDir, []string{"ocp"}, nil); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !runDirExists(runsDir, "run-1") {
		t.Fatal("a run with no task in the purge scope must be left untouched")
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", "clusters.other.install")); err != nil {
		t.Fatalf("unrelated task dir must survive: %v", err)
	}
}

func TestPurgeRunHistoryForComponentsMatchesMachinesByNode(t *testing.T) {
	runsDir := t.TempDir()
	mustArchiveLedger(t, runsDir, "run-1", []workflow.TaskLedgerEntry{
		{ID: "infra.lab.ceph-0", Kind: "machine-infra", Node: "ceph-0"},
		{ID: "infra.lab.ceph-1", Kind: "machine-infra", Node: "ceph-1"},
	})
	touchTaskDir(t, runsDir, "run-1", "infra.lab.ceph-0")
	touchTaskDir(t, runsDir, "run-1", "infra.lab.ceph-1")

	if err := purgeRunHistoryForComponents(runsDir, nil, []string{"ceph-0"}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !runDirExists(runsDir, "run-1") {
		t.Fatal("run directory must survive: ceph-1's task is still out of scope")
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", "infra.lab.ceph-0")); !os.IsNotExist(err) {
		t.Fatalf("targeted machine's task dir must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", "infra.lab.ceph-1")); err != nil {
		t.Fatalf("out-of-scope machine's task dir must survive: %v", err)
	}
}

func TestPurgeClusterRuntimeDirRemovesWholeTree(t *testing.T) {
	clustersDir := t.TempDir()
	installerFile := filepath.Join(clustersDir, "ocp", "runtime", "installer", "install-config.yaml")
	if err := os.MkdirAll(filepath.Dir(installerFile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(installerFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := purgeClusterRuntimeDir(clustersDir, "ocp"); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clustersDir, "ocp")); !os.IsNotExist(err) {
		t.Fatalf("cluster runtime directory must be gone, stat err = %v", err)
	}
}

func TestResetConvergeRecordsAfterDestroyPurgeHistoryRemovesClusterRuntimeDir(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	st := destroyResetState(v1alpha1.StorageCephDistributionOSS)

	if err := workflow.SaveClusterInstallRecord(clustersDir, workflow.ClusterInstallRecord{
		Cluster:   "ocp",
		Status:    workflow.ClusterInstallStatusInstalled,
		Phase:     workflow.ClusterInstallPhaseComplete,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed install record: %v", err)
	}
	installerFile := filepath.Join(clustersDir, "ocp", "runtime", "installer", "install-config.yaml")
	if err := os.MkdirAll(filepath.Dir(installerFile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(installerFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustArchiveLedger(t, runsDir, "run-1", []workflow.TaskLedgerEntry{
		{ID: "clusters.ocp.install", Kind: "cluster-install", Cluster: "ocp"},
	})
	touchTaskDir(t, runsDir, "run-1", "clusters.ocp.install")

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, nil, nil, nil, true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if _, err := os.Stat(filepath.Join(clustersDir, "ocp")); !os.IsNotExist(err) {
		t.Fatalf("--purge-history must remove the whole cluster runtime directory, stat err = %v", err)
	}
	if runDirExists(runsDir, "run-1") {
		t.Fatal("--purge-history must remove run history whose entire task set belonged to the destroyed cluster")
	}
}

func TestResetConvergeRecordsAfterDestroyPurgeHistoryKeepsPartiallyDestroyedStorageHistory(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := twoCephClustersState()

	mustArchiveLedger(t, runsDir, "run-a", []workflow.TaskLedgerEntry{
		{ID: "storage.ceph-a.cluster", Kind: "storage-cluster", Cluster: "ceph-a"},
	})
	touchTaskDir(t, runsDir, "run-a", "storage.ceph-a.cluster")
	mustArchiveLedger(t, runsDir, "run-b", []workflow.TaskLedgerEntry{
		{ID: "storage.ceph-b.cluster", Kind: "storage-cluster", Cluster: "ceph-b"},
	})
	touchTaskDir(t, runsDir, "run-b", "storage.ceph-b.cluster")

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", ClustersScope, st, nil, []string{"ceph-a"}, nil, nil, true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if !runDirExists(runsDir, "run-a") {
		t.Fatal("history for a partially-destroyed (skip-unreachable) cluster must be kept for troubleshooting/retry")
	}
	if runDirExists(runsDir, "run-b") {
		t.Fatal("history for the fully-destroyed sibling cluster must be purged")
	}
}

func TestResetMachineConvergeRecordsAfterDestroyPurgeHistoryScopesToSelectedMachine(t *testing.T) {
	runsDir := t.TempDir()
	st := bareMetalCephDestroyState()

	mustArchiveLedger(t, runsDir, "run-1", []workflow.TaskLedgerEntry{
		{ID: "infra.lab.ceph-0", Kind: "machine-infra", Node: "ceph-0"},
		{ID: "infra.lab.ceph-1", Kind: "machine-infra", Node: "ceph-1"},
	})
	touchTaskDir(t, runsDir, "run-1", "infra.lab.ceph-0")
	touchTaskDir(t, runsDir, "run-1", "infra.lab.ceph-1")

	machineProvision := map[string]bool{"ceph-0": true}
	if problems := ResetMachineConvergeRecordsAfterDestroy(runsDir, st, machineProvision, nil, true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if !runDirExists(runsDir, "run-1") {
		t.Fatal("run directory must survive: ceph-1 was outside the --machines selection")
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", "infra.lab.ceph-0")); !os.IsNotExist(err) {
		t.Fatalf("selected machine's task dir must be purged, stat err = %v", err)
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", "infra.lab.ceph-1")); err != nil {
		t.Fatalf("unselected machine's task dir must survive: %v", err)
	}
}
