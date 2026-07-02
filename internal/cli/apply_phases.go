package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func applyClusterLifecycleSummary(ledger workflow.RunLedger, cluster string) (output.Status, string) {
	phases := status.ApplyClusterPhases(ledger, cluster)
	if len(phases) == 0 {
		return output.StatusOK, ""
	}
	worst := output.StatusOK
	parts := make([]string, 0, len(phases))
	for _, phase := range phases {
		phaseStatus := applyPhaseOutputStatus(phase.Status)
		worst = applyOutputStatus(worst, phaseStatus)
		parts = append(parts, phase.Label+" "+string(phaseStatus))
	}
	return worst, strings.Join(parts, ", ")
}

func applyPhaseOutputStatus(taskStatus workflow.TaskStatus) output.Status {
	switch taskStatus {
	case workflow.TaskStatusFailed:
		return output.StatusFailed
	case workflow.TaskStatusBlocked:
		return output.StatusBlocked
	case workflow.TaskStatusCancelled:
		return output.StatusCancel
	case workflow.TaskStatusRunning, workflow.TaskStatusReady:
		return output.StatusRunning
	case workflow.TaskStatusPending:
		return output.StatusPending
	case workflow.TaskStatusSkipped:
		return output.StatusSkipped
	default:
		return output.StatusOK
	}
}

func applyOutputStatus(current output.Status, next output.Status) output.Status {
	if applyOutputStatusPriority(next) > applyOutputStatusPriority(current) {
		return next
	}
	return current
}

func applyOutputStatusPriority(status output.Status) int {
	switch status {
	case output.StatusFailed, output.StatusFail:
		return 6
	case output.StatusBlocked:
		return 5
	case output.StatusCancel:
		return 4
	case output.StatusRunning, output.StatusWarn:
		return 3
	case output.StatusPending:
		return 2
	case output.StatusSkipped, output.StatusSkip:
		return 1
	default:
		return 0
	}
}

func applyProgressFields(ledger workflow.RunLedger) []output.ProgressField {
	fields := make([]output.ProgressField, 0, len(ledger.ProgressCounts()))
	for _, count := range ledger.ProgressCounts() {
		fields = append(fields, output.ProgressField{Label: string(count.Status), Count: count.Count})
	}
	return fields
}

// applyBlockedReason explains why a task is blocked in fleet terms. It walks the
// dependency chain to the actual failed root and, when that root lives in
// another cluster (the KubeVirt host parent), names that cluster — so a blocked
// child reads "host cluster dc1-metal-ocp not ready" instead of leaking a raw
// internal task ID or blaming a sibling task one hop up.
func applyBlockedReason(ledger workflow.RunLedger, task workflow.TaskLedgerEntry) string {
	if dep, ok := status.ApplyBlockingRoot(ledger, task); ok {
		label := applyTaskDisplayLabel(dep.Label)
		if dep.Cluster != "" && dep.Cluster != task.Cluster {
			return fmt.Sprintf("host cluster %s not ready (blocked by %s)", dep.Cluster, label)
		}
		return "blocked by " + label
	}
	if task.SkippedReason != "" {
		return task.SkippedReason
	}
	return fmt.Sprintf("%s %s", task.Kind, task.Status)
}
