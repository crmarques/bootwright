package localkeyring

import (
	"testing"
)

// A keyring record reads back only as the exact canonical bytes it encodes,
// LF-terminated, and every other encoding refuses in its own words.
func TestTheKeyringRecordRefusesEachNonCanonicalEncodingInItsOwnWords(t *testing.T) {
	const exact = `{"formatVersion":4,"context":"lab","id":"store"}` + "\n"
	var record identityRecord
	if err := decodeCanonical([]byte(exact), selectorMaximum, 16, &record); err != nil {
		t.Fatalf("the canonical record refused: %v", err)
	}
	for name, tc := range map[string]struct{ data, message string }{
		"an undeclared member":  {`{"undeclared":true,"formatVersion":4,"context":"lab","id":"store"}` + "\n", "invalid private record"},
		"a case-variant member": {`{"FormatVersion":3,"formatVersion":4,"context":"lab","id":"store"}` + "\n", "noncanonical private record"},
		"a duplicate member":    {`{"formatVersion":3,"formatVersion":4,"context":"lab","id":"store"}` + "\n", "noncanonical private record"},
		"trailing data":         {exact + "x", "invalid private record"},
		"a space after a colon": {`{"formatVersion": 4,"context":"lab","id":"store"}` + "\n", "noncanonical private record"},
		"a missing line feed":   {exact[:len(exact)-1], "noncanonical private record"},
		"a doubled line feed":   {exact + "\n", "noncanonical private record"},
	} {
		var record identityRecord
		if err := decodeCanonical([]byte(tc.data), selectorMaximum, 16, &record); err == nil || err.Error() != tc.message {
			t.Errorf("%s: refusal = %v, want %q", name, err, tc.message)
		}
	}
}
