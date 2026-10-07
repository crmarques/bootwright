package artifactserver

import (
	"context"
	"slices"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// Capability realizes managed artifact servers. It owns what completion,
// readiness and absence mean for this kind and nothing else.
type Capability struct {
	runner Runner
	clock  lifecycle.Clock
}

func New(runner Runner, clock lifecycle.Clock) Capability {
	return Capability{runner: runner, clock: clock}
}

func (c Capability) now() time.Time {
	if c.clock == nil {
		return time.Time{}
	}
	return c.clock.Now()
}

// Plan derives one block per managed artifact server. It reads no host,
// endpoint or Secret material and performs no effect.
func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, refusal("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	catalog := input.State.Effective()
	requests, err := Requests(catalog, input.Controller, input.Context.Name)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	digest := ContentDigest()
	for _, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		definition := reconciliation.BlockDefinition{
			ID:             request.Identity.Block,
			Description:    description(input.Verb, request),
			Stage:          reconciliation.StageInfraComponents,
			Requires:       managedservice.BridgeRequirements(catalog, request.Placement.Machine, probedAddresses(request)),
			Impacts:        impacts(input.Verb, request),
			Groups:         groups(input.Verb, request),
			Kind:           Kind,
			Object:         request.Identity.Service,
			Implementation: Implementation,
			ContentDigest:  digest,
			Request:        canonical,
		}
		plan.Definitions = append(plan.Definitions, definition)
		for _, reference := range request.secretReferences() {
			if !slices.Contains(plan.Secrets, reference) {
				plan.Secrets = append(plan.Secrets, reference)
			}
		}
		claim := prerequisites.HostReservation{
			Context: input.Context.Name, Kind: "artifact-server", Service: request.Identity.Service, Keys: request.reservationKeys(),
		}
		if request.Placement.Connection != connectionLocal {
			plan.SSHReservations = append(plan.SSHReservations, lifecycle.SSHReservation{
				Machine: request.Placement.Machine, Address: request.Placement.Address, Port: request.Placement.Port, Reservation: claim,
			})
			continue
		}
		plan.Reservations = append(plan.Reservations, claim)
	}
	slices.Sort(plan.Secrets)
	return plan, nil
}

func description(verb reconciliation.Verb, request Request) string {
	if verb == reconciliation.Destroy {
		return "remove the artifact server " + request.Identity.Service + " from " + request.Placement.Machine
	}
	return "serve artifacts for " + request.Identity.Service + " on " + request.Placement.Machine
}

func impacts(verb reconciliation.Verb, request Request) []string {
	unit, root, listener := "create-container-unit", "create-path", "open-listener"
	if verb == reconciliation.Destroy {
		unit, root, listener = "remove-container-unit", "remove-path", "close-listener"
	}
	impacts := []string{unit + " " + request.Unit, root + " " + request.ContentRoot}
	for _, target := range request.ProbeTargets() {
		impacts = append(impacts, listener+" "+managedservice.HostPort(target.Address, target.Port))
	}
	slices.Sort(impacts)
	return slices.Compact(impacts)
}

func groups(verb reconciliation.Verb, request Request) []reconciliation.Group {
	machines := []string{request.Placement.Machine}
	steps := [][2]string{
		{"pull-image", "acquire the pinned server image"},
		{"publish-configuration", "publish the server configuration and unit"},
		{"start-service", "start the managed service"},
		{"verify-readiness", "verify every listener answers"},
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

// Removal reads a frozen artifact-server block as the removal of the service it
// installed. What it served is published beneath its own root by its consumers,
// so removing it consumes nothing of its own.
func (c Capability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
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

func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "apply")
}

func (c Capability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, execution, "destroy")
}

func (c Capability) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	request, fingerprint, err := c.prepare(ctx, execution, operation == "apply")
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, execution, operation, request, fingerprint)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	if result.Outcome != "changed" && result.Outcome != "unchanged" {
		return unknown, refusal("lifecycle.state", "the artifact-server adapter reported no usable outcome", "")
	}
	digest := execution.Block.RequestDigest
	if operation == "apply" {
		err = ValidatePresence(result.Evidence, request, digest, fingerprint)
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
// own server part way realized, a present one whose listener stays silent
// included, is positive partial; anything else, including an observation that
// could not be made, stays unknown. A fresh destroy over an incomplete apply
// resolves the apply's block through this reading too.
func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, func(evidence []byte, request Request, digest, fingerprint string) reconciliation.EffectState {
		switch {
		case ValidatePresence(evidence, request, digest, fingerprint) == nil:
			return reconciliation.EffectCompleted
		case ValidateAbsence(evidence, digest) == nil:
			return reconciliation.EffectNoEffect
		case ValidatePartial(evidence, request, digest, fingerprint) == nil:
			return reconciliation.EffectPartial
		}
		return reconciliation.EffectUnknown
	})
}

// ObserveRemoval reads the same observation for what a removal takes back, not
// for readiness: nothing present is its completion; the unit active, the
// container of the frozen image and the content root all present is positive
// no effect, whatever the listeners answer or present; and part of them is a
// positive partial removal the next attempt converges. Only an observation
// that cannot be made or read as this request's stays unknown.
func (c Capability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, func(evidence []byte, request Request, digest, _ string) reconciliation.EffectState {
		switch {
		case ValidateAbsence(evidence, digest) == nil:
			return reconciliation.EffectCompleted
		case ValidateUnremoved(evidence, request, digest) == nil:
			return reconciliation.EffectNoEffect
		case ValidateRemovalUnfinished(evidence, request, digest) == nil:
			return reconciliation.EffectPartial
		}
		return reconciliation.EffectUnknown
	})
}

// observe runs the one read-only observation both resolutions share and reads
// its evidence for the verb the block was frozen for.
func (c Capability) observe(ctx context.Context, execution lifecycle.Execution, read func([]byte, Request, string, string) reconciliation.EffectState) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, fingerprint, err := c.prepare(ctx, execution, false)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, execution, "observe", request, fingerprint)
	if err != nil {
		// An observation that failed proves nothing either way, so the effect
		// stays unknown. The reason still belongs in the attempt log: without
		// it a resolution loop reports only that it could not resolve.
		recordObservationFailure(ctx, execution, err)
		return unknown, nil
	}
	return lifecycle.Observation{Effect: read(result.Evidence, request, execution.Block.RequestDigest, fingerprint), Evidence: result.Evidence}, nil
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

// prepare decodes the frozen request and, when the operation serves content,
// proves the bound certificate before any connection or installation.
func (c Capability) prepare(ctx context.Context, execution lifecycle.Execution, verifyCertificate bool) (Request, string, error) {
	if err := ctx.Err(); err != nil {
		return Request{}, "", err
	}
	if c.runner == nil {
		return Request{}, "", refusal("lifecycle.state", "the artifact-server adapter is not configured", "")
	}
	request, err := DecodeRequest(execution.Block.Request)
	if err != nil {
		return Request{}, "", err
	}
	if request.TLS == nil {
		return request, "", nil
	}
	material, ok := execution.Material[request.TLS.Secret]
	if !ok {
		return Request{}, "", refusal("secret.store", "the bound serving certificate is not available to this attempt", "repeat the operation so its Secret bindings are reopened")
	}
	certificate, err := ValidateServingCertificate(material, request.TLS.Secret, execution.Context, request.servedAddresses("https"), c.now())
	if err != nil && verifyCertificate {
		return Request{}, "", err
	}
	if err != nil {
		return request, "", nil
	}
	return request, certificate.Fingerprint, nil
}

func (c Capability) run(ctx context.Context, execution lifecycle.Execution, operation string, request Request, fingerprint string) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	values := map[string]string{}
	if fingerprint != "" {
		values["fingerprint"] = fingerprint
	}
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: Implementation, Operation: operation, Variable: variablePrefix,
		Canonical: canonical, Placement: request.Placement, Materials: materials(request), Values: values,
	}))
}

const variablePrefix = "bootwright_artifact_server"

// materials lists this capability's own bound parts. The placement's identity
// and host key are added by the attempt, so they are not repeated here.
// Anything not listed never leaves bounded memory.
func materials(request Request) []lifecycle.MaterialFile {
	var files []lifecycle.MaterialFile
	if request.TLS != nil {
		files = append(files,
			lifecycle.MaterialFile{Name: "tls.crt", Part: secrets.CertificatePart, Secret: request.TLS.Secret, Variable: "certificate"},
			lifecycle.MaterialFile{Name: "tls.key", Part: secrets.PrivateKeyPart, Secret: request.TLS.Secret, Variable: "privateKey"},
		)
	}
	return files
}

// Unsupported refuses every selected object this capability cannot realize,
// with its reason and remedy, so the operation refuses before registration
// instead of part way through.
func (Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	return Refusals(state.Effective())
}

// Quiescent is derived rather than probed, exactly as the managed network
// services are: what fetches from this server is the installations of this
// same context, and a removal probes the Machines behind them.
func (Capability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its consumers are probed in this removal"}, nil
}
