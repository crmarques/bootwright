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
}

type Materializer interface {
	Acquire(context.Context, secrets.Declaration, secrets.Input) (secrets.Material, error)
	File(context.Context, secrets.Declaration) (secrets.Material, error)
	Generate(context.Context, secrets.Declaration) (secrets.Material, error)
	Validate(context.Context, secrets.Declaration, secrets.Material) error
}

type Confirmer interface {
	Confirm(context.Context, string, string) error
}

type Compiler interface {
	Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error)
}
