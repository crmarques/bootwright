package ceph

import "github.com/crmarques/bootwright/internal/reconciliation"

func (Capability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (HostCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeHostRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (ImageCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeImageRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (ExportCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeExportRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (SMBCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeSMBRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (ReplicationCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	_, err := DecodeReplicationRequest(block.Request)
	return nil, err
}

func (PhysicalRetirementCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	_, err := DecodePhysicalRetirementRequest(block.Request)
	return nil, err
}
