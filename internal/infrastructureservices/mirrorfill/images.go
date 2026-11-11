package mirrorfill

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	imageBudget  = 20 * time.Minute
	SkopeoReason = "skopeo-missing"
)

func imagesDeadline(request ImagesRequest) time.Duration {
	return min(lifecycle.MaxDeadline, runOverhead+time.Duration(len(request.Images))*imageBudget)
}

type ImageFill struct {
	runner Runner
	images Images
}

func NewImages(runner Runner, images Images) ImageFill {
	return ImageFill{runner: runner, images: images}
}

func (c ImageFill) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, refusal("lifecycle.state", "mirror planning requires compiled desired state", "")
	}
	request, found, err := c.Request(input.State.Effective(), input.Controller, input.Context.Name)
	if err != nil || !found {
		return plan, err
	}
	canonical, err := request.Canonical()
	if err != nil {
		return plan, err
	}
	plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
		ID: ImagesBlockID(request.Identity.Environment), Description: imagesDescription(input.Verb, request), Stage: reconciliation.StageInfrastructureServices,
		Requires: []reconciliation.ObjectRef{{Kind: ImagesKind, Object: request.Identity.Registry, Family: managedservice.Family}},
		Impacts:  imagesImpacts(input.Verb, request), Consumes: []string{},
		Kind: ImagesKind, Object: request.Identity.Registry, Family: Family, Implementation: ImagesImplementation, ContentDigest: ImagesContentDigest(), Request: canonical,
	})
	plan.Secrets = lifecycle.SecretNames(request.SecretReferences()...)
	return plan, nil
}

func (c ImageFill) Request(catalog api.Catalog, controllerMachine, contextName string) (ImagesRequest, bool, error) {
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared || !mirror.Managed || mirror.Location == "" {
		return ImagesRequest{}, false, nil
	}
	if c.images == nil {
		return ImagesRequest{}, false, refusal("lifecycle.state", "mirror planning requires the acquisition list", "")
	}
	images, err := c.images(catalog)
	if err != nil || len(images) == 0 {
		return ImagesRequest{}, false, err
	}
	registry := mirror.Registry
	host, found := catalog.Find(api.Machine, registry.Spec().Get("machineRef").Text())
	if !found {
		return ImagesRequest{}, false, refusal("api.reference", "the image mirror's placement Machine is not in the selected graph", "declare it or correct machineRef on "+registry.Identity())
	}
	placement, err := machineref.PlacementFor(host, controllerMachine)
	if err != nil {
		return ImagesRequest{}, false, err
	}
	egress, err := managedservice.EgressFor(catalog, host)
	if err != nil {
		return ImagesRequest{}, false, err
	}
	source := managedservice.ImageSource{Host: mirror.Location, CredentialsRef: mirror.CredentialsRef, TrustBundleRef: mirror.TrustBundleRef}
	if declaration, found := catalog.Find(api.Secret, source.CredentialsRef); found {
		source.CredentialsType = declaration.Spec().Get("type").Text()
	}
	request := ImagesRequest{
		Egress: egress, Identity: ImagesIdentity{Context: contextName, Environment: catalog.OfKind(api.Environment)[0].Name(), Registry: registry.Name()},
		Images: slices.Clone(images), Mirror: source, Placement: placement, Version: ImagesImplementation,
	}
	slices.SortFunc(request.Images, func(x, y Image) int { return strings.Compare(x.Source, y.Source) })
	request.Images = slices.Compact(request.Images)
	if err := request.validate(); err != nil {
		return ImagesRequest{}, false, err
	}
	return request, true, nil
}

func imagesDescription(verb reconciliation.Verb, request ImagesRequest) string {
	if verb == reconciliation.Destroy {
		return "release the image mirror of " + request.Identity.Environment + " on " + request.Placement.Machine + ", whose Registry's removal deletes its images"
	}
	return "fill the image mirror of " + request.Identity.Environment + " on " + request.Placement.Machine
}

func imagesImpacts(verb reconciliation.Verb, request ImagesRequest) []string {
	if verb == reconciliation.Destroy {
		return []string{}
	}
	impacts := make([]string, 0, len(request.Images))
	for _, image := range request.Images {
		impacts = append(impacts, "publish-content "+request.Mirror.Host+"/"+image.Repository+"@"+image.Digest)
	}
	return impacts
}

func (ImageFill) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := DecodeImagesRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: imagesDescription(reconciliation.Destroy, request), Impacts: imagesImpacts(reconciliation.Destroy, request), Consumes: []string{}}, nil
}

func (c ImageFill) run(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.RunResult, ImagesRequest, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, ImagesRequest{}, err
	}
	request, err := DecodeImagesRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	if execution.Block.Kind != ImagesKind || execution.Block.Object != request.Identity.Registry {
		return lifecycle.RunResult{}, request, refusal("lifecycle.state", "the mirror images request does not match its frozen block identity", "")
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, request, refusal("lifecycle.state", "the mirror images adapter is not configured", "")
	}
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	var refusals map[string]error
	var materials []lifecycle.MaterialFile
	if operation != "destroy" {
		materials = request.Mirror.Materials()
	}
	if operation == "apply" {
		refusals = imagesRefusals(execution.Context, request)
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: ImagesImplementation,
		Operation: operation, Variable: "bootwright_mirror_images", Canonical: canonical, Placement: request.Placement,
		Materials: materials, Refusals: refusals, Deadline: imagesDeadline(request),
	}))
	return result, request, err
}

func imagesRefusals(contextName string, request ImagesRequest) map[string]error {
	registry := diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: ImagesKind, Name: request.Identity.Registry}
	refused := func(code, message, remediation string) error {
		return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: code, Message: message, Remediation: remediation, Object: &registry}}}
	}
	machine := "Machine/" + request.Placement.Machine
	return map[string]error{
		SkopeoReason: refused("lifecycle.state", machine+" has no working /usr/bin/skopeo, so the image mirror of "+request.Identity.Environment+" cannot copy its images; this attempt copied nothing",
			"install the skopeo package on "+machine+" with its package manager and repeat the apply, or place Registry/"+request.Identity.Registry+" on a Machine that has it"),
		"image-digest-mismatch": refused("lifecycle.state", "the image mirror on "+request.Placement.Machine+" reports a digest other than the pinned one for an image it was given, so the fill stopped",
			"verify the Registry and the route from "+request.Placement.Machine+" to the image's origin, then run bootwright apply --context "+contextName),
		"image-unreachable": refused("lifecycle.unknown", machine+" could not copy an image from its origin into the image mirror",
			"give "+request.Placement.Machine+" a route to the origins bootwright render acquisitions --context "+contextName+" names, then run bootwright apply --context "+contextName),
		"mirror-unreadable": refused("lifecycle.unknown", machine+" could not read the image mirror at "+request.Mirror.Host+" with its credential and trust anchor",
			"verify Registry/"+request.Identity.Registry+" is ready and answers its credential, then run bootwright apply --context "+contextName),
	}
}

func (c ImageFill) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c ImageFill) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

func (c ImageFill) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	result, request, err := c.run(ctx, execution, operation)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome := map[string]reconciliation.Outcome{"changed": reconciliation.OutcomeChanged, "unchanged": reconciliation.OutcomeUnchanged}[result.Outcome]
	evidence, err := decodeImagesEvidence(result.Evidence, execution.Block.RequestDigest)
	if outcome == "" || err != nil || !evidence.Postcondition || imagesEffect(evidence, request, operation == "destroy") != reconciliation.EffectCompleted {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, refusal("lifecycle.state", "the mirror images adapter did not prove its postcondition", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func (c ImageFill) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	observation := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	result, request, err := c.run(ctx, execution, "observe")
	if err != nil {
		return observation, err
	}
	observation.Evidence = result.Evidence
	evidence, err := decodeImagesEvidence(result.Evidence, execution.Block.RequestDigest)
	if err != nil {
		return observation, nil
	}
	observation.Effect = imagesEffect(evidence, request, false)
	return observation, nil
}

func (ImageFill) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	if _, err := DecodeImagesRequest(execution.Block.Request); err != nil {
		return unknown, err
	}
	evidence, err := json.Marshal(ImagesEvidence{Absent: true, Images: []ImageEvidence{}, Postcondition: true, Request: execution.Block.RequestDigest, Version: ImagesEvidenceVersion})
	if err != nil {
		return unknown, refusal("lifecycle.state", "the mirror images removal evidence cannot be encoded", "")
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func (ImageFill) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "every consumer of the image mirror precedes its fill in removal"}, nil
}

func (ImageFill) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeImagesRequest(block.Request)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	evidence, err := decodeImagesEvidence(data, block.RequestDigest)
	if err != nil || imagesEffect(evidence, request, verb == reconciliation.Destroy) != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the image mirror of "+request.Identity.Environment, lifecycle.PlacedOn(request.Placement), imagesDrift(evidence, request))
}

func (ImageFill) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeImagesRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return lifecycle.SecretNames(request.SecretReferences()...), nil
}

func (ImageFill) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeImagesRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretClasses(), nil
}

func (ImageFill) Claimed(catalog api.Catalog) []string {
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared || !mirror.Managed {
		return nil
	}
	return []string{mirror.Registry.Identity()}
}
