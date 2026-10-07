package artifactserver

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Unresolved says why an observation of this artifact server proved nothing,
// from the evidence it recorded: evidence this request's own that none of the
// verb's checks accept is named by the check that decided. The bound
// certificate's fingerprint is not recorded, so it is read as none: evidence
// an apply reads as unknown is refused before any listener's certificate is
// compared, by its postcondition, its unit or its content root, so the
// fingerprint changes neither whether the effect is unknown nor the refusal
// that decided it. Empty evidence, evidence for another request and evidence
// that does not decode are left to the engine's general reason.
func (Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeRequest(block.Request)
	if err != nil || len(evidence) == 0 {
		return lifecycle.Unresolved{}, false
	}
	observed, err := decodeEvidence(evidence)
	if err != nil || observed.Request != block.RequestDigest {
		return lifecycle.Unresolved{}, false
	}
	effect, refused := serverEffect(verb, evidence, request, block.RequestDigest, "")
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "ArtifactServer "+block.Object, lifecycle.PlacedOn(request.Placement), refused)
}
