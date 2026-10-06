package installation

import (
	"context"
	"maps"
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
	"github.com/crmarques/bootwright/internal/substrate"
)

// Capability installs the operating system of a Bootwright-installed Machine.
// It owns what completion, replay and absence mean for that installation and
// nothing else: the Machine it installs onto is the substrate's.
type Capability struct {
	runner     Runner
	identities Identities
}

func New(runner Runner) Capability { return Capability{runner: runner} }

// WithIdentities returns a copy that reads a physical target's pin through
// identities, which composition binds to the pin's own owner.
func (c Capability) WithIdentities(identities Identities) Capability {
	c.identities = identities
	return c
}

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
		definition := reconciliation.BlockDefinition{
			ID:             request.Identity.Block,
			Description:    description(input.Verb, request),
			Stage:          reconciliation.StageMachines,
			Requires:       requires(requirements[index]),
			Impacts:        impacts(input.Verb, request),
			Groups:         groups(input.Verb, request),
			Exclusive:      request.ExclusiveKeys(),
			Kind:           Kind,
			Object:         request.Identity.Object,
			Implementation: Implementation,
			ContentDigest:  digest,
			Request:        canonical,
		}
		// A machine its substrate created holds nothing until this installs
		// something, and its disks are acknowledged when that substrate
		// destroys them. A physical machine already holds whatever it holds,
		// and this installation is the moment that content is lost, so the
		// acknowledgement belongs to the apply that erases it.
		if request.Target.Physical && input.Verb == reconciliation.Apply {
			definition.Consumes = []string{reconciliation.AuthorizationDataLoss}
		}
		plan.Definitions = append(plan.Definitions, definition)
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

// description states what the planned verb does to this installation. A
// removal takes back the media this block published; the installed system
// itself leaves with the disks the machine block deletes.
func description(verb reconciliation.Verb, request Request) string {
	if verb == reconciliation.Destroy {
		return "remove the installer media of " + request.Identity.Object
	}
	return "install the operating system of " + request.Identity.Object
}

func impacts(verb reconciliation.Verb, request Request) []string {
	published := []string{request.Image.Path}
	if request.Tree != nil {
		published = append(published, request.Tree.Path)
	}
	var impacts []string
	for _, path := range published {
		if verb == reconciliation.Destroy {
			impacts = append(impacts, "remove-content "+path)
			continue
		}
		impacts = append(impacts, "publish-content "+path)
	}
	if verb != reconciliation.Destroy {
		impacts = append(impacts,
			"install-operating-system "+request.Identity.Object,
			"power-on "+request.Identity.Object)
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
		{"await-installation", "wait for the installer to write the disk and power the machine off"},
		{"await-machine", "wait for the installed machine to boot and answer through its own channel"},
		{"verify-installation", "prove the installed machine answers on the key it reported"},
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

// Removal reads a frozen installation block as the removal of what it
// published. The installed system is retained on every arm, and the erasure a
// physical installation performed was acknowledged by the apply that performed
// it, so removing this block consumes nothing.
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
	request, marker, err := c.prepare(ctx, execution, operation)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, execution, operation, request, marker, "")
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
// positive no effect; published content on a guest that never installed, or
// the frozen marker with the completion incomplete, is positive partial;
// anything else stays unknown, including a guest that answers with a different
// marker and one powered on without any.
func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, "", func(evidence []byte, request Request, digest, marker string) reconciliation.EffectState {
		switch {
		case ValidatePresence(evidence, request, digest, marker) == nil:
			return reconciliation.EffectCompleted
		case ValidateNoEffect(evidence, digest) == nil:
			return reconciliation.EffectNoEffect
		case ValidatePartial(evidence, digest, marker) == nil:
			return reconciliation.EffectPartial
		}
		return reconciliation.EffectUnknown
	})
}

// observesRemoval scopes an observation to what a removal takes back: the
// adapter reads the published content alone, and neither the machine's
// identity channel, its fleet account nor its controller, none of which a
// removal changes.
const observesRemoval = "removal"

// ObserveRemoval reads, for what a removal proves, an observation of the
// published content alone. The removal withdraws that content whatever the
// guest holds, so no content left is its completion; the whole completion is
// positive no effect; and any content left, a package tree without its
// marker included, is a positive partial removal the next attempt converges.
func (c Capability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, execution, observesRemoval, func(evidence []byte, request Request, digest, marker string) reconciliation.EffectState {
		switch {
		case ValidateWithdrawn(evidence, digest) == nil:
			return reconciliation.EffectCompleted
		case ValidatePresence(evidence, request, digest, marker) == nil:
			return reconciliation.EffectNoEffect
		case ValidateWithdrawalUnfinished(evidence, digest) == nil:
			return reconciliation.EffectPartial
		}
		return reconciliation.EffectUnknown
	})
}

// observe runs the read-only observation operation, scoped by observes, and
// reads its evidence for the verb the block was frozen for.
func (c Capability) observe(ctx context.Context, execution lifecycle.Execution, observes string, read func([]byte, Request, string, string) reconciliation.EffectState) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, marker, err := c.prepare(ctx, execution, "observe")
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, execution, "observe", request, marker, observes)
	if err != nil {
		recordObservationFailure(ctx, execution, err)
		return unknown, nil
	}
	return lifecycle.Observation{Effect: read(result.Evidence, request, execution.Block.RequestDigest, string(marker)), Evidence: result.Evidence}, nil
}

// prepare decodes the frozen request and derives the exact marker bytes this
// attempt proves, which name the request digest the plan froze.
func (c Capability) prepare(ctx context.Context, execution lifecycle.Execution, operation string) (Request, []byte, error) {
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
	if operation == "apply" {
		if err := refusedContinuation(execution.Context, request); err != nil {
			return Request{}, nil, err
		}
	}
	marker, err := MarkerFor(request, execution.Block.RequestDigest)
	if err != nil {
		return Request{}, nil, err
	}
	return request, marker, nil
}

// refusedContinuation says why this executable does not apply a request an
// earlier executable froze, or nothing when it would plan that request itself.
// Planning refuses a physical target and a private publication before
// registration, but an operation registered before that refusal still carries
// one, and an apply performs exactly what was frozen. A destroy and an
// observation are never refused: the one takes back published content and the
// other reads. The removal it names is the one of the context the operation
// runs in.
func refusedContinuation(contextName string, request Request) error {
	machine := "Machine/" + request.Identity.Object
	remediation := "run bootwright destroy --context " + contextName + " to end this operation, then plan it again under this executable"
	if request.Target.Physical {
		return refusal("lifecycle.state", "this operation froze a physical installation of "+machine+", which this executable refuses", remediation)
	}
	if request.Private != nil {
		return refusal("lifecycle.state", "this operation froze a private publication for "+machine+
			" that the publicly served installer image would expose, which this executable refuses", remediation)
	}
	return nil
}

func (c Capability) run(ctx context.Context, execution lifecycle.Execution, operation string, request Request, marker []byte, observes string) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	values := map[string]string{"marker": string(marker)}
	if observes != "" {
		values["observes"] = observes
	}
	var refusals map[string]error
	if operation == "apply" {
		// Only an apply proves its target before it boots it.
		refusals = substrate.PreBootRefusals(request.Target.Substrate, execution.Context, request.Identity.Object, request.Target.Controller.Endpoint)
		key, err := authorizedKey(execution, request)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		values["authorizedKey"] = key
		pin, err := c.pinValues(execution, request)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		maps.Copy(values, pin)
	}
	// A machine proved by a delivered key is read over a connection pinned to
	// exactly that key, so the public half reaches every operation, not only
	// the one that installs it.
	if request.Target.HostKeyRef != "" {
		key, err := publicHalf(execution, request.Target.HostKeyRef, "SSH host key")
		if err != nil {
			return lifecycle.RunResult{}, err
		}
		values["hostKey"] = key
	}
	materials := []lifecycle.MaterialFile{
		{Name: "bmc-user", Part: secrets.UsernamePart, Secret: request.Target.Controller.CredentialsRef, Variable: "controllerUser"},
		{Name: "bmc-password", Part: secrets.PasswordPart, Secret: request.Target.Controller.CredentialsRef, Variable: "controllerPassword"},
		{Name: "fleet-id", Part: secrets.PrivateKeyPart, Secret: request.FleetKeyRef, Variable: "fleetIdentity"},
	}
	if request.Target.Controller.TrustBundleRef != "" {
		materials = append(materials,
			lifecycle.MaterialFile{Name: "bmc-ca", Part: secrets.CertificatePart, Secret: request.Target.Controller.TrustBundleRef, Variable: "controllerCA"})
	}
	// The private publication carries the key pair itself.
	if request.Private != nil {
		materials = append(materials,
			lifecycle.MaterialFile{Name: "host-key", Part: secrets.PrivateKeyPart, Secret: request.Target.HostKeyRef, Variable: "hostIdentity"},
			lifecycle.MaterialFile{Name: "host-key.pub", Part: secrets.PublicKeyPart, Secret: request.Target.HostKeyRef, Variable: "hostIdentityPublic"})
	}
	// The serving certificate is what the installing machine verifies the
	// private fetch against, and what a controller importing it trusts.
	if request.TLSCertificateRef != "" {
		materials = append(materials,
			lifecycle.MaterialFile{Name: "artifact-ca", Part: secrets.CertificatePart, Secret: request.TLSCertificateRef, Variable: "artifactCertificate"})
	}
	// The deadline follows the budgets this request froze, not this build's.
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: Implementation, Operation: operation, Variable: variablePrefix,
		Canonical: canonical, Placement: request.Placement, Materials: materials, Values: values,
		Refusals: refusals, Deadline: request.Deadline(),
	}))
}

// pinValues carries the identity a physical target's own Machine block proved
// earlier in this operation, so the pre-boot proof refuses a machine that
// answers as another system. A target its substrate created, and a Machine
// with no pin, carry nothing.
func (c Capability) pinValues(execution lifecycle.Execution, request Request) (map[string]string, error) {
	if !request.Target.Physical {
		return nil, nil
	}
	if c.identities == nil {
		return nil, refusal("lifecycle.state",
			"the reader of Machine/"+request.Identity.Object+"'s pinned identity is not configured", "")
	}
	pin, pinned, err := c.identities.PinnedIdentity(request.Identity.Object, execution.Proved)
	if err != nil || !pinned {
		return nil, err
	}
	return pin.PinValues(""), nil
}

// authorizedKey reads the public half of the bound fleet key. Only that half
// ever reaches the Kickstart, the image, the tree, the evidence or the logs.
// The adapter substitutes it after rendering, so the renderer's guard never
// sees it: it is refused here unless it stays inside its quoted directive.
func authorizedKey(execution lifecycle.Execution, request Request) (string, error) {
	key, err := publicHalf(execution, request.FleetKeyRef, "fleet access key")
	if err != nil {
		return "", err
	}
	if !kickstartQuoted(key) {
		return "", refusal("api.value", "the fleet access key's public half holds a character a Kickstart directive cannot carry",
			"replace Secret "+request.FleetKeyRef+" with a key whose public line, its separators and comment included, has no control character (a tab included), line or paragraph separator, double quote or backslash")
	}
	return key, nil
}

// publicHalf reads the public half of one bound key pair. A private half never
// leaves its binding as a value: where one is needed it is written to an
// operation-scoped file the adapter removes.
func publicHalf(execution lifecycle.Execution, reference, subject string) (string, error) {
	material, ok := execution.Material[reference]
	if !ok {
		return "", refusal("secret.store", "the bound "+subject+" is not available to this attempt", "repeat the operation so its Secret bindings are reopened")
	}
	value, ok := material.Part(secrets.PublicKeyPart)
	if !ok || len(value) == 0 {
		return "", refusal("secret.part", "the bound "+subject+" carries no public half", "repeat the operation so its Secret bindings are reopened")
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

// Unsupported refuses every selected installation this capability cannot
// realize, with its reason and remedy, so the operation refuses before
// registration.
func (Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	return Refusals(state.Effective())
}

// Quiescent is derived rather than probed. This block owns published installer
// content, which an installed Machine no longer reads; a Machine still reading
// it is one that is running, and its own block is probed in the same removal.
func (Capability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its machine is probed in this removal"}, nil
}
