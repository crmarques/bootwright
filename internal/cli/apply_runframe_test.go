package cli

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func TestApplyRunFrameGroupsInfraAndClusters(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "provider", Kind: workflow.ApplyTaskKindProvider, Label: "provider services", Status: workflow.TaskStatusOK},
		{ID: "iso.foo", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso foo", Cluster: "foo", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK},
		{ID: "wait.foo", Kind: workflow.ApplyTaskKindInstallWait, Label: "wait install foo", Cluster: "foo", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusRunning},
		{ID: "storage.bar", Kind: workflow.ApplyTaskKindStorageCluster, Label: "storage bar", Cluster: "bar", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusPending},
	}, time.Now())

	frame := applyRunFrame(ledger, nil)

	if frame.BarLabel != "Provisioning" || frame.Total != 4 {
		t.Fatalf("bar label/total = %q/%d, want Provisioning/4 (one line per high-level phase)", frame.BarLabel, frame.Total)
	}
	if frame.Done != 2 {
		t.Fatalf("done = %d, want 2 settled phases", frame.Done)
	}
	if len(frame.Groups) != 3 {
		t.Fatalf("groups = %d, want 3 (infrastructure, foo, bar): %+v", len(frame.Groups), frame.Groups)
	}
	if frame.Groups[0].Title != runGroupInfrastructure {
		t.Fatalf("first group = %q, want %q", frame.Groups[0].Title, runGroupInfrastructure)
	}
	if got := frame.Groups[0].Steps[0]; got.Label != status.PhaseSharedServices || got.Status != output.StatusDone {
		t.Fatalf("infra step = %+v, want %q DONE", got, status.PhaseSharedServices)
	}
	if frame.Groups[1].Title != "bar (StorageCluster)" {
		t.Fatalf("second group = %q, want bar (StorageCluster)", frame.Groups[1].Title)
	}
	if frame.Groups[2].Title != "foo (ContainerCluster)" {
		t.Fatalf("third group = %q, want foo (ContainerCluster)", frame.Groups[2].Title)
	}
	if got := frame.Groups[1].Steps[0]; got.Label != status.PhaseClusterInstall || got.Status != output.StatusPending {
		t.Fatalf("storage step = %+v, want %q PENDING", got, status.PhaseClusterInstall)
	}
}

func TestApplyRunFrameCollapsesTasksIntoHighLevelPhases(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "infraprepare.ocp.host", Kind: workflow.ApplyTaskKindMachineInfraPrepare, Label: "machine infra prepare ocp on host", Cluster: "ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK},
		{ID: "infra.ocp.node01", Kind: workflow.ApplyTaskKindClusterInstall, Label: "provision machine node01", Cluster: "ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK, Dependencies: []string{"infraprepare.ocp.host"}},
		{ID: "infra.ocp.node02", Kind: workflow.ApplyTaskKindClusterInstall, Label: "provision machine node02", Cluster: "ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusRunning, Dependencies: []string{"infraprepare.ocp.host"}},
		{ID: "iso.ocp", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso ocp", Cluster: "ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusPending, Dependencies: []string{"infra.ocp.node02"}},
		{ID: "boot.ocp", Kind: workflow.ApplyTaskKindNodeBoot, Label: "boot ocp nodes", Cluster: "ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusPending, Dependencies: []string{"iso.ocp"}},
		{ID: "wait.ocp", Kind: workflow.ApplyTaskKindInstallWait, Label: "wait install ocp", Cluster: "ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusPending, Dependencies: []string{"boot.ocp"}},
		{ID: "addon.ocp.df", Kind: workflow.ApplyTaskKindClusterAddon, Label: "addon df", Cluster: "ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusPending, Dependencies: []string{"wait.ocp"}},
	}, time.Now())

	steps := applyRunFrame(ledger, nil).Groups[0].Steps
	got := make([]string, 0, len(steps))
	for _, step := range steps {
		got = append(got, step.Label)
	}
	want := []string{status.PhasePrerequisites, status.PhaseClusterInstall, status.PhaseAddOns}
	if len(got) != len(want) {
		t.Fatalf("steps = %v, want the three high-level phases %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("steps = %v, want %v", got, want)
		}
	}
	if steps[0].Status != output.StatusRunning || steps[0].Detail != "Provision machine node02  2/4" {
		t.Fatalf("prerequisites step = %+v, want RUNNING naming the running task and its progress", steps[0])
	}
}

func TestApplyRunFrameNamesTheCrossClusterBlocker(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "wait.host", Kind: workflow.ApplyTaskKindInstallWait, Label: "wait install host", Cluster: "host", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusRunning},
		{ID: "infra.guest.node01", Kind: workflow.ApplyTaskKindClusterInstall, Label: "provision machine node01", Cluster: "guest", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusPending, Dependencies: []string{"wait.host"}},
	}, time.Now())

	for _, group := range applyRunFrame(ledger, nil).Groups {
		if group.Title != "guest (ContainerCluster)" {
			continue
		}
		if got := group.Steps[0].Detail; got != "waiting on host: "+status.PhaseClusterInstall {
			t.Fatalf("guest prerequisites detail = %q, want the blocking cluster and phase named", got)
		}
		return
	}
	t.Fatalf("guest group missing")
}

func TestApplyRunFrameInfraOnlyHasNonClusterGroup(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "infra", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "provider", Kind: workflow.ApplyTaskKindProvider, Label: "provider services", Status: workflow.TaskStatusRunning},
	}, time.Now())

	frame := applyRunFrame(ledger, nil)
	if len(frame.Groups) != 1 || frame.Groups[0].Title != runGroupInfrastructure {
		t.Fatalf("groups = %+v, want a single %q group", frame.Groups, runGroupInfrastructure)
	}
	if frame.Groups[0].Steps[0].Status != output.StatusRunning {
		t.Fatalf("step status = %q, want RUNNING", frame.Groups[0].Steps[0].Status)
	}
}

func TestApplyRunFrameKeepsPlaybookHookPointsSeparate(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "playbook.pre", Kind: workflow.ApplyTaskKindPlaybook, Label: "playbook pre (before machines)", Cluster: "ceph-prd", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
		{ID: "playbook.post", Kind: workflow.ApplyTaskKindPlaybook, Label: "playbook post (after machines)", Cluster: "ceph-prd", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
		{ID: "playbook.post2", Kind: workflow.ApplyTaskKindPlaybook, Label: "playbook tuning (after machines)", Cluster: "ceph-prd", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusRunning},
	}, time.Now())

	steps := applyRunFrame(ledger, nil).Groups[0].Steps
	if len(steps) != 2 {
		t.Fatalf("steps = %+v, want one line per hook point", steps)
	}
	if steps[0].Label != status.PhaseCustomPlaybooks+" (before machines)" || steps[1].Label != status.PhaseCustomPlaybooks+" (after machines)" {
		t.Fatalf("hook points not separated: %+v", steps)
	}
	if steps[1].Status != output.StatusRunning {
		t.Fatalf("aggregated hook status = %q, want RUNNING", steps[1].Status)
	}
}

func TestApplyRunFramePhaseSurfacesFailingTask(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "infra.sno.node01", Kind: workflow.ApplyTaskKindClusterInstall, Label: "provision machine node01", Cluster: "sno", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK},
		{ID: "infra.sno.node02", Kind: workflow.ApplyTaskKindClusterInstall, Label: "provision machine node02", Cluster: "sno", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusFailed, Failure: "failure: libvirt define failed"},
	}, time.Now())

	got := applyRunFrame(ledger, nil).Groups[0].Steps[0]
	if got.Status != output.StatusFailed {
		t.Fatalf("phase status = %q, want FAILED", got.Status)
	}
	if got.Detail != "Provision machine node02: libvirt define failed" {
		t.Fatalf("phase detail = %q, want the failing task named with its reason", got.Detail)
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
	if got := applyStepDetail(task, workflow.RunLedger{}); got != "bootstrap timed out" {
		t.Fatalf("failure detail = %q, want bootstrap timed out", got)
	}
	blocked := workflow.TaskLedgerEntry{Status: workflow.TaskStatusBlocked, SkippedReason: "dependency install.foo failed"}
	if got := applyStepDetail(blocked, workflow.RunLedger{}); got != "dependency install.foo failed" {
		t.Fatalf("blocked detail = %q", got)
	}
}

func TestApplyPlanGroupsNameEveryMachineOfACollapsedProvisionTask(t *testing.T) {
	groups := applyPlanGroups([]workflow.TaskLedgerEntry{
		{ID: "infra.ocp.localhost", Kind: workflow.ApplyTaskKindClusterInstall, Label: "provision machines ocp", Cluster: "ocp", ClusterKind: workflow.ApplyClusterKindContainer, Nodes: []string{"node01", "node02"}, Status: workflow.TaskStatusPending},
	}, nil)

	steps := groups[0].Steps
	if len(steps) != 1 {
		t.Fatalf("steps = %+v, want a single prerequisites step", steps)
	}
	if steps[0].Detail != "node01, node02" {
		t.Fatalf("prerequisites detail = %q, want both machines the collapsed task provisions", steps[0].Detail)
	}
}

func TestApplyTaskDisplayLabelKeepsSingularAndPluralProvisionLabels(t *testing.T) {
	if got := applyTaskDisplayLabel("provision machines ocp"); got != "Provision machines ocp" {
		t.Fatalf("plural label = %q", got)
	}
	if got := applyTaskDisplayLabel("provision machine node01"); got != "Provision machine node01" {
		t.Fatalf("singular label = %q", got)
	}
}
