package contextfs

import (
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A persisted record reads back only as the exact canonical bytes its type
// encodes, LF-terminated, and every other encoding refuses in its own words.
func TestThePersistedRecordRefusesEachNonCanonicalEncodingInItsOwnWords(t *testing.T) {
	const exact = `{"version":3,"name":"lab"}` + "\n"
	var record reservation
	if err := decodeRecord([]byte(exact), maxRecord, &record); err != nil {
		t.Fatalf("the canonical record refused: %v", err)
	}
	for name, tc := range map[string]struct{ data, message string }{
		"an undeclared member":  {`{"undeclared":true,"version":3,"name":"lab"}` + "\n", "persisted record is malformed or unsupported"},
		"a case-variant member": {`{"Version":2,"version":3,"name":"lab"}` + "\n", "persisted record is not canonical"},
		"a duplicate member":    {`{"version":2,"version":3,"name":"lab"}` + "\n", "persisted record is not canonical"},
		"trailing data":         {exact + "x", "persisted record contains trailing data"},
		"a space after a colon": {`{"version": 3,"name":"lab"}` + "\n", "persisted record is not canonical"},
		"a missing line feed":   {exact[:len(exact)-1], "persisted record is not canonical"},
		"a doubled line feed":   {exact + "\n", "persisted record is not canonical"},
	} {
		var record reservation
		reported := diagnostics.Of(decodeRecord([]byte(tc.data), maxRecord, &record))
		if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != tc.message {
			t.Errorf("%s: refusal = %+v, want %q", name, reported, tc.message)
		}
	}
}
