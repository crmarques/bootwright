package ceph

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type endpointFixture struct {
	blocks []reconciliation.Block
	proofs []lifecycle.BlockEvidence
	count  int
}

func (f *endpointFixture) block(t *testing.T, kind, object string, r Request) reconciliation.Block {
	t.Helper()
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	f.count++
	return reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: fmt.Sprintf("block-%d", f.count), Kind: kind, Object: object, Implementation: Implementation, Request: data},
		RequestDigest:   strings.Repeat(fmt.Sprintf("%x", f.count), 64)[:64],
	}
}

func (f *endpointFixture) service(t *testing.T, unit ServiceUnit) reconciliation.Block {
	t.Helper()
	r := runtimeRequest()
	r.Action, r.Service = "service", &unit
	block := f.block(t, string(api.StorageCluster), "example", r)
	f.blocks = append(f.blocks, block)
	return block
}

func (f *endpointFixture) gateway(t *testing.T, object string, r Request) reconciliation.Block {
	t.Helper()
	block := f.block(t, string(api.StorageObjectGateway), object, r)
	f.blocks = append(f.blocks, block)
	return block
}

func (f *endpointFixture) prove(t *testing.T, block reconciliation.Block, mutate func(*Evidence)) {
	t.Helper()
	r, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	e := readyEvidence(r)
	e.Request = block.RequestDigest
	if mutate != nil {
		mutate(&e)
	}
	f.proofs = append(f.proofs, lifecycle.BlockEvidence{Kind: block.Kind, Object: block.Object, Implementation: block.Implementation,
		Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidenceBytes(t, e)})
}

func reached(digest string) func(*Evidence) {
	return func(e *Evidence) { e.Endpoint = EndpointEvidence{CertificateSHA256: digest, Reachable: true} }
}

func endpointRow(rows []EndpointInspection, service string) EndpointInspection {
	for _, row := range rows {
		if row.Service == service {
			return row
		}
	}
	return EndpointInspection{Service: "missing " + service}
}

func plainGatewayRequest(t *testing.T) Request {
	t.Helper()
	return gatewayOf(t, gatewayBlock(t, planCatalog(3, 3, withPairAndHTTPRGW, objectGateway("objects"))))
}

func nfsUnitWithIngress(t *testing.T) ServiceUnit {
	t.Helper()
	units := unitsOf(t, haproxyProtocolNFS()...)
	if len(units) != 1 {
		t.Fatalf("nfs units %+v", units)
	}
	return units[0]
}

func TestClusterInfoEndpoints(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	bound := gatewayUnitOf(t, gatewayCatalog(t, vipGateway("192.0.2.60"), []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")}))
	f := &endpointFixture{}
	mgmt := f.service(t, bound)
	f.prove(t, mgmt, reached(digest))
	rows := InspectEndpoints("example", f.blocks, f.proofs)
	want := EndpointInspection{Service: "mgmt-gateway", URLs: []string{"https://192.0.2.60:9443/"}, CertificateSHA256: digest, Issuer: IssuerBound, Status: EndpointReady}
	if len(rows) != 1 || fmt.Sprint(rows[0]) != fmt.Sprint(want) {
		t.Fatalf("bound virtual-IP management gateway: %+v want %+v", rows, want)
	}

	t.Run("host placement is cephadm-signed and sorted", func(t *testing.T) {
		f := &endpointFixture{}
		unit := gatewayUnitOf(t, gatewayCatalog(t, []api.Value{hostGateway("node-3", "node-1", "node-2")}, nil))
		block := f.service(t, unit)
		f.prove(t, block, reached(digest))
		row := endpointRow(InspectEndpoints("example", f.blocks, f.proofs), "mgmt-gateway")
		if !slices.Equal(row.URLs, []string{"https://node-1:9443/", "https://node-2:9443/", "https://node-3:9443/"}) || row.Issuer != IssuerCephadm || row.Status != EndpointReady {
			t.Fatalf("host-placed gateway %+v", row)
		}
	})

	t.Run("an IPv6 virtual IP is bracketed", func(t *testing.T) {
		f := &endpointFixture{}
		block := f.service(t, gatewayUnitOf(t, gatewayCatalog(t, vipGateway("2001:db8::60"), nil)))
		f.prove(t, block, reached(digest))
		row := endpointRow(InspectEndpoints("example", f.blocks, f.proofs), "mgmt-gateway")
		if !slices.Equal(row.URLs, []string{"https://[2001:db8::60]:9443/"}) {
			t.Fatalf("IPv6 gateway %+v", row)
		}
	})

	t.Run("an unreached endpoint stays unproved", func(t *testing.T) {
		f := &endpointFixture{}
		block := f.service(t, bound)
		f.prove(t, block, nil)
		row := endpointRow(InspectEndpoints("example", f.blocks, f.proofs), "mgmt-gateway")
		if row.Status != EndpointUnproved || row.Reason != unreachedEndpoint || row.CertificateSHA256 != "" || row.Issuer != IssuerBound {
			t.Fatalf("done but unreached %+v", row)
		}
	})

	t.Run("a failed block or another request digest proves nothing", func(t *testing.T) {
		for name, mutate := range map[string]func(*endpointFixture){
			"failed": func(f *endpointFixture) { f.proofs[0].State = reconciliation.BlockFailed },
			"digest": func(f *endpointFixture) {
				e, err := decodeEvidence(f.proofs[0].Evidence)
				if err != nil {
					t.Fatal(err)
				}
				e.Request = strings.Repeat("0", 64)
				f.proofs[0].Evidence = evidenceBytes(t, e)
			},
			"none": func(f *endpointFixture) { f.proofs = nil },
		} {
			f := &endpointFixture{}
			block := f.service(t, bound)
			f.prove(t, block, reached(digest))
			mutate(f)
			row := endpointRow(InspectEndpoints("example", f.blocks, f.proofs), "mgmt-gateway")
			if row.Status != EndpointUnproved || row.Reason != noEndpointProof || row.CertificateSHA256 != "" {
				t.Fatalf("%s: %+v", name, row)
			}
		}
	})

	t.Run("object gateways", func(t *testing.T) {
		f := &endpointFixture{}
		tls := gatewayOf(t, gatewayBlock(t, directTLSCatalog()))
		secure := f.gateway(t, "secure", tls)
		f.prove(t, secure, reached(digest))
		plain := f.gateway(t, "plain", plainGatewayRequest(t))
		f.prove(t, plain, reached(digest))
		rows := InspectEndpoints("example", f.blocks, f.proofs)
		if len(rows) != 2 {
			t.Fatalf("gateway rows %+v", rows)
		}
		for _, row := range rows {
			switch row.Issuer {
			case IssuerBound:
				if row.CertificateSHA256 != digest || row.Status != EndpointReady || !slices.Equal(row.URLs, []string{"https://s3.example.test:443/"}) {
					t.Fatalf("tls gateway %+v", row)
				}
			case "":
				if row.CertificateSHA256 != "" || row.Status != EndpointReady || !slices.Equal(row.URLs, []string{"http://node-1:80/", "http://node-2:80/", "http://node-3:80/"}) {
					t.Fatalf("plain gateway %+v", row)
				}
			default:
				t.Fatalf("unexpected issuer in %+v", row)
			}
		}
	})

	t.Run("NFS", func(t *testing.T) {
		f := &endpointFixture{}
		fronted := f.service(t, nfsUnitWithIngress(t))
		f.prove(t, fronted, nil)
		rows := InspectEndpoints("example", f.blocks, f.proofs)
		if len(rows) != 1 || rows[0].Service != "nfs.files" || !slices.Equal(rows[0].URLs, []string{"nfs://192.0.2.50:2049/"}) || rows[0].Status != EndpointReady ||
			rows[0].Reason != "" || rows[0].Issuer != "" || rows[0].CertificateSHA256 != "" {
			t.Fatalf("fronted NFS %+v", rows)
		}
		bare := &endpointFixture{}
		unit := unitsOf(t, nfsService("files"))[0]
		block := bare.service(t, unit)
		bare.prove(t, block, reached(digest))
		row := endpointRow(InspectEndpoints("example", bare.blocks, bare.proofs), "nfs.files")
		if row.Status != EndpointUnproved || row.Reason != unprobedNFS || len(row.URLs) == 0 || !strings.HasPrefix(row.URLs[0], "nfs://") || row.CertificateSHA256 != "" {
			t.Fatalf("unfronted NFS %+v", row)
		}
	})

	t.Run("an object gateway behind an ingress follows the ingress", func(t *testing.T) {
		catalog := planCatalog(3, 3, func(spec api.Value) api.Value {
			spec = withCorePair(spec)
			rgw := httpRGWService("objects").WithPath(api.IntegerValue("8080"), "spec", "rgw_frontend_port")
			ingress := rgwIngress().WithPath(api.IntegerValue("8443"), "spec", "frontend_port")
			spec = spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), rgw, ingress)...), "ceph", "services")
			return spec.WithPath(api.ListValue(tlsBinding("ingress.rgw.objects", "gateway-tls")), "ceph", "serviceBindings")
		}, objectGateway("objects", "endpoint", requestMap("fqdn", "s3.example.test")))
		request := gatewayOf(t, gatewayBlock(t, catalog))
		f := &endpointFixture{}
		f.prove(t, f.gateway(t, "named", request), reached(digest))
		row := endpointRow(InspectEndpoints("example", f.blocks, f.proofs), "rgw.objects")
		if !slices.Equal(row.URLs, []string{"https://s3.example.test:8443/"}) || row.Issuer != IssuerBound || row.CertificateSHA256 != digest || row.Status != EndpointReady {
			t.Fatalf("fronted gateway %+v", row)
		}
		request.Gateway.Endpoint = ""
		unnamed := &endpointFixture{}
		unnamed.gateway(t, "unnamed", request)
		row = endpointRow(InspectEndpoints("example", unnamed.blocks, nil), "rgw.objects")
		if !slices.Equal(row.URLs, []string{"https://192.0.2.50:8443/"}) {
			t.Fatalf("fronted gateway without an endpoint name %+v", row)
		}
	})

	t.Run("an NFS service behind a keepalive-only ingress keeps its own port", func(t *testing.T) {
		services := haproxyProtocolNFS()
		services[1] = services[1].WithPath(api.BoolValue(true), "spec", "keepalive_only")
		f := &endpointFixture{}
		f.prove(t, f.service(t, unitsOf(t, services...)[0]), nil)
		row := endpointRow(InspectEndpoints("example", f.blocks, f.proofs), "nfs.files")
		if !slices.Equal(row.URLs, []string{"nfs://192.0.2.50:12049/"}) || row.Status != EndpointReady {
			t.Fatalf("keepalive-only NFS %+v", row)
		}
	})

	t.Run("a foreign cluster and an unthawable request add no row", func(t *testing.T) {
		f := &endpointFixture{}
		foreign := plainGatewayRequest(t)
		foreign.Cluster = "other"
		f.gateway(t, "foreign", foreign)
		f.blocks = append(f.blocks, reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "x", Kind: string(api.StorageObjectGateway), Object: "broken", Implementation: Implementation, Request: []byte("{")}})
		f.gateway(t, "plain", plainGatewayRequest(t))
		if names := EndpointGateways("example", f.blocks); !slices.Equal(names, []string{"plain"}) {
			t.Fatalf("gateways %v", names)
		}
		if rows := InspectEndpoints("example", f.blocks, nil); len(rows) != 1 || rows[0].Reason != noEndpointProof {
			t.Fatalf("rows %+v", rows)
		}
	})

	t.Run("rows are ordered by service", func(t *testing.T) {
		f := &endpointFixture{}
		f.gateway(t, "plain", plainGatewayRequest(t))
		f.service(t, nfsUnitWithIngress(t))
		f.service(t, bound)
		var services []string
		for _, row := range InspectEndpoints("example", f.blocks, nil) {
			services = append(services, row.Service)
		}
		if !slices.Equal(services, []string{"mgmt-gateway", "nfs.files", "rgw.objects"}) {
			t.Fatalf("services %v", services)
		}
	})

	if rows := InspectEndpoints("example", nil, nil); rows == nil || len(rows) != 0 {
		t.Fatalf("no blocks gave %#v", rows)
	}
}
