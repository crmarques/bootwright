package cli

import (
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

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
	collapsed := map[string]int{}
	grouped := map[int][]workflow.TaskLedgerEntry{}
	for _, task := range tasks {
		if task.Kind == workflow.ApplyTaskKindProvisioningPlaybook {
			key := task.Cluster + "\x00" + provisioningHookPoint(task.Label)
			if idx, ok := collapsed[key]; ok {
				grouped[idx] = append(grouped[idx], task)
				continue
			}
			idx := len(steps)
			collapsed[key] = idx
			grouped[idx] = []workflow.TaskLedgerEntry{task}
			steps = append(steps, output.Step{})
			continue
		}
		steps = append(steps, output.Step{
			ID:     task.ID,
			Label:  applyTaskDisplayLabel(task.Label),
			Status: applyStepStatus(task.Status),
			Detail: applyStepDetail(task, ledger),
		})
	}
	for idx, group := range grouped {
		steps[idx] = collapsedProvisioningStep(group, ledger)
	}
	return steps
}

func collapsedProvisioningStep(tasks []workflow.TaskLedgerEntry, ledger workflow.RunLedger) output.Step {
	hook := provisioningHookPoint(tasks[0].Label)
	label := "Custom playbooks"
	if hook != "" {
		label += " (" + hook + ")"
	}
	agg := aggregateProvisioningStatus(tasks)
	detail := ""
	for _, task := range tasks {
		if applyStepStatus(task.Status) == agg {
			detail = applyStepDetail(task, ledger)
			break
		}
	}
	return output.Step{
		ID:     "playbooks:" + tasks[0].Cluster + ":" + hook,
		Label:  label,
		Status: agg,
		Detail: detail,
		Count:  len(tasks),
	}
}

func aggregateProvisioningStatus(tasks []workflow.TaskLedgerEntry) output.Status {
	var failed, blocked, running, pending, done, skipped, cancelled bool
	for _, task := range tasks {
		switch applyStepStatus(task.Status) {
		case output.StatusFailed:
			failed = true
		case output.StatusBlocked:
			blocked = true
		case output.StatusRunning:
			running = true
		case output.StatusPending:
			pending = true
		case output.StatusDone:
			done = true
		case output.StatusSkipped:
			skipped = true
		case output.StatusCancel:
			cancelled = true
		}
	}
	switch {
	case failed:
		return output.StatusFailed
	case blocked:
		return output.StatusBlocked
	case running:
		return output.StatusRunning
	case pending && (done || skipped || cancelled):
		return output.StatusRunning
	case pending:
		return output.StatusPending
	case done:
		return output.StatusDone
	case cancelled:
		return output.StatusCancel
	default:
		return output.StatusSkipped
	}
}

func provisioningHookPoint(label string) string {
	open := strings.LastIndex(label, "(")
	closeIdx := strings.LastIndex(label, ")")
	if open >= 0 && closeIdx > open {
		return label[open+1 : closeIdx]
	}
	return ""
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
