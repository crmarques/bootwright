package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func TestGeneratedIssuerThroughProductionCustodyAndFrozenBinding(t *testing.T) {
	services, _, input, _ := contextFixture(t)
	addSecretInput(t, input, "issuer.yaml", strings.Join([]string{
		secretDocument("a-leaf", "tlsCertificate", "  source: {generated: {issuerRef: z-root, commonName: bmc.example.test, dnsNames: [bmc.example.test], validityDays: 30}}\n"),
		secretDocument("z-root", "caBundle", "  source: {generated: {commonName: synthetic-root, validityDays: 30}}\n"),
	}, "\n---\n"))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "encryption", "init")
	out, _ := contextRun(t, services, 0, "secret", "generate", "--name", "a-leaf")
	if strings.Contains(out, "PRIVATE KEY") || strings.Contains(out, "CERTIFICATE") {
		t.Fatal("generation disclosed material")
	}
	contextRun(t, services, 0, "secret", "check")
	bindings := services.Secrets.(interface {
		Bind(context.Context, custody.BindRequest) (secretstore.Binding, error)
		Reopen(context.Context, custody.BindingRequest) ([]secretstore.BoundMaterial, error)
	})
	binding, err := bindings.Bind(context.Background(), custody.BindRequest{Names: []string{"a-leaf", "z-root"}})
	if err != nil {
		t.Fatal(err)
	}
	read := func() []byte {
		items, err := bindings.Reopen(context.Background(), custody.BindingRequest{BindingID: binding.ID})
		if err != nil {
			t.Fatal(err)
		}
		var chain, root []byte
		for _, item := range items {
			certificate, _ := item.Material.Part(secrets.CertificatePart)
			if item.Version.Declaration.Name == "z-root" {
				root = certificate
				if _, ok := item.Material.Part(secrets.PrivateKeyPart); ok {
					t.Fatal("CA key lent to consumer")
				}
			} else {
				chain = certificate
			}
			item.Material.Clear()
		}
		defer clear(root)
		leafBlock, rest := pem.Decode(chain)
		if leafBlock == nil {
			t.Fatal("leaf absent")
		}
		rootBlock, _ := pem.Decode(rest)
		if rootBlock == nil {
			t.Fatal("issuer chain absent")
		}
		leaf, err := x509.ParseCertificate(leafBlock.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(root) {
			t.Fatal("invalid CA")
		}
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "bmc.example.test"}); err != nil {
			t.Fatal("serving leaf does not chain to physical CA", err)
		}
		return chain
	}
	original := read()
	defer clear(original)
	contextRun(t, services, 0, "secret", "generate", "--name", "z-root", "--renew")
	contextRun(t, services, 0, "secret", "generate", "--name", "a-leaf", "--renew")
	reopened := read()
	defer clear(reopened)
	if !bytes.Equal(original, reopened) {
		t.Fatal("renewal changed frozen trust")
	}
}

func TestCAIssuedRehearsalKeepsSubstrateAndInstallationOwnersSeparate(t *testing.T) {
	catalog := compiledEffective(t, "lab-baremetal-substrate")
	leaf, found := catalog.Find(api.Secret, "lab-bmc-tls")
	if !found || leaf.Spec().Get("source", "generated", "issuerRef").Text() != "lab-bmc-ca" {
		t.Fatal("rehearsal lacks signed leaf")
	}
	if _, found := catalog.Find(api.Secret, "lab-bmc-ca"); !found {
		t.Fatal("issuer pruned from effective closure")
	}
	if len(installation.InstalledMachines(catalog)) != 0 {
		t.Fatal("substrate context owns OS installation")
	}
	if refused := libvirt.Refusals(catalog); len(refused) != 0 {
		t.Fatalf("substrate refuses: %+v", refused)
	}
	physical := compiledEffective(t, "lab-baremetal")
	requests := plannedInstallations(t, "lab-baremetal", "controller", "lab")
	if len(requests) != 1 || !requests[0].Target.Physical || !requests[0].Target.Controller.TLSVerify || requests[0].Target.Controller.TrustBundleRef != "lab-bmc-ca" {
		t.Fatal("physical installation lost verified CA contract")
	}
	if refused := installation.Refusals(physical); len(refused) != 0 {
		t.Fatalf("physical installation refuses: %+v", refused)
	}
}
