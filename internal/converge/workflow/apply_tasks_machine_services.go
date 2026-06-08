package workflow

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func planMachineServiceActivities(graph *ActivityGraph, state v1alpha1.State, phaseSet map[string]bool) ([]string, error) {
	taskIDs := []string{}
	if phaseSet[ApplyPhaseFabric] {
		for _, host := range render.HostGroupMembers(state)[render.GroupProviderHosts] {
			taskID := "provider." + host
			taskIDs = append(taskIDs, taskID)
			if err := graph.Add(Activity{
				ID:       taskID,
				Kind:     ActivityKindProviderHostPrepare,
				Requires: []CapabilityRef{machineOSReadyCapability(host)},
				Provides: []CapabilityRef{providerHostReadyCapability(host), providerServiceReadyCapability(host)},
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindProvider,
						Label:        "provider services " + host,
						Host:         host,
						ResourceKeys: []string{hostMutationResource(host)},
						Status:       TaskStatusPending,
					},
					Playbook:        applyProviderPlaybook,
					Limit:           host,
					Forks:           1,
					State:           state,
					DesiredHashVars: render.FabricHostDesiredVars(state, host),
				},
			}); err != nil {
				return nil, err
			}
		}
	}
	if phaseSet[ApplyPhaseFabric] {
		for _, host := range render.HostGroupMembers(state)[render.GroupInfraComponentHosts] {
			taskID := "infra-component." + host
			taskIDs = append(taskIDs, taskID)
			if err := graph.Add(Activity{
				ID:       taskID,
				Kind:     ActivityKindInfraComponentServiceApply,
				Requires: []CapabilityRef{machineOSReadyCapability(host)},
				Provides: []CapabilityRef{serviceEndpointReadyCapability(host)},
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindInfraComponentServices,
						Label:        "infra component services " + host,
						Host:         host,
						ResourceKeys: []string{hostMutationResource(host)},
						Status:       TaskStatusPending,
					},
					Playbook:        applyInfraComponentsPlaybook,
					Limit:           host,
					Forks:           1,
					State:           state,
					DesiredHashVars: render.FabricHostDesiredVars(state, host),
				},
			}); err != nil {
				return nil, err
			}
		}
	}
	return taskIDs, nil
}
