package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/workflow"
)

func printApplyRunStart(stdout io.Writer, ledger workflow.RunLedger) {
	p := output.NewContinuation(stdout)
	p.Section("Apply run")
	fields := []output.Field{
		{Key: "Run", Value: ledger.RunID},
		{Key: "Target", Value: ledger.Target},
		{Key: "Tasks", Value: fmt.Sprintf("%d", len(ledger.Tasks))},
		{Key: "Parallelism", Value: fmt.Sprintf("%d", ledger.Limits.Parallelism)},
	}
	if ledger.Scope != "" {
		fields = append(fields, output.Field{Key: "Scope", Value: ledger.Scope})
	}
	p.Fields(fields)
	printApplyProgress(stdout, ledger)
}

func printApplyTaskStart(stdout io.Writer, ledger workflow.RunLedger, id string) {
	task, ok := ledger.Task(id)
	if !ok {
		return
	}
	output.NewContinuation(stdout).Tasks([]output.TaskLine{{
		Status: output.StatusWarn,
		Label:  applyTaskDisplayLabel(task.Label),
		Detail: "running",
	}})
}

func printApplyTaskResult(stdout io.Writer, ledger workflow.RunLedger, id string) {
	task, ok := ledger.Task(id)
	if !ok {
		return
	}
	status := output.StatusOK
	detail := "complete"
	switch task.Status {
	case workflow.TaskStatusSkipped:
		status = output.StatusSkip
		detail = task.SkippedReason
	case workflow.TaskStatusFailed:
		status = output.StatusFail
		detail = task.Failure
	case workflow.TaskStatusBlocked:
		status = output.StatusSkip
		detail = task.SkippedReason
	}
	output.NewContinuation(stdout).Tasks([]output.TaskLine{{
		Status: status,
		Label:  applyTaskDisplayLabel(task.Label),
		Detail: detail,
	}})
	printApplyProgress(stdout, ledger)
}

func printApplyProgress(stdout io.Writer, ledger workflow.RunLedger) {
	var fields []output.ProgressField
	for _, count := range ledger.ProgressCounts() {
		fields = append(fields, output.ProgressField{Label: string(count.Status), Count: count.Count})
	}
	output.NewContinuation(stdout).Progress("Progress", fields)
}

func applyTaskDisplayLabel(label string) string {
	switch {
	case label == "provider services":
		return "Provider services"
	case strings.HasPrefix(label, "infra "):
		return "Infra " + strings.TrimPrefix(label, "infra ")
	case strings.HasPrefix(label, "install "):
		return "Install " + strings.TrimPrefix(label, "install ")
	case strings.HasPrefix(label, "iso "):
		return "Create ISO " + strings.TrimPrefix(label, "iso ")
	case strings.HasPrefix(label, "boot "):
		return "Boot " + strings.TrimPrefix(label, "boot ")
	case strings.HasPrefix(label, "wait install "):
		return "Wait install " + strings.TrimPrefix(label, "wait install ")
	default:
		return label
	}
}
