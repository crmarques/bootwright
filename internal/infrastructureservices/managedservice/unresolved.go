package managedservice

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Unresolved says why an observation of this managed service proved nothing,
// from the evidence it recorded: evidence this request's own that none of the
// verb's checks accept is named by the check that decided. Empty evidence,
// evidence for another request and evidence that does not decode are left to
// the engine's general reason.
func (c Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeRequest(block.Request, c.definition.Version)
	if err != nil || len(evidence) == 0 {
		return lifecycle.Unresolved{}, false
	}
	observed, err := DecodeEvidence(evidence)
	if err != nil || observed.Request != block.RequestDigest {
		return lifecycle.Unresolved{}, false
	}
	effect, refused := serviceEffect(verb, evidence, request, block.RequestDigest)
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, block.Kind+" "+block.Object, lifecycle.PlacedOn(request.Placement), refused)
}
