package machine

import (
	"maps"
	"testing"
)

// A pin reaches an adapter only as base64 text under keys that name the
// machine it belongs to, and carries exactly the fields the proof recorded: a
// value read as a template there would be evaluated rather than compared.
func TestPinValuesEncodeOnlyWhatWasProved(t *testing.T) {
	for name, test := range map[string]struct {
		identity HardwareIdentity
		suffix   string
		want     map[string]string
	}{
		"both fields": {
			HardwareIdentity{UUID: "4c4c4544-0042-3510-8052-b4c04f4d4e31", Serial: "CZJ2440ABC"}, "",
			map[string]string{
				"pinnedUUIDBase64":   "NGM0YzQ1NDQtMDA0Mi0zNTEwLTgwNTItYjRjMDRmNGQ0ZTMx",
				"pinnedSerialBase64": "Q1pKMjQ0MEFCQw==",
			},
		},
		"a template, under a node's suffix": {
			HardwareIdentity{UUID: "{{ 6 * 7 }}"}, "Node2",
			map[string]string{"pinnedUUIDBase64Node2": "e3sgNiAqIDcgfX0="},
		},
		"a serial alone": {
			HardwareIdentity{Serial: "CZJ2440ABC"}, "Node0",
			map[string]string{"pinnedSerialBase64Node0": "Q1pKMjQ0MEFCQw=="},
		},
		"nothing proved": {HardwareIdentity{}, "Node1", nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := test.identity.PinValues(test.suffix)
			if !maps.Equal(got, test.want) || (test.want == nil) != (got == nil) {
				t.Fatalf("pin values = %v, want %v", got, test.want)
			}
		})
	}
}
