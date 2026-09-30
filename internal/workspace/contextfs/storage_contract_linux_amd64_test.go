//go:build linux && amd64

package contextfs

import (
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/prerequisites/storagecontract"
)

func TestControllerStorageHonoursTheStorageContract(t *testing.T) {
	storagecontract.Verify(t, func(t *testing.T) storagecontract.Subject {
		store, record := lifecycleFixture(t)
		return storagecontract.Subject{Storage: store, Scope: prerequisites.SetupContext{Name: record.Name, Revision: record.Revision, Machine: "controller"}}
	})
}
