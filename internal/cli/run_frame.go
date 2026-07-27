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
			case output.StatusDone, output.StatusSkipped, output.StatusFailed,
				output.StatusBlocked, output.StatusCancel:
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
		return strings.Join(parts, " — ")
	}
	if opts.units {
		return runPhaseUnits(phase)
	}
	if opts.progress {
		if settled := status.PhaseSettledTasks(phase); settled > 0 && settled < len(phase.Tasks) {
			return fmt.Sprintf("%d/%d", settled, len(phase.Tasks))
		}
	}
	return ""
}

func phaseNamesItsResources(phase status.RunPhase) bool {
	switch phase.Label {
	case status.PhaseStorageClusters, status.PhaseClusterRuntime, status.PhaseContainerClusters:
		return true
	default:
		return false
	}
}

func runPhaseUnits(phase status.RunPhase) string {
	var units []string
	seen := map[string]bool{}
	for _, task := range phase.Tasks {
		unit := status.TaskUnitName(task)
		if unit == "" || seen[unit] {
			continue
		}
		seen[unit] = true
		units = append(units, unit)
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
		for _, key := range task.ResourceKeys {
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
