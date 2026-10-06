package custody

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type StoreAccess interface {
	Context(context.Context, string) (secretstore.ContextSnapshot, error)
	View(context.Context, secretstore.Context, bool, func(secretstore.StoreSession, secretstore.Selection) error) error
	Mutate(context.Context, secretstore.Context, func(secretstore.StoreSession, secretstore.Selection) error) error
	// MutateArea opens the store in an area the caller already holds, so a
	// lifecycle transaction publishes produced material without a second lock.
	MutateArea(context.Context, secretstore.Context, secretstore.Area, func(secretstore.StoreSession, secretstore.Selection) error) error
}

type Materializer interface {
	Acquire(context.Context, secrets.Declaration, secrets.Input) (secrets.Material, error)
	Generate(context.Context, secrets.Declaration) (secrets.Material, error)
	Validate(context.Context, secrets.Declaration, secrets.Material) error
}

// Confirmer is the confirmation capability the composition supplies. Custody
// asks through its ContextConfirmer when it has one, so a prompt names the
// Secret and the context it belongs to.
type Confirmer interface {
	Confirm(context.Context, string, string) error
}

type ContextConfirmer interface {
	ConfirmIn(ctx context.Context, action, object, contextName string) error
}

type Compiler interface {
	Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error)
}
