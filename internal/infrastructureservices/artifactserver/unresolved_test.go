package artifactserver

import (
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Evidence this request's own that none of the verb's checks accept is named
// by the check that decided: under an apply a server claiming its
// postcondition with its unit inactive, refused by the presence check; under a
// removal an absence form that still reports the server, refused by the
// unfinished removal's. Evidence for another request and no evidence at all
// are left to the general reason.
func TestUnresolvedNamesTheRefusingCheck(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	digest := call.Block.RequestDigest
	present := func(change func(*Evidence)) json.RawMessage {
		var evidence Evidence
		if err := json.Unmarshal(presenceEvidence(request, digest, fingerprint), &evidence); err != nil {
			t.Fatal(err)
		}
		change(&evidence)
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	inactive := present(func(e *Evidence) { e.Listeners, e.Unit = []ListenerEvidence{}, "inactive" })
	contradicted := present(func(e *Evidence) { e.Absent = true })
	subject := "ArtifactServer " + call.Block.Object + " on Machine controller"
	for name, test := range map[string]struct {
		verb     reconciliation.Verb
		evidence []byte
		want     lifecycle.Unresolved
		explains bool
	}{
		"an apply over an inactive unit": {reconciliation.Apply, inactive, lifecycle.Unresolved{
			Reason: subject + " is not what its apply froze: the artifact-server unit is not active",
			Remedy: "restore " + subject + " to what its frozen request names",
		}, true},
		"a removal over an absence form that reports the server": {reconciliation.Destroy, contradicted, lifecycle.Unresolved{
			Reason: subject + " is not what its destroy froze: the artifact-server evidence is an absence form, which reports nothing left to take back",
			Remedy: "restore " + subject + " so its observation reads it as this context's own",
		}, true},
		"a removal over an inactive unit": {reconciliation.Destroy, inactive, lifecycle.Unresolved{}, false},
		"evidence for another request":    {reconciliation.Apply, present(func(e *Evidence) { e.Unit, e.Request = "inactive", testDigest }), lifecycle.Unresolved{}, false},
		"no evidence":                     {reconciliation.Apply, nil, lifecycle.Unresolved{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, explains := New(nil, fixedClock{}).Unresolved(test.verb, call.Block, test.evidence)
			if explains != test.explains || got != test.want {
				t.Fatalf("unresolved = %+v (%t), want %+v (%t)", got, explains, test.want, test.explains)
			}
		})
	}
}
