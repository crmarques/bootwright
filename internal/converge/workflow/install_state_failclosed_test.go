package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReconcileApplyClusterInstallStateFailsClosed(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	const cluster = "sno-libvirt"
	tasks := mustPlanApplyTasks(applyContainerClusterTarget(), state)
	now := time.Now()

	matchingHash := func(t *testing.T, secretsDir string) string {
		t.Helper()
		hash, err := clusterInstallDesiredHashForContext("test", state, cluster, secretsDir)
		if err != nil {
			t.Fatalf("clusterInstallDesiredHash: %v", err)
		}
		return hash
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

	cases := []struct {
		name    string
		seed    func(t *testing.T, clustersDir, secretsDir string)
		wantErr string
	}{
		{
			name: "installed record not available",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
					Cluster: cluster, DesiredHash: matchingHash(t, secretsDir), HashSchema: ConvergeHashSchema,
					Status: ClusterInstallStatusInstalled, Phase: ClusterInstallPhaseComplete,
					UpdatedAt: now.UTC(),
				}); err != nil {
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
			wantErr: "existing kubeconfig but does not report Available=True",
		},
		{
			name: "booting phase is uncertain",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
					Cluster: cluster, DesiredHash: matchingHash(t, secretsDir), HashSchema: ConvergeHashSchema,
					Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseBooting,
					UpdatedAt: now.UTC(),
				}); err != nil {
					t.Fatalf("SaveClusterInstallRecord: %v", err)
				}
			},
			wantErr: "node boot completion is uncertain",
		},
		{
			name: "unrecognized phase",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
					Cluster: cluster, DesiredHash: matchingHash(t, secretsDir), HashSchema: ConvergeHashSchema,
					Status: ClusterInstallStatusInstalling, Phase: "bogus-phase",
					UpdatedAt: now.UTC(),
				}); err != nil {
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
			_, _, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", secretsDir, "run", state, tasks, ApplyModeContinue, nil, checker, now)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected fail-closed error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
