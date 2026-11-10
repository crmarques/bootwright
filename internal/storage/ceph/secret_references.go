package ceph

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

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

func (ConsumerCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	request, err := DecodeConsumerRequest(block.Request)
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

func (s ClusterScope) secretClasses() secrets.Classes {
	classes := secrets.Classes{}.
		Add(secrets.EffectMaterial, s.ClusterKeyRef).
		Add(secrets.AccessCredential, s.RegistryRef).
		Add(secrets.TrustAnchor, s.RegistryTrustBundleRef)
	for _, host := range s.Hosts {
		classes.Add(secrets.EffectMaterial, host.SSHKeyRef, host.HostKeyRef, host.KnownHostsRef)
	}
	if s.Packages != nil {
		classes.Add(secrets.TrustAnchor, s.Packages.PackageRepositoryTrustBundleRef)
		for _, source := range s.Packages.Sources {
			classes.Add(secrets.TrustAnchor, source.CertificateRef)
		}
	}
	return classes
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	r, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	classes := scopeOf(r).secretClasses()
	if r.Gateway != nil {
		for _, binding := range r.Gateway.Bindings {
			classes.Add(secrets.EffectMaterial, binding.SecretRef)
		}
	}
	if r.Service != nil {
		for _, binding := range r.Service.Bindings {
			classes.Add(secrets.EffectMaterial, binding.SecretRef)
		}
	}
	if r.ObjectBucket != nil {
		classes.Add(secrets.TrustAnchor, r.ObjectBucket.CertificateRef)
	}
	return classes, nil
}

func (HostCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	r, err := DecodeHostRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return r.Cluster.secretClasses(), nil
}

func (ImageCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	r, err := DecodeImageRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return secrets.Classes{}.Add(secrets.AccessCredential, r.RegistryRef).Add(secrets.TrustAnchor, r.RegistryTrustBundleRef), nil
}

func (ConsumerCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	r, err := DecodeConsumerRequest(block.Request)
	if err != nil {
		return nil, err
	}
	classes := secrets.Classes{}
	if r.Gateway != nil {
		classes.Add(secrets.EffectMaterial, r.Gateway.CertificateRef)
	}
	return classes, nil
}

func (SMBCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	r, err := DecodeSMBRequest(block.Request)
	if err != nil {
		return nil, err
	}
	classes := secrets.Classes{}
	for _, binding := range r.Bindings {
		classes.Add(secrets.EffectMaterial, binding.SecretRef)
	}
	return classes, nil
}

func (ReplicationCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	_, err := DecodeReplicationRequest(block.Request)
	return secrets.Classes{}, err
}

func (PhysicalRetirementCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	_, err := DecodePhysicalRetirementRequest(block.Request)
	return secrets.Classes{}, err
}
