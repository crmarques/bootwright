package clusteraccess

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

const cephFixture = "006-ceph-3nodes-libvirt-managed-os"

func TestStorageAccessSummariesDeriveSeedAndCommands(t *testing.T) {
	state := loadFixtureState(t, cephFixture)
	summaries := StorageSummaries(state, "")
	if len(summaries) != 1 {
		t.Fatalf("summaries = %+v, want one storage cluster", summaries)
	}
	summary := summaries[0]
	if summary.Name != "ceph-libvirt" {
		t.Fatalf("name = %q", summary.Name)
	}
	if summary.Type != "ceph" || summary.Management != "managed" {
		t.Fatalf("type/management = %q/%q", summary.Type, summary.Management)
	}
	if summary.SeedHost != "ceph-0" {
		t.Fatalf("seed node = %q", summary.SeedHost)
	}
	if summary.SeedAddress == "" {
		t.Fatal("seed address not resolved from machine")
	}
	wantSSH := "ssh root@" + summary.SeedAddress
	if summary.SSHCommand != wantSSH {
		t.Fatalf("ssh command = %q, want %q", summary.SSHCommand, wantSSH)
	}
	if summary.HealthCommand != wantSSH+" sudo cephadm shell -- ceph -s" {
		t.Fatalf("health command = %q", summary.HealthCommand)
	}
	if summary.ShellCommand != wantSSH+" sudo cephadm shell" {
		t.Fatalf("shell command = %q", summary.ShellCommand)
	}
	if summary.DashboardURL != "https://"+summary.SeedAddress+":8443" {
		t.Fatalf("dashboard url = %q", summary.DashboardURL)
	}
	if summary.ConfigPath != "/etc/ceph/ceph.conf" || summary.KeyringPath != "/etc/ceph/ceph.client.admin.keyring" {
		t.Fatalf("config/keyring = %q/%q", summary.ConfigPath, summary.KeyringPath)
	}
	if len(summary.MonitorEndpoints) != 3 {
		t.Fatalf("monitor endpoints = %v, want three", summary.MonitorEndpoints)
	}
	if !strings.HasPrefix(summary.MonitorEndpoints[0], "ceph-0=") || !strings.HasSuffix(summary.MonitorEndpoints[0], ":6789") {
		t.Fatalf("monitor endpoint[0] = %q", summary.MonitorEndpoints[0])
	}
}

func TestStorageAccessSummariesForApplySkipsUnrunCluster(t *testing.T) {
	state := loadFixtureState(t, cephFixture)
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "clusters", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "storage.ceph-libvirt", Kind: workflow.ApplyTaskKindStorageCluster, Cluster: "ceph-libvirt", Status: workflow.TaskStatusSkipped},
	}, now)
	ledger.Finish(workflow.RunStatusOK, now)

	if summaries := StorageSummariesForApply(state, ledger, t.TempDir()); len(summaries) != 0 {
		t.Fatalf("summaries = %+v, want none for skipped install", summaries)
	}
}

func TestStorageAccessSummaryReportsDashboardPasswordPath(t *testing.T) {
	state := loadFixtureState(t, cephFixture)
	clustersDir := t.TempDir()
	summaries := StorageSummaries(state, clustersDir)
	if len(summaries) != 1 {
		t.Fatalf("summaries = %+v, want one storage cluster", summaries)
	}
	summary := summaries[0]
	wantPath := filepath.Join(clustersDir, "ceph-libvirt", "secrets", "dashboard-password")
	if summary.DashboardUser != "admin" {
		t.Fatalf("dashboard user = %q, want admin", summary.DashboardUser)
	}
	if summary.DashboardPasswordPath != wantPath {
		t.Fatalf("dashboard password path = %q, want %q", summary.DashboardPasswordPath, wantPath)
	}
	if summary.DashboardPasswordCommand != "sudo cat "+wantPath {
		t.Fatalf("dashboard password command = %q", summary.DashboardPasswordCommand)
	}
	if summary.DashboardPassword.Present {
		t.Fatalf("dashboard password reported present before install: %+v", summary.DashboardPassword)
	}
}

func TestStorageAccessSummaryUsesManagementVIPDashboardURL(t *testing.T) {
	managementCluster := func(port int) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: "ceph-ibm"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Management: &v1alpha1.StorageCephManagement{
						DNSName: "dashboard.ceph.bootwright.test",
						Port:    port,
						Ingress: v1alpha1.StorageCephManagementIngress{
							Name: "lab", Address: "192.168.140.81", PrefixLength: 24,
						},
					},
				},
			},
		}
	}

	for _, tc := range []struct {
		name string
		port int
		want string
	}{
		{"default port", 0, "https://dashboard.ceph.bootwright.test:8443"},
		{"explicit port", 9443, "https://dashboard.ceph.bootwright.test:9443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{managementCluster(tc.port)}}
			summaries := StorageSummaries(state, "")
			if len(summaries) != 1 {
				t.Fatalf("summaries = %+v, want one storage cluster", summaries)
			}
			if got := summaries[0].DashboardURL; got != tc.want {
				t.Fatalf("dashboard url = %q, want %q", got, tc.want)
			}
		})
	}
}
