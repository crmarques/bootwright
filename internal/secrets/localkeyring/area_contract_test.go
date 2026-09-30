package localkeyring

import (
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/secrets/secretstore/areacontract"
)

func TestLimitAreaHonoursTheSecretAreaContract(t *testing.T) {
	areacontract.Verify(t, func(*testing.T) areacontract.Open {
		area := newLimitArea(map[string][]byte{})
		return func(readOnly bool, use func(secretstore.Area)) {
			use(area.next(readOnly))
		}
	})
}
