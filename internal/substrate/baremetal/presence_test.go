package baremetal

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A proof is read only for a block this implementation owns, and then only
// when it is a complete presence proof of the block's own frozen request.
func TestAPresenceProofIsReadOnlyForAnOwnedBlocksOwnRequest(t *testing.T) {
	definition := planOf(t, reconciliation.Apply).Definitions[0]
	block := reconciliation.Block{BlockDefinition: definition, RequestDigest: provedDigest}
	good := proof(t, "uuid-1", "SN1")
	evidence, owned, err := ProvedPresence(block, good)
	if err != nil || !owned || evidence.UUID != "uuid-1" || evidence.Serial != "SN1" || !evidence.Postcondition {
		t.Fatalf("proof = %+v, %v (%v)", evidence, owned, err)
	}
	other := block
	other.Implementation = "another-implementation"
	if _, owned, err := ProvedPresence(other, good); owned || err != nil {
		t.Fatalf("another implementation's block was read: %v (%v)", owned, err)
	}
	for name, refused := range map[string]struct {
		block    reconciliation.Block
		evidence []byte
	}{
		"no frozen request": {func() reconciliation.Block { b := block; b.Request = nil; return b }(), good},
		"another object":    {func() reconciliation.Block { b := block; b.Object = "other"; return b }(), good},
		"another block":     {func() reconciliation.Block { b := block; b.ID = "machine-other"; return b }(), good},
		"no proof":          {block, encode(t, Evidence{Request: provedDigest})},
		"another request":   {block, encode(t, Evidence{Addresses: fixtureRequest(t).Addresses(), Postcondition: true, Power: "Off", Request: "another", UUID: "uuid-1"})},
	} {
		if _, owned, err := ProvedPresence(refused.block, refused.evidence); !owned || err == nil {
			t.Errorf("%s = owned %v (%v)", name, owned, err)
		}
	}
}
