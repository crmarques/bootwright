package workflow

import (
	"fmt"
	"reflect"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/render"
)

type controllerNameResolutionScopeError struct {
	provider  string
	name      string
	host      string
	consumers []string
}

func (e *controllerNameResolutionScopeError) Error() string {
	return fmt.Sprintf("controller name-resolution target %s/%s on %s is narrowed by selection; refusing to record desired state for consumers this run would not configure", e.provider, e.name, e.host)
}

func (e *controllerNameResolutionScopeError) Remedy() remedy.Request {
	targets := make([]remedy.Target, 0, len(e.consumers))
	for _, name := range e.consumers {
		targets = append(targets, remedy.Target{Role: remedy.TargetRoleClusterRoot, Name: name})
	}
	return remedy.Request{Action: remedy.ActionApplyAllConsumers, Targets: targets}
}

func planMachineServiceActivities(graph *ActivityGraph, state v1alpha1.State, hashState v1alpha1.State, target ApplyTarget, phaseSet map[string]bool) ([]string, error) {
	taskIDs := []string{}
	if phaseSet[ApplyPhaseFabric] {
		for _, host := range render.HostGroupMembers(state)[render.GroupProviderHosts] {
			if !target.FabricHostIncluded(host) {
				continue
			}
			taskID := "provider." + host
			taskIDs = append(taskIDs, taskID)
			if err := graph.Add(Activity{
				ID:       taskID,
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
					DesiredHashVars: render.FabricHostDesiredVars(hashState, host),
				},
			}); err != nil {
				return nil, err
			}
		}
	}
	if phaseSet[ApplyPhaseFabric] {
		for _, host := range render.HostGroupMembers(state)[render.GroupInfraComponentHosts] {
			if !target.FabricHostIncluded(host) {
				continue
			}
			taskID := "infra-component." + host
			taskIDs = append(taskIDs, taskID)
			if err := graph.Add(Activity{
				ID:       taskID,
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
					DesiredHashVars: render.FabricHostDesiredVars(hashState, host),
				},
			}); err != nil {
				return nil, err
			}
		}
	}
	if phaseSet[ApplyPhaseFabric] || phaseSet[ApplyPhaseMachines] || phaseSet[ApplyPhaseDeps] || phaseSet[ApplyPhaseBase] || phaseSet[ApplyPhaseAddons] {
		resolvers := render.ControllerNameResolutionTargets(state)
		unscopedResolvers := render.ControllerNameResolutionTargets(hashState)
		controllerMutationSelected := phaseSet[ApplyPhaseFabric] || phaseSet[ApplyPhaseMachines]
		automaticMutation := "bootwright_controller_name_resolution_automatic_mutation=false"
		if len(unscopedResolvers) == 1 {
			automaticMutation = "bootwright_controller_name_resolution_automatic_mutation=true"
		}
		mutationSelected := "bootwright_controller_name_resolution_mutation_selected=false"
		if controllerMutationSelected {
			mutationSelected = "bootwright_controller_name_resolution_mutation_selected=true"
		}
		for _, resolver := range resolvers {
			if !target.FabricHostIncluded(resolver.MachineRef) {
				continue
			}
			runtimeState := controllerNameResolutionStateForTarget(state, resolver)
			runtimeTargets := render.ControllerNameResolutionTargets(runtimeState)
			if len(runtimeTargets) != 1 || runtimeTargets[0].ProviderName != resolver.ProviderName || runtimeTargets[0].Name != resolver.Name || runtimeTargets[0].MachineRef != resolver.MachineRef {
				return nil, fmt.Errorf("controller name-resolution target %s/%s on %s did not narrow to exactly that managed service", resolver.ProviderName, resolver.Name, resolver.MachineRef)
			}
			var unscopedResolver render.ControllerNameResolutionTarget
			unscopedMatches := 0
			for _, candidate := range unscopedResolvers {
				if candidate.ProviderName == resolver.ProviderName && candidate.Name == resolver.Name && candidate.MachineRef == resolver.MachineRef {
					unscopedResolver = candidate
					unscopedMatches++
				}
			}
			if unscopedMatches != 1 {
				return nil, fmt.Errorf("controller name-resolution target %s/%s on %s has %d exact unscoped targets, want 1", resolver.ProviderName, resolver.Name, resolver.MachineRef, unscopedMatches)
			}
			desiredHashVars := render.ControllerNameResolutionTargetDesiredVars(hashState, unscopedResolver)
			if len(desiredHashVars) != 1 {
				return nil, fmt.Errorf("controller name-resolution target %s/%s on %s has no exact unscoped desired projection", resolver.ProviderName, resolver.Name, resolver.MachineRef)
			}
			runtimeDesiredVars := render.ControllerNameResolutionTargetDesiredVars(runtimeState, runtimeTargets[0])
			taskDesiredHashVars := desiredHashVars
			if controllerMutationSelected && !reflect.DeepEqual(runtimeDesiredVars, desiredHashVars) {
				return nil, &controllerNameResolutionScopeError{provider: resolver.ProviderName, name: resolver.Name, host: resolver.MachineRef, consumers: append([]string(nil), unscopedResolver.ConsumerClusters...)}
			}
			taskID := "controller-name-resolution." + resolver.ProviderName + "." + resolver.Name
			failureTargets := make([]remedy.Target, 0, len(unscopedResolver.ConsumerClusters))
			for _, cluster := range unscopedResolver.ConsumerClusters {
				failureTargets = append(failureTargets, remedy.Target{Role: remedy.TargetRoleClusterRoot, Name: cluster})
			}
			failureAction := remedy.ActionReconcileSharedServiceThenRetrySameSelection
			if controllerMutationSelected {
				failureAction = remedy.ActionResumeControllerDNSMutation
			}
			taskIDs = append(taskIDs, taskID)
			if err := graph.Add(Activity{
				ID:       taskID,
				Requires: []CapabilityRef{serviceEndpointReadyCapability(resolver.MachineRef)},
				Task: ApplyTask{
					Entry: TaskLedgerEntry{
						ID:           taskID,
						Kind:         ApplyTaskKindControllerNameResolution,
						Label:        "controller name resolution " + resolver.ProviderName + "/" + resolver.Name,
						Host:         resolver.MachineRef,
						ResourceKeys: []string{controllerNameResolutionResource},
						Status:       TaskStatusPending,
					},
					Playbook:        applyControllerNameResolution,
					Limit:           render.GroupControllerHosts,
					Forks:           1,
					State:           runtimeState,
					DesiredHashVars: taskDesiredHashVars,
					ExtraVarPairs:   []string{automaticMutation, mutationSelected},
					FailureRemedy: remedy.Request{
						Action:  failureAction,
						Targets: failureTargets,
					},
					ExecutionClass: func() ApplyTaskExecutionClass {
						if controllerMutationSelected {
							return ""
						}
						return ApplyTaskExecutionLiveProof
					}(),
				},
			}); err != nil {
				return nil, err
			}
		}
	}
	return taskIDs, nil
}

func controllerNameResolutionStateForTarget(state v1alpha1.State, target render.ControllerNameResolutionTarget) v1alpha1.State {
	out := state
	out.Environments = append([]v1alpha1.Environment(nil), state.Environments...)
	entryNames := make(map[string]bool, len(target.EntryNames))
	for _, name := range target.EntryNames {
		entryNames[name] = true
	}
	for i := range out.Environments {
		env := out.Environments[i]
		infra := env.Spec.InfraComponents
		entries := make([]v1alpha1.EnvironmentNameResolutionComponent, 0, len(infra.NameResolution))
		for _, entry := range infra.NameResolution {
			if entry.Management == v1alpha1.EnvironmentComponentManaged &&
				entry.ComponentRef.Name == target.Name && entryNames[entry.Name] {
				entries = append(entries, entry)
			}
		}
		infra.NameResolution = entries
		env.Spec.InfraComponents = infra
		out.Environments[i] = env
	}
	return out
}
