package converge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	secretstore "github.com/crmarques/bootwright/internal/secrets"
)

func TestResetConvergeRecordsRemovesStorageClusterDashboardPassword(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := twoCephClustersState()

	secretsDir := workflow.ClusterSecretsDir(clustersDir, "ceph-a")
	store := secretstore.NewContextStore("test", secretsDir)
	if err := store.Write(secretstore.MaterialKey{Name: "dashboard-password", Role: secretstore.MaterialPrimary}, []byte("hunter2\n")); err != nil {
		t.Fatalf("seed dashboard-password: %v", err)
	}
	path := filepath.Join(secretsDir, "dashboard-password")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("precondition: dashboard-password must exist, %v", err)
	}

	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", ClustersScope, st, nil, nil, nil, nil, false, false); len(problems) != 0 {
		t.Fatalf("ResetConvergeRecordsAfterDestroy: %v", problems)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("destroying StorageCluster/ceph-a must remove its captured dashboard-password, stat err=%v", err)
	}
}

func TestResetConvergeRecordsKeepsCapturedSecretsOfTheClusterThatFailed(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	st := twoCephClustersState()

	seed := func(cluster string) string {
		t.Helper()
		secretsDir := workflow.ClusterSecretsDir(clustersDir, cluster)
		store := secretstore.NewContextStore("test", secretsDir)
		if err := store.Write(secretstore.MaterialKey{Name: "dashboard-password", Role: secretstore.MaterialPrimary}, []byte("hunter2\n")); err != nil {
			t.Fatalf("seed dashboard-password for %s: %v", cluster, err)
		}
		return filepath.Join(secretsDir, "dashboard-password")
	}
	pathA := seed("ceph-a")
	pathB := seed("ceph-b")

	succeeded := workflow.SucceededDestroyTaskKinds(workflow.RunLedger{Tasks: []workflow.TaskLedgerEntry{
		{ID: "destroy.storage-clusters.ceph-a", Kind: workflow.DestroyTaskKindStorageCluster, ResourceKeys: []string{"ceph-a"}, Status: workflow.TaskStatusOK},
		{ID: "destroy.storage-clusters.ceph-b", Kind: workflow.DestroyTaskKindStorageCluster, ResourceKeys: []string{"ceph-b"}, Status: workflow.TaskStatusFailed},
	}})
	if problems := ResetConvergeRecordsAfterDestroy(runsDir, clustersDir, "test", ClustersScope, st, nil, nil, nil, succeeded, false, false); len(problems) != 0 {
		t.Fatalf("ResetConvergeRecordsAfterDestroy: %v", problems)
	}

	if _, err := os.Stat(pathA); !os.IsNotExist(err) {
		t.Fatalf("ceph-a tore down cleanly, so its captured dashboard-password must go, stat err=%v", err)
	}
	if _, err := os.Stat(pathB); err != nil {
		t.Fatalf("ceph-b is still standing after its teardown failed: dropping its dashboard credential leaves the live cluster unmanageable, stat err=%v", err)
	}
}
