package enrollment

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/trust"
)

// EffectiveState compiles the selected context's immutable input into the
// graph enrollment selects its Machines from.
type EffectiveState interface {
	RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error)
}

// HostKeyStore holds the host keys a context trusts. Publication is compared
// against the exact records this command read, so a concurrent first-use
// confirmation is never silently discarded.
type HostKeyStore interface {
	ReadHostKeys(ctx context.Context, contextName string) ([]byte, error)
	ReplaceHostKeys(ctx context.Context, contextName string, data, expected []byte) error
}

// Observer reads the key a server presents. It offers no credential, so what
// it returns is an observation and becomes trust only once this command
// records it.
type Observer interface {
	Observe(ctx context.Context, address string, port int) (trust.HostKey, error)
}

type Confirmer interface {
	Confirm(ctx context.Context, action, name string) error
}

// PlanPresenter shows the exact evaluated plan before the confirmation that
// authorizes it, so no operator confirms a fingerprint they were not shown.
type PlanPresenter interface {
	PresentTrustPlan(context.Context, Report) error
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
