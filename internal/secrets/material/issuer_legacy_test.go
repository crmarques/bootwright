package material

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/secrets"
)

func TestGeneratedTLSAcceptsLegacyIssuerStartClamp(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(-time.Hour).AddDate(0, 0, 10), BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	der, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	private, _ := x509.MarshalPKCS8PrivateKey(key)
	ca := secrets.Declaration{Name: "root", Type: "caBundle", Source: "generated", Generation: secrets.Generation{CommonName: "synthetic root", ValidityDays: 10}}
	root := secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), secrets.PrivateKeyPart: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})})
	defer root.Clear()
	service := New(nil, Options{Clock: func() time.Time { return now }})
	if err := service.Validate(context.Background(), ca, root); err != nil {
		t.Fatal("CA prerequisite", err)
	}
	leaf := secrets.Declaration{Name: "leaf", Type: "tlsCertificate", Source: "generated", Generation: secrets.Generation{IssuerRef: "root", CommonName: "leaf.example.test", DNSNames: []string{"leaf.example.test"}, ValidityDays: 1}}
	result, err := service.GenerateSigned(context.Background(), leaf, ca, root)
	result.Clear()
	if err != nil {
		t.Fatal("valid legacy issuer cannot sign leaf", err)
	}
}
