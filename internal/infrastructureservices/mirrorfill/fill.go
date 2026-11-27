package mirrorfill

import (
	"context"
	"encoding/json"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type fillCapability interface {
	Plan(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error)
	Removal(context.Context, reconciliation.Block) (lifecycle.Removal, error)
	Apply(context.Context, lifecycle.Execution) (lifecycle.Result, error)
	Destroy(context.Context, lifecycle.Execution) (lifecycle.Result, error)
	Observe(context.Context, lifecycle.Execution) (lifecycle.Observation, error)
	ObserveRemoval(context.Context, lifecycle.Execution) (lifecycle.Observation, error)
	Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error)
	Unresolved(reconciliation.Verb, reconciliation.Block, json.RawMessage) (lifecycle.Unresolved, bool)
	SecretReferences(reconciliation.Block) ([]string, error)
	SecretClasses(reconciliation.Block) (secrets.Classes, error)
	ProveBinding(context.Context, string, reconciliation.Block, map[string]secrets.Material) error
	Claimed(api.Catalog) []string
	withHostKeys(lifecycle.InstalledHostKey) fillCapability
}

type fill[R, E any] struct {
	runner   Runner
	hostKeys lifecycle.InstalledHostKey

	noun           string
	kind           string
	implementation string
	variable       string
	quiescent      string

	requests    func(catalog api.Catalog, controllerMachine, contextName string) ([]R, error)
	decode      func([]byte) (R, error)
	canonical   func(R) ([]byte, error)
	object      func(R) string
	placement   func(R) machineref.Placement
	definition  func(verb reconciliation.Verb, request R, canonical []byte) reconciliation.BlockDefinition
	describe    func(verb reconciliation.Verb, request R) string
	impacts     func(verb reconciliation.Verb, request R) []string
	nothing     func(R) string
	references  func(R) []string
	classes     func(R) secrets.Classes
	deadline    func(R) time.Duration
	materials   func(R) []lifecycle.MaterialFile
	publicFiles func(context.Context, R) (map[string]string, error)
	refusals    func(contextName string, request R) map[string]error
	lend        func(execution lifecycle.Execution, request R) (lifecycle.Execution, []lifecycle.MaterialFile, func(), error)
	decodeProof func(data []byte, digest string) (E, error)
	proven      func(E) bool
	effect      func(evidence E, request R, removal bool) reconciliation.EffectState
	drift       func(evidence E, request R) error
	subject     func(R) string
	removed     func(digest string) any
	claimed     func(api.Catalog) []string
}

func (c fill[R, E]) withHostKeys(hostKeys lifecycle.InstalledHostKey) fillCapability {
	c.hostKeys = hostKeys
	return c
}

func (c fill[R, E]) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, mirrorPlanningInputMissing.Fail("mirror planning requires compiled desired state", "")
	}
	requests, err := c.requests(input.State.Effective(), input.Controller, input.Context.Name)
	if err != nil {
		return plan, err
	}
	var references []string
	for _, request := range requests {
		canonical, err := c.canonical(request)
		if err != nil {
			return plan, err
		}
		plan.Definitions = append(plan.Definitions, c.definition(input.Verb, request, canonical))
		references = append(references, c.references(request)...)
	}
	plan.Secrets = lifecycle.SecretNames(references...)
	return plan, nil
}

func (c fill[R, E]) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := c.decode(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	removal := lifecycle.Removal{Description: c.describe(reconciliation.Destroy, request), Impacts: c.impacts(reconciliation.Destroy, request), Consumes: []string{}}
	if c.nothing != nil {
		removal.Nothing = c.nothing(request)
	}
	return removal, nil
}

func (c fill[R, E]) run(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.RunResult, R, error) {
	var none R
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, none, err
	}
	request, err := c.decode(execution.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	if execution.Block.Kind != c.kind || execution.Block.Object != c.object(request) {
		return lifecycle.RunResult{}, request, mirrorRequestInvalid.Fail("the "+c.noun+" request does not match its frozen block identity", "")
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, request, mirrorUnconfigured.Fail("the "+c.noun+" adapter is not configured", "")
	}
	canonical, err := c.canonical(request)
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	var refusals map[string]error
	var materials []lifecycle.MaterialFile
	var publicFiles map[string]string
	if operation != "destroy" && c.materials != nil {
		materials = c.materials(request)
	}
	if operation == "apply" {
		refusals = c.refusals(execution.Context, request)
		if c.publicFiles != nil {
			publicFiles, err = c.publicFiles(ctx, request)
			if err != nil {
				return lifecycle.RunResult{}, request, err
			}
		}
		if c.lend != nil {
			lent, extra, release, err := c.lend(execution, request)
			if err != nil {
				return lifecycle.RunResult{}, request, err
			}
			defer release()
			execution, materials = lent, append(materials, extra...)
		}
	}
	execution, placement, err := lifecycle.PinInstalledHost(execution, c.placement(request), c.hostKeys)
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: c.implementation,
		Operation: operation, Variable: c.variable, Canonical: canonical, Placement: placement,
		Materials: materials, PublicFiles: publicFiles, Refusals: refusals, Deadline: c.deadline(request),
	}))
	return result, request, err
}

func (c fill[R, E]) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c fill[R, E]) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

func (c fill[R, E]) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	result, request, err := c.run(ctx, execution, operation)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome, usable := result.Outcome, result.Outcome.Settled()
	evidence, err := c.decodeProof(result.Evidence, execution.Block.RequestDigest)
	if !usable || err != nil || !c.proven(evidence) || c.effect(evidence, request, operation == "destroy") != reconciliation.EffectCompleted {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, mirrorAdapterUnproved.Fail("the "+c.noun+" adapter did not prove its postcondition", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func (c fill[R, E]) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, false)
}

func (c fill[R, E]) observe(ctx context.Context, execution lifecycle.Execution, removal bool) (lifecycle.Observation, error) {
	observation := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	result, request, err := c.run(ctx, execution, "observe")
	if err != nil {
		return observation, err
	}
	observation.Evidence = result.Evidence
	evidence, err := c.decodeProof(result.Evidence, execution.Block.RequestDigest)
	if err != nil {
		return observation, nil
	}
	observation.Effect = c.effect(evidence, request, removal)
	return observation, nil
}

func (c fill[R, E]) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	if c.removed == nil {
		return c.observe(ctx, execution, true)
	}
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	if _, err := c.decode(execution.Block.Request); err != nil {
		return unknown, err
	}
	evidence, err := json.Marshal(c.removed(execution.Block.RequestDigest))
	if err != nil {
		return unknown, mirrorEvidenceUnencodable.Fail("the "+c.noun+" removal evidence cannot be encoded", "")
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func (c fill[R, E]) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: c.quiescent}, nil
}

func (c fill[R, E]) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := c.decode(block.Request)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	evidence, err := c.decodeProof(data, block.RequestDigest)
	if err != nil || c.effect(evidence, request, verb == reconciliation.Destroy) != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, c.subject(request), lifecycle.PlacedOn(c.placement(request)), c.drift(evidence, request))
}

func (c fill[R, E]) ProveBinding(_ context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	request, err := c.decode(block.Request)
	if err != nil {
		return err
	}
	return c.placement(request).ProveHostPin(contextName, material)
}

func (c fill[R, E]) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := c.decode(block.Request)
	if err != nil {
		return nil, err
	}
	return lifecycle.SecretNames(c.references(request)...), nil
}

func (c fill[R, E]) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := c.decode(block.Request)
	if err != nil {
		return nil, err
	}
	return c.classes(request), nil
}

func (c fill[R, E]) Claimed(catalog api.Catalog) []string { return c.claimed(catalog) }

type mirrorHost struct {
	placement machineref.Placement
	egress    managedservice.Egress
	registry  api.Object
	source    managedservice.ImageSource
}

func imageMirrorHost(catalog api.Catalog, mirror infrastructureservices.ImageMirror, controllerMachine string) (mirrorHost, error) {
	registry := mirror.Registry
	host, found := catalog.Find(api.Machine, registry.Spec().Get("machineRef").Text())
	if !found {
		return mirrorHost{}, refusal("api.reference", "the image mirror's placement Machine is not in the selected graph", "declare it or correct machineRef on "+registry.Identity())
	}
	placement, err := machineref.ServicePlacementFor(host, controllerMachine)
	if err != nil {
		return mirrorHost{}, err
	}
	egress, err := managedservice.EgressFor(catalog, host)
	if err != nil {
		return mirrorHost{}, err
	}
	return mirrorHost{placement: placement, egress: egress, registry: registry, source: managedservice.MirrorSource(catalog, mirror)}, nil
}
