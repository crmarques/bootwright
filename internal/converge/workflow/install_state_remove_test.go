package workflow

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoveClusterInstallStateClearsControllerRecords(t *testing.T) {
	clustersDir := t.TempDir()
	cluster := "demo"
	now := time.Unix(0, 0).UTC()

	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
		Cluster: cluster, Status: ClusterInstallStatusInstalled,
		Phase: ClusterInstallPhaseComplete, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed install record: %v", err)
	}
	if err := SaveClusterConnectionRecord(clustersDir, ClusterConnectionRecord{
		Cluster: cluster, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed connection record: %v", err)
	}
	kubeconfig := clusterKubeconfigPath(clustersDir, cluster)
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o700); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("seed kubeconfig: %v", err)
	}

	if _, found, err := LoadClusterInstallRecord(clustersDir, cluster); err != nil || !found {
		t.Fatalf("precondition: install record should exist, found=%v err=%v", found, err)
	}

	if err := RemoveClusterInstallState(clustersDir, cluster); err != nil {
		t.Fatalf("RemoveClusterInstallState: %v", err)
	}

	if _, found, err := LoadClusterInstallRecord(clustersDir, cluster); err != nil || found {
		t.Fatalf("install record must be gone after removal, found=%v err=%v", found, err)
	}
	for _, path := range []string{kubeconfig, ClusterConnectionPath(clustersDir, cluster)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s must be removed, stat err=%v", path, err)
		}
	}

	if err := RemoveClusterInstallState(clustersDir, cluster); err != nil {
		t.Fatalf("removing absent state must be a no-op, got %v", err)
	}
}

func TestContainerInstallClusterNames(t *testing.T) {
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{ID: "iso.a", Kind: ApplyTaskKindClusterISO, Cluster: "a"}},
		{Entry: TaskLedgerEntry{ID: "wait.a", Kind: ApplyTaskKindInstallWait, Cluster: "a"}},
		{Entry: TaskLedgerEntry{ID: "boot.b", Kind: ApplyTaskKindNodeBoot, Cluster: "b"}},
		{Entry: TaskLedgerEntry{ID: "storage.c", Kind: ApplyTaskKindStorageCluster, Cluster: "c"}},
		{Entry: TaskLedgerEntry{ID: "prov.h", Kind: ApplyTaskKindProvider, Cluster: ""}},
	}
	got := ContainerInstallClusterNames(tasks)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("expected [a b] (container-install clusters only, sorted), got %v", got)
	}
}
