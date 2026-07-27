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
	baseID               string
	kind                 string
	label                string
	playbook             string
	limit                string
	forksLimit           string
	resourceKeys         []string
	dependencies         []string
	orderingDependencies []string
}

func (s destroyStep) base() string {
	if s.baseID != "" {
		return s.baseID
	}
	return s.id
}

func destroyChain(state v1alpha1.State, limit string, extraVars []string, steps []destroyStep, storageWorkNames []string) ([]ApplyTask, error) {
	planned := plannedDestroySteps(state, steps, storageWorkNames)
	declared := make(map[string][]string, len(steps))
	emitted := make(map[string][]string, len(planned))
	for _, step := range steps {
		declared[step.id] = step.orderingDependencies
	}
	for _, step := range planned {
		declared[step.id] = step.orderingDependencies
	}
	for _, step := range planned {
		base := step.base()
		emitted[base] = append(emitted[base], step.id)
		if step.id != base {
			emitted[step.id] = []string{step.id}
		}
	}
	tasks := make([]ApplyTask, 0, len(planned))
	for _, step := range planned {
		entry := TaskLedgerEntry{
			ID:                   step.id,
			Kind:                 step.kind,
			Label:                step.label,
			ResourceKeys:         step.resourceKeys,
			Status:               TaskStatusPending,
			Dependencies:         destroyEmittedDependencies(step.dependencies, emitted),
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
	if err := detectDestroyTaskCycle(tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func detectDestroyTaskCycle(tasks []ApplyTask) error {
	known := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		known[task.Entry.ID] = true
	}
	edges := make(map[string][]string, len(tasks))
	dependents := map[string][]string{}
	for _, task := range tasks {
		var deps []string
		for _, dep := range append(append([]string(nil), task.Entry.Dependencies...), task.Entry.OrderingDependencies...) {
			if known[dep] {
				deps = appendUniqueString(deps, dep)
			}
		}
		edges[task.Entry.ID] = deps
	}
	remaining := make(map[string]int, len(tasks))
	for id, deps := range edges {
		remaining[id] = len(deps)
		for _, dep := range deps {
			dependents[dep] = append(dependents[dep], id)
		}
	}
	ready := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if remaining[task.Entry.ID] == 0 {
			ready = append(ready, task.Entry.ID)
		}
	}
	settled := 0
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		settled++
		for _, dependent := range dependents[id] {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	if settled == len(tasks) {
		return nil
	}
	var members []string
	for _, task := range tasks {
		if remaining[task.Entry.ID] > 0 {
			members = append(members, task.Entry.ID)
		}
	}
	sort.Strings(members)
	return fmt.Errorf("cannot plan destroy: task dependency cycle among %s", strings.Join(members, ", "))
}

func plannedDestroySteps(state v1alpha1.State, steps []destroyStep, storageWorkNames []string) []destroyStep {
	planned := make([]destroyStep, 0, len(steps))
	for _, step := range steps {
		clusters := destroyStepClusters(state, step.kind)
		if destroyStepIsStorageScoped(step.kind) && storageWorkNames != nil {
			if len(storageWorkNames) == 0 {
				continue
			}
			clusters = append([]string(nil), storageWorkNames...)
			sort.Strings(clusters)
		}
		if destroyStepIsStorageScoped(step.kind) {
			if fannable := destroyFanOutClusters(state, clusters); len(fannable) > 1 {
				planned = append(planned, fanOutDestroyStep(state, step, fannable)...)
				continue
			}
		}
		step.resourceKeys = clusters
		planned = append(planned, step)
	}
	return planned
}

func destroyStepIsStorageScoped(kind string) bool {
	return kind == DestroyTaskKindStorageCluster || kind == DestroyTaskKindStorageNodeAccess
}

func destroyFanOutClusters(state v1alpha1.State, clusters []string) []string {
	grouped := destroyStorageInventoryGroupClusters(state)
	out := make([]string, 0, len(clusters))
	for _, name := range clusters {
		if grouped[name] {
			out = append(out, name)
		}
	}
	if len(out) != len(clusters) {
		return nil
	}
	return out
}

func destroyStorageInventoryGroupClusters(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if strings.TrimSpace(node.MachineRef.Name) != "" {
				out[cluster.Metadata.Name] = true
				break
			}
		}
	}
	return out
}

func fanOutDestroyStep(state v1alpha1.State, step destroyStep, clusters []string) []destroyStep {
	out := make([]destroyStep, 0, len(clusters))
	for _, cluster := range clusters {
		fanned := step
		fanned.id = step.id + "." + cluster
		fanned.baseID = step.id
		fanned.label = step.label + " " + cluster
		fanned.limit = render.StorageClusterGroupName(cluster)
		fanned.forksLimit = render.StorageClusterGroupName(cluster)
		fanned.resourceKeys = destroyClusterResourceKeys(state, cluster)
		out = append(out, fanned)
	}
	return out
}

func destroyClusterResourceKeys(state v1alpha1.State, cluster string) []string {
	out := []string{cluster}
	for _, machine := range ClusterSubstrateMachineNames(state, cluster) {
		out = append(out, DestroyMachineResourceKeyPrefix+machine)
	}
	return out
}

func destroyEmittedDependencies(ids []string, emitted map[string][]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		for _, emittedID := range emitted[id] {
			if seen[emittedID] {
				continue
			}
			seen[emittedID] = true
			out = append(out, emittedID)
		}
	}
	return out
}

func destroyOrderingDependencies(step destroyStep, declared map[string][]string, emitted map[string][]string) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(ids []string)
	walk = func(ids []string) {
		for _, id := range ids {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			if concrete := emitted[id]; len(concrete) > 0 {
				out = append(out, concrete...)
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
	parents := map[string]map[string]bool{}
	for child, hosts := range KubeVirtHostParentsByChild(state) {
		if !names[child] {
			continue
		}
		for parent := range hosts {
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

type DestroyOutcome map[string]bool

const destroyOutcomeAttemptedPrefix = "attempted:"

const DestroyMachineResourceKeyPrefix = "machine:"

func destroyOutcomeClusterKey(kind, cluster string) string {
	return kind + "/" + cluster
}

func destroyTaskClusterKeys(task TaskLedgerEntry) []string {
	var out []string
	for _, key := range task.ResourceKeys {
		if key == "" || strings.HasPrefix(key, DestroyMachineResourceKeyPrefix) {
			continue
		}
		out = append(out, key)
	}
	return out
}

func SucceededDestroyTaskKinds(ledger RunLedger) DestroyOutcome {
	out := DestroyOutcome{}
	kinds := map[string]bool{}
	unfinished := map[string]bool{}
	for _, task := range ledger.Tasks {
		if task.Kind == "" {
			continue
		}
		kinds[task.Kind] = true
		out[destroyOutcomeAttemptedPrefix+task.Kind] = true
		for _, cluster := range destroyTaskClusterKeys(task) {
			out[destroyOutcomeAttemptedPrefix+destroyOutcomeClusterKey(task.Kind, cluster)] = true
		}
		if task.Status != TaskStatusOK {
			unfinished[task.Kind] = true
			continue
		}
		for _, cluster := range destroyTaskClusterKeys(task) {
			out[destroyOutcomeClusterKey(task.Kind, cluster)] = true
		}
	}
	for kind := range kinds {
		if !unfinished[kind] {
			out[kind] = true
		}
	}
	return out
}

func (o DestroyOutcome) Covers(kind, cluster string) bool {
	if o == nil {
		return true
	}
	if o[kind] {
		return true
	}
	return cluster != "" && o[destroyOutcomeClusterKey(kind, cluster)]
}

func (o DestroyOutcome) Attempted(kind, cluster string) bool {
	if o == nil {
		return false
	}
	if o[destroyOutcomeAttemptedPrefix+kind] {
		return true
	}
	return cluster != "" && o[destroyOutcomeAttemptedPrefix+destroyOutcomeClusterKey(kind, cluster)]
}

func DestroyScopeCoversStorage(scopeName string) bool {
	switch strings.TrimSpace(scopeName) {
	case "clusters", "all":
		return true
	default:
		return false
	}
}
