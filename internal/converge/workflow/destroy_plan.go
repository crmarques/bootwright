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
	DestroyTaskKindMachineRegistration      = "destroyMachineRegistration"
	DestroyTaskKindMachineInfra             = "destroyMachineInfra"
	DestroyTaskKindInfraComponents          = "destroyInfraComponents"
	DestroyTaskKindControllerNameResolution = "destroyControllerNameResolution"
	DestroyTaskKindProviderServices         = "destroyProviderServices"
	DestroyTaskKindStorageCluster           = "destroyStorageCluster"
	DestroyTaskKindContainerCluster         = "destroyContainerCluster"
	DestroyTaskKindContainerClusterRuntime  = "destroyContainerClusterRuntime"
	DestroyTaskKindStorageNodeAccess        = "destroyStorageNodeAccess"
)

const DestroyStorageScopeExtraVar = "bootwright_destroy_storage_scope"

const DestroyMachineScopeExtraVar = "bootwright_destroy_machine_scope"

const DestroyClusterOrderExtraVar = "bootwright_destroy_cluster_order"

const DestroyClusterLevelsExtraVar = "bootwright_destroy_cluster_levels"

const DestroyClusterScopeExtraVar = "bootwright_destroy_cluster_scope"

const InfraDestroyContextSweepExtraVar = "bootwright_infra_destroy_context_sweep"

const DestroySkipOrphanSweepExtraVar = "bootwright_destroy_skip_orphan_sweep"

const DestroyContainerClusterExtraVar = "bootwright_destroy_container_cluster"

const MachineInfraRecordsOnlyExtraVar = "bootwright_machine_infra_records_only"

const (
	destroyClusterRuntimeLabel = "Cluster runtime (controller)"
	destroyClusterRecordsLabel = "Cluster records (controller)"
)

func PlanDestroyTasks(scopeName string, state v1alpha1.State, limit string, extraVars []string, storageWorkNames []string) ([]ApplyTask, error) {
	if destroyMachineScoped(extraVars) {
		return destroyChain(state, limit, extraVars, infraMachineDestroySteps())
	}
	facts := destroyGraphFactsFor(state)
	work := destroyStorageWorkFor(state, storageWorkNames)
	var steps []destroyStep
	var err error
	switch strings.TrimSpace(scopeName) {
	case "infra":
		steps, err = infraDestroySteps(state, facts, work)
	case "clusters":
		steps, err = clusterDestroySteps(state, facts, work)
	case "all":
		steps, err = fullDestroySteps(state, facts, work)
	default:
		return nil, fmt.Errorf("granular destroy is only supported for the infra, clusters, and all stages, not %q", scopeName)
	}
	if err != nil {
		return nil, err
	}
	steps = dropOutOfScopeSharedServiceSteps(state, extraVars, steps)
	steps = bracketControllerNameResolutionDestroy(scopeName, extraVars, steps)
	return destroyChain(state, limit, extraVars, steps)
}

func bracketControllerNameResolutionDestroy(scopeName string, extraVars []string, steps []destroyStep) []destroyStep {
	if (scopeName != "infra" && scopeName != "all") || destroyOrphanSweepSuppressed(extraVars) {
		return steps
	}
	hasInfraComponents := false
	for _, step := range steps {
		if step.id == destroyInfraComponentsTaskID {
			hasInfraComponents = true
			break
		}
	}
	if !hasInfraComponents {
		return steps
	}
	preflight := destroyStep{
		id:              destroyControllerNameResolutionPreflightTaskID,
		kind:            DestroyTaskKindControllerNameResolution,
		label:           "Controller name resolution preflight",
		playbook:        roles.PlaybookTaskControllerNameResolutionDestroyPreflight,
		limit:           render.GroupControllerHosts,
		forksLimit:      render.GroupControllerHosts,
		completionLimit: render.GroupControllerHosts,
	}
	cleanup := destroyStep{
		id:              destroyControllerNameResolutionCleanupTaskID,
		kind:            DestroyTaskKindControllerNameResolution,
		label:           "Controller name resolution cleanup",
		playbook:        roles.PlaybookTaskControllerNameResolutionDestroyCleanup,
		limit:           render.GroupControllerHosts,
		forksLimit:      render.GroupControllerHosts,
		completionLimit: render.GroupControllerHosts,
	}
	bracketed := make([]destroyStep, 0, len(steps)+2)
	bracketed = append(bracketed, preflight)
	for i := range steps {
		steps[i].successDependencies = appendUniqueString(steps[i].successDependencies, preflight.id)
		cleanup.dependencies = appendUniqueString(cleanup.dependencies, steps[i].id)
		bracketed = append(bracketed, steps[i])
	}
	return append(bracketed, cleanup)
}

func destroyOrphanSweepSuppressed(extraVars []string) bool {
	for _, pair := range extraVars {
		if pair == DestroySkipOrphanSweepExtraVar+"=true" {
			return true
		}
	}
	return false
}

func dropOutOfScopeSharedServiceSteps(state v1alpha1.State, extraVars []string, steps []destroyStep) []destroyStep {
	if !destroyClusterScoped(extraVars) {
		return steps
	}
	members := render.HostGroupMembers(state)
	drop := map[string]bool{
		destroyInfraComponentsTaskID:  len(members[render.GroupInfraComponentHosts]) == 0,
		destroyProviderServicesTaskID: len(members[render.GroupProviderHosts]) == 0,
	}
	out := make([]destroyStep, 0, len(steps))
	for _, step := range steps {
		if drop[step.id] {
			continue
		}
		out = append(out, step)
	}
	return out
}

func destroyClusterScoped(extraVars []string) bool {
	prefix := DestroyClusterScopeExtraVar + "="
	for _, pair := range extraVars {
		if strings.HasPrefix(pair, prefix) && strings.TrimSpace(strings.TrimPrefix(pair, prefix)) != "" {
			return true
		}
	}
	return false
}

const (
	destroyStorageNodeAccessTaskID                 = "destroy.storage-node-access"
	destroyMachineRegistrationTaskID               = "destroy.machine-registration"
	destroyInfraComponentsTaskID                   = "destroy.infra-components"
	destroyMachineInfraTaskID                      = "destroy.machine-infra"
	destroyMachineInfraRecordsTaskID               = "destroy.machine-infra-records"
	destroyClusterRuntimeTaskID                    = "destroy.cluster-runtime"
	destroyContainerClustersTaskID                 = "destroy.container-clusters"
	destroyProviderServicesTaskID                  = "destroy.provider-services"
	destroyControllerNameResolutionPreflightTaskID = "destroy.controller-name-resolution-preflight"
	destroyControllerNameResolutionCleanupTaskID   = "destroy.controller-name-resolution-cleanup"
)

const destroyMaxForks = 20

func infraDestroySteps(state v1alpha1.State, facts destroyGraphFacts, work destroyStorageWork) ([]destroyStep, error) {
	steps := storageFamilySteps(state, work, machineRegistrationFamily(work, false))
	placementFirst := destroyPlacementFirst(facts)
	machineSteps, err := machineInfraFamilySteps(state, facts, func(cluster string) []string {
		out := appendUniqueStrings(nil, work.allIDs(destroyMachineRegistrationTaskID)...)
		if destroyOrdersInfraComponentsFirst(facts, placementFirst, cluster) {
			out = appendUniqueString(out, destroyInfraComponentsTaskID)
		}
		return out
	}, nil)
	if err != nil {
		return nil, err
	}
	steps = append(steps, machineSteps...)
	steps = append(steps, infraComponentsDestroyStep(destroyInfraComponentsOrdering(facts, work, placementFirst)))
	steps = append(steps, providerServicesDestroyStep(destroyProviderServicesOrdering(facts, false)))
	return steps, nil
}

func infraMachineDestroySteps() []destroyStep {
	return []destroyStep{
		{
			id:              destroyMachineRegistrationTaskID,
			kind:            DestroyTaskKindMachineRegistration,
			label:           "Machine registration",
			playbook:        roles.PlaybookTaskMachineRegistrationDeregister,
			limit:           render.GroupStorageHosts,
			forksLimit:      render.GroupStorageHosts,
			completionLimit: render.GroupStorageHosts,
		},
		{
			id:                   destroyMachineInfraTaskID,
			kind:                 DestroyTaskKindMachineInfra,
			label:                "Machine infrastructure",
			playbook:             roles.PlaybookTaskMachineInfraDestroy,
			limit:                machineInfraDestroyLimit(),
			completionLimit:      machineInfraDestroyLimit(),
			orderingDependencies: []string{destroyMachineRegistrationTaskID},
		},
	}
}

func clusterDestroySteps(state v1alpha1.State, facts destroyGraphFacts, work destroyStorageWork) ([]destroyStep, error) {
	steps := containerClusterFamilySteps(facts, destroyClusterRuntimeTaskID, DestroyTaskKindContainerClusterRuntime,
		destroyClusterRuntimeLabel, roles.PlaybookTaskContainerClusterRuntimeDestroy, nil, nil)
	steps = append(steps, storageFamilySteps(state, work, storageClusterFamily(facts, work, true))...)
	steps = append(steps, containerClusterFamilySteps(facts, destroyContainerClustersTaskID, DestroyTaskKindContainerCluster,
		destroyClusterRecordsLabel, roles.PlaybookTaskContainerClusterAgentDestroy, nil,
		destroyContainerRecordsOrdering(facts, work))...)
	steps = append(steps, storageFamilySteps(state, work, storageNodeAccessFamily(work, false, func(cluster string) []string {
		return facts.containerAllIDs(destroyContainerClustersTaskID)
	}))...)
	return steps, nil
}

func fullDestroySteps(state v1alpha1.State, facts destroyGraphFacts, work destroyStorageWork) ([]destroyStep, error) {
	steps := containerClusterFamilySteps(facts, destroyClusterRuntimeTaskID, DestroyTaskKindContainerClusterRuntime,
		destroyClusterRuntimeLabel, roles.PlaybookTaskContainerClusterRuntimeDestroy, nil, nil)
	steps = append(steps, storageFamilySteps(state, work, storageClusterFamily(facts, work, true))...)
	steps = append(steps, storageFamilySteps(state, work, machineRegistrationFamily(work, true))...)
	steps = append(steps, storageFamilySteps(state, work, storageNodeAccessFamily(work, true, nil))...)

	placementFirst := destroyPlacementFirst(facts)
	machineSteps, err := machineInfraFamilySteps(state, facts, func(cluster string) []string {
		var out []string
		if cluster == "" || facts.isContainerCluster(cluster) {
			out = appendUniqueStrings(out, facts.containerAllIDs(destroyClusterRuntimeTaskID)...)
		}
		if cluster == "" || work.nameSet[cluster] {
			out = appendUniqueStrings(out, work.stepIDsFor(cluster,
				destroyStorageNodeAccessTaskID, destroyMachineRegistrationTaskID)...)
		}
		if destroyOrdersInfraComponentsFirst(facts, placementFirst, cluster) {
			out = appendUniqueString(out, destroyInfraComponentsTaskID)
		}
		return out
	}, func(cluster string) []string {
		return work.proofIDs(DestroyStorageClustersTaskID, cluster)
	})
	if err != nil {
		return nil, err
	}
	steps = append(steps, machineSteps...)
	steps = append(steps, containerClusterFamilySteps(facts, destroyContainerClustersTaskID, DestroyTaskKindContainerCluster,
		destroyClusterRecordsLabel, roles.PlaybookTaskContainerClusterAgentDestroy,
		[]string{destroyMachineInfraTaskID}, destroyContainerRecordsOrdering(facts, work))...)
	steps = append(steps, infraComponentsDestroyStep(destroyInfraComponentsOrdering(facts, work, placementFirst)))
	steps = append(steps, providerServicesDestroyStep(destroyProviderServicesOrdering(facts, true)))
	return steps, nil
}

func destroyPlacementFirst(facts destroyGraphFacts) bool {
	return !facts.machineInfraFanned() && len(facts.placement) > 0
}

func destroyOrdersInfraComponentsFirst(facts destroyGraphFacts, placementFirst bool, cluster string) bool {
	if cluster == "" {
		return placementFirst
	}
	return facts.placement[cluster]
}

func destroyInfraComponentsOrdering(facts destroyGraphFacts, work destroyStorageWork, placementFirst bool) []string {
	var out []string
	if facts.machineInfraFanned() {
		placementFanned := false
		for _, cluster := range facts.machineInfraFan {
			if facts.placement[cluster] {
				placementFanned = true
				continue
			}
			out = appendUniqueString(out, facts.machineInfraID(cluster))
		}
		if !placementFanned {
			out = appendUniqueString(out, destroyMachineInfraRecordsTaskID)
		}
	} else if !placementFirst {
		out = appendUniqueString(out, destroyMachineInfraTaskID)
	}
	out = appendUniqueStrings(out, work.allIDs(destroyStorageNodeAccessTaskID)...)
	out = appendUniqueStrings(out, work.allIDs(destroyMachineRegistrationTaskID)...)
	return out
}

func destroyProviderServicesOrdering(facts destroyGraphFacts, withContainers bool) []string {
	out := []string{destroyMachineInfraTaskID}
	if facts.machineInfraFanned() {
		out = append(out, destroyMachineInfraRecordsTaskID)
	}
	if withContainers {
		out = append(out, destroyContainerClustersTaskID)
	}
	return append(out, destroyInfraComponentsTaskID)
}

func destroyContainerRecordsOrdering(facts destroyGraphFacts, work destroyStorageWork) func(cluster string) []string {
	return func(cluster string) []string {
		out := appendUniqueStrings(nil, facts.containerStepID(destroyClusterRuntimeTaskID, cluster))
		return appendUniqueStrings(out, work.allIDs(DestroyStorageClustersTaskID)...)
	}
}

func infraComponentsDestroyStep(ordering []string) destroyStep {
	return destroyStep{
		id:                   destroyInfraComponentsTaskID,
		kind:                 DestroyTaskKindInfraComponents,
		label:                "Infra component services",
		playbook:             roles.PlaybookTaskInfraComponentServicesDestroy,
		forksLimit:           render.GroupInfraComponentHosts,
		completionLimit:      render.GroupInfraComponentHosts,
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
		completionLimit:      render.GroupProviderHosts,
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

type destroyStep struct {
	id                   string
	baseID               string
	kind                 string
	label                string
	playbook             string
	limit                string
	forksLimit           string
	completionLimit      string
	hostSlotKey          string
	resourceKeys         []string
	dependencies         []string
	successDependencies  []string
	orderingDependencies []string
	extraVarOverrides    []string
	dropExtraVarNames    []string
}

func (s destroyStep) base() string {
	if s.baseID != "" {
		return s.baseID
	}
	return s.id
}

func destroyChain(state v1alpha1.State, limit string, extraVars []string, steps []destroyStep) ([]ApplyTask, error) {
	planned := steps
	declared := make(map[string][]string, len(steps))
	emitted := make(map[string][]string, len(planned))
	for _, step := range steps {
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
	hostGroups := render.HostGroupMembers(state)
	for _, step := range planned {
		resourceKeys := append([]string(nil), step.resourceKeys...)
		if len(resourceKeys) == 0 && step.forksLimit != "" {
			for _, host := range hostGroups[step.forksLimit] {
				resourceKeys = append(resourceKeys, DestroyMachineResourceKeyPrefix+host)
			}
		}
		entry := TaskLedgerEntry{
			ID:                   step.id,
			Kind:                 step.kind,
			Label:                step.label,
			ResourceKeys:         resourceKeys,
			Status:               TaskStatusPending,
			Dependencies:         destroyEmittedDependencies(step.dependencies, emitted),
			SuccessDependencies:  destroyEmittedDependencies(step.successDependencies, emitted),
			OrderingDependencies: destroyOrderingDependencies(step, declared, emitted),
		}
		taskLimit := limit
		if step.limit != "" {
			taskLimit = step.limit
		}
		entry.HostSlotKey = step.hostSlotKey
		if step.hostSlotKey != "" {
			entry.HostSlotCount = 1
		}
		tasks = append(tasks, ApplyTask{
			Entry:               entry,
			Playbook:            step.playbook,
			Limit:               taskLimit,
			CompletionHostLimit: step.completionLimit,
			Forks:               destroyStepForks(state, step, taskLimit),
			ExtraVarPairs:       destroyTaskExtraVars(extraVars, step),
			HostSlotKey:         step.hostSlotKey,
			HostSlotCount:       destroyStepHostSlotCount(step),
			State:               state,
		})
	}
	partitionControllerNameResolutionCleanupDependencies(tasks)
	if err := detectDestroyTaskCycle(tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func partitionControllerNameResolutionCleanupDependencies(tasks []ApplyTask) {
	entries := make(map[string]TaskLedgerEntry, len(tasks))
	for _, task := range tasks {
		entries[task.Entry.ID] = task.Entry
	}
	for i := range tasks {
		if tasks[i].Entry.ID != destroyControllerNameResolutionCleanupTaskID {
			continue
		}
		var dependencies []string
		successDependencies := append([]string(nil), tasks[i].Entry.SuccessDependencies...)
		for _, dependency := range tasks[i].Entry.Dependencies {
			entry, ok := entries[dependency]
			if ok && DestroyTaskNeedsCompletionProof(entry) && len(entry.ResourceKeys) > 0 {
				successDependencies = appendUniqueString(successDependencies, dependency)
				continue
			}
			dependencies = appendUniqueString(dependencies, dependency)
		}
		tasks[i].Entry.Dependencies = dependencies
		tasks[i].Entry.SuccessDependencies = successDependencies
		return
	}
}

func destroyStepHostSlotCount(step destroyStep) int {
	if step.hostSlotKey == "" {
		return 0
	}
	return 1
}

func destroyTaskExtraVars(runVars []string, step destroyStep) []string {
	drop := make(map[string]bool, len(step.dropExtraVarNames)+len(step.extraVarOverrides))
	for _, name := range step.dropExtraVarNames {
		drop[name] = true
	}
	for _, pair := range step.extraVarOverrides {
		if name, _, ok := strings.Cut(pair, "="); ok {
			drop[name] = true
		}
	}
	out := make([]string, 0, len(runVars)+len(step.extraVarOverrides))
	for _, pair := range runVars {
		name, _, _ := strings.Cut(pair, "=")
		if drop[name] {
			continue
		}
		out = append(out, pair)
	}
	return append(out, step.extraVarOverrides...)
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
		for _, dep := range taskDependencyIDs(task.Entry) {
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

func PrepareDestroyTaskGraph(runsDir string, opts RunOptions, tasks []ApplyTask, limits ConcurrencyLimits) (PreparedApplyTaskGraph, error) {
	startedAt := time.Now()
	runID := destroyRunID(startedAt)
	if opts.RunLease != nil {
		if err := opts.RunLease.RequireOwned(); err != nil {
			return PreparedApplyTaskGraph{}, err
		}
		startedAt = opts.RunLease.StartedAt
		runID = opts.RunLease.RunID
	}
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
		RunID:     runID,
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

func DestroyTaskClusterKeys(task TaskLedgerEntry) []string {
	var out []string
	for _, key := range task.ResourceKeys {
		if key == "" || strings.HasPrefix(key, DestroyMachineResourceKeyPrefix) {
			continue
		}
		out = append(out, key)
	}
	return out
}

func DestroyTaskMachineKeys(task TaskLedgerEntry) []string {
	var out []string
	for _, key := range task.ResourceKeys {
		machine := strings.TrimPrefix(key, DestroyMachineResourceKeyPrefix)
		if machine != key && machine != "" {
			out = append(out, machine)
		}
	}
	return out
}

func DestroyTaskNeedsCompletionProof(task TaskLedgerEntry) bool {
	return IsDestroyTaskKind(task.Kind)
}

var destroyTaskKinds = map[string]bool{
	DestroyTaskKindMachineRegistration:      true,
	DestroyTaskKindMachineInfra:             true,
	DestroyTaskKindInfraComponents:          true,
	DestroyTaskKindControllerNameResolution: true,
	DestroyTaskKindProviderServices:         true,
	DestroyTaskKindStorageCluster:           true,
	DestroyTaskKindContainerCluster:         true,
	DestroyTaskKindContainerClusterRuntime:  true,
	DestroyTaskKindStorageNodeAccess:        true,
}

func IsDestroyTaskKind(kind string) bool {
	return destroyTaskKinds[kind]
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
		for _, cluster := range DestroyTaskClusterKeys(task) {
			out[destroyOutcomeAttemptedPrefix+destroyOutcomeClusterKey(task.Kind, cluster)] = true
		}
		if task.Status != TaskStatusOK {
			unfinished[task.Kind] = true
			continue
		}
		for _, cluster := range DestroyTaskClusterKeys(task) {
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

func DestroyOutcomeFullySucceeded(outcome DestroyOutcome) bool {
	if outcome == nil {
		return true
	}
	attempted := false
	for key, reached := range outcome {
		if !reached || !strings.HasPrefix(key, destroyOutcomeAttemptedPrefix) {
			continue
		}
		attempted = true
		if !outcome[strings.TrimPrefix(key, destroyOutcomeAttemptedPrefix)] {
			return false
		}
	}
	return attempted
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
