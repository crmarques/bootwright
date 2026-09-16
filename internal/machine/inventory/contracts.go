package inventory

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
)

// EffectiveState compiles the selected context's immutable input into the
// graph this command reads. Inspection opens no host and writes nothing.
type EffectiveState interface {
	RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error)
}

// Ownership reports what the context's durable operation evidence proves about
// the objects its frozen plan names. A Machine the evidence does not name is
// reported as unmanaged rather than guessed.
type Ownership interface {
	Ownership(context.Context, string) (map[string]machine.OwnershipState, error)
}
