package clients

import "github.com/crmarques/bootwright/internal/reconciliation"

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil || request.MirrorTrust == nil {
		return nil, err
	}
	return []string{request.MirrorTrust.TrustBundleRef}, nil
}
