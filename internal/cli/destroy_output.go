package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func printDestroySafety(stdout io.Writer, decision workflow.DestroySafetyDecision, authorized bool, dryRun bool) {
	if len(decision.Reasons) == 0 {
		return
	}
	message := decision.Summary()
	if authorized {
		output.NewContinuation(stdout).Warning("authorize "+authorizeProtected, message+"; --authorize "+authorizeProtected+" supplied for this command only")
		return
	}
	if dryRun {
		output.NewContinuation(stdout).Warning("destroy protection", message+"; a mutating destroy requires --authorize "+authorizeProtected)
	}
}

type destroyReporter struct {
	stdout  io.Writer
	stderr  io.Writer
	runsDir string
	view    *output.RunView
}

func newDestroyReporter(stdout, stderr io.Writer, runsDir string, streamAnsible bool) *destroyReporter {
	view := output.NewRunView(stdout)
	if streamAnsible {
		view.Streaming()
	}
	return &destroyReporter{stdout: stdout, stderr: stderr, runsDir: runsDir, view: view}
}

func (r *destroyReporter) RunStart(ledger workflow.RunLedger) {
	printDestroyRunStart(r.stdout, r.runsDir, ledger)
}

func (r *destroyReporter) StageSnapshot(ledger workflow.RunLedger) {
	r.view.Render(destroyRunFrame(ledger))
}

func (r *destroyReporter) RunSummary(ledger workflow.RunLedger) {
	r.view.Finish(destroyRunFrame(ledger))
	printDestroyRunSummary(r.stdout, r.runsDir, ledger)
}

func (r *destroyReporter) PromptGap() {
	output.NewContinuation(r.stderr).BlankLine()
}

func destroyRunFrame(ledger workflow.RunLedger) output.RunFrame {
	return newRunFrame("Teardown", destroyStepGroups(ledger))
}

func destroyPlanGroups(tasks []workflow.TaskLedgerEntry) []output.StepGroup {
	return destroyStepGroups(workflow.RunLedger{Tasks: tasks})
}

func destroyStepGroups(ledger workflow.RunLedger) []output.StepGroup {
	steps := runPhaseSteps("", ledger.Tasks, ledger, phaseDetailOptions{resources: true})
	if len(steps) == 0 {
		return nil
	}
	return []output.StepGroup{{Steps: steps}}
}

func printDestroyRunStart(stdout io.Writer, runsDir string, ledger workflow.RunLedger) {
	output.NewContinuation(stdout).Fields([]output.Field{
		{Key: "ID", Value: ledger.RunID},
		{Key: "Target", Value: ledger.Target},
		{Key: "Tasks", Value: fmt.Sprintf("%d", len(ledger.Tasks))},
		{Key: "Run log", Value: workflow.ApplyRunLogPath(runsDir, ledger.RunID)},
	})
}

func printDestroyRunSummary(stdout io.Writer, runsDir string, ledger workflow.RunLedger) {
	p := output.NewContinuation(stdout)
	p.Section("Summary")
	summaryStatus := output.StatusOK
	detail := "complete"
	switch ledger.Status {
	case workflow.RunStatusFailed:
		summaryStatus = output.StatusFailed
		detail = "failed"
	case workflow.RunStatusCancelled:
		summaryStatus = output.StatusCancel
		detail = "cancelled"
	}
	p.Status(summaryStatus, ledger.Target, detail)
	p.Fields([]output.Field{{Key: "Run log", Value: workflow.ApplyRunLogPath(runsDir, ledger.RunID)}})
	for _, task := range ledger.FailedTasks() {
		fields := []output.Field{
			{Key: "failed task", Value: task.Label},
		}
		if covered := strings.Join(workflow.DestroyTaskClusterKeys(task), ", "); covered != "" {
			fields = append(fields, output.Field{Key: "clusters", Value: covered})
		}
		fields = append(fields, output.Field{Key: "reason", Value: status.ApplyFailureReason(task.Failure)})
		if logPath := writtenTaskLogPath(task.LogPath); logPath != "" {
			fields = append(fields, output.Field{Key: "log", Value: logPath})
		}
		p.Details(fields)
	}
	for _, task := range ledger.BlockedTasks() {
		fields := []output.Field{{Key: "blocked task", Value: task.Label}}
		if covered := strings.Join(workflow.DestroyTaskClusterKeys(task), ", "); covered != "" {
			fields = append(fields, output.Field{Key: "clusters", Value: covered})
		}
		fields = append(fields, output.Field{Key: "reason", Value: applyBlockedReason(ledger, task)})
		p.Details(fields)
	}
}
