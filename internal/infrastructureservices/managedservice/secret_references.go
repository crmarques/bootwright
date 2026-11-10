package managedservice

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func (c Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeRequest(block.Request, c.definition.Version)
	if err != nil {
		return nil, err
	}
	return lifecycle.SecretNames(append(request.Placement.SecretReferences(), request.ImageSource.SecretReferences()...)...), nil
}
