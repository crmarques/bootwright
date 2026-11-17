package baremetal

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
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
		if input.Preparation != nil && !slices.Contains(input.Preparation.Targets, request.Identity.Object) {
			continue
		}
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
			Family:         substrate.MachineFamily,
			Implementation: Implementation,
			ContentDigest:  digest,
			Request:        canonical,
		})
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
		if !request.Placement.Local() {
			continue
		}
		plan.Reservations = append(plan.Reservations, infrastructureservices.HostReservation{
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
	return lifecycle.StepGroups(steps, machines)
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
	return lifecycle.MutateBlock(ctx, c.mutation("apply"), execution, "apply")
}

func (c MachineCapability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return lifecycle.MutateBlock(ctx, c.mutation("destroy"), execution, "destroy")
}

func (c MachineCapability) mutation(operation string) lifecycle.Mutation[Request] {
	return lifecycle.Mutation[Request]{
		Prepare: c.prepare, Run: func(ctx context.Context, execution lifecycle.Execution, _ string, request Request) (lifecycle.RunResult, error) {
			return c.run(ctx, execution, operation, request)
		},
		Reading: func(request Request, digest string) lifecycle.EffectReading {
			return lifecycle.EffectReading{
				Presence: func(evidence []byte) error { return ValidatePresence(evidence, request, digest) },
				Absence:  func(evidence []byte) error { return ValidateAbsence(evidence, digest) },
			}
		},
		Unusable: refusal("lifecycle.state", "the machine adapter reported no usable outcome", ""),
	}
}

// Observe is read-only. The exact machine answering with its complete declared
// inventory is positive completion. There is no absence to observe, because
// this block created nothing whose removal could be seen, and no partial
// realization for the same reason: the proof either holds or it does not. The
// observation repeats the apply's own proof, so the outcome its adapter
// publishes is the one the apply's would have been.
func (c MachineCapability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, execution, "observe", request)
	if err != nil {
		return unknown, err
	}
	if ValidatePresence(result.Evidence, request, execution.Block.RequestDigest) == nil {
		return lifecycle.Observation{
			Effect: reconciliation.EffectCompleted, Outcome: result.Outcome, Evidence: result.Evidence,
		}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: result.Evidence}, nil
}

// ObserveRemoval runs nothing. The removal releases only the claim and never
// contacts the machine, so nothing the machine reports could prove it either
// way: resolving it is always its completion, with the evidence it publishes.
func (c MachineCapability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if _, err := c.prepare(ctx, execution); err != nil {
		return unknown, err
	}
	evidence, err := json.Marshal(Evidence{Absent: true, Addresses: []string{}, Postcondition: true, Request: execution.Block.RequestDigest})
	if err != nil {
		return unknown, refusal("lifecycle.state", "the machine removal evidence cannot be encoded", "")
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
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
	if request.Controller.TrustBundleRef != "" {
		materials = append(materials,
			lifecycle.MaterialFile{Name: "bmc-ca", Part: secrets.CertificatePart, Secret: request.Controller.TrustBundleRef, Variable: "controllerCA"})
	}
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: Implementation, Operation: operation, Variable: machineVariable,
		Canonical: canonical, Placement: request.Placement, Materials: materials,
		Refusals: map[string]error{substrate.RefusalTargetUnanswered: substrate.UnansweredTarget(request.Identity.Object, request.Controller.Endpoint)},
	}))
}

var systemToken = regexp.MustCompile(`^[0-9a-z][0-9a-z._-]{0,127}$`)

func (MachineCapability) ProvedClaim(contextName string, block reconciliation.Block, evidence json.RawMessage) (infrastructureservices.HostReservation, bool, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil || !request.Placement.Local() {
		return infrastructureservices.HostReservation{}, false, err
	}
	identity, err := ProvedIdentity(evidence)
	if err != nil {
		return infrastructureservices.HostReservation{}, false, err
	}
	claim := infrastructureservices.HostReservation{Context: contextName, Kind: "substrate-physical", Service: request.Identity.Object, Keys: request.ReservationKeys()}
	if identity.UUID != "" {
		system := strings.ToLower(identity.UUID)
		if !systemToken.MatchString(system) {
			return infrastructureservices.HostReservation{}, false, refusal("lifecycle.state",
				"the system UUID the management controller at "+request.Controller.Endpoint+" reported cannot name a host claim",
				"correct spec.hardware.bmc.address on Machine/"+request.Identity.Object+" so it names a BMC that reports a standard UUID")
		}
		claim.Keys = append(claim.Keys, infrastructureservices.SystemReservationKey(system).String())
	}
	addresses := request.Addresses()
	if len(claim.Keys)+len(addresses) > infrastructureservices.MaxReservationKeys {
		return infrastructureservices.HostReservation{}, false, refusal("lifecycle.state",
			"Machine/"+request.Identity.Object+" declares more than "+strconv.Itoa(infrastructureservices.MaxReservationKeys-len(claim.Keys))+" MAC addresses to claim the machine by",
			"correct spec.hardware.nics on Machine/"+request.Identity.Object)
	}
	if identity.UUID == "" && len(addresses) == 0 {
		return infrastructureservices.HostReservation{}, false, refusal("lifecycle.state",
			"the management controller at "+request.Controller.Endpoint+" reported no system UUID, and Machine/"+request.Identity.Object+
				" declares no MAC address to claim the machine by",
			"correct spec.hardware.nics on Machine/"+request.Identity.Object)
	}
	hardware := make([]string, 0, len(addresses))
	for _, address := range addresses {
		hardware = append(hardware, infrastructureservices.MACReservationKey(address).String())
	}
	slices.Sort(hardware)
	claim.Keys = append(claim.Keys, slices.Compact(hardware)...)
	return claim, true, nil
}

var _ lifecycle.ProvingClaimer = MachineCapability{}
