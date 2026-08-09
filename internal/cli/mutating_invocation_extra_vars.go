package cli

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/converge"
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
