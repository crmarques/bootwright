package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReconcileApplyClusterInstallStateOverride(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	now := time.Now()

	installSkipped := func(out []ApplyTask) (skipped, total int) {
		for _, task := range out {
			if task.Entry.Cluster != cluster || !isClusterInstallTaskKind(task.Entry.Kind) {
				continue
			}
			total++
			if task.Entry.Status == TaskStatusSkipped {
				skipped++
			}
		}
		return skipped, total
	}
	writeKubeconfig := func(t *testing.T, clustersDir string) {
		t.Helper()
		kubeconfig := clusterKubeconfigPath(clustersDir, cluster)
		if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o700); err != nil {
			t.Fatalf("mkdir kubeconfig dir: %v", err)
		}
		if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
			t.Fatalf("write kubeconfig: %v", err)
		}
	}
	reconcile := func(t *testing.T, hash string, available bool) []ApplyTask {
		t.Helper()
		dir := t.TempDir()
		clustersDir := filepath.Join(dir, "clusters")
		secretsDir := writeWorkflowInstallerSecrets(t, dir)
		if hash == "" {
			var err error
			if hash, err = clusterInstallDesiredHashForContext("test", state, cluster, secretsDir); err != nil {
				t.Fatalf("clusterInstallDesiredHash: %v", err)
			}
		}
		if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
			Cluster: cluster, DesiredHash: hash, HashSchema: ConvergeHashSchema,
			Status: ClusterInstallStatusInstalled, Phase: ClusterInstallPhaseComplete,
			UpdatedAt: now.UTC(),
		}); err != nil {
			t.Fatalf("SaveClusterInstallRecord: %v", err)
		}
		writeKubeconfig(t, clustersDir)
		out, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", secretsDir, "run", state, tasks, ApplyModeOverride, &fakeClusterAvailabilityChecker{available: available}, now)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		return out
	}

	t.Run("healthy match is not rebuilt", func(t *testing.T) {
		skipped, total := installSkipped(reconcile(t, "", true))
		if total == 0 || skipped != total {
			t.Fatalf("override over a healthy match must skip all %d install tasks, skipped %d", total, skipped)
		}
	})
	t.Run("drift is rebuilt", func(t *testing.T) {
		skipped, total := installSkipped(reconcile(t, "sha256:stale", true))
		if total == 0 || skipped != 0 {
			t.Fatalf("override over a drifted cluster must rebuild (skip 0 of %d install tasks), skipped %d", total, skipped)
		}
	})
	t.Run("match but not available is rebuilt", func(t *testing.T) {
		skipped, total := installSkipped(reconcile(t, "", false))
		if total == 0 || skipped != 0 {
			t.Fatalf("override over an unreachable installed cluster must rebuild (skip 0 of %d), skipped %d", total, skipped)
		}
	})
}
