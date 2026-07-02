package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReconcileApplyClusterInstallStateFailsClosed pins the fail-closed branches
// that stop a default (non-override) apply from re-running the destructive
// installer (re-imaging / rebooting nodes) over a cluster that is recorded
// installed-or-booted but not provably healthy. Without these guards a default
// apply could regenerate installer inputs and reboot a live cluster; the test
// fails if any refusal is removed or inverted.
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
			// Installed record (hash matches) but the kubeconfig does not report
			// Available=True: refuse rather than regenerate installer inputs.
			name: "installed record not available",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
					Cluster: cluster, DesiredHash: matchingHash(t, secretsDir),
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
			// No record, only a surviving kubeconfig: adoption must verify the
			// cluster is Available before claiming it installed.
			name: "adopted kubeconfig not available",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				writeKubeconfig(t, clustersDir)
			},
			wantErr: "existing kubeconfig but does not report Available=True",
		},
		{
			// Prior install state stuck mid-boot: node-boot completion is uncertain,
			// so refuse to reboot without --override.
			name: "booting phase is uncertain",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
					Cluster: cluster, DesiredHash: matchingHash(t, secretsDir),
					Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseBooting,
					UpdatedAt: now.UTC(),
				}); err != nil {
					t.Fatalf("SaveClusterInstallRecord: %v", err)
				}
			},
			wantErr: "node boot completion is uncertain",
		},
		{
			// An unrecognized phase is ambiguous: refuse rather than guess.
			name: "unrecognized phase",
			seed: func(t *testing.T, clustersDir, secretsDir string) {
				if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
					Cluster: cluster, DesiredHash: matchingHash(t, secretsDir),
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
			// available:false models a cluster that is not provably healthy.
			checker := &fakeClusterAvailabilityChecker{available: false}
			_, err := ReconcileApplyClusterInstallState(context.Background(), clustersDir, "", secretsDir, "run", state, tasks, ApplyModeContinue, checker, now)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected fail-closed error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
