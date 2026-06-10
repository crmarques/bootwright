package clusteraccess

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
)

func TestClusterAccessSummariesRequireSuccessfulInstallWait(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	result := render.Result{InstallerAssets: render.InstallerAssets(filepath.Join(t.TempDir(), "clusters"), state)}
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "cluster", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "wait.sno-libvirt", Kind: workflow.ApplyTaskKindInstallWait, Cluster: "sno-libvirt", Status: workflow.TaskStatusSkipped},
	}, now)
	ledger.Finish(workflow.RunStatusOK, now)

	if summaries := ClusterSummariesForApply(state, result, ledger); len(summaries) != 0 {
		t.Fatalf("summaries = %+v, want none", summaries)
	}
}
