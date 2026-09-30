//go:build linux && amd64

package contextfs

import (
	"testing"

	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/managedos/media/storecontract"
)

func TestMediaStoreHonoursTheStoreContract(t *testing.T) {
	storecontract.Verify(t, func(t *testing.T) media.Store { return mediaFixture(t) })
}
