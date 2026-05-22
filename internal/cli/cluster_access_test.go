package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/provisioning/render"
	"github.com/crmarques/bootwright/internal/workflow"
)

func TestClusterAccessSummariesUseRuntimeAuthPaths(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	stateDir := filepath.Join(t.TempDir(), "state")
	result := render.Result{InstallerAssets: render.InstallerAssets(stateDir, state)}
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "cluster", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "wait.sno-libvirt", Kind: applyTaskKindInstallWait, Cluster: "sno-libvirt", Status: workflow.TaskStatusOK},
	}, now)
	ledger.Finish(workflow.RunStatusOK, now)

	summaries := clusterAccessSummaries(state, result, ledger)
	if len(summaries) != 1 {
		t.Fatalf("summaries = %+v, want one cluster", summaries)
	}
	summary := summaries[0]
	kubeconfigPath := filepath.Join(stateDir, "runtime", "sno-libvirt", "installer", "auth", "kubeconfig")
	passwordPath := filepath.Join(stateDir, "runtime", "sno-libvirt", "installer", "auth", "kubeadmin-password")
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
	printClusterAccess(&out, state, result, ledger)
	got := out.String()
	for _, want := range []string{
		"Cluster access",
		"Kubeconfig: " + kubeconfigPath,
		"Kube context: KUBECONFIG=" + kubeconfigPath,
		"API: https://api.sno-libvirt.bootwright.test:6443",
		"Console: https://console-openshift-console.apps.sno-libvirt.bootwright.test",
		"Credentials: user kubeadmin; password file " + passwordPath,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cluster access output missing %q:\n%s", want, got)
		}
	}
}

func TestClusterAccessSummariesRequireSuccessfulInstallWait(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	stateDir := filepath.Join(t.TempDir(), "state")
	result := render.Result{InstallerAssets: render.InstallerAssets(stateDir, state)}
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "cluster", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "wait.sno-libvirt", Kind: applyTaskKindInstallWait, Cluster: "sno-libvirt", Status: workflow.TaskStatusSkipped},
	}, now)
	ledger.Finish(workflow.RunStatusOK, now)

	if summaries := clusterAccessSummaries(state, result, ledger); len(summaries) != 0 {
		t.Fatalf("summaries = %+v, want none", summaries)
	}
}
