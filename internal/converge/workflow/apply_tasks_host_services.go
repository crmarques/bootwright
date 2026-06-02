package workflow

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func planHostServiceTasks(state v1alpha1.State, phaseSet map[string]bool) ([]ApplyTask, []string) {
	var tasks []ApplyTask
	taskIDs := []string{}
	if phaseSet[ApplyPhaseProvider] {
		for _, host := range render.HostGroupMembers(state)[render.GroupProviderHosts] {
			taskID := "provider." + host
			taskIDs = append(taskIDs, taskID)
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindProvider,
					Label:        "provider services " + host,
					Host:         host,
					ResourceKeys: []string{hostMutationResource(host)},
					Status:       TaskStatusPending,
				},
				Playbook: applyProviderPlaybook,
				Limit:    host,
				Forks:    1,
				State:    state,
			})
		}
	}
	if phaseSet[ApplyPhaseInfraComponents] {
		for _, host := range render.HostGroupMembers(state)[render.GroupInfraComponentHosts] {
			taskID := "infra-component." + host
			taskIDs = append(taskIDs, taskID)
			tasks = append(tasks, ApplyTask{
				Entry: TaskLedgerEntry{
					ID:           taskID,
					Kind:         ApplyTaskKindInfraComponentServices,
					Label:        "infra component services " + host,
					Host:         host,
					ResourceKeys: []string{hostMutationResource(host)},
					Status:       TaskStatusPending,
				},
				Playbook: applyInfraComponentsPlaybook,
				Limit:    host,
				Forks:    1,
				State:    state,
			})
		}
	}
	return tasks, taskIDs
}
