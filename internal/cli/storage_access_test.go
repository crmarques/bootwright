package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
)

const cephFixture = "006-ceph-3nodes-libvirt-managed-os"

func TestStorageAccessSummariesDeriveSeedAndCommands(t *testing.T) {
	state := loadFixtureState(t, cephFixture)
	summaries := storageAccessSummaries(state)
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

func TestClusterAccessInfoReportsStorageClusters(t *testing.T) {
	initTestContext(t, cephFixture)
	stdout, stderr, code := runCLI(t, "cluster", "access-info")
	if code != 0 {
		t.Fatalf("cluster access-info exited %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "none declared") {
		t.Fatalf("storage cluster reported as none declared:\n%s", stdout)
	}
	for _, want := range []string{
		"Bootwright: cluster access-info",
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
			t.Fatalf("cluster access-info missing %q:\n%s", want, stdout)
		}
	}
}

func TestClusterAccessInfoFiltersStorageClusterByName(t *testing.T) {
	initTestContext(t, cephFixture)
	stdout, stderr, code := runCLI(t, "cluster", "access-info", "--cluster", "ceph-libvirt")
	if code != 0 {
		t.Fatalf("cluster access-info --cluster exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "Storage cluster ceph-libvirt") {
		t.Fatalf("filtered output missing storage cluster:\n%s", stdout)
	}

	_, stderr, code = runCLI(t, "cluster", "access-info", "--cluster", "missing")
	if code == 0 {
		t.Fatal("cluster access-info accepted unknown storage cluster")
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
	printClusterAccess(&out, state, render.Result{}, ledger)
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

	if summaries := storageAccessSummariesForApply(state, ledger); len(summaries) != 0 {
		t.Fatalf("summaries = %+v, want none for skipped install", summaries)
	}
}
