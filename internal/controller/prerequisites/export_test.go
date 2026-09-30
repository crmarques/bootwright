package prerequisites

import "testing"

// NewMemoryStorage is the storage double storagecontract.Verify holds: it
// holds the ready context scope names and no controller state. The function
// it returns makes the next publication's outcome unknown.
func NewMemoryStorage(t *testing.T, scope SetupContext) (Storage, func()) {
	f := newFixture(t)
	f.store.scope = SetupContext{Name: scope.Name, Revision: scope.Revision}
	return &f.store, func() { f.store.failPublication = f.store.writes + 1 }
}
