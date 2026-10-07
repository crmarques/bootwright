package artifactserver

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// ProveBinding proves the serving certificate a fresh apply just bound for
// block, before the operation registers, so unusable material refuses while
// nothing is held instead of after registration.
func (c Capability) ProveBinding(ctx context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return err
	}
	if request.TLS == nil {
		return nil
	}
	bound, err := BoundServingMaterial(material, request.TLS.Secret, contextName)
	if err != nil {
		return err
	}
	return ProveServingCertificate(bound, request.TLS.Secret, contextName, request.servedAddresses("https"), c.now())
}
