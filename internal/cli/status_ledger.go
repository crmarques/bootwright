package cli

import (
	"fmt"
	"strings"
	"time"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func printApplyLedgerStatus(p *cliout.Printer, runsDir string, clustersDir string, ledger workflow.RunLedger, found bool, loadErr error) {
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
	p.Section("Progress")
	p.ProgressBar("Tasks", applyProgressDone(ledger), len(ledger.Tasks), applyProgressFields(ledger))
	printApplyLedgerRunning(p, clustersDir, ledger)
	printApplyLedgerClusters(p, clustersDir, ledger)
	printApplyLedgerBlocked(p, ledger)
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

func printApplyLedgerRunning(p *cliout.Printer, clustersDir string, ledger workflow.RunLedger) {
	running := ledger.RunningTasks()
	if len(running) == 0 {
		return
	}
	p.Section("Running work")
	lines := make([]cliout.TaskLine, 0, len(running))
	for _, task := range running {
		lines = append(lines, cliout.TaskLine{Status: cliout.StatusRunning, Label: applyTaskDisplayLabel(task.Label), Detail: applyTaskRunDetail(clustersDir, task)})
	}
	p.Tasks(lines)
}

func printApplyLedgerClusters(p *cliout.Printer, clustersDir string, ledger workflow.RunLedger) {
	names := ledger.ClusterNames()
	if len(names) == 0 {
		return
	}
	p.Section("Apply clusters")
	p.ClusterPhases(applyClusterPhaseLines(clustersDir, ledger))
}

func printApplyLedgerBlocked(p *cliout.Printer, ledger workflow.RunLedger) {
	blocked := ledger.BlockedTasks()
	if len(blocked) == 0 {
		return
	}
	p.Section("Blocked work")
	lines := make([]cliout.TaskLine, 0, len(blocked))
	for _, task := range blocked {
		lines = append(lines, cliout.TaskLine{Status: cliout.StatusBlocked, Label: applyTaskDisplayLabel(task.Label), Detail: task.SkippedReason})
	}
	p.Tasks(lines)
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

func ledgerNextSteps(ledger workflow.RunLedger, activity workflow.RunActivity, existing []string) []string {
	switch ledger.Status {
	case workflow.RunStatusRunning:
		if activity.State == workflow.RunActivityStale {
			return append([]string{fmt.Sprintf("bootwright apply %s --yes", ledger.Target)}, existing...)
		}
		return append([]string{"bootwright status --watch"}, existing...)
	case workflow.RunStatusFailed:
		hints := []string{fmt.Sprintf("bootwright apply %s --yes", ledger.Target)}
		for _, task := range ledger.FailedTasks() {
			if task.LogPath != "" {
				hints = append(hints, "inspect "+task.LogPath)
			}
		}
		return append(hints, existing...)
	default:
		return existing
	}
}
