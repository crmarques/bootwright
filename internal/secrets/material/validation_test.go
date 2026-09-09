package material

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/secrets"
	"golang.org/x/crypto/ssh"
)

func TestStrictPEMRejectsSkippedMalformedAndNestedBlocks(t *testing.T) {
	service := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }})
	tlsValue, err := service.Generate(context.Background(), secrets.Declaration{
		Type: "tlsCertificate", Source: "generated", Generation: secrets.Generation{CommonName: "tls.example", ValidityDays: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tlsValue.Clear()
	certificate := requiredPart(t, tlsValue, secrets.CertificatePart)
	privateKey := requiredPart(t, tlsValue, secrets.PrivateKeyPart)
	defer clear(certificate)
	defer clear(privateKey)

	badKeys := [][]byte{
		append([]byte("not PEM\n"), privateKey...),
		append(append([]byte{}, privateKey...), privateKey...),
		append([]byte("-----BEGIN PRIVATE KEY-----\n"), privateKey...),
		append([]byte("-----BEGIN PRIVATE KEY-----\nnot-base64\n-----END PRIVATE KEY-----\n"), privateKey...),
		append(append([]byte("-----BEGIN PRIVATE KEY-----\nnot-base64\n-----END PRIVATE KEY-----\n"), privateKey...), '\n'),
	}
	for index, bad := range badKeys {
		if _, err := exactPrivatePEM(bad); err == nil {
			t.Fatalf("invalid private PEM case %d accepted", index)
		}
		clear(bad)
	}

	badCertificates := [][]byte{
		append([]byte("not PEM\n"), certificate...),
		append([]byte("-----BEGIN CERTIFICATE-----\n"), certificate...),
		append(append([]byte{}, certificate...), []byte("\nnot PEM")...),
		append([]byte("-----BEGIN CERTIFICATE-----\nnot-base64\n-----END CERTIFICATE-----\n"), certificate...),
	}
	for index, bad := range badCertificates {
		if _, err := parseCertificates(bad); err == nil {
			t.Fatalf("invalid certificate PEM case %d accepted", index)
		}
		clear(bad)
	}
}

func TestCertificateValidationRejectsMismatchExpiryUsageAndFalseChain(t *testing.T) {
	service := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }})
	declaration := secrets.Declaration{Type: "tlsCertificate", Source: "generated", Generation: secrets.Generation{CommonName: "tls.example", ValidityDays: 10}}
	first, err := service.Generate(context.Background(), declaration)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Clear()
	second, err := service.Generate(context.Background(), declaration)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Clear()
	firstCertificate := requiredPart(t, first, secrets.CertificatePart)
	firstKey := requiredPart(t, first, secrets.PrivateKeyPart)
	secondKey := requiredPart(t, second, secrets.PrivateKeyPart)
	defer clear(firstCertificate)
	defer clear(firstKey)
	defer clear(secondKey)

	assertFailureCode(t, validateTLS(firstCertificate, secondKey, generationTime.UTC()), "secret.input")
	assertFailureCode(t, validateTLS(firstCertificate, firstKey, generationTime.AddDate(0, 0, 11).UTC()), "secret.input")

	clientCertificate, clientKey := customCertificate(t, x509.ExtKeyUsageClientAuth, false)
	defer clear(clientCertificate)
	defer clear(clientKey)
	assertFailureCode(t, validateTLS(clientCertificate, clientKey, generationTime.UTC()), "secret.input")

	caService := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }})
	ca, err := caService.Generate(context.Background(), secrets.Declaration{
		Type: "caBundle", Source: "generated", Generation: secrets.Generation{CommonName: "unrelated-ca.example", ValidityDays: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ca.Clear()
	caCertificate := requiredPart(t, ca, secrets.CertificatePart)
	defer clear(caCertificate)
	falseChain := append(append([]byte{}, firstCertificate...), caCertificate...)
	defer clear(falseChain)
	assertFailureCode(t, validateTLS(falseChain, firstKey, generationTime.UTC()), "secret.input")

	assertFailureCode(t, validateCA(firstCertificate, nil, generationTime.UTC()), "secret.input")
	validBundle := append(append([]byte{}, caCertificate...), caCertificate...)
	defer clear(validBundle)
	if err := validateCA(validBundle, nil, generationTime.UTC()); err != nil {
		t.Fatalf("multiple CA bundle: %v", err)
	}
}

func TestSSHValidationRejectsWeakEncryptedTrailingOptionsAndMismatch(t *testing.T) {
	service := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }})
	first, err := service.Generate(context.Background(), secrets.Declaration{Type: "sshKeyPair", Source: "generated"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Clear()
	second, err := service.Generate(context.Background(), secrets.Declaration{Type: "sshKeyPair", Source: "generated"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Clear()
	privateKey := requiredPart(t, first, secrets.PrivateKeyPart)
	publicKey := requiredPart(t, first, secrets.PublicKeyPart)
	otherPublic := requiredPart(t, second, secrets.PublicKeyPart)
	defer clear(privateKey)
	defer clear(publicKey)
	defer clear(otherPublic)

	assertFailureCode(t, validateSSH(privateKey, otherPublic), "secret.input")
	twoPrivate := append(append([]byte{}, privateKey...), privateKey...)
	defer clear(twoPrivate)
	assertFailureCode(t, validateSSH(twoPrivate, publicKey), "secret.input")
	twoPublic := append(append([]byte{}, publicKey...), publicKey...)
	defer clear(twoPublic)
	assertFailureCode(t, validateSSH(privateKey, twoPublic), "secret.input")
	withOptions := append([]byte("restrict "), publicKey...)
	defer clear(withOptions)
	assertFailureCode(t, validateSSH(privateKey, withOptions), "secret.input")
	withLeadingComment := append([]byte("# ignored\n"), publicKey...)
	defer clear(withLeadingComment)
	assertFailureCode(t, validateSSH(privateKey, withLeadingComment), "secret.input")

	block, err := exactPrivatePEM(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	encryptedLooking := pem.EncodeToMemory(&pem.Block{Type: block.Type, Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"}, Bytes: block.Bytes})
	defer clear(encryptedLooking)
	assertFailureCode(t, validateSSH(encryptedLooking, publicKey), "secret.input")

	weak, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	defer clearPrivateKey(weak)
	weakDER, err := x509.MarshalPKCS8PrivateKey(weak)
	if err != nil {
		t.Fatal(err)
	}
	weakPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: weakDER})
	clear(weakDER)
	defer clear(weakPEM)
	weakPublic, err := ssh.NewPublicKey(&weak.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	weakAuthorized := ssh.MarshalAuthorizedKey(weakPublic)
	defer clear(weakAuthorized)
	assertFailureCode(t, validateSSH(weakPEM, weakAuthorized), "secret.input")

}

func customCertificate(t *testing.T, usage x509.ExtKeyUsage, isCA bool) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	defer clearPrivateKey(key)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "custom.example"},
		NotBefore: generationTime.Add(-time.Hour), NotAfter: generationTime.Add(time.Hour),
		BasicConstraintsValid: true, IsCA: isCA, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{usage},
	}
	if isCA {
		template.KeyUsage |= x509.KeyUsageCertSign
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	clear(der)
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	privateKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	clear(privateDER)
	return certificate, privateKey
}

func FuzzStrictPEMParsers(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("-----BEGIN PRIVATE KEY-----\n-----END PRIVATE KEY-----\n"),
		[]byte("-----BEGIN CERTIFICATE-----\n-----END CERTIFICATE-----\n"),
		[]byte("-----BEGIN PRIVATE KEY-----\n-----BEGIN PRIVATE KEY-----\n-----END PRIVATE KEY-----\n"),
		{0xff, 0, '-'},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > secrets.MaxPartBytes {
			t.Skip()
		}
		_, _ = exactPrivatePEM(data)
		_, _ = parseCertificates(data)
	})
}
