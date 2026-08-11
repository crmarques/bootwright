package converge

import (
	"fmt"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func ResetMachineConvergeRecordsAfterDestroy(runsDir, clustersDir string, state v1alpha1.State, machineProvision map[string]bool, succeededDestroyKinds workflow.DestroyOutcome, destroyRunID string, purgeHistory, skipUnreachable bool) []error {
	include := destroyKindIncluded(succeededDestroyKinds)
	if !include(workflow.DestroyTaskKindMachineInfra, "") {
		return nil
	}
	target := InfraScope.ApplyTarget()
	target.MachineProvision = machineProvision
	tasks, err := workflow.PlanApplyTasksChecked(target, state)
	if err != nil {
		return []error{fmt.Errorf("plan machine converge-record reset: %w", err)}
	}
	var problems []error
	for _, task := range tasks {
		if destroyKindForApplyTaskKind(task.Entry.Kind) != workflow.DestroyTaskKindMachineInfra {
			continue
		}
		if err := workflow.RemoveApplyTaskConvergeSafety(runsDir, task); err != nil {
			problems = append(problems, fmt.Errorf("remove converge record for %s: %w", task.Entry.ID, err))
		}
	}
	if !skipUnreachable {
		for _, cluster := range workflow.MachineSubstrateClusters(tasks) {
			var released []string
			for _, name := range workflow.ClusterSubstrateMachineNames(state, cluster) {
				if machineProvision[name] {
					released = append(released, name)
				}
			}
			if err := workflow.MarkSubstrateMachinesReleased(runsDir, cluster, released, time.Now()); err != nil {
				problems = append(problems, fmt.Errorf("record substrate release for %s: %w", cluster, err))
			}
		}
	}
	if purgeHistory && !skipUnreachable {
		machineNames := make([]string, 0, len(machineProvision))
		for name := range machineProvision {
			machineNames = append(machineNames, name)
		}
		if err := purgeRunHistoryForComponents(runsDir, nil, machineNames, destroyRunID); err != nil {
			problems = append(problems, fmt.Errorf("purge run history: %w", err))
		}
	}
	return append(problems, pruneDestroyedClusterStateDirs(clustersDir, workflow.MachineSubstrateClusters(tasks))...)
}

func destroyKindIncluded(succeeded workflow.DestroyOutcome) func(string, string) bool {
	if succeeded == nil {
		return func(string, string) bool { return true }
	}
	return func(kind, cluster string) bool {
		if kind == "" {
			return false
		}
		if succeeded.Covers(kind, cluster) {
			return true
		}
		switch kind {
		case workflow.DestroyTaskKindContainerCluster, workflow.DestroyTaskKindStorageCluster, workflow.DestroyTaskKindStorageNodeAccess:
			return !succeeded.Attempted(kind, cluster) && succeeded.Covers(workflow.DestroyTaskKindMachineInfra, cluster)
		default:
			return false
		}
	}
}

func destroyKindForApplyTaskKind(kind string) string {
	switch kind {
	case workflow.ApplyTaskKindStorageNodeAccess:
		return workflow.DestroyTaskKindStorageNodeAccess
	case workflow.ApplyTaskKindStorageInfra, workflow.ApplyTaskKindStorageCluster:
		return workflow.DestroyTaskKindStorageCluster
	case workflow.ApplyTaskKindClusterISO, workflow.ApplyTaskKindNodeBoot, workflow.ApplyTaskKindBootstrapWait, workflow.ApplyTaskKindInstallWait,
		workflow.ApplyTaskKindClusterInstall, workflow.ApplyTaskKindNodeConfigApply, workflow.ApplyTaskKindClusterAddon,
		workflow.ApplyTaskKindHostVirtctl:
		return workflow.DestroyTaskKindContainerCluster
	case workflow.ApplyTaskKindManagedMachineOS, workflow.ApplyTaskKindMachineInfraPrepare, workflow.ApplyTaskKindMachineInfraFinalize,
		workflow.ApplyTaskKindMachineRegistration, workflow.ApplyTaskKindMachineRepositories,
		workflow.ApplyTaskKindPlaybook:
		return workflow.DestroyTaskKindMachineInfra
	case workflow.ApplyTaskKindInfraComponentServices:
		return workflow.DestroyTaskKindInfraComponents
	case workflow.ApplyTaskKindControllerNameResolution:
		return workflow.DestroyTaskKindControllerNameResolution
	case workflow.ApplyTaskKindProvider:
		return workflow.DestroyTaskKindProviderServices
	default:
		return ""
	}
}

func isPartialStorageTask(task workflow.ApplyTask, partial map[string]bool) bool {
	if len(partial) == 0 {
		return false
	}
	switch task.Entry.Kind {
	case workflow.ApplyTaskKindStorageNodeAccess, workflow.ApplyTaskKindStorageInfra, workflow.ApplyTaskKindStorageCluster:
		return partial[task.Entry.Cluster]
	}
	return false
}
