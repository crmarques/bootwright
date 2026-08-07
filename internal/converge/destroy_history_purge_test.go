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

func touchTaskDir(t *testing.T, runsDir, runID, taskID string, cluster ...string) {
	t.Helper()
	entry := workflow.TaskLedgerEntry{ID: taskID}
	if len(cluster) > 0 {
		entry.Cluster = cluster[0]
	}
	path := workflow.TaskLogPath(runsDir, runID, entry)
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir cluster log dir: %v", err)
	}
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
	touchTaskDir(t, runsDir, "run-1", "clusters.ocp.install", "ocp")
	touchClusterLog(t, runsDir, "run-1", "ocp")

	if err := purgeRunHistoryForComponents(runsDir, []string{"ocp"}, nil, ""); err != nil {
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
	touchTaskDir(t, runsDir, "run-1", "clusters.ocp.install", "ocp")
	touchTaskDir(t, runsDir, "run-1", "clusters.other.install", "other")
	touchClusterLog(t, runsDir, "run-1", "ocp")
	touchClusterLog(t, runsDir, "run-1", "other")

	if err := purgeRunHistoryForComponents(runsDir, []string{"ocp"}, nil, ""); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !runDirExists(runsDir, "run-1") {
		t.Fatal("a run that also covers a still-live component must keep its run directory (ledger, shared log)")
	}
	if _, err := os.Stat(filepath.Join(runsDir, "history", "run-1", "ledger.json")); err != nil {
		t.Fatalf("ledger.json must survive a mixed-scope purge: %v", err)
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", workflow.TaskLedgerEntry{ID: "clusters.ocp.install", Cluster: "ocp"})); !os.IsNotExist(err) {
		t.Fatalf("purged component's task dir must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(workflow.ApplyClusterLogPath(runsDir, "run-1", "ocp")); !os.IsNotExist(err) {
		t.Fatalf("purged component's cluster log must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", workflow.TaskLedgerEntry{ID: "clusters.other.install", Cluster: "other"})); err != nil {
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
	touchTaskDir(t, runsDir, "run-1", "clusters.other.install", "other")

	if err := purgeRunHistoryForComponents(runsDir, []string{"ocp"}, nil, ""); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !runDirExists(runsDir, "run-1") {
		t.Fatal("a run with no task in the purge scope must be left untouched")
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", workflow.TaskLedgerEntry{ID: "clusters.other.install", Cluster: "other"})); err != nil {
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

	if err := purgeRunHistoryForComponents(runsDir, nil, []string{"ceph-0"}, ""); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !runDirExists(runsDir, "run-1") {
		t.Fatal("run directory must survive: ceph-1's task is still out of scope")
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", workflow.TaskLedgerEntry{ID: "infra.lab.ceph-0"})); !os.IsNotExist(err) {
		t.Fatalf("targeted machine's task dir must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", workflow.TaskLedgerEntry{ID: "infra.lab.ceph-1"})); err != nil {
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
	if err := purgeClusterRuntimeDir(clustersDir, "ocp", false); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clustersDir, "ocp")); !os.IsNotExist(err) {
		t.Fatalf("cluster runtime directory must be gone, stat err = %v", err)
	}
}

func TestPurgeClusterRuntimeDirKeepsStandingMachineState(t *testing.T) {
	clustersDir := t.TempDir()
	installerFile := filepath.Join(clustersDir, "ocp", "runtime", "installer", "install-config.yaml")
	if err := os.MkdirAll(filepath.Dir(installerFile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(installerFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	machineState := filepath.Join(workflow.ClusterProviderStateDir(clustersDir, "ocp"), "libvirt", "machines", "node-0", "domain.xml")
	if err := os.MkdirAll(filepath.Dir(machineState), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(machineState, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := purgeClusterRuntimeDir(clustersDir, "ocp", true); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(machineState); err != nil {
		t.Fatalf("a purge that does not tear the machine layer must keep provider state: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clustersDir, "ocp", "runtime", "installer")); !os.IsNotExist(err) {
		t.Fatalf("cluster history must still be purged, stat err = %v", err)
	}
}

func TestPruneEmptyClusterStateDirsRemovesEmptiedSkeleton(t *testing.T) {
	clustersDir := t.TempDir()
	emptied := filepath.Join(workflow.ClusterProviderStateDir(clustersDir, "ceph"), "baremetal")
	if err := os.MkdirAll(emptied, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(workflow.ClusterSecretsDir(clustersDir, "ceph"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := pruneEmptyClusterStateDirs(clustersDir, "ceph"); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clustersDir, "ceph")); !os.IsNotExist(err) {
		t.Fatalf("a fully emptied cluster state tree must be removed, stat err = %v", err)
	}
}

func TestPruneEmptyClusterStateDirsKeepsDirectoriesThatStillHoldState(t *testing.T) {
	clustersDir := t.TempDir()
	kept := filepath.Join(workflow.ClusterSecretsDir(clustersDir, "ceph"), "dashboard-password")
	if err := os.MkdirAll(filepath.Dir(kept), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(kept, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	emptied := filepath.Join(workflow.ClusterProviderStateDir(clustersDir, "ceph"), "baremetal")
	if err := os.MkdirAll(emptied, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := pruneEmptyClusterStateDirs(clustersDir, "ceph"); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("retained material must survive the prune: %v", err)
	}
	if _, err := os.Stat(emptied); !os.IsNotExist(err) {
		t.Fatalf("emptied provider-state tree must be pruned, stat err = %v", err)
	}
	if _, err := os.Stat(workflow.ClusterRuntimeDir(clustersDir, "ceph")); !os.IsNotExist(err) {
		t.Fatalf("emptied runtime tree must be pruned, stat err = %v", err)
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
	touchTaskDir(t, runsDir, "run-1", "clusters.ocp.install", "ocp")

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, nil, nil, nil, "", true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if _, err := os.Stat(filepath.Join(clustersDir, "ocp")); !os.IsNotExist(err) {
		t.Fatalf("--purge-history must remove the whole cluster runtime directory, stat err = %v", err)
	}
	if runDirExists(runsDir, "run-1") {
		t.Fatal("--purge-history must remove run history whose entire task set belonged to the destroyed cluster")
	}
}

func TestResetConvergeRecordsAfterDestroyPurgeHistoryRemovesStorageClusterStateTree(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()

	for _, dir := range []string{
		filepath.Join(workflow.ClusterProviderStateDir(clustersDir, "ceph-bm"), "baremetal"),
		workflow.ClusterSecretsDir(clustersDir, "ceph-bm"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, nil, nil, nil, "", true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if _, err := os.Stat(filepath.Join(clustersDir, "ceph-bm")); !os.IsNotExist(err) {
		t.Fatalf("--purge-history must remove a destroyed StorageCluster's state tree, not just a ContainerCluster's, stat err = %v", err)
	}
}

func TestResetConvergeRecordsAfterDestroyPrunesEmptiedStorageClusterStateTree(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()

	for _, dir := range []string{
		filepath.Join(workflow.ClusterProviderStateDir(clustersDir, "ceph-bm"), "baremetal"),
		workflow.ClusterSecretsDir(clustersDir, "ceph-bm"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, nil, nil, nil, "", false, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if _, err := os.Stat(filepath.Join(clustersDir, "ceph-bm")); !os.IsNotExist(err) {
		t.Fatalf("a destroy that emptied a cluster's state tree must leave no directory skeleton behind, stat err = %v", err)
	}
}

func TestResetConvergeRecordsAfterDestroyPurgeHistoryKeepsStandingMachineState(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()

	machineState := filepath.Join(workflow.ClusterProviderStateDir(clustersDir, "ceph-bm"), "baremetal", "ceph-0.yml")
	if err := os.MkdirAll(filepath.Dir(machineState), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(machineState, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", ClustersScope, st, nil, nil, nil, nil, "", true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if _, err := os.Stat(machineState); err != nil {
		t.Fatalf("a clusters-stage --purge-history must not delete the standing machine layer's provider state: %v", err)
	}
}

func TestResetConvergeRecordsAfterDestroyPurgeHistoryKeepsPartiallyDestroyedStorageStateTree(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := bareMetalCephDestroyState()

	secretsDir := workflow.ClusterSecretsDir(clustersDir, "ceph-bm")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, []string{"ceph-bm"}, nil, nil, "", true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if _, err := os.Stat(secretsDir); err != nil {
		t.Fatalf("a partially-destroyed cluster kept for retry must keep its state tree: %v", err)
	}
}

func TestResetConvergeRecordsAfterDestroyPurgeHistoryKeepsPartiallyDestroyedStorageHistory(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := twoCephClustersState()

	mustArchiveLedger(t, runsDir, "run-a", []workflow.TaskLedgerEntry{
		{ID: "storage.ceph-a.cluster", Kind: "storage-cluster", Cluster: "ceph-a"},
	})
	touchTaskDir(t, runsDir, "run-a", "storage.ceph-a.cluster", "ceph-a")
	mustArchiveLedger(t, runsDir, "run-b", []workflow.TaskLedgerEntry{
		{ID: "storage.ceph-b.cluster", Kind: "storage-cluster", Cluster: "ceph-b"},
	})
	touchTaskDir(t, runsDir, "run-b", "storage.ceph-b.cluster", "ceph-b")

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", ClustersScope, st, nil, []string{"ceph-a"}, nil, nil, "", true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if !runDirExists(runsDir, "run-a") {
		t.Fatal("history for a partially-destroyed (skip-unreachable) cluster must be kept for troubleshooting/retry")
	}
	if runDirExists(runsDir, "run-b") {
		t.Fatal("history for the fully-destroyed sibling cluster must be purged")
	}
}

func TestPurgeRunHistoryForComponentsMatchesDestroyRunsByResourceKeys(t *testing.T) {
	runsDir := t.TempDir()
	mustArchiveLedger(t, runsDir, "destroy-old", []workflow.TaskLedgerEntry{
		{ID: "destroy.storage-clusters.ceph", Kind: workflow.DestroyTaskKindStorageCluster, ResourceKeys: []string{"ceph", workflow.DestroyMachineResourceKeyPrefix + "ceph-0"}},
	})
	touchTaskDir(t, runsDir, "destroy-old", "destroy.storage-clusters.ceph")

	if err := purgeRunHistoryForComponents(runsDir, []string{"ceph"}, nil, ""); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if runDirExists(runsDir, "destroy-old") {
		t.Fatal("a prior destroy run that recorded the cluster only through resource keys must be purged with it")
	}
}

func TestPurgeRunHistoryForComponentsSkipsTheCurrentDestroyRun(t *testing.T) {
	runsDir := t.TempDir()
	mustArchiveLedger(t, runsDir, "destroy-current", []workflow.TaskLedgerEntry{
		{ID: "destroy.storage-clusters.ceph", Kind: workflow.DestroyTaskKindStorageCluster, ResourceKeys: []string{"ceph"}},
	})
	touchTaskDir(t, runsDir, "destroy-current", "destroy.storage-clusters.ceph")

	if err := purgeRunHistoryForComponents(runsDir, []string{"ceph"}, nil, "destroy-current"); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !runDirExists(runsDir, "destroy-current") {
		t.Fatal("the purging destroy run must never purge its own record")
	}
}

func TestPurgeRunHistoryForComponentsKeepsDestroyTaskSpanningUnselectedMachines(t *testing.T) {
	runsDir := t.TempDir()
	mustArchiveLedger(t, runsDir, "destroy-old", []workflow.TaskLedgerEntry{
		{ID: "destroy.machine-infra", Kind: workflow.DestroyTaskKindMachineInfra, ResourceKeys: []string{workflow.DestroyMachineResourceKeyPrefix + "ceph-0", workflow.DestroyMachineResourceKeyPrefix + "ceph-1"}},
	})
	touchTaskDir(t, runsDir, "destroy-old", "destroy.machine-infra")

	if err := purgeRunHistoryForComponents(runsDir, nil, []string{"ceph-0"}, ""); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !runDirExists(runsDir, "destroy-old") {
		t.Fatal("a destroy task that also covered an unselected machine must keep its record")
	}

	if err := purgeRunHistoryForComponents(runsDir, nil, []string{"ceph-0", "ceph-1"}, ""); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if runDirExists(runsDir, "destroy-old") {
		t.Fatal("a destroy task whose machines were all selected must be purged")
	}
}

func TestPurgeRunHistoryForComponentsIgnoresApplyTaskResourceKeys(t *testing.T) {
	runsDir := t.TempDir()
	mustArchiveLedger(t, runsDir, "run-1", []workflow.TaskLedgerEntry{
		{ID: "addon.gitops", Kind: "cluster-addon", Cluster: "other", ResourceKeys: []string{"ceph"}},
	})
	touchTaskDir(t, runsDir, "run-1", "addon.gitops", "other")

	if err := purgeRunHistoryForComponents(runsDir, []string{"ceph"}, nil, ""); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if !runDirExists(runsDir, "run-1") {
		t.Fatal("resource keys on non-destroy tasks must not put a live component's run in purge scope")
	}
}

func TestResetConvergeRecordsAfterDestroyFullScopePurgeSweepsAllRunHistory(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := destroyResetState(v1alpha1.StorageCephDistributionOSS)

	mustArchiveLedger(t, runsDir, "run-fabric", []workflow.TaskLedgerEntry{
		{ID: "provider.kvm-host", Kind: "provider", Host: "kvm-host"},
		{ID: "clusters.ocp.install", Kind: "cluster-install", Cluster: "ocp"},
	})
	touchTaskDir(t, runsDir, "run-fabric", "provider.kvm-host")
	mustArchiveLedger(t, runsDir, "destroy-old", []workflow.TaskLedgerEntry{
		{ID: "destroy.storage-clusters", Kind: workflow.DestroyTaskKindStorageCluster, ResourceKeys: []string{"ocp"}},
	})
	touchTaskDir(t, runsDir, "destroy-old", "destroy.storage-clusters")
	touchTaskDir(t, runsDir, "run-orphan", "clusters.ocp.install", "ocp")
	mustArchiveLedger(t, runsDir, "destroy-current", []workflow.TaskLedgerEntry{
		{ID: "destroy.cluster-runtime", Kind: workflow.DestroyTaskKindContainerClusterRuntime, ResourceKeys: []string{"ocp"}},
	})

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, nil, nil, nil, "destroy-current", true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	for _, runID := range []string{"run-fabric", "destroy-old", "run-orphan"} {
		if runDirExists(runsDir, runID) {
			t.Fatalf("a fully successful unscoped --purge-history destroy must sweep run history entry %s", runID)
		}
	}
	if !runDirExists(runsDir, "destroy-current") {
		t.Fatal("the purging destroy run's own record must survive the full-context sweep")
	}
}

func TestResetConvergeRecordsAfterDestroySkippedNodesDisableHistorySweep(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := destroyResetState(v1alpha1.StorageCephDistributionOSS)

	mustArchiveLedger(t, runsDir, "run-fabric", []workflow.TaskLedgerEntry{
		{ID: "provider.kvm-host", Kind: "provider", Host: "kvm-host"},
	})
	touchTaskDir(t, runsDir, "run-fabric", "provider.kvm-host")

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, nil, nil, nil, "destroy-current", true, true); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if !runDirExists(runsDir, "run-fabric") {
		t.Fatal("a teardown authorized with --authorize unreachable-nodes proves no per-node completion outside a managed storage cluster, so --purge-history must keep the context's run history: it is what a retry and a diagnosis read after nodes were skipped")
	}
}

func TestResetMachineConvergeRecordsAfterDestroySkippedNodesKeepHistory(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := destroyResetState(v1alpha1.StorageCephDistributionOSS)

	mustArchiveLedger(t, runsDir, "run-machines", []workflow.TaskLedgerEntry{
		{ID: "machines.ceph-0.substrate", Kind: "machine-infra", Node: "ceph-0"},
	})
	touchTaskDir(t, runsDir, "run-machines", "machines.ceph-0.substrate")

	problems := ResetMachineConvergeRecordsAfterDestroy(runsDir, clustersDir, st, map[string]bool{"ceph-0": true}, nil, "destroy-current", true, true)
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if !runDirExists(runsDir, "run-machines") {
		t.Fatal("a machine teardown authorized with --authorize unreachable-nodes has no per-node completion proof, so --purge-history must keep its history")
	}
}

func TestResetConvergeRecordsAfterDestroyFullScopePartialDisablesHistorySweep(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := twoCephClustersState()

	mustArchiveLedger(t, runsDir, "run-a", []workflow.TaskLedgerEntry{
		{ID: "storage.ceph-a.cluster", Kind: "storage-cluster", Cluster: "ceph-a"},
	})
	touchTaskDir(t, runsDir, "run-a", "storage.ceph-a.cluster", "ceph-a")
	mustArchiveLedger(t, runsDir, "run-fabric", []workflow.TaskLedgerEntry{
		{ID: "provider.kvm-host", Kind: "provider", Host: "kvm-host"},
	})
	touchTaskDir(t, runsDir, "run-fabric", "provider.kvm-host")

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", AllScope, st, nil, []string{"ceph-a"}, nil, nil, "", true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if !runDirExists(runsDir, "run-a") {
		t.Fatal("a partially-destroyed cluster's history must survive even an unscoped --purge-history destroy")
	}
	if !runDirExists(runsDir, "run-fabric") {
		t.Fatal("the full-context sweep must stay off while any cluster is left partially destroyed")
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
	if problems := ResetMachineConvergeRecordsAfterDestroy(runsDir, t.TempDir(), st, machineProvision, nil, "", true, false); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	if !runDirExists(runsDir, "run-1") {
		t.Fatal("run directory must survive: ceph-1 was outside the --machines selection")
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", workflow.TaskLedgerEntry{ID: "infra.lab.ceph-0"})); !os.IsNotExist(err) {
		t.Fatalf("selected machine's task dir must be purged, stat err = %v", err)
	}
	if _, err := os.Stat(workflow.TaskLogPath(runsDir, "run-1", workflow.TaskLedgerEntry{ID: "infra.lab.ceph-1"})); err != nil {
		t.Fatalf("unselected machine's task dir must survive: %v", err)
	}
}
