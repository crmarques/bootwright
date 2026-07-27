package status

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

const (
	PhaseSharedServices    = "Shared services"
	PhaseMachines          = "Machines"
	PhasePrerequisites     = "Prerequisites"
	PhaseClusterInstall    = "Cluster install"
	PhaseAddOns            = "Add-ons"
	PhaseCustomPlaybooks   = "Custom playbooks"
	PhaseStorageClusters   = "Storage clusters"
	PhaseContainerClusters = "Container clusters"
	PhaseWork              = "Work"
)

type RunPhase struct {
	Label  string
	Status workflow.TaskStatus
	Tasks  []workflow.TaskLedgerEntry
}

func RunPhases(tasks []workflow.TaskLedgerEntry) []RunPhase {
	var phases []RunPhase
	index := map[string]int{}
	for _, task := range TasksInDisplayOrder(tasks) {
		label := TaskPhaseLabel(task)
		if i, ok := index[label]; ok {
			phases[i].Tasks = append(phases[i].Tasks, task)
			continue
		}
		index[label] = len(phases)
		phases = append(phases, RunPhase{Label: label, Tasks: []workflow.TaskLedgerEntry{task}})
	}
	for i := range phases {
		phases[i].Status = applyPhaseStatus(phases[i].Tasks)
	}
	return phases
}

func TaskPhaseLabel(task workflow.TaskLedgerEntry) string {
	switch task.Kind {
	case workflow.ApplyTaskKindProvider, workflow.ApplyTaskKindInfraComponentServices,
		workflow.DestroyTaskKindProviderServices, workflow.DestroyTaskKindInfraComponents:
		return PhaseSharedServices
	case workflow.ApplyTaskKindMachineInfraPrepare, workflow.ApplyTaskKindClusterInstall,
		workflow.ApplyTaskKindMachineInfraFinalize, workflow.ApplyTaskKindManagedMachineOS,
		workflow.ApplyTaskKindMachineRegistration, workflow.ApplyTaskKindStorageNodeAccess,
		workflow.DestroyTaskKindMachineInfra, workflow.DestroyTaskKindMachineRegistration,
		workflow.DestroyTaskKindStorageNodeAccess:
		return PhaseMachines
	case workflow.ApplyTaskKindClusterISO, workflow.ApplyTaskKindHostVirtctl, workflow.ApplyTaskKindStorageInfra:
		return PhasePrerequisites
	case workflow.ApplyTaskKindNodeBoot, workflow.ApplyTaskKindInstallWait, workflow.ApplyTaskKindStorageCluster:
		return PhaseClusterInstall
	case workflow.ApplyTaskKindClusterAddon, workflow.ApplyTaskKindNodeConfigApply:
		return PhaseAddOns
	case workflow.ApplyTaskKindPlaybook:
		if hook := PlaybookHookPoint(task.Label); hook != "" {
			return PhaseCustomPlaybooks + " (" + hook + ")"
		}
		return PhaseCustomPlaybooks
	case workflow.DestroyTaskKindStorageCluster:
		return PhaseStorageClusters
	case workflow.DestroyTaskKindContainerCluster:
		return PhaseContainerClusters
	default:
		return PhaseWork
	}
}

func PlaybookHookPoint(label string) string {
	open := strings.LastIndex(label, "(")
	closeIdx := strings.LastIndex(label, ")")
	if open >= 0 && closeIdx > open {
		return label[open+1 : closeIdx]
	}
	return ""
}

func TasksInDisplayOrder(tasks []workflow.TaskLedgerEntry) []workflow.TaskLedgerEntry {
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

func TaskUnitName(task workflow.TaskLedgerEntry) string {
	if task.Node != "" {
		return task.Node
	}
	switch task.Kind {
	case workflow.ApplyTaskKindClusterAddon:
		return strings.TrimPrefix(task.Label, "addon ")
	case workflow.ApplyTaskKindPlaybook:
		name := strings.TrimPrefix(task.Label, "playbook ")
		if open := strings.LastIndex(name, " ("); open > 0 {
			name = name[:open]
		}
		return name
	default:
		return ""
	}
}

func PhaseSettledTasks(phase RunPhase) int {
	settled := 0
	for _, task := range phase.Tasks {
		switch task.Status {
		case workflow.TaskStatusOK, workflow.TaskStatusSkipped, workflow.TaskStatusFailed,
			workflow.TaskStatusBlocked, workflow.TaskStatusCancelled:
			settled++
		}
	}
	return settled
}
