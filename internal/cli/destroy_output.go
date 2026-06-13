package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// destroyReporter drives the live destroy progress surface, the teardown
// counterpart of applyReporter. It owns one output.RunView fed from the destroy
// run ledger (see destroyRunFrame). The scheduler calls these from a single
// goroutine, so the view needs no locking.
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

// destroyRunFrame projects the destroy ledger into a single ordered group of
// teardown steps. Destroy tasks are scope-level (host-group teardown), not
// per-cluster, so they share one group rather than the per-cluster grouping
// apply uses. The cluster-stage steps cover several clusters at once, so each
// step names the clusters it tears down (from the persisted ResourceKeys) —
// otherwise a failed "Container clusters" step would not say which of the fleet
// it covered.
func destroyRunFrame(ledger workflow.RunLedger) output.RunFrame {
	steps := make([]output.Step, 0, len(ledger.Tasks))
	for _, task := range ledger.Tasks {
		steps = append(steps, output.Step{
			ID:     task.ID,
			Label:  task.Label,
			Status: applyStepStatus(task.Status),
			Detail: destroyStepDetail(task, ledger),
		})
	}
	return output.RunFrame{
		BarLabel: "Teardown",
		Done:     applyProgressDone(ledger),
		Total:    len(ledger.Tasks),
		Counts:   applyProgressFields(ledger),
		Groups:   []output.StepGroup{{Steps: steps}},
	}
}

// destroyStepDetail names the clusters a teardown step covers, appending the
// failure/blocked reason when the step did not simply succeed so identity is
// kept even on failure.
func destroyStepDetail(task workflow.TaskLedgerEntry, ledger workflow.RunLedger) string {
	covered := strings.Join(task.ResourceKeys, ", ")
	reason := applyStepDetail(task, ledger)
	switch {
	case covered != "" && reason != "":
		return covered + " — " + reason
	case covered != "":
		return covered
	default:
		return reason
	}
}

// printDestroyRunStart writes the run-identity fields under the already-open
// "Run" section opened by the bundle reporter.
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
	status := output.StatusOK
	detail := "complete"
	switch ledger.Status {
	case workflow.RunStatusFailed:
		status = output.StatusFailed
		detail = "failed"
	case workflow.RunStatusCancelled:
		status = output.StatusCancel
		detail = "cancelled"
	}
	p.Status(status, ledger.Target, detail)
	p.Fields([]output.Field{{Key: "Run log", Value: workflow.ApplyRunLogPath(runsDir, ledger.RunID)}})
	for _, task := range ledger.FailedTasks() {
		fields := []output.Field{
			{Key: "failed task", Value: task.Label},
		}
		if covered := strings.Join(task.ResourceKeys, ", "); covered != "" {
			fields = append(fields, output.Field{Key: "clusters", Value: covered})
		}
		fields = append(fields, output.Field{Key: "reason", Value: applyFailureReason(task.Failure)})
		if task.LogPath != "" {
			fields = append(fields, output.Field{Key: "log", Value: task.LogPath})
		}
		p.Details(fields)
	}
	for _, task := range ledger.BlockedTasks() {
		fields := []output.Field{{Key: "blocked task", Value: task.Label}}
		if covered := strings.Join(task.ResourceKeys, ", "); covered != "" {
			fields = append(fields, output.Field{Key: "clusters", Value: covered})
		}
		fields = append(fields, output.Field{Key: "reason", Value: applyBlockedReason(ledger, task)})
		p.Details(fields)
	}
}
