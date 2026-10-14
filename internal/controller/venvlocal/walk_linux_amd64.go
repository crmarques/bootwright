//go:build linux && amd64

package venvlocal

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const maxTrustBytes = 8 << 20

// ownedExecutable resolves path to an executable regular file a trusted
// account owns below directories it owns, none writable by a group or other
// account, as privilege.QualifiedSudo does for sudo.
func (a *Adapter) ownedExecutable(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", failure(path + " is not installed")
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || !a.owned(info) {
		return "", failure(resolved + " is not an executable regular file root owns without group or other write")
	}
	if err := a.ownedDirectory(filepath.Dir(resolved)); err != nil {
		return "", err
	}
	return resolved, nil
}

// ownedDirectory holds a directory and every parent to the same rule.
func (a *Adapter) ownedDirectory(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(resolved) {
		return failure(path + " is not an installed directory")
	}
	for directory := resolved; ; directory = filepath.Dir(directory) {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || !a.owned(info) {
			return failure(directory + " is writable by an account other than root, or not owned by root")
		}
		if directory == "/" || directory == a.top {
			return nil
		}
	}
}

func (a *Adapter) owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && a.trusted(stat.Uid) && info.Mode().Perm()&0o022 == 0
}

const fedoraTrustBundle = "/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"

// systemRoots reads the Fedora/RHEL bundle through root-owned directories, never
// SSL_CERT_*; off the family it falls back to Go's pool, the lock's SHA-256 holds.
func systemRoots(ctx context.Context) (*x509.CertPool, error) {
	return trustedRoots(ctx, fedoraTrustBundle, x509.SystemCertPool)
}

func trustedRoots(ctx context.Context, bundle string, fallback func() (*x509.CertPool, error)) (*x509.CertPool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(bundle); errors.Is(err, fs.ErrNotExist) {
		return fallbackRoots(bundle, fallback)
	}
	root := &Adapter{top: "/", trusted: func(uid uint32) bool { return uid == 0 }}
	if err := root.ownedDirectory(filepath.Dir(bundle)); err != nil {
		return nil, trustFailure("the system TLS trust directory has unsafe ownership or mode", "Restore "+filepath.Dir(bundle)+" as a root-owned directory without group or other write")
	}
	file, err := os.OpenFile(bundle, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, trustFailure("the system TLS trust bundle is unavailable", "Restore "+bundle+" with update-ca-trust")
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || !root.owned(before) || before.Size() <= 0 || before.Size() > maxTrustBytes {
		return nil, trustFailure("the system TLS trust bundle has unsafe ownership, mode or size", "Restore "+bundle+" as a root-owned file without group or other write with update-ca-trust")
	}
	data, err := io.ReadAll(io.LimitReader(file, before.Size()+1))
	if err != nil || int64(len(data)) != before.Size() {
		return nil, trustFailure("the system TLS trust bundle changed while being read", "Repeat this command once nothing rewrites "+bundle)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, trustFailure("the system TLS trust bundle holds no usable certificate", "Restore "+bundle+" with update-ca-trust")
	}
	return pool, nil
}

func fallbackRoots(bundle string, system func() (*x509.CertPool, error)) (*x509.CertPool, error) {
	pool, err := system()
	if err != nil || pool == nil || pool.Equal(x509.NewCertPool()) {
		return nil, trustFailure("this host has no system TLS trust bundle: "+bundle+" is absent and the system certificate pool is empty",
			"Install the host's CA certificates package, or run this on a host that has one, or pass --wheel-mirror file:///dir")
	}
	return pool, nil
}

func trustFailure(message, correction string) error {
	return &prerequisites.ScopedFailure{Message: message, Correction: correction}
}
