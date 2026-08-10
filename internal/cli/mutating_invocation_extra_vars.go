package cli

import (
	"encoding/json"
	"fmt"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func appendMutatingInvocationExtraVars(plan *converge.WorkflowPlan, invocation resolvedInvocation, effectiveReclaimDevices string) error {
	pairs, err := mutatingInvocationExtraVars(invocation, effectiveReclaimDevices)
	if err != nil {
		return err
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, pairs...)
	return nil
}

func appendControllerNameResolutionInvocationExtraVars(tasks []workflow.ApplyTask, invocation resolvedInvocation) error {
	controllerDNS, err := controllerNameResolutionRetryInvocation(invocation)
	if err != nil {
		return err
	}
	for i := range tasks {
		if tasks[i].Entry.Kind != workflow.ApplyTaskKindControllerNameResolution {
			continue
		}
		values := map[string]any{
			converge.ApplyControllerDNSInvocationExtraVar: controllerDNS.String(),
		}
		switch tasks[i].FailureRemedy.Action {
		case remedy.ActionResumeControllerDNSMutation:
			if _, err := exactClusterRootRemedyTargets(tasks[i].FailureRemedy); err != nil {
				return fmt.Errorf("controller name-resolution task %s: %w", tasks[i].Entry.ID, err)
			}
		case remedy.ActionReconcileSharedServiceThenRetrySameSelection:
			repair, resume, err := sharedServiceRepairCommands(tasks[i].FailureRemedy, invocation)
			if err != nil {
				return fmt.Errorf("controller name-resolution task %s: %w", tasks[i].Entry.ID, err)
			}
			values[converge.ApplyControllerDNSRepairInvocationExtraVar] = repair.String()
			values[converge.ApplyControllerDNSResumeInvocationExtraVar] = resume.String()
		default:
			return fmt.Errorf("controller name-resolution task %s has no closed typed recovery", tasks[i].Entry.ID)
		}
		data, err := json.Marshal(values)
		if err != nil {
			return err
		}
		tasks[i].ExtraVarPairs = append(tasks[i].ExtraVarPairs, string(data))
	}
	return nil
}

func mutatingInvocationExtraVars(invocation resolvedInvocation, effectiveReclaimDevices string) ([]string, error) {
	current, err := invocation.retry(retryIntent{})
	if err != nil {
		return nil, err
	}
	values := map[string]any{
		converge.MutatingInvocationExtraVar: current.String(),
	}
	if invocation.verb == invocationApply {
		reconcile, err := invocation.retry(retryIntent{mode: workflow.ApplyModeReconcile})
		if err != nil {
			return nil, err
		}
		rebuild, err := invocation.retry(retryIntent{
			mode:                   workflow.ApplyModeRebuild,
			requiredAuthorizations: []string{authorizeDataLoss},
		})
		if err != nil {
			return nil, err
		}
		controllerDNS, err := controllerNameResolutionRetryInvocation(invocation)
		if err != nil {
			return nil, err
		}
		fullInvocation := invocation
		fullInvocation.flags.selection.stage = ""
		fullInvocation.flags.selection.through = ""
		full, err := fullInvocation.retry(retryIntent{})
		if err != nil {
			return nil, err
		}
		throughBaseInvocation := invocation
		throughBaseInvocation.flags.selection.stage = ""
		throughBaseInvocation.flags.selection.through = converge.PhaseBase
		throughBase, err := throughBaseInvocation.retry(retryIntent{})
		if err != nil {
			return nil, err
		}
		reclaim, preservedDevices, err := invocation.applyRuntimeReclaimRetryTemplate(effectiveReclaimDevices)
		if err != nil {
			return nil, err
		}
		values[converge.ApplyReconcileInvocationExtraVar] = reconcile.String()
		values[converge.ApplyRebuildInvocationExtraVar] = rebuild.String()
		values[converge.ApplyReclaimInvocationExtraVar] = reclaim.String()
		values[converge.ApplyReclaimDevicesExtraVar] = preservedDevices
		values[converge.ApplyControllerDNSInvocationExtraVar] = controllerDNS.String()
		values[converge.ApplyFullInvocationExtraVar] = full.String()
		values[converge.ApplyThroughBaseInvocationExtraVar] = throughBase.String()
	}
	if invocation.verb == invocationReplaceArbiter {
		degraded, err := invocation.retry(retryIntent{requiredAuthorizations: []string{authorizeDegradedQuorum}})
		if err != nil {
			return nil, err
		}
		sameSite, err := invocation.retry(retryIntent{requiredAuthorizations: []string{authorizeSameSiteArbiter}})
		if err != nil {
			return nil, err
		}
		unreachable, err := invocation.retry(retryIntent{requiredAuthorizations: []string{authorizeUnreachableNodes}})
		if err != nil {
			return nil, err
		}
		values[converge.ArbiterDegradedInvocationExtraVar] = degraded.String()
		values[converge.ArbiterSameSiteInvocationExtraVar] = sameSite.String()
		values[converge.ArbiterUnreachableInvocationExtraVar] = unreachable.String()
	}
	data, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	return []string{string(data)}, nil
}

func controllerNameResolutionRetryInvocation(invocation resolvedInvocation) (retryCommand, error) {
	scope, err := converge.ApplyRangeScope(invocation.flags.selection.stage, invocation.flags.selection.through)
	if err != nil {
		return retryCommand{}, err
	}
	next := invocation
	if !converge.ScopeIncludesApplyPhase(scope, converge.PhaseFabric) {
		next.flags.selection.stage = converge.PhaseFabric
		next.flags.selection.through = scope.PhaseNames[len(scope.PhaseNames)-1]
	}
	intent := retryIntent{}
	if invocation.flags.mode == workflow.ApplyModeCreate {
		intent.mode = workflow.ApplyModeReconcile
	}
	return next.retry(intent)
}
