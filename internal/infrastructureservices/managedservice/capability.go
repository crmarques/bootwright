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
	// Purpose is what an apply makes this service do; Subject is what a
	// removal takes away. A plan needs both, because a destroy that borrows
	// the apply's wording reads as though it were installing.
	Purpose string
	Subject string
	Image   string
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
	requests, err := c.Requests(input.State.Effective(), input.Controller, input.Context.Name)
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
			Description:    c.definition.describe(input.Verb, request),
			Stage:          reconciliation.StageInfraComponents,
			Impacts:        impacts(input.Verb, request),
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
			Context: input.Context.Name, Kind: c.definition.Slug,
			Service: request.Identity.Service, Keys: request.ReservationKeys(),
		})
	}
	slices.Sort(plan.Secrets)
	return plan, nil
}

// Requests derives one frozen request per managed service of this kind, in
// canonical object order. It reads no host, endpoint or Secret material.
func (c Capability) Requests(catalog api.Catalog, controllerMachine, contextName string) ([]Request, error) {
	if !ValidContextName(contextName) {
		return nil, Refusal("lifecycle.state", "the lifecycle context identity is invalid", "")
	}
	var requests []Request
	for _, object := range ManagedObjects(catalog, c.definition.Kind) {
		request, err := c.requestFor(catalog, object, controllerMachine, contextName)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func (c Capability) requestFor(catalog api.Catalog, object api.Object, controllerMachine, contextName string) (Request, error) {
	name := object.Name()
	if !SafeSegment(name) {
		return Request{}, Refusal("lifecycle.state", "the managed service name is not a safe host identifier", "rename "+object.Identity())
	}
	spec := object.Spec()
	machine, found := catalog.Find(api.Machine, spec.Get("machineRef").Text())
	if !found {
		return Request{}, Refusal("api.reference", "the managed service's placement Machine is not in the selected graph", "declare "+spec.Get("machineRef").Text()+" or change the reference")
	}
	placement, err := lifecycle.PlacementFor(machine, controllerMachine)
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
		ContentRoot: ContentRoot(contextName, c.definition.Slug, name),
		Egress:      egress,
		Endpoints:   endpoints,
		Identity:    Identity{Block: c.definition.BlockID(name), Context: contextName, Service: name},
		Image:       image,
		Kind:        string(c.definition.Kind),
		Placement:   placement,
		Port:        port,
		Unit:        UnitName(contextName, c.definition.Slug, name),
		Version:     c.definition.Version,
	}
	if c.definition.Extend != nil {
		if err := c.definition.Extend(catalog, spec, &request); err != nil {
			return Request{}, err
		}
	}
	return request, nil
}

func (d Definition) describe(verb reconciliation.Verb, request Request) string {
	if verb == reconciliation.Destroy {
		return "remove the " + d.Subject + " " + request.Identity.Service + " from " + request.Placement.Machine
	}
	return d.Purpose + " for " + request.Identity.Service + " on " + request.Placement.Machine
}

func impacts(verb reconciliation.Verb, request Request) []string {
	unit, root, listener := "create-container-unit", "create-path", "open-listener"
	if verb == reconciliation.Destroy {
		unit, root, listener = "remove-container-unit", "remove-path", "close-listener"
	}
	impacts := []string{unit + " " + request.Unit, root + " " + request.ContentRoot}
	for _, address := range request.ProbeTargets() {
		impacts = append(impacts, listener+" "+address+":"+FormatPort(request.Port))
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
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
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
// positive completion; nothing present is positive no effect; this context's
// own service part way realized is positive partial; anything else, including
// an observation that could not be made, stays unknown.
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
	if ValidatePartial(result.Evidence, digest) == nil {
		return lifecycle.Observation{Effect: reconciliation.EffectPartial, Evidence: result.Evidence}, nil
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

func (c Capability) run(ctx context.Context, execution lifecycle.Execution, operation string, request Request) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return c.runner.Run(ctx, lifecycle.RunRequest{
		Implementation: c.definition.Implementation,
		Operation:      operation,
		Variable:       c.definition.Variable,
		Digest:         execution.Block.RequestDigest,
		Canonical:      canonical,
		Placement:      request.Placement,
		Materials:      lifecycle.Materials(request.Placement),
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

// Unsupported names every managed service of this kind the capability cannot
// realize. Every declared shape is realizable today, so the list is empty.
func (Capability) Unsupported(*compilation.State) []string { return nil }
