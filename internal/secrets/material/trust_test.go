package material

import (
	"context"
	"crypto/x509"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/secrets"
)

func TestTrustBundleAcceptsPublicLeavesAndUnorderedMixedAnchors(t *testing.T) {
	service := New(nil, Options{Clock: func() time.Time { return generationTime }})
	leaf, key := customCertificate(t, x509.ExtKeyUsageServerAuth, false)
	defer clear(leaf)
	defer clear(key)
	ca, _ := customCertificate(t, x509.ExtKeyUsageServerAuth, true)
	defer clear(ca)
	declaration := secrets.Declaration{Name: "trust", Type: "trustBundle", Source: "contextStore"}
	for _, data := range [][]byte{leaf, ca, append(append([]byte{}, leaf...), ca...), append(append([]byte{}, ca...), leaf...)} {
		material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: data})
		if err := service.Validate(context.Background(), declaration, material); err != nil {
			t.Fatal(err)
		}
		material.Clear()
	}
	assertFailureCode(t, validateCA(leaf, nil, generationTime), "secret.input")
	weak := rsaCertificate(t, 1024, false)
	defer weak.Clear()
	weakCertificate := requiredPart(t, weak, secrets.CertificatePart)
	defer clear(weakCertificate)
	assertFailureCode(t, validateTrust(weakCertificate, generationTime), "secret.input")
	assertFailureCode(t, validateTrust(leaf, generationTime.AddDate(0, 0, 100)), "secret.input")
	client, clientKey := customCertificate(t, x509.ExtKeyUsageClientAuth, false)
	defer clear(client)
	defer clear(clientKey)
	assertFailureCode(t, validateTrust(client, generationTime), "secret.input")
	for _, data := range [][]byte{nil, append(append([]byte{}, leaf...), key...), append([]byte("garbage"), leaf...)} {
		assertFailureCode(t, validateTrust(data, generationTime), "secret.input")
	}
	extra := secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: leaf, secrets.PrivateKeyPart: key})
	defer extra.Clear()
	assertFailureCode(t, service.Validate(context.Background(), declaration, extra), "secret.input")
	declaration.Source = "generated"
	if _, err := service.Generate(context.Background(), declaration); err == nil {
		t.Fatal("generated trustBundle accepted")
	}
}
