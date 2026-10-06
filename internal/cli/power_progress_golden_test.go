package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A power stop waits on the guest for as long as its shutdown takes, so the
// run is one step under Progress after the Logs field, the role's groups are
// its sub-steps, the heartbeat repeats the step while the shutdown is silent,
// and the step closes with the power it proved and how long it took.
func TestPowerStopProgressPastTheHeartbeatGolden(t *testing.T) {
	clock := newManualClock()
	var out bytes.Buffer
	presenter := &LifecycleProgressPresenter{progress: progressPresenter{out: &out, clock: clock.clock()}}
	ctx := context.Background()
	event := func(group, detail, status string) lifecycle.ProgressEvent {
		return lifecycle.ProgressEvent{Block: "power", Description: "Stop Machine/rhel-01", Group: group, Detail: detail,
			Status: status, Position: 1, Total: 1}
	}
	presenter.ReportLogLocation(ctx, "/var/lib/bootwright/contexts/lab-rhel/state/runs/run-"+strings.Repeat("a", 32))
	presenter.ReportProgress(ctx, event("", "", "running"))
	presenter.ReportProgress(ctx, event("read-state", "read the power state", "running"))
	clock.advance(time.Second)
	presenter.ReportProgress(ctx, event("read-state", "read the power state", "ok"))
	presenter.ReportProgress(ctx, event("power-off", "shut down", "running"))
	clock.advance(25 * time.Second)
	presenter.ReportProgress(ctx, event("power-off", "shut down", "ok"))
	presenter.ReportProgress(ctx, event("", "off", "changed"))
	presenter.Finish()
	text := out.String()
	if !strings.Contains(text, "[RUNNING]  [1/1] Stop Machine/rhel-01: shut down  still running, ") ||
		!strings.HasSuffix(text, "[DONE]     [1/1] Stop Machine/rhel-01: off  26s\n") {
		t.Fatalf("a stop past the heartbeat rendered\n%s", text)
	}
	matchesTextGolden(t, "progress-machine-stop", out.Bytes())
}
