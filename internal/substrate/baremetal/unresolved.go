package baremetal

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Unresolved says why an apply's observation of this machine proved nothing,
// from the evidence it recorded: evidence this request's own that the presence
// proof refuses is named by that refusal. A removal always completes, so it
// never leaves the block unknown from evidence. Empty evidence, evidence for
// another request and evidence that does not decode are left to the engine's
// general reason.
func (MachineCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeRequest(block.Request)
	if err != nil || len(evidence) == 0 {
		return lifecycle.Unresolved{}, false
	}
	if _, err := decodeEvidence(evidence, block.RequestDigest); err != nil {
		return lifecycle.Unresolved{}, false
	}
	effect, refused := machineEffect(verb, evidence, request, block.RequestDigest)
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "Machine "+block.Object, lifecycle.PlacedOn(request.Placement), refused)
}
