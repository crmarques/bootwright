package networking

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return lifecycle.SecretNames(request.Placement.SecretReferences()...), nil
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.Placement.SecretClasses(), nil
}
