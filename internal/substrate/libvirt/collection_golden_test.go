package libvirt

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const collectionTaggedHost = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/libvirt_host_tagged.json"

// The collection's tests validate and render this request and publish this
// evidence from an observation of it, so a request or evidence field either
// side changes alone fails on one of them.
func TestTheCollectionHoldsItsHostTestsToTheTaggedRequestAndEvidence(t *testing.T) {
	request := taggedHostRequest(t, taggedObjects()...)
	frozen, err := request.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	var evidence HostEvidence
	if err := json.Unmarshal(hostEvidence(request, evidenceDigest), &evidence); err != nil {
		t.Fatal(err)
	}
	published, err := reconciliation.Freeze(evidence, "provider host evidence")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	shared, err := json.MarshalIndent(map[string]json.RawMessage{"evidence": published, "request": frozen}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	shared = append(shared, '\n')
	if *update {
		if err := os.WriteFile(collectionTaggedHost, shared, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(collectionTaggedHost)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(want, shared) {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s", collectionTaggedHost, lineDiff(string(want), string(shared)))
	}
}
