package reconciliation

import (
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A block's request is canonical only as one sorted, compact object; every
// other encoding refuses in the words the plan always used. Its members are
// not declared, so a case-variant member that sorts in place is its own key.
func TestThePlanRequestRefusesEachNonCanonicalEncodingInItsOwnWords(t *testing.T) {
	const exact = `{"machine":"node-01","release":"9.6"}`
	if err := canonicalRequest(json.RawMessage(exact)); err != nil {
		t.Fatalf("the canonical request refused: %v", err)
	}
	for name, tc := range map[string]struct{ data, message string }{
		"an unsorted member":    {`{"undeclared":true,"machine":"node-01","release":"9.6"}`, "lifecycle block request is not canonical"},
		"a case-variant member": {`{"Machine":"other","machine":"node-01","release":"9.6"}`, ""},
		"a duplicate member":    {`{"machine":"other","machine":"node-01","release":"9.6"}`, "lifecycle block request is not canonical"},
		"trailing data":         {exact + "x", "lifecycle block request contains trailing data"},
		"a space after a colon": {`{"machine": "node-01","release":"9.6"}`, "lifecycle block request is not canonical"},
		"a line feed after it":  {exact + "\n", "lifecycle block request is not canonical"},
		"a truncated object":    {`{"machine":"node-01"`, "lifecycle block request is malformed"},
	} {
		err := canonicalRequest(json.RawMessage(tc.data))
		if tc.message == "" {
			if err != nil {
				t.Errorf("%s: refused: %v", name, err)
			}
			continue
		}
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != tc.message {
			t.Errorf("%s: refusal = %+v, want %q", name, reported, tc.message)
		}
	}
}
