package ceph

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func evaluateExecutionEvidence(data []byte, r Request, execution lifecycle.Execution, removal bool) (reconciliation.EffectState, error) {
	effect, err := EvaluateEvidence(data, r, execution.Block.RequestDigest, removal)
	if err != nil || effect != reconciliation.EffectCompleted || removal || r.Action != "service" || r.Service == nil {
		return effect, err
	}
	for _, binding := range r.Service.Bindings {
		if binding.ServiceRef != "mgmt-gateway" || binding.Path != "/spec/ssl_cert" {
			continue
		}
		leaf, ok := boundLeaf(execution.Material[binding.SecretRef])
		if !ok {
			return reconciliation.EffectUnknown, evidenceFailure()
		}
		evidence, err := decodeEvidence(data)
		if err != nil {
			return reconciliation.EffectUnknown, evidenceFailure()
		}
		sum := sha256.Sum256(leaf.Raw)
		if evidence.Endpoint.CertificateSHA256 != hex.EncodeToString(sum[:]) {
			return reconciliation.EffectUnknown, evidenceFailure()
		}
	}
	return effect, nil
}
