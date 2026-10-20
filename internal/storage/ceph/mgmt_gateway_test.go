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
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
)

const gatewaySentinel = "mgmt-gateway-sentinel-never-frozen"

const gatewaySet = "bootwright secret set --context example --name mgmt-tls --certificate-file <path> --private-key-file <path>"

const oauth2ProxyReason = "StorageCluster example declares an oauth2-proxy service, and Bootwright does not realize single sign-on through oauth2-proxy yet"

const oauth2ProxyRemedy = "remove the oauth2-proxy service and its spec.ceph.serviceBindings entries from StorageCluster example, and omit spec.enable_auth on its mgmt-gateway service or set it to false; B479 tracks single sign-on"

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
		spec = spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), services...)...), "ceph", "services")
		if len(bindings) != 0 {
			spec = spec.WithPath(api.ListValue(bindings...), "ceph", "serviceBindings")
		}
		return spec
	}).Objects(), extra...))
}

func gatewayUnitOf(t *testing.T, catalog api.Catalog) ServiceUnit {
	t.Helper()
	cluster, found := catalog.Find(api.StorageCluster, "example")
	if !found {
		t.Fatal("the catalog has no StorageCluster example")
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
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "ceph-service-example-mgmt-gateway", Kind: string(api.StorageCluster), Object: "example", Implementation: Implementation, Request: data}}
}

func TestGatewayCertificateMustCoverVirtualIP(t *testing.T) {
	ctx, capability := context.Background(), New(api.StorageCluster, nil)
	services := append(vipGateway("192.0.2.60"), auxiliaryService("prometheus", "", api.Value{}))
	catalog := gatewayCatalog(t, services, []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")})
	block := gatewayUnitBlock(t, catalog)
	for _, leaf := range [][]byte{leafPEM(t, []string{"node-1"}), leafPEM(t, nil, net.ParseIP("192.0.2.61"))} {
		err := capability.ProveBinding(ctx, "example", block, mgmtMaterial(leaf))
		refusedWith(t, err, "secret.part", "the certificate of Secret mgmt-tls does not name 192.0.2.60, the virtual IP of the management gateway of StorageCluster example that clients and its readiness proof connect to",
			"store a certificate whose subject alternative names include the IP address 192.0.2.60 with "+gatewaySet+", or set the management gateway's spec.virtual_ip and its ingress's spec.virtual_ip to an address the certificate holds, then apply again")
		if strings.Contains(diagnostics.Of(err)[0].Message+diagnostics.Of(err)[0].Remediation, gatewaySentinel) {
			t.Fatal("the refusal carries bound material")
		}
	}
	if err := capability.ProveBinding(ctx, "example", block, mgmtMaterial(leafPEM(t, []string{"node-1"}, net.ParseIP("192.0.2.60")))); err != nil {
		t.Fatalf("a certificate naming the virtual IP refuses %+v", diagnostics.Of(err))
	}
	err := capability.ProveBinding(ctx, "example", block, mgmtMaterial([]byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")))
	refusedWith(t, err, "secret.part", "the certificate of Secret mgmt-tls that the management gateway of StorageCluster example binds cannot be read", "store a readable certificate and its private key with "+gatewaySet+", then apply again")
	err = capability.ProveBinding(ctx, "example", block, map[string]secrets.Material{})
	refusedWith(t, err, "lifecycle.state", "the TLS Secret the management gateway of a StorageCluster binds is not bound to this apply", "")
	plan := cephPlan(t, catalog)
	for _, id := range []string{"ceph-service-example-monitoring", "ceph-cluster-example"} {
		if err := capability.ProveBinding(ctx, "example", blockOf(t, plan, id), nil); err != nil {
			t.Fatalf("%s reads material: %v", id, err)
		}
	}
	object := reconciliation.Block{BlockDefinition: gatewayBlock(t, directTLSCatalog())}
	if err := capability.ProveBinding(ctx, "example", object, nil); err != nil {
		t.Fatalf("an object gateway block under the StorageCluster capability reads material: %v", err)
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
	ctx, capability := context.Background(), New(api.StorageCluster, nil)
	catalog := gatewayCatalog(t, []api.Value{hostGateway("node-1", "node-2", "node-3")}, []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")})
	block := gatewayUnitBlock(t, catalog)
	unnamed := func(err error, host string) {
		t.Helper()
		refusedWith(t, err, "secret.part", "the certificate of Secret mgmt-tls does not name "+host+", a host of the management gateway of StorageCluster example that clients and its readiness proof connect to",
			"store a certificate whose subject alternative names include "+host+" (a DNS name or a matching wildcard) with "+gatewaySet+", or place the management gateway only on hosts the certificate names, then apply again")
	}
	unnamed(capability.ProveBinding(ctx, "example", block, mgmtMaterial(leafPEM(t, []string{"node-1", "node-2"}))), "node-3")
	unnamed(capability.ProveBinding(ctx, "example", block, mgmtMaterial(leafPEM(t, nil, net.ParseIP("192.0.2.1")))), "node-1")
	if err := capability.ProveBinding(ctx, "example", block, mgmtMaterial(leafPEM(t, []string{"node-1", "node-2", "node-3"}))); err != nil {
		t.Fatalf("a certificate naming every host refuses %+v", diagnostics.Of(err))
	}
	qualified := planCatalog(3, 3, func(spec api.Value) api.Value {
		nodes := spec.Get("ceph", "topology", "nodes").Items()
		for i, node := range nodes {
			nodes[i] = node.With("fqdn", api.StringValue(node.Get("name").Text()+".ceph.example.test"))
		}
		spec = spec.WithPath(api.ListValue(nodes...), "ceph", "topology", "nodes")
		spec = spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), mgmtGateway(requestMap("port", 9443)))...), "ceph", "services")
		return spec.WithPath(api.ListValue(tlsBinding("mgmt-gateway", "mgmt-tls")), "ceph", "serviceBindings")
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
			if unit.Kind != string(api.StorageCluster) || unit.Object != "example" || unit.Implementation != Implementation || unit.Stage != reconciliation.StageClusters ||
				!slices.Equal(unit.Exclusive, []string{"ceph/example"}) || len(unit.Requires) != 0 || !slices.Equal(unit.Dependencies, c.dependencies) ||
				unit.Description != "realize Ceph service mgmt-gateway of example" || !slices.Equal(unit.Impacts, c.impacts) {
				t.Fatalf("the gateway block is %s %s %v %v %q %q", unit.Kind, unit.Object, unit.Exclusive, unit.Dependencies, unit.Description, unit.Impacts)
			}
		})
	}
}

func TestOAuth2ProxyRefusedAtPlanUntilDecided(t *testing.T) {
	services := []api.Value{mgmtGateway(requestMap("port", 9443, "enable_auth", true)), auxiliaryService("oauth2-proxy", "", requestMap("provider_display_name", "SSO"))}
	bindings := []api.Value{requestMap("serviceRef", "oauth2-proxy", "secrets", api.ListValue(requestMap("path", "/spec/client_secret", "secretRef", "sso-client"),
		requestMap("path", "/spec/cookie_secret", "secretRef", "sso-cookie")))}
	catalog := delegatedCatalog(services, bindings, replicatedPool("volumes"))
	for _, kind := range []api.Kind{api.StorageCluster, api.StoragePool} {
		name := map[api.Kind]string{api.StorageCluster: "example", api.StoragePool: "volumes"}[kind]
		_, err := planned(t, catalog, kind, name)
		refusedWith(t, err, "lifecycle.unsupported", oauth2ProxyReason, oauth2ProxyRemedy)
	}
	if regexp.MustCompile(`\bO?D[0-9]+\b`).MatchString(oauth2ProxyReason + oauth2ProxyRemedy) {
		t.Fatal("the refusal names a decision or question")
	}
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
		object, _ := delegatedCatalog(services, nil).Find(api.StorageCluster, "example")
		return object
	}
	_, err := gatewayUnits(cluster(mgmtGateway(admitted.With("ssl_session_cache", api.StringValue("shared:SSL:10m"))), mgmtIngress("192.0.2.60")))
	refusedWith(t, err, "lifecycle.unsupported", "the native service mgmt-gateway carries spec.ssl_session_cache, which Bootwright does not realize for this service",
		"remove spec.ssl_session_cache from mgmt-gateway in spec.ceph.services")
	_, err = gatewayUnits(cluster(mgmtGateway(admitted), mgmtIngress("192.0.2.60", "frontend_port", 8443)))
	refusedWith(t, err, "lifecycle.unsupported", "the native service ingress.mgmt carries spec.frontend_port, which Bootwright does not realize for this service",
		"remove spec.frontend_port from ingress.mgmt in spec.ceph.services")
}

func TestGatewayBlockIdentityFitsItsCluster(t *testing.T) {
	cluster, _ := delegatedCatalog(vipGateway("192.0.2.60"), nil).Find(api.StorageCluster, "example")
	long := api.NewObject(api.StorageCluster, strings.Repeat("c", 38), api.Value{}, cluster.Spec())
	_, err := gatewayUnits(long)
	refusedWith(t, err, "api.invariant", "StorageCluster "+long.Name()+" has a name too long to identify its management gateway service block, whose identity holds at most 63 characters",
		"rename the StorageCluster to at most 37 characters")
	fits := api.NewObject(api.StorageCluster, strings.Repeat("c", 37), api.Value{}, cluster.Spec())
	units, err := gatewayUnits(fits)
	if err != nil || len(units) != 1 || len(ServiceUnitBlockID(fits.Name(), units[0])) != 63 {
		t.Fatalf("a 37-character cluster plans %+v (%v)", units, err)
	}
}

func TestGatewayUnitDeadline(t *testing.T) {
	for _, hosts := range []int{3, 6} {
		catalog := planCatalog(hosts, hosts, func(spec api.Value) api.Value {
			return spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), vipGateway("192.0.2.60")...)...), "ceph", "services")
		})
		cluster, _ := catalog.Find(api.StorageCluster, "example")
		base, err := New(api.StorageCluster, nil).request(catalog, cluster, "synthetic")
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
		if _, err := New(api.StorageCluster, runner).WithProofs(dependencyProofs{}).run(context.Background(), e, operation, r); err != nil {
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
	if _, err := New(api.StorageCluster, runner).WithProofs(dependencyProofs{}).run(context.Background(), monitoringExecution(t, monitoring), "observe", monitoring); err != nil {
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
	cluster, _ := delegatedCatalog(vipGateway("192.0.2.60"), nil).Find(api.StorageCluster, "example")
	for key, leaf := range gatewayLeaves {
		serviceType, path, _ := strings.Cut(key, " ")
		slot, found := storage.NativeBindingSlot(cluster, serviceType, path)
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
