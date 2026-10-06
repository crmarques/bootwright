package power

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// EffectiveState compiles the selected context's immutable input into the
// graph a power operation resolves its target in.
type EffectiveState interface {
	RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error)
}

// Realization reports the verb of the context's current operation and the
// state of the one block that realizes a Machine on its provider, and whether
// the current plan names such a block. An emulated management controller is
// one of that block's effects, so a power operation asks it rather than the
// Machine as a whole, whose installation may still be unsettled.
type Realization interface {
	Realization(ctx context.Context, contextName, name string) (machine.OwnershipState, bool, error)
}

// Identities reports the hardware identity the context's current apply proved
// for one physical Machine, and whether it proved one. A physical controller
// answers whatever server is cabled behind it, so a power operation holds the
// Machine to that pin before it sends any power request.
type Identities interface {
	ProvedIdentity(context.Context, string, string) (machine.HardwareIdentity, bool, error)
}

// Runtime lends the controller's private execution boundary for exactly one
// bounded adapter call, with the Secret material that call needs bound.
type Runtime interface {
	WithRuntime(context.Context, lifecycle.RuntimeRequest, func(context.Context, lifecycle.Runtime) error) error
}

// Runner is the one Ansible boundary every power effect crosses. Power goes
// through the Machine's management controller, never through a hypervisor, so
// this is the same path a physical server takes.
type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

// Confirmer is the confirmation capability the composition supplies. Power
// asks through its ContextConfirmer when it has one, so a prompt names the
// Machine and the context it acts in.
type Confirmer interface {
	Confirm(context.Context, string, string) error
}

type ContextConfirmer interface {
	ConfirmIn(ctx context.Context, action, object, contextName string) error
}

// Reporter names where this run retains what its adapter prints, before that
// adapter runs, because a run that refuses reports a diagnostic rather than a
// result and its retained output is what an operator is told to read. It then
// carries the run as one progress step whose sub-steps are the adapter's
// groups, so a stop that waits on a guest is never silent.
type Reporter interface {
	ReportLogLocation(context.Context, string)
	ReportProgress(context.Context, lifecycle.ProgressEvent)
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
