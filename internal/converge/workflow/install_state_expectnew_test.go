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
			hash, err := clusterInstallDesiredHashForContext("", state, cluster, secretsDir)
			if err != nil {
				t.Fatalf("hash: %v", err)
			}
			if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
				Cluster: cluster, DesiredHash: hash, HashSchema: ConvergeHashSchema,
				Status: tc.status, Phase: tc.phase, UpdatedAt: now.UTC(),
			}); err != nil {
				t.Fatalf("SaveClusterInstallRecord: %v", err)
			}
			checker := &fakeClusterAvailabilityChecker{available: true}
			_, _, err = ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", "", secretsDir, "run", state, tasks, ApplyModeCreate, nil, checker, now)
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

	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
		Cluster: cluster, DesiredHash: "stale-hash", HashSchema: ConvergeHashSchema,
		Status: ClusterInstallStatusInstalled, Phase: ClusterInstallPhaseComplete, UpdatedAt: now.UTC(),
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	drifted := OverrideReinstallInputDriftedClusters(clustersDir, "", "", secretsDir, state, tasks)
	if len(drifted) != 1 || drifted[0] != cluster {
		t.Fatalf("stale install inputs must flag the cluster for reinstall, got %v", drifted)
	}

	hash, err := clusterInstallDesiredHashForContext("", state, cluster, secretsDir)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
		Cluster: cluster, DesiredHash: hash, HashSchema: ConvergeHashSchema,
		Status: ClusterInstallStatusInstalled, Phase: ClusterInstallPhaseComplete, UpdatedAt: now.UTC(),
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	if got := OverrideReinstallInputDriftedClusters(clustersDir, "", "", secretsDir, state, tasks); len(got) != 0 {
		t.Fatalf("matching install inputs must not flag a reinstall, got %v", got)
	}
}
