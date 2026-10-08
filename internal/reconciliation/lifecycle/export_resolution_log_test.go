package lifecycle

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// ResolutionLog is the records of one event the first resolution of the first
// attempt of one block of the context's first operation wrote to its log.
func (j *CapabilityJourney) ResolutionLog(t *testing.T, block, event string) []operationstore.LogRecord {
	t.Helper()
	return resolutionLog(t, j.harness, block, event)
}

// BlockLog is the records of one event every log of one block, of every
// operation of the context, holds.
func (j *CapabilityJourney) BlockLog(t *testing.T, block, event string) []operationstore.LogRecord {
	t.Helper()
	area := j.harness.workspace.area
	area.mutex.Lock()
	logs := map[string][]byte{}
	for name, data := range area.files {
		if strings.Contains(name, "/logs/blocks/"+block+"/") {
			logs[name] = bytes.Clone(data)
		}
	}
	area.mutex.Unlock()
	var found []operationstore.LogRecord
	for _, data := range logs {
		lines := bufio.NewScanner(bytes.NewReader(data))
		for lines.Scan() {
			var record operationstore.LogRecord
			if err := json.Unmarshal(lines.Bytes(), &record); err != nil {
				t.Fatalf("the log of %s holds %q (%v)", block, lines.Text(), err)
			}
			if record.Event == event {
				found = append(found, record)
			}
		}
	}
	return found
}
