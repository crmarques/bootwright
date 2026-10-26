package artifactserver

import "github.com/crmarques/bootwright/internal/reconciliation"

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.secretReferences(), nil
}
