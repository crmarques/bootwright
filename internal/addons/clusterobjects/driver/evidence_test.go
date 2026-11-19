package driver

import (
	"slices"
	"testing"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	evidenceDigest  = "digest"
	evidenceVersion = "cluster-objects-evidence-v1"
)

var configMap = co.Ref{APIVersion: "v1", Kind: "ConfigMap", Namespace: "operators", Name: "settings", Namespaced: true}

func createProgram() co.Program {
	return co.Program{Steps: []co.Step{{Kind: "create", Create: &co.ObjectEntry{Ref: configMap, DesiredDigest: "desired"}}}}
}

func encoded(t *testing.T, e Evidence) []byte {
	t.Helper()
	e.Request, e.Version = evidenceDigest, evidenceVersion
	data, err := co.Canonical(e)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stopCode(t *testing.T, err error) string {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 {
		t.Fatalf("diagnostics = %+v (%v)", reported, err)
	}
	return reported[0].Code
}

// What the objects in the cluster prove decides the effect: a complete and
// postcondition-proved read completes, a complete read without the
// postcondition and a half-built one are partial, objects absent are no
// effect, and unknown or retired evidence says nothing.
func TestEvidenceDecidesTheEffectOfTheObjects(t *testing.T) {
	own := co.Evidence{
		Receipts:     []co.Receipt{{Ref: configMap, UID: "uid", DesiredDigest: "desired", Disposition: "created", Basis: co.Created}},
		Observations: []co.ObjectObservation{{Ref: configMap, Ownership: co.Own, Equal: true}},
	}
	for name, test := range map[string]struct {
		evidence Evidence
		want     reconciliation.EffectState
	}{
		"complete with its postcondition":    {Evidence{Evidence: own, Postcondition: true}, reconciliation.EffectCompleted},
		"complete without its postcondition": {Evidence{Evidence: own}, reconciliation.EffectPartial},
		"objects absent":                     {Evidence{Evidence: co.Evidence{Observations: []co.ObjectObservation{{Ref: configMap, Ownership: co.Absent}}}}, reconciliation.EffectNoEffect},
		"an object that differs":             {Evidence{Evidence: co.Evidence{Observations: []co.ObjectObservation{{Ref: configMap, Ownership: co.Own}}}}, reconciliation.EffectPartial},
		"a foreign object":                   {Evidence{Evidence: co.Evidence{Observations: []co.ObjectObservation{{Ref: configMap, Ownership: co.Foreign}}}}, reconciliation.EffectUnknown},
		"an unknown read":                    {Evidence{Evidence: co.Evidence{Unknown: true}}, reconciliation.EffectUnknown},
		"no coverage of the program":         {Evidence{}, reconciliation.EffectUnknown},
	} {
		got, err := Effect(encoded(t, test.evidence), createProgram(), evidenceDigest, evidenceVersion)
		if err != nil || got != test.want {
			t.Errorf("%s = %s (%v), want %s", name, got, err, test.want)
		}
	}
	retired, err := Retirement(evidenceDigest, evidenceVersion)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Effect(retired, createProgram(), evidenceDigest, evidenceVersion); err != nil || got != reconciliation.EffectUnknown {
		t.Fatalf("retired evidence = %s (%v)", got, err)
	}
}

// Evidence of another request or version, evidence carrying a native value
// that may not be kept and evidence that is not a record are each refused
// with their own stop, and an effect read of them is unknown.
func TestEvidenceOfAnotherRequestOrUnsafeValuesIsRefused(t *testing.T) {
	good := encoded(t, Evidence{})
	if _, err := DecodeEvidence(good, evidenceDigest, evidenceVersion); err != nil {
		t.Fatal(err)
	}
	unsafe := encoded(t, Evidence{Evidence: co.Evidence{Observations: []co.ObjectObservation{{Ref: co.Ref{Kind: "bad kind", Name: "x"}, Ownership: co.Own}}}})
	for name, test := range map[string]struct {
		data            []byte
		digest, version string
		code            string
	}{
		"another request": {good, "another", evidenceVersion, clusterobjectsEvidenceMismatch.Code},
		"another version": {good, evidenceDigest, "other-v1", clusterobjectsEvidenceMismatch.Code},
		"unsafe values":   {unsafe, evidenceDigest, evidenceVersion, clusterobjectsEvidenceUnsafe.Code},
	} {
		_, err := DecodeEvidence(test.data, test.digest, test.version)
		if got := stopCode(t, err); got != test.code {
			t.Errorf("%s = %s, want %s", name, got, test.code)
		}
		if state, err := Effect(test.data, createProgram(), test.digest, test.version); err == nil || state != reconciliation.EffectUnknown {
			t.Errorf("%s effect = %s (%v)", name, state, err)
		}
	}
	if _, err := DecodeEvidence([]byte(`not json`), evidenceDigest, evidenceVersion); err == nil {
		t.Fatal("evidence that is no record decoded")
	}
}

// Retirement evidence reads back as this request's, retired and proved, and
// only evidence that decodes for the request is kept, as a copy.
func TestRetirementAndKeptEvidenceReadBackAsTheRequests(t *testing.T) {
	retired, err := Retirement(evidenceDigest, evidenceVersion)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEvidence(retired, evidenceDigest, evidenceVersion)
	if err != nil || !decoded.Retired || !decoded.Postcondition || decoded.Request != evidenceDigest || decoded.Version != evidenceVersion {
		t.Fatalf("retirement = %+v (%v)", decoded, err)
	}
	kept := SafeEvidence(retired, evidenceDigest, evidenceVersion)
	if !slices.Equal(kept, retired) {
		t.Fatalf("kept = %s", kept)
	}
	kept[0] = 'X'
	if retired[0] == 'X' {
		t.Fatal("the kept evidence aliases its source")
	}
	if got := SafeEvidence(retired, "another", evidenceVersion); got != nil {
		t.Fatalf("another request's evidence was kept: %s", got)
	}
	if got := SafeEvidence([]byte(`{`), evidenceDigest, evidenceVersion); got != nil {
		t.Fatalf("malformed evidence was kept: %s", got)
	}
}

// The declared stops are a copy a caller cannot edit.
func TestTheDeclaredStopsAreACopy(t *testing.T) {
	first := Stops()
	if !slices.Equal(first, stops) {
		t.Fatalf("stops = %+v", first)
	}
	first[0].Code = "edited"
	if Stops()[0].Code != clusterobjectsRuntimeUnavailable.Code {
		t.Fatal("editing the returned stops changed the declared ones")
	}
}
