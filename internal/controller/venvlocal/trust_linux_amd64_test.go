//go:build linux && amd64

package venvlocal

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func onePool(t *testing.T) *x509.CertPool {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fallback"}, IsCA: true, BasicConstraintsValid: true,
		NotAfter: time.Now().Add(time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(1)}, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	return pool
}

func TestAnAbsentFedoraBundleFallsBackToTheSystemPool(t *testing.T) {
	want := onePool(t)
	bundle := filepath.Join(t.TempDir(), "pki", "tls-ca-bundle.pem")
	asked := 0
	pool, err := trustedRoots(context.Background(), bundle, func() (*x509.CertPool, error) {
		asked++
		return want, nil
	})
	if err != nil || asked != 1 || !pool.Equal(want) {
		t.Fatalf("an absent bundle gave %v, %v after %d fallbacks", pool, err, asked)
	}
}

func TestAHostWithNoTrustAtAllRefusesNamingTheBundleAndARemedy(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "tls-ca-bundle.pem")
	for name, fallback := range map[string]func() (*x509.CertPool, error){
		"an empty pool": func() (*x509.CertPool, error) { return x509.NewCertPool(), nil },
		"an error":      func() (*x509.CertPool, error) { return nil, errors.New("no roots") },
	} {
		_, err := trustedRoots(context.Background(), bundle, fallback)
		var scoped *prerequisites.ScopedFailure
		if !errors.As(err, &scoped) || !strings.Contains(scoped.Message, bundle+" is absent") || strings.Contains(scoped.Message, "unsafe ownership") ||
			!strings.Contains(scoped.Correction, "--wheel-mirror") || strings.Contains(scoped.Correction, "Python interpreter") {
			t.Errorf("%s refused with %#v", name, err)
		}
	}
}

func TestAnUnsafeBundleIsNeverReplacedByTheSystemPool(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "tls-ca-bundle.pem")
	if err := os.WriteFile(bundle, []byte("not a certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := trustedRoots(context.Background(), bundle, func() (*x509.CertPool, error) {
		t.Error("the system pool replaced a bundle that exists")
		return x509.NewCertPool(), nil
	})
	var scoped *prerequisites.ScopedFailure
	if !errors.As(err, &scoped) || !strings.Contains(scoped.Message, "unsafe ownership") {
		t.Fatalf("a bundle outside a root-owned directory was accepted: %v", err)
	}
}
