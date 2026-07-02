package cli

import (
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

// applyRunFrame projects a run ledger into a progress frame: a header progress
// bar plus, for each cluster, every ledger task as a step. Fabric/infra tasks
// that own no cluster lead in a non-cluster "infra" group so an infra-only run
// (apply --stage infra) still lists its steps. This single mapper feeds both the
// live apply reporter and `status --watch`, so the two views cannot diverge.
//
// displays carries the desired-state descriptor/ordering metadata so the group
// headings distinguish a bare-metal cluster from a KubeVirt-hosted one and a
// child is ordered after its host parent. It may be nil (no state loaded), in
// which case headings fall back to the ledger-derived kind word and ordering
// falls back to alphabetical.
func applyRunFrame(ledger workflow.RunLedger, displays map[string]clusterDisplay) output.RunFrame {
	groups := make([]output.StepGroup, 0)
	if infra := applyNonClusterSteps(ledger); len(infra) > 0 {
		groups = append(groups, output.StepGroup{Title: "infra", Steps: infra})
	}
	for _, name := range orderClusterNames(ledger.ClusterNames(), displays) {
		tasks := ledger.TasksForCluster(name)
		title := clusterGroupTitle(name, displays, status.ApplyClusterKind(tasks))
		groups = append(groups, output.StepGroup{Title: title, Steps: applyStepsForTasks(tasks, ledger)})
	}
	return output.RunFrame{
		BarLabel: "Provisioning Progress",
		Done:     status.ApplyProgressDone(ledger),
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
	return applyStepsForTasks(tasks, ledger)
}

func applyStepsForTasks(tasks []workflow.TaskLedgerEntry, ledger workflow.RunLedger) []output.Step {
	tasks = applyTasksInDisplayOrder(tasks)
	steps := make([]output.Step, 0, len(tasks))
	for _, task := range tasks {
		steps = append(steps, output.Step{
			ID:     task.ID,
			Label:  applyTaskDisplayLabel(task.Label),
			Status: applyStepStatus(task.Status),
			Detail: applyStepDetail(task, ledger),
		})
	}
	return steps
}

func applyTasksInDisplayOrder(tasks []workflow.TaskLedgerEntry) []workflow.TaskLedgerEntry {
	if len(tasks) < 2 {
		return tasks
	}
	byID := map[string]workflow.TaskLedgerEntry{}
	indegree := map[string]int{}
	dependents := map[string][]string{}
	for _, task := range tasks {
		byID[task.ID] = task
		indegree[task.ID] = 0
	}
	for _, task := range tasks {
		seen := map[string]bool{}
		for _, dep := range task.Dependencies {
			if _, ok := byID[dep]; !ok || seen[dep] {
				continue
			}
			seen[dep] = true
			indegree[task.ID]++
			dependents[dep] = append(dependents[dep], task.ID)
		}
	}
	ready := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if indegree[task.ID] == 0 {
			ready = append(ready, task.ID)
		}
	}
	ordered := make([]workflow.TaskLedgerEntry, 0, len(tasks))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byID[id])
		for _, dependent := range dependents[id] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	if len(ordered) != len(tasks) {
		return tasks
	}
	return ordered
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
