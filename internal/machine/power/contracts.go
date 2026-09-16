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

// Ownership reports what the context's durable evidence proves about each
// object. An emulated management controller exists only while the Machine that
// owns it does, so a power operation asks before it acts.
type Ownership interface {
	Ownership(context.Context, string) (map[string]machine.OwnershipState, error)
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

type Confirmer interface {
	Confirm(context.Context, string, string) error
}

// Reporter names where this run retains what its adapter prints, before that
// adapter runs. It is the only thing a power operation reports while it works:
// a run that refuses reports a diagnostic rather than a result, and its
// retained output is what an operator is told to read.
type Reporter interface {
	ReportLogLocation(context.Context, string)
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
