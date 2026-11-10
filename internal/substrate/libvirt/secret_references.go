package libvirt

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

func (HostCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeHostRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (HostCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeHostRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.Placement.SecretClasses(), nil
}

func (MachineCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (MachineCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.Placement.SecretClasses().
		Add(secrets.EffectMaterial, request.Controller.CredentialsRef, request.Controller.CertificateRef).
		Merge(request.Controller.ImageSource.SecretClasses()), nil
}
