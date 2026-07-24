package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/secrets"
)

const cephFixture = "006-ceph-3nodes-libvirt-managed-os"

func TestClusterAccessReportsStorageClusters(t *testing.T) {
	initTestContext(t, cephFixture)
	stdout, stderr, code := runCLI(t, "cluster", "info")
	if code != 0 {
		t.Fatalf("cluster info exited %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "none declared") {
		t.Fatalf("storage cluster reported as none declared:\n%s", stdout)
	}
	for _, want := range []string{
		"Bootwright: cluster info",
		"ceph-libvirt:",
		"Type: ceph (managed)",
		"Seed node: node01",
		"SSH: ssh root@",
		"Monitors:",
		"    - node01.ceph-libvirt.bootwright.test=",
		"Health check: ssh root@",
		"sudo cephadm shell -- ceph -s",
		"Admin keyring: /etc/ceph/ceph.client.admin.keyring (on node01)",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("cluster info missing %q:\n%s", want, stdout)
		}
	}
}

func TestClusterAccessFiltersStorageClusterByName(t *testing.T) {
	initTestContext(t, cephFixture)
	stdout, stderr, code := runCLI(t, "cluster", "info", "--name", "ceph-libvirt")
	if code != 0 {
		t.Fatalf("cluster info --name exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "ceph-libvirt:") {
		t.Fatalf("filtered output missing storage cluster:\n%s", stdout)
	}

	_, stderr, code = runCLI(t, "cluster", "info", "--name", "missing")
	if code == 0 {
		t.Fatal("cluster info accepted unknown storage cluster")
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
	printClusterAccess(&out, state, render.Result{}, ledger, "test", t.TempDir())
	got := out.String()
	for _, want := range []string{
		"ceph-libvirt:",
		"Health check: ssh root@",
		"sudo cephadm shell -- ceph -s",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("post-apply storage access missing %q:\n%s", want, got)
		}
	}
}

func TestClusterAccessShowsDashboardPasswordAndDoesNotRevealIt(t *testing.T) {
	ctx := initTestContext(t, cephFixture)
	passwordPath := filepath.Join(ctx.ClustersDir, "ceph-libvirt", "secrets", "dashboard-password")
	store := secret.NewContextStore(ctx.Name, workflow.ClusterSecretsDir(ctx.ClustersDir, "ceph-libvirt"))
	if err := store.Write(secret.MaterialKey{Name: "dashboard-password", Role: secret.MaterialPrimary}, []byte("do-not-print-this-dashboard-password\n")); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, "cluster", "info", "--name", "ceph-libvirt")
	if code != 0 {
		t.Fatalf("cluster info exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"Dashboard user: admin",
		"Show password: bootwright cluster info --name ceph-libvirt --secrets",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("cluster info missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "do-not-print-this-dashboard-password") {
		t.Fatalf("cluster info leaked dashboard password bytes without --secrets:\n%s", stdout)
	}
	if strings.Contains(stdout, "Dashboard password file") || strings.Contains(stdout, passwordPath) {
		t.Fatalf("cluster info still exposes the encrypted dashboard password file path:\n%s", stdout)
	}

	secretsOut, _, secretsCode := runCLI(t, "cluster", "info", "--name", "ceph-libvirt", "--secrets")
	if secretsCode != 0 {
		t.Fatalf("cluster info --secrets exited %d", secretsCode)
	}
	if !strings.Contains(secretsOut, "Dashboard password: do-not-print-this-dashboard-password") {
		t.Fatalf("cluster info --secrets did not reveal the dashboard password:\n%s", secretsOut)
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
