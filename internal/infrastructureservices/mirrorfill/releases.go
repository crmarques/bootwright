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

const releaseBudget = 4 * time.Hour

func releaseDeadline() time.Duration { return min(lifecycle.MaxDeadline, runOverhead+releaseBudget) }

type ReleaseFill struct {
	runner   Runner
	releases Releases
	auth     ReleaseAuth
}

func NewReleases(runner Runner, releases Releases, auth ReleaseAuth) ReleaseFill {
	return ReleaseFill{runner: runner, releases: releases, auth: auth}
}

func (c ReleaseFill) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, refusal("lifecycle.state", "mirror planning requires compiled desired state", "")
	}
	requests, err := c.Requests(input.State.Effective(), input.Controller, input.Context.Name)
	if err != nil {
		return plan, err
	}
	var references []string
	for _, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return plan, err
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID: ReleaseBlockID(request.Identity.Cluster), Description: releaseDescription(input.Verb, request), Stage: reconciliation.StageInfrastructureServices,
			Requires:  []reconciliation.ObjectRef{{Kind: ImagesKind, Object: request.Identity.Registry, Family: managedservice.Family}},
			Exclusive: []string{"registry-push:" + request.Identity.Registry}, Impacts: releaseImpacts(input.Verb, request), Consumes: []string{},
			Kind: ReleaseKind, Object: request.Identity.Cluster, Family: Family, Implementation: ReleaseImplementation, ContentDigest: ReleaseContentDigest(), Request: canonical,
		})
		references = append(references, request.SecretReferences()...)
	}
	plan.Secrets = lifecycle.SecretNames(references...)
	return plan, nil
}

func (c ReleaseFill) Requests(catalog api.Catalog, controllerMachine, contextName string) ([]ReleaseRequest, error) {
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared || !mirror.Managed || mirror.Location == "" {
		return nil, nil
	}
	if c.releases == nil {
		return nil, refusal("lifecycle.state", "mirror planning requires the cluster releases", "")
	}
	releases, err := c.releases(catalog)
	if err != nil || len(releases) == 0 {
		return nil, err
	}
	registry := mirror.Registry
	host, found := catalog.Find(api.Machine, registry.Spec().Get("machineRef").Text())
	if !found {
		return nil, refusal("api.reference", "the image mirror's placement Machine is not in the selected graph", "declare it or correct machineRef on "+registry.Identity())
	}
	placement, err := machineref.PlacementFor(host, controllerMachine)
	if err != nil {
		return nil, err
	}
	egress, err := managedservice.EgressFor(catalog, host)
	if err != nil {
		return nil, err
	}
	source := managedservice.ImageSource{Host: mirror.Location, CredentialsRef: mirror.CredentialsRef, TrustBundleRef: mirror.TrustBundleRef}
	if declaration, found := catalog.Find(api.Secret, source.CredentialsRef); found {
		source.CredentialsType = declaration.Spec().Get("type").Text()
	}
	environment := catalog.OfKind(api.Environment)[0].Name()
	requests := make([]ReleaseRequest, 0, len(releases))
	for _, release := range releases {
		if err := release.Refusal(); err != nil {
			return nil, err
		}
		request := ReleaseRequest{
			Client: release.Client, Egress: egress, Mirror: source, Placement: placement, PullSecretRef: release.PullSecretRef, Version: ReleaseImplementation,
			Identity: ReleaseIdentity{Cluster: release.Cluster, Context: contextName, Environment: environment, Registry: registry.Name()},
			Release: ReleaseImage{Components: infrastructureservices.OpenShiftComponentRepository, Digest: release.Digest,
				Repository: infrastructureservices.OpenShiftReleaseRepository, Tag: ReleaseTag(release.Version), Version: release.Version},
		}
		if err := request.validate(); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	slices.SortFunc(requests, func(x, y ReleaseRequest) int { return strings.Compare(x.Identity.Cluster, y.Identity.Cluster) })
	return requests, nil
}

func releaseDescription(verb reconciliation.Verb, request ReleaseRequest) string {
	if verb == reconciliation.Destroy {
		return "release the copy of " + request.Identity.Cluster + "'s release in the image mirror on " + request.Placement.Machine + ", whose Registry's removal deletes it"
	}
	return "copy the release " + request.Release.Version + " of " + request.Identity.Cluster + " into the image mirror on " + request.Placement.Machine
}

func releaseImpacts(verb reconciliation.Verb, request ReleaseRequest) []string {
	if verb == reconciliation.Destroy {
		return []string{}
	}
	return []string{"publish-content " + request.Mirror.Host + "/" + request.Release.Components, "publish-content " + request.Mirror.Host + "/" + request.Release.Repository + ":" + request.Release.Tag}
}

func (ReleaseFill) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := DecodeReleaseRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: releaseDescription(reconciliation.Destroy, request), Impacts: releaseImpacts(reconciliation.Destroy, request), Consumes: []string{}}, nil
}

func (c ReleaseFill) run(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.RunResult, ReleaseRequest, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, ReleaseRequest{}, err
	}
	request, err := DecodeReleaseRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	if execution.Block.Kind != ReleaseKind || execution.Block.Object != request.Identity.Cluster {
		return lifecycle.RunResult{}, request, refusal("lifecycle.state", "the mirror release request does not match its frozen block identity", "")
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, request, refusal("lifecycle.state", "the mirror release adapter is not configured", "")
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
		refusals = releaseRefusals(execution.Context, request)
		lent, err := c.lendReleaseAuth(execution, request)
		if err != nil {
			return lifecycle.RunResult{}, request, err
		}
		defer lent.Material[releaseAuthMaterial].Clear()
		execution = lent
		materials = append(materials, lifecycle.MaterialFile{Name: "release-auth", Part: secrets.ValuePart, Secret: releaseAuthMaterial, Variable: "releaseAuth"})
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: ReleaseImplementation,
		Operation: operation, Variable: "bootwright_mirror_release", Canonical: canonical, Placement: request.Placement,
		Materials: materials, Refusals: refusals, Deadline: releaseDeadline(),
	}))
	return result, request, err
}

func releaseRefusals(contextName string, request ReleaseRequest) map[string]error {
	cluster := diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: ReleaseKind, Name: request.Identity.Cluster}
	refused := func(code, message, remediation string) error {
		return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: code, Message: message, Remediation: remediation, Object: &cluster}}}
	}
	machine, repeat := "Machine/"+request.Placement.Machine, "bootwright apply --context "+contextName
	return map[string]error{
		"client-unreachable": refused("lifecycle.unknown", machine+" could not read the release's oc or its published checksums from "+request.Client.URL+", so the release was not copied",
			"give "+request.Placement.Machine+" a route to the origins bootwright render acquisitions --context "+contextName+" names, then run "+repeat),
		"client-digest-mismatch": refused("lifecycle.state", "the oc archive "+machine+" read from "+request.Client.URL+" does not match the SHA-256 its publisher lists, so it was not run",
			"verify the publisher and the route from "+request.Placement.Machine+" to it, then run "+repeat),
		"release-unreachable": refused("lifecycle.unknown", machine+" could not copy the release "+request.Release.Version+" from "+request.Release.Repository+" into the image mirror at "+request.Mirror.Host,
			"verify that Secret/"+request.PullSecretRef+" holds a quay.io entry that reads the release, that "+request.Placement.Machine+" reaches quay.io and Registry/"+request.Identity.Registry+" answers its credential, then run "+repeat),
		"release-incomplete": refused("lifecycle.state", "the image mirror at "+request.Mirror.Host+" lacks the release "+request.Release.Version+" or one of its components after the copy, so the cluster would not install from it",
			"verify the free space of Registry/"+request.Identity.Registry+"'s storage on "+request.Placement.Machine+", then run "+repeat),
		"release-digest-mismatch": refused("lifecycle.state", "the image mirror at "+request.Mirror.Host+" holds the release "+request.Release.Tag+" at another digest than the pinned one, so the fill stopped",
			"verify spec.distribution.release.image on OpenShiftCluster/"+request.Identity.Cluster+" names the release "+request.Release.Version+", then run "+repeat),
		"mirror-unreadable": refused("lifecycle.unknown", machine+" could not read the image mirror at "+request.Mirror.Host+" with its credential and trust anchor",
			"verify Registry/"+request.Identity.Registry+" is ready and answers its credential, then run "+repeat),
	}
}

func (c ReleaseFill) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c ReleaseFill) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

func (c ReleaseFill) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	result, request, err := c.run(ctx, execution, operation)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome := map[string]reconciliation.Outcome{"changed": reconciliation.OutcomeChanged, "unchanged": reconciliation.OutcomeUnchanged}[result.Outcome]
	evidence, err := decodeReleaseEvidence(result.Evidence, execution.Block.RequestDigest)
	if outcome == "" || err != nil || !evidence.Postcondition || releaseEffect(evidence, request, operation == "destroy") != reconciliation.EffectCompleted {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, refusal("lifecycle.state", "the mirror release adapter did not prove its postcondition", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func (c ReleaseFill) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	observation := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	result, request, err := c.run(ctx, execution, "observe")
	if err != nil {
		return observation, err
	}
	observation.Evidence = result.Evidence
	evidence, err := decodeReleaseEvidence(result.Evidence, execution.Block.RequestDigest)
	if err != nil {
		return observation, nil
	}
	observation.Effect = releaseEffect(evidence, request, false)
	return observation, nil
}

func (ReleaseFill) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	if _, err := DecodeReleaseRequest(execution.Block.Request); err != nil {
		return unknown, err
	}
	evidence, err := json.Marshal(ReleaseEvidence{Absent: true, Components: []ComponentEvidence{}, Postcondition: true, Request: execution.Block.RequestDigest, Version: ReleaseEvidenceVersion})
	if err != nil {
		return unknown, refusal("lifecycle.state", "the mirror release removal evidence cannot be encoded", "")
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func (ReleaseFill) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "the cluster that installs from the copied release precedes it in removal"}, nil
}

func (ReleaseFill) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeReleaseRequest(block.Request)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	evidence, err := decodeReleaseEvidence(data, block.RequestDigest)
	if err != nil || releaseEffect(evidence, request, verb == reconciliation.Destroy) != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the release copy of "+request.Identity.Cluster, lifecycle.PlacedOn(request.Placement), releaseDrift(evidence, request))
}

func (ReleaseFill) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeReleaseRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return lifecycle.SecretNames(request.SecretReferences()...), nil
}

func (ReleaseFill) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeReleaseRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretClasses(), nil
}

func (c ReleaseFill) Claimed(catalog api.Catalog) []string {
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared || !mirror.Managed || c.releases == nil {
		return nil
	}
	releases, err := c.releases(catalog)
	if err != nil {
		return nil
	}
	claimed := make([]string, 0, len(releases))
	for _, release := range releases {
		claimed = append(claimed, api.NewObject(api.OpenShiftCluster, release.Cluster, api.Value{}, api.Value{}).Identity())
	}
	return claimed
}
