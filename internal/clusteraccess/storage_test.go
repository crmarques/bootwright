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
	if summary.SeedHost != "node01.ceph-libvirt.bootwright.test" {
		t.Fatalf("seed node = %q", summary.SeedHost)
	}
	if summary.SeedAddress == "" {
		t.Fatal("seed address not resolved from machine")
	}
	wantSSH := "ssh cephadm@" + summary.SeedAddress
	if summary.SSHCommand != wantSSH {
		t.Fatalf("ssh command = %q, want %q", summary.SSHCommand, wantSSH)
	}
	if summary.HealthCommand != wantSSH+" sudo cephadm shell -- ceph -s" {
		t.Fatalf("health command = %q", summary.HealthCommand)
	}
	if summary.ShellCommand != wantSSH+" sudo cephadm shell" {
		t.Fatalf("shell command = %q", summary.ShellCommand)
	}
	if summary.DashboardURL != "https://mgr.ceph-libvirt.bootwright.test:8443" {
		t.Fatalf("dashboard url = %q", summary.DashboardURL)
	}
	if summary.ConfigPath != "/etc/ceph/ceph.conf" || summary.KeyringPath != "/etc/ceph/ceph.client.admin.keyring" {
		t.Fatalf("config/keyring = %q/%q", summary.ConfigPath, summary.KeyringPath)
	}
	if len(summary.MonitorEndpoints) != 3 {
		t.Fatalf("monitor endpoints = %v, want three", summary.MonitorEndpoints)
	}
	if !strings.HasPrefix(summary.MonitorEndpoints[0], "node01.ceph-libvirt.bootwright.test=") || !strings.HasSuffix(summary.MonitorEndpoints[0], ":6789") {
		t.Fatalf("monitor endpoint[0] = %q", summary.MonitorEndpoints[0])
	}
}

func TestStorageAccessSummaryFallsBackToSeedAddressWithoutDomain(t *testing.T) {
	state := loadFixtureState(t, cephFixture)
	state.Environments = nil
	summaries := StorageSummaries(state, "")
	if len(summaries) != 1 {
		t.Fatalf("summaries = %+v, want one storage cluster", summaries)
	}
	summary := summaries[0]
	if summary.SeedAddress == "" {
		t.Fatal("seed address not resolved from machine")
	}
	if summary.DashboardURL != "https://"+summary.SeedAddress+":8443" {
		t.Fatalf("dashboard url = %q, want IP-based fallback", summary.DashboardURL)
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
	if summary.DashboardPasswordCommand != "bootwright cluster info --name ceph-libvirt --secrets" {
		t.Fatalf("dashboard password command = %q", summary.DashboardPasswordCommand)
	}
	if summary.DashboardPassword.Present {
		t.Fatalf("dashboard password reported present before install: %+v", summary.DashboardPassword)
	}
}

func TestStorageAccessSummaryUsesMgmtGatewayVIPDashboardURL(t *testing.T) {
	managementCluster := func(label string, port int) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: "ceph-ibm"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					MgmtGateway: &v1alpha1.StorageCephMgmtGateway{
						DNSLabel: label,
						Port:     port,
						Ingress: v1alpha1.StorageCephMgmtGatewayIngress{
							Name: "lab", Address: "192.168.140.81", PrefixLength: 24,
						},
					},
				},
			},
		}
	}
	environment := v1alpha1.Environment{
		Metadata: v1alpha1.Metadata{Name: "lab"},
		Spec: v1alpha1.EnvironmentSpec{
			Domains: v1alpha1.EnvironmentDomainsSpec{Base: "bootwright.test"},
		},
	}

	for _, tc := range []struct {
		name  string
		label string
		port  int
		want  string
	}{
		{"default port", "dashboard", 0, "https://dashboard.ceph-ibm.bootwright.test:8443"},
		{"explicit port", "dashboard", 9443, "https://dashboard.ceph-ibm.bootwright.test:9443"},
		{"default label", "", 0, "https://mgr.ceph-ibm.bootwright.test:8443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := v1alpha1.State{
				Environments:    []v1alpha1.Environment{environment},
				StorageClusters: []v1alpha1.StorageCluster{managementCluster(tc.label, tc.port)},
			}
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
