package lifecycle

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// ResolutionLog is the records of one event the first resolution of the first
// attempt of one block of the context's first operation wrote to its log.
func (j *CapabilityJourney) ResolutionLog(t *testing.T, block, event string) []operationstore.LogRecord {
	t.Helper()
	return resolutionLog(t, j.harness, block, event)
}
