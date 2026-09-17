package inventory

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
)

// EffectiveState compiles the selected context's immutable input into the
// graph this command reads. Compiling it opens no host and writes nothing.
type EffectiveState interface {
	RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error)
}

// Ownership reports what the context's durable operation evidence proves about
// the objects its frozen plan names. A Machine the evidence does not name is
// reported as one no operation has applied rather than guessed.
type Ownership interface {
	Ownership(context.Context, string) (map[string]machine.OwnershipState, error)
}

// PowerReader reports what each named Machine's own management controller
// answers about its power right now. It is the only thing this inspection asks
// a host for, so it is consulted only when the invocation asked for a reading;
// a Machine it returns no answer for is one this context reaches no management
// controller for.
type PowerReader interface {
	Read(ctx context.Context, contextName string, names []string) (map[string]string, error)
}
