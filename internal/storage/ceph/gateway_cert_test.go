package ceph

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

func directTLSCatalog() api.Catalog {
	return planCatalog(3, 3, func(spec api.Value) api.Value {
		spec = withPairAndRGW(spec)
		return spec.WithPath(api.ListValue(tlsBinding("rgw.objects", "gateway-tls")), "serviceBindings")
	}, objectGateway("objects", "endpoint", requestMap("fqdn", "s3.example.test")))
}

func leafPEM(t *testing.T, names []string, addresses ...net.IP) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(7), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: names, IPAddresses: addresses}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func tlsMaterial(certificate []byte) map[string]secrets.Material {
	return map[string]secrets.Material{"gateway-tls": secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: certificate, secrets.PrivateKeyPart: []byte("gateway-private")})}
}

func TestGatewayCertificateSANMustCoverEndpoint(t *testing.T) {
	ctx, capability := context.Background(), New(api.CephObjectGateway, nil)
	block := reconciliation.Block{BlockDefinition: gatewayBlock(t, directTLSCatalog())}
	set := "bootwright secret set --context example --name gateway-tls --certificate-file <path> --private-key-file <path>"
	unnamed := func(err error) {
		t.Helper()
		refusedWith(t, err, "secret.part", "the certificate of Secret gateway-tls does not name s3.example.test, the endpoint of CephObjectGateway objects that clients and its readiness proof connect to",
			"store a certificate whose subject alternative names include s3.example.test (a DNS name or a matching wildcard) with "+set+", or set spec.endpoint.fqdn of CephObjectGateway objects to a name the certificate holds, then apply again")
	}
	unnamed(capability.ProveBinding(ctx, "example", block, tlsMaterial(leafPEM(t, nil, net.ParseIP("192.0.2.50")))))
	unnamed(capability.ProveBinding(ctx, "example", block, tlsMaterial(leafPEM(t, []string{"*.test"}))))
	for _, names := range [][]string{{"s3.example.test"}, {"*.example.test"}} {
		if err := capability.ProveBinding(ctx, "example", block, tlsMaterial(leafPEM(t, names, net.ParseIP("192.0.2.50")))); err != nil {
			t.Fatalf("a certificate naming %v refuses %v", names, err)
		}
	}
	err := capability.ProveBinding(ctx, "example", block, tlsMaterial([]byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")))
	refusedWith(t, err, "secret.part", "the certificate of Secret gateway-tls that CephObjectGateway objects binds cannot be read", "store a readable certificate and its private key with "+set+", then apply again")
	err = capability.ProveBinding(ctx, "example", block, map[string]secrets.Material{})
	refusedWith(t, err, "lifecycle.state", "the TLS Secret a CephObjectGateway binds is not bound to this apply", "")
	plain := reconciliation.Block{BlockDefinition: gatewayBlock(t, planCatalog(3, 3, withPairAndHTTPRGW, objectGateway("objects")))}
	pool := reconciliation.Block{BlockDefinition: gatewayBlocks(t, directTLSCatalog())[0]}
	for name, c := range map[string]struct {
		capability Capability
		block      reconciliation.Block
	}{"plain gateway": {capability, plain}, "derived pool": {capability, pool}, "cluster capability": {New(api.CephCluster, nil), block}} {
		if err := c.capability.ProveBinding(ctx, "example", c.block, nil); err != nil {
			t.Fatalf("%s reads material: %v", name, err)
		}
	}
}

func TestGatewayPlanBindsItsTLSSecret(t *testing.T) {
	plan, err := kindPlan(api.CephObjectGateway, directTLSCatalog())
	if err != nil || !slices.Contains(plan.Secrets, "gateway-tls") {
		t.Fatalf("the TLS gateway plan binds %v (%v)", plan.Secrets, err)
	}
	plan, err = kindPlan(api.CephObjectGateway, planCatalog(3, 3, withPairAndHTTPRGW, objectGateway("objects")))
	if err != nil || slices.Contains(plan.Secrets, "gateway-tls") {
		t.Fatalf("the plain gateway plan binds %v (%v)", plan.Secrets, err)
	}
}

func TestGatewayFrontedByAnIngressDependsOnTheSettingsBlock(t *testing.T) {
	gateway := objectGateway("objects")
	fronted := gatewayRuntimeRequest()
	fronted.Gateway.Documents = append(fronted.Gateway.Documents, []byte(`{"service_id":"rgw.objects","service_type":"ingress"}`))
	fronted.Auxiliary = []AuxiliaryReference{{Name: "haproxy", Image: "registry.example.test/haproxy@sha256:" + resolvedDigest}}
	direct := gatewayRuntimeRequest()
	direct.Auxiliary = fronted.Auxiliary
	unselected := gatewayRuntimeRequest()
	unselected.Gateway.Documents = fronted.Gateway.Documents
	for name, c := range map[string]struct {
		request Request
		want    bool
	}{"fronted": {fronted, true}, "without the ingress": {direct, false}, "without auxiliary": {unselected, false}} {
		_, dependencies := placement(gateway, c.request)
		if slices.Contains(dependencies, "ceph-settings-example") != c.want {
			t.Fatalf("%s depends on %v", name, dependencies)
		}
	}
}
