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
