package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestDestroyRunFrameListsTeardownSteps(t *testing.T) {
	ledger := workflow.NewRunLedger("destroy-test", "infra destroy", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "destroy.machine-infra", Kind: workflow.DestroyTaskKindMachineInfra, Label: "Machine infrastructure", Status: workflow.TaskStatusOK},
		{ID: "destroy.infra-components", Kind: workflow.DestroyTaskKindInfraComponents, Label: "Infra component services", Status: workflow.TaskStatusRunning},
		{ID: "destroy.provider-services", Kind: workflow.DestroyTaskKindProviderServices, Label: "Provider services", Status: workflow.TaskStatusPending},
	}, time.Now())

	frame := destroyRunFrame(ledger)
	if frame.BarLabel != "Teardown" || frame.Total != 3 {
		t.Fatalf("bar = %q total = %d, want Teardown/3", frame.BarLabel, frame.Total)
	}
	if len(frame.Groups) != 1 || len(frame.Groups[0].Steps) != 3 {
		t.Fatalf("want one group of three steps: %+v", frame.Groups)
	}
	if frame.Groups[0].Steps[0].Status != output.StatusDone || frame.Groups[0].Steps[1].Status != output.StatusRunning {
		t.Fatalf("step statuses = %+v", frame.Groups[0].Steps)
	}
}

func TestDestroyOrphanHintScopedToSweepCoverage(t *testing.T) {
	var buf bytes.Buffer
	printDestroyOrphans(&buf, []workflow.UndeclaredResource{
		{Kind: "libvirt-domain", Name: "vm-a"},
		{Kind: "kubevirt-machine", Name: "vm-b"},
		{Kind: "storage-cluster", Name: "ceph-old"},
	})
	out := buf.String()
	if !strings.Contains(out, "libvirt-domain/vm-a") || !strings.Contains(out, "a full `bootwright destroy` reclaims it") {
		t.Fatalf("libvirt-domain orphan should promise reclaim:\n%s", out)
	}
	for _, line := range []string{"kubevirt-machine/vm-b", "storage-cluster/ceph-old"} {
		if !strings.Contains(out, line) {
			t.Fatalf("missing orphan %q:\n%s", line, out)
		}
	}
	// The sweep does not cover kubevirt/storage records, so their hint must not
	// over-promise an automatic reclaim.
	if strings.Count(out, "does not reclaim this record") != 2 {
		t.Fatalf("kubevirt and storage orphans should each get the not-reclaimed hint:\n%s", out)
	}
}

func TestDestroyOutputNamesCoveredClusters(t *testing.T) {
	now := time.Now()
	ledger := workflow.NewRunLedger("destroy-test", "clusters destroy", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "destroy.storage-clusters", Kind: workflow.DestroyTaskKindStorageCluster, Label: "Storage clusters", ResourceKeys: []string{"ceph-storage"}, Status: workflow.TaskStatusOK},
		{ID: "destroy.container-clusters", Kind: workflow.DestroyTaskKindContainerCluster, Label: "Container clusters", ResourceKeys: []string{"dc1-metal-ocp", "dc1-child-ocp"}, Status: workflow.TaskStatusFailed, Dependencies: []string{"destroy.storage-clusters"}, Failure: "failure: agent removal failed"},
	}, now)

	// The frame names the clusters each step covers, even on the failed step.
	frame := destroyRunFrame(ledger)
	if got := frame.Groups[0].Steps[1].Detail; !strings.Contains(got, "dc1-metal-ocp, dc1-child-ocp") {
		t.Fatalf("failed step detail = %q, want covered cluster names", got)
	}

	var buf bytes.Buffer
	printDestroyRunSummary(&buf, "/runs", ledger)
	if !strings.Contains(buf.String(), "clusters: dc1-metal-ocp, dc1-child-ocp") {
		t.Fatalf("summary missing covered clusters:\n%s", buf.String())
	}
}

func TestDestroyRunSummaryPrintsRunLogAndFailure(t *testing.T) {
	now := time.Now()
	ledger := workflow.NewRunLedger("destroy-test", "infra destroy", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "destroy.machine-infra", Kind: workflow.DestroyTaskKindMachineInfra, Label: "Machine infrastructure", Status: workflow.TaskStatusPending},
	}, now)
	ledger.MarkRunning("destroy.machine-infra", "/runs/log", now)
	ledger.MarkFailed("destroy.machine-infra", "failure: libvirt domain still running", now)
	ledger.Finish(workflow.RunStatusFailed, now)

	var buf bytes.Buffer
	printDestroyRunSummary(&buf, "/runs", ledger)
	out := buf.String()
	for _, want := range []string{"[FAILED] infra destroy", "Run log", "Machine infrastructure", "libvirt domain still running"} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q:\n%s", want, out)
		}
	}
}
