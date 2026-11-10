package agentinstall

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

func (MediaCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeMediaRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (MediaCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeMediaRequest(block.Request)
	if err != nil {
		return nil, err
	}
	classes := request.Placement.SecretClasses().
		Add(secrets.EffectMaterial, request.PullSecretRef, request.SSHKeyRef).
		Add(secrets.TrustAnchor, request.TLSCertificateRef).
		Add(secrets.TrustAnchor, request.TrustBundleRefs...)
	if request.InstallProxy != nil {
		classes.Add(secrets.EffectMaterial, request.InstallProxy.CredentialsRef)
	}
	if request.RegistryMirror != nil {
		classes.Add(secrets.EffectMaterial, request.RegistryMirror.CredentialsRef)
	}
	return classes, nil
}

func (InstallCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeInstallRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (InstallCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeInstallRequest(block.Request)
	if err != nil {
		return nil, err
	}
	classes := request.Placement.SecretClasses().Add(secrets.TrustAnchor, request.TLSCertificateRef)
	for _, node := range request.Nodes {
		credential := secrets.EffectMaterial
		if substrate.ControllerCredentialIsAccess(node.Substrate) {
			credential = secrets.AccessCredential
		}
		classes.Add(credential, node.Controller.CredentialsRef).Add(secrets.TrustAnchor, node.Controller.TrustBundleRef)
	}
	return classes, nil
}
