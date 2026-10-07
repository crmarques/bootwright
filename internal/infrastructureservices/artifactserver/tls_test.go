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

const testSecret = "artifact-server-tls"

func TestServingCertificateCoversEveryServedAddress(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	certificate, err := ValidateServingCertificate(material, testSecret, testContext, []string{"192.0.2.1"}, testMoment)
	if err != nil || certificate.Fingerprint != fingerprint {
		t.Fatalf("fingerprint = %q (%v), want %q", certificate.Fingerprint, err, fingerprint)
	}
	if _, err := ValidateServingCertificate(material, testSecret, testContext, []string{"192.0.2.1", "artifacts.lab.example.test"}, testMoment); err != nil {
		t.Fatal("a covered DNS name was rejected:", err)
	}
	_, err = ValidateServingCertificate(material, testSecret, testContext, []string{"192.0.2.9"}, testMoment)
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
			if _, err := ValidateServingCertificate(material, testSecret, testContext, []string{"192.0.2.1"}, testMoment); err == nil {
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
			if _, err := ValidateServingCertificate(candidate, testSecret, testContext, []string{"192.0.2.1"}, testMoment); err == nil {
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
	_, err := ValidateServingCertificate(material, testSecret, testContext, []string{"192.0.2.1"}, testMoment)
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
	_, err := ValidateServingCertificate(issueRSA(t, 1024), testSecret, testContext, []string{"192.0.2.1"}, testMoment)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "secret.part" || reported[0].Message != "the serving certificate's RSA key has 1024 bits; a serving key needs at least 2048" ||
		reported[0].Remediation != "replace the tlsCertificate Secret with an RSA-2048 or P-256 certificate, stored with bootwright secret set --name artifact-server-tls --certificate-file <path> --private-key-file <path> --context lab; then destroy this apply with bootwright destroy --context lab and apply again" {
		t.Fatalf("an RSA-1024 serving key: %+v", reported)
	}
	if _, err := ValidateServingCertificate(issueRSA(t, 2048), testSecret, testContext, []string{"192.0.2.1"}, testMoment); err != nil {
		t.Fatalf("an RSA-2048 serving key was refused: %+v", diagnostics.Of(err))
	}
}

// Every refusal of the bound material names the Secret it read, as the object
// secret check and secret set act on, and a remedy that stores usable material
// in this context and then leaves the apply the only way that uses it: the
// operation keeps the version it bound, so it is destroyed and applied again.
// No refusal carries material.
func TestServingCertificateRefusalsNameTheSecretAndTheirRemedy(t *testing.T) {
	material, _ := issue(t, validOptions())
	other, _ := issue(t, validOptions())
	certificate, _ := material.Part(secrets.CertificatePart)
	key, _ := material.Part(secrets.PrivateKeyPart)
	otherKey, _ := other.Part(secrets.PrivateKeyPart)
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("CANARY")})
	expired, authority, client := validOptions(), validOptions(), validOptions()
	expired.notBefore, expired.notAfter = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	authority.isCA, client.clientOnly = true, true
	issued := func(options certificateOptions) secrets.Material {
		candidate, _ := issue(t, options)
		return candidate
	}
	for name, candidate := range map[string]secrets.Material{
		"no certificate":  secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: key}),
		"no private key":  secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: certificate}),
		"oversized":       secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: make([]byte, maxCertificateBytes+1), secrets.PrivateKeyPart: key}),
		"not pem":         secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte("CANARY"), secrets.PrivateKeyPart: key}),
		"too many blocks": secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte(strings.Repeat(string(block), maxPEMBlocks+1)), secrets.PrivateKeyPart: key}),
		"mismatched key":  secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: certificate, secrets.PrivateKeyPart: otherKey}),
		"expired":         issued(expired),
		"authority":       issued(authority),
		"client only":     issued(client),
		"rsa-1024":        issueRSA(t, 1024),
		"uncovered":       material,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateServingCertificate(candidate, testSecret, testContext, []string{"192.0.2.1", "192.0.2.9"}, testMoment)
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "secret.part" {
				t.Fatalf("refusal = %#v, want one secret.part", reported)
			}
			got := reported[0]
			if got.Object == nil || got.Object.Kind != "Secret" || got.Object.Name != testSecret {
				t.Fatalf("object = %#v, want Secret/%s", got.Object, testSecret)
			}
			if !strings.Contains(got.Remediation, "bootwright secret set --name "+testSecret+" ") || !strings.Contains(got.Remediation, "--context "+testContext) ||
				!strings.HasSuffix(got.Remediation, "then destroy this apply with bootwright destroy --context "+testContext+" and apply again") {
				t.Fatalf("remediation = %q, want the set command in this context and the destroy exit", got.Remediation)
			}
			for _, forbidden := range []string{"CANARY", "BEGIN"} {
				if strings.Contains(got.Message+got.Remediation, forbidden) {
					t.Fatalf("a refusal disclosed material: %#v", got)
				}
			}
		})
	}
	_, err := ValidateServingCertificate(material, testSecret, testContext, []string{"192.0.2.9"}, testMoment)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || !strings.Contains(reported[0].Message, "192.0.2.9") ||
		reported[0].Remediation != "store a certificate that names 192.0.2.9 with bootwright secret set --name artifact-server-tls --certificate-file <path> --private-key-file <path> --context lab, "+
			"or, for a generated Secret, add 192.0.2.9 to spec.source.generated.ipAddresses, import the change with bootwright context update --name lab --input-dir <dir> "+
			"and run bootwright secret generate --name artifact-server-tls --context lab; then destroy this apply with bootwright destroy --context lab and apply again" {
		t.Fatalf("an uncovered IP address = %#v", reported)
	}
	_, err = ValidateServingCertificate(material, testSecret, testContext, []string{"artifacts.other.example.test"}, testMoment)
	if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Remediation, "add artifacts.other.example.test to spec.source.generated.dnsNames,") {
		t.Fatalf("an uncovered name = %#v, want its dnsNames remedy", reported)
	}
}
