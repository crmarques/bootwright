//go:build linux && amd64

package bundlelocal

import (
	"context"
	"crypto/x509"
	"io"
	"os"
	"syscall"
)

const maxTrustBytes = 8 << 20

// Both qualified OS releases publish the system TLS trust bundle here. Open
// each fixed directory through an owned root and reject writable ownership
// boundaries. SSL_CERT_FILE/DIR and other ambient trust overrides are unused.
func qualifiedSystemRoots(ctx context.Context) (*x509.CertPool, error) {
	data, err := qualifiedTrustPEM(ctx)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, bundleFailure("qualified system TLS trust bundle contains no usable certificates")
	}
	return pool, nil
}

func qualifiedTrustPEM(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot("/")
	if err != nil {
		return nil, bundleFailure("qualified system TLS trust cannot be opened")
	}
	defer func() { root.Close() }()
	for _, component := range []string{".", "etc", "pki", "ca-trust", "extracted", "pem"} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		child, err := root.OpenRoot(component)
		if err != nil {
			return nil, bundleFailure("qualified system TLS trust directory is unavailable")
		}
		info, err := child.Stat(".")
		if err != nil || !trustedInfo(info, true) {
			child.Close()
			return nil, bundleFailure("qualified system TLS trust directory has unsafe ownership or mode")
		}
		root.Close()
		root = child
	}
	file, err := root.Open("tls-ca-bundle.pem")
	if err != nil {
		return nil, bundleFailure("qualified system TLS trust bundle is unavailable")
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !trustedInfo(before, false) || before.Size() <= 0 || before.Size() > maxTrustBytes {
		return nil, bundleFailure("qualified system TLS trust bundle has unsafe ownership, mode or size")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, before.Size()+1))
	after, statErr := file.Stat()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if readErr != nil || statErr != nil || int64(len(data)) != before.Size() || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || !trustedInfo(after, false) {
		return nil, bundleFailure("qualified system TLS trust bundle changed while being read")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, bundleFailure("qualified system TLS trust bundle contains no usable certificates")
	}
	return data, nil
}

func trustedInfo(info os.FileInfo, directory bool) bool {
	if info == nil || info.IsDir() != directory || !directory && !info.Mode().IsRegular() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && stat.Mode&0022 == 0
}
