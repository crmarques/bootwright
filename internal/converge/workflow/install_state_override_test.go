package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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
		writeEncryptedClusterKubeconfig(t, clustersDir, cluster)
	}
	reconcile := func(t *testing.T, hash string, available bool, acked []string) []ApplyTask {
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
		out, _, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", secretsDir, "run", state, tasks, ApplyModeOverride, acked, &fakeClusterAvailabilityChecker{available: available}, now)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		return out
	}

	t.Run("healthy match is not rebuilt", func(t *testing.T) {
		skipped, total := installSkipped(reconcile(t, "", true, nil))
		if total == 0 || skipped != total {
			t.Fatalf("override over a healthy match must skip all %d install tasks, skipped %d", total, skipped)
		}
	})
	t.Run("acked drift is rebuilt", func(t *testing.T) {
		skipped, total := installSkipped(reconcile(t, "sha256:stale", true, []string{cluster}))
		if total == 0 || skipped != 0 {
			t.Fatalf("override over a drifted cluster must rebuild (skip 0 of %d install tasks), skipped %d", total, skipped)
		}
	})
	t.Run("acked match but not available is rebuilt", func(t *testing.T) {
		skipped, total := installSkipped(reconcile(t, "", false, []string{cluster}))
		if total == 0 || skipped != 0 {
			t.Fatalf("override over an unreachable installed cluster must rebuild (skip 0 of %d), skipped %d", total, skipped)
		}
	})
	t.Run("unacked drift fails closed", func(t *testing.T) {
		dir := t.TempDir()
		clustersDir := filepath.Join(dir, "clusters")
		secretsDir := writeWorkflowInstallerSecrets(t, dir)
		if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
			Cluster: cluster, DesiredHash: "sha256:stale", HashSchema: ConvergeHashSchema,
			Status: ClusterInstallStatusInstalled, Phase: ClusterInstallPhaseComplete,
			UpdatedAt: now.UTC(),
		}); err != nil {
			t.Fatalf("SaveClusterInstallRecord: %v", err)
		}
		writeKubeconfig(t, clustersDir)
		_, _, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", secretsDir, "run", state, tasks, ApplyModeOverride, nil, &fakeClusterAvailabilityChecker{available: true}, now)
		if err == nil || !strings.Contains(err.Error(), "--confirm-data-loss") || !strings.Contains(err.Error(), "was not acknowledged") {
			t.Fatalf("unacked drifted reinstall must fail closed naming --confirm-data-loss, got: %v", err)
		}
	})
	t.Run("unacked not available fails closed", func(t *testing.T) {
		dir := t.TempDir()
		clustersDir := filepath.Join(dir, "clusters")
		secretsDir := writeWorkflowInstallerSecrets(t, dir)
		hash, err := clusterInstallDesiredHashForContext("test", state, cluster, secretsDir)
		if err != nil {
			t.Fatalf("clusterInstallDesiredHash: %v", err)
		}
		if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
			Cluster: cluster, DesiredHash: hash, HashSchema: ConvergeHashSchema,
			Status: ClusterInstallStatusInstalled, Phase: ClusterInstallPhaseComplete,
			UpdatedAt: now.UTC(),
		}); err != nil {
			t.Fatalf("SaveClusterInstallRecord: %v", err)
		}
		writeKubeconfig(t, clustersDir)
		_, _, err = ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", secretsDir, "run", state, tasks, ApplyModeOverride, nil, &fakeClusterAvailabilityChecker{available: false}, now)
		if err == nil || !strings.Contains(err.Error(), "--confirm-data-loss") {
			t.Fatalf("unacked unavailable reinstall must fail closed naming --confirm-data-loss, got: %v", err)
		}
	})
}

func TestOverrideRebuildInstalledClustersDescriptors(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	now := time.Now()

	seed := func(t *testing.T) (clustersDir, secretsDir string) {
		t.Helper()
		dir := t.TempDir()
		return filepath.Join(dir, "clusters"), writeWorkflowInstallerSecrets(t, dir)
	}
	writeKubeconfig := func(t *testing.T, clustersDir string) {
		t.Helper()
		writeEncryptedClusterKubeconfig(t, clustersDir, cluster)
	}
	one := func(t *testing.T, got []ClusterReinstall, want string) {
		t.Helper()
		if len(got) != 1 || got[0].Name != cluster || !strings.Contains(got[0].Descriptor, want) {
			t.Fatalf("want one reinstall for %s containing %q, got %v", cluster, want, got)
		}
	}

	t.Run("no record with kubeconfig is destructive", func(t *testing.T) {
		clustersDir, secretsDir := seed(t)
		writeKubeconfig(t, clustersDir)
		one(t, OverrideRebuildInstalledClusters(context.Background(), clustersDir, "test", secretsDir, state, tasks, &fakeClusterAvailabilityChecker{available: true}), "no install record")
	})
	t.Run("no record and no kubeconfig is greenfield", func(t *testing.T) {
		clustersDir, secretsDir := seed(t)
		if got := OverrideRebuildInstalledClusters(context.Background(), clustersDir, "test", secretsDir, state, tasks, &fakeClusterAvailabilityChecker{available: true}); len(got) != 0 {
			t.Fatalf("greenfield cluster must not be flagged destructive, got %v", got)
		}
	})
	t.Run("incomplete booted record is destructive", func(t *testing.T) {
		clustersDir, secretsDir := seed(t)
		if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
			Cluster: cluster, DesiredHash: "sha256:stale", HashSchema: ConvergeHashSchema,
			Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseNodesBooted,
			UpdatedAt: now.UTC(),
		}); err != nil {
			t.Fatalf("SaveClusterInstallRecord: %v", err)
		}
		one(t, OverrideRebuildInstalledClusters(context.Background(), clustersDir, "test", secretsDir, state, tasks, &fakeClusterAvailabilityChecker{available: true}), "incomplete install record")
	})
	t.Run("probe error names exclusion escape", func(t *testing.T) {
		clustersDir, secretsDir := seed(t)
		hash, err := clusterInstallDesiredHashForContext("test", state, cluster, secretsDir)
		if err != nil {
			t.Fatalf("clusterInstallDesiredHash: %v", err)
		}
		if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
			Cluster: cluster, DesiredHash: hash, HashSchema: ConvergeHashSchema,
			Status: ClusterInstallStatusInstalled, Phase: ClusterInstallPhaseComplete,
			UpdatedAt: now.UTC(),
		}); err != nil {
			t.Fatalf("SaveClusterInstallRecord: %v", err)
		}
		writeKubeconfig(t, clustersDir)
		got := OverrideRebuildInstalledClusters(context.Background(), clustersDir, "test", secretsDir, state, tasks, &fakeClusterAvailabilityChecker{err: errors.New("connection refused")})
		one(t, got, "availability could not be verified")
		if !strings.Contains(got[0].Descriptor, "--clusters") {
			t.Fatalf("probe-error descriptor must name the --clusters exclusion escape, got %q", got[0].Descriptor)
		}
	})
}
