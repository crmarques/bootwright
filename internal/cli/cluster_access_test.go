package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
)

func TestClusterAccessSummariesUseClusterSecretsPaths(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	clustersDir := filepath.Join(t.TempDir(), "clusters")
	result := render.Result{InstallerAssets: render.InstallerAssets(clustersDir, state)}
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "cluster", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "wait.sno-libvirt", Kind: workflow.ApplyTaskKindInstallWait, Cluster: "sno-libvirt", Status: workflow.TaskStatusOK},
	}, now)
	ledger.Finish(workflow.RunStatusOK, now)

	summaries := clusterAccessSummariesForApply(state, result, ledger)
	if len(summaries) != 1 {
		t.Fatalf("summaries = %+v, want one cluster", summaries)
	}
	summary := summaries[0]
	kubeconfigPath := filepath.Join(clustersDir, "sno-libvirt", "secrets", "kubeconfig")
	passwordPath := filepath.Join(clustersDir, "sno-libvirt", "secrets", "kubeadmin-password")
	if summary.KubeconfigPath != kubeconfigPath {
		t.Fatalf("kubeconfig path = %q, want %q", summary.KubeconfigPath, kubeconfigPath)
	}
	if summary.KubeContextCommand != "KUBECONFIG="+kubeconfigPath {
		t.Fatalf("kube context command = %q", summary.KubeContextCommand)
	}
	if summary.APIURL != "https://api.sno-libvirt.bootwright.test:6443" {
		t.Fatalf("API URL = %q", summary.APIURL)
	}
	if summary.ConsoleURL != "https://console-openshift-console.apps.sno-libvirt.bootwright.test" {
		t.Fatalf("console URL = %q", summary.ConsoleURL)
	}
	if summary.KubeadminPasswordPath != passwordPath {
		t.Fatalf("password path = %q, want %q", summary.KubeadminPasswordPath, passwordPath)
	}
	var out bytes.Buffer
	printClusterAccess(&out, state, result, ledger, clustersDir)
	got := out.String()
	for _, want := range []string{
		"Cluster access",
		"Kubeconfig: " + kubeconfigPath,
		"Kube context: KUBECONFIG=" + kubeconfigPath,
		"API: https://api.sno-libvirt.bootwright.test:6443",
		"Console: https://console-openshift-console.apps.sno-libvirt.bootwright.test",
		"Kubeadmin user: kubeadmin",
		"Password file: " + passwordPath,
		"Show password: sudo cat " + passwordPath,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cluster access output missing %q:\n%s", want, got)
		}
	}
}

func TestClusterAccessSummariesRequireSuccessfulInstallWait(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	result := render.Result{InstallerAssets: render.InstallerAssets(filepath.Join(t.TempDir(), "clusters"), state)}
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "cluster", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "wait.sno-libvirt", Kind: workflow.ApplyTaskKindInstallWait, Cluster: "sno-libvirt", Status: workflow.TaskStatusSkipped},
	}, now)
	ledger.Finish(workflow.RunStatusOK, now)

	if summaries := clusterAccessSummariesForApply(state, result, ledger); len(summaries) != 0 {
		t.Fatalf("summaries = %+v, want none", summaries)
	}
}

func TestClusterAccessCommandPrintsAllClustersAndDoesNotRevealPassword(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	kubeconfigPath := filepath.Join(ctx.ClustersDir, "sno-libvirt", "secrets", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	passwordPath := filepath.Join(ctx.ClustersDir, "sno-libvirt", "secrets", "kubeadmin-password")
	if err := os.WriteFile(passwordPath, []byte("do-not-print-this-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, "cluster", "access")
	if code != 0 {
		t.Fatalf("cluster access exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"Bootwright: cluster access",
		"Cluster sno-libvirt",
		"Kubeconfig: " + kubeconfigPath,
		"Password file: " + passwordPath,
		"Show password: sudo cat " + passwordPath,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("cluster access output missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "do-not-print-this-password") {
		t.Fatalf("cluster access leaked password bytes:\n%s", stdout)
	}
}

func TestClusterAccessCommandRejectsUnknownCluster(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	_, stderr, code := runCLI(t, "cluster", "access", "--cluster", "missing")
	if code == 0 {
		t.Fatal("cluster access accepted unknown cluster")
	}
	if !strings.Contains(stderr, "unknown cluster(s): missing") {
		t.Fatalf("stderr missing unknown cluster message: %q", stderr)
	}
}

func TestContainerClusterAccessSubtreeRemoved(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "container-cluster", "access")
	if code == 0 {
		t.Fatalf("container-cluster access unexpectedly succeeded:\n%s", stdout)
	}
	if !strings.Contains(stderr, `unknown command "container-cluster"`) {
		t.Fatalf("stderr does not reject removed container-cluster subtree: %q", stderr)
	}
}

func TestClusterListJSONReportsAccessStatus(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	kubeconfigPath := filepath.Join(ctx.ClustersDir, "sno-libvirt", "secrets", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, "cluster", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("cluster list exited %d, stderr=%q", code, stderr)
	}
	var report clusterListReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Context != "test" {
		t.Fatalf("context = %q", report.Context)
	}
	if len(report.Clusters) != 1 {
		t.Fatalf("clusters = %+v, want one", report.Clusters)
	}
	cluster := report.Clusters[0]
	if cluster.Name != "sno-libvirt" {
		t.Fatalf("cluster name = %q", cluster.Name)
	}
	if !cluster.Kubeconfig.Present || cluster.Kubeconfig.Path != kubeconfigPath {
		t.Fatalf("kubeconfig status = %+v", cluster.Kubeconfig)
	}
	if cluster.KubeadminPassword.Present {
		t.Fatalf("kubeadmin password unexpectedly present: %+v", cluster.KubeadminPassword)
	}
	if cluster.Ready {
		t.Fatal("cluster list marked cluster ready with missing password")
	}
}
