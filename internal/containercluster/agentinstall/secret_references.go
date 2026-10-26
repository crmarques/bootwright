package agentinstall

import "github.com/crmarques/bootwright/internal/reconciliation"

func (MediaCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeMediaRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (InstallCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeInstallRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}
