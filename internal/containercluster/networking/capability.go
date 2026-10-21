package networking

import (
	"context"
	"encoding/json"
	"fmt"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/clusterobjects/driver"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func (c Capability) run(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.RunResult, Request, error) {
	request, err := DecodeRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	config := driver.Config{Implementation: Implementation, Variable: "bootwright_cluster_network", EvidenceVersion: evidenceVersion, Placement: request.Placement, Tool: controller.InstalledTool{Kind: request.Tool.Kind, Compatibility: request.Tool.Compatibility, Version: request.Tool.Version, Executable: "oc"}, BudgetSeconds: request.BudgetSeconds, ProducedInput: request.ProducedInputs[0]}
	result, err := driver.Run(ctx, c.runner, execution, operation, config)
	return result, request, err
}
func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	result, request, err := c.run(ctx, execution, "apply")
	safe := driver.SafeEvidence(result.Evidence, execution.Block.RequestDigest, evidenceVersion)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err), Evidence: safe}, err
	}
	program, err := request.Objects()
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown, Evidence: safe}, err
	}
	effect, err := driver.Effect(result.Evidence, program, execution.Block.RequestDigest, evidenceVersion)
	if err != nil || effect != reconciliation.EffectCompleted {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown, Evidence: safe}, fmt.Errorf("cluster network adapter did not prove completion")
	}
	outcome := reconciliation.OutcomeChanged
	if result.Outcome == "unchanged" {
		outcome = reconciliation.OutcomeUnchanged
	} else if result.Outcome != "changed" {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown, Evidence: safe}, fmt.Errorf("cluster network adapter returned unknown outcome")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: safe}, nil
}
func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	result, request, err := c.run(ctx, execution, "observe")
	safe := driver.SafeEvidence(result.Evidence, execution.Block.RequestDigest, evidenceVersion)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: safe}, err
	}
	program, err := request.Objects()
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: safe}, err
	}
	effect, err := driver.Effect(result.Evidence, program, execution.Block.RequestDigest, evidenceVersion)
	if err != nil {
		effect = reconciliation.EffectUnknown
	}
	return lifecycle.Observation{Effect: effect, Evidence: safe}, nil
}
func (c Capability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	r, err := DecodeRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: "retire secondary networks with " + r.Cluster + "; objects leave with cluster disks or stay on a running cluster", Impacts: []string{}}, nil
}
func (c Capability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Result{}, err
	}
	if _, err := DecodeRequest(e.Block.Request); err != nil {
		return lifecycle.Result{}, err
	}
	data, err := driver.Retirement(e.Block.RequestDigest, evidenceVersion)
	return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: data}, err
}
func (c Capability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	result, err := c.Destroy(ctx, e)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: result.Evidence}, nil
}
func (c Capability) Quiescent(ctx context.Context, p lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{}, err
	}
	if _, err := DecodeRequest(p.Block.Request); err != nil {
		return lifecycle.Quiescence{}, err
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "secondary networks retire with their cluster"}, nil
}
func (c Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	if verb == reconciliation.Destroy {
		return lifecycle.Unresolved{}, false
	}
	if _, err := DecodeRequest(block.Request); err != nil {
		return lifecycle.Unresolved{}, false
	}
	e, err := driver.DecodeEvidence(data, block.RequestDigest, evidenceVersion)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	for _, object := range e.Observations {
		if object.Ownership == co.Foreign || object.Ownership == co.Unknown {
			return lifecycle.Unresolved{Reason: object.Kind + "/" + object.Name + " is foreign or unreadable", Remedy: "destroy and apply again"}, true
		}
	}
	return lifecycle.Unresolved{Reason: "secondary network objects or readiness remain unproven", Remedy: "repeat apply to inspect the node enactments and network conditions"}, true
}

func (c Capability) RetiresOffline() bool { return true }
