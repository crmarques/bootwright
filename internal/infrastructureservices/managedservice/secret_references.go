package managedservice

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func (c Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeRequest(block.Request, c.definition.Version)
	if err != nil {
		return nil, err
	}
	return lifecycle.SecretNames(request.SecretReferences()...), nil
}

func (c Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeRequest(block.Request, c.definition.Version)
	if err != nil {
		return nil, err
	}
	classes := request.Placement.SecretClasses().Merge(request.ImageSource.SecretClasses())
	if request.TLS != nil {
		classes = classes.Add(secrets.EffectMaterial, request.TLS.Secret)
	}
	return classes.Add(secrets.EffectMaterial, request.CredentialsRef), nil
}
