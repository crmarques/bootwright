//go:build !linux || !amd64

package bundlelocal

import (
	"context"
	"crypto/x509"
)

func qualifiedSystemRoots(ctx context.Context) (*x509.CertPool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, unsupportedPlatform()
}
