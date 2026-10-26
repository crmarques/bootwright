//go:build linux && amd64

package contextfs

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

var _ prerequisites.ControllerRebinder = (*Store)(nil)

const rebindRetry = "repeat bootwright controller rebind, which reads the record again"

func (s *Store) RebindController(ctx context.Context, recorded, next controller.InstalledHostIdentity) (prerequisites.RebindResult, error) {
	if !recorded.Valid() {
		return prerequisites.RebindResult{}, state("the recorded controller host identity is invalid")
	}
	nextDigest, err := next.PrivateDigest()
	if err != nil {
		return prerequisites.RebindResult{}, err
	}
	var result prerequisites.RebindResult
	err = s.transact(ctx, false, nil, "", func(t *transaction) error {
		stored, err := readControllerStored(ctx, t.root, t.registry)
		if err != nil {
			return err
		}
		if stored.data == nil || t.registry.Controller.Mode != "ready" {
			return controllerFailure("controller.identity", "this store holds no controller record to rebind", "")
		}
		switch {
		case stored.value.Host.Equal(next):
			return controllerFailure("controller.identity", "the controller record already names this host; nothing changed", "")
		case !stored.value.Host.Equal(recorded):
			return controllerFailure("controller.identity", "the controller record changed while its rebind was being confirmed", rebindRetry)
		}
		value := cloneControllerState(stored.value)
		for index := range value.Bindings {
			value.Bindings[index].HostDigest = nextDigest
		}
		value.Host = next
		if err := validateControllerState(value, stored.bundles); err != nil {
			return err
		}
		data, err := encodeRecord(controllerRecord(value, stored.bundles), maxControllerState)
		if err != nil {
			return err
		}
		dir, err := openControllerDirectory(t.root, t.registry)
		if err != nil {
			return err
		}
		defer dir.file.Close()
		if _, err := s.replaceControllerRecord(ctx, dir, stored, data, rebindRetry); err != nil {
			return err
		}
		result = prerequisites.RebindResult{Outcome: "rebound", Bindings: len(value.Bindings), Reservations: len(value.Reservations)}
		return nil
	})
	return result, err
}
