package power

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Service struct {
	state     EffectiveState
	ownership Ownership
	runtime   Runtime
	runner    Runner
	confirmer Confirmer
	reporter  Reporter
	selection machine.CurrentSelection
}

func New(state EffectiveState, ownership Ownership, runtime Runtime, runner Runner, confirmer Confirmer, reporter Reporter, selection machine.CurrentSelection) Service {
	return Service{state: state, ownership: ownership, runtime: runtime, runner: runner, confirmer: confirmer, reporter: reporter, selection: selection}
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
	if s.state == nil || s.ownership == nil || s.runtime == nil || s.runner == nil {
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
	owned, err := s.ownership.Ownership(ctx, name)
	if err != nil {
		return nil, err
	}
	surveys, err := readSurveysFor(effective.Effective, name, names, owned)
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
	// other: nothing here is an operation an operator resumes or inspects.
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
// proves only what the controller reported when it settled.
func (s Service) converge(ctx context.Context, request PowerRequest) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.state == nil || s.ownership == nil || s.runtime == nil || s.runner == nil {
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
	owned, err := s.ownership.Ownership(ctx, name)
	if err != nil {
		return nil, err
	}
	frozen, err := requestFor(effective.Effective, name, request.Name, request.Verb, request.Force, owned)
	if err != nil {
		return nil, err
	}
	if err := s.confirm(ctx, request, frozen); err != nil {
		return nil, err
	}
	return s.execute(ctx, name, frozen)
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

func (s Service) execute(ctx context.Context, name string, frozen Request) (*Result, error) {
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
	var result *Result
	err = s.runtime.WithRuntime(ctx, lifecycle.RuntimeRequest{ContextName: name, Secrets: references}, func(inner context.Context, runtime lifecycle.Runtime) error {
		// The location is named before the adapter runs, because a run that
		// refuses reports a diagnostic rather than this result, and its output
		// is exactly what the operator is then told to read.
		if s.reporter != nil {
			s.reporter.ReportLogLocation(inner, runtime.LogLocation)
		}
		run, err := s.runner.Run(inner, invocation(runtime, frozen, canonical, digest))
		if err != nil {
			return err
		}
		evidence, err := validate(run.Evidence, frozen, digest)
		if err != nil {
			return err
		}
		result = &Result{
			Context: name, Machine: frozen.Identity.Object, Verb: frozen.Verb,
			Power: evidence.Power, Previous: evidence.Previous, Changed: evidence.Changed,
			LogLocation: runtime.LogLocation, Logs: slices.Clone(runtime.Logs),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
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
		Sudo:           frozen.Placement.SudoPasswordRef,
		Launch:         runtime.Launch,
		Bundle:         runtime.Bundle,
		Area:           runtime.Area,
		Material:       runtime.Material,
		Output:         runtime.Output,
	}
}

func invocation(runtime lifecycle.Runtime, frozen Request, canonical []byte, digest string) lifecycle.RunRequest {
	materials := []lifecycle.MaterialFile{
		{Name: "bmc-user", Part: secrets.UsernamePart, Secret: frozen.Controller.CredentialsRef, Variable: "controllerUser"},
		{Name: "bmc-password", Part: secrets.PasswordPart, Secret: frozen.Controller.CredentialsRef, Variable: "controllerPassword"},
	}
	return lifecycle.RunRequest{
		Implementation: Implementation,
		Operation:      Operation,
		Variable:       Variable,
		Digest:         digest,
		Canonical:      canonical,
		Placement:      frozen.Placement,
		Materials:      append(materials, lifecycle.Materials(frozen.Placement)...),
		Sudo:           frozen.Placement.SudoPasswordRef,
		Launch:         runtime.Launch,
		Bundle:         runtime.Bundle,
		Area:           runtime.Area,
		Material:       runtime.Material,
		Output:         runtime.Output,
	}
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
