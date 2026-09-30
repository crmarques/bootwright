package prerequisites_test

import (
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/prerequisites/storagecontract"
)

func TestMemoryStorageHonoursTheStorageContract(t *testing.T) {
	storagecontract.Verify(t, func(t *testing.T) storagecontract.Subject {
		scope := prerequisites.SetupContext{Name: "lab", Revision: "rev-0123456789abcdef0123456789abcdef", Machine: "controller"}
		storage, unsettle := prerequisites.NewMemoryStorage(t, scope)
		return storagecontract.Subject{Storage: storage, Scope: scope, Unsettle: unsettle}
	})
}
