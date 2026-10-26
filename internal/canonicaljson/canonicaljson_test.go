package canonicaljson

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

type shape struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

func TestEncodeAddsOneLineFeedOnlyForLine(t *testing.T) {
	for ending, want := range map[Ending]string{Bare: `{"name":"a","version":1}`, Line: "{\"name\":\"a\",\"version\":1}\n"} {
		data, err := Encode(shape{Name: "a", Version: 1}, ending)
		if err != nil || string(data) != want {
			t.Errorf("ending %v: %q (%v), want %q", ending, data, err, want)
		}
	}
	if _, err := Encode(func() {}, Bare); !errors.Is(err, ErrEncode) {
		t.Errorf("an unencodable value gave %v, want ErrEncode", err)
	}
}

func TestDecodeClosedRefusesAnUndeclaredMember(t *testing.T) {
	var target shape
	if err := DecodeClosed([]byte(`{"name":"a","version":1,"extra":true}`), &target); !errors.Is(err, ErrMalformed) {
		t.Fatalf("an undeclared member gave %v, want ErrMalformed", err)
	}
	if err := DecodeClosed([]byte(`{"name":"a"`), &target); !errors.Is(err, ErrMalformed) {
		t.Fatalf("a truncated value gave %v, want ErrMalformed", err)
	}
}

func TestDecodeRefusesACaseVariantOrDuplicateMemberAsNotCanonical(t *testing.T) {
	for _, data := range []string{
		`{"Name":"b","name":"a","version":1}`,
		`{"name":"b","name":"a","version":1}`,
		`{"name":"a","version":1,"Version":1}`,
	} {
		var target shape
		if err := Decode([]byte(data), &target, Bare); !errors.Is(err, ErrNotCanonical) {
			t.Errorf("%s gave %v, want ErrNotCanonical", data, err)
		}
	}
}

func TestDecodeRefusesAnythingButWhitespaceAfterTheValue(t *testing.T) {
	const exact = `{"name":"a","version":1}`
	for _, suffix := range []string{"x", "}", "]", "{}", " 1", "\n\"\""} {
		var target shape
		if err := DecodeClosed([]byte(exact+suffix), &target); !errors.Is(err, ErrTrailing) {
			t.Errorf("%q after the value gave %v, want ErrTrailing", suffix, err)
		}
	}
	var target shape
	if err := DecodeClosed([]byte(exact+" \t\r\n"), &target); err != nil {
		t.Errorf("white space after the value refused: %v", err)
	}
}

func TestDecodeWithLineAcceptsExactlyOneLineFeed(t *testing.T) {
	var target struct{}
	if err := Decode([]byte("{}\n"), &target, Line); err != nil {
		t.Fatalf("one line feed refused: %v", err)
	}
	for _, data := range []string{"{}", "{}\n\n", "{} \n"} {
		if err := Decode([]byte(data), &target, Line); !errors.Is(err, ErrNotCanonical) {
			t.Errorf("%q gave %v, want ErrNotCanonical", data, err)
		}
	}
}

func TestProveObjectRequiresSortedKeysAndNoWhitespace(t *testing.T) {
	if err := ProveObject([]byte(`{"a":1,"b":[true,null,"x"],"c":{"d":2}}`)); err != nil {
		t.Fatalf("a canonical object refused: %v", err)
	}
	for _, data := range []string{`{"b":1,"a":1}`, `{"a": 1}`, `{"a":1} `, `{"a":1,"a":1}`, `{"a":{"c":1,"b":2}}`} {
		if err := ProveObject([]byte(data)); !errors.Is(err, ErrNotCanonical) {
			t.Errorf("%s gave %v, want ErrNotCanonical", data, err)
		}
	}
	if err := ProveObject([]byte(`{"a":1}x`)); !errors.Is(err, ErrTrailing) {
		t.Errorf("trailing data gave %v, want ErrTrailing", err)
	}
}

func TestProveObjectKeepsLargeIntegersExactly(t *testing.T) {
	for _, data := range []string{`{"a":12345678901234567890}`, `{"a":1.50}`, `{"a":-0}`} {
		if err := ProveObject([]byte(data)); err != nil {
			t.Errorf("%s refused: %v", data, err)
		}
	}
}

func TestProveObjectRefusesANonObject(t *testing.T) {
	for _, data := range []string{`[]`, `null`, `1`, `"a"`, ``} {
		if err := ProveObject([]byte(data)); !errors.Is(err, ErrMalformed) {
			t.Errorf("%q gave %v, want ErrMalformed", data, err)
		}
	}
}

func TestProveObjectRequiresEscapedHTML(t *testing.T) {
	if err := ProveObject([]byte(`{"a":"<"}`)); !errors.Is(err, ErrNotCanonical) {
		t.Fatalf("an unescaped < gave %v, want ErrNotCanonical", err)
	}
	if err := ProveObject([]byte(`{"a":"\u003c"}`)); err != nil {
		t.Fatalf("an escaped < refused: %v", err)
	}
}

// canonicalVectors is read by the collection's test_canonical_json.py and by
// internal/adapterprotocol's decoder test: each line is the exact bytes
// module_utils/canonical_json.py writes for one record.
const canonicalVectors = "../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/canonical_json_records.json"

// TestEncodeEqualsThePythonVectorsWhereTheEscapingAgrees holds Encode to the
// bytes the Python encoder writes for every ASCII-only record without <, > or &,
// and pins the one divergence: json.Marshal escapes those three and writes
// non-ASCII raw, which the Python encoder escapes the other way round.
func TestEncodeEqualsThePythonVectorsWhereTheEscapingAgrees(t *testing.T) {
	data, err := os.ReadFile(canonicalVectors)
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct{ Admission, Line string }
	if err := json.Unmarshal(data, &vectors); err != nil || len(vectors) == 0 {
		t.Fatalf("read %s: %v", canonicalVectors, err)
	}
	diverged := 0
	for _, vector := range vectors {
		decoder := json.NewDecoder(strings.NewReader(vector.Line))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("%s: %v", vector.Line, err)
		}
		got, err := Encode(value, Bare)
		if err != nil {
			t.Fatalf("%s: %v", vector.Line, err)
		}
		if !strings.ContainsAny(vector.Line, "<>&") && !strings.Contains(vector.Line, `\u`) {
			if !bytes.Equal(got, []byte(vector.Line)) {
				t.Errorf("Encode = %s, want the Python bytes %s", got, vector.Line)
			}
			continue
		}
		diverged++
		if bytes.Equal(got, []byte(vector.Line)) {
			t.Errorf("Encode = %s, want Go's escaping, which differs from the Python bytes %s", got, vector.Line)
		}
		for _, want := range []string{"café", "☃", `\u003cb\u003e`, `\u0026`} {
			if !strings.Contains(string(got), want) {
				t.Errorf("Encode = %s, want it to hold %s", got, want)
			}
		}
	}
	if diverged != 1 {
		t.Fatalf("%d vectors hold <, >, & or an escape, want the one that pins the divergence", diverged)
	}
}
