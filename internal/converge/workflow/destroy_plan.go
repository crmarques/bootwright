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

const DestroyClusterLevelsExtraVar = "bootwright_destroy_cluster_levels"

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

const (
	destroyStorageNodeAccessTaskID   = "destroy.storage-node-access"
	destroyMachineRegistrationTaskID = "destroy.machine-registration"
	destroyInfraComponentsTaskID     = "destroy.infra-components"
	destroyMachineInfraTaskID        = "destroy.machine-infra"
	destroyContainerClustersTaskID   = "destroy.container-clusters"
	destroyProviderServicesTaskID    = "destroy.provider-services"
)

const destroyMaxForks = 20

func infraDestroySteps() []destroyStep {
	return []destroyStep{
		machineRegistrationDestroyStep(nil),
		infraComponentsDestroyStep([]string{destroyMachineRegistrationTaskID}),
		machineInfraDestroyStep([]string{destroyInfraComponentsTaskID}),
		providerServicesDestroyStep([]string{destroyMachineInfraTaskID}),
	}
}

func infraMachineDestroySteps() []destroyStep {
	return []destroyStep{
		machineRegistrationDestroyStep(nil),
		machineInfraDestroyStep([]string{destroyMachineRegistrationTaskID}),
	}
}

func fullDestroySteps() []destroyStep {
	return []destroyStep{
		storageClusterDestroyStep(nil),
		machineRegistrationDestroyStep([]string{DestroyStorageClustersTaskID}),
		storageNodeAccessDestroyStep([]string{destroyMachineRegistrationTaskID}),
		infraComponentsDestroyStep([]string{destroyStorageNodeAccessTaskID}),
		machineInfraDestroyStep([]string{destroyInfraComponentsTaskID}),
		containerClustersDestroyStep([]string{destroyMachineInfraTaskID}, []string{destroyMachineInfraTaskID}),
		providerServicesDestroyStep([]string{destroyMachineInfraTaskID, destroyContainerClustersTaskID}),
	}
}

func storageClusterDestroyStep(ordering []string) destroyStep {
	return destroyStep{
		id:                   DestroyStorageClustersTaskID,
		kind:                 DestroyTaskKindStorageCluster,
		label:                "Storage clusters",
		playbook:             roles.PlaybookTaskStorageClusterDestroy,
		limit:                render.GroupStorageHosts,
		forksLimit:           render.GroupStorageHosts,
		orderingDependencies: ordering,
	}
}

func machineRegistrationDestroyStep(ordering []string) destroyStep {
	return destroyStep{
		id:                   destroyMachineRegistrationTaskID,
		kind:                 DestroyTaskKindMachineRegistration,
		label:                "Machine registration",
		playbook:             roles.PlaybookTaskMachineRegistrationDeregister,
		limit:                render.GroupStorageHosts,
		forksLimit:           render.GroupStorageHosts,
		orderingDependencies: ordering,
	}
}

func storageNodeAccessDestroyStep(ordering []string) destroyStep {
	return destroyStep{
		id:                   destroyStorageNodeAccessTaskID,
		kind:                 DestroyTaskKindStorageNodeAccess,
		label:                "Storage node access",
		playbook:             roles.PlaybookTaskStorageNodeAccessDestroy,
		limit:                render.GroupStorageHosts,
		forksLimit:           render.GroupStorageHosts,
		orderingDependencies: ordering,
	}
}

func infraComponentsDestroyStep(ordering []string) destroyStep {
	return destroyStep{
		id:                   destroyInfraComponentsTaskID,
		kind:                 DestroyTaskKindInfraComponents,
		label:                "Infra component services",
		playbook:             roles.PlaybookTaskInfraComponentServicesDestroy,
		forksLimit:           render.GroupInfraComponentHosts,
		orderingDependencies: ordering,
	}
}

func machineInfraDestroyStep(ordering []string) destroyStep {
	return destroyStep{
		id:                   destroyMachineInfraTaskID,
		kind:                 DestroyTaskKindMachineInfra,
		label:                "Machine infrastructure",
		playbook:             roles.PlaybookTaskMachineInfraDestroy,
		orderingDependencies: ordering,
	}
}

func containerClustersDestroyStep(hard, ordering []string) destroyStep {
	return destroyStep{
		id:                   destroyContainerClustersTaskID,
		kind:                 DestroyTaskKindContainerCluster,
		label:                "Container clusters",
		playbook:             roles.PlaybookTaskContainerClusterAgentDestroy,
		forksLimit:           render.GroupOCPHosts,
		dependencies:         hard,
		orderingDependencies: ordering,
	}
}

func providerServicesDestroyStep(ordering []string) destroyStep {
	return destroyStep{
		id:                   destroyProviderServicesTaskID,
		kind:                 DestroyTaskKindProviderServices,
		label:                "Provider services",
		playbook:             roles.PlaybookTaskProviderServicesDestroy,
		forksLimit:           render.GroupProviderHosts,
		orderingDependencies: ordering,
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
		storageClusterDestroyStep(nil),
		containerClustersDestroyStep(nil, []string{DestroyStorageClustersTaskID}),
	}
}

func storageNodeAccessDestroySteps() []destroyStep {
	return []destroyStep{
		storageNodeAccessDestroyStep([]string{destroyContainerClustersTaskID}),
	}
}

type destroyStep struct {
	id                   string
	kind                 string
	label                string
	playbook             string
	limit                string
	forksLimit           string
	dependencies         []string
	orderingDependencies []string
}

func destroyChain(state v1alpha1.State, limit string, extraVars []string, steps []destroyStep, storageWorkNames []string) ([]ApplyTask, error) {
	planned, keys := plannedDestroySteps(state, steps, storageWorkNames)
	declared := make(map[string][]string, len(steps))
	emitted := make(map[string]bool, len(planned))
	for _, step := range steps {
		declared[step.id] = step.orderingDependencies
	}
	for _, step := range planned {
		emitted[step.id] = true
	}
	tasks := make([]ApplyTask, 0, len(planned))
	for _, step := range planned {
		entry := TaskLedgerEntry{
			ID:                   step.id,
			Kind:                 step.kind,
			Label:                step.label,
			ResourceKeys:         keys[step.id],
			Status:               TaskStatusPending,
			Dependencies:         append([]string(nil), step.dependencies...),
			OrderingDependencies: destroyOrderingDependencies(step, declared, emitted),
		}
		taskLimit := limit
		if step.limit != "" {
			taskLimit = step.limit
		}
		taskExtraVars := append([]string(nil), extraVars...)
		if step.kind == DestroyTaskKindMachineInfra {
			taskLimit = machineInfraDestroyLimit()
			levels, err := machineInfraDestroyLevels(state)
			if err != nil {
				return nil, err
			}
			if order := flattenDestroyLevels(levels); len(order) > 0 {
				taskExtraVars = append(taskExtraVars,
					DestroyClusterOrderExtraVar+"="+strings.Join(order, ","),
					DestroyClusterLevelsExtraVar+"="+joinDestroyLevels(levels),
				)
			}
		}
		tasks = append(tasks, ApplyTask{
			Entry:         entry,
			Playbook:      step.playbook,
			Limit:         taskLimit,
			Forks:         destroyStepForks(state, step, taskLimit),
			ExtraVarPairs: taskExtraVars,
			State:         state,
		})
	}
	return tasks, nil
}

func plannedDestroySteps(state v1alpha1.State, steps []destroyStep, storageWorkNames []string) ([]destroyStep, map[string][]string) {
	planned := make([]destroyStep, 0, len(steps))
	keys := make(map[string][]string, len(steps))
	for _, step := range steps {
		resourceKeys := destroyStepClusters(state, step.kind)
		if (step.kind == DestroyTaskKindStorageCluster || step.kind == DestroyTaskKindStorageNodeAccess) && storageWorkNames != nil {
			if len(storageWorkNames) == 0 {
				continue
			}
			resourceKeys = append([]string(nil), storageWorkNames...)
			sort.Strings(resourceKeys)
		}
		planned = append(planned, step)
		keys[step.id] = resourceKeys
	}
	return planned, keys
}

func destroyOrderingDependencies(step destroyStep, declared map[string][]string, emitted map[string]bool) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(ids []string)
	walk = func(ids []string) {
		for _, id := range ids {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			if emitted[id] {
				out = append(out, id)
				continue
			}
			walk(declared[id])
		}
	}
	walk(step.orderingDependencies)
	return out
}

func machineInfraDestroyLimit() string {
	return strings.Join([]string{
		render.GroupMachineTaskHosts,
		render.GroupProviderHosts,
		render.GroupInfraHosts,
	}, ":")
}

func destroyStepForks(state v1alpha1.State, step destroyStep, taskLimit string) int {
	forksLimit := step.forksLimit
	if strings.TrimSpace(forksLimit) == "" {
		forksLimit = taskLimit
	}
	forks := AnsibleForksForLimit(state, forksLimit)
	if forks > destroyMaxForks {
		return destroyMaxForks
	}
	if forks < 1 {
		return 1
	}
	return forks
}

func flattenDestroyLevels(levels [][]string) []string {
	var out []string
	for _, level := range levels {
		out = append(out, level...)
	}
	return out
}

func joinDestroyLevels(levels [][]string) string {
	joined := make([]string, 0, len(levels))
	for _, level := range levels {
		joined = append(joined, strings.Join(level, ","))
	}
	return strings.Join(joined, ";")
}

func machineInfraDestroyLevels(state v1alpha1.State) ([][]string, error) {
	names, parents := machineInfraDestroyGraph(state)
	incoming := make(map[string]int, len(names))
	for name := range names {
		incoming[name] = 0
	}
	for _, hosts := range parents {
		for parent := range hosts {
			incoming[parent]++
		}
	}
	var levels [][]string
	placed := 0
	for placed < len(names) {
		var level []string
		for name := range names {
			if incoming[name] == 0 {
				level = append(level, name)
			}
		}
		if len(level) == 0 {
			return nil, fmt.Errorf("cannot plan machine infrastructure destroy: KubeVirt hostClusterRef dependency cycle")
		}
		sort.Strings(level)
		for _, name := range level {
			incoming[name] = -1
		}
		for _, name := range level {
			for parent := range parents[name] {
				if incoming[parent] > 0 {
					incoming[parent]--
				}
			}
		}
		levels = append(levels, level)
		placed += len(level)
	}
	return levels, nil
}

func machineInfraDestroyGraph(state v1alpha1.State) (map[string]bool, map[string]map[string]bool) {
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
			parents[child][parent] = true
		}
	}
	return names, parents
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
