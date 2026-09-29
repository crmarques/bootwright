//go:build linux && amd64

package contextfs

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore/areacontract"
)

func TestOperationAreaHonoursTheAreaContract(t *testing.T) {
	areacontract.Verify(t, func(t *testing.T, use func(operationstore.Area)) {
		store, _ := lifecycleFixture(t)
		err := store.MutateLifecycle(context.Background(), "example", func(tx lifecycle.Transaction) error {
			use(tx.Operations())
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
