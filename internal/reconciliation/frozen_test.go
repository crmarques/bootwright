package reconciliation

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Evidence reads as exactly one bounded value of the shape the capability
// declares, and every other reading refuses in the capability's words.
func TestDecodeEvidenceAdmitsOnlyOneBoundedValueOfItsShape(t *testing.T) {
	type evidence struct {
		Postcondition bool   `json:"postcondition"`
		Request       string `json:"request"`
	}
	const exact = `{"postcondition":true,"request":"r"}`
	for name, data := range map[string]string{
		"the shape":                   exact,
		"the shape at its bound":      exact + strings.Repeat(" ", 64-len(exact)),
		"the shape followed by space": exact + " \t\r\n",
	} {
		decoded, err := DecodeEvidence[evidence]([]byte(data), 64, "fixture")
		if err != nil || !decoded.Postcondition || decoded.Request != "r" {
			t.Errorf("%s: decoded %+v (%v)", name, decoded, err)
		}
	}
	for name, tc := range map[string]struct{ data, message string }{
		"nothing":                    {"", "the fixture adapter returned no bounded evidence"},
		"one byte past its bound":    {exact + strings.Repeat(" ", 65-len(exact)), "the fixture adapter returned no bounded evidence"},
		"a member it never declared": {`{"postcondition":true,"request":"r","widened":true}`, "the fixture adapter returned malformed evidence"},
		"another shape":              {`["r"]`, "the fixture adapter returned malformed evidence"},
		"a truncated value":          {`{"postcondition":true`, "the fixture adapter returned malformed evidence"},
		"a closing brace after it":   {exact + "}", "the fixture adapter returned trailing evidence"},
		"a closing bracket after it": {exact + "]", "the fixture adapter returned trailing evidence"},
		"a second value":             {exact + "{}", "the fixture adapter returned trailing evidence"},
	} {
		decoded, err := DecodeEvidence[evidence]([]byte(tc.data), 64, "fixture")
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != tc.message {
			t.Errorf("%s: refused with %v, want %q", name, err, tc.message)
		}
		if decoded != (evidence{}) {
			t.Errorf("%s: a refused reading returned %+v", name, decoded)
		}
	}
}

func TestThawRefusesAClosingDelimiterAfterTheRequest(t *testing.T) {
	type request struct {
		Version string `json:"version"`
	}
	if _, err := Thaw[request]([]byte(`{"version":"v1"}`), "fixture"); err != nil {
		t.Fatal(err)
	}
	for _, closer := range []string{"}", "]"} {
		if _, err := Thaw[request]([]byte(`{"version":"v1"}`+closer), "fixture"); err == nil {
			t.Errorf("a request followed by %s thawed", closer)
		}
	}
}
