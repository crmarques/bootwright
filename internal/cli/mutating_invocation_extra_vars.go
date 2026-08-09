package cli

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func appendMutatingInvocationExtraVars(plan *converge.WorkflowPlan, invocation resolvedInvocation) error {
	pairs, err := mutatingInvocationExtraVars(invocation)
	if err != nil {
		return err
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, pairs...)
	return nil
}

func mutatingInvocationExtraVars(invocation resolvedInvocation) ([]string, error) {
	current, err := invocation.retry(retryIntent{})
	if err != nil {
		return nil, err
	}
	values := map[string]string{
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
		values[converge.ApplyReconcileInvocationExtraVar] = reconcile.String()
		values[converge.ApplyRebuildInvocationExtraVar] = rebuild.String()
		values[converge.ApplyFullInvocationExtraVar] = full.String()
		values[converge.ApplyThroughBaseInvocationExtraVar] = throughBase.String()
	}
	data, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	return []string{string(data)}, nil
}
