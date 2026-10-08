package access

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// EffectiveState compiles the selected context's immutable input into the
// graph a cluster name resolves in. Compiling it opens no host and writes
// nothing.
type EffectiveState interface {
	RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error)
}

// ProducedReader reads the material a lifecycle block left in the context's
// custody, keyed by that block and the output's name. An entry that does not
// exist reports false and no material.
type ProducedReader interface {
	ReadProduced(ctx context.Context, contextName, block, name string) (Custodied, bool, error)
}

// Custodied is one custody entry's material and whether its access was never
// proved: a copy a removal kept from an installation whose completion no
// observation proved (D124). Material is bounded memory the caller clears.
type Custodied struct {
	Material secrets.Material
	Unproved bool
}
