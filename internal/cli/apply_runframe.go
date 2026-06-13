package cli

import (
	"sort"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// applyRunFrame projects a run ledger into a progress frame: a header progress
// bar plus, for each cluster, every ledger task as a step. Fabric/infra tasks
// that own no cluster lead in a non-cluster "infra" group so an infra-only run
// (apply --stage infra) still lists its steps. This single mapper feeds both the
// live apply reporter and `status --watch`, so the two views cannot diverge.
func applyRunFrame(ledger workflow.RunLedger) output.RunFrame {
	groups := make([]output.StepGroup, 0)
	if infra := applyNonClusterSteps(ledger); len(infra) > 0 {
		groups = append(groups, output.StepGroup{Title: "infra", Steps: infra})
	}
	for _, name := range ledger.ClusterNames() {
		tasks := ledger.TasksForCluster(name)
		title := name
		if kind := applyClusterKind(tasks); kind != "" {
			title = name + " (" + kind + ")"
		}
		groups = append(groups, output.StepGroup{Title: title, Steps: applyStepsForTasks(tasks)})
	}
	return output.RunFrame{
		BarLabel: "Fleet",
		Done:     applyProgressDone(ledger),
		Total:    len(ledger.Tasks),
		Counts:   applyProgressFields(ledger),
		Groups:   groups,
	}
}

func applyNonClusterSteps(ledger workflow.RunLedger) []output.Step {
	var tasks []workflow.TaskLedgerEntry
	for _, task := range ledger.Tasks {
		if task.Cluster == "" {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return applyStepsForTasks(tasks)
}

func applyStepsForTasks(tasks []workflow.TaskLedgerEntry) []output.Step {
	steps := make([]output.Step, 0, len(tasks))
	for _, task := range tasks {
		steps = append(steps, output.Step{
			ID:     task.ID,
			Label:  applyTaskDisplayLabel(task.Label),
			Status: applyStepStatus(task.Status),
			Detail: applyStepDetail(task),
		})
	}
	return steps
}

func applyStepStatus(status workflow.TaskStatus) output.Status {
	switch status {
	case workflow.TaskStatusOK:
		return output.StatusDone
	case workflow.TaskStatusRunning, workflow.TaskStatusReady:
		return output.StatusRunning
	case workflow.TaskStatusPending:
		return output.StatusPending
	case workflow.TaskStatusFailed:
		return output.StatusFailed
	case workflow.TaskStatusBlocked:
		return output.StatusBlocked
	case workflow.TaskStatusSkipped:
		return output.StatusSkipped
	case workflow.TaskStatusCancelled:
		return output.StatusCancel
	default:
		return output.StatusPending
	}
}

// applyStepDetail gives a short inline reason for a step that did not simply
// succeed; the full failure tail lives in the task log, surfaced by the Summary.
func applyStepDetail(task workflow.TaskLedgerEntry) string {
	switch task.Status {
	case workflow.TaskStatusFailed:
		return applyFailureReason(task.Failure)
	case workflow.TaskStatusBlocked:
		return applyBlockedReason(task)
	case workflow.TaskStatusSkipped:
		if task.SkippedReason != "" {
			return task.SkippedReason
		}
	}
	return ""
}
