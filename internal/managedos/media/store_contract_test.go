package media_test

import (
	"testing"

	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/managedos/media/storecontract"
)

func TestMemoryStoreHonoursTheStoreContract(t *testing.T) {
	storecontract.Verify(t, func(*testing.T) media.Store { return media.NewMemoryStore() })
}
