package power

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	selection machine.CurrentSelection
}

func New(state EffectiveState, ownership Ownership, runtime Runtime, runner Runner, confirmer Confirmer, selection machine.CurrentSelection) Service {
	return Service{state: state, ownership: ownership, runtime: runtime, runner: runner, confirmer: confirmer, selection: selection}
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
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
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
