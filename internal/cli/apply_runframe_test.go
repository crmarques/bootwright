package cli

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestApplyRunFrameGroupsInfraAndClusters(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "provider", Kind: workflow.ApplyTaskKindProvider, Label: "provider services", Status: workflow.TaskStatusOK},
		{ID: "iso.foo", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso foo", Cluster: "foo", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK},
		{ID: "wait.foo", Kind: workflow.ApplyTaskKindInstallWait, Label: "wait install foo", Cluster: "foo", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusRunning},
		{ID: "storage.bar", Kind: workflow.ApplyTaskKindStorageCluster, Label: "storage bar", Cluster: "bar", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusPending},
	}, time.Now())

	frame := applyRunFrame(ledger)

	if frame.BarLabel != "Fleet" || frame.Total != 4 {
		t.Fatalf("bar label/total = %q/%d, want Fleet/4", frame.BarLabel, frame.Total)
	}
	if len(frame.Groups) != 3 {
		t.Fatalf("groups = %d, want 3 (infra, foo, bar): %+v", len(frame.Groups), frame.Groups)
	}
	// Leading non-cluster group holds the fabric task.
	if frame.Groups[0].Title != "infra" {
		t.Fatalf("first group = %q, want infra", frame.Groups[0].Title)
	}
	if got := frame.Groups[0].Steps[0]; got.Label != "Provider services" || got.Status != output.StatusDone {
		t.Fatalf("infra step = %+v, want Provider services DONE", got)
	}
	// Cluster groups carry the kind suffix and per-task steps, ordered by the
	// ledger's sorted cluster names (bar before foo).
	if frame.Groups[1].Title != "bar (StorageCluster)" {
		t.Fatalf("second group = %q, want bar (StorageCluster)", frame.Groups[1].Title)
	}
	if frame.Groups[2].Title != "foo (ContainerCluster)" {
		t.Fatalf("third group = %q, want foo (ContainerCluster)", frame.Groups[2].Title)
	}
	if got := frame.Groups[1].Steps[0]; got.Status != output.StatusPending {
		t.Fatalf("storage step status = %q, want PENDING", got.Status)
	}
}

func TestApplyRunFrameInfraOnlyHasNonClusterGroup(t *testing.T) {
	// apply --stage infra: tasks belong to clusters by machine ownership, so a
	// fabric-only task with no cluster must still surface in the infra group.
	ledger := workflow.NewRunLedger("apply-test", "infra", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "provider", Kind: workflow.ApplyTaskKindProvider, Label: "provider services", Status: workflow.TaskStatusRunning},
	}, time.Now())

	frame := applyRunFrame(ledger)
	if len(frame.Groups) != 1 || frame.Groups[0].Title != "infra" {
		t.Fatalf("groups = %+v, want a single infra group", frame.Groups)
	}
	if frame.Groups[0].Steps[0].Status != output.StatusRunning {
		t.Fatalf("step status = %q, want RUNNING", frame.Groups[0].Steps[0].Status)
	}
}

func TestApplyStepStatusMapping(t *testing.T) {
	cases := map[workflow.TaskStatus]output.Status{
		workflow.TaskStatusOK:        output.StatusDone,
		workflow.TaskStatusRunning:   output.StatusRunning,
		workflow.TaskStatusReady:     output.StatusRunning,
		workflow.TaskStatusPending:   output.StatusPending,
		workflow.TaskStatusFailed:    output.StatusFailed,
		workflow.TaskStatusBlocked:   output.StatusBlocked,
		workflow.TaskStatusSkipped:   output.StatusSkipped,
		workflow.TaskStatusCancelled: output.StatusCancel,
	}
	for in, want := range cases {
		if got := applyStepStatus(in); got != want {
			t.Fatalf("applyStepStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyStepDetailSurfacesFailureReason(t *testing.T) {
	task := workflow.TaskLedgerEntry{Status: workflow.TaskStatusFailed, Failure: "failure: bootstrap timed out"}
	if got := applyStepDetail(task); got != "bootstrap timed out" {
		t.Fatalf("failure detail = %q, want bootstrap timed out", got)
	}
	blocked := workflow.TaskLedgerEntry{Status: workflow.TaskStatusBlocked, SkippedReason: "dependency install.foo failed"}
	if got := applyStepDetail(blocked); got != "dependency install.foo failed" {
		t.Fatalf("blocked detail = %q", got)
	}
}
