package material

import (
	"bytes"
	"context"
	"encoding/pem"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/secrets"
)

func TestGeneratedTLSIssuerChainCustodyAndValidity(t *testing.T) {
	ctx := context.Background()
	now := generationTime
	service := New(nil, Options{Clock: func() time.Time { return now }})
	ca := secrets.Declaration{Name: "root", Type: "caBundle", Source: "generated", Generation: secrets.Generation{CommonName: "synthetic root", ValidityDays: 2}}
	root, err := service.Generate(ctx, ca)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Clear()
	leaf := secrets.Declaration{Name: "leaf", Type: "tlsCertificate", Source: "generated", Generation: secrets.Generation{IssuerRef: "root", CommonName: "bmc.example.test", DNSNames: []string{"bmc.example.test"}, ValidityDays: 10}}
	if unproved, err := service.Generate(ctx, leaf); err == nil {
		unproved.Clear()
		t.Fatal("unproved issuer self-signed")
	}
	signed, err := service.GenerateSigned(ctx, leaf, ca, root)
	if err != nil {
		t.Fatal(err)
	}
	defer signed.Clear()
	if err := service.Validate(ctx, leaf, signed); err != nil {
		t.Fatal(err)
	}
	chain := requiredPart(t, signed, secrets.CertificatePart)
	defer clear(chain)
	certificates, err := parseCertificates(chain)
	if err != nil || len(certificates) != 2 {
		t.Fatal("missing chain", err)
	}
	if certificates[0].CheckSignatureFrom(certificates[1]) != nil || !certificates[0].NotAfter.Equal(certificates[1].NotAfter) {
		t.Fatal("unproved signer or uncapped validity")
	}
	leafKey := requiredPart(t, signed, secrets.PrivateKeyPart)
	defer clear(leafKey)
	caKey := requiredPart(t, root, secrets.PrivateKeyPart)
	defer clear(caKey)
	if bytes.Equal(leafKey, caKey) || bytes.Contains(chain, caKey) {
		t.Fatal("issuer key leaked")
	}
	another, err := service.GenerateSigned(ctx, leaf, ca, root)
	if err != nil {
		t.Fatal(err)
	}
	defer another.Clear()
	otherKey := requiredPart(t, another, secrets.PrivateKeyPart)
	defer clear(otherKey)
	if bytes.Equal(leafKey, otherKey) {
		t.Fatal("leaf key reused")
	}
	for _, wrong := range []secrets.Declaration{{Name: "wrong", Type: ca.Type, Source: ca.Source, Generation: ca.Generation}, {Name: ca.Name, Type: ca.Type, Source: "contextStore", Generation: ca.Generation}} {
		value, err := service.GenerateSigned(ctx, leaf, wrong, root)
		value.Clear()
		if err == nil {
			t.Fatal("wrong issuer admitted")
		}
	}
	rootCert := requiredPart(t, root, secrets.CertificatePart)
	defer clear(rootCert)
	missingKey := secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: rootCert})
	defer missingKey.Clear()
	value, err := service.GenerateSigned(ctx, leaf, ca, missingKey)
	value.Clear()
	if err == nil {
		t.Fatal("issuer without signing key admitted")
	}
	leafBlock, rest := pem.Decode(chain)
	for _, broken := range [][]byte{pem.EncodeToMemory(leafBlock), append(append([]byte{}, rest...), pem.EncodeToMemory(leafBlock)...)} {
		corrupt := secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: broken, secrets.PrivateKeyPart: leafKey})
		err := service.Validate(ctx, leaf, corrupt)
		corrupt.Clear()
		clear(broken)
		if err == nil {
			t.Fatal("missing/reordered issuer chain admitted")
		}
	}
	now = now.Add(3 * 24 * time.Hour)
	value, err = service.GenerateSigned(ctx, leaf, ca, root)
	value.Clear()
	if err == nil {
		t.Fatal("expired issuer admitted")
	}
	now = generationTime.Add(-48 * time.Hour)
	value, err = service.GenerateSigned(ctx, leaf, ca, root)
	value.Clear()
	if err == nil {
		t.Fatal("future issuer admitted")
	}
}
