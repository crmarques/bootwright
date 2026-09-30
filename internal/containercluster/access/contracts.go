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
	ReadProduced(ctx context.Context, contextName, block, name string) (secrets.Material, bool, error)
}
