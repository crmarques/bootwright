package operationstore_test

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore/areacontract"
)

func TestMemoryAreaHonoursTheAreaContract(t *testing.T) {
	areacontract.Verify(t, func(t *testing.T, use func(operationstore.Area)) {
		use(operationstore.NewMemoryArea())
	})
}
