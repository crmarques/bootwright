package providedregistration

import (
	"context"
	"encoding/json"
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
	Consumer      string `json:"consumer"`
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
		return plan, requestInvalid.Fail("provided registration planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(api.Machine) {
		reference := machine.ProvidedSubscription(object)
		if reference == "" {
			continue
		}
		placement, err := machine.PlacementFor(object, input.Controller)
		if err != nil {
			return plan, err
		}
		entitlement, found := catalog.Find(api.Entitlement, reference)
		if !found {
			return plan, requestInvalid.Fail("Entitlement/"+reference+" of Machine/"+object.Name()+" is not in the selected state", "")
		}
		satellite := entitlement.Spec().Get("rhsm", "satellite")
		id := Identity{Block: blockID(object.Name()), Context: input.Context.Name, Object: object.Name()}
		request := Request{EntitlementRef: reference, Identity: id, Placement: placement, Version: Version,
			Satellite: Satellite{ContentBaseURL: satellite.Get("contentBaseURL").Text(), Hostname: satellite.Get("hostname").Text(), TrustBundleRef: satellite.Get("trustBundleRef").Text()}}
		data, err := reconciliation.Freeze(request, "provided registration")
		if err != nil {
			return plan, err
		}
		if _, err := decodeRequest(data); err != nil {
			return plan, err
		}
		description := "converge the Satellite registration of " + id.Object + " onto " + request.Satellite.Hostname
		impacts := []string{"point the subscription configuration of " + id.Object + " at " + request.Satellite.Hostname + " and regenerate its repositories"}
		if input.Verb == reconciliation.Destroy {
			description, impacts = retentionDescription(id.Object), nil
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID: id.Block, Description: description, Stage: reconciliation.StageMachines,
			Kind: string(api.Machine), Object: id.Object, Family: machine.ProvidedRegistrationFamily,
			Implementation: Implementation, ContentDigest: ContentDigest(), Request: data,
			Requires: []reconciliation.ObjectRef{{Kind: string(api.Machine), Object: id.Object, Family: machine.HardwareFamily}},
			Impacts:  impacts,
		})
		plan.Secrets = append(plan.Secrets, placement.SecretReferences()...)
		plan.Secrets = append(plan.Secrets, request.Satellite.TrustBundleRef)
	}
	plan.Secrets = lifecycle.SecretNames(plan.Secrets...)
	return plan, nil
}

func (Capability) Claimed(catalog api.Catalog) []string {
	var objects []string
	for _, object := range catalog.OfKind(api.Machine) {
		if machine.ProvidedSubscription(object) != "" {
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
	if err != nil {
		return nil, err
	}
	return append(request.Placement.SecretReferences(), request.Satellite.TrustBundleRef), nil
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := decodeBlock(block)
	if err != nil {
		return nil, err
	}
	return request.Placement.SecretClasses().Add(secrets.TrustAnchor, request.Satellite.TrustBundleRef), nil
}

func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	result, state, err := c.run(ctx, execution, "apply", 10*time.Minute)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	if state != reconciliation.EffectCompleted || !result.Outcome.Settled() {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, registrationUnproved.Fail("Machine/"+execution.Block.Object+" did not prove its registration", "")
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
		return lifecycle.RunResult{}, unknown, requestInvalid.Fail("provided registration requires its adapter and original apply identity", "")
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: Implementation, Operation: operation, Variable: "bootwright_provided_registration",
		Canonical: execution.Block.Request, Placement: request.Placement, Deadline: deadline,
		Materials: []lifecycle.MaterialFile{{Name: "satellite-ca", Secret: request.Satellite.TrustBundleRef, Part: secrets.CertificatePart, Variable: "satelliteCA"}},
		Refusals:  refusals(request),
	}))
	if err != nil {
		return result, unknown, err
	}
	evidence, err := reconciliation.DecodeEvidence[Evidence](result.Evidence, 4096, "provided registration")
	if err != nil || evidence.Version != EvidenceVersion || evidence.Request != execution.Block.RequestDigest {
		return result, unknown, nil
	}
	switch evidence.State {
	case "absent":
		if evidence.Consumer == "" && !evidence.Postcondition {
			return result, reconciliation.EffectNoEffect, nil
		}
	case "diverged":
		if evidence.Consumer != "" && !evidence.Postcondition {
			return result, reconciliation.EffectPartial, nil
		}
	case "converged", "unchanged":
		if evidence.Consumer != "" {
			if evidence.Postcondition {
				return result, reconciliation.EffectCompleted, nil
			}
			return result, reconciliation.EffectPartial, nil
		}
	}
	return result, unknown, nil
}

func refusals(request Request) map[string]error {
	name, hostname, bundle := "Machine/"+request.Identity.Object, request.Satellite.Hostname, "Secret/"+request.Satellite.TrustBundleRef
	refuse := func(message, remedy string) error { return registrationRefused.Fail(message, remedy) }
	return map[string]error{
		"registration-absent": refuse(name+" holds no consumer certificate, and the registration of a provided Machine is its owner's",
			"register "+name+" with the Satellite "+hostname+", under any of its names, and repeat apply"),
		"registration-foreign": refuse("the Satellite "+hostname+" does not know the consumer that "+name+" is registered as",
			"register "+name+" with the Satellite "+hostname+", or name the Entitlement of the Satellite it is registered with in spec.os.provided.subscription.entitlementRef on "+name+", and repeat apply"),
		"registration-trust": refuse(name+" reached the Satellite "+hostname+", which presented a certificate that the CA bundle of "+bundle+" does not verify",
			"set "+bundle+" to the CA that signs the certificate of "+hostname+", such as the Satellite's katello-server-ca.crt, and repeat apply"),
		"registration-unavailable": refuse(name+" could not prove its consumer to the Satellite "+hostname+" or run its subscription tooling",
			"restore the route from "+name+" to the Satellite "+hostname+" and its subscription tooling, or, when its record /etc/bootwright/provided-registration.json is unreadable, restore its settings by hand and remove it, and repeat apply"),
		"registration-content-root": refuse("the repositories of "+name+" are not all served beneath the content root "+request.Satellite.ContentBaseURL,
			"make the Satellite "+hostname+" serve the host's repositories beneath "+request.Satellite.ContentBaseURL+", or correct spec.rhsm.satellite.contentBaseURL on Entitlement/"+request.EntitlementRef+", and repeat apply"),
	}
}

func (Capability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := decodeBlock(block)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: retentionDescription(request.Identity.Object), Retains: []string{retained(request.Identity.Object)}}, nil
}

func retentionDescription(machine string) string {
	return "retain the Satellite registration of " + machine
}

func retained(machine string) string { return "the Satellite registration of " + machine }

func (Capability) Quiescent(ctx context.Context, _ lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "removal retains the Machine, its registration and its configuration"}, nil
}

func (c Capability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	observation, err := c.ObserveRemoval(ctx, execution)
	return lifecycle.Result{Outcome: observation.Outcome, Evidence: observation.Evidence}, err
}

func (Capability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Outcome: reconciliation.OutcomeCanceled}, err
	}
	if _, err := decodeBlock(execution.Block); err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Outcome: reconciliation.OutcomeFailed}, err
	}
	evidence, err := json.Marshal(Evidence{Version: EvidenceVersion, State: "retained", Postcondition: true, Request: execution.Block.RequestDigest})
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, err
}

func (Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := decodeBlock(block)
	if verb != reconciliation.Apply || err != nil || len(data) == 0 {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the Satellite registration of Machine "+block.Object, lifecycle.PlacedOn(request.Placement),
		registrationUnproved.Fail("Machine/"+block.Object+" did not prove its Satellite registration", "check the host's subscription configuration before repeating the operation"))
}
