package installation

import (
	"encoding/json"
	"testing"
)

func encode(t *testing.T, evidence Evidence) []byte {
	t.Helper()
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Positive no effect is exactly a powered-off machine holding no marker with
// nothing published. Anything else stays unknown, so a resolution never claims
// an installation did not happen when it cannot prove it.
func TestNoEffectRequiresAPoweredOffMachineWithNothingPublished(t *testing.T) {
	if err := ValidateNoEffect(encode(t, Evidence{Power: "Off", Request: "digest"}), "digest"); err != nil {
		t.Fatalf("a fresh machine was refused: %v", err)
	}
	for name, evidence := range map[string]Evidence{
		"running":          {Power: "On", Request: "digest"},
		"marker present":   {Marker: "{}", Power: "Off", Request: "digest"},
		"image published":  {Image: true, Power: "Off", Request: "digest"},
		"tree published":   {Power: "Off", Request: "digest", Tree: true},
		"no power reading": {Request: "digest"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateNoEffect(encode(t, evidence), "digest"); err == nil {
				t.Fatal("unproved absence was accepted as no effect")
			}
		})
	}
}

func TestAbsenceRequiresPositiveRemovalOfPublishedContent(t *testing.T) {
	gone := Evidence{Absent: true, Postcondition: true, Request: "digest"}
	if err := ValidateAbsence(encode(t, gone), "digest"); err != nil {
		t.Fatalf("removal evidence was refused: %v", err)
	}
	for name, evidence := range map[string]Evidence{
		"not absent":       {Postcondition: true, Request: "digest"},
		"no postcondition": {Absent: true, Request: "digest"},
		"image remains":    {Absent: true, Postcondition: true, Image: true, Request: "digest"},
		"tree remains":     {Absent: true, Postcondition: true, Request: "digest", Tree: true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAbsence(encode(t, evidence), "digest"); err == nil {
				t.Fatal("incomplete removal evidence was accepted")
			}
		})
	}
}

// Evidence that names another request proves nothing about this one, whatever
// else it reports.
func TestEvidenceMustNameItsOwnRequest(t *testing.T) {
	evidence := encode(t, Evidence{Absent: true, Postcondition: true, Request: "other"})
	if err := ValidateAbsence(evidence, "digest"); err == nil {
		t.Fatal("evidence for another request was accepted")
	}
	if err := ValidateNoEffect(evidence, "digest"); err == nil {
		t.Fatal("evidence for another request was accepted")
	}
}

func TestEvidenceIsBoundedAndStrictlyShaped(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":         {},
		"unknown field": []byte(`{"request":"digest","invented":true}`),
		"trailing":      []byte(`{"request":"digest"}{}`),
		"not an object": []byte(`"digest"`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAbsence(data, "digest"); err == nil {
				t.Fatal("malformed evidence was accepted")
			}
		})
	}
	oversized := make([]byte, maxEvidenceBytes+1)
	for index := range oversized {
		oversized[index] = ' '
	}
	if err := ValidateAbsence(oversized, "digest"); err == nil {
		t.Fatal("unbounded evidence was accepted")
	}
}
