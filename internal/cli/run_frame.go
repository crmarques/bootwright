package cli

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

const runGroupInfrastructure = "Infrastructure"

type phaseDetailOptions struct {
	progress  bool
	resources bool
	units     bool
}

const phaseUnitListLimit = 6

func newRunFrame(barLabel string, groups []output.StepGroup) output.RunFrame {
	done, total := 0, 0
	counts := map[output.Status]int{}
	for _, group := range groups {
		for _, step := range group.Steps {
			total++
			counts[step.Status]++
			switch step.Status {
			case output.StatusDone, output.StatusSkipped:
				done++
			}
		}
	}
	return output.RunFrame{
		BarLabel: barLabel,
		Done:     done,
		Total:    total,
		Counts:   runProgressFields(counts),
		Groups:   groups,
	}
}

func runProgressFields(counts map[output.Status]int) []output.ProgressField {
	order := []output.Status{
		output.StatusDone,
		output.StatusRunning,
		output.StatusPending,
		output.StatusBlocked,
		output.StatusSkipped,
		output.StatusFailed,
		output.StatusCancel,
	}
	fields := make([]output.ProgressField, 0, len(order))
	for _, s := range order {
		if counts[s] > 0 {
			fields = append(fields, output.ProgressField{Label: string(s), Count: counts[s]})
		}
	}
	return fields
}

func runPhaseSteps(cluster string, tasks []workflow.TaskLedgerEntry, ledger workflow.RunLedger, opts phaseDetailOptions) []output.Step {
	phases := status.RunPhases(tasks)
	steps := make([]output.Step, 0, len(phases))
	for _, phase := range phases {
		steps = append(steps, output.Step{
			ID:     cluster + "|" + phase.Label,
			Label:  phase.Label,
			Status: applyStepStatus(phase.Status),
			Detail: runPhaseDetail(phase, ledger, opts),
		})
	}
	return steps
}

func runPhaseDetail(phase status.RunPhase, ledger workflow.RunLedger, opts phaseDetailOptions) string {
	var parts []string
	if opts.resources && phaseNamesItsResources(phase) {
		if keys := runPhaseResourceKeys(phase); keys != "" {
			parts = append(parts, keys)
		}
	}
	if reason := runPhaseReason(phase, ledger); reason != "" {
		parts = append(parts, reason)
	}
	if len(parts) > 0 {
		if opts.progress {
			if unsettled := runPhaseUnsettledDetail(phase, ledger); unsettled != "" {
				parts = append(parts, unsettled)
			}
		}
		return strings.Join(parts, " — ")
	}
	if opts.units {
		return runPhaseUnits(phase)
	}
	if opts.progress {
		if activity := runPhaseActivity(phase, ledger); activity != "" {
			parts = append(parts, activity)
		}
		if settled := status.PhaseSettledTasks(phase); settled > 0 && settled < len(phase.Tasks) {
			parts = append(parts, fmt.Sprintf("%d/%d", settled, len(phase.Tasks)))
		}
		return strings.Join(parts, "  ")
	}
	return ""
}

func runPhaseUnsettledDetail(phase status.RunPhase, ledger workflow.RunLedger) string {
	settled := status.PhaseSettledTasks(phase)
	if settled >= len(phase.Tasks) {
		return ""
	}
	var parts []string
	for _, task := range phase.Tasks {
		if task.Status == workflow.TaskStatusRunning {
			parts = append(parts, applyTaskDisplayLabel(task.Label))
			break
		}
	}
	if len(parts) == 0 {
		if wait := runPhaseSlotWait(phase); wait != "" {
			parts = append(parts, wait)
		}
	}
	parts = append(parts, fmt.Sprintf("%d/%d settled", settled, len(phase.Tasks)))
	return strings.Join(parts, "  ")
}

func runPhaseActivity(phase status.RunPhase, ledger workflow.RunLedger) string {
	switch phase.Status {
	case workflow.TaskStatusRunning:
		for _, task := range phase.Tasks {
			if task.Status == workflow.TaskStatusRunning {
				return applyTaskDisplayLabel(task.Label)
			}
		}
		if waiting := runPhaseWaitingOn(phase, ledger); waiting != "" {
			return waiting
		}
		return runPhaseSlotWait(phase)
	case workflow.TaskStatusPending:
		if waiting := runPhaseWaitingOn(phase, ledger); waiting != "" {
			return waiting
		}
		return runPhaseSlotWait(phase)
	default:
		return ""
	}
}

func runPhaseSlotWait(phase status.RunPhase) string {
	for _, task := range phase.Tasks {
		if task.Status != workflow.TaskStatusPending || task.ReadyAt == nil || len(task.BlockedOn) == 0 {
			continue
		}
		if wait := runBudgetWaitLabel(task.BlockedOn[len(task.BlockedOn)-1]); wait != "" {
			return wait
		}
	}
	return ""
}

func runBudgetWaitLabel(blocker string) string {
	switch {
	case blocker == workflow.ApplyClusterInstallBlocker:
		return "waiting for a cluster install slot"
	case blocker == "redfish budget":
		return "waiting for Redfish slots"
	case blocker == "task budget":
		return "waiting for a task slot"
	case strings.HasPrefix(blocker, "host slot "):
		return "waiting for a slot on " + strings.TrimPrefix(blocker, "host slot ")
	case strings.HasPrefix(blocker, "resource "):
		return "waiting on " + strings.TrimPrefix(blocker, "resource ")
	default:
		return ""
	}
}

func runPhaseWaitingOn(phase status.RunPhase, ledger workflow.RunLedger) string {
	var shared workflow.TaskLedgerEntry
	haveShared := false
	for _, task := range phase.Tasks {
		for _, dependencySet := range []struct {
			ids             []string
			requiresSuccess bool
		}{
			{ids: task.Dependencies},
			{ids: task.SuccessDependencies, requiresSuccess: true},
		} {
			for _, dep := range dependencySet.ids {
				blocker, ok := ledger.Task(dep)
				if !ok || runPhaseDependencySettled(blocker, dependencySet.requiresSuccess) {
					continue
				}
				if blocker.Cluster == task.Cluster {
					return ""
				}
				if blocker.Cluster != "" {
					return "waiting on " + blocker.Cluster + ": " + status.TaskPhaseLabel(blocker)
				}
				if !haveShared {
					shared = blocker
					haveShared = true
				}
			}
		}
	}
	if haveShared {
		return "waiting on " + status.TaskPhaseLabel(shared)
	}
	return ""
}

func runPhaseDependencySettled(task workflow.TaskLedgerEntry, requiresSuccess bool) bool {
	return task.Status == workflow.TaskStatusOK || !requiresSuccess && task.Status == workflow.TaskStatusSkipped
}

func phaseNamesItsResources(phase status.RunPhase) bool {
	switch phase.Label {
	case status.PhaseStorageClusters, status.PhaseMachines, status.PhaseContainerClusters:
		return true
	default:
		return false
	}
}

func runPhaseUnits(phase status.RunPhase) string {
	var units []string
	seen := map[string]bool{}
	for _, task := range phase.Tasks {
		for _, unit := range status.TaskUnitNames(task) {
			if unit == "" || seen[unit] {
				continue
			}
			seen[unit] = true
			units = append(units, unit)
		}
	}
	if len(units) > phaseUnitListLimit {
		rest := len(units) - phaseUnitListLimit
		units = append(units[:phaseUnitListLimit:phaseUnitListLimit], fmt.Sprintf("+%d more", rest))
	}
	return strings.Join(units, ", ")
}

func runPhaseResourceKeys(phase status.RunPhase) string {
	var keys []string
	seen := map[string]bool{}
	for _, task := range phase.Tasks {
		for _, key := range workflow.DestroyTaskClusterKeys(task) {
			if seen[key] {
				continue
			}
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return strings.Join(keys, ", ")
}

func runPhaseReason(phase status.RunPhase, ledger workflow.RunLedger) string {
	for _, task := range phase.Tasks {
		if applyStepStatus(task.Status) != applyStepStatus(phase.Status) {
			continue
		}
		reason := applyStepDetail(task, ledger)
		if reason == "" {
			continue
		}
		if len(phase.Tasks) == 1 {
			return reason
		}
		return applyTaskDisplayLabel(task.Label) + ": " + reason
	}
	return ""
}
