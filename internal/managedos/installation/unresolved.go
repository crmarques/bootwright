package installation

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Unresolved says why an observation of this installation proved nothing,
// from the evidence it recorded: evidence this request's own that none of the
// verb's checks accept is named by the check that decided, against the marker
// the frozen request and its digest derive. Empty evidence, evidence for
// another request and evidence that does not decode are left to the engine's
// general reason.
func (Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeRequest(block.Request)
	if err != nil || len(evidence) == 0 {
		return lifecycle.Unresolved{}, false
	}
	if _, err := decodeEvidence(evidence, block.RequestDigest); err != nil {
		return lifecycle.Unresolved{}, false
	}
	marker, err := MarkerFor(request, block.RequestDigest)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	effect, refused := installationEffect(verb, evidence, request, block.RequestDigest, string(marker))
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the installation of Machine "+block.Object, lifecycle.PlacedOn(request.Placement), refused)
}
