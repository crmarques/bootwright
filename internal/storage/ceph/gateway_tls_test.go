package ceph

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func rgwIngress(extra ...any) api.Value {
	return requestMap("service_type", "ingress", "service_id", "rgw.objects", "placement", requestMap("label", "mon", "count", 2),
		"spec", requestMap(append([]any{"backend_service", "rgw.objects", "virtual_ip", "192.0.2.50/24", "frontend_port", 443, "monitor_port", 1967, "ssl", true}, extra...)...))
}

func ingressGatewayCatalog(ingress api.Value) api.Catalog {
	return planCatalog(3, 3, func(spec api.Value) api.Value {
		spec = withCorePair(spec)
		spec = spec.WithPath(api.ListValue(append(spec.Get("services").Items(), httpRGWService("objects"), ingress)...), "services")
		return spec.WithPath(api.ListValue(tlsBinding("ingress.rgw.objects", "gateway-tls")), "serviceBindings")
	}, objectGateway("objects", "endpoint", requestMap("fqdn", "s3.example.test")))
}

func TestGatewayRequestCarriesTheExcludedBindings(t *testing.T) {
	catalog := ingressGatewayCatalog(rgwIngress())
	d := gatewayBlock(t, catalog)
	g := gatewayOf(t, d).Gateway
	want := []GatewayBinding{{Path: "/spec/ssl_cert", SecretRef: "gateway-tls", ServiceRef: "ingress.rgw.objects"}, {Path: "/spec/ssl_key", SecretRef: "gateway-tls", ServiceRef: "ingress.rgw.objects"}}
	if !reflect.DeepEqual(g.Bindings, want) || gatewayTLSSecret(g) != "gateway-tls" {
		t.Fatalf("the gateway froze bindings %+v", g.Bindings)
	}
	cluster, err := planned(t, catalog, api.CephCluster, "example")
	if err != nil {
		t.Fatal(err)
	}
	data, err := cluster.Canonical()
	if err != nil || strings.Contains(string(data), "gateway-tls") || strings.Contains(string(data), "serviceBindings") {
		t.Fatalf("the cluster request carries the gateway's bindings (%v)", err)
	}
	matchesGolden(t, "request-gateway-ingress", d.Request, false)
}

func TestGatewayBindingsSortByServiceThenPath(t *testing.T) {
	catalog := planCatalog(3, 3, func(spec api.Value) api.Value {
		spec = withCorePair(spec)
		spec = spec.WithPath(api.ListValue(append(spec.Get("services").Items(), rgwService("objects"), rgwIngress())...), "services")
		reversed := requestMap("serviceRef", "rgw.objects", "secrets", api.ListValue(
			requestMap("path", "/spec/ssl_key", "secretRef", "backend-tls"), requestMap("path", "/spec/ssl_cert", "secretRef", "backend-tls")))
		return spec.WithPath(api.ListValue(reversed, tlsBinding("ingress.rgw.objects", "gateway-tls")), "serviceBindings")
	}, objectGateway("objects", "endpoint", requestMap("fqdn", "s3.example.test")))
	g := gatewayOf(t, gatewayBlock(t, catalog)).Gateway
	want := []GatewayBinding{{Path: "/spec/ssl_cert", SecretRef: "gateway-tls", ServiceRef: "ingress.rgw.objects"}, {Path: "/spec/ssl_key", SecretRef: "gateway-tls", ServiceRef: "ingress.rgw.objects"},
		{Path: "/spec/ssl_cert", SecretRef: "backend-tls", ServiceRef: "rgw.objects"}, {Path: "/spec/ssl_key", SecretRef: "backend-tls", ServiceRef: "rgw.objects"}}
	if !reflect.DeepEqual(g.Bindings, want) {
		t.Fatalf("the gateway froze bindings %+v", g.Bindings)
	}
}

func TestGatewayFreezesTheIngressThatFrontsItsRGW(t *testing.T) {
	type document struct {
		ServiceID   string         `json:"service_id"`
		ServiceType string         `json:"service_type"`
		Spec        map[string]any `json:"spec"`
	}
	want := map[string]any{"backend_service": "rgw.objects", "frontend_port": float64(443), "monitor_port": float64(1967), "ssl": true, "virtual_ip": "192.0.2.50/24"}
	for name, ingress := range map[string]api.Value{"derived": rgwIngress(), "inline": rgwIngress("certificate_source", "inline")} {
		g := gatewayOf(t, gatewayBlock(t, ingressGatewayCatalog(ingress))).Gateway
		if len(g.Documents) != 2 {
			t.Fatalf("%s: the gateway froze %d documents", name, len(g.Documents))
		}
		var rgw, fronting document
		if json.Unmarshal(g.Documents[0], &rgw) != nil || json.Unmarshal(g.Documents[1], &fronting) != nil || rgw.ServiceType != "rgw" || rgw.ServiceID != "objects" ||
			fronting.ServiceType != "ingress" || fronting.ServiceID != "rgw.objects" {
			t.Fatalf("%s: the gateway documents %s %s", name, g.Documents[0], g.Documents[1])
		}
		expected := map[string]any{}
		for k, v := range want {
			expected[k] = v
		}
		if name == "inline" {
			expected["certificate_source"] = "inline"
		}
		if !reflect.DeepEqual(fronting.Spec, expected) {
			t.Fatalf("%s: the ingress document froze spec %v, want %v", name, fronting.Spec, expected)
		}
	}
}

func TestGatewayTLSNeedsAnEndpoint(t *testing.T) {
	catalogWith := func(gateway api.Object) api.Catalog {
		return planCatalog(3, 3, func(spec api.Value) api.Value {
			spec = withPairAndRGW(spec)
			return spec.WithPath(api.ListValue(tlsBinding("rgw.objects", "gateway-tls")), "serviceBindings")
		}, gateway)
	}
	_, err := kindPlan(api.CephObjectGateway, catalogWith(objectGateway("objects")))
	refusedWith(t, err, "api.invariant", "CephObjectGateway objects terminates TLS at rgw.objects but declares no spec.endpoint, whose FQDN its certificate must name",
		"declare spec.endpoint on CephObjectGateway objects: spec.endpoint.fqdn with the name its clients use, or spec.endpoint: {} to derive it from the gateway, cluster and Environment storage domain")
	g := gatewayOf(t, gatewayBlock(t, catalogWith(objectGateway("objects", "endpoint", requestMap("fqdn", "s3.example.test"))))).Gateway
	want := []GatewayBinding{{Path: "/spec/ssl_cert", SecretRef: "gateway-tls", ServiceRef: "rgw.objects"}, {Path: "/spec/ssl_key", SecretRef: "gateway-tls", ServiceRef: "rgw.objects"}}
	if !reflect.DeepEqual(g.Bindings, want) || g.Endpoint != "s3.example.test" || len(g.Documents) != 1 {
		t.Fatalf("the TLS gateway froze %+v", g)
	}
}

func TestGatewayEndpointEvidence(t *testing.T) {
	plain := gatewayRuntimeRequest()
	tls := gatewayRuntimeRequest()
	tls.Gateway.Bindings = []GatewayBinding{{Path: "/spec/ssl_cert", SecretRef: "gateway-tls", ServiceRef: "rgw.objects"}, {Path: "/spec/ssl_key", SecretRef: "gateway-tls", ServiceRef: "rgw.objects"}}
	pool := runtimeRequest()
	pool.Action, pool.Pool = "pool", &Pool{Name: "objects.rgw.meta"}
	pooled := readyEvidence(pool)
	pooled.Resource = ResourceEvidence{ID: "7"}
	absent := Evidence{Version: EvidenceVersion, Request: "digest", Action: plain.Action, Owner: plain.Owner, FSID: plain.FSID, Owned: true, Hosts: hostNames(plain),
		Monitors: []string{}, MGRStandbys: []string{}, MonAddrs: []MonitorAddress{}, OSDs: []OSDEvidence{}, Daemons: []UnitDaemon{}}
	digest := strings.Repeat("a", 64)
	for name, c := range map[string]struct {
		request  Request
		evidence Evidence
		want     reconciliation.EffectState
	}{
		"plain reachable":            {plain, gatewayEvidence(plain), reconciliation.EffectCompleted},
		"tls with its digest":        {tls, endpointOf(gatewayEvidence(tls), EndpointEvidence{CertificateSHA256: digest, Reachable: true}), reconciliation.EffectCompleted},
		"plain unreachable":          {plain, endpointOf(gatewayEvidence(plain), EndpointEvidence{}), reconciliation.EffectUnknown},
		"plain with a digest":        {plain, endpointOf(gatewayEvidence(plain), EndpointEvidence{CertificateSHA256: digest, Reachable: true}), reconciliation.EffectUnknown},
		"tls without a digest":       {tls, gatewayEvidence(tls), reconciliation.EffectUnknown},
		"tls with an uppercase hash": {tls, endpointOf(gatewayEvidence(tls), EndpointEvidence{CertificateSHA256: strings.Repeat("A", 64), Reachable: true}), reconciliation.EffectUnknown},
		"pool reachable":             {pool, endpointOf(pooled, EndpointEvidence{Reachable: true}), reconciliation.EffectUnknown},
		"absent with a digest":       {tls, endpointOf(absent, EndpointEvidence{CertificateSHA256: digest}), reconciliation.EffectUnknown},
	} {
		effect, _ := EvaluateEvidence(evidenceBytes(t, c.evidence), c.request, "digest", false)
		if effect != c.want {
			t.Errorf("%s: gateway endpoint evidence is %s, want %s", name, effect, c.want)
		}
	}
	if effect, err := EvaluateEvidence(evidenceBytes(t, pooled), pool, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("pool evidence without an endpoint is %s (%v)", effect, err)
	}
}

func endpointOf(e Evidence, value EndpointEvidence) Evidence {
	e.Endpoint = value
	return e
}
