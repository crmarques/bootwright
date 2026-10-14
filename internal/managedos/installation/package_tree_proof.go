package installation

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// ProvedPackageTree returns the original apply's immutable DVD publication.
// Consumers may use another declared listener of the same server, but may not
// replace its media pin, profile or owned publication path.
func ProvedPackageTree(proofs []lifecycle.BlockEvidence, profile string) (TreeRequest, error) {
	var selected *TreeRequest
	for _, proof := range proofs {
		if proof.Kind != TreeKind || proof.Object != profile || proof.Implementation != TreeImplementation {
			continue
		}
		if selected != nil || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone {
			return TreeRequest{}, refusal("lifecycle.state", "package acquisition requires one completed DVD publication", "")
		}
		request, err := DecodeTreeRequest(proof.Block.Request)
		digest, digestErr := reconciliation.RequestDigest(proof.Block.BlockDefinition)
		if err != nil || digestErr != nil || proof.Block.RequestDigest != digest || request.Identity.Profile != profile || proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation {
			return TreeRequest{}, refusal("lifecycle.state", "the DVD proof does not match its original frozen publication", "")
		}
		evidence, err := decodeTreeEvidence(proof.Evidence, proof.Block.RequestDigest)
		if err != nil || treeEffect(evidence, request, false) != reconciliation.EffectCompleted {
			return TreeRequest{}, refusal("lifecycle.state", "the original DVD publication is not proved ready", "")
		}
		selected = &request
	}
	if selected == nil {
		return TreeRequest{}, refusal("lifecycle.state", "package acquisition has no original apply DVD publication", "restore the retained apply evidence; do not substitute another media source")
	}
	return *selected, nil
}
