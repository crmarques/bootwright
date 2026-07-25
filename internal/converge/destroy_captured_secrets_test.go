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
