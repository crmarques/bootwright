package encryption

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type StoreAccess interface {
	Context(context.Context, string) (secretstore.ContextSnapshot, error)
	Types() []string
	View(context.Context, secretstore.Context, bool, func(secretstore.StoreSession, secretstore.Selection) error) error
	Mutate(context.Context, secretstore.Context, func(secretstore.StoreSession, secretstore.Selection) error) error
	Initialize(context.Context, secretstore.Context, string, func(secretstore.StoreSession, secretstore.Selection, bool) error) error
}

type Confirmer interface {
	Confirm(context.Context, string, string) error
}
