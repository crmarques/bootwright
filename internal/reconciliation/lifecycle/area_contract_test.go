package lifecycle

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore/areacontract"
)

func TestJourneyAreaHonoursTheAreaContract(t *testing.T) {
	areacontract.Verify(t, func(t *testing.T, use func(operationstore.Area)) {
		use(newArea())
	})
}
