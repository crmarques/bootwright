package baremetal

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

func (MachineCapability) ProveBinding(_ context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return err
	}
	return request.Placement.ProveHostPin(contextName, material)
}

func (MachineCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (MachineCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.Placement.SecretClasses().
		Add(secrets.AccessCredential, request.Controller.CredentialsRef).
		Add(secrets.TrustAnchor, request.Controller.TrustBundleRef), nil
}
