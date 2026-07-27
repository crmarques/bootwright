package cli

import (
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func applyRunFrame(ledger workflow.RunLedger, displays map[string]clusterDisplay) output.RunFrame {
	return newRunFrame("Provisioning", applyStepGroups(ledger, displays, phaseDetailOptions{progress: true}))
}

func applyPlanGroups(tasks []workflow.TaskLedgerEntry, displays map[string]clusterDisplay) []output.StepGroup {
	return applyStepGroups(workflow.RunLedger{Tasks: tasks}, displays, phaseDetailOptions{units: true})
}

func applyStepGroups(ledger workflow.RunLedger, displays map[string]clusterDisplay, opts phaseDetailOptions) []output.StepGroup {
	groups := make([]output.StepGroup, 0)
	if infra := runPhaseSteps("", applyNonClusterTasks(ledger), ledger, opts); len(infra) > 0 {
		groups = append(groups, output.StepGroup{Title: runGroupInfrastructure, Steps: infra})
	}
	for _, name := range orderClusterNames(ledger.ClusterNames(), displays) {
		tasks := ledger.TasksForCluster(name)
		groups = append(groups, output.StepGroup{
			Title: clusterGroupTitle(name, displays, status.ApplyClusterKind(tasks)),
			Steps: runPhaseSteps(name, tasks, ledger, opts),
		})
	}
	return groups
}

func applyNonClusterTasks(ledger workflow.RunLedger) []workflow.TaskLedgerEntry {
	var tasks []workflow.TaskLedgerEntry
	for _, task := range ledger.Tasks {
		if task.Cluster == "" {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func applyStepStatus(taskStatus workflow.TaskStatus) output.Status {
	switch taskStatus {
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

func applyStepDetail(task workflow.TaskLedgerEntry, ledger workflow.RunLedger) string {
	switch task.Status {
	case workflow.TaskStatusFailed:
		return status.ApplyFailureReason(task.Failure)
	case workflow.TaskStatusBlocked:
		return applyBlockedReason(ledger, task)
	case workflow.TaskStatusSkipped:
		if task.SkippedReason != "" {
			return task.SkippedReason
		}
	}
	return ""
}
