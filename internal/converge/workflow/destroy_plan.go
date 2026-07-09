package workflow

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/roles"
)

const (
	DestroyTaskKindMachineInfra     = "destroyMachineInfra"
	DestroyTaskKindInfraComponents  = "destroyInfraComponents"
	DestroyTaskKindProviderServices = "destroyProviderServices"
	DestroyTaskKindStorageCluster   = "destroyStorageCluster"
	DestroyTaskKindContainerCluster = "destroyContainerCluster"
)

const DestroyStorageScopeExtraVar = "bootwright_destroy_storage_scope"

func PlanDestroyTasks(scopeName string, state v1alpha1.State, limit string, extraVars []string, storageWorkNames []string) ([]ApplyTask, error) {
	switch strings.TrimSpace(scopeName) {
	case "infra":
		return destroyChain(state, limit, extraVars, infraDestroySteps(), storageWorkNames), nil
	case "clusters":
		return destroyChain(state, limit, extraVars, clusterDestroySteps(), storageWorkNames), nil
	case "all":
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

const DestroyStorageClustersTaskID = "destroy.storage-clusters"

func clusterDestroySteps() []destroyStep {
	return []destroyStep{
		{id: DestroyStorageClustersTaskID, kind: DestroyTaskKindStorageCluster, label: "Storage clusters", playbook: roles.PlaybookTaskStorageClusterDestroy},
		{id: "destroy.container-clusters", kind: DestroyTaskKindContainerCluster, label: "Container clusters", playbook: roles.PlaybookTaskContainerClusterAgentDestroy},
	}
}

type destroyStep struct {
	id       string
	kind     string
	label    string
	playbook string
}

func destroyChain(state v1alpha1.State, limit string, extraVars []string, steps []destroyStep, storageWorkNames []string) []ApplyTask {
	tasks := make([]ApplyTask, 0, len(steps))
	prev := ""
	for _, step := range steps {
		resourceKeys := destroyStepClusters(state, step.kind)
		if step.kind == DestroyTaskKindStorageCluster && storageWorkNames != nil {
			if len(storageWorkNames) == 0 {
				continue
			}
			resourceKeys = append([]string(nil), storageWorkNames...)
			sort.Strings(resourceKeys)
		}
		entry := TaskLedgerEntry{
			ID:           step.id,
			Kind:         step.kind,
			Label:        step.label,
			ResourceKeys: resourceKeys,
			Status:       TaskStatusPending,
		}
		if prev != "" {
			entry.OrderingDependencies = []string{prev}
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
