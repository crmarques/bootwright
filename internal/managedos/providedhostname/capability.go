package providedhostname

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Capability struct{ runner Runner }

func New(runner Runner) Capability { return Capability{runner: runner} }

type Evidence struct {
	Changed       bool   `json:"changed"`
	Hostname      string `json:"hostname"`
	Postcondition bool   `json:"postcondition"`
	Request       string `json:"request"`
	State         string `json:"state"`
	Version       string `json:"version"`
}

func (Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	var plan lifecycle.CapabilityPlan
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.Preparation != nil {
		return plan, nil
	}
	if input.State == nil {
		return plan, requestInvalid.Fail("provided OS hostname planning requires compiled state", "")
	}
	for _, object := range input.State.Effective().OfKind(api.Machine) {
		hostname := machine.ProvidedHostname(object, input.Controller)
		if hostname == "" {
			continue
		}
		placement, err := machine.PlacementFor(object, input.Controller)
		if err != nil {
			return plan, err
		}
		id := Identity{Block: blockID(object.Name()), Context: input.Context.Name, Object: object.Name()}
		request := Request{Hostname: hostname, Identity: id, Placement: placement, Version: Version}
		data, err := reconciliation.Freeze(request, "provided OS hostname")
		if err != nil {
			return plan, err
		}
		if _, err := decodeRequest(data); err != nil {
			return plan, err
		}
		description := "set the hostname of " + id.Object + " to " + hostname
		impacts := []string{"set the static and kernel hostname of " + id.Object + " to " + hostname}
		if input.Verb == reconciliation.Destroy {
			description, impacts = retentionDescription(id.Object), nil
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID: id.Block, Description: description, Stage: reconciliation.StageMachines,
			Kind: string(api.Machine), Object: id.Object, Family: machine.ProvidedHostnameFamily,
			Implementation: Implementation, ContentDigest: ContentDigest(), Request: data,
			Requires: []reconciliation.ObjectRef{{Kind: string(api.Machine), Object: id.Object, Family: machine.HardwareFamily}},
			Impacts:  impacts,
		})
		plan.Secrets = append(plan.Secrets, placement.SecretReferences()...)
	}
	plan.Secrets = lifecycle.SecretNames(plan.Secrets...)
	return plan, nil
}

func (Capability) Claimed(catalog api.Catalog) []string {
	controller, _ := lifecycle.ControllerMachine(catalog)
	var objects []string
	for _, object := range catalog.OfKind(api.Machine) {
		if machine.ProvidedHostname(object, controller) != "" {
			objects = append(objects, object.Identity())
		}
	}
	return objects
}

func (Capability) ProveBinding(_ context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	request, err := decodeBlock(block)
	if err != nil {
		return err
	}
	return request.Placement.ProveHostPin(contextName, material)
}

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := decodeBlock(block)
	return request.Placement.SecretReferences(), err
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := decodeBlock(block)
	return request.Placement.SecretClasses(), err
}

func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	result, state, err := c.run(ctx, execution, "apply", 5*time.Minute)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	if state != reconciliation.EffectCompleted || !result.Outcome.Settled() {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, hostnameUnproved.Fail("Machine/"+execution.Block.Object+" did not prove its hostname", "")
	}
	return lifecycle.Result{Outcome: result.Outcome, Evidence: result.Evidence}, nil
}

func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	result, state, err := c.run(ctx, execution, "observe", 2*time.Minute)
	return lifecycle.Observation{Effect: state, Outcome: reconciliation.OutcomeUnchanged, Evidence: result.Evidence}, err
}

func (c Capability) run(ctx context.Context, execution lifecycle.Execution, operation string, deadline time.Duration) (lifecycle.RunResult, reconciliation.EffectState, error) {
	unknown := reconciliation.EffectUnknown
	request, err := decodeBlock(execution.Block)
	if err != nil {
		return lifecycle.RunResult{}, unknown, err
	}
	if c.runner == nil || execution.ApplyOperation == "" {
		return lifecycle.RunResult{}, unknown, requestInvalid.Fail("provided OS hostname requires its adapter and original apply identity", "")
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: Implementation, Operation: operation, Variable: "bootwright_provided_hostname",
		Canonical: execution.Block.Request, Placement: request.Placement, Deadline: deadline,
		Refusals: refusals(request),
	}))
	if err != nil {
		return result, unknown, err
	}
	evidence, err := reconciliation.DecodeEvidence[Evidence](result.Evidence, 4096, "provided OS hostname")
	if err != nil || evidence.Version != EvidenceVersion || evidence.Request != execution.Block.RequestDigest ||
		evidence.Hostname != request.Hostname {
		return result, unknown, nil
	}
	switch evidence.State {
	case "absent":
		if !evidence.Changed && !evidence.Postcondition {
			return result, reconciliation.EffectNoEffect, nil
		}
	case "complete":
		if evidence.Postcondition {
			return result, reconciliation.EffectCompleted, nil
		}
	}
	return result, unknown, nil
}

func refusals(request Request) map[string]error {
	result := make(map[string]error)
	for reason, message := range map[string]string{
		"hostname-write":    "the host could not set its hostname to " + request.Hostname,
		"hostname-unproved": "the host's hostname is not " + request.Hostname + " after it was set; check what else manages the hostname, such as cloud-init or a configuration-management agent",
	} {
		result[reason] = hostnameRefused.Fail(fmt.Sprintf("Machine/%s: %s", request.Identity.Object, message), "")
	}
	return result
}

func (Capability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := decodeBlock(block)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: retentionDescription(request.Identity.Object)}, nil
}

func retentionDescription(machine string) string { return "retain the hostname of " + machine }

func (Capability) Quiescent(ctx context.Context, _ lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "removal retains the Machine and its hostname"}, nil
}

func (c Capability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	observation, err := c.ObserveRemoval(ctx, execution)
	return lifecycle.Result{Outcome: observation.Outcome, Evidence: observation.Evidence}, err
}

func (Capability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Outcome: reconciliation.OutcomeCanceled}, err
	}
	request, err := decodeBlock(execution.Block)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Outcome: reconciliation.OutcomeFailed}, err
	}
	evidence, err := json.Marshal(Evidence{Version: EvidenceVersion, State: "retained", Postcondition: true, Request: execution.Block.RequestDigest, Hostname: request.Hostname})
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, err
}

func (Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := decodeBlock(block)
	if verb != reconciliation.Apply || err != nil || len(data) == 0 {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the hostname of Machine "+block.Object, lifecycle.PlacedOn(request.Placement),
		hostnameUnproved.Fail("Machine/"+block.Object+" did not prove its hostname "+request.Hostname, "check the host's hostname before repeating the operation"))
}
