package installation

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/trust"
)

func ProvedHostKey(proofs []lifecycle.BlockEvidence, machine, address string) (trust.HostKey, error) {
	var selected *trust.HostKey
	for _, proof := range proofs {
		if proof.Kind != Kind || proof.Object != machine || proof.Implementation != Implementation {
			continue
		}
		if selected != nil || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone {
			return trust.HostKey{}, refusal("lifecycle.state", "dependent access has no unique completed installation identity", "")
		}
		frozen, err := DecodeRequest(proof.Block.Request)
		if err != nil || proof.Block.RequestDigest == "" || frozen.Identity.Object != machine || frozen.Address != address || proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation {
			return trust.HostKey{}, refusal("lifecycle.state", "installation proof does not match its frozen Machine request", "")
		}
		marker, err := MarkerFor(frozen, proof.Block.RequestDigest)
		if err != nil || ValidatePresence(proof.Evidence, frozen, proof.Block.RequestDigest, string(marker)) != nil {
			return trust.HostKey{}, refusal("lifecycle.state", "installation identity no longer proves its original apply request", "")
		}
		capturedAddress, line, err := HostKeyEvidence(proof.Evidence)
		if err != nil || capturedAddress != address {
			return trust.HostKey{}, refusal("lifecycle.state", "host address does not match its original installation proof", "")
		}
		key, err := trust.ParseAuthorizedKey(line)
		if err != nil {
			return trust.HostKey{}, refusal("lifecycle.state", "installation proof contains an invalid host key", "")
		}
		selected = &key
	}
	if selected == nil {
		return trust.HostKey{}, refusal("lifecycle.state", "dependent access requires its original apply's installed host-key proof", "restore the original apply records; a fresh guest key cannot replace the proved identity")
	}
	return *selected, nil
}
