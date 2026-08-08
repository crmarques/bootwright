package workflow

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
)

type destroyStorageWork struct {
	names   []string
	nameSet map[string]bool
	fan     bool
	present bool
}

func destroyStorageWorkFor(state v1alpha1.State, storageWorkNames []string) destroyStorageWork {
	work := destroyStorageWork{present: true, nameSet: map[string]bool{}}
	if storageWorkNames != nil {
		if len(storageWorkNames) == 0 {
			work.present = false
			return work
		}
		work.names = append([]string(nil), storageWorkNames...)
	} else {
		for _, cluster := range state.StorageClusters {
			work.names = append(work.names, cluster.Metadata.Name)
		}
	}
	sort.Strings(work.names)
	for _, name := range work.names {
		work.nameSet[name] = true
	}
	if len(work.names) > 1 {
		grouped := destroyStorageInventoryGroupClusters(state)
		work.fan = true
		for _, name := range work.names {
			if !grouped[name] {
				work.fan = false
				break
			}
		}
	}
	return work
}

func (w destroyStorageWork) stepID(base, cluster string) string {
	if w.fan && cluster != "" {
		return base + "." + cluster
	}
	return base
}

func (w destroyStorageWork) allIDs(base string) []string {
	if !w.present {
		return nil
	}
	if !w.fan {
		return []string{base}
	}
	out := make([]string, 0, len(w.names))
	for _, name := range w.names {
		out = append(out, base+"."+name)
	}
	return out
}

func (w destroyStorageWork) stepIDsFor(cluster string, bases ...string) []string {
	if !w.present {
		return nil
	}
	out := make([]string, 0, len(bases))
	for _, base := range bases {
		out = append(out, w.stepID(base, cluster))
	}
	return out
}

type destroyStorageFamily struct {
	base     string
	kind     string
	label    string
	playbook string
	ordering func(cluster string) []string
}

func storageFamilySteps(state v1alpha1.State, work destroyStorageWork, family destroyStorageFamily) []destroyStep {
	if !work.present {
		return nil
	}
	base := destroyStep{
		id:         family.base,
		kind:       family.kind,
		label:      family.label,
		playbook:   family.playbook,
		limit:      render.GroupStorageHosts,
		forksLimit: render.GroupStorageHosts,
	}
	if !work.fan {
		base.resourceKeys = work.names
		if family.ordering != nil {
			base.orderingDependencies = family.ordering("")
		}
		return []destroyStep{base}
	}
	out := make([]destroyStep, 0, len(work.names))
	for _, cluster := range work.names {
		step := base
		step.id = family.base + "." + cluster
		step.baseID = family.base
		step.label = family.label + " " + cluster
		step.limit = render.StorageClusterGroupName(cluster)
		step.forksLimit = render.StorageClusterGroupName(cluster)
		step.resourceKeys = destroyClusterResourceKeys(state, cluster)
		if family.ordering != nil {
			step.orderingDependencies = family.ordering(cluster)
		}
		out = append(out, step)
	}
	return out
}

func (f destroyGraphFacts) machineInfraFanned() bool {
	return len(f.machineInfraFan) > 0
}

func (f destroyGraphFacts) machineInfraID(cluster string) string {
	if f.machineInfraFanned() && f.machineInfraFanSet[cluster] {
		return destroyMachineInfraTaskID + "." + cluster
	}
	return destroyMachineInfraTaskID
}

func (f destroyGraphFacts) containerFanned() bool {
	return len(f.containerClusters) > 1
}

func (f destroyGraphFacts) containerStepID(base, cluster string) string {
	if f.containerFanned() && cluster != "" {
		return base + "." + cluster
	}
	return base
}

func (f destroyGraphFacts) isContainerCluster(name string) bool {
	for _, cluster := range f.containerClusters {
		if cluster == name {
			return true
		}
	}
	return false
}

func (f destroyGraphFacts) containerAllIDs(base string) []string {
	if !f.containerFanned() {
		return []string{base}
	}
	out := make([]string, 0, len(f.containerClusters))
	for _, cluster := range f.containerClusters {
		out = append(out, base+"."+cluster)
	}
	return out
}

func containerClusterFamilySteps(facts destroyGraphFacts, base, kind, label, playbook string, hard []string, ordering func(cluster string) []string) []destroyStep {
	step := destroyStep{
		id:           base,
		kind:         kind,
		label:        label,
		playbook:     playbook,
		forksLimit:   render.GroupOCPHosts,
		dependencies: hard,
	}
	if !facts.containerFanned() {
		step.resourceKeys = facts.containerClusters
		if ordering != nil {
			step.orderingDependencies = ordering("")
		}
		return []destroyStep{step}
	}
	out := make([]destroyStep, 0, len(facts.containerClusters))
	for _, cluster := range facts.containerClusters {
		fanned := step
		fanned.id = base + "." + cluster
		fanned.baseID = base
		fanned.label = label + " " + cluster
		fanned.resourceKeys = []string{cluster}
		if ordering != nil {
			fanned.orderingDependencies = ordering(cluster)
		}
		fanned.extraVarOverrides = []string{DestroyContainerClusterExtraVar + "=" + cluster}
		out = append(out, fanned)
	}
	return out
}

func machineInfraFamilySteps(state v1alpha1.State, facts destroyGraphFacts, ordering func(cluster string) []string, hard func(cluster string) []string) ([]destroyStep, error) {
	step := destroyStep{
		id:       destroyMachineInfraTaskID,
		kind:     DestroyTaskKindMachineInfra,
		label:    "Machine infrastructure",
		playbook: roles.PlaybookTaskMachineInfraDestroy,
		limit:    machineInfraDestroyLimit(),
	}
	if !facts.machineInfraFanned() {
		levels, err := machineInfraDestroyLevels(state)
		if err != nil {
			return nil, err
		}
		if order := flattenDestroyLevels(levels); len(order) > 0 {
			step.resourceKeys = order
			step.extraVarOverrides = []string{
				DestroyClusterOrderExtraVar + "=" + strings.Join(order, ","),
				DestroyClusterLevelsExtraVar + "=" + joinDestroyLevels(levels),
			}
		}
		if ordering != nil {
			step.orderingDependencies = ordering("")
		}
		if hard != nil {
			step.dependencies = appendUniqueStrings(step.dependencies, hard("")...)
		}
		return []destroyStep{step}, nil
	}
	out := make([]destroyStep, 0, len(facts.machineInfraFan)+1)
	for _, cluster := range facts.machineInfraFan {
		fanned := step
		fanned.id = facts.machineInfraID(cluster)
		fanned.baseID = destroyMachineInfraTaskID
		fanned.label = "Machine infrastructure " + cluster
		group := destroyMachineInfraClusterGroup(state, cluster)
		fanned.limit = strings.Join([]string{group, render.GroupProviderHosts, render.GroupInfraHosts}, ":")
		fanned.forksLimit = group
		fanned.resourceKeys = destroyClusterResourceKeys(state, cluster)
		fanned.hostSlotKey = destroyMachineInfraHostSlotKey(state, cluster)
		for _, guest := range facts.guestsByHost[cluster] {
			if facts.machineInfraFanSet[guest] {
				fanned.dependencies = appendUniqueString(fanned.dependencies, facts.machineInfraID(guest))
			}
		}
		if hard != nil {
			fanned.dependencies = appendUniqueStrings(fanned.dependencies, hard(cluster)...)
		}
		sort.Strings(fanned.dependencies)
		if ordering != nil {
			fanned.orderingDependencies = ordering(cluster)
		}
		fanned.extraVarOverrides = []string{
			DestroyClusterOrderExtraVar + "=" + cluster,
			DestroyClusterLevelsExtraVar + "=" + cluster,
			DestroyClusterScopeExtraVar + "=" + cluster,
		}
		fanned.dropExtraVarNames = []string{InfraDestroyContextSweepExtraVar}
		out = append(out, fanned)
	}
	out = append(out, destroyStep{
		id:                destroyMachineInfraRecordsTaskID,
		kind:              DestroyTaskKindMachineInfra,
		label:             "Machine infrastructure records",
		playbook:          roles.PlaybookTaskMachineInfraDestroy,
		limit:             strings.Join([]string{render.GroupProviderHosts, render.GroupInfraHosts}, ":"),
		forksLimit:        render.GroupProviderHosts,
		dependencies:      []string{destroyMachineInfraTaskID},
		extraVarOverrides: []string{MachineInfraRecordsOnlyExtraVar + "=true"},
	})
	return out, nil
}

func destroyMachineInfraHostSlotKey(state v1alpha1.State, cluster string) string {
	for _, machine := range ClusterSubstrateMachineNames(state, cluster) {
		if host := applyMachineHost(state, machine); host != "" {
			return "destroy:machine-infra:" + host
		}
	}
	return ""
}

func storageClusterFamily(facts destroyGraphFacts, work destroyStorageWork, runtimeGated bool) destroyStorageFamily {
	return destroyStorageFamily{
		base:     DestroyStorageClustersTaskID,
		kind:     DestroyTaskKindStorageCluster,
		label:    "Storage clusters",
		playbook: roles.PlaybookTaskStorageClusterDestroy,
		ordering: func(cluster string) []string {
			if !runtimeGated {
				return nil
			}
			var consumers []string
			if cluster == "" {
				for _, name := range work.names {
					consumers = append(consumers, facts.consumersByStorage[name]...)
				}
			} else {
				consumers = facts.consumersByStorage[cluster]
			}
			var ordering []string
			for _, consumer := range consumers {
				if !facts.isContainerCluster(consumer) {
					continue
				}
				ordering = appendUniqueString(ordering, facts.containerStepID(destroyClusterRuntimeTaskID, consumer))
			}
			sort.Strings(ordering)
			return ordering
		},
	}
}

func machineRegistrationFamily(work destroyStorageWork, afterStorage bool) destroyStorageFamily {
	return destroyStorageFamily{
		base:     destroyMachineRegistrationTaskID,
		kind:     DestroyTaskKindMachineRegistration,
		label:    "Machine registration",
		playbook: roles.PlaybookTaskMachineRegistrationDeregister,
		ordering: func(cluster string) []string {
			if !afterStorage {
				return nil
			}
			return []string{work.stepID(DestroyStorageClustersTaskID, cluster)}
		},
	}
}

func storageNodeAccessFamily(work destroyStorageWork, afterRegistration bool, extra func(cluster string) []string) destroyStorageFamily {
	return destroyStorageFamily{
		base:     destroyStorageNodeAccessTaskID,
		kind:     DestroyTaskKindStorageNodeAccess,
		label:    "Storage node access",
		playbook: roles.PlaybookTaskStorageNodeAccessDestroy,
		ordering: func(cluster string) []string {
			var out []string
			if afterRegistration {
				out = append(out,
					work.stepID(destroyMachineRegistrationTaskID, cluster),
					work.stepID(DestroyStorageClustersTaskID, cluster),
				)
			}
			if extra != nil {
				out = appendUniqueStrings(out, extra(cluster)...)
			}
			return out
		},
	}
}
