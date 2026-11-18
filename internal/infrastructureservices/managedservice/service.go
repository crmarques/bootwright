package managedservice

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Frame struct {
	Identity       Identity
	Placement      machine.Placement
	Image          string
	ImageSource    *ImageSource
	Unit           string
	ContentRoot    string
	BindAddress    string
	TLSSecret      string
	CredentialsRef string
	Served         []string
	Probed         []string
	Sockets        []string
	Ports          []int
	Secrets        []string
	Reservations   []string
}

type Observed struct {
	Absent            bool
	Answered          bool
	Container         string
	ContentRoot       bool
	Postcondition     bool
	Request           string
	StartedAfterFiles bool
	Unit              string
}

// KindPort is everything one managed service kind supplies to the shared
// lifecycle; Noun is what its diagnostics call it.
type KindPort[R Frozen] struct {
	Kind           string
	Implementation string
	Variable       string
	Version        string
	Noun           string
	Purpose        string
	Subject        string
	Reservation    string
	ContentDigest  string
	Steps          [][2]string
	Requests       func(catalog api.Catalog, controllerMachine, contextName string) ([]R, error)
	Bound          func(material map[string]secrets.Material, secret, contextName string) error
	Certificate    Certificate
	Materials      func(execution lifecycle.Execution, request R, operation string) (lifecycle.Execution, []lifecycle.MaterialFile, error)
}

// Service realizes one managed service kind. It owns what completion,
// readiness and absence mean for that kind and nothing else.
type Service[R Frozen, E Reported[R]] struct {
	port     KindPort[R]
	runner   Runner
	clock    lifecycle.Clock
	hostKeys lifecycle.InstalledHostKey
}

func NewService[R Frozen, E Reported[R]](port KindPort[R], runner Runner) Service[R, E] {
	return Service[R, E]{port: port, runner: runner}
}

func (s Service[R, E]) WithClock(clock lifecycle.Clock) Service[R, E] {
	s.clock = clock
	return s
}

func (s Service[R, E]) WithInstalledHostKeys(hostKeys lifecycle.InstalledHostKey) Service[R, E] {
	s.hostKeys = hostKeys
	return s
}

func (s Service[R, E]) now() time.Time {
	if s.clock == nil {
		return time.Time{}
	}
	return s.clock.Now()
}

func (s Service[R, E]) decode(data []byte) (R, error) {
	return reconciliation.ThawVersion[R](data, s.port.Noun, s.port.Version)
}

// Plan derives one block per managed service of this kind. It reads no host,
// endpoint or Secret material and performs no effect.
func (s Service[R, E]) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	if input.State == nil {
		return lifecycle.CapabilityPlan{}, serviceRequestInvalid.Fail("lifecycle planning requires compiled desired state", "")
	}
	catalog := input.State.Effective()
	requests, err := s.port.Requests(catalog, input.Controller, input.Context.Name)
	if err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	for _, request := range requests {
		canonical, err := request.Canonical()
		if err != nil {
			return lifecycle.CapabilityPlan{}, err
		}
		frame := request.Frame()
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{
			ID:             frame.Identity.Block,
			Description:    s.describe(input.Verb, frame),
			Stage:          reconciliation.StageInfrastructureServices,
			Requires:       append(PlacementRequirements(catalog, frame.Placement, frame.Probed), MirrorRequirements(catalog, frame.ImageSource)...),
			Impacts:        impacts(input.Verb, frame),
			Groups:         s.groups(input.Verb, frame),
			Kind:           s.port.Kind,
			Object:         frame.Identity.Service,
			Family:         Family,
			Implementation: s.port.Implementation,
			ContentDigest:  s.port.ContentDigest,
			Request:        canonical,
		})
		for _, reference := range frame.Secrets {
			if !slices.Contains(plan.Secrets, reference) {
				plan.Secrets = append(plan.Secrets, reference)
			}
		}
		claim := infrastructureservices.HostReservation{
			Context: input.Context.Name, Kind: s.port.Reservation,
			Service: frame.Identity.Service, Keys: frame.Reservations,
		}
		if !frame.Placement.Local() {
			plan.SSHReservations = append(plan.SSHReservations, lifecycle.SSHReservation{
				Machine: frame.Placement.Machine, Address: frame.Placement.Address, Port: frame.Placement.Port,
				KnownHostsRef: frame.Placement.KnownHostsRef, Installed: frame.Placement.Installed(), Reservation: claim,
			})
			continue
		}
		plan.Reservations = append(plan.Reservations, claim)
	}
	slices.Sort(plan.Secrets)
	return plan, nil
}

func (s Service[R, E]) describe(verb reconciliation.Verb, frame Frame) string {
	if verb == reconciliation.Destroy {
		return "remove the " + s.port.Subject + " " + frame.Identity.Service + " from " + frame.Placement.Machine
	}
	return s.port.Purpose + " for " + frame.Identity.Service + " on " + frame.Placement.Machine
}

func impacts(verb reconciliation.Verb, frame Frame) []string {
	unit, root, listener := "create-container-unit", "create-path", "open-listener"
	if verb == reconciliation.Destroy {
		unit, root, listener = "remove-container-unit", "remove-path", "close-listener"
	}
	impacts := []string{unit + " " + frame.Unit, root + " " + frame.ContentRoot}
	for _, socket := range frame.Sockets {
		impacts = append(impacts, listener+" "+socket)
	}
	slices.Sort(impacts)
	return slices.Compact(impacts)
}

func (s Service[R, E]) groups(verb reconciliation.Verb, frame Frame) []reconciliation.Group {
	steps := s.port.Steps
	if steps == nil {
		steps = [][2]string{
			{"pull-image", "acquire the pinned service image"},
			{"publish-configuration", "publish the service configuration and unit"},
			{"start-service", "start the managed service"},
			{"verify-readiness", "verify the service answers"},
		}
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{
			{"stop-service", "stop the managed service"},
			{"remove-configuration", "remove the configuration, unit and content root"},
			{"verify-absence", "verify every owned resource is gone"},
		}
	}
	return lifecycle.StepGroups(steps, []string{frame.Placement.Machine})
}

// Removal reads a frozen block as the removal of the service it installed. It
// takes back only what it created, so removing it consumes nothing.
func (s Service[R, E]) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := s.decode(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	frame := request.Frame()
	return lifecycle.Removal{
		Description: s.describe(reconciliation.Destroy, frame),
		Impacts:     impacts(reconciliation.Destroy, frame),
		Groups:      s.groups(reconciliation.Destroy, frame),
	}, nil
}

func (s Service[R, E]) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return s.mutate(ctx, execution, "apply")
}

func (s Service[R, E]) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return s.mutate(ctx, execution, "destroy")
}

func (s Service[R, E]) mutate(ctx context.Context, execution lifecycle.Execution, operation string) (lifecycle.Result, error) {
	result, err := lifecycle.MutateBlock(ctx, s.mutation(operation), execution, operation)
	if errors.Is(err, ErrCertificateMismatch) && result.Outcome == reconciliation.OutcomeUnknown {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return result, err
}

type prepared[R any] struct {
	request     R
	fingerprint string
}

func (s Service[R, E]) mutation(operation string) lifecycle.Mutation[prepared[R]] {
	return lifecycle.Mutation[prepared[R]]{
		Prepare: func(ctx context.Context, execution lifecycle.Execution) (prepared[R], error) {
			request, fingerprint, err := s.prepare(ctx, execution, operation == "apply")
			return prepared[R]{request, fingerprint}, err
		},
		Run: func(ctx context.Context, execution lifecycle.Execution, _ string, call prepared[R]) (lifecycle.RunResult, error) {
			return s.run(ctx, execution, operation, call.request, call.fingerprint)
		},
		Reading:  s.reading,
		Unusable: serviceOutcomeUnusable.Fail("the "+s.port.Noun+" adapter reported no usable outcome", ""),
	}
}

// Observe is read-only. Live state matching the frozen request in full is
// positive completion; nothing present is positive no effect; this context's
// own service part way realized, a present one whose listener stays silent or
// that started before a file it runs from included, is positive partial;
// anything else, including an observation that could not be made, stays
// unknown. A fresh destroy over an incomplete apply resolves the apply's block
// through this reading too.
func (s Service[R, E]) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.ObserveBlock(ctx, s.mutation("observe"), execution, reconciliation.Apply)
}

// ObserveRemoval reads the same observation for what a removal takes back, not
// for readiness: nothing present is its completion; the unit active, the
// container of the frozen image and the content root all present is positive
// no effect, whether or not a listener answers; and part of them is a positive
// partial removal the next attempt converges. Only an observation that cannot
// be made or read as this request's stays unknown.
func (s Service[R, E]) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.ObserveBlock(ctx, s.mutation("observe"), execution, reconciliation.Destroy)
}

func (s Service[R, E]) reading(call prepared[R], digest string) lifecycle.EffectReading {
	noun, request, fingerprint := s.port.Noun, call.request, call.fingerprint
	return lifecycle.EffectReading{
		Presence:   func(evidence []byte) error { return Presence[R, E](noun, evidence, request, digest, fingerprint) },
		Absence:    func(evidence []byte) error { return Absence[R, E](noun, evidence, digest) },
		Partial:    func(evidence []byte) error { return Partial[R, E](noun, evidence, request, digest, fingerprint) },
		Unremoved:  func(evidence []byte) error { return Unremoved[R, E](noun, evidence, request, digest) },
		Unfinished: func(evidence []byte) error { return RemovalUnfinished[R, E](noun, evidence, request, digest) },
	}
}

// Unresolved says why an observation proved nothing, from the evidence it
// recorded: evidence this request's own that none of the verb's checks accept
// is named by the check that decided. Empty evidence, evidence for another
// request and evidence that does not decode are left to the engine's general
// reason.
func (s Service[R, E]) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := s.decode(block.Request)
	if err != nil || len(evidence) == 0 {
		return lifecycle.Unresolved{}, false
	}
	observed, err := decodeEvidence[E](s.port.Noun, evidence)
	if err != nil || observed.Observed().Request != block.RequestDigest {
		return lifecycle.Unresolved{}, false
	}
	effect, refused := lifecycle.ReadEffect(s.reading(prepared[R]{request, AnyCertificate}, block.RequestDigest), verb, evidence)
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, block.Kind+" "+block.Object, lifecycle.PlacedOn(request.Frame().Placement), refused)
}

func (s Service[R, E]) prepare(ctx context.Context, execution lifecycle.Execution, verify bool) (R, string, error) {
	var none R
	if err := ctx.Err(); err != nil {
		return none, "", err
	}
	if s.runner == nil {
		return none, "", serviceUnconfigured.Fail("the "+s.port.Noun+" adapter is not configured", "")
	}
	request, err := s.decode(execution.Block.Request)
	if err != nil {
		return none, "", err
	}
	frame := request.Frame()
	if frame.CredentialsRef != "" && verify {
		if err := ProveCredential(execution.Material, frame.CredentialsRef, execution.Context, true); err != nil {
			return none, "", err
		}
	}
	if frame.TLSSecret == "" {
		return request, "", nil
	}
	if s.port.Bound != nil {
		if err := s.port.Bound(execution.Material, frame.TLSSecret, execution.Context); err != nil {
			return none, "", err
		}
	}
	if s.port.Certificate == nil {
		return none, "", serviceUnconfigured.Fail("the "+s.port.Noun+" capability proves no serving certificate", "")
	}
	fingerprint, err := s.port.Certificate(execution.Material, frame.TLSSecret, execution.Context, frame.Served, s.now(), true)
	if err != nil && verify {
		return none, "", err
	}
	if err != nil {
		return request, "", nil
	}
	return request, fingerprint, nil
}

// ProveBinding proves the material a fresh apply just bound, before the
// operation registers, so unusable material refuses while nothing is held.
func (s Service[R, E]) ProveBinding(ctx context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	request, err := s.decode(block.Request)
	if err != nil {
		return err
	}
	frame := request.Frame()
	if frame.CredentialsRef != "" {
		if err := ProveCredential(material, frame.CredentialsRef, contextName, false); err != nil {
			return err
		}
	}
	if frame.TLSSecret == "" {
		return nil
	}
	if s.port.Certificate == nil {
		return serviceUnconfigured.Fail("the "+s.port.Noun+" capability proves no serving certificate", "")
	}
	_, err = s.port.Certificate(material, frame.TLSSecret, contextName, frame.Served, s.now(), false)
	return err
}

func (s Service[R, E]) run(ctx context.Context, execution lifecycle.Execution, operation string, request R, fingerprint string) (lifecycle.RunResult, error) {
	canonical, err := request.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	execution, materials, err := s.port.Materials(execution, request, operation)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	values := map[string]string{}
	if fingerprint != "" {
		values["fingerprint"] = fingerprint
	}
	frame := request.Frame()
	execution, placement, err := lifecycle.PinInstalledHost(execution, frame.Placement, s.hostKeys)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return s.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{
		Implementation: s.port.Implementation, Operation: operation, Variable: s.port.Variable,
		Canonical: canonical, Placement: placement, Materials: materials, Values: values,
		Refusals: ReadinessRefusals(s.port.Kind, frame.Identity.Service, frame.Placement.Machine, frame.BindAddress, frame.Ports),
	}))
}

func (s Service[R, E]) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := s.decode(block.Request)
	if err != nil {
		return nil, err
	}
	return lifecycle.SecretNames(request.Frame().Secrets...), nil
}

func (s Service[R, E]) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := s.decode(block.Request)
	if err != nil {
		return nil, err
	}
	frame := request.Frame()
	classes := frame.Placement.SecretClasses().Merge(frame.ImageSource.SecretClasses())
	return classes.Add(secrets.EffectMaterial, frame.TLSSecret).Add(secrets.EffectMaterial, frame.CredentialsRef), nil
}

// Quiescent is derived rather than probed. What consumes a managed service is
// the Machines and installations of this same context, and a removal probes
// every one of them, so a service whose consumers are all idle is idle too.
// Refusing on the service's own listener would instead refuse a removal whose
// consumers are already gone.
func (Service[R, E]) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its consumers are probed in this removal"}, nil
}
