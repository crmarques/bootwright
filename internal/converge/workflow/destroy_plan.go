package workflow

import (
	"fmt"
	"sort"
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

// DestroyStorageScopeExtraVar is the comma-separated allowlist of StorageCluster
// names the storage teardown is restricted to. It is the destroy mirror of
// apply's applyTarget.StorageClusterNames: the storage destroy playbook
// end_hosts any rendered storage node whose cluster is not in the allowlist, so
// a render-reference StorageCluster present in the (render-inclusive) state is
// never wiped. Emitted only when a --clusters selection narrows storage to a
// non-empty set; a selection that names no storage cluster drops the storage
// teardown step entirely, and no selection at all leaves it unset (tear down
// every storage host the state renders).
const DestroyStorageScopeExtraVar = "bootwright_destroy_storage_scope"

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
// storageWorkNames restricts the storage teardown to the directly-selected
// storage roots (the destroy mirror of apply's applyTarget.StorageClusterNames):
// nil means no --clusters narrowing (tear down every StorageCluster the state
// renders); a non-nil empty slice means a selection that names no storage
// cluster (drop the storage teardown step); a non-empty slice restricts teardown
// to those names via DestroyStorageScopeExtraVar.
func PlanDestroyTasks(scopeName string, state v1alpha1.State, limit string, extraVars []string, storageWorkNames []string) ([]ApplyTask, error) {
	switch strings.TrimSpace(scopeName) {
	case "infra":
		return destroyChain(state, limit, extraVars, infraDestroySteps(), storageWorkNames), nil
	case "clusters":
		return destroyChain(state, limit, extraVars, clusterDestroySteps(), storageWorkNames), nil
	case "all":
		// Whole-context teardown: tear the clusters down before the infra they
		// run on (the reverse of the apply order, infra then clusters). One
		// sequential chain so each step waits for the previous, exactly as the
		// stage chains do, with the clusters steps ahead of the infra steps.
		return destroyChain(state, limit, extraVars, append(clusterDestroySteps(), infraDestroySteps()...), storageWorkNames), nil
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
// depends on the previous emitted step), preserving the monolith's teardown
// order. When a --clusters selection narrows storage teardown, the storage step
// is dropped if no storage cluster is selected, or restricted to the selected
// roots via DestroyStorageScopeExtraVar otherwise; skipped steps do not break
// the dependency chain because prev only advances for emitted steps.
func destroyChain(state v1alpha1.State, limit string, extraVars []string, steps []destroyStep, storageWorkNames []string) []ApplyTask {
	tasks := make([]ApplyTask, 0, len(steps))
	prev := ""
	for _, step := range steps {
		stepExtraVars := extraVars
		resourceKeys := destroyStepClusters(state, step.kind)
		if step.kind == DestroyTaskKindStorageCluster && storageWorkNames != nil {
			if len(storageWorkNames) == 0 {
				// Selection names no storage cluster (e.g. a container-only scope):
				// tear down none, so emit no storage teardown step at all.
				continue
			}
			resourceKeys = append([]string(nil), storageWorkNames...)
			sort.Strings(resourceKeys)
			stepExtraVars = append(append([]string(nil), extraVars...), DestroyStorageScopeExtraVar+"="+strings.Join(resourceKeys, ","))
		}
		entry := TaskLedgerEntry{
			ID:           step.id,
			Kind:         step.kind,
			Label:        step.label,
			ResourceKeys: resourceKeys,
			Status:       TaskStatusPending,
		}
		if prev != "" {
			entry.Dependencies = []string{prev}
		}
		tasks = append(tasks, ApplyTask{
			Entry:         entry,
			Playbook:      step.playbook,
			Limit:         limit,
			ExtraVarPairs: append([]string(nil), stepExtraVars...),
			State:         state,
		})
		prev = step.id
	}
	return tasks
}

// destroyStepClusters lists the clusters a cluster-stage teardown step covers,
// so the ledger entry (and the progress frame/summary built from it) can name
// them. The host-group infra steps are not per-cluster, so they carry none.
func destroyStepClusters(state v1alpha1.State, kind string) []string {
	var names []string
	switch kind {
	case DestroyTaskKindStorageCluster:
		for _, cluster := range state.StorageClusters {
			names = append(names, cluster.Metadata.Name)
		}
	case DestroyTaskKindContainerCluster:
		for _, cluster := range state.ContainerClusters {
			names = append(names, cluster.Metadata.Name)
		}
	default:
		return nil
	}
	sort.Strings(names)
	return names
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
