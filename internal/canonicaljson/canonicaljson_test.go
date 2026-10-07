package canonicaljson

import (
	"errors"
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
