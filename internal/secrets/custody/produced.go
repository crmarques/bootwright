package custody

import (
	"context"

	"github.com/crmarques/bootwright/internal/diagnostics"
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
			return diagnostics.NewFailureWithRemediation("secret.store.uninitialized",
				"produced material needs an initialized secret store, and the secret store of context "+selected.Name+" is not initialized", "",
				secrets.Command(selected.Name, "encryption init"))
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

// Holds reports, through the lent area, whether custody holds the produced
// entry of block and name, proved or not. A store never initialized holds
// none. It reads the entry's material only to prove the entry readable, and
// clears it at once.
func (s Service) Holds(ctx context.Context, selected secretstore.Context, area secretstore.Area, block, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.access == nil {
		return false, secretstore.Failure("store.implementation", "secret service is not configured")
	}
	held := false
	err := s.access.MutateArea(ctx, selected, area, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return nil
		}
		read, found, err := session.ReadProduced(ctx, block, name)
		read.Material.Clear()
		held = found
		return err
	})
	return held, err
}

// ReadProduced reads one produced entry under the store's shared lock. An
// entry that does not exist, including in a store never initialized, reports
// false and no material; an entry kept unproved says so.
func (s Service) ReadProduced(ctx context.Context, request ReadProducedRequest) (secretstore.ProducedMaterial, bool, error) {
	if err := ctx.Err(); err != nil {
		return secretstore.ProducedMaterial{}, false, err
	}
	if s.access == nil {
		return secretstore.ProducedMaterial{}, false, secretstore.Failure("store.implementation", "secret service is not configured")
	}
	selected, err := s.access.Context(ctx, request.ContextName)
	if err != nil {
		return secretstore.ProducedMaterial{}, false, err
	}
	var read secretstore.ProducedMaterial
	found := false
	err = s.access.View(ctx, selected.Context, true, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return nil
		}
		var err error
		read, found, err = session.ReadProduced(ctx, request.Block, request.Name)
		return err
	})
	if err != nil {
		read.Material.Clear()
		return secretstore.ProducedMaterial{}, false, err
	}
	return read, found, nil
}
