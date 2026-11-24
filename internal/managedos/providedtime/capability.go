package providedtime

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Capability struct{ runner Runner }

func New(runner Runner) Capability { return Capability{runner: runner} }

type Evidence struct {
	Changed       bool     `json:"changed"`
	Postcondition bool     `json:"postcondition"`
	Request       string   `json:"request"`
	Servers       []string `json:"servers"`
	State         string   `json:"state"`
	Version       string   `json:"version"`
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
		return plan, requestInvalid.Fail("provided time synchronization planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(api.Machine) {
		selections := machine.ProvidedTime(object, input.Controller)
		if len(selections) == 0 {
			continue
		}
		placement, err := machine.PlacementFor(object, input.Controller)
		if err != nil {
			return plan, err
		}
		servers, services, err := resolve(catalog, object, selections)
		if err != nil {
			return plan, err
		}
		id := Identity{Block: blockID(object.Name()), Context: input.Context.Name, Object: object.Name()}
		data, err := reconciliation.Freeze(Request{Identity: id, Placement: placement, Servers: servers, Version: Version}, "provided time synchronization")
		if err != nil {
			return plan, err
		}
		if _, err := decodeRequest(data); err != nil {
			return plan, err
		}
		list := strings.Join(servers, ", ")
		description := "synchronize the clock of " + id.Object + " with " + list
		impacts := []string{"point the chrony time sources of " + id.Object + " at " + list + " and prove its clock synchronized"}
		if input.Verb == reconciliation.Destroy {
			description, impacts = retentionDescription(id.Object), nil
		}
		requires := []reconciliation.ObjectRef{{Kind: string(api.Machine), Object: id.Object, Family: machine.HardwareFamily}}
		for _, service := range services {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.NTPServer), Object: service})
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID: id.Block, Description: description, Stage: reconciliation.StageMachines,
			Kind: string(api.Machine), Object: id.Object, Family: machine.ProvidedTimeFamily,
			Implementation: Implementation, ContentDigest: ContentDigest(), Request: data,
			Requires: requires, Impacts: impacts,
		})
		plan.Secrets = append(plan.Secrets, placement.SecretReferences()...)
	}
	plan.Secrets = lifecycle.SecretNames(plan.Secrets...)
	return plan, nil
}

func resolve(catalog api.Catalog, object api.Object, selections []api.Value) (servers, services []string, err error) {
	for _, selection := range selections {
		address, service, err := infrastructureservices.ServiceEndpoint(catalog, api.NTPServer, selection, object.Identity())
		if err != nil {
			return nil, nil, err
		}
		if service != "" && !slices.Contains(services, service) {
			services = append(services, service)
		}
		if !slices.Contains(servers, address) {
			servers = append(servers, address)
		}
	}
	slices.Sort(services)
	return servers, services, nil
}

func (Capability) Claimed(catalog api.Catalog) []string {
	controller := machine.ControllerName(catalog)
	var objects []string
	for _, object := range catalog.OfKind(api.Machine) {
		if len(machine.ProvidedTime(object, controller)) != 0 {
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
	result, state, err := c.run(ctx, execution, "apply", 15*time.Minute)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	if state != reconciliation.EffectCompleted || !result.Outcome.Settled() {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, timeUnproved.Fail("Machine/"+execution.Block.Object+" did not prove its clock synchronized", "")
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
		return lifecycle.RunResult{}, unknown, requestInvalid.Fail("provided time synchronization requires its adapter and original apply identity", "")
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: Implementation, Operation: operation, Variable: "bootwright_provided_time",
		Canonical: execution.Block.Request, Placement: request.Placement, Deadline: deadline,
		Refusals: refusals(request),
	}))
	if err != nil {
		return result, unknown, err
	}
	evidence, err := reconciliation.DecodeEvidence[Evidence](result.Evidence, 4096, "provided time synchronization")
	if err != nil || evidence.Version != EvidenceVersion || evidence.Request != execution.Block.RequestDigest ||
		!slices.Equal(evidence.Servers, request.Servers) {
		return result, unknown, nil
	}
	switch evidence.State {
	case "absent":
		if !evidence.Changed && !evidence.Postcondition {
			return result, reconciliation.EffectNoEffect, nil
		}
	case "partial":
		if !evidence.Changed && !evidence.Postcondition {
			return result, reconciliation.EffectPartial, nil
		}
	case "complete":
		if evidence.Postcondition {
			return result, reconciliation.EffectCompleted, nil
		}
		return result, reconciliation.EffectPartial, nil
	}
	return result, unknown, nil
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

func retentionDescription(machine string) string {
	return "retain the time synchronization of " + machine
}

func (Capability) Quiescent(ctx context.Context, _ lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "removal retains the Machine and its time configuration"}, nil
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
	evidence, err := json.Marshal(Evidence{Version: EvidenceVersion, State: "retained", Postcondition: true, Request: execution.Block.RequestDigest, Servers: request.Servers})
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, err
}

func (Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := decodeBlock(block)
	if verb != reconciliation.Apply || err != nil || len(data) == 0 {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the time synchronization of Machine "+block.Object, lifecycle.PlacedOn(request.Placement),
		timeUnproved.Fail("Machine/"+block.Object+" did not prove its clock synchronized with "+strings.Join(request.Servers, ", "), "check the host's chrony configuration and /etc/bootwright/provided-time.json before repeating the operation"))
}
