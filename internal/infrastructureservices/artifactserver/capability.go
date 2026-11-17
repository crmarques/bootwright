package artifactserver

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// Capability realizes managed artifact servers. It owns what completion,
// readiness and absence mean for this kind and nothing else.
type Capability struct {
	runner   Runner
	clock    lifecycle.Clock
	hostKeys lifecycle.InstalledHostKey
}

func New(runner Runner, clock lifecycle.Clock) Capability {
	return Capability{runner: runner, clock: clock}
}

func (c Capability) WithInstalledHostKeys(hostKeys lifecycle.InstalledHostKey) Capability {
	c.hostKeys = hostKeys
	return c
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
			Stage:          reconciliation.StageInfrastructureServices,
			Requires:       append(managedservice.PlacementRequirements(catalog, request.Placement, probedAddresses(request)), managedservice.MirrorRequirements(catalog, request.ImageSource)...),
			Impacts:        impacts(input.Verb, request),
			Groups:         groups(input.Verb, request),
			Kind:           Kind,
			Object:         request.Identity.Service,
			Family:         managedservice.Family,
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
				Machine: request.Placement.Machine, Address: request.Placement.Address, Port: request.Placement.Port,
				KnownHostsRef: request.Placement.KnownHostsRef, Installed: request.Placement.Installed(), Reservation: claim,
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
		impacts = append(impacts, listener+" "+infrastructureservices.HostPort(target.Address, target.Port))
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
	return lifecycle.StepGroups(steps, machines)
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
	result, err := lifecycle.MutateBlock(ctx, c.mutation(operation), execution, operation)
	if errors.Is(err, certificateMismatch) && result.Outcome == reconciliation.OutcomeUnknown {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return result, err
}

type prepared struct {
	request     Request
	fingerprint string
}

func (c Capability) mutation(operation string) lifecycle.Mutation[prepared] {
	return lifecycle.Mutation[prepared]{
		Prepare: func(ctx context.Context, execution lifecycle.Execution) (prepared, error) {
			request, fingerprint, err := c.prepare(ctx, execution, operation == "apply")
			return prepared{request, fingerprint}, err
		},
		Run: func(ctx context.Context, execution lifecycle.Execution, _ string, call prepared) (lifecycle.RunResult, error) {
			return c.run(ctx, execution, operation, call.request, call.fingerprint)
		},
		Reading:  serverReading,
		Unusable: refusal("lifecycle.state", "the artifact-server adapter reported no usable outcome", ""),
	}
}

// Observe is read-only. Live state matching the frozen request in full is
// positive completion; nothing present is positive no effect; this context's
// own server part way realized, a present one whose listener stays silent
// included, is positive partial; anything else, including an observation that
// could not be made, stays unknown. A fresh destroy over an incomplete apply
// resolves the apply's block through this reading too.
func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.ObserveBlock(ctx, c.mutation("observe"), execution, reconciliation.Apply)
}

// ObserveRemoval reads the same observation for what a removal takes back, not
// for readiness: nothing present is its completion; the unit active, the
// container of the frozen image and the content root all present is positive
// no effect, whatever the listeners answer or present; and part of them is a
// positive partial removal the next attempt converges. Only an observation
// that cannot be made or read as this request's stays unknown.
func (c Capability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.ObserveBlock(ctx, c.mutation("observe"), execution, reconciliation.Destroy)
}

// serverReading reads an artifact-server observation for the verb the block
// was frozen for. Only an apply reads the fingerprint of the bound
// certificate.
func serverReading(call prepared, digest string) lifecycle.EffectReading {
	request, fingerprint := call.request, call.fingerprint
	return lifecycle.EffectReading{
		Presence:   func(evidence []byte) error { return ValidatePresence(evidence, request, digest, fingerprint) },
		Absence:    func(evidence []byte) error { return ValidateAbsence(evidence, digest) },
		Partial:    func(evidence []byte) error { return ValidatePartial(evidence, request, digest, fingerprint) },
		Unremoved:  func(evidence []byte) error { return ValidateUnremoved(evidence, request, digest) },
		Unfinished: func(evidence []byte) error { return ValidateRemovalUnfinished(evidence, request, digest) },
	}
}

func serverEffect(verb reconciliation.Verb, evidence []byte, request Request, digest, fingerprint string) (reconciliation.EffectState, error) {
	return lifecycle.ReadEffect(serverReading(prepared{request, fingerprint}, digest), verb, evidence)
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
	material, err := BoundServingMaterial(execution.Material, request.TLS.Secret, execution.Context)
	if err != nil {
		return Request{}, "", err
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
	execution, placement, err := lifecycle.PinInstalledHost(execution, request.Placement, c.hostKeys)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: Implementation, Operation: operation, Variable: variablePrefix,
		Canonical: canonical, Placement: placement, Materials: materials(request, operation), Values: values,
		Refusals: managedservice.ReadinessRefusals(Kind, request.Identity.Service, request.Placement.Machine, request.BindAddress, listenerPorts(request)),
	}))
}

const variablePrefix = "bootwright_artifact_server"

// materials lists this capability's own bound parts. The placement's identity
// and host key are added by the attempt, so they are not repeated here.
// Anything not listed never leaves bounded memory.
func materials(request Request, operation string) []lifecycle.MaterialFile {
	var files []lifecycle.MaterialFile
	if request.TLS != nil {
		files = append(files,
			lifecycle.MaterialFile{Name: "tls.crt", Part: secrets.CertificatePart, Secret: request.TLS.Secret, Variable: "certificate"},
			lifecycle.MaterialFile{Name: "tls.key", Part: secrets.PrivateKeyPart, Secret: request.TLS.Secret, Variable: "privateKey"},
		)
	}
	if operation != "apply" {
		return files
	}
	return append(files, request.ImageSource.Materials()...)
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

func (Capability) PrecedesController(definition reconciliation.BlockDefinition) bool {
	request, err := DecodeRequest(definition.Request)
	return err == nil && (request.Placement.Connection == connectionSSH || request.Placement.Connection == connectionLocal || request.Placement.Installed())
}
