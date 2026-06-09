package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
)

const cephFixture = "006-ceph-3nodes-libvirt-managed-os"

func TestStorageAccessSummariesDeriveSeedAndCommands(t *testing.T) {
	state := loadFixtureState(t, cephFixture)
	summaries := storageAccessSummaries(state, "")
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
	if summary.SeedNode != "ceph-0" {
		t.Fatalf("seed node = %q", summary.SeedNode)
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

func TestClusterAccessReportsStorageClusters(t *testing.T) {
	initTestContext(t, cephFixture)
	stdout, stderr, code := runCLI(t, "cluster", "access")
	if code != 0 {
		t.Fatalf("cluster access exited %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "none declared") {
		t.Fatalf("storage cluster reported as none declared:\n%s", stdout)
	}
	for _, want := range []string{
		"Bootwright: cluster access",
		"Storage cluster ceph-libvirt",
		"Type: ceph (managed)",
		"Seed node: ceph-0",
		"SSH: ssh root@",
		"Health check: ssh root@",
		"sudo cephadm shell -- ceph -s",
		"Admin keyring: /etc/ceph/ceph.client.admin.keyring (on ceph-0)",
		"[INFO] health:",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("cluster access missing %q:\n%s", want, stdout)
		}
	}
}

func TestClusterAccessFiltersStorageClusterByName(t *testing.T) {
	initTestContext(t, cephFixture)
	stdout, stderr, code := runCLI(t, "cluster", "access", "--cluster", "ceph-libvirt")
	if code != 0 {
		t.Fatalf("cluster access --cluster exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "Storage cluster ceph-libvirt") {
		t.Fatalf("filtered output missing storage cluster:\n%s", stdout)
	}

	_, stderr, code = runCLI(t, "cluster", "access", "--cluster", "missing")
	if code == 0 {
		t.Fatal("cluster access accepted unknown storage cluster")
	}
	if !strings.Contains(stderr, "unknown cluster(s): missing") {
		t.Fatalf("stderr missing unknown cluster message: %q", stderr)
	}
}

func TestPrintClusterAccessShowsStorageAfterSuccessfulApply(t *testing.T) {
	state := loadFixtureState(t, cephFixture)
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "clusters", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "storage.ceph-libvirt", Kind: workflow.ApplyTaskKindStorageCluster, Cluster: "ceph-libvirt", Status: workflow.TaskStatusOK},
	}, now)
	ledger.Finish(workflow.RunStatusOK, now)

	var out bytes.Buffer
	printClusterAccess(&out, state, render.Result{}, ledger, t.TempDir())
	got := out.String()
	for _, want := range []string{
		"Storage cluster ceph-libvirt",
		"Health check: ssh root@",
		"sudo cephadm shell -- ceph -s",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("post-apply storage access missing %q:\n%s", want, got)
		}
	}
}

func TestStorageAccessSummariesForApplySkipsUnrunCluster(t *testing.T) {
	state := loadFixtureState(t, cephFixture)
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "clusters", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "storage.ceph-libvirt", Kind: workflow.ApplyTaskKindStorageCluster, Cluster: "ceph-libvirt", Status: workflow.TaskStatusSkipped},
	}, now)
	ledger.Finish(workflow.RunStatusOK, now)

	if summaries := storageAccessSummariesForApply(state, ledger, t.TempDir()); len(summaries) != 0 {
		t.Fatalf("summaries = %+v, want none for skipped install", summaries)
	}
}

func TestStorageAccessSummaryReportsDashboardPasswordPath(t *testing.T) {
	state := loadFixtureState(t, cephFixture)
	clustersDir := t.TempDir()
	summaries := storageAccessSummaries(state, clustersDir)
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

func TestClusterAccessShowsDashboardPasswordAndDoesNotRevealIt(t *testing.T) {
	ctx := initTestContext(t, cephFixture)
	passwordPath := filepath.Join(ctx.ClustersDir, "ceph-libvirt", "secrets", "dashboard-password")
	if err := os.MkdirAll(filepath.Dir(passwordPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwordPath, []byte("do-not-print-this-dashboard-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, "cluster", "access", "--cluster", "ceph-libvirt")
	if code != 0 {
		t.Fatalf("cluster access exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"Dashboard user: admin",
		"Dashboard password file: " + passwordPath,
		"Show dashboard password: sudo cat " + passwordPath,
		"[OK] dashboard password",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("cluster access missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "do-not-print-this-dashboard-password") {
		t.Fatalf("cluster access leaked dashboard password bytes:\n%s", stdout)
	}

	listOut, _, listCode := runCLI(t, "cluster", "list")
	if listCode != 0 {
		t.Fatalf("cluster list exited %d", listCode)
	}
	if !strings.Contains(listOut, "Dashboard password: OK "+passwordPath) {
		t.Fatalf("cluster list missing dashboard password status:\n%s", listOut)
	}
	if strings.Contains(listOut, "do-not-print-this-dashboard-password") {
		t.Fatalf("cluster list leaked dashboard password bytes:\n%s", listOut)
	}
}
