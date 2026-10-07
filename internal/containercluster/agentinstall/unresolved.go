package agentinstall

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Unresolved says why an observation of this cluster's agent media proved
// nothing, from the evidence it recorded: evidence this request's own that
// none of the verb's checks accept is named by the check that decided. Empty
// evidence, evidence for another request and evidence that does not decode are
// left to the engine's general reason.
func (MediaCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeMediaRequest(block.Request)
	if err != nil || len(evidence) == 0 {
		return lifecycle.Unresolved{}, false
	}
	if _, err := decodeMediaEvidence(evidence, block.RequestDigest); err != nil {
		return lifecycle.Unresolved{}, false
	}
	effect, refused := mediaEffect(verb, evidence, request, block.RequestDigest)
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the agent media of ContainerCluster "+block.Object, lifecycle.PlacedOn(request.Placement), refused)
}

// Unresolved says why an observation of this cluster's installation proved
// nothing, from the evidence it recorded: evidence this request's own that
// none of the verb's checks accept is named by the check that decided. Empty
// evidence, evidence for another request and evidence that does not decode are
// left to the engine's general reason.
func (InstallCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeInstallRequest(block.Request)
	if err != nil || len(evidence) == 0 {
		return lifecycle.Unresolved{}, false
	}
	if _, err := decodeInstallEvidence(evidence, block.RequestDigest); err != nil {
		return lifecycle.Unresolved{}, false
	}
	effect, refused := installEffect(verb, evidence, request, block.RequestDigest)
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the installation of ContainerCluster "+block.Object, lifecycle.PlacedOn(request.Placement), refused)
}
