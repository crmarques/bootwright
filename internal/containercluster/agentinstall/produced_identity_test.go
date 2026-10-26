package agentinstall

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

func clientCertificate(t *testing.T, subject pkix.Name) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: subject, NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1<<32, 0)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func kubeconfigOf(server, authority, certificate string) secrets.Material {
	value := "apiVersion: v1\nkind: Config\nclusters:\n- name: lab\n  cluster:\n    server: " + server +
		"\n    certificate-authority-data: " + authority + "\nusers:\n- name: admin\n  user:\n    client-certificate-data: " + certificate +
		"\n    client-key-data: c2VjcmV0\n"
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})
}

// A kubeconfig's identity is its cluster's server, its decoded authority and
// its client certificate's subject, never its credential, so a recapture that
// only rotates the client key keeps it, and a
// reinstalled cluster's new authority or another principal does not.
func TestAKubeconfigsIdentityIsItsServerAuthorityAndPrincipal(t *testing.T) {
	identity := func(material secrets.Material) string {
		t.Helper()
		value, err := InstallCapability{}.ProducedIdentity(reconciliation.Block{}, KubeconfigOutput, material)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	admin := pkix.Name{CommonName: "system:admin", Organization: []string{"system:masters"}}
	const server, authority = "https://api.lab.test:6443", "QUFBQQ=="
	first := identity(kubeconfigOf(server, authority, clientCertificate(t, admin)))
	if !strings.HasPrefix(first, server+" under authority sha256:") || !strings.HasSuffix(first, " as CN=system:admin,O=system:masters") || strings.Contains(first, "c2VjcmV0") {
		t.Fatalf("identity = %q", first)
	}
	if rotated := identity(kubeconfigOf(server, authority, clientCertificate(t, admin))); rotated != first {
		t.Fatalf("a rotated client key changed the identity to %q", rotated)
	}
	for name, material := range map[string]secrets.Material{
		"another authority": kubeconfigOf(server, "QkJCQg==", clientCertificate(t, admin)),
		"another server":    kubeconfigOf("https://api.other.test:6443", authority, clientCertificate(t, admin)),
		"another principal": kubeconfigOf(server, authority, clientCertificate(t, pkix.Name{CommonName: "system:kubelet"})),
	} {
		if identity(material) == first {
			t.Fatalf("%s kept the identity", name)
		}
	}
	for name, material := range map[string]secrets.Material{
		"no cluster":            secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("clusters: []\n")}),
		"an undecodable CA":     kubeconfigOf(server, "!!", clientCertificate(t, admin)),
		"no client certificate": kubeconfigOf(server, authority, ""),
	} {
		if _, err := (InstallCapability{}).ProducedIdentity(reconciliation.Block{}, KubeconfigOutput, material); err == nil {
			t.Fatalf("a kubeconfig with %s has an identity", name)
		}
	}
}
