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

// The version a request holds is read before its shape, so a request whose
// version removed a member refuses by naming that version, and only a version
// this build writes is read closed and proved canonical.
func TestThawVersionReadsTheVersionBeforeTheShape(t *testing.T) {
	type probe struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	const exact = `{"name":"lab","version":"probe-v2"}`
	decoded, err := ThawVersion[probe]([]byte(exact), "probe", "probe-v2")
	if err != nil || decoded != (probe{Name: "lab", Version: "probe-v2"}) {
		t.Fatalf("this version read as %+v (%v)", decoded, err)
	}
	const remedy = "destroy it with the build that applied it"
	const unreadable = "the frozen probe request has an unsupported version it does not name readably"
	for name, tc := range map[string]struct{ data, message, remediation string }{
		"another version with a removed member": {`{"aRemovedField":true,"name":"lab","version":"probe-v1"}`, "the frozen probe request has an unsupported version: probe-v1", remedy},
		"a number":                              {`{"name":"lab","version":2}`, unreadable, remedy},
		"no version":                            {`{"name":"lab"}`, unreadable, remedy},
		"an empty version":                      {`{"name":"lab","version":""}`, unreadable, remedy},
		"an unsafe version":                     {`{"name":"lab","version":"v2\nx"}`, unreadable, remedy},
		"a non-object":                          {`["probe-v2"]`, "the frozen probe request is malformed", ""},
		"this version and a new member":         {`{"aRemovedField":true,"name":"lab","version":"probe-v2"}`, "the frozen probe request is malformed", ""},
		"a case-variant version":                {`{"Version":"probe-v1","name":"lab","version":"probe-v2"}`, "the frozen probe request is not canonical", ""},
		"trailing data":                         {exact + "x", "the frozen probe request contains trailing data", ""},
	} {
		decoded, err := ThawVersion[probe]([]byte(tc.data), "probe", "probe-v2")
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != tc.message || reported[0].Remediation != tc.remediation || reported[0].Source != nil {
			t.Errorf("%s: refusal = %+v, want %q", name, reported, tc.message)
		}
		if decoded != (probe{}) {
			t.Errorf("%s: a refused reading returned %+v", name, decoded)
		}
	}
}

// A frozen request read back through Thaw and ProveCanonical refuses every
// encoding other than its canonical one, each in the words it always used.
func TestTheFrozenRequestRefusesEachNonCanonicalEncodingInItsOwnWords(t *testing.T) {
	type probe struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	const exact = `{"name":"lab","version":"v1"}`
	read := func(data string) error {
		request, err := Thaw[probe]([]byte(data), "probe")
		if err != nil {
			return err
		}
		return ProveCanonical([]byte(data), request, "probe")
	}
	if err := read(exact); err != nil {
		t.Fatalf("the canonical request refused: %v", err)
	}
	for name, tc := range map[string]struct{ data, message string }{
		"an undeclared member":         {`{"undeclared":true,"name":"lab","version":"v1"}`, "the frozen probe request is malformed"},
		"a case-variant member":        {`{"Name":"other","name":"lab","version":"v1"}`, "the frozen probe request is not canonical"},
		"a duplicate member":           {`{"name":"other","name":"lab","version":"v1"}`, "the frozen probe request is not canonical"},
		"trailing data":                {exact + "x", "the frozen probe request contains trailing data"},
		"a space after a colon":        {`{"name": "lab","version":"v1"}`, "the frozen probe request is not canonical"},
		"a line feed after the object": {exact + "\n", "the frozen probe request is not canonical"},
	} {
		reported := diagnostics.Of(read(tc.data))
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != tc.message {
			t.Errorf("%s: refusal = %+v, want %q", name, reported, tc.message)
		}
	}
}
