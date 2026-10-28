package installation

import (
	"encoding/json"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func originalPhysicalErase(t *testing.T) (lifecycle.BlockEvidence, lifecycle.BlockEvidence) {
	t.Helper()
	machine := eraseMachine()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	erase, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	eraseCanonical, err := erase.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	proof := func(implementation, id string, data []byte) lifecycle.BlockEvidence {
		definition := reconciliation.BlockDefinition{ID: id, Kind: Kind, Object: machine.Name(), Implementation: implementation, ContentDigest: ContentDigest(), Request: data}
		if implementation == EraseImplementation {
			definition.ContentDigest = EraseContentDigest()
		}
		digest, err := reconciliation.RequestDigest(definition)
		if err != nil {
			t.Fatal(err)
		}
		return lifecycle.BlockEvidence{Kind: Kind, Implementation: implementation, Object: machine.Name(), Verb: reconciliation.Apply, State: reconciliation.BlockPending,
			Block: reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}}
	}
	return proof(Implementation, BlockID(machine.Name()), canonical), proof(EraseImplementation, EraseBlockID(machine.Name()), eraseCanonical)
}

func TestPhysicalEraseUnstartedRequiresPositiveOriginalAndPairedRetention(t *testing.T) {
	install, policy := originalPhysicalErase(t)
	original := []lifecycle.BlockEvidence{install, policy}
	if ok, err := PhysicalEraseUnstarted(original, nil, install.Object); err != nil || !ok {
		t.Fatalf("pending policy: %t %v", ok, err)
	}
	policy.State = reconciliation.BlockDone
	policy.Evidence, _ = json.Marshal(EraseEvidence{Version: EraseImplementation, Request: policy.Block.RequestDigest, Prepared: true})
	original[1] = policy
	removed := policy
	removed.Verb, removed.State = reconciliation.Destroy, reconciliation.BlockDone
	removed.Evidence, _ = json.Marshal(EraseEvidence{Version: EraseImplementation, Request: policy.Block.RequestDigest, Prepared: true, Removed: true, Retained: true})
	pairs := []lifecycle.RemovalEvidence{{Applied: policy, Removed: removed}}
	if ok, err := PhysicalEraseUnstarted(original, pairs, install.Object); err != nil || !ok {
		t.Fatalf("retained policy: %t %v", ok, err)
	}
	for name, mutate := range map[string]func([]lifecycle.BlockEvidence, []lifecycle.RemovalEvidence){
		"missing original":   func(records []lifecycle.BlockEvidence, _ []lifecycle.RemovalEvidence) { records[0].Object = "another" },
		"duplicate original": func(records []lifecycle.BlockEvidence, _ []lifecycle.RemovalEvidence) { records[1] = records[0] },
		"started install": func(records []lifecycle.BlockEvidence, _ []lifecycle.RemovalEvidence) {
			records[0].State = reconciliation.BlockRunning
		},
		"pending evidence": func(records []lifecycle.BlockEvidence, _ []lifecycle.RemovalEvidence) {
			records[0].Evidence = json.RawMessage(`{}`)
		},
		"foreign paired apply": func(_ []lifecycle.BlockEvidence, pairs []lifecycle.RemovalEvidence) {
			pairs[0].Applied.Object = "another"
		},
		"foreign paired inverse": func(_ []lifecycle.BlockEvidence, pairs []lifecycle.RemovalEvidence) {
			pairs[0].Removed.Block.ID = "another"
		},
		"partial inverse": func(_ []lifecycle.BlockEvidence, pairs []lifecycle.RemovalEvidence) {
			pairs[0].Removed.State = reconciliation.BlockUnknown
		},
		"erase claimed instead": func(_ []lifecycle.BlockEvidence, pairs []lifecycle.RemovalEvidence) {
			pairs[0].Removed.Evidence = json.RawMessage(`{"version":"physical-erase-v6","request":"wrong","prepared":true,"removed":true}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			records := append([]lifecycle.BlockEvidence(nil), original...)
			proofs := append([]lifecycle.RemovalEvidence(nil), pairs...)
			mutate(records, proofs)
			if ok, _ := PhysicalEraseUnstarted(records, proofs, install.Object); ok {
				t.Fatal("unproved retention accepted")
			}
		})
	}
	if ok, _ := PhysicalEraseUnstarted(original, nil, install.Object); ok {
		t.Fatal("missing inverse accepted")
	}
	if ok, _ := PhysicalEraseUnstarted(original, append(pairs, pairs[0]), install.Object); ok {
		t.Fatal("ambiguous inverse accepted")
	}
}

func TestPhysicalEraseUnstartedCanRetainAnUnstartedNonErasingInstall(t *testing.T) {
	install, policy := originalPhysicalErase(t)
	request, err := DecodeRequest(install.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	request.EraseOnRemoval = false
	install.Block.Request, _ = request.Canonical()
	install.Block.RequestDigest, err = reconciliation.RequestDigest(install.Block.BlockDefinition)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := PhysicalEraseUnstarted([]lifecycle.BlockEvidence{install}, nil, install.Object); err != nil || !ok {
		t.Fatalf("retention omitted: %t %v", ok, err)
	}
	if ok, _ := PhysicalEraseUnstarted([]lifecycle.BlockEvidence{install, policy}, nil, install.Object); ok {
		t.Fatal("unexpected destructive policy accepted")
	}
}
