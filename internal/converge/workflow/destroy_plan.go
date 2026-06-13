package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/roles"
)

// Destroy task kinds. Destroy reuses the ApplyTask/TaskLedgerEntry shape and the
// apply scheduler; only the per-task executor (runOneDestroyTask) and the plan
// differ.
const (
	DestroyTaskKindMachineInfra     = "destroyMachineInfra"
	DestroyTaskKindInfraComponents  = "destroyInfraComponents"
	DestroyTaskKindProviderServices = "destroyProviderServices"
	DestroyTaskKindStorageCluster   = "destroyStorageCluster"
	DestroyTaskKindContainerCluster = "destroyContainerCluster"
)

// PlanDestroyTasks decomposes a scoped destroy into the task graph the apply
// scheduler runs. The workflow_*_destroy playbooks are thin import wrappers that
// run the task playbooks in teardown order in a single ansible process; this
// rebuilds that order as a sequential dependency chain of individual graph
// tasks so destroy shows granular progress.
//
// Safety: every task reuses the run's limit and extra-vars unchanged, and each
// task playbook restricts itself with its own hosts: selector, so a task sees
// exactly the hosts it would have inside the monolithic playbook. Per-cluster
// teardown (e.g. which Ceph cluster a storage host belongs to) is driven by
// rendered per-host inventory vars, not by per-task parameters, so running a
// task playbook once against its host group tears down every cluster correctly.
func PlanDestroyTasks(scopeName string, state v1alpha1.State, limit string, extraVars []string) ([]ApplyTask, error) {
	switch strings.TrimSpace(scopeName) {
	case "infra":
		return destroyChain(state, limit, extraVars, infraDestroySteps()), nil
	case "clusters":
		return destroyChain(state, limit, extraVars, clusterDestroySteps()), nil
	case "all":
		// Whole-context teardown: tear the clusters down before the infra they
		// run on (the reverse of the apply order, infra then clusters). One
		// sequential chain so each step waits for the previous, exactly as the
		// stage chains do, with the clusters steps ahead of the infra steps.
		return destroyChain(state, limit, extraVars, append(clusterDestroySteps(), infraDestroySteps()...)), nil
	default:
		return nil, fmt.Errorf("granular destroy is only supported for the infra, clusters, and all stages, not %q", scopeName)
	}
}

func infraDestroySteps() []destroyStep {
	return []destroyStep{
		{id: "destroy.machine-infra", kind: DestroyTaskKindMachineInfra, label: "Machine infrastructure", playbook: roles.PlaybookTaskMachineInfraDestroy},
		{id: "destroy.infra-components", kind: DestroyTaskKindInfraComponents, label: "Infra component services", playbook: roles.PlaybookTaskInfraComponentServicesDestroy},
		{id: "destroy.provider-services", kind: DestroyTaskKindProviderServices, label: "Provider services", playbook: roles.PlaybookTaskProviderServicesDestroy},
	}
}

func clusterDestroySteps() []destroyStep {
	return []destroyStep{
		{id: "destroy.storage-clusters", kind: DestroyTaskKindStorageCluster, label: "Storage clusters", playbook: roles.PlaybookTaskStorageClusterDestroy},
		{id: "destroy.container-clusters", kind: DestroyTaskKindContainerCluster, label: "Container clusters", playbook: roles.PlaybookTaskContainerClusterAgentDestroy},
	}
}

type destroyStep struct {
	id       string
	kind     string
	label    string
	playbook string
}

// destroyChain turns the ordered steps into a sequential task chain (each task
// depends on the previous), preserving the monolith's teardown order.
func destroyChain(state v1alpha1.State, limit string, extraVars []string, steps []destroyStep) []ApplyTask {
	tasks := make([]ApplyTask, 0, len(steps))
	prev := ""
	for _, step := range steps {
		entry := TaskLedgerEntry{
			ID:     step.id,
			Kind:   step.kind,
			Label:  step.label,
			Status: TaskStatusPending,
		}
		if prev != "" {
			entry.Dependencies = []string{prev}
		}
		tasks = append(tasks, ApplyTask{
			Entry:         entry,
			Playbook:      step.playbook,
			Limit:         limit,
			ExtraVarPairs: append([]string(nil), extraVars...),
			State:         state,
		})
		prev = step.id
	}
	return tasks
}

// PrepareDestroyTaskGraph mints the destroy run ID and resolves concurrency
// limits. Unlike apply it does no install-state reconcile: destroy has no
// per-cluster install state to advance.
func PrepareDestroyTaskGraph(runsDir string, opts RunOptions, tasks []ApplyTask, limits ConcurrencyLimits) (PreparedApplyTaskGraph, error) {
	startedAt := time.Now()
	if strings.TrimSpace(opts.ClustersDir) == "" {
		return PreparedApplyTaskGraph{}, fmt.Errorf("clusters dir is required")
	}
	if strings.TrimSpace(opts.RenderedDir) == "" {
		return PreparedApplyTaskGraph{}, fmt.Errorf("rendered dir is required")
	}
	if strings.TrimSpace(runsDir) == "" {
		return PreparedApplyTaskGraph{}, fmt.Errorf("runs dir is required")
	}
	return PreparedApplyTaskGraph{
		RunID:     destroyRunID(startedAt),
		StartedAt: startedAt,
		Tasks:     tasks,
		Limits:    ResolveApplyConcurrencyLimits(limits, tasks),
	}, nil
}

func destroyRunID(now time.Time) string {
	return "destroy-" + now.UTC().Format("20060102T150405.000000000Z")
}
