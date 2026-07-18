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

	frame := applyRunFrame(ledger, nil)

	if frame.BarLabel != "Provisioning Progress" || frame.Total != 4 {
		t.Fatalf("bar label/total = %q/%d, want Provisioning Progress/4", frame.BarLabel, frame.Total)
	}
	if len(frame.Groups) != 3 {
		t.Fatalf("groups = %d, want 3 (infra, foo, bar): %+v", len(frame.Groups), frame.Groups)
	}
	if frame.Groups[0].Title != "infra" {
		t.Fatalf("first group = %q, want infra", frame.Groups[0].Title)
	}
	if got := frame.Groups[0].Steps[0]; got.Label != "Provider services" || got.Status != output.StatusDone {
		t.Fatalf("infra step = %+v, want Provider services DONE", got)
	}
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

func TestApplyRunFrameOrdersClusterStepsByDependencies(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "storage.ceph", Kind: workflow.ApplyTaskKindStorageCluster, Label: "storage ceph", Cluster: "ceph", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusRunning, Dependencies: []string{"storageinfra.ceph"}},
		{ID: "storageinfra.ceph", Kind: workflow.ApplyTaskKindStorageInfra, Label: "storage infra ceph", Cluster: "ceph", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
	}, time.Now())

	frame := applyRunFrame(ledger, nil)

	if len(frame.Groups) != 1 || len(frame.Groups[0].Steps) != 2 {
		t.Fatalf("groups = %+v, want one group with two steps", frame.Groups)
	}
	got := []string{frame.Groups[0].Steps[0].Label, frame.Groups[0].Steps[1].Label}
	want := []string{"Provision infra ceph", "Provision ceph"}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("step order = %v, want %v", got, want)
	}
}

func TestApplyRunFrameInfraOnlyHasNonClusterGroup(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "infra", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "provider", Kind: workflow.ApplyTaskKindProvider, Label: "provider services", Status: workflow.TaskStatusRunning},
	}, time.Now())

	frame := applyRunFrame(ledger, nil)
	if len(frame.Groups) != 1 || frame.Groups[0].Title != "infra" {
		t.Fatalf("groups = %+v, want a single infra group", frame.Groups)
	}
	if frame.Groups[0].Steps[0].Status != output.StatusRunning {
		t.Fatalf("step status = %q, want RUNNING", frame.Groups[0].Steps[0].Status)
	}
}

func TestApplyRunFrameCollapsesProvisioningPlaybooks(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "managedos.ceph-prd", Kind: workflow.ApplyTaskKindManagedMachineOS, Label: "managed OS ceph-prd machines", Cluster: "ceph-prd", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
		{ID: "storageinfra.ceph-prd", Kind: workflow.ApplyTaskKindStorageInfra, Label: "storage infra ceph-prd", Cluster: "ceph-prd", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
		{ID: "playbook.os-customizations", Kind: workflow.ApplyTaskKindProvisioningPlaybook, Label: "playbook os-customizations (after machines)", Cluster: "ceph-prd", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
		{ID: "playbook.tuning", Kind: workflow.ApplyTaskKindProvisioningPlaybook, Label: "playbook tuning (after machines)", Cluster: "ceph-prd", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusRunning},
		{ID: "storage.ceph-prd", Kind: workflow.ApplyTaskKindStorageCluster, Label: "storage ceph-prd", Cluster: "ceph-prd", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusPending},
	}, time.Now())

	frame := applyRunFrame(ledger, nil)
	if len(frame.Groups) != 1 {
		t.Fatalf("groups = %d, want 1: %+v", len(frame.Groups), frame.Groups)
	}
	steps := frame.Groups[0].Steps
	var playbookSteps []output.Step
	for _, step := range steps {
		if step.Label == "Custom playbooks (after machines)" {
			playbookSteps = append(playbookSteps, step)
		}
	}
	if len(playbookSteps) != 1 {
		t.Fatalf("custom playbook steps = %d, want 1 collapsed line: %+v", len(playbookSteps), steps)
	}
	if playbookSteps[0].Status != output.StatusRunning {
		t.Fatalf("collapsed status = %q, want RUNNING", playbookSteps[0].Status)
	}
	if playbookSteps[0].ID != "playbooks:ceph-prd:after machines" {
		t.Fatalf("collapsed id = %q, want playbooks:ceph-prd:after machines", playbookSteps[0].ID)
	}
	if len(steps) != 4 {
		t.Fatalf("steps = %d, want 4 (managed OS, storage infra, custom playbooks, provision): %+v", len(steps), steps)
	}
}

func TestApplyRunFrameSeparatesPlaybooksByHookPoint(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "playbook.pre", Kind: workflow.ApplyTaskKindProvisioningPlaybook, Label: "playbook pre (before machines)", Cluster: "ceph-prd", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
		{ID: "playbook.post", Kind: workflow.ApplyTaskKindProvisioningPlaybook, Label: "playbook post (after machines)", Cluster: "ceph-prd", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
	}, time.Now())

	frame := applyRunFrame(ledger, nil)
	labels := map[string]bool{}
	for _, step := range frame.Groups[0].Steps {
		labels[step.Label] = true
	}
	if !labels["Custom playbooks (before machines)"] || !labels["Custom playbooks (after machines)"] {
		t.Fatalf("hook points not separated: %+v", frame.Groups[0].Steps)
	}
}

func TestApplyRunFrameCollapsedPlaybookSurfacesFailure(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "playbook.ok", Kind: workflow.ApplyTaskKindProvisioningPlaybook, Label: "playbook ok (after base)", Cluster: "sno", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK},
		{ID: "playbook.bad", Kind: workflow.ApplyTaskKindProvisioningPlaybook, Label: "playbook bad (after base)", Cluster: "sno", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusFailed, Failure: "failure: role failed"},
	}, time.Now())

	frame := applyRunFrame(ledger, nil)
	got := frame.Groups[0].Steps[0]
	if got.Status != output.StatusFailed || got.Detail != "role failed" {
		t.Fatalf("collapsed failed step = %+v, want FAILED role failed", got)
	}
}

func TestAggregateProvisioningStatus(t *testing.T) {
	entry := func(status workflow.TaskStatus) workflow.TaskLedgerEntry {
		return workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindProvisioningPlaybook, Status: status}
	}
	cases := []struct {
		name     string
		statuses []workflow.TaskStatus
		want     output.Status
	}{
		{"done-and-skipped-reads-done", []workflow.TaskStatus{workflow.TaskStatusOK, workflow.TaskStatusSkipped}, output.StatusDone},
		{"done-and-pending-stays-running", []workflow.TaskStatus{workflow.TaskStatusOK, workflow.TaskStatusPending}, output.StatusRunning},
		{"all-skipped-reads-skipped", []workflow.TaskStatus{workflow.TaskStatusSkipped, workflow.TaskStatusSkipped}, output.StatusSkipped},
		{"all-pending-reads-pending", []workflow.TaskStatus{workflow.TaskStatusPending, workflow.TaskStatusPending}, output.StatusPending},
		{"any-failed-reads-failed", []workflow.TaskStatus{workflow.TaskStatusOK, workflow.TaskStatusFailed}, output.StatusFailed},
		{"blocked-over-running", []workflow.TaskStatus{workflow.TaskStatusRunning, workflow.TaskStatusBlocked}, output.StatusBlocked},
		{"running-over-pending", []workflow.TaskStatus{workflow.TaskStatusRunning, workflow.TaskStatusPending}, output.StatusRunning},
	}
	for _, tc := range cases {
		tasks := make([]workflow.TaskLedgerEntry, 0, len(tc.statuses))
		for _, s := range tc.statuses {
			tasks = append(tasks, entry(s))
		}
		if got := aggregateProvisioningStatus(tasks); got != tc.want {
			t.Fatalf("%s: aggregate = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestApplyTaskDisplayLabelProvisioningPlaybook(t *testing.T) {
	if got := applyTaskDisplayLabel("playbook os-customizations (after machines)"); got != "Custom playbook os-customizations (after machines)" {
		t.Fatalf("display label = %q", got)
	}
}

func TestProvisioningHookPoint(t *testing.T) {
	if got := provisioningHookPoint("playbook os-customizations (after machines)"); got != "after machines" {
		t.Fatalf("hook point = %q, want after machines", got)
	}
	if got := provisioningHookPoint("playbook noparen"); got != "" {
		t.Fatalf("hook point = %q, want empty", got)
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
