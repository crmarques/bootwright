package secretstore

import (
	"testing"
)

// A private record reads back only as the exact canonical bytes it encodes,
// LF-terminated, and every other encoding refuses in its own words.
func TestTheSecretStoreRecordRefusesEachNonCanonicalEncodingInItsOwnWords(t *testing.T) {
	const exact = `{"version":3,"context":"lab","backend":"local","generation":"g1","payload":{}}` + "\n"
	var record Record
	if err := DecodeCanonical([]byte(exact), &record); err != nil {
		t.Fatalf("the canonical record refused: %v", err)
	}
	for name, tc := range map[string]struct{ data, message string }{
		"an undeclared member":  {`{"undeclared":true,"version":3,"context":"lab","backend":"local","generation":"g1","payload":{}}` + "\n", "invalid private record"},
		"a case-variant member": {`{"Version":2,"version":3,"context":"lab","backend":"local","generation":"g1","payload":{}}` + "\n", "noncanonical private record"},
		"a duplicate member":    {`{"version":2,"version":3,"context":"lab","backend":"local","generation":"g1","payload":{}}` + "\n", "noncanonical private record"},
		"trailing data":         {exact + "x", "noncanonical private record"},
		"a space after a colon": {`{"version": 3,"context":"lab","backend":"local","generation":"g1","payload":{}}` + "\n", "noncanonical private record"},
		"a missing line feed":   {exact[:len(exact)-1], "noncanonical private record"},
		"a doubled line feed":   {exact + "\n", "noncanonical private record"},
	} {
		var record Record
		if err := DecodeCanonical([]byte(tc.data), &record); err == nil || err.Error() != tc.message {
			t.Errorf("%s: refusal = %v, want %q", name, err, tc.message)
		}
	}
}
