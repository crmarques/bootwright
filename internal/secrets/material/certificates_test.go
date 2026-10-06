package material

import (
	"context"
	"crypto"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

// earlierRule signs a generated certificate as an earlier build did: from the
// generation second, not a day before it.
type earlierRule struct{ standardCryptography }

func (c earlierRule) CreateCertificate(template, parent *x509.Certificate, public crypto.PublicKey, private crypto.Signer) ([]byte, error) {
	template.NotBefore = template.NotBefore.Add(certificateBackdate)
	return c.standardCryptography.CreateCertificate(template, parent, public, private)
}

func generatedCertificate(t *testing.T, service *Service, declaration secrets.Declaration) (secrets.Material, *x509.Certificate) {
	t.Helper()
	value, err := service.Generate(context.Background(), declaration)
	if err != nil {
		t.Fatalf("Generate: %v", diagnostics.Of(err))
	}
	data := requiredPart(t, value, secrets.CertificatePart)
	defer clear(data)
	certificates, err := parseCertificates(data)
	if err != nil || len(certificates) != 1 {
		t.Fatal(err)
	}
	return value, certificates[0]
}

// A generated certificate starts 24 hours before its generation second, so a
// verifier whose clock trails accepts it, and ends validityDays after that
// second; one an earlier build started at that second stays current.
func TestGeneratedCertificatesStartADayEarlier(t *testing.T) {
	generated := generationTime.UTC().Truncate(time.Second)
	at := func(moment time.Time) Options {
		return Options{Random: cryptorand.Reader, Clock: func() time.Time { return moment }}
	}
	declaration := secrets.Declaration{Type: "tlsCertificate", Source: "generated", Generation: secrets.Generation{CommonName: "service.example", DNSNames: []string{"service.example"}, ValidityDays: 30}}
	value, certificate := generatedCertificate(t, New(nil, at(generated)), declaration)
	defer value.Clear()
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	for _, behind := range []time.Duration{30 * time.Minute, 3 * time.Hour} {
		if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots, DNSName: "service.example", CurrentTime: generated.Add(-behind)}); err != nil {
			t.Fatalf("a verifier %v behind refused the certificate: %v", behind, err)
		}
	}
	if !certificate.NotBefore.Equal(generated.Add(-24*time.Hour)) || !certificate.NotAfter.Equal(generated.AddDate(0, 0, 30)) {
		t.Fatalf("validity = %v to %v, want a day before %v to 30 days after it", certificate.NotBefore, certificate.NotAfter, generated)
	}
	if err := New(nil, at(generated.Add(-3*time.Hour))).Validate(context.Background(), declaration, value); err != nil {
		t.Fatalf("a validator 3h behind refused the certificate: %+v", diagnostics.Of(err))
	}

	oneDay := declaration
	oneDay.Generation.ValidityDays = 1
	short, _ := generatedCertificate(t, New(nil, at(generated)), oneDay)
	defer short.Clear()
	if err := New(nil, at(generated.Add(23*time.Hour))).Validate(context.Background(), oneDay, short); err != nil {
		t.Fatalf("a one-day certificate refused within its day: %+v", diagnostics.Of(err))
	}
	assertFailureCode(t, New(nil, at(generated.Add(24*time.Hour+time.Second))).Validate(context.Background(), oneDay, short), "secret.input")

	earlier := at(generated)
	earlier.Cryptography = earlierRule{}
	previous, certificate := generatedCertificate(t, New(nil, earlier), declaration)
	defer previous.Clear()
	if !certificate.NotBefore.Equal(generated) || !certificate.NotAfter.Equal(generated.AddDate(0, 0, 30)) {
		t.Fatalf("the earlier rule's validity = %v to %v", certificate.NotBefore, certificate.NotAfter)
	}
	if err := New(nil, at(generated.Add(time.Hour))).Validate(context.Background(), declaration, previous); err != nil {
		t.Fatalf("a certificate generated under the earlier rule is no longer current: %+v", diagnostics.Of(err))
	}
}

func rsaCertificate(t *testing.T, bits int, authority bool) secrets.Material {
	t.Helper()
	key, err := rsa.GenerateKey(cryptorand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2026), Subject: pkix.Name{CommonName: "service.example"}, DNSNames: []string{"service.example"},
		NotBefore: generationTime.AddDate(0, 0, -1), NotAfter: generationTime.AddDate(0, 0, 30),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if authority {
		template.IsCA, template.BasicConstraintsValid, template.KeyUsage, template.ExtKeyUsage = true, true, x509.KeyUsageCertSign, nil
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	parts := map[secrets.Part][]byte{secrets.CertificatePart: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
	if !authority {
		parts[secrets.PrivateKeyPart] = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	}
	return secrets.NewMaterial(parts)
}

// A TLS serving key with fewer than 2048 RSA bits refuses, naming its size;
// the roots of a CA bundle are trusted as given.
func TestTLSServingKeysUnder2048BitsRefuse(t *testing.T) {
	validator := New(nil, Options{Clock: func() time.Time { return generationTime }})
	serving := secrets.Declaration{Name: "serving", Type: "tlsCertificate", Source: "contextStore"}
	weak := rsaCertificate(t, 1024, false)
	defer weak.Clear()
	err := validator.Validate(context.Background(), serving, weak)
	assertFailureCode(t, err, "secret.input")
	if message := diagnosticMessage(err); !strings.HasPrefix(message, "TLS serving key is RSA-1024; a serving key needs at least 2048 RSA bits") {
		t.Fatalf("message = %q", message)
	}
	strong := rsaCertificate(t, 2048, false)
	defer strong.Clear()
	if err := validator.Validate(context.Background(), serving, strong); err != nil {
		t.Fatalf("an RSA-2048 serving key refused: %+v", diagnostics.Of(err))
	}
	authority := rsaCertificate(t, 1024, true)
	defer authority.Clear()
	if err := validator.Validate(context.Background(), secrets.Declaration{Name: "roots", Type: "caBundle", Source: "contextStore"}, authority); err != nil {
		t.Fatalf("an RSA-1024 CA bundle root refused: %+v", diagnostics.Of(err))
	}
}
