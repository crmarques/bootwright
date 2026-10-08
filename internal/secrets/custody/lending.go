package custody

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// ReadCurrentRequest names the Secrets one bounded consumer reads for the
// length of one call.
type ReadCurrentRequest struct {
	ContextName string
	Names       []string
}

// ReadCurrent reads the current version of each named Secret in one keyring
// session under the store's shared lock, for a bounded consumer such as a
// machine power run, rsh or exec. It selects and refuses exactly as Bind does,
// but publishes nothing: no binding and no identity reservation, so any number
// of calls leaves the keyring as it found it, and bounded consumers of two
// contexts read together. A caBundle comes back as its certificate alone, as
// Reopen lends it. The caller owns the material and clears it.
func (s Service) ReadCurrent(ctx context.Context, request ReadCurrentRequest) ([]secretstore.BoundMaterial, error) {
	selected, requested, err := s.requested(ctx, request.ContextName, request.Names)
	if err != nil {
		return nil, err
	}
	var result []secretstore.BoundMaterial
	err = s.access.View(ctx, selected, true, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		if session == nil {
			return secretstore.Uninitialized(selected.Name)
		}
		selection, err := s.readCurrent(ctx, session, selected.Name, requested)
		if err != nil {
			return err
		}
		result = make([]secretstore.BoundMaterial, 0, len(selection))
		for _, item := range selection {
			result = append(result, secretstore.BoundMaterial{Version: item.version, Material: item.material})
		}
		return nil
	})
	if err != nil {
		clearBound(result)
		return nil, err
	}
	narrowCABundles(result)
	return result, nil
}
