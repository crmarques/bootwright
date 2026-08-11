package workflow

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

func TestReconcileApplyClusterInstallStateFailsClosed(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	now := time.Now()

	writeKubeconfig := func(t *testing.T, clustersDir string) {
		t.Helper()
		writeEncryptedClusterKubeconfig(t, clustersDir, cluster)
	}

	cases := []struct {
		name    string
		seed    func(t *testing.T, clustersDir, secretsDir string)
		wantErr string
	}{
		{
			name: "installed record not available",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
				record.Status = ClusterInstallStatusInstalled
				record.Phase = ClusterInstallPhaseComplete
				installedAt := now.UTC()
				record.InstalledAt = &installedAt
				if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
					t.Fatalf("SaveClusterInstallRecord: %v", err)
				}
				writeKubeconfig(t, clustersDir)
			},
			wantErr: "does not report Available=True",
		},
		{
			name: "adopted kubeconfig not available",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				writeKubeconfig(t, clustersDir)
			},
			wantErr: "existing kubeconfig but no install record and does not report Available=True",
		},
		{
			name: "booting phase is uncertain",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
				record.Status = ClusterInstallStatusInstalling
				record.Phase = ClusterInstallPhaseBooting
				if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
					t.Fatalf("SaveClusterInstallRecord: %v", err)
				}
			},
			wantErr: "node boot completion is uncertain",
		},
		{
			name: "unrecognized phase",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				record := matchingLifecycleRecord(t, state, secretsDir, cluster, now)
				record.Status = ClusterInstallStatusInstalling
				record.Phase = "bogus-phase"
				if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
					t.Fatalf("SaveClusterInstallRecord: %v", err)
				}
			},
			wantErr: "unrecognized install phase",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			clustersDir := filepath.Join(dir, "clusters")
			secretsDir := writeWorkflowInstallerSecrets(t, dir)
			tc.seed(t, clustersDir, secretsDir)
			checker := &fakeClusterAvailabilityChecker{available: false}
			_, _, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", "", secretsDir, "run", state, tasks, ApplyModeReconcile, nil, checker, now)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected fail-closed error containing %q, got %v", tc.wantErr, err)
			}
			if tc.name == "booting phase is uncertain" {
				assertClusterInstallRemedy(t, err, remedy.ActionDestroyAndReapplyCluster, cluster)
			}
			if tc.name == "unrecognized phase" {
				assertClusterInstallRemedy(t, err, remedy.ActionRebuildCluster, cluster)
			}
		})
	}
}
