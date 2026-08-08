package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestApplyTaskDisplayLabelNamesTheBootstrapWait(t *testing.T) {
	if got := applyTaskDisplayLabel("wait bootstrap ocp-prd-02"); got != "Bootstrap ocp-prd-02" {
		t.Fatalf("applyTaskDisplayLabel(%q) = %q; a failure row must not print the internal task label", "wait bootstrap ocp-prd-02", got)
	}
	if got := applyTaskDisplayLabel("wait install ocp-prd-02"); got != "Install ocp-prd-02" {
		t.Fatalf("applyTaskDisplayLabel(%q) = %q, want the install wait to keep its phrasing", "wait install ocp-prd-02", got)
	}
}

func TestApplyRunStartPrintsTheLimitTheRunResolved(t *testing.T) {
	t.Setenv(workflow.ParallelismClustersEnvVar, "")
	tasks := []workflow.ApplyTask{
		{Entry: workflow.TaskLedgerEntry{ID: "iso.ocp-prd-01", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso ocp-prd-01", Cluster: "ocp-prd-01"}},
		{Entry: workflow.TaskLedgerEntry{ID: "iso.ocp-prd-02", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso ocp-prd-02", Cluster: "ocp-prd-02"}},
	}
	limits := workflow.ResolveApplyConcurrencyLimits(workflow.ConcurrencyLimits{}, tasks)
	if limits.ParallelismClusters != workflow.DefaultParallelismClusters {
		t.Fatalf("resolved cluster install limit = %d, want the %d default persisted on the ledger", limits.ParallelismClusters, workflow.DefaultParallelismClusters)
	}
	ledger := workflow.NewRunLedger("apply-limits", "clusters", "", limits, workflow.TaskLedgerEntries(tasks), time.Now())
	var stdout strings.Builder
	t.Setenv(workflow.ParallelismClustersEnvVar, "8")
	printApplyRunStart(&stdout, "test", t.TempDir(), ledger)
	if !strings.Contains(stdout.String(), "cluster installs 1") {
		t.Fatalf("run header = %q; it must report the limit the run resolved, not the one the printing shell asks for", stdout.String())
	}
}
