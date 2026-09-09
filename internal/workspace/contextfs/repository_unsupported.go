//go:build !linux || !amd64

package contextfs

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/secrets/storage"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func unsupported(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return state("durable context storage requires qualified Linux/amd64")
}

func (*Store) CheckInputDirectory(ctx context.Context, _ string) error { return unsupported(ctx) }

func (*Store) View(ctx context.Context) (contexts.Registry, error) {
	return contexts.Registry{}, unsupported(ctx)
}

func (*Store) ReadInputs(ctx context.Context, _, _ string) (desiredstate.Sources, error) {
	return desiredstate.Sources{}, unsupported(ctx)
}

func (*Store) Transact(ctx context.Context, _ bool, _ []string, _ func(contexts.Transaction) error) error {
	return unsupported(ctx)
}

func (*Store) SecretContext(ctx context.Context, _ string) (storage.ContextSnapshot, error) {
	return storage.ContextSnapshot{}, unsupported(ctx)
}

func (*Store) ReadSecrets(ctx context.Context, _ storage.Context, _ func(storage.Area) error) error {
	return unsupported(ctx)
}

func (*Store) MutateSecrets(ctx context.Context, _ storage.Context, _ func(storage.Area) error) error {
	return unsupported(ctx)
}
