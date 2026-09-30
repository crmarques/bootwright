package custody

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// Produce keeps what one block's proved effect produced, through the area the
// lifecycle transaction lends. It takes no lock of its own, because that
// transaction already holds the store's.
func (s Service) Produce(ctx context.Context, selected secretstore.Context, area secretstore.Area, request ProduceRequest) ([]secretstore.Produced, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.access == nil {
		return nil, secretstore.Failure("store.implementation", "secret service is not configured")
	}
	var result []secretstore.Produced
	err := s.access.MutateArea(ctx, selected, area, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return secretstore.Failure("store.uninitialized", "produced material needs an initialized secret store; run secret encryption init")
		}
		var err error
		result, err = session.Produce(ctx, request.Block, request.Outputs)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Withdraw removes every produced entry through the lent area. A store never
// initialized holds none, so there is nothing to withdraw.
func (s Service) Withdraw(ctx context.Context, selected secretstore.Context, area secretstore.Area) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.access == nil {
		return false, secretstore.Failure("store.implementation", "secret service is not configured")
	}
	withdrawn := false
	err := s.access.MutateArea(ctx, selected, area, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return nil
		}
		var err error
		withdrawn, err = session.Withdraw(ctx)
		return err
	})
	return withdrawn, err
}

// ReadProduced reads one produced entry under the store's shared lock. An
// entry that does not exist, including in a store never initialized, reports
// false and no material.
func (s Service) ReadProduced(ctx context.Context, request ReadProducedRequest) (secrets.Material, bool, error) {
	if err := ctx.Err(); err != nil {
		return secrets.Material{}, false, err
	}
	if s.access == nil {
		return secrets.Material{}, false, secretstore.Failure("store.implementation", "secret service is not configured")
	}
	selected, err := s.access.Context(ctx, request.ContextName)
	if err != nil {
		return secrets.Material{}, false, err
	}
	var material secrets.Material
	found := false
	err = s.access.View(ctx, selected.Context, true, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return nil
		}
		var err error
		material, found, err = session.ReadProduced(ctx, request.Block, request.Name)
		return err
	})
	if err != nil {
		material.Clear()
		return secrets.Material{}, false, err
	}
	return material, found, nil
}
