//go:build !linux || !amd64

package bundlelocal

import (
	"context"
	"crypto/x509"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func qualifiedSystemRoots(ctx context.Context) (*x509.CertPool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, diagnostics.NewFailure("controller.unsupported", "reading the system trust store requires Linux/amd64", "")
}
