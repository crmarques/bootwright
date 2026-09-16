package installation

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// Capability installs the operating system of a Bootwright-installed Machine.
// It owns what completion, replay and absence mean for that installation and
// nothing else: the Machine it installs onto is the substrate's.
type Capability struct{ runner Runner }

func New(runner Runner) Capability { return Capability{runner: runner} }

const variablePrefix = "bootwright_os_install"

// Plan derives one block per Bootwright-installed Machine. It reads no host,
// endpoint or Secret material and performs no effect.
func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, refusal("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	requests, requirements, err := Requests(input.State.Effective(), input.Controller, input.Context.Name)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	digest := ContentDigest()
	var media []string
	for index, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID:             request.Identity.Block,
			Description:    "install the operating system of " + request.Identity.Object,
			Stage:          reconciliation.StageMachines,
			Requires:       requires(requirements[index]),
			Impacts:        impacts(request),
			Groups:         groups(input.Verb, request),
			Kind:           Kind,
			Object:         request.Identity.Object,
			Implementation: Implementation,
			ContentDigest:  digest,
			Request:        canonical,
		})
		plan.Secrets = append(plan.Secrets, request.SecretReferences()...)
		media = append(media, request.MediaNames()...)
		if !request.Placement.Local() {
			continue
		}
		plan.Reservations = append(plan.Reservations, prerequisites.HostReservation{
			Context: input.Context.Name, Kind: "os-install", Service: request.Identity.Object, Keys: request.ReservationKeys(),
		})
	}
	if claim, held := mediaReservation(input.Context.Name, media); held {
		plan.Reservations = append(plan.Reservations, claim)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

// requires names the API objects this block waits for, never their block
// identities, so no capability learns another's naming.
func requires(needs Requirements) []reconciliation.ObjectRef {
	references := []reconciliation.ObjectRef{{Kind: "Machine", Object: needs.Machine}}
	for _, name := range needs.ArtifactServers {
		references = append(references, reconciliation.ObjectRef{Kind: "ArtifactServer", Object: name})
	}
	for _, name := range needs.DNSServers {
		references = append(references, reconciliation.ObjectRef{Kind: "DNSServer", Object: name})
	}
	for _, name := range needs.NTPServers {
		references = append(references, reconciliation.ObjectRef{Kind: "NTPServer", Object: name})
	}
	return references
}

// mediaReservation freezes every store entry this context's installations name,
// as one claim carrying one key each: a reservation is identified by its
// context, kind and service, so the store entries are keys rather than
// services of their own. The claim is shared, so any number of contexts may
// hold it; it blocks only deletion and replacement of what it names.
func mediaReservation(contextName string, names []string) (prerequisites.HostReservation, bool) {
	keys := make([]string, 0, len(names))
	for _, name := range names {
		keys = append(keys, managedos.MediaReservationKey(name))
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	if len(keys) == 0 {
		return prerequisites.HostReservation{}, false
	}
	return prerequisites.HostReservation{
		Context: contextName, Kind: "media", Service: "media", Shared: true, Keys: keys,
	}, true
}

func impacts(request Request) []string {
	impacts := []string{
		"publish-content " + request.Image.Path,
		"install-operating-system " + request.Identity.Object,
		"power-on " + request.Identity.Object,
	}
	if request.Tree != nil {
		impacts = append(impacts, "publish-content "+request.Tree.Path)
	}
	slices.Sort(impacts)
	return slices.Compact(impacts)
}

func groups(verb reconciliation.Verb, request Request) []reconciliation.Group {
	machines := []string{request.Identity.Object}
	steps := [][2]string{
		{"publish-tree", "publish the package tree the installer fetches"},
		{"build-image", "build the machine's own installer image"},
		{"boot-installer", "insert the image and boot the machine from it"},
		{"verify-installation", "prove the installation completed and eject the media"},
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{
			{"remove-published", "remove the published image and tree"},
			{"verify-absence", "verify every published file is gone"},
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
	request, marker, err := c.prepare(ctx, execution)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, execution, operation, request, marker)
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
		return unknown, refusal("lifecycle.state", "the installation adapter reported no usable outcome", "")
	}
	digest := execution.Block.RequestDigest
	if operation == "apply" {
		err = ValidatePresence(result.Evidence, request, digest, string(marker))
	} else {
		err = ValidateAbsence(result.Evidence, digest)
	}
	if err != nil {
		return unknown, err
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

// Observe is read-only. A guest holding the frozen marker with its content
// published is positive completion; a powered-off guest with neither is
// positive no effect; anything else stays unknown, including a guest that
// answers with a different marker.
func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, marker, err := c.prepare(ctx, execution)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, execution, "observe", request, marker)
	if err != nil {
		recordObservationFailure(ctx, execution, err)
		return unknown, nil
	}
	digest := execution.Block.RequestDigest
	if ValidatePresence(result.Evidence, request, digest, string(marker)) == nil {
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: result.Evidence}, nil
	}
	if ValidateNoEffect(result.Evidence, digest) == nil {
		return lifecycle.Observation{Effect: reconciliation.EffectNoEffect, Evidence: result.Evidence}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectUnknown, Evidence: result.Evidence}, nil
}

// prepare decodes the frozen request and derives the exact marker bytes this
// attempt proves, which name the request digest the plan froze.
func (c Capability) prepare(ctx context.Context, execution lifecycle.Execution) (Request, []byte, error) {
	if err := ctx.Err(); err != nil {
		return Request{}, nil, err
	}
	if c.runner == nil {
		return Request{}, nil, refusal("lifecycle.state", "the installation adapter is not configured", "")
	}
	request, err := DecodeRequest(execution.Block.Request)
	if err != nil {
		return Request{}, nil, err
	}
	marker, err := MarkerFor(request, execution.Block.RequestDigest)
	if err != nil {
		return Request{}, nil, err
	}
	return request, marker, nil
}

func (c Capability) run(ctx context.Context, execution lifecycle.Execution, operation string, request Request, marker []byte) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	values := map[string]string{"marker": string(marker)}
	if operation == "apply" {
		key, err := authorizedKey(execution, request)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		values["authorizedKey"] = key
	}
	return c.runner.Run(ctx, lifecycle.RunRequest{
		Implementation: Implementation,
		Operation:      operation,
		Variable:       variablePrefix,
		Digest:         execution.Block.RequestDigest,
		Canonical:      canonical,
		Placement:      request.Placement,
		Materials: append([]lifecycle.MaterialFile{
			{Name: "bmc-user", Part: secrets.UsernamePart, Secret: request.Controller.CredentialsRef, Variable: "controllerUser"},
			{Name: "bmc-password", Part: secrets.PasswordPart, Secret: request.Controller.CredentialsRef, Variable: "controllerPassword"},
			{Name: "fleet-id", Part: secrets.PrivateKeyPart, Secret: request.FleetKeyRef, Variable: "fleetIdentity"},
		}, lifecycle.Materials(request.Placement)...),
		MaterialValues: values,
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

// authorizedKey reads the public half of the bound fleet key. Only that half
// ever reaches the Kickstart, the image, the tree, the evidence or the logs.
func authorizedKey(execution lifecycle.Execution, request Request) (string, error) {
	material, ok := execution.Material[request.FleetKeyRef]
	if !ok {
		return "", refusal("secret.store", "the bound fleet access key is not available to this attempt", "repeat the operation so its Secret bindings are reopened")
	}
	value, ok := material.Part(secrets.PublicKeyPart)
	if !ok || len(value) == 0 {
		return "", refusal("secret.part", "the bound fleet access key carries no public half", "repeat the operation so its Secret bindings are reopened")
	}
	return strings.TrimRight(string(value), "\n"), nil
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

// Unsupported names every selected installation this capability cannot
// realize, so the operation refuses before registration.
func (Capability) Unsupported(state *compilation.State) []string {
	if state == nil {
		return nil
	}
	return Unsupported(state.Effective())
}
