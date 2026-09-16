package baremetal

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// MachineCapability claims one operator-owned physical machine and proves it is
// the machine the desired state names. It realizes no hardware: the server
// exists before Bootwright is told about it and outlives every context that
// uses it, so what this block establishes is identity, and what it takes back
// is only a claim.
type MachineCapability struct{ runner Runner }

func NewMachine(runner Runner) MachineCapability { return MachineCapability{runner: runner} }

const machineVariable = "bootwright_substrate_physical"

// Plan derives one block per physical Machine. It reads no host, endpoint or
// Secret material and performs no effect.
func (c MachineCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, refusal("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	requests, err := Requests(input.State.Effective(), input.Controller, input.Context.Name)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	digest := ContentDigest()
	for _, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID:             request.Identity.Block,
			Description:    description(input.Verb, request),
			Stage:          reconciliation.StageMachines,
			Impacts:        impacts(input.Verb, request),
			Groups:         groups(input.Verb, request),
			Kind:           Kind,
			Object:         request.Identity.Object,
			Implementation: Implementation,
			ContentDigest:  digest,
			Request:        canonical,
		})
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
		if !request.Placement.Local() {
			continue
		}
		plan.Reservations = append(plan.Reservations, prerequisites.HostReservation{
			Context: input.Context.Name, Kind: "substrate-physical", Service: request.Identity.Object,
			Keys: request.ReservationKeys(),
		})
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

// description states what the planned verb does. A removal says it retains,
// because nothing about the machine is taken away with the claim.
func description(verb reconciliation.Verb, request Request) string {
	if verb == reconciliation.Destroy {
		return "release the claim on " + request.Identity.Object + " and retain the machine"
	}
	return "claim the physical machine " + request.Identity.Object + " and prove its identity"
}

// impacts list what the verb changes on the machine. A removal changes nothing
// about it, so it lists nothing at all.
func impacts(verb reconciliation.Verb, request Request) []string {
	if verb == reconciliation.Destroy {
		return nil
	}
	return []string{"claim-machine " + request.Controller.Endpoint}
}

func groups(verb reconciliation.Verb, request Request) []reconciliation.Group {
	machines := []string{request.Identity.Object}
	steps := [][2]string{
		{"reach-controller", "reach the machine's management controller"},
		{"prove-identity", "prove this is the exact machine the declaration names"},
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{{"release-claim", "release the claim and leave the machine as it is"}}
	}
	out := make([]reconciliation.Group, 0, len(steps))
	for _, step := range steps {
		out = append(out, reconciliation.Group{ID: step[0], Description: step[1], Machines: machines})
	}
	return out
}

// Removal reads a frozen physical-machine block as the release of the claim it
// took. The server and everything installed on it are retained, so removing it
// consumes nothing.
func (c MachineCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{
		Description: description(reconciliation.Destroy, request),
		Impacts:     impacts(reconciliation.Destroy, request),
		Groups:      groups(reconciliation.Destroy, request),
	}, nil
}

func (c MachineCapability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c MachineCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

func (c MachineCapability) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, execution, operation, request)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	var outcome reconciliation.Outcome
	switch result.Outcome {
	case "changed":
		outcome = reconciliation.OutcomeChanged
	case "unchanged":
		outcome = reconciliation.OutcomeUnchanged
	default:
		return unknown, refusal("lifecycle.state", "the machine adapter reported no usable outcome", "")
	}
	digest := execution.Block.RequestDigest
	if operation == "apply" {
		err = ValidatePresence(result.Evidence, request, digest)
	} else {
		err = ValidateAbsence(result.Evidence, digest)
	}
	if err != nil {
		return unknown, err
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

// Observe is read-only. The exact machine answering with its complete declared
// inventory is positive completion. There is no absence to observe, because
// this block created nothing whose removal could be seen, and no partial
// realization for the same reason: the proof either holds or it does not.
func (c MachineCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, execution, "observe", request)
	if err != nil {
		recordObservationFailure(ctx, execution, err)
		return unknown, nil
	}
	if ValidatePresence(result.Evidence, request, execution.Block.RequestDigest) == nil {
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: result.Evidence}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: result.Evidence}, nil
}

// Quiescent is derived rather than probed. The removal takes back a claim,
// which nothing reads, and leaves the machine and whatever runs on it exactly
// as they are, so there is nothing a removal here could interrupt.
func (MachineCapability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its removal retains the machine"}, nil
}

func (c MachineCapability) prepare(ctx context.Context, execution lifecycle.Execution) (Request, error) {
	if err := ctx.Err(); err != nil {
		return Request{}, err
	}
	if c.runner == nil {
		return Request{}, refusal("lifecycle.state", "the machine adapter is not configured", "")
	}
	return DecodeRequest(execution.Block.Request)
}

func (c MachineCapability) run(ctx context.Context, execution lifecycle.Execution, operation string, request Request) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	materials := []lifecycle.MaterialFile{
		{Name: "bmc-user", Part: secrets.UsernamePart, Secret: request.Controller.CredentialsRef, Variable: "controllerUser"},
		{Name: "bmc-password", Part: secrets.PasswordPart, Secret: request.Controller.CredentialsRef, Variable: "controllerPassword"},
	}
	return c.runner.Run(ctx, lifecycle.RunRequest{
		Implementation: Implementation,
		Operation:      operation,
		Variable:       machineVariable,
		Digest:         execution.Block.RequestDigest,
		Canonical:      canonical,
		Placement:      request.Placement,
		Materials:      append(materials, lifecycle.Materials(request.Placement)...),
		Sudo:           request.Placement.SudoPasswordRef,
		Launch:         execution.Launch,
		Bundle:         execution.Bundle,
		Area:           execution.Area,
		Material:       execution.Material,
		Log:            execution.Log,
		Progress:       execution.Progress,
		Output:         execution.Output,
	})
}

func recordObservationFailure(ctx context.Context, execution lifecycle.Execution, err error) {
	if execution.Log == nil {
		return
	}
	for _, reported := range diagnostics.Of(err) {
		_ = execution.Log(ctx, operationstore.LogRecord{
			Event: "observation-failed", Block: execution.Block.ID, Detail: reported.Code + ": " + reported.Message,
		})
	}
}
