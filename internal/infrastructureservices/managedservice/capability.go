package managedservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Definition is everything that distinguishes one managed network service from
// another. The behavior around it — selection, freezing, evidence and the
// inverse — is identical, so it is implemented once.
type Definition struct {
	Kind           api.Kind
	Implementation string
	Version        string
	Slug           string
	Variable       string
	Purpose        string
	Image          string
	// Extend adds the kind's own frozen intent to a request whose shared
	// fields are already derived.
	Extend func(catalog api.Catalog, spec api.Value, request *Request) error
}

func (d Definition) BlockID(service string) string { return d.Slug + "-" + service }

// ContentDigest binds the plan to the exact behavior this build implements, so
// changing the request shape, the compiled image or the host layout
// invalidates a frozen plan.
func (d Definition) ContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"bootwright.infrastructureservices.managed-service-v1",
		d.Implementation, d.Version, d.Image, ContentRootPrefix, UnitPrefix,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

// Capability realizes one managed network service kind. It owns what
// completion, readiness and absence mean for that kind and nothing else.
type Capability struct {
	definition Definition
	runner     Runner
}

func NewCapability(definition Definition, runner Runner) Capability {
	return Capability{definition: definition, runner: runner}
}

func (c Capability) Kind() string { return string(c.definition.Kind) }

// Plan derives one block per managed service of this kind. It reads no host,
// endpoint or Secret material and performs no effect.
func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, Refusal("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	requests, err := c.Requests(input.State.Effective(), input.Controller, input.Context.ID)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	digest := c.definition.ContentDigest()
	for _, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID:             request.Identity.Block,
			Description:    c.definition.Purpose + " for " + request.Identity.Service + " on " + request.Placement.Machine,
			Stage:          reconciliation.StageInfraComponents,
			Impacts:        impacts(request),
			Groups:         groups(input.Verb, request),
			Kind:           string(c.definition.Kind),
			Object:         request.Identity.Service,
			Implementation: c.definition.Implementation,
			ContentDigest:  digest,
			Request:        canonical,
		})
		for _, reference := range request.Placement.SecretReferences() {
			if !slices.Contains(plan.Secrets, reference) {
				plan.Secrets = append(plan.Secrets, reference)
			}
		}
		if !request.Placement.Local() {
			continue
		}
		plan.Reservations = append(plan.Reservations, prerequisites.HostReservation{
			ContextID: input.Context.ID, Kind: c.definition.Slug,
			Service: request.Identity.Service, Keys: request.ReservationKeys(),
		})
	}
	slices.Sort(plan.Secrets)
	return plan, nil
}

// Requests derives one frozen request per managed service of this kind, in
// canonical object order. It reads no host, endpoint or Secret material.
func (c Capability) Requests(catalog api.Catalog, controllerMachine, contextID string) ([]Request, error) {
	if !ValidContextID(contextID) {
		return nil, Refusal("lifecycle.state", "the lifecycle context identity is invalid", "")
	}
	var requests []Request
	for _, object := range ManagedObjects(catalog, c.definition.Kind) {
		request, err := c.requestFor(catalog, object, controllerMachine, contextID)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func (c Capability) requestFor(catalog api.Catalog, object api.Object, controllerMachine, contextID string) (Request, error) {
	name := object.Name()
	if !SafeSegment(name) {
		return Request{}, Refusal("lifecycle.state", "the managed service name is not a safe host identifier", "rename "+object.Identity())
	}
	spec := object.Spec()
	machine, found := catalog.Find(api.Machine, spec.Get("machineRef").Text())
	if !found {
		return Request{}, Refusal("api.reference", "the managed service's placement Machine is not in the selected graph", "declare "+spec.Get("machineRef").Text()+" or change the reference")
	}
	placement, err := PlacementFor(machine, controllerMachine)
	if err != nil {
		return Request{}, err
	}
	image, err := ImageFor(spec, object.Identity(), c.definition.Image)
	if err != nil {
		return Request{}, err
	}
	egress, err := EgressFor(catalog, machine)
	if err != nil {
		return Request{}, err
	}
	endpoints, err := EndpointsFor(spec, machine)
	if err != nil {
		return Request{}, err
	}
	bind, err := BindAddress(spec, object.Identity())
	if err != nil {
		return Request{}, err
	}
	port, err := Port(spec, object.Identity())
	if err != nil {
		return Request{}, err
	}
	request := Request{
		BindAddress: bind,
		ContentRoot: ContentRoot(contextID, c.definition.Slug, name),
		Egress:      egress,
		Endpoints:   endpoints,
		Identity:    Identity{Block: c.definition.BlockID(name), Context: contextID, Service: name},
		Image:       image,
		Kind:        string(c.definition.Kind),
		Placement:   placement,
		Port:        port,
		Unit:        UnitName(contextID, c.definition.Slug, name),
		Version:     c.definition.Version,
	}
	if c.definition.Extend != nil {
		if err := c.definition.Extend(catalog, spec, &request); err != nil {
			return Request{}, err
		}
	}
	return request, nil
}

func impacts(request Request) []string {
	impacts := []string{"create-container-unit " + request.Unit, "create-path " + request.ContentRoot}
	for _, address := range request.ProbeTargets() {
		impacts = append(impacts, "open-listener "+address+":"+FormatPort(request.Port))
	}
	slices.Sort(impacts)
	return slices.Compact(impacts)
}

func groups(verb reconciliation.Verb, request Request) []reconciliation.Group {
	machines := []string{request.Placement.Machine}
	steps := [][2]string{
		{"pull-image", "acquire the pinned service image"},
		{"publish-configuration", "publish the service configuration and unit"},
		{"start-service", "start the managed service"},
		{"verify-readiness", "verify the service answers"},
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{
			{"stop-service", "stop the managed service"},
			{"remove-configuration", "remove the configuration, unit and content root"},
			{"verify-absence", "verify every owned resource is gone"},
		}
	}
	out := make([]reconciliation.Group, 0, len(steps))
	for _, step := range steps {
		out = append(out, reconciliation.Group{ID: step[0], Description: step[1], Machines: machines})
	}
	return out
}

func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c Capability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

func (c Capability) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, execution, operation, request)
	if err != nil {
		return unknown, err
	}
	if result.Outcome != "changed" && result.Outcome != "unchanged" {
		return unknown, Refusal("lifecycle.state", "the managed service adapter reported no usable outcome", "")
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
	outcome := reconciliation.OutcomeChanged
	if result.Outcome == "unchanged" {
		outcome = reconciliation.OutcomeUnchanged
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

// Observe is read-only. Live state matching the frozen request in full is
// positive completion; nothing present is positive no effect; anything else,
// including a partial observation, stays unknown.
func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, err := c.prepare(ctx, execution)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, execution, "observe", request)
	if err != nil {
		return unknown, nil
	}
	digest := execution.Block.RequestDigest
	if ValidatePresence(result.Evidence, request, digest) == nil {
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: result.Evidence}, nil
	}
	if ValidateAbsence(result.Evidence, digest) == nil {
		return lifecycle.Observation{Effect: reconciliation.EffectNoEffect, Evidence: result.Evidence}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: result.Evidence}, nil
}

func (c Capability) prepare(ctx context.Context, execution lifecycle.Execution) (Request, error) {
	if err := ctx.Err(); err != nil {
		return Request{}, err
	}
	if c.runner == nil {
		return Request{}, Refusal("lifecycle.state", "the managed service adapter is not configured", "")
	}
	return DecodeRequest(execution.Block.Request, c.definition.Version)
}

func (c Capability) run(ctx context.Context, execution lifecycle.Execution, operation string, request Request) (RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return RunResult{}, err
	}
	return c.runner.Run(ctx, RunRequest{
		Kind:      string(c.definition.Kind),
		Operation: operation,
		Variable:  c.definition.Variable,
		Digest:    execution.Block.RequestDigest,
		Canonical: canonical,
		Placement: request.Placement,
		Materials: Materials(request.Placement),
		Sudo:      request.Placement.SudoPasswordRef,
		Launch:    execution.Launch,
		Bundle:    execution.Bundle,
		Area:      execution.Area,
		Material:  execution.Material,
		Log:       execution.Log,
		Progress:  execution.Progress,
	})
}

// Unsupported names every managed service of this kind the capability cannot
// realize. Every declared shape is realizable today, so the list is empty.
func (Capability) Unsupported(*compilation.State) []string { return nil }
