package clients

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

func (r Request) secretReferences() []string {
	if r.MirrorTrust == nil {
		return nil
	}
	return []string{r.MirrorTrust.TrustBundleRef}
}

func (Capability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	classes := secrets.Classes{}
	if request.MirrorTrust != nil {
		classes.Add(secrets.TrustAnchor, request.MirrorTrust.TrustBundleRef)
	}
	return classes, nil
}
