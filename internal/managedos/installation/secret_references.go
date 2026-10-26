package installation

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return referencedSecrets(request), nil
}

func referencedSecrets(request Request) []string {
	if request.Private != nil {
		return lifecycle.SecretNames(append(request.SecretReferences(), request.Private.CertificateRef)...)
	}
	return request.SecretReferences()
}

func (EraseCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeEraseRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (TreeCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeTreeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}
