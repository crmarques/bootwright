package workflow

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
)

const (
	DestroyTaskKindMachineRegistration = "destroyMachineRegistration"
	DestroyTaskKindMachineInfra        = "destroyMachineInfra"
	DestroyTaskKindInfraComponents     = "destroyInfraComponents"
	DestroyTaskKindProviderServices    = "destroyProviderServices"
	DestroyTaskKindStorageCluster      = "destroyStorageCluster"
	DestroyTaskKindContainerCluster    = "destroyContainerCluster"
	DestroyTaskKindStorageNodeAccess   = "destroyStorageNodeAccess"
)

const DestroyStorageScopeExtraVar = "bootwright_destroy_storage_scope"

const DestroyMachineScopeExtraVar = "bootwright_destroy_machine_scope"

const DestroyClusterOrderExtraVar = "bootwright_destroy_cluster_order"

func PlanDestroyTasks(scopeName string, state v1alpha1.State, limit string, extraVars []string, storageWorkNames []string) ([]ApplyTask, error) {
	if destroyMachineScoped(extraVars) {
		return destroyChain(state, limit, extraVars, infraMachineDestroySteps(), storageWorkNames)
	}
	switch strings.TrimSpace(scopeName) {
	case "infra":
		return destroyChain(state, limit, extraVars, infraDestroySteps(), storageWorkNames)
	case "clusters":
		return destroyChain(state, limit, extraVars, append(clusterDestroySteps(), storageNodeAccessDestroySteps()...), storageWorkNames)
	case "all":
		return destroyChain(state, limit, extraVars, fullDestroySteps(), storageWorkNames)
	default:
		return nil, fmt.Errorf("granular destroy is only supported for the infra, clusters, and all stages, not %q", scopeName)
	}
}

func infraDestroySteps() []destroyStep {
	return []destroyStep{
		{id: "destroy.machine-registration", kind: DestroyTaskKindMachineRegistration, label: "Machine registration", playbook: roles.PlaybookTaskMachineRegistrationDeregister, limit: render.GroupStorageHosts},
		{id: "destroy.infra-components", kind: DestroyTaskKindInfraComponents, label: "Infra component services", playbook: roles.PlaybookTaskInfraComponentServicesDestroy},
		{id: "destroy.machine-infra", kind: DestroyTaskKindMachineInfra, label: "Machine infrastructure", playbook: roles.PlaybookTaskMachineInfraDestroy},
		{id: "destroy.provider-services", kind: DestroyTaskKindProviderServices, label: "Provider services", playbook: roles.PlaybookTaskProviderServicesDestroy},
	}
}

func infraMachineDestroySteps() []destroyStep {
	return []destroyStep{
		{id: "destroy.machine-registration", kind: DestroyTaskKindMachineRegistration, label: "Machine registration", playbook: roles.PlaybookTaskMachineRegistrationDeregister, limit: render.GroupStorageHosts},
		{id: "destroy.machine-infra", kind: DestroyTaskKindMachineInfra, label: "Machine infrastructure", playbook: roles.PlaybookTaskMachineInfraDestroy},
	}
}

func fullDestroySteps() []destroyStep {
	return []destroyStep{
		{id: DestroyStorageClustersTaskID, kind: DestroyTaskKindStorageCluster, label: "Storage clusters", playbook: roles.PlaybookTaskStorageClusterDestroy},
		{id: "destroy.machine-registration", kind: DestroyTaskKindMachineRegistration, label: "Machine registration", playbook: roles.PlaybookTaskMachineRegistrationDeregister, limit: render.GroupStorageHosts},
		{id: "destroy.infra-components", kind: DestroyTaskKindInfraComponents, label: "Infra component services", playbook: roles.PlaybookTaskInfraComponentServicesDestroy},
		{id: "destroy.machine-infra", kind: DestroyTaskKindMachineInfra, label: "Machine infrastructure", playbook: roles.PlaybookTaskMachineInfraDestroy},
		{id: "destroy.container-clusters", kind: DestroyTaskKindContainerCluster, label: "Container clusters", playbook: roles.PlaybookTaskContainerClusterAgentDestroy, dependencies: []string{"destroy.machine-infra"}},
		{id: "destroy.provider-services", kind: DestroyTaskKindProviderServices, label: "Provider services", playbook: roles.PlaybookTaskProviderServicesDestroy},
		{id: "destroy.storage-node-access", kind: DestroyTaskKindStorageNodeAccess, label: "Storage node access", playbook: roles.PlaybookTaskStorageNodeAccessDestroy, limit: render.GroupStorageHosts},
	}
}

func destroyMachineScoped(extraVars []string) bool {
	prefix := DestroyMachineScopeExtraVar + "="
	for _, pair := range extraVars {
		if strings.HasPrefix(pair, prefix) {
			return true
		}
	}
	return false
}

const DestroyStorageClustersTaskID = "destroy.storage-clusters"

func clusterDestroySteps() []destroyStep {
	return []destroyStep{
		{id: DestroyStorageClustersTaskID, kind: DestroyTaskKindStorageCluster, label: "Storage clusters", playbook: roles.PlaybookTaskStorageClusterDestroy},
		{id: "destroy.container-clusters", kind: DestroyTaskKindContainerCluster, label: "Container clusters", playbook: roles.PlaybookTaskContainerClusterAgentDestroy},
	}
}

func storageNodeAccessDestroySteps() []destroyStep {
	return []destroyStep{
		{id: "destroy.storage-node-access", kind: DestroyTaskKindStorageNodeAccess, label: "Storage node access", playbook: roles.PlaybookTaskStorageNodeAccessDestroy, limit: render.GroupStorageHosts},
	}
}

type destroyStep struct {
	id           string
	kind         string
	label        string
	playbook     string
	limit        string
	dependencies []string
}

func destroyChain(state v1alpha1.State, limit string, extraVars []string, steps []destroyStep, storageWorkNames []string) ([]ApplyTask, error) {
	tasks := make([]ApplyTask, 0, len(steps))
	prev := ""
	for _, step := range steps {
		resourceKeys := destroyStepClusters(state, step.kind)
		if (step.kind == DestroyTaskKindStorageCluster || step.kind == DestroyTaskKindStorageNodeAccess) && storageWorkNames != nil {
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
			Dependencies: append([]string(nil), step.dependencies...),
		}
		if prev != "" {
			entry.OrderingDependencies = []string{prev}
		}
		taskLimit := limit
		if step.limit != "" {
			taskLimit = step.limit
		}
		taskExtraVars := append([]string(nil), extraVars...)
		forks := 0
		if step.kind == DestroyTaskKindMachineInfra {
			taskLimit = strings.Join([]string{
				render.GroupMachineTaskHosts,
				render.GroupProviderHosts,
				render.GroupInfraHosts,
			}, ":")
			forks = AnsibleForksForLimit(state, taskLimit)
			order, err := machineInfraDestroyOrder(state)
			if err != nil {
				return nil, err
			}
			if len(order) > 0 {
				taskExtraVars = append(taskExtraVars, DestroyClusterOrderExtraVar+"="+strings.Join(order, ","))
			}
		}
		tasks = append(tasks, ApplyTask{
			Entry:         entry,
			Playbook:      step.playbook,
			Limit:         taskLimit,
			Forks:         forks,
			ExtraVarPairs: taskExtraVars,
			State:         state,
		})
		prev = step.id
	}
	return tasks, nil
}

func machineInfraDestroyOrder(state v1alpha1.State) ([]string, error) {
	names := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		names[cluster.Metadata.Name] = true
	}
	for _, cluster := range state.StorageClusters {
		names[cluster.Metadata.Name] = true
	}
	machines := make(map[string]v1alpha1.Machine, len(state.Machines))
	for _, machine := range state.Machines {
		machines[machine.Metadata.Name] = machine
	}
	providers := make(map[string]v1alpha1.InfraProvider, len(state.InfraProviders))
	for _, provider := range state.InfraProviders {
		providers[provider.Metadata.Name] = provider
	}
	parents := map[string]map[string]bool{}
	incoming := map[string]int{}
	for name := range names {
		incoming[name] = 0
	}
	for _, cluster := range state.ContainerClusters {
		child := cluster.Metadata.Name
		for _, node := range cluster.Spec.Nodes {
			machine, ok := machines[node.MachineRef.Name]
			if !ok {
				continue
			}
			provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
			if !ok || provider.Spec.Type != v1alpha1.ProvisionerKubeVirt || provider.Spec.KubeVirt == nil || provider.Spec.KubeVirt.HostClusterRef == nil {
				continue
			}
			parent := provider.Spec.KubeVirt.HostClusterRef.Name
			if !names[parent] {
				continue
			}
			if parents[child] == nil {
				parents[child] = map[string]bool{}
			}
			if parents[child][parent] {
				continue
			}
			parents[child][parent] = true
			incoming[parent]++
		}
	}
	var ready []string
	for name, count := range incoming {
		if count == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)
	order := make([]string, 0, len(names))
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		order = append(order, name)
		var next []string
		for parent := range parents[name] {
			incoming[parent]--
			if incoming[parent] == 0 {
				next = append(next, parent)
			}
		}
		sort.Strings(next)
		ready = append(ready, next...)
		sort.Strings(ready)
	}
	if len(order) != len(names) {
		return nil, fmt.Errorf("cannot plan machine infrastructure destroy: KubeVirt hostClusterRef dependency cycle")
	}
	return order, nil
}

func destroyStepClusters(state v1alpha1.State, kind string) []string {
	var names []string
	switch kind {
	case DestroyTaskKindStorageCluster, DestroyTaskKindStorageNodeAccess:
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

func SucceededDestroyTaskKinds(ledger RunLedger) map[string]bool {
	out := map[string]bool{}
	for _, task := range ledger.Tasks {
		if task.Status == TaskStatusOK {
			out[task.Kind] = true
		}
	}
	return out
}

func DestroyScopeCoversStorage(scopeName string) bool {
	switch strings.TrimSpace(scopeName) {
	case "clusters", "all":
		return true
	default:
		return false
	}
}
