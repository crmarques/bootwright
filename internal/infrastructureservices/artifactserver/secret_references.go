package artifactserver

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.secretReferences(), nil
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	classes := request.Placement.SecretClasses().Merge(request.ImageSource.SecretClasses())
	if request.TLS != nil {
		classes.Add(secrets.EffectMaterial, request.TLS.Secret)
	}
	return classes, nil
}
