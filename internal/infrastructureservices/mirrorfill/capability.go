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
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	FileStall   = 2 * time.Minute
	fileBudget  = 10 * time.Minute
	runOverhead = 15 * time.Minute
)

func filesDeadline(request FilesRequest) time.Duration {
	return min(lifecycle.MaxDeadline, runOverhead+time.Duration(len(request.Files))*fileBudget)
}

type Files struct {
	runner    Runner
	artifacts Artifacts
}

func NewFiles(runner Runner, artifacts Artifacts) Files {
	return Files{runner: runner, artifacts: artifacts}
}

func (c Files) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
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
		ID: FilesBlockID(request.Identity.Environment), Description: filesDescription(input.Verb, request), Stage: reconciliation.StageInfrastructureServices,
		Requires:  []reconciliation.ObjectRef{{Kind: Kind, Object: request.Identity.Server, Family: managedservice.Family}},
		Exclusive: []string{"path:" + request.Root}, Impacts: filesImpacts(input.Verb, request), Consumes: []string{},
		Kind: Kind, Object: request.Identity.Server, Family: Family, Implementation: FilesImplementation, ContentDigest: FilesContentDigest(), Request: canonical,
	})
	plan.Secrets = lifecycle.SecretNames(request.SecretReferences()...)
	return plan, nil
}

func (c Files) Request(catalog api.Catalog, controllerMachine, contextName string) (FilesRequest, bool, error) {
	mirror, declared := infrastructureservices.ArtifactMirrorOf(catalog)
	if !declared || !mirror.Managed || mirror.URL == "" {
		return FilesRequest{}, false, nil
	}
	if c.artifacts == nil {
		return FilesRequest{}, false, refusal("lifecycle.state", "mirror planning requires the acquisition list", "")
	}
	artifacts, err := c.artifacts(catalog)
	if err != nil {
		return FilesRequest{}, false, err
	}
	server := mirror.Server
	machine, found := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	if !found {
		return FilesRequest{}, false, refusal("api.reference", "the artifact mirror's placement Machine is not in the selected graph", "declare it or correct machineRef on "+server.Identity())
	}
	placement, err := artifactserver.PlacementFor(catalog, server, controllerMachine)
	if err != nil {
		return FilesRequest{}, false, err
	}
	egress, err := managedservice.EgressFor(catalog, machine)
	if err != nil {
		return FilesRequest{}, false, err
	}
	environment := catalog.OfKind(api.Environment)[0].Name()
	served := artifactserver.ServedRoot(contextName, server.Name())
	request := FilesRequest{
		Egress: egress, Files: []File{}, Identity: Identity{Context: contextName, Environment: environment, Server: server.Name()},
		Placement: placement, Root: served + "/" + infrastructureservices.MirrorPath + "/" + environment, ServedRoot: served, Version: FilesImplementation,
	}
	for _, artifact := range artifacts {
		path, beneath := strings.CutPrefix(artifact.Location, mirror.URL+"/")
		if !beneath {
			continue
		}
		request.Files = append(request.Files, File{Bytes: artifact.Bytes, Path: path, SHA256: artifact.SHA256, Source: artifact.Source})
	}
	slices.SortFunc(request.Files, func(x, y File) int { return strings.Compare(x.Path, y.Path) })
	request.Files = slices.CompactFunc(request.Files, func(x, y File) bool { return x == y })
	if len(request.Files) == 0 {
		return FilesRequest{}, false, nil
	}
	if err := request.validate(); err != nil {
		return FilesRequest{}, false, err
	}
	return request, true, nil
}

func filesDescription(verb reconciliation.Verb, request FilesRequest) string {
	if verb == reconciliation.Destroy {
		return "remove the artifact mirror files of " + request.Identity.Environment + " from " + request.Placement.Machine
	}
	return "fill the artifact mirror of " + request.Identity.Environment + " on " + request.Placement.Machine
}

func filesImpacts(verb reconciliation.Verb, request FilesRequest) []string {
	if verb == reconciliation.Destroy {
		return []string{"remove-content " + request.Root}
	}
	return []string{"publish-content " + request.Root}
}

func (Files) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	request, err := DecodeFilesRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: filesDescription(reconciliation.Destroy, request), Impacts: filesImpacts(reconciliation.Destroy, request), Consumes: []string{}}, nil
}

func (c Files) run(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.RunResult, FilesRequest, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, FilesRequest{}, err
	}
	request, err := DecodeFilesRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	if execution.Block.Kind != Kind || execution.Block.Object != request.Identity.Server {
		return lifecycle.RunResult{}, request, refusal("lifecycle.state", "the mirror files request does not match its frozen block identity", "")
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, request, refusal("lifecycle.state", "the mirror files adapter is not configured", "")
	}
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, request, err
	}
	var refusals map[string]error
	if operation == "apply" {
		refusals = filesRefusals(execution.Context, request)
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: FilesImplementation,
		Operation: operation, Variable: "bootwright_mirror_files", Canonical: canonical, Placement: request.Placement,
		Refusals: refusals, Deadline: filesDeadline(request),
	}))
	return result, request, err
}

func filesRefusals(contextName string, request FilesRequest) map[string]error {
	server := diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: Kind, Name: request.Identity.Server}
	refused := func(code, message, remediation string) error {
		return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: code, Message: message, Remediation: remediation, Object: &server}}}
	}
	return map[string]error{
		"file-digest-mismatch": refused("lifecycle.state", "a publisher served a client file for the artifact mirror on "+request.Placement.Machine+" whose SHA-256 is not the one this build pins, so it was not published",
			"verify the publisher and the route from "+request.Placement.Machine+" to it, then run bootwright apply --context "+contextName),
		"file-unreachable": refused("lifecycle.unknown", "Machine/"+request.Placement.Machine+" could not read a client file for the artifact mirror from its publisher, so it was not published",
			"give "+request.Placement.Machine+" a route to the publishers bootwright render acquisitions --context "+contextName+" names, then run bootwright apply --context "+contextName),
	}
}

func (c Files) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c Files) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

func (c Files) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	result, request, err := c.run(ctx, execution, operation)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome := map[string]reconciliation.Outcome{"changed": reconciliation.OutcomeChanged, "unchanged": reconciliation.OutcomeUnchanged}[result.Outcome]
	evidence, err := decodeFilesEvidence(result.Evidence, execution.Block.RequestDigest)
	if outcome == "" || err != nil || !evidence.Postcondition || filesEffect(evidence, request, operation == "destroy") != reconciliation.EffectCompleted {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, refusal("lifecycle.state", "the mirror files adapter did not prove its postcondition", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func (c Files) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, false)
}

func (c Files) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, true)
}

func (c Files) observe(ctx context.Context, execution lifecycle.Execution, removal bool) (lifecycle.Observation, error) {
	observation := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	result, request, err := c.run(ctx, execution, "observe")
	if err != nil {
		return observation, err
	}
	observation.Evidence = result.Evidence
	evidence, err := decodeFilesEvidence(result.Evidence, execution.Block.RequestDigest)
	if err != nil {
		return observation, nil
	}
	observation.Effect = filesEffect(evidence, request, removal)
	return observation, nil
}

func (Files) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "the controller stage that reads the mirror precedes its fill in removal"}, nil
}

func (Files) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeFilesRequest(block.Request)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	evidence, err := decodeFilesEvidence(data, block.RequestDigest)
	if err != nil || filesEffect(evidence, request, verb == reconciliation.Destroy) != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the artifact mirror files of "+request.Identity.Environment, lifecycle.PlacedOn(request.Placement), filesDrift(evidence, request))
}

func (Files) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeFilesRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return lifecycle.SecretNames(request.SecretReferences()...), nil
}

func (Files) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeFilesRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.Placement.SecretClasses(), nil
}

func (Files) PrecedesController(definition reconciliation.BlockDefinition) bool {
	_, err := DecodeFilesRequest(definition.Request)
	return err == nil
}

func (Files) Claimed(catalog api.Catalog) []string {
	mirror, declared := infrastructureservices.ArtifactMirrorOf(catalog)
	if !declared || !mirror.Managed {
		return nil
	}
	return []string{mirror.Server.Identity()}
}
