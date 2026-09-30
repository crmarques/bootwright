//go:build linux && amd64

package contextfs

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/secrets/secretstore/areacontract"
)

func TestSecretAreaHonoursTheSecretAreaContract(t *testing.T) {
	areacontract.Verify(t, func(t *testing.T) areacontract.Open {
		store, sources := fixture(t)
		token := secretToken(publish(t, store, "example", sources))
		return func(readOnly bool, use func(secretstore.Area)) {
			access := store.MutateSecrets
			if readOnly {
				access = store.ReadSecrets
			}
			err := access(context.Background(), token, func(area secretstore.Area) error {
				use(area)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	})
}
