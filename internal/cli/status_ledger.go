package cli

import (
	"fmt"
	"strings"
	"time"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func printApplyLedgerStatus(p *cliout.Printer, runsDir string, displays map[string]clusterDisplay, ledger workflow.RunLedger, found bool, loadErr error) {
	p.Section("Current apply")
	if loadErr != nil {
		p.Status(cliout.StatusWarn, "Apply ledger", loadErr.Error())
		return
	}
	if !found {
		p.Status(cliout.StatusSkip, "Apply run", "no run ledger found")
		return
	}
	fields := []cliout.Field{
		{Key: "Run", Value: ledger.RunID},
		{Key: "Target", Value: ledger.Target},
		{Key: "Status", Value: string(ledger.Status)},
		{Key: "Started", Value: ledger.StartedAt.Format(time.RFC3339)},
	}
	if ledger.Scope != "" {
		fields = append(fields, cliout.Field{Key: "Scope", Value: ledger.Scope})
	}
	p.Fields(fields)
	if ledger.EndedAt != nil {
		p.Fields([]cliout.Field{{Key: "Ended", Value: ledger.EndedAt.Format(time.RFC3339)}})
	}
	if ledger.Active() {
		printApplyRunActivity(p, runsDir, ledger)
	}
	// Render the same step frame the live apply view uses, so `status` /
	// `status --watch` and a running apply can never disagree about a run's
	// shape. RenderFrame width 0 disables wrap accounting: status reprints the
	// whole page each poll rather than redrawing the frame in place.
	p.Section("Progress")
	// collapse=false: a one-shot status report prints the full record, including
	// finished groups, rather than summarizing them like the live redraw does.
	p.RenderFrame(applyRunFrame(ledger, displays), 0, false)
	printApplyLedgerFailures(p, ledger)
}

func printApplyRunActivity(p *cliout.Printer, runsDir string, ledger workflow.RunLedger) {
	activity, err := workflow.AssessRunActivity(runsDir, ledger, time.Now())
	if err != nil {
		p.Status(cliout.StatusWarn, "Lease", err.Error())
		return
	}
	switch activity.State {
	case workflow.RunActivityActive:
		detail := activity.Detail
		if activity.Lease != nil {
			detail = fmt.Sprintf("%s; pid %d heartbeat %s", detail, activity.Lease.PID, activity.Lease.HeartbeatAt.Format(time.RFC3339))
		}
		p.Status(cliout.StatusRunning, "Lease", detail)
	case workflow.RunActivityStale:
		p.Status(cliout.StatusWarn, "Lease", activity.Detail+"; next apply or destroy will mark it cancelled")
	}
}

func printApplyLedgerFailures(p *cliout.Printer, ledger workflow.RunLedger) {
	failed := ledger.FailedTasks()
	if len(failed) == 0 {
		return
	}
	p.Section("Failures")
	lines := make([]cliout.TaskLine, 0, len(failed))
	for _, task := range failed {
		detail := task.Failure
		if task.LogPath != "" {
			detail = strings.TrimSpace(detail + "; log " + task.LogPath)
		}
		lines = append(lines, cliout.TaskLine{Status: cliout.StatusFailed, Label: applyTaskDisplayLabel(task.Label), Detail: detail})
	}
	p.Tasks(lines)
}
