package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const progressGiB = int64(1) << 30

func byteEvent(group, status string, completed int64) lifecycle.ProgressEvent {
	return lifecycle.ProgressEvent{Block: "erase", Description: "Erase Machine/metal-1", Group: group, Detail: group,
		Status: status, Position: 1, Total: 1, Bytes: lifecycle.ByteProgress{Completed: completed, Total: 8 * progressGiB}}
}

// A running group with a known byte count shows the percentage proved, the
// bytes, and the rate and time left this process observed on its own clock,
// and a group without one shows none of it.
func TestByteProgressRowsGolden(t *testing.T) {
	clock := newManualClock()
	var out bytes.Buffer
	presenter := &LifecycleProgressPresenter{progress: progressPresenter{out: &out, clock: clock.clock()}}
	ctx := context.Background()
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{Block: "erase", Description: "Erase Machine/metal-1", Status: "running", Position: 1, Total: 1})
	presenter.ReportProgress(ctx, byteEvent("zero and read back sda", "running", 0))
	clock.advance(30 * time.Second)
	presenter.ReportProgress(ctx, byteEvent("zero and read back sda", "running", progressGiB))
	clock.advance(30 * time.Second)
	presenter.ReportProgress(ctx, byteEvent("zero and read back sda", "running", 3*progressGiB))
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{Block: "erase", Description: "Erase Machine/metal-1", Group: "power off", Detail: "power off", Status: "running", Position: 1, Total: 1})
	presenter.ReportProgress(ctx, byteEvent("zero and read back sda", "ok", 8*progressGiB))
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{Block: "erase", Description: "Erase Machine/metal-1", Detail: "3 of 3 disks erased", Status: "done", Position: 1, Total: 1})
	presenter.Finish()
	text := out.String()
	for _, want := range []string{
		"zero and read back sda  0% (0 B of 8.0 GiB)\n",
		"zero and read back sda  12% (1.0 GiB of 8.0 GiB), 34.1 MiB/s, about 4m left at the observed rate\n",
		"zero and read back sda  37% (3.0 GiB of 8.0 GiB), 51.2 MiB/s, about 2m left at the observed rate\n",
		"power off\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("progress lacks %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "100%") || strings.Contains(text, "power off  ") {
		t.Fatalf("a settled or unmeasured group showed a measure\n%s", text)
	}
	matchesTextGolden(t, "progress-erase-bytes", out.Bytes())
}

func TestByteProgressReplacesTheHeartbeatNoteOnATerminalAndFitsIt(t *testing.T) {
	for _, width := range []int{60, 100, 160} {
		clock := newManualClock()
		var out bytes.Buffer
		presenter := &LifecycleProgressPresenter{progress: progressPresenter{out: &out, clock: clock.clock(), columns: fixedColumns(width)}}
		ctx := context.Background()
		presenter.ReportProgress(ctx, lifecycle.ProgressEvent{Block: "erase", Description: "Erase Machine/metal-1", Status: "running", Position: 1, Total: 1})
		presenter.ReportProgress(ctx, byteEvent("zero and read back sda", "running", 0))
		clock.advance(30 * time.Second)
		presenter.ReportProgress(ctx, byteEvent("zero and read back sda", "running", progressGiB))
		clock.advance(3 * time.Second)
		screen := terminalScreen(out.String())
		line := screen[len(screen)-1]
		if displayCellWidth(line) >= width || !strings.Contains(line, "12%") && width >= 100 || strings.Contains(line, "still running") {
			t.Fatalf("width %d: row = %q", width, line)
		}
		presenter.Finish()
	}
}
