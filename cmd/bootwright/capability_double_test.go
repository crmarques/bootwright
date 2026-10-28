//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// contractDoubleImplementation names the synthetic implementation the
// capability contract suite runs beside every bound one, so the suite holds
// any implementation of the port and not only the shapes this build ships.
const contractDoubleImplementation = "contract-double-v1"

// contractDouble is the smallest implementation of the Capability port that
// honors its contract: one block holding a marker on the controller Machine,
// crossing its frozen request to the runner unchanged, and reading only
// evidence that names that request. Its helpers and its adapter field carry
// names nothing else in this package declares, because
// TestDocsNameEveryCheckThatWalksTheWorkingTree follows calls by name and
// the docs checks that plan through every binding reach these methods.
type contractDouble struct {
	adapter func(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

type contractDoubleRequest struct {
	Context    string `json:"context"`
	Controller string `json:"controller"`
}

type contractDoubleEvidence struct {
	Request       string `json:"request"`
	Present       bool   `json:"present"`
	Absent        bool   `json:"absent"`
	Postcondition bool   `json:"postcondition"`
	Marker        string `json:"marker"`
}

func contractDoubleRow() contractRow {
	return contractRow{
		kind: string(api.Machine), implementation: contractDoubleImplementation, example: "lab-rhel",
		synthetic: "a synthetic double has no collection role",
		presence: func(*testing.T, lifecycle.Execution) map[string]any {
			return map[string]any{"present": true, "postcondition": true, "marker": "owned"}
		},
		drift: map[string]any{"marker": "foreign"},
	}
}

func (d contractDouble) doubleDefinition(request contractDoubleRequest) (reconciliation.BlockDefinition, error) {
	canonical, err := json.Marshal(request)
	if err != nil {
		return reconciliation.BlockDefinition{}, err
	}
	return reconciliation.BlockDefinition{
		ID: "contract-double-" + request.Context, Description: "hold the contract double's marker for " + request.Context,
		Stage: reconciliation.StageInfrastructureServices, Kind: string(api.Machine), Object: request.Controller,
		Implementation: contractDoubleImplementation, ContentDigest: strings.Repeat("d", 64), Request: canonical,
		Groups: []reconciliation.Group{{ID: "hold", Description: "hold the double's marker", Machines: []string{request.Controller}}},
	}, nil
}

func (contractDouble) Claimed(api.Catalog) []string { return nil }

func (d contractDouble) Plan(_ context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	definition, err := d.doubleDefinition(contractDoubleRequest{Context: input.Context.Name, Controller: input.Controller})
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	return lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{definition}}, nil
}

func (d contractDouble) Removal(_ context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	var request contractDoubleRequest
	if err := json.Unmarshal(block.Request, &request); err != nil || request.Context == "" || request.Controller == "" {
		return lifecycle.Removal{}, diagnostics.NewFailure("lifecycle.state", "the contract double's frozen request is malformed", "")
	}
	definition, err := d.doubleDefinition(request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: definition.Description, Impacts: definition.Impacts, Consumes: definition.Consumes, Groups: definition.Groups}, nil
}

func (d contractDouble) invoke(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.RunResult, error) {
	return d.adapter(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: contractDoubleImplementation, Operation: operation, Variable: "contract_double",
		Canonical: execution.Block.Request, Deadline: time.Minute,
	}))
}

func (d contractDouble) doubleEvidence(execution lifecycle.Execution, data json.RawMessage) (contractDoubleEvidence, bool) {
	var evidence contractDoubleEvidence
	if err := json.Unmarshal(data, &evidence); err != nil || evidence.Request != execution.Block.RequestDigest {
		return contractDoubleEvidence{}, false
	}
	return evidence, true
}

func (e contractDoubleEvidence) doubleRealized() bool {
	return e.Present && e.Postcondition && e.Marker == "owned"
}

func (e contractDoubleEvidence) doubleRemoved() bool { return e.Absent && e.Postcondition }

func (e contractDoubleEvidence) doublePartial() bool {
	return e.Present && !e.Postcondition && e.Marker == "owned"
}

func (d contractDouble) doubleMutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	result, err := d.invoke(ctx, execution, operation)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	evidence, named := d.doubleEvidence(execution, result.Evidence)
	proved := named && (operation == "apply" && evidence.doubleRealized() || operation == "destroy" && evidence.doubleRemoved())
	if !proved || result.Outcome != "changed" && result.Outcome != "unchanged" {
		return unknown, diagnostics.NewFailure("lifecycle.unknown", "the contract double's evidence proves nothing of its request", "")
	}
	return lifecycle.Result{Outcome: reconciliation.Outcome(result.Outcome), Evidence: result.Evidence}, nil
}

func (d contractDouble) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return d.doubleMutate(ctx, execution, "apply")
}

func (d contractDouble) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return d.doubleMutate(ctx, execution, "destroy")
}

func (d contractDouble) doubleObserve(ctx context.Context, execution lifecycle.Execution, removal bool) (lifecycle.Observation, error) {
	result, err := d.invoke(ctx, execution, lifecycle.ObserveOperation)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	evidence, named := d.doubleEvidence(execution, result.Evidence)
	effect := reconciliation.EffectUnknown
	switch {
	case !named:
	case evidence.doubleRemoved() && removal, evidence.doubleRealized() && !removal:
		effect = reconciliation.EffectCompleted
	case evidence.doubleRemoved(), evidence.doubleRealized():
		effect = reconciliation.EffectNoEffect
	case evidence.doublePartial():
		effect = reconciliation.EffectPartial
	}
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence}, nil
}

func (d contractDouble) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return d.doubleObserve(ctx, execution, false)
}

func (d contractDouble) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return d.doubleObserve(ctx, execution, true)
}

func (d contractDouble) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent}, nil
}

func (d contractDouble) Unresolved(_ reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	evidence, named := d.doubleEvidence(lifecycle.Execution{Block: block}, data)
	if !named || evidence.Marker == "owned" || evidence.Marker == "" {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Unresolved{Reason: "the double's marker belongs to " + evidence.Marker, Remedy: "remove the foreign marker"}, true
}
