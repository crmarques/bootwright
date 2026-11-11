package hardware

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

type Capability struct{ runner Runner }

var outcomes = map[string]reconciliation.Outcome{"changed": reconciliation.OutcomeChanged, "unchanged": reconciliation.OutcomeUnchanged}

func New(runner Runner) Capability { return Capability{runner: runner} }

func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, refusal("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	requests, err := Requests(input.State.Effective(), input.Controller, input.Context.Name)
	if err != nil {
		return plan, err
	}
	for _, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return plan, err
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID: request.Identity.Block, Description: description(input.Verb, request), Stage: reconciliation.StageInfrastructureServices,
			Impacts: impacts(input.Verb, request), Groups: groups(input.Verb, request), Consumes: []string{},
			Kind: Kind, Object: request.Identity.Object, Family: Family, Implementation: Implementation, ContentDigest: ContentDigest(), Request: canonical,
		})
		plan.Reservations = append(plan.Reservations, prerequisites.HostReservation{Context: input.Context.Name, Kind: ClaimKind, Service: request.Identity.Object})
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
	}
	plan.Secrets = lifecycle.SecretNames(plan.Secrets...)
	return plan, nil
}

func description(verb reconciliation.Verb, request Request) string {
	if verb == reconciliation.Destroy {
		return "release the claim on the server behind " + request.Identity.Object + " and retain it"
	}
	return "claim the server behind " + request.Identity.Object + " by the permanent hardware addresses it reports"
}

func impacts(verb reconciliation.Verb, request Request) []string {
	if verb == reconciliation.Destroy {
		return []string{}
	}
	return []string{"claim-machine " + machineref.SSHHost(request.Placement.Address, request.Placement.Port)}
}

func groups(verb reconciliation.Verb, request Request) []reconciliation.Group {
	step := reconciliation.Group{ID: "read-hardware", Description: "read the permanent hardware addresses the host reports"}
	if verb == reconciliation.Destroy {
		step = reconciliation.Group{ID: "release-claim", Description: "release the claim and leave the server as it is"}
	}
	step.Machines = []string{request.Identity.Object}
	return []reconciliation.Group{step}
}

func (Capability) Claimed(api.Catalog) []string { return nil }

func (Capability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: description(reconciliation.Destroy, request), Impacts: impacts(reconciliation.Destroy, request),
		Groups: groups(reconciliation.Destroy, request), Consumes: []string{}}, nil
}

func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	result, request, err := c.run(ctx, execution, "apply")
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome, usable := outcomes[result.Outcome]
	if !usable {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, refusal("lifecycle.state", "the machine hardware adapter reported no usable outcome", "")
	}
	if _, err := presence(result.Evidence, execution.Block.RequestDigest); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	claimed, err := claim(execution.Context, request, result.Evidence)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	if len(claimed.Keys) == 0 {
		lifecycle.ReportWarning(ctx, execution, UnclaimedServer, "Machine/"+request.Identity.Object+
			" reports no permanent hardware address, so the server behind it is not claimed and a context that drives it by its BMC is not refused")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func (c Capability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	result, _, err := c.run(ctx, execution, "destroy")
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome, usable := outcomes[result.Outcome]
	evidence, err := decodeEvidence(result.Evidence)
	if err != nil || !usable || !evidence.Absent || !evidence.Postcondition || evidence.Request != execution.Block.RequestDigest {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, refusal("lifecycle.state", "the machine hardware adapter did not prove the release of its claim", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	result, _, err := c.run(ctx, execution, "observe")
	if err != nil {
		return unknown, err
	}
	unknown.Evidence = result.Evidence
	if _, err := presence(result.Evidence, execution.Block.RequestDigest); err != nil {
		return unknown, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Outcome: reconciliation.OutcomeUnchanged, Evidence: result.Evidence}, nil
}

func (Capability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	if _, err := DecodeRequest(execution.Block.Request); err != nil {
		return unknown, err
	}
	evidence, err := json.Marshal(Evidence{Absent: true, Addresses: []string{}, Postcondition: true, Request: execution.Block.RequestDigest, Version: EvidenceVersion})
	if err != nil {
		return unknown, refusal("lifecycle.state", "the machine hardware removal evidence cannot be encoded", "")
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func (Capability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its removal retains the server"}, nil
}

func (Capability) PrecedesController(reconciliation.BlockDefinition) bool { return true }

func (c Capability) run(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.RunResult, Request, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, Request{}, err
	}
	request, err := DecodeRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	if execution.Block.Kind != Kind || execution.Block.Object != request.Identity.Object {
		return lifecycle.RunResult{}, request, refusal("lifecycle.state", "the machine hardware request does not match its frozen block identity", "")
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, request, refusal("lifecycle.state", "the machine hardware adapter is not configured", "")
	}
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: Implementation, Operation: operation, Variable: variable, Canonical: canonical, Placement: request.Placement,
	}))
	return result, request, err
}

func (Capability) ProvedClaim(contextName string, block reconciliation.Block, evidence json.RawMessage) (prerequisites.HostReservation, bool, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return prerequisites.HostReservation{}, false, err
	}
	reservation, err := claim(contextName, request, evidence)
	return reservation, err == nil && len(reservation.Keys) != 0, err
}

func claim(contextName string, request Request, data []byte) (prerequisites.HostReservation, error) {
	evidence, err := decodeEvidence(data)
	if err != nil {
		return prerequisites.HostReservation{}, err
	}
	if evidence.Absent || !evidence.Postcondition {
		return prerequisites.HostReservation{}, refusal("lifecycle.state", "the machine hardware evidence proves no read of its host", "")
	}
	if len(evidence.Addresses) > prerequisites.MaxReservationKeys {
		return prerequisites.HostReservation{}, refusal("lifecycle.state",
			"Machine/"+request.Identity.Object+" reports "+strconv.Itoa(len(evidence.Addresses))+" permanent hardware addresses, more than the "+
				strconv.Itoa(prerequisites.MaxReservationKeys)+" one claim holds, so the server behind it cannot be claimed",
			"place the work on a Machine whose host reports at most "+strconv.Itoa(prerequisites.MaxReservationKeys)+" physical network interfaces")
	}
	keys := make([]string, 0, len(evidence.Addresses))
	for _, address := range evidence.Addresses {
		keys = append(keys, substrate.MACReservationKey(address))
	}
	slices.Sort(keys)
	return prerequisites.HostReservation{Context: contextName, Kind: ClaimKind, Service: request.Identity.Object, Keys: slices.Compact(keys)}, nil
}

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return lifecycle.SecretNames(request.SecretReferences()...), nil
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretClasses(), nil
}

var _ lifecycle.ProvingClaimer = Capability{}

func (Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeRequest(block.Request)
	if err != nil || len(data) == 0 || verb == reconciliation.Destroy {
		return lifecycle.Unresolved{}, false
	}
	evidence, err := decodeEvidence(data)
	if err != nil || evidence.Request != block.RequestDigest {
		return lifecycle.Unresolved{}, false
	}
	_, refused := presence(data, block.RequestDigest)
	if refused == nil {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the network interfaces of Machine "+block.Object, lifecycle.PlacedOn(request.Placement), refused)
}
