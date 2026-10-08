package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A power reading waits on every controller one host reaches, so each host is
// one check under Checks, its role's groups are sub-steps, the heartbeat
// repeats it while the controllers are silent, and it closes OK with how many
// Machines it read. A reading keeps no log, so no Logs field precedes it.
func TestPowerReadProgressPastTheHeartbeatGolden(t *testing.T) {
	clock := newManualClock()
	var out bytes.Buffer
	presenter := &LifecycleProgressPresenter{progress: progressPresenter{out: &out, clock: clock.clock()}}
	ctx := context.Background()
	event := func(group, detail, status string) lifecycle.ProgressEvent {
		return lifecycle.ProgressEvent{Phase: lifecycle.CheckPhase, Block: "power-read",
			Description: "Read the power state through Machine/hypervisor", Group: group, Detail: detail,
			Status: status, Position: 1, Total: 1}
	}
	presenter.ReportProgress(ctx, event("", "", "running"))
	presenter.ReportProgress(ctx, event("read-state", "read-state", "running"))
	clock.advance(25 * time.Second)
	presenter.ReportProgress(ctx, event("read-state", "read-state", "ok"))
	presenter.ReportProgress(ctx, event("", "2 of 2 machines read", "ok"))
	presenter.Finish()
	text := out.String()
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if !strings.Contains(text, "Checks\n") || strings.Contains(text, "Progress\n") || strings.Contains(text, "Logs") ||
		!strings.Contains(text, "[RUNNING]  [1/1] Read the power state through Machine/hypervisor: read-state  still running, ") ||
		!strings.HasPrefix(lines[len(lines)-1], "  [OK]") {
		t.Fatalf("a reading past the heartbeat rendered\n%s", text)
	}
	matchesTextGolden(t, "progress-machine-list-power", out.Bytes())
}
