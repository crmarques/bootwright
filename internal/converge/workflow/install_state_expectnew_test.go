package workflow

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReconcileApplyClusterInstallStateExpectNewRefusesExistingRecord(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	now := time.Now()

	cases := []struct {
		name   string
		status ClusterInstallStatus
		phase  ClusterInstallPhase
	}{
		{"installed", ClusterInstallStatusInstalled, ClusterInstallPhaseComplete},
		{"installing iso-created", ClusterInstallStatusInstalling, ClusterInstallPhaseISOCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			clustersDir := filepath.Join(dir, "clusters")
			secretsDir := writeWorkflowInstallerSecrets(t, dir)
			record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
			record.Status = tc.status
			record.Phase = tc.phase
			if tc.status == ClusterInstallStatusInstalled {
				installedAt := now.UTC()
				record.InstalledAt = &installedAt
			}
			if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
				t.Fatalf("SaveClusterInstallRecord: %v", err)
			}
			checker := &fakeClusterAvailabilityChecker{available: true}
			_, _, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", "", secretsDir, "run", state, tasks, ApplyModeCreate, nil, checker, now)
			if err == nil || !strings.Contains(err.Error(), "requires a greenfield environment") {
				t.Fatalf("expect-new against an existing install record must fail closed, got %v", err)
			}
			if !strings.Contains(err.Error(), "--mode create") || !strings.Contains(err.Error(), cluster) {
				t.Fatalf("refusal must name --mode create and the cluster, got %v", err)
			}
		})
	}
}

func TestOverrideReinstallInputDriftedClusters(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	now := time.Now()
	dir := t.TempDir()
	clustersDir := filepath.Join(dir, "clusters")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)

	record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
	record.DesiredHash = "sha256:" + strings.Repeat("0", 64)
	record.StructuralHash = "sha256:" + strings.Repeat("0", 64)
	record.Status = ClusterInstallStatusInstalled
	record.Phase = ClusterInstallPhaseComplete
	installedAt := now.UTC()
	record.InstalledAt = &installedAt
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	drifted := OverrideReinstallInputDriftedClusters(clustersDir, "", "", secretsDir, state, tasks)
	if len(drifted) != 1 || drifted[0] != cluster {
		t.Fatalf("stale install inputs must flag the cluster for reinstall, got %v", drifted)
	}

	record = matchingLifecycleRecord(t, state, secretsDir, cluster, now)
	record.Status = ClusterInstallStatusInstalled
	record.Phase = ClusterInstallPhaseComplete
	record.InstalledAt = &installedAt
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	if got := OverrideReinstallInputDriftedClusters(clustersDir, "", "", secretsDir, state, tasks); len(got) != 0 {
		t.Fatalf("matching install inputs must not flag a reinstall, got %v", got)
	}
}
