package clients

import "github.com/crmarques/bootwright/internal/reconciliation"

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	_, err := DecodeRequest(block.Request)
	return nil, err
}
