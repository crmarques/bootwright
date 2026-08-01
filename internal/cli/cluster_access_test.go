package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/secrets"
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

	summaries := clusteraccess.ClusterSummariesForApply(state, result, ledger)
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
	printClusterAccess(&out, state, result, ledger, "test", clustersDir)
	got := out.String()
	for _, want := range []string{
		"Cluster access",
		"API: https://api.sno-libvirt.bootwright.test:6443",
		"Console: https://console-openshift-console.apps.sno-libvirt.bootwright.test",
		"Kubeadmin user: kubeadmin",
		"Show password: bootwright cluster info --name sno-libvirt --secrets",
		"oc: bootwright container-cluster oc --name sno-libvirt get nodes",
		"kubectl: bootwright container-cluster kubectl --name sno-libvirt get nodes",
		"Kubeconfig: bootwright container-cluster kubeconfig --name sno-libvirt",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cluster access output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Kube context") || strings.Contains(got, "Password file") {
		t.Fatalf("cluster access output still presents an encrypted secret file as an access method:\n%s", got)
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

	stdout, stderr, code := runCLI(t, "cluster", "info")
	if code != 0 {
		t.Fatalf("cluster info exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"Bootwright: cluster info",
		"sno-libvirt:",
		"Show password: bootwright cluster info --name sno-libvirt --secrets",
		"oc: bootwright container-cluster oc --name sno-libvirt get nodes",
		"kubectl: bootwright container-cluster kubectl --name sno-libvirt get nodes",
		"Kubeconfig: bootwright container-cluster kubeconfig --name sno-libvirt",
		"Node master-0: bootwright cluster rsh --name sno-libvirt --node master-0",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("cluster info output missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "do-not-print-this-password") {
		t.Fatalf("cluster info leaked password bytes without --secrets:\n%s", stdout)
	}
	if strings.Contains(stdout, kubeconfigPath) || strings.Contains(stdout, passwordPath) {
		t.Fatalf("cluster info still exposes an encrypted secret file path:\n%s", stdout)
	}
}

func TestClusterNodeAccessReportsPublicOnlyNodeSSH(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	state.ContainerClusters[0].Spec.Install.NodeSSH = v1alpha1.NodeSSHSpec{
		PublicKeyRef: v1alpha1.SecretRef{Name: "cluster-public"},
	}
	var out bytes.Buffer
	printClusterNodeAccess(cliout.New(&out), state, "sno-libvirt")
	got := out.String()
	if !strings.Contains(got, "unavailable: set spec.install.nodeSSH.keyPairRef or privateKeyRef") {
		t.Fatalf("public-only access output = %q", got)
	}
	if strings.Contains(got, "bootwright cluster rsh") {
		t.Fatalf("public-only access advertised a failing command: %q", got)
	}
}

func TestClusterInfoSecretsRevealsKubeadminPassword(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	kubeconfigPath := filepath.Join(ctx.ClustersDir, "sno-libvirt", "secrets", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := secret.NewContextStore(ctx.Name, workflow.ClusterSecretsDir(ctx.ClustersDir, "sno-libvirt"))
	if err := store.Write(secret.MaterialKey{Name: "kubeadmin-password", Role: secret.MaterialPrimary}, []byte("reveal-this-password\n")); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, "cluster", "info", "--name", "sno-libvirt", "--secrets")
	if code != 0 {
		t.Fatalf("cluster info --secrets exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "Kubeadmin password: reveal-this-password") {
		t.Fatalf("cluster info --secrets did not reveal the password:\n%s", stdout)
	}
}

func TestClusterAccessCommandRejectsUnknownCluster(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	_, stderr, code := runCLI(t, "cluster", "info", "--name", "missing")
	if code == 0 {
		t.Fatal("cluster info accepted unknown cluster")
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
	if !strings.Contains(stderr, `invalid argument "access" for "bootwright container-cluster"`) {
		t.Fatalf("stderr does not reject removed container-cluster access subtree: %q", stderr)
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
