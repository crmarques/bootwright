package artifactserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

type certificateOptions struct {
	ips        []string
	dnsNames   []string
	notBefore  time.Time
	notAfter   time.Time
	isCA       bool
	clientOnly bool
}

func issue(t *testing.T, options certificateOptions) (secrets.Material, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(2026),
		Subject:      pkix.Name{CommonName: "artifacts.lab.example.test"},
		NotBefore:    options.notBefore,
		NotAfter:     options.notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     options.dnsNames,
	}
	if options.isCA {
		template.IsCA = true
		template.BasicConstraintsValid = true
		template.KeyUsage |= x509.KeyUsageCertSign
	}
	if options.clientOnly {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	for _, value := range options.ips {
		template.IPAddresses = append(template.IPAddresses, net.ParseIP(value))
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	material := secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		secrets.PrivateKeyPart:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}),
	})
	digest := sha256.Sum256(der)
	return material, hex.EncodeToString(digest[:])
}

func validOptions() certificateOptions {
	return certificateOptions{
		ips:       []string{"192.0.2.1"},
		dnsNames:  []string{"artifacts.lab.example.test"},
		notBefore: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		notAfter:  time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

var testMoment = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

func TestServingCertificateCoversEveryServedAddress(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	certificate, err := ValidateServingCertificate(material, []string{"192.0.2.1"}, testMoment)
	if err != nil || certificate.Fingerprint != fingerprint {
		t.Fatalf("fingerprint = %q (%v), want %q", certificate.Fingerprint, err, fingerprint)
	}
	if _, err := ValidateServingCertificate(material, []string{"192.0.2.1", "artifacts.lab.example.test"}, testMoment); err != nil {
		t.Fatal("a covered DNS name was rejected:", err)
	}
	_, err = ValidateServingCertificate(material, []string{"192.0.2.9"}, testMoment)
	reported := diagnostics.Of(err)
	if err == nil || len(reported) != 1 || reported[0].Code != "secret.part" || !strings.Contains(reported[0].Remediation, "192.0.2.9") {
		t.Fatalf("an uncovered address was accepted: %+v", reported)
	}
}

func TestServingCertificateRefusesUnusableMaterial(t *testing.T) {
	expired := validOptions()
	expired.notBefore, expired.notAfter = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	future := validOptions()
	future.notBefore, future.notAfter = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	authority := validOptions()
	authority.isCA = true
	client := validOptions()
	client.clientOnly = true
	for name, options := range map[string]certificateOptions{
		"expired": expired, "not yet valid": future, "certificate authority": authority, "client only": client,
	} {
		t.Run(name, func(t *testing.T) {
			material, _ := issue(t, options)
			if _, err := ValidateServingCertificate(material, []string{"192.0.2.1"}, testMoment); err == nil {
				t.Fatal("unusable serving material was accepted")
			}
		})
	}
}

func TestServingCertificateRefusesMalformedOrMismatchedParts(t *testing.T) {
	material, _ := issue(t, validOptions())
	other, _ := issue(t, validOptions())
	certificate, _ := material.Part(secrets.CertificatePart)
	key, _ := material.Part(secrets.PrivateKeyPart)
	otherKey, _ := other.Part(secrets.PrivateKeyPart)
	cases := map[string]secrets.Material{
		"no certificate": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: key}),
		"no private key": secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: certificate}),
		"mismatched key": secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: certificate, secrets.PrivateKeyPart: otherKey}),
		"not pem":        secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte("not a certificate"), secrets.PrivateKeyPart: key}),
		"oversized": secrets.NewMaterial(map[secrets.Part][]byte{
			secrets.CertificatePart: make([]byte, maxCertificateBytes+1), secrets.PrivateKeyPart: key,
		}),
	}
	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateServingCertificate(candidate, []string{"192.0.2.1"}, testMoment); err == nil {
				t.Fatal("unusable serving material was accepted")
			}
		})
	}
}

// A refusal must explain the unmet condition without ever echoing material.
func TestServingCertificateFailuresDiscloseNoMaterial(t *testing.T) {
	canary := []byte("-----BEGIN PRIVATE KEY-----\nCANARYCANARYCANARY\n-----END PRIVATE KEY-----\n")
	material := secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: canary, secrets.PrivateKeyPart: canary,
	})
	_, err := ValidateServingCertificate(material, []string{"192.0.2.1"}, testMoment)
	if err == nil {
		t.Fatal("canary material was accepted")
	}
	for _, reported := range diagnostics.Of(err) {
		for _, forbidden := range []string{"CANARY", "BEGIN PRIVATE KEY"} {
			if strings.Contains(reported.Message+reported.Remediation, forbidden) {
				t.Fatalf("a diagnostic disclosed material: %+v", reported)
			}
		}
	}
}

func TestCanonicalAddressNormalizesLiterals(t *testing.T) {
	for value, want := range map[string]string{
		"192.0.2.1":    "192.0.2.1",
		"2001:db8::1":  "2001:db8::1",
		"2001:DB8::1":  "2001:db8::1",
		"example.test": "example.test",
	} {
		if got := CanonicalAddress(value); got != want {
			t.Fatalf("CanonicalAddress(%q) = %q, want %q", value, got, want)
		}
	}
}

func issueRSA(t *testing.T, bits int) secrets.Material {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	options := validOptions()
	template := x509.Certificate{
		SerialNumber: big.NewInt(2026), Subject: pkix.Name{CommonName: "artifacts.lab.example.test"},
		NotBefore: options.notBefore, NotAfter: options.notAfter, DNSNames: options.dnsNames, IPAddresses: []net.IP{net.ParseIP("192.0.2.1")},
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		secrets.PrivateKeyPart:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}),
	})
}

// A serving key with fewer than 2048 RSA bits refuses, naming its size and the
// certificates that replace it.
func TestTLSServingKeysUnder2048BitsRefuse(t *testing.T) {
	_, err := ValidateServingCertificate(issueRSA(t, 1024), []string{"192.0.2.1"}, testMoment)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "secret.part" || reported[0].Message != "the serving certificate's RSA key has 1024 bits; a serving key needs at least 2048" ||
		reported[0].Remediation != "replace the tlsCertificate Secret with an RSA-2048 or P-256 certificate" {
		t.Fatalf("an RSA-1024 serving key: %+v", reported)
	}
	if _, err := ValidateServingCertificate(issueRSA(t, 2048), []string{"192.0.2.1"}, testMoment); err != nil {
		t.Fatalf("an RSA-2048 serving key was refused: %+v", diagnostics.Of(err))
	}
}
