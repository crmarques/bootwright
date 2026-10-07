package managedos

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A media record reads back only as the exact canonical bytes it encodes,
// LF-terminated, and every other encoding refuses in its own words.
func TestTheMediaRecordRefusesEachNonCanonicalEncodingInItsOwnWords(t *testing.T) {
	const name = "rhel-9.7-x86_64-boot.iso"
	const body = `"name":"` + name + `","size":1388429312,"sha256":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `","source":"file:///images/` + name + `","added":"2026-09-15T09:00:00Z"}`
	const exact = `{"version":1,` + body + "\n"
	if _, err := DecodeMediaRecord([]byte(exact), name); err != nil {
		t.Fatalf("the canonical record refused: %v", err)
	}
	for label, tc := range map[string]struct{ data, message string }{
		"an undeclared member":  {`{"undeclared":true,"version":1,` + body + "\n", "media record is malformed"},
		"a case-variant member": {`{"Version":2,"version":1,` + body + "\n", "media record is not canonical"},
		"a duplicate member":    {`{"version":2,"version":1,` + body + "\n", "media record is not canonical"},
		"trailing data":         {exact + "x", "media record exceeds its bounds or encoding"},
		"trailing data before":  {strings.TrimSuffix(exact, "\n") + "x\n", "media record is malformed"},
		"a space after a colon": {`{"version": 1,` + body + "\n", "media record is not canonical"},
		"a missing line feed":   {strings.TrimSuffix(exact, "\n"), "media record exceeds its bounds or encoding"},
		"a doubled line feed":   {exact + "\n", "media record is not canonical"},
	} {
		_, err := DecodeMediaRecord([]byte(tc.data), name)
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "media.store" || reported[0].Message != tc.message {
			t.Errorf("%s: refusal = %+v, want %q", label, reported, tc.message)
		}
	}
}
