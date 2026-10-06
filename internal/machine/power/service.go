package power

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Service struct {
	state       EffectiveState
	realization Realization
	identities  Identities
	runtime     Runtime
	runner      Runner
	confirmer   Confirmer
	reporter    Reporter
	selection   machine.CurrentSelection
}

func New(state EffectiveState, realization Realization, identities Identities, runtime Runtime, runner Runner, confirmer Confirmer, reporter Reporter, selection machine.CurrentSelection) Service {
	return Service{
		state: state, realization: realization, identities: identities, runtime: runtime, runner: runner,
		confirmer: confirmer, reporter: reporter, selection: selection,
	}
}

// realized asks the Realization port about the Machines of one context, for
// as long as the invocation that asks lasts.
func (s Service) realized(ctx context.Context, contextName string) realizations {
	return func(name string) (machine.OwnershipState, bool, error) {
		return s.realization.Realization(ctx, contextName, name)
	}
}

// Start converges one Machine to powered on.
func (s Service) Start(ctx context.Context, request PowerRequest) (*Result, error) {
	request.Verb = Start
	return s.converge(ctx, request)
}

// Stop converges one Machine to powered off. It asks the guest to shut down and
// proves it arrived; forcing the power off is a separate, explicit request.
func (s Service) Stop(ctx context.Context, request PowerRequest) (*Result, error) {
	request.Verb = Stop
	return s.converge(ctx, request)
}

// Restart converges one Machine to powered on through a stop it proves first,
// so a guest that never stopped is never reported as restarted.
func (s Service) Restart(ctx context.Context, request PowerRequest) (*Result, error) {
	request.Verb = Restart
	return s.converge(ctx, request)
}

// Read reports what each named Machine's own management controller answers
// about its power right now. It registers no operation and changes nothing: a
// reading is a live observation, so it is never evidence of ownership and
// never outlives the invocation that asked for it. A Machine this context
// resolves no reachable controller for is absent from the result rather than
// reported in a state nothing proved.
func (s Service) Read(ctx context.Context, contextName string, names []string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.state == nil || s.realization == nil || s.runtime == nil || s.runner == nil {
		return nil, availability.ErrNotImplemented
	}
	name, err := machine.SelectedContext(ctx, s.selection, contextName)
	if err != nil {
		return nil, err
	}
	effective, err := s.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: name})
	if err != nil {
		return nil, err
	}
	surveys, err := readSurveysFor(effective.Effective, name, names, s.realized(ctx, name))
	if err != nil {
		return nil, err
	}
	readings := map[string]string{}
	if len(surveys) == 0 {
		return readings, nil
	}
	var references []string
	for _, survey := range surveys {
		for _, reference := range readReferences(survey) {
			if !slices.Contains(references, reference) {
				references = append(references, reference)
			}
		}
	}
	// A reading names no retained output. An inspection that succeeds prints a
	// table, and one that refuses reports its own diagnostic, so a path to an
	// adapter log would be noise in the one case and the wrong answer in the
	// other: nothing here is an operation an operator resumes or inspects. Its
	// request therefore retains no output, so its run keeps no file and carries
	// no remediation pointing at one.
	err = s.runtime.WithRuntime(ctx, lifecycle.RuntimeRequest{ContextName: name, Secrets: references}, func(inner context.Context, runtime lifecycle.Runtime) error {
		for _, survey := range surveys {
			answered, err := s.observe(inner, runtime, survey)
			if err != nil {
				return err
			}
			maps.Copy(readings, answered)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return readings, nil
}

// observe runs one bounded reading against every controller a single host
// reaches, inside the runtime the whole survey shares.
func (s Service) observe(ctx context.Context, runtime lifecycle.Runtime, frozen ReadSurvey) (map[string]string, error) {
	canonical, err := frozen.Canonical()
	if err != nil {
		return nil, err
	}
	digest, err := reconciliation.RequestDigest(reconciliation.BlockDefinition{
		Kind: "Machine", Object: frozen.Placement.Machine, Implementation: ReadImplementation,
		ContentDigest: ReadContentDigest(), Request: canonical,
	})
	if err != nil {
		return nil, err
	}
	run, err := s.runner.Run(ctx, readInvocation(runtime, frozen, canonical, digest))
	if err != nil {
		return nil, err
	}
	return validateReading(run.Evidence, frozen, digest)
}

// converge drives one Machine to the power state its verb names. The operation
// registers nothing and owns nothing: a power state is not desired state, so it
// proves only what the controller reported when it settled. A physical Machine
// is held to the identity the context's current apply proved for it, read
// before anything is asked or run, so a pin that cannot be read refuses
// without a prompt.
func (s Service) converge(ctx context.Context, request PowerRequest) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.state == nil || s.realization == nil || s.identities == nil || s.runtime == nil || s.runner == nil {
		return nil, availability.ErrNotImplemented
	}
	name, err := machine.SelectedContext(ctx, s.selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	effective, err := s.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: name})
	if err != nil {
		return nil, err
	}
	frozen, physical, err := requestFor(effective.Effective, name, request.Name, request.Verb, request.Force, s.realized(ctx, name))
	if err != nil {
		return nil, err
	}
	var pin machine.HardwareIdentity
	if physical {
		proved, found, err := s.identities.ProvedIdentity(ctx, name, frozen.Identity.Object)
		if err != nil {
			return nil, err
		}
		if found {
			pin = proved
		}
	}
	if err := s.confirm(ctx, request, frozen); err != nil {
		return nil, err
	}
	return s.execute(ctx, name, frozen, pin)
}

// confirm asks before an operation interrupts a running system. Powering a
// machine on interrupts nothing, so only stopping and restarting ask.
func (s Service) confirm(ctx context.Context, request PowerRequest, frozen Request) error {
	if request.Verb == Start || request.SkipConfirmation {
		return nil
	}
	if s.confirmer == nil {
		return failure("lifecycle.state", "this operation requires confirmation", "repeat the command with --yes")
	}
	action := frozen.Verb + " machine"
	if frozen.Force {
		action = "force " + action
	}
	return s.confirmer.Confirm(ctx, action, frozen.Identity.Object)
}

func (s Service) execute(ctx context.Context, name string, frozen Request, pin machine.HardwareIdentity) (*Result, error) {
	canonical, err := frozen.Canonical()
	if err != nil {
		return nil, err
	}
	digest, err := reconciliation.RequestDigest(reconciliation.BlockDefinition{
		Kind: "Machine", Object: frozen.Identity.Object, Implementation: Implementation,
		ContentDigest: ContentDigest(), Request: canonical,
	})
	if err != nil {
		return nil, err
	}
	references := append([]string{frozen.Controller.CredentialsRef}, frozen.Placement.SecretReferences()...)
	if frozen.Controller.TrustBundleRef != "" {
		references = append(references, frozen.Controller.TrustBundleRef)
	}
	var result, retained *Result
	err = s.runtime.WithRuntime(ctx, lifecycle.RuntimeRequest{ContextName: name, Secrets: references, RetainOutput: true}, func(inner context.Context, runtime lifecycle.Runtime) error {
		retained = &Result{LogLocation: runtime.LogLocation, Logs: slices.Clone(runtime.Logs)}
		// The location is named before the adapter runs, because a run that
		// refuses reports a diagnostic rather than this result, and its output
		// is exactly what the operator is then told to read.
		if s.reporter != nil {
			s.reporter.ReportLogLocation(inner, runtime.LogLocation)
		}
		step := powerStep(frozen)
		s.progress(inner, step, "running", "")
		evidence, err := s.run(inner, runtime, frozen, canonical, digest, pin, step)
		if err != nil {
			s.progress(inner, step, settlement(inner, err), "")
			return err
		}
		s.progress(inner, step, outcome(evidence), evidence.Power)
		result = &Result{
			Context: name, Machine: frozen.Identity.Object, Verb: frozen.Verb,
			Power: evidence.Power, Previous: evidence.Previous, Changed: evidence.Changed,
			LogLocation: runtime.LogLocation, Logs: slices.Clone(runtime.Logs),
		}
		return nil
	})
	if err != nil {
		return retained, err
	}
	return result, nil
}

func (s Service) run(ctx context.Context, runtime lifecycle.Runtime, frozen Request, canonical []byte, digest string,
	pin machine.HardwareIdentity, step lifecycle.ProgressEvent) (Evidence, error) {
	run, err := s.runner.Run(ctx, invocation(runtime, frozen, canonical, digest, pin, s.groups(step, frozen.Force)))
	if err != nil {
		return Evidence{}, err
	}
	return validate(run.Evidence, frozen, digest)
}

// groups reports each adapter group as a sub-step of the run's one step. The
// runner calls it from the goroutine that reads the adapter, so it shares
// nothing but the reporter, which serializes its own rows.
func (s Service) groups(step lifecycle.ProgressEvent, force bool) func(context.Context, string, string) {
	return func(ctx context.Context, group, status string) {
		nested := step
		nested.Group, nested.Detail, nested.Status = group, groupDetail(group, force), status
		s.report(ctx, nested)
	}
}

// powerStep is the one progress step a power run reports. The adapter's groups
// are its sub-steps; it declares no count of them, because a stop skips the
// shutdown of a Machine that is already off.
func powerStep(frozen Request) lifecycle.ProgressEvent {
	action := map[string]string{Start: "Start", Stop: "Stop", Restart: "Restart"}[frozen.Verb]
	if frozen.Verb == Stop && frozen.Force {
		action = "Force off"
	}
	return lifecycle.ProgressEvent{
		Block: "power", Description: action + " " + string(api.Machine) + "/" + frozen.Identity.Object,
		Position: 1, Total: 1,
	}
}

// groupDetail names what one adapter group is doing in the operator's words.
func groupDetail(group string, force bool) string {
	switch group {
	case "read-state":
		return "read the power state"
	case "power-off":
		if force {
			return "power off"
		}
		return "shut down"
	case "power-on":
		return "power on"
	}
	return group
}

// outcome is what a settled run proved: a change, or a Machine that was
// already where its verb converges.
func outcome(evidence Evidence) string {
	if evidence.Changed {
		return "changed"
	}
	return "unchanged"
}

// settlement is what a run that did not settle proved: nothing once the
// invocation was canceled, an unproved state when the controller never
// confirmed one, and a failure otherwise.
func settlement(ctx context.Context, err error) string {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if slices.ContainsFunc(diagnostics.Of(err), func(reported diagnostics.Diagnostic) bool {
		return reported.Code == "lifecycle.unknown"
	}) {
		return "unknown"
	}
	return "failed"
}

func (s Service) progress(ctx context.Context, step lifecycle.ProgressEvent, status, detail string) {
	step.Status, step.Detail = status, detail
	s.report(ctx, step)
}

func (s Service) report(ctx context.Context, event lifecycle.ProgressEvent) {
	if s.reporter != nil {
		s.reporter.ReportProgress(ctx, event)
	}
}

func readInvocation(runtime lifecycle.Runtime, frozen ReadSurvey, canonical []byte, digest string) lifecycle.RunRequest {
	return lifecycle.RunRequest{
		Implementation: ReadImplementation,
		Operation:      ReadOperation,
		Variable:       ReadVariable,
		Digest:         digest,
		Canonical:      canonical,
		Placement:      frozen.Placement,
		Materials:      append(readMaterials(frozen), lifecycle.Materials(frozen.Placement)...),
		Launch:         runtime.Launch,
		Bundle:         runtime.Bundle,
		Area:           runtime.Area,
		Material:       runtime.Material,
		Output:         runtime.Output,
	}
}

func invocation(runtime lifecycle.Runtime, frozen Request, canonical []byte, digest string, pin machine.HardwareIdentity,
	progress func(context.Context, string, string)) lifecycle.RunRequest {
	materials := []lifecycle.MaterialFile{
		{Name: "bmc-user", Part: secrets.UsernamePart, Secret: frozen.Controller.CredentialsRef, Variable: "controllerUser"},
		{Name: "bmc-password", Part: secrets.PasswordPart, Secret: frozen.Controller.CredentialsRef, Variable: "controllerPassword"},
	}
	if frozen.Controller.TrustBundleRef != "" {
		materials = append(materials,
			lifecycle.MaterialFile{Name: "bmc-ca", Part: secrets.CertificatePart, Secret: frozen.Controller.TrustBundleRef, Variable: "controllerCA"})
	}
	request := lifecycle.RunRequest{
		Implementation:    Implementation,
		Operation:         Operation,
		Variable:          Variable,
		Digest:            digest,
		Canonical:         canonical,
		Placement:         frozen.Placement,
		Materials:         append(materials, lifecycle.Materials(frozen.Placement)...),
		MaterialValues:    pinValues(pin),
		Launch:            runtime.Launch,
		Bundle:            runtime.Bundle,
		Area:              runtime.Area,
		Material:          runtime.Material,
		Output:            runtime.Output,
		OutputRemediation: runtime.OutputRemediation,
		Progress:          progress,
	}
	if pin.Present() {
		request.Refusals = map[string]error{identityMismatch: identityRefusal(frozen, runtime.OutputRemediation)}
	}
	return request
}

// identityMismatch is the refusal a power run names when the machine's
// controller answers as another system than the one its pin records, before
// any power request. Only a run that carries a pin can name it.
const identityMismatch = "identity-mismatch"

// identityRefusal is what that refusal reports: the Machine, that its
// controller answers as another system, and what to change. The identities are
// what a controller reported, so they are printed bounded and printable in the
// run's retained output alone, which the remedy points at.
func identityRefusal(frozen Request, output string) error {
	identity := string(api.Machine) + "/" + frozen.Identity.Object
	remediation := "correct spec.hardware.management.bmc.address on " + identity + ", or run bootwright destroy --context " +
		frozen.Identity.Context + " and bootwright apply --context " + frozen.Identity.Context + " so the machine is proved again"
	if output != "" {
		remediation += "; " + output + " for both identities"
	}
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
		Severity: "error", Code: "lifecycle.state", Remediation: remediation,
		Message: "the management controller at " + frozen.Controller.Endpoint +
			" answers as another system than the one this context's current apply proved, so no power request was sent",
		Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: frozen.Identity.Object},
	}}}
}

// pinValues carries a proved identity to the adapter encoded, under the keys a
// power run reads for its one machine.
func pinValues(pin machine.HardwareIdentity) map[string]string {
	return pin.PinValues("")
}

// ContentDigest binds a power request to the exact behavior this build
// implements, so evidence returned for one request shape can never satisfy
// another.
func ContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"bootwright.machine.power-v1", Implementation, Operation, Variable,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}
