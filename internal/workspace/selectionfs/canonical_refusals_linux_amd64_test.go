//go:build linux && amd64

package selectionfs

import (
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A selection reads back only as the exact canonical bytes it encodes,
// LF-terminated, and every other encoding refuses in its own words. A
// repeated or case-variant member reads closed and then fails the proof, so
// it is not canonical; a reader treats every refusal alike, as no selection.
func TestTheSelectionRefusesEachNonCanonicalEncodingInItsOwnWords(t *testing.T) {
	const exact = `{"version":2,"name":"lab"}` + "\n"
	if _, err := decodeSelection([]byte(exact)); err != nil {
		t.Fatalf("the canonical selection refused: %v", err)
	}
	for name, tc := range map[string]struct{ data, message string }{
		"an undeclared member":  {`{"undeclared":true,"version":2,"name":"lab"}` + "\n", "selection record is invalid"},
		"a case-variant member": {`{"Version":1,"version":2,"name":"lab"}` + "\n", "selection record is not canonical"},
		"a duplicate member":    {`{"version":1,"version":2,"name":"lab"}` + "\n", "selection record is not canonical"},
		"trailing data":         {exact + "x", "selection record is invalid"},
		"a space after a colon": {`{"version": 2,"name":"lab"}` + "\n", "selection record is not canonical"},
		"a missing line feed":   {exact[:len(exact)-1], "selection record is not canonical"},
		"a doubled line feed":   {exact + "\n", "selection record is not canonical"},
	} {
		_, err := decodeSelection([]byte(tc.data))
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != tc.message {
			t.Errorf("%s: refusal = %+v, want %q", name, reported, tc.message)
		}
	}
}
