package installation

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
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

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.secretClasses(), nil
}

func (r Request) secretClasses() secrets.Classes {
	classes := r.Placement.SecretClasses().
		Add(controllerCredentialClass(r.Target.Substrate), r.Target.Controller.CredentialsRef).
		Add(secrets.TrustAnchor, r.Target.Controller.TrustBundleRef, r.TLSCertificateRef).
		Add(secrets.EffectMaterial, r.FleetKeyRef, r.Target.HostKeyRef)
	if r.DiskEncryption != nil {
		classes.Add(secrets.EffectMaterial, r.DiskEncryption.RecoveryPassphraseRef)
	}
	if r.Registration != nil {
		classes.Add(secrets.EffectMaterial, r.Registration.OrganizationRef, r.Registration.ActivationKeyRef)
		if r.Registration.Satellite != nil {
			classes.Add(secrets.TrustAnchor, r.Registration.Satellite.TrustBundleRef)
		}
	}
	for _, publication := range []*Publication{r.Image, r.Tree, r.Private} {
		if publication != nil {
			classes.Add(secrets.TrustAnchor, publication.CertificateRef)
		}
	}
	return classes
}

func controllerCredentialClass(target string) secrets.Class {
	if substrate.ControllerCredentialIsAccess(target) {
		return secrets.AccessCredential
	}
	return secrets.EffectMaterial
}

func (EraseCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeEraseRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (EraseCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeEraseRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.Install.secretClasses(), nil
}

func (TreeCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeTreeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.SecretReferences(), nil
}

func (TreeCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeTreeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return request.Placement.SecretClasses().Add(secrets.TrustAnchor, request.Publication.CertificateRef), nil
}
