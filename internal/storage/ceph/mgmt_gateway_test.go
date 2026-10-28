package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

const gatewaySentinel = "mgmt-gateway-sentinel-never-frozen"

const gatewaySet = "bootwright secret set --context example --name mgmt-tls --certificate-file <path> --private-key-file <path>"

const gatewayUnbound = "cephadm signs the management gateway's TLS certificate; the readiness proof reports its SHA-256"

func mgmtGateway(spec api.Value) api.Value {
	return requestMap("service_type", "mgmt-gateway", "placement", requestMap("label", "mon", "count", 2), "spec", spec)
}

func mgmtIngress(vip string, extra ...any) api.Value {
	spec := requestMap(append([]any{"backend_service", "mgmt-gateway", "keepalive_only", true, "virtual_ip", vip}, extra...)...)
	return requestMap("service_type", "ingress", "service_id", "mgmt", "placement", requestMap("label", "mon", "count", 2), "spec", spec)
}

func vipGateway(vip string) []api.Value {
	return []api.Value{mgmtGateway(requestMap("port", 9443, "virtual_ip", vip)), mgmtIngress(vip)}
}

func gatewayCatalog(t *testing.T, services, bindings []api.Value, extra ...api.Object) api.Catalog {
	t.Helper()
	return api.NewCatalog(append(pinnedCatalog(t, func(spec api.Value) api.Value {
		spec = spec.WithPath(api.ListValue(append(spec.Get("services").Items(), services...)...), "services")
		if len(bindings) != 0 {
			spec = spec.WithPath(api.ListValue(bindings...), "serviceBindings")
		}
		return spec
	}).Objects(), extra...))
}

func gatewayUnitOf(t *testing.T, catalog api.Catalog) ServiceUnit {
	t.Helper()
	cluster, found := catalog.Find(api.CephCluster, "example")
	if !found {
		t.Fatal("the catalog has no CephCluster example")
	}
	units, err := gatewayUnits(cluster)
	if err != nil || len(units) != 1 {
		t.Fatalf("the gateway units are %+v (%+v)", units, diagnostics.Of(err))
	}
	return units[0]
}

func mgmtMaterial(certificate []byte) map[string]secrets.Material {
	return map[string]secrets.Material{"mgmt-tls": secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: certificate, secrets.PrivateKeyPart: []byte(gatewaySentinel)})}
}

func gatewayUnitBlock(t *testing.T, catalog api.Catalog) reconciliation.Block {
	t.Helper()
	return blockOf(t, cephPlan(t, catalog), "ceph-service-example-mgmt-gateway")
}

func requestBlock(t *testing.T, r Request) reconciliation.Block {
	t.Helper()
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "ceph-service-example-mgmt-gateway", Kind: string(api.CephCluster), Object: "example", Implementation: Implementation, Request: data}}
}

func TestGatewayCertificateMustCoverVirtualIP(t *testing.T) {
	ctx, capability := context.Background(), New(api.CephCluster, nil)
	services := append(vipGateway("192.0.2.60"), auxiliaryService("prometheus", "", api.Value{}))
	catalog := gatewayCatalog(t, services, []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")})
	block := gatewayUnitBlock(t, catalog)
	for _, leaf := range [][]byte{leafPEM(t, []string{"node-1"}), leafPEM(t, nil, net.ParseIP("192.0.2.61"))} {
		err := capability.ProveBinding(ctx, "example", block, mgmtMaterial(leaf))
		refusedWith(t, err, "secret.part", "the certificate of Secret mgmt-tls does not name 192.0.2.60, the virtual IP of the management gateway of CephCluster example that clients and its readiness proof connect to",
			"store a certificate whose subject alternative names include the IP address 192.0.2.60 with "+gatewaySet+", or set the management gateway's spec.virtual_ip and its ingress's spec.virtual_ip to an address the certificate holds, then apply again")
		if strings.Contains(diagnostics.Of(err)[0].Message+diagnostics.Of(err)[0].Remediation, gatewaySentinel) {
			t.Fatal("the refusal carries bound material")
		}
	}
	if err := capability.ProveBinding(ctx, "example", block, mgmtMaterial(leafPEM(t, []string{"node-1"}, net.ParseIP("192.0.2.60")))); err != nil {
		t.Fatalf("a certificate naming the virtual IP refuses %+v", diagnostics.Of(err))
	}
	err := capability.ProveBinding(ctx, "example", block, mgmtMaterial([]byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")))
	refusedWith(t, err, "secret.part", "the certificate of Secret mgmt-tls that the management gateway of CephCluster example binds cannot be read", "store a readable certificate and its private key with "+gatewaySet+", then apply again")
	err = capability.ProveBinding(ctx, "example", block, map[string]secrets.Material{})
	refusedWith(t, err, "lifecycle.state", "the TLS Secret the management gateway of a CephCluster binds is not bound to this apply", "")
	plan := cephPlan(t, catalog)
	for _, id := range []string{"ceph-service-example-monitoring", "ceph-cluster-example"} {
		if err := capability.ProveBinding(ctx, "example", blockOf(t, plan, id), nil); err != nil {
			t.Fatalf("%s reads material: %v", id, err)
		}
	}
	object := reconciliation.Block{BlockDefinition: gatewayBlock(t, directTLSCatalog())}
	if err := capability.ProveBinding(ctx, "example", object, nil); err != nil {
		t.Fatalf("an object gateway block under the CephCluster capability reads material: %v", err)
	}
	unbound := gatewayUnitBlock(t, gatewayCatalog(t, vipGateway("192.0.2.60"), nil))
	if err := capability.ProveBinding(ctx, "example", unbound, nil); err != nil {
		t.Fatalf("an unbound gateway reads material: %v", err)
	}
}

func hostGateway(hosts ...string) api.Value {
	return requestMap("service_type", "mgmt-gateway", "placement", requestMap("hosts", api.StringList(hosts...)), "spec", requestMap("port", 9443))
}

func TestGatewayCertificateMustCoverHostFQDNs(t *testing.T) {
	ctx, capability := context.Background(), New(api.CephCluster, nil)
	catalog := gatewayCatalog(t, []api.Value{hostGateway("node-1", "node-2", "node-3")}, []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")})
	block := gatewayUnitBlock(t, catalog)
	unnamed := func(err error, host string) {
		t.Helper()
		refusedWith(t, err, "secret.part", "the certificate of Secret mgmt-tls does not name "+host+", a host of the management gateway of CephCluster example that clients and its readiness proof connect to",
			"store a certificate whose subject alternative names include "+host+" (a DNS name or a matching wildcard) with "+gatewaySet+", or place the management gateway only on hosts the certificate names, then apply again")
	}
	unnamed(capability.ProveBinding(ctx, "example", block, mgmtMaterial(leafPEM(t, []string{"node-1", "node-2"}))), "node-3")
	unnamed(capability.ProveBinding(ctx, "example", block, mgmtMaterial(leafPEM(t, nil, net.ParseIP("192.0.2.1")))), "node-1")
	if err := capability.ProveBinding(ctx, "example", block, mgmtMaterial(leafPEM(t, []string{"node-1", "node-2", "node-3"}))); err != nil {
		t.Fatalf("a certificate naming every host refuses %+v", diagnostics.Of(err))
	}
	qualified := planCatalog(3, 3, func(spec api.Value) api.Value {
		nodes := spec.Get("topology", "nodes").Items()
		for i, node := range nodes {
			nodes[i] = node.With("fqdn", api.StringValue(node.Get("name").Text()+".ceph.example.test"))
		}
		spec = spec.WithPath(api.ListValue(nodes...), "topology", "nodes")
		spec = spec.WithPath(api.ListValue(append(spec.Get("services").Items(), mgmtGateway(requestMap("port", 9443)))...), "services")
		return spec.WithPath(api.ListValue(tlsBinding("mgmt-gateway", "mgmt-tls")), "serviceBindings")
	})
	unit := gatewayUnitOf(t, qualified)
	r := runtimeRequest()
	r.Action, r.Service = "service", &unit
	wildcard := requestBlock(t, r)
	if err := capability.ProveBinding(ctx, "example", wildcard, mgmtMaterial(leafPEM(t, []string{"*.ceph.example.test"}))); err != nil {
		t.Fatalf("a wildcard naming every host refuses %+v", diagnostics.Of(err))
	}
	err := capability.ProveBinding(ctx, "example", wildcard, mgmtMaterial(leafPEM(t, []string{"node-1.ceph.example.test"})))
	unnamed(err, "node-2.ceph.example.test")
}

func TestGatewayUnitDependsOnMonitoringUnit(t *testing.T) {
	for name, c := range map[string]struct {
		services     []api.Value
		bindings     []api.Value
		dependencies []string
		impacts      []string
	}{
		"with monitoring": {append(vipGateway("192.0.2.60"), auxiliaryService("prometheus", "", api.Value{})), nil,
			[]string{"ceph-cluster-example", "ceph-image-example", "ceph-service-example-monitoring"}, []string{"realize Ceph service mgmt-gateway of example", gatewayUnbound}},
		"bound with monitoring": {append(vipGateway("192.0.2.60"), auxiliaryService("prometheus", "", api.Value{})), []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")},
			[]string{"ceph-cluster-example", "ceph-image-example", "ceph-service-example-monitoring"}, []string{"realize Ceph service mgmt-gateway of example"}},
		"without monitoring": {vipGateway("192.0.2.60"), nil, []string{"ceph-cluster-example", "ceph-image-example"}, []string{"realize Ceph service mgmt-gateway of example", gatewayUnbound}},
	} {
		t.Run(name, func(t *testing.T) {
			unit := gatewayUnitBlock(t, gatewayCatalog(t, c.services, c.bindings))
			if unit.Kind != string(api.CephCluster) || unit.Object != "example" || unit.Implementation != Implementation || unit.Stage != reconciliation.StageClusters ||
				!slices.Equal(unit.Exclusive, []string{"ceph/example"}) || len(unit.Requires) != 0 || !slices.Equal(unit.Dependencies, c.dependencies) ||
				unit.Description != "realize Ceph service mgmt-gateway of example" || !slices.Equal(unit.Impacts, c.impacts) {
				t.Fatalf("the gateway block is %s %s %v %v %q %q", unit.Kind, unit.Object, unit.Exclusive, unit.Dependencies, unit.Description, unit.Impacts)
			}
		})
	}
}

func TestOAuth2ProxyNoLongerRefusesR16(t *testing.T) {
	catalog := withAuthoredImages(delegatedCatalog(ssoServices(), ssoBindings(), replicatedPool("volumes")), "keepalived", "nginx", "oauth2_proxy")
	for _, kind := range []api.Kind{api.CephCluster, api.CephPool} {
		name := map[api.Kind]string{api.CephCluster: "example", api.CephPool: "volumes"}[kind]
		if _, err := planned(t, catalog, kind, name); err != nil {
			t.Fatalf("%s %s refuses %+v", kind, name, diagnostics.Of(err))
		}
	}
	unit := gatewayUnitOf(t, catalog)
	documents := documentsOf(unit)
	if len(documents) != 3 || !strings.Contains(documents[0], `"service_type":"ingress"`) || !strings.Contains(documents[1], `"service_type":"mgmt-gateway"`) ||
		!strings.HasPrefix(documents[2], `{"custom_configs":[{"mount_path":"/etc/pki/tls/certs/oauth2-proxy-idp-ca.pem"}],`) || !strings.Contains(documents[2], `"service_type":"oauth2-proxy"`) {
		t.Fatalf("the SSO gateway unit renders:\n%s", strings.Join(documents, "\n"))
	}
	for _, document := range documents {
		if regexp.MustCompile(`client_secret|cookie_secret|ssl_cert|ssl_key|"content"`).MatchString(document) {
			t.Fatalf("an SSO document carries a bound leaf: %s", document)
		}
	}
	const sso = "the management gateway signs users in through oauth2-proxy and its identity provider, and Bootwright enables the Ceph dashboard's OAuth2 single sign-on"
	if impacts := gatewayImpacts(api.Object{}, unit); !slices.Equal(impacts, []string{gatewayUnbound, sso}) {
		t.Fatalf("the SSO gateway impacts are %q", impacts)
	}
	unsigned := unit
	unsigned.Bindings = slices.DeleteFunc(slices.Clone(unit.Bindings), func(b ServiceBinding) bool { return strings.HasPrefix(b.Path, "/spec/ssl_") })
	if impacts := gatewayImpacts(api.Object{}, unsigned); !slices.Equal(impacts, []string{gatewayUnbound, sso, "cephadm signs oauth2-proxy's TLS certificate"}) {
		t.Fatalf("the SSO gateway without its proxy's TLS pair impacts %q", impacts)
	}
}

func ssoProxy(extra ...any) api.Value {
	spec := requestMap(append([]any{"client_id", "ceph", "oidc_issuer_url", "https://idp.example.test/realms/ceph", "provider_display_name", "SSO"}, extra...)...)
	return requestMap("service_type", "oauth2-proxy", "placement", requestMap("label", "mon", "count", 1), "spec", spec,
		"custom_configs", api.ListValue(requestMap("mount_path", cephcluster.OAuth2ProxyCABundlePath)))
}

func ssoServices(extra ...any) []api.Value {
	return []api.Value{mgmtGateway(requestMap("port", 9443, "enable_auth", true, "virtual_ip", "192.0.2.60")), mgmtIngress("192.0.2.60"), ssoProxy(extra...)}
}

func ssoBindings() []api.Value {
	return []api.Value{requestMap("serviceRef", "oauth2-proxy", "secrets", api.ListValue(requestMap("path", "/custom_configs/0/content", "secretRef", "sso-ca"),
		requestMap("path", "/spec/client_secret", "secretRef", "sso-client"), requestMap("path", "/spec/cookie_secret", "secretRef", "sso-cookie"),
		requestMap("path", "/spec/ssl_cert", "secretRef", "sso-tls"), requestMap("path", "/spec/ssl_key", "secretRef", "sso-tls")))}
}

func withAuthoredImages(catalog api.Catalog, images ...string) api.Catalog {
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() != api.CephCluster {
			continue
		}
		spec := object.Spec()
		for _, name := range images {
			spec = spec.WithPath(requestMap("base", PinnedRegistry+"/synthetic/"+name, "version", "synthetic-1"), "images", name)
		}
		objects[i] = object.WithSpec(spec)
	}
	return api.NewCatalog(objects)
}

func ssoRequest(t *testing.T) Request {
	t.Helper()
	r := frozenGoldenRequest()
	unit := gatewayUnitOf(t, delegatedCatalog(ssoServices(), ssoBindings()))
	r.Action, r.Service = "service", &unit
	for _, document := range unitDocuments(r) {
		r.Delegated = append(r.Delegated, DelegatedService{Name: documentName(document), Hosts: document.Placement.Hosts})
	}
	for i, name := range []string{"keepalived", "nginx", "oauth2_proxy"} {
		r.Auxiliary = append(r.Auxiliary, AuxiliaryReference{Name: name, Image: PinnedRegistry + "/synthetic/" + name + "@sha256:" + strings.Repeat(string("abc"[i]), 64)})
	}
	return r
}

func TestOAuth2ProxyPlansWithItsSelectedImage(t *testing.T) {
	catalog := settingsCatalog(t, ssoServices())
	state := withImages(t, compilation.NewState(catalog, catalog, nil), "keepalived", "nginx", "oauth2_proxy")
	notRefused(t, state, "an SSO gateway with its images")
	plan := plannedAuxiliary(t, state)
	core, err := DecodeRequest(plan[ClusterBlockID("example")].Request)
	if err != nil || !slices.ContainsFunc(core.Auxiliary, func(a AuxiliaryReference) bool {
		return a.Name == "oauth2_proxy" && strings.HasPrefix(a.Image, PinnedRegistry+"/synthetic/oauth2_proxy")
	}) {
		t.Fatalf("the core request freezes %+v (%v)", core.Auxiliary, err)
	}
	if at := slices.IndexFunc(core.Delegated, func(d DelegatedService) bool { return d.Name == "oauth2-proxy" }); at < 0 || len(core.Delegated[at].Hosts) == 0 {
		t.Fatalf("the core request delegates %+v", core.Delegated)
	}
	r := ssoRequest(t)
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	matchesGolden(t, "request-cluster-sso", data, false)
	decoded, err := DecodeRequest(data)
	if roundtrip, rerr := decoded.Canonical(); err != nil || rerr != nil || !bytes.Equal(data, roundtrip) {
		t.Fatal("the SSO request did not round-trip")
	}
	plain := mgmtGatewayRequest(t)
	for request, want := range map[*Request]time.Duration{&r: 4260 * time.Second, &plain: 2940 * time.Second} {
		if got, err := cephDeadline(*request, "apply"); err != nil || got != want {
			t.Fatalf("the gateway apply deadline is %v (%v), want %v", got, err, want)
		}
		for operation, want := range map[string]time.Duration{"observe": 720 * time.Second, "destroy": 1380 * time.Second} {
			if got, err := cephDeadline(*request, operation); err != nil || got != want {
				t.Fatalf("the gateway %s deadline is %v (%v), want %v", operation, got, err, want)
			}
		}
	}
}

func TestSSOGatewayWithoutTheOAuth2ProxyImageRefusesNamingTheField(t *testing.T) {
	catalog := settingsCatalog(t, ssoServices())
	state := withImages(t, compilation.NewState(catalog, catalog, nil), "keepalived", "nginx")
	unauthoredRefusal(t, state, "oauth2-proxy", "oauth2-proxy", "oauth2_proxy")
}

func TestSSOUnitRealizesOnlyItsRecordMembers(t *testing.T) {
	for _, member := range []string{"scope", "email_domains"} {
		cluster, _ := delegatedCatalog(ssoServices(member, api.StringList("x")), ssoBindings()).Find(api.CephCluster, "example")
		_, err := gatewayUnits(cluster)
		refusedWith(t, err, "lifecycle.unsupported", "the native service oauth2-proxy carries spec."+member+", which Bootwright does not realize for this service",
			"remove spec."+member+" from oauth2-proxy in spec.services")
	}
	cluster, _ := delegatedCatalog(append(ssoServices()[:2], ssoProxy().With("custom_configs", api.ListValue(requestMap("mount_path", "/etc/other.pem")))), nil).Find(api.CephCluster, "example")
	_, err := gatewayUnits(cluster)
	refusedWith(t, err, "lifecycle.unsupported", "the native service oauth2-proxy carries config, extra container or entrypoint arguments or custom configs, which Bootwright does not realize",
		"remove those options from oauth2-proxy in spec.services")
}

func ssoReadyEvidence(r Request) Evidence {
	e := readyEvidence(r)
	e.Daemons = []UnitDaemon{{Host: "node-1", Name: "keepalived.mgmt.node-1.abcdef", Service: "ingress.mgmt"}, {Host: "node-1", Name: "mgmt-gateway.node-1", Service: "mgmt-gateway"},
		{Host: "node-2", Name: "oauth2-proxy.node-2.ghijkl", Service: "oauth2-proxy"}}
	e.Endpoint = EndpointEvidence{CertificateSHA256: strings.Repeat("a", 64), Reachable: true}
	e.SSO = "oauth2"
	return e
}

func TestSSOEvidence(t *testing.T) {
	r := runtimeRequest()
	unit := gatewayUnitOf(t, delegatedCatalog(ssoServices(), ssoBindings()))
	r.Action, r.Service = "service", &unit
	ready := ssoReadyEvidence(r)
	if effect, err := EvaluateEvidence(evidenceBytes(t, ready), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready SSO evidence is %s (%+v)", effect, diagnostics.Of(err))
	}
	for name, sso := range map[string]string{"single sign-on not enabled": "", "another protocol": "saml2"} {
		e := ready
		e.SSO = sso
		if effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("%s is %s", name, effect)
		}
	}
	plain := runtimeRequest()
	gateway := gatewayUnitOf(t, delegatedCatalog(vipGateway("192.0.2.60"), nil))
	plain.Action, plain.Service = "service", &gateway
	e := ssoReadyEvidence(plain)
	e.Daemons = e.Daemons[:2]
	if effect, err := EvaluateEvidence(evidenceBytes(t, e), plain, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("SSO evidence for a gateway without oauth2-proxy is %s", effect)
	}
	e.SSO = ""
	if effect, err := EvaluateEvidence(evidenceBytes(t, e), plain, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("a gateway without oauth2-proxy is %s (%+v)", effect, diagnostics.Of(err))
	}
	members := map[string]json.RawMessage{}
	if err := json.Unmarshal(evidenceBytes(t, ready), &members); err != nil || len(members) != 24 {
		t.Fatalf("ready SSO evidence has %d members (%v)", len(members), err)
	}
	members["specializations"] = json.RawMessage(`["synthetic-seam"]`)
	if effect, err := EvaluateEvidence(canonicalMembers(t, members), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("24 members with specializations are %s (%+v)", effect, diagnostics.Of(err))
	}
	delete(members, "specializations")
	delete(members, "sso")
	if effect, err := EvaluateEvidence(canonicalMembers(t, members), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("22 members without sso are %s", effect)
	}
}

func canonicalMembers(t *testing.T, members map[string]json.RawMessage) []byte {
	t.Helper()
	data, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestGatewayProofIgnoresTheOAuth2ProxyBindings(t *testing.T) {
	ctx, capability := context.Background(), New(api.CephCluster, nil)
	block := gatewayUnitBlock(t, gatewayCatalog(t, ssoServices(), ssoBindings()))
	ca := map[string]secrets.Material{"sso-ca": secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: leafPEM(t, []string{"idp.example.test"})})}
	if err := capability.ProveBinding(ctx, "example", block, ca); err != nil {
		t.Fatalf("a gateway whose own certificate cephadm signs reads the oauth2-proxy bindings: %+v", diagnostics.Of(err))
	}
	bound := gatewayUnitBlock(t, gatewayCatalog(t, ssoServices(), append(ssoBindings(), tlsBinding("mgmt-gateway", "mgmt-tls"))))
	refusedWith(t, capability.ProveBinding(ctx, "example", bound, ca), "lifecycle.state", "the TLS Secret the management gateway of a CephCluster binds is not bound to this apply", "")
}

func mgmtGatewayRequest(t *testing.T) Request {
	t.Helper()
	r := frozenGoldenRequest()
	unit := gatewayUnitOf(t, delegatedCatalog(vipGateway("192.0.2.60"), []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")}))
	r.Action, r.Service = "service", &unit
	for _, document := range unitDocuments(r) {
		r.Delegated = append(r.Delegated, DelegatedService{Name: documentName(document), Hosts: document.Placement.Hosts})
	}
	for i, name := range []string{"keepalived", "nginx"} {
		r.Auxiliary = append(r.Auxiliary, AuxiliaryReference{Name: name, Image: PinnedRegistry + "/synthetic/" + name + "@sha256:" + strings.Repeat(string("ab"[i]), 64)})
	}
	return r
}

func TestMgmtGatewayRequestGolden(t *testing.T) {
	r := mgmtGatewayRequest(t)
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	matchesGolden(t, "request-cluster-mgmt-gateway", data, false)
	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := decoded.Canonical()
	if err != nil || !bytes.Equal(data, roundtrip) {
		t.Fatal("the gateway request did not round-trip")
	}
	for _, leak := range []string{gatewaySentinel, `"ssl_cert":`, `"ssl_key":`} {
		if bytes.Contains(data, []byte(leak)) {
			t.Fatalf("the gateway request carries %q", leak)
		}
	}
	documents := decoded.Service.Documents
	if len(documents) != 2 || !bytes.Contains(documents[0], []byte(`"keepalive_only":true`)) || !bytes.Contains(documents[0], []byte(`"service_id":"mgmt"`)) ||
		bytes.Contains(documents[1], []byte(`"service_id"`)) || !bytes.Contains(documents[1], []byte(`"service_type":"mgmt-gateway"`)) {
		t.Fatalf("the gateway documents are %s", documents)
	}
	want := []ServiceBinding{{Path: "/spec/ssl_cert", SecretRef: "mgmt-tls", ServiceRef: "mgmt-gateway"}, {Path: "/spec/ssl_key", SecretRef: "mgmt-tls", ServiceRef: "mgmt-gateway"}}
	if !reflect.DeepEqual(decoded.Service.Bindings, want) {
		t.Fatalf("the gateway unit binds %+v", decoded.Service.Bindings)
	}
}

func TestGatewayUnitRendersTheCanonicalVirtualIP(t *testing.T) {
	unit := gatewayUnitOf(t, delegatedCatalog(vipGateway("2001:DB8:0:0::60"), nil))
	for _, document := range documentsOf(unit) {
		if !strings.Contains(document, `"virtual_ip":"2001:db8::60"`) {
			t.Fatalf("the gateway document %s carries no canonical virtual IP", document)
		}
	}
}

func TestGatewayProjectionRefusesUnrealizedMembers(t *testing.T) {
	admitted := requestMap("enable_auth", false, "enable_health_check_endpoint", true, "port", 9443, "ssl", true, "ssl_ciphers", api.StringList("ECDHE-ECDSA-AES256-GCM-SHA384"),
		"ssl_protocols", api.StringList("TLSv1.3"), "virtual_ip", "192.0.2.60")
	ingress := mgmtIngress("192.0.2.60", "first_virtual_router_id", 60, "health_check_interval", "2s", "virtual_interface_networks", api.StringList("192.0.2.0/24"))
	unit := gatewayUnitOf(t, delegatedCatalog([]api.Value{mgmtGateway(admitted), ingress}, nil))
	want := []string{`{"placement":{"count":2,"hosts":["node-1","node-2","node-3"]},"service_id":"mgmt","service_type":"ingress","spec":{"backend_service":"mgmt-gateway","first_virtual_router_id":60,"health_check_interval":"2s","keepalive_only":true,"virtual_ip":"192.0.2.60","virtual_interface_networks":["192.0.2.0/24"]}}`,
		`{"placement":{"count":2,"hosts":["node-1","node-2","node-3"]},"service_type":"mgmt-gateway","spec":{"enable_health_check_endpoint":true,"port":9443,"ssl":true,"ssl_ciphers":["ECDHE-ECDSA-AES256-GCM-SHA384"],"ssl_protocols":["TLSv1.3"],"virtual_ip":"192.0.2.60"}}`}
	if !slices.Equal(documentsOf(unit), want) {
		t.Fatalf("the gateway unit renders:\n%s", strings.Join(documentsOf(unit), "\n"))
	}
	cluster := func(services ...api.Value) api.Object {
		object, _ := delegatedCatalog(services, nil).Find(api.CephCluster, "example")
		return object
	}
	_, err := gatewayUnits(cluster(mgmtGateway(admitted.With("ssl_session_cache", api.StringValue("shared:SSL:10m"))), mgmtIngress("192.0.2.60")))
	refusedWith(t, err, "lifecycle.unsupported", "the native service mgmt-gateway carries spec.ssl_session_cache, which Bootwright does not realize for this service",
		"remove spec.ssl_session_cache from mgmt-gateway in spec.services")
	_, err = gatewayUnits(cluster(mgmtGateway(admitted), mgmtIngress("192.0.2.60", "frontend_port", 8443)))
	refusedWith(t, err, "lifecycle.unsupported", "the native service ingress.mgmt carries spec.frontend_port, which Bootwright does not realize for this service",
		"remove spec.frontend_port from ingress.mgmt in spec.services")
}

func TestGatewayBlockIdentityHashesALongCluster(t *testing.T) {
	cluster, _ := delegatedCatalog(vipGateway("192.0.2.60"), nil).Find(api.CephCluster, "example")
	fits := api.NewObject(api.CephCluster, strings.Repeat("c", 37), api.Value{}, cluster.Spec())
	units, err := gatewayUnits(fits)
	if err != nil || len(units) != 1 || ServiceUnitBlockID(fits.Name(), units[0]) != "ceph-service-"+fits.Name()+"-mgmt-gateway" {
		t.Fatalf("a 37-character cluster plans %+v (%v)", units, err)
	}
	long := api.NewObject(api.CephCluster, strings.Repeat("c", 63), api.Value{}, cluster.Spec())
	units, err = gatewayUnits(long)
	if err != nil || len(units) != 1 || !reconciliation.ValidSegment(ServiceUnitBlockID(long.Name(), units[0])) {
		t.Fatalf("a 63-character cluster plans %+v (%v)", units, err)
	}
}

func TestGatewayUnitDeadline(t *testing.T) {
	for _, hosts := range []int{3, 6} {
		catalog := planCatalog(hosts, hosts, func(spec api.Value) api.Value {
			return spec.WithPath(api.ListValue(append(spec.Get("services").Items(), vipGateway("192.0.2.60")...)...), "services")
		})
		cluster, _ := catalog.Find(api.CephCluster, "example")
		base, err := New(api.CephCluster, nil).request(catalog, cluster, "synthetic")
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		units, err := serviceUnitRequests(base, cluster)
		if err != nil || !slices.Equal(unitTypesOf(units), []string{"crash", "mgmt-gateway"}) || len(units[1].Hosts) != hosts {
			t.Fatalf("%d hosts plan units %+v (%v)", hosts, units, err)
		}
		for operation, want := range map[string]time.Duration{"apply": 2940 * time.Second, "observe": 720 * time.Second, "destroy": 1380 * time.Second} {
			if got, err := cephDeadline(units[1], operation); err != nil || got != want {
				t.Fatalf("with %d hosts the gateway %s deadline is %v (%v), want %v", hosts, operation, got, err, want)
			}
		}
	}
}

func TestGatewayUnitLendsCertificateToEveryOperationAndKeyToApply(t *testing.T) {
	r := runtimeRequest()
	unit := gatewayUnitOf(t, delegatedCatalog(vipGateway("192.0.2.60"), []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")}))
	r.Action, r.Service = "service", &unit
	certificate := lifecycle.MaterialFile{Name: "mgmt-gateway-certificate", Secret: "mgmt-tls", Part: secrets.CertificatePart, Variable: "mgmtGatewayCertificate"}
	key := lifecycle.MaterialFile{Name: "mgmt-gateway-key", Secret: "mgmt-tls", Part: secrets.PrivateKeyPart, Variable: "mgmtGatewayKey"}
	evidence := readyEvidence(r)
	evidence.Endpoint = EndpointEvidence{CertificateSHA256: strings.Repeat("a", 64), Reachable: true}
	for _, operation := range []string{"apply", "observe", "destroy"} {
		e := withMaterial(executionFor(t, r), r)
		maps.Copy(e.Material, mgmtMaterial([]byte(gatewaySentinel)))
		runner := &recordingRunner{}
		if _, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).run(context.Background(), e, operation, r); err != nil {
			t.Fatalf("%s: %+v", operation, diagnostics.Of(err))
		}
		call := runner.request
		visible, err := json.Marshal([]any{call.Canonical, call.Materials, call.Description, evidenceBytes(t, evidence)})
		if err != nil || bytes.Contains(visible, []byte(gatewaySentinel)) {
			t.Fatalf("%s leaks the bound value (%v)", operation, err)
		}
		lent := slices.DeleteFunc(slices.Clone(call.Materials), func(m lifecycle.MaterialFile) bool { return m.Secret != "mgmt-tls" })
		want := []lifecycle.MaterialFile{certificate}
		if operation == "apply" {
			want = append(want, key)
		}
		if !reflect.DeepEqual(lent, want) {
			t.Fatalf("%s lends %+v, want %+v", operation, lent, want)
		}
	}
	monitoring := runtimeRequest()
	grafana := monitoringUnit(t, []api.Value{auxiliaryService("grafana", "", requestMap("anonymous_access", false))}, []api.Value{grafanaPasswordBinding()})
	monitoring.Action, monitoring.Service = "service", &grafana
	runner := &recordingRunner{}
	if _, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).run(context.Background(), monitoringExecution(t, monitoring), "observe", monitoring); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if slices.ContainsFunc(runner.request.Materials, func(m lifecycle.MaterialFile) bool { return m.Secret == "grafana-admin" }) {
		t.Fatalf("a monitoring unit lends %+v to observe", runner.request.Materials)
	}
}

func TestGatewayUnitEndpointEvidence(t *testing.T) {
	r := runtimeRequest()
	unit := gatewayUnitOf(t, delegatedCatalog(vipGateway("192.0.2.60"), []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")}))
	r.Action, r.Service = "service", &unit
	ready := readyEvidence(r)
	ready.Daemons = []UnitDaemon{{Host: "node-1", Name: "keepalived.mgmt.node-1.abcdef", Service: "ingress.mgmt"}, {Host: "node-1", Name: "mgmt-gateway.node-1", Service: "mgmt-gateway"}}
	ready.Endpoint = EndpointEvidence{CertificateSHA256: strings.Repeat("a", 64), Reachable: true}
	if effect, err := EvaluateEvidence(evidenceBytes(t, ready), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready gateway evidence is %s (%+v)", effect, diagnostics.Of(err))
	}
	for name, endpoint := range map[string]EndpointEvidence{
		"reachable without a digest": {Reachable: true},
		"a digest without reachable": {CertificateSHA256: strings.Repeat("a", 64)},
		"an uppercase digest":        {CertificateSHA256: strings.Repeat("A", 64), Reachable: true},
		"ready but unreachable":      {},
	} {
		e := ready
		e.Endpoint = endpoint
		if effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("%s is %s", name, effect)
		}
	}
	monitoring := runtimeRequest()
	prometheus := monitoringUnit(t, []api.Value{auxiliaryService("prometheus", "", api.Value{})}, nil)
	monitoring.Action, monitoring.Service = "service", &prometheus
	e := readyEvidence(monitoring)
	e.Daemons = []UnitDaemon{{Host: "node-1", Name: "prometheus.node-1", Service: "prometheus"}}
	if effect, err := EvaluateEvidence(evidenceBytes(t, e), monitoring, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready monitoring evidence is %s (%+v)", effect, diagnostics.Of(err))
	}
	e.Endpoint = EndpointEvidence{CertificateSHA256: strings.Repeat("a", 64), Reachable: true}
	if effect, err := EvaluateEvidence(evidenceBytes(t, e), monitoring, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("a monitoring endpoint is %s", effect)
	}
}

func TestGatewayLeavesFollowTheBindingRegistry(t *testing.T) {
	cluster, _ := delegatedCatalog(ssoServices(), nil).Find(api.CephCluster, "example")
	for key, leaf := range gatewayLeaves {
		serviceType, path, _ := strings.Cut(key, " ")
		slot, found := cephcluster.NativeBindingSlot(cluster, serviceType, path)
		if !found || slot.Part != leaf.Part {
			t.Fatalf("%s lends part %s, the registry holds %+v", key, leaf.Part, slot)
		}
	}
}

func TestGatewayLeavesMatchTheWorker(t *testing.T) {
	want := []string{}
	for _, key := range slices.Sorted(maps.Keys(gatewayLeaves)) {
		serviceType, path, _ := strings.Cut(key, " ")
		want = append(want, serviceType, path, gatewayLeaves[key].Variable)
	}
	if got := pythonTuple(t, observabilityModule, "GATEWAY_LEAVES"); !slices.Equal(got, want) {
		t.Fatalf("the worker binds %v, the plan %v", got, want)
	}
}

func TestGatewayTypesMatchTheWorker(t *testing.T) {
	if got := pythonTuple(t, observabilityModule, "GATEWAY_TYPES"); !slices.Equal(got, GatewayTypes) {
		t.Fatalf("the worker realizes %v, the plan %v", got, GatewayTypes)
	}
}
