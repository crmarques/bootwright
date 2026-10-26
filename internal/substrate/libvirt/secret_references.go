package libvirt

import "github.com/crmarques/bootwright/internal/reconciliation"

func (HostCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeHostRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (MachineCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}
