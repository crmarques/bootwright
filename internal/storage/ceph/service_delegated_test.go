package ceph

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func rgwService(id string) api.Value {
	return requestMap("service_type", "rgw", "service_id", id, "placement", requestMap("label", "mon", "count", 2), "spec", requestMap("ssl", true))
}

func ingressService(backend string) api.Value {
	return requestMap("service_type", "ingress", "service_id", backend, "placement", requestMap("label", "mon", "count", 2),
		"spec", requestMap("backend_service", backend, "virtual_ip", "192.0.2.50/24", "ssl", true))
}

func nfsService(id string) api.Value {
	return requestMap("service_type", "nfs", "service_id", id, "placement", requestMap("label", "mon", "count", 2))
}

func tlsBinding(ref, secret string) api.Value {
	return requestMap("serviceRef", ref, "secrets", api.ListValue(
		requestMap("path", "/spec/ssl_cert", "secretRef", secret), requestMap("path", "/spec/ssl_key", "secretRef", secret)))
}

func delegatedCatalog(services, bindings []api.Value, children ...api.Object) api.Catalog {
	return planCatalog(3, 3, func(spec api.Value) api.Value {
		spec = spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), services...)...), "ceph", "services")
		if len(bindings) != 0 {
			spec = spec.WithPath(api.ListValue(bindings...), "ceph", "serviceBindings")
		}
		return spec
	}, children...)
}

func gatewayGraph(bindings ...api.Value) api.Catalog {
	objects := delegatedCatalog([]api.Value{rgwService("objects"), ingressService("rgw.objects")}, bindings,
		childOf(api.StorageObjectGateway, "objects", "serviceRef", "rgw.objects"), replicatedPool("volumes"),
		childOf(api.StorageClient, "consumer", "poolRef", "volumes", "access", "read-write")).Objects()
	for i, object := range objects {
		if object.Kind() == api.StorageCluster {
			objects[i] = object.WithSpec(withCorePair(object.Spec()))
		}
	}
	return api.NewCatalog(objects)
}

func TestDelegatedServicesProjectIntoDelegated(t *testing.T) {
	r, err := clusterRequestWith(t, func(services []api.Value) []api.Value {
		return append(services, rgwService("objects"), ingressService("rgw.objects"), nfsService("files"))
	})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	got := []string{}
	for _, raw := range r.Services {
		var service nativeService
		if err := json.Unmarshal(raw, &service); err != nil {
			t.Fatal(err)
		}
		got = append(got, service.ServiceType)
	}
	if !slices.Equal(got, []string{"mon", "mgr", "osd"}) {
		t.Fatalf("cluster services %v", got)
	}
	hosts := []string{"node-1", "node-2", "node-3"}
	want := []DelegatedService{{Name: "crash", Hosts: hosts}, {Name: "ingress.rgw.objects", Hosts: hosts}, {Name: "nfs.files", Hosts: hosts}, {Name: "rgw.objects", Hosts: hosts}}
	if !reflect.DeepEqual(r.Delegated, want) {
		t.Fatalf("delegated %+v, want %+v", r.Delegated, want)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"192.0.2.50", "backend_service", "virtual_ip", "ssl"} {
		if bytes.Contains(data, []byte(leak)) {
			t.Fatalf("the core request carries %q of a delegated service", leak)
		}
	}
}

func TestClusterRequestRefusesOnlyUnexecutedBindings(t *testing.T) {
	catalog := gatewayGraph(tlsBinding("rgw.objects", "gateway-tls"), tlsBinding("ingress.rgw.objects", "gateway-tls"))
	r, err := planned(t, catalog, api.StorageCluster, "example")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	data, err := r.Canonical()
	if err != nil || bytes.Contains(data, []byte("serviceBindings")) {
		t.Fatalf("the core request carries a binding: %v", err)
	}
	grafanaTLS := requestMap("serviceRef", "grafana", "secrets", api.ListValue(requestMap("path", "/spec/ssl_cert", "secretRef", "gateway-tls")))
	for name, c := range map[string]struct {
		ref      string
		services []api.Value
		binding  api.Value
	}{
		"nfs.files":               {"nfs.files", []api.Value{nfsService("files")}, tlsBinding("nfs.files", "gateway-tls")},
		"ingress.nfs.files":       {"ingress.nfs.files", []api.Value{nfsService("files"), ingressService("nfs.files")}, tlsBinding("ingress.nfs.files", "gateway-tls")},
		"rgw.other":               {"rgw.other", []api.Value{rgwService("other")}, tlsBinding("rgw.other", "gateway-tls")},
		"undeclared grafana":      {"grafana", nil, tlsBinding("grafana", "gateway-tls")},
		"grafana TLS binding":     {"grafana", []api.Value{auxiliaryService("grafana", "", api.Value{})}, grafanaTLS},
		"grafana password beside": {"grafana", []api.Value{auxiliaryService("grafana", "", api.Value{})}, passwordAndTLS()},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := delegatedCatalog(append([]api.Value{rgwService("objects")}, c.services...), []api.Value{tlsBinding("rgw.objects", "gateway-tls"), c.binding},
				childOf(api.StorageObjectGateway, "objects", "serviceRef", "rgw.objects"))
			_, err := planned(t, catalog, api.StorageCluster, "example")
			refusedWith(t, err, "lifecycle.unsupported", "Bootwright does not realize the service binding of "+c.ref+"; it realizes only the TLS bindings of an rgw service that a StorageObjectGateway binds and of the ingress that fronts it, the TLS binding of the management gateway, the credential, TLS and CA bundle bindings of its oauth2-proxy, and the credential bindings of the grafana and snmp-gateway services",
				"remove the spec.ceph.serviceBindings entry for "+c.ref)
		})
	}
}

func passwordAndTLS() api.Value {
	return requestMap("serviceRef", "grafana", "secrets", api.ListValue(requestMap("path", "/spec/initial_admin_password", "secretRef", "grafana-admin"),
		requestMap("path", "/spec/ssl_cert", "secretRef", "gateway-tls")))
}

func TestClusterRequestNeverCarriesSecretLeaves(t *testing.T) {
	catalog := gatewayGraph(tlsBinding("rgw.objects", "gateway-tls"), tlsBinding("ingress.rgw.objects", "gateway-tls"))
	for _, object := range []struct {
		kind api.Kind
		name string
	}{{api.StorageCluster, "example"}, {api.StoragePool, "volumes"}, {api.StorageClient, "consumer"}} {
		r, err := planned(t, catalog, object.kind, object.name)
		if err != nil {
			t.Fatal(object.kind, diagnostics.Of(err))
		}
		if len(r.Delegated) != 3 {
			t.Fatalf("%s request lists %+v", object.kind, r.Delegated)
		}
		data, err := r.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		for _, leak := range []string{"ssl_cert", "ssl_key", "gateway-tls", "secrets"} {
			if bytes.Contains(data, []byte(leak)) {
				t.Fatalf("the %s request carries %q", object.kind, leak)
			}
		}
	}
}

func TestDelegatedServicesOutsideClusterScope(t *testing.T) {
	plain, err := clusterRequestWith(t, func(services []api.Value) []api.Value { return services })
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	withRGW, err := clusterRequestWith(t, func(services []api.Value) []api.Value { return append(services, rgwService("objects")) })
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	before, _ := plain.Canonical()
	after, _ := withRGW.Canonical()
	if bytes.Equal(before, after) {
		t.Fatal("a delegated service left the core request unchanged")
	}
	hostBefore, errBefore := HostRequest{Version: HostVersion, Cluster: scopeOf(plain), Host: "node-1"}.canonical()
	hostAfter, errAfter := HostRequest{Version: HostVersion, Cluster: scopeOf(withRGW), Host: "node-1"}.canonical()
	if errBefore != nil || errAfter != nil || !bytes.Equal(hostBefore, hostAfter) {
		t.Fatalf("a delegated service changed the host request: %v %v", errBefore, errAfter)
	}
}

func TestClusterDeadlineUnchangedByDelegatedServices(t *testing.T) {
	r, err := clusterRequestWith(t, func(services []api.Value) []api.Value {
		return append(services, rgwService("objects"), ingressService("rgw.objects"), nfsService("files"), mdsRequestService("files"))
	})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if got, err := cephDeadline(r, "apply"); err != nil || got != 16260*time.Second {
		t.Fatalf("cluster deadline %v (%v), want 16260s", got, err)
	}
}

func TestDelegatedServiceRefusesUntilItsBlockLands(t *testing.T) {
	graph := func(services []api.Value, children ...api.Object) *compilation.State {
		catalog := pinnedCatalog(t, func(spec api.Value) api.Value {
			return spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), services...)...), "ceph", "services")
		})
		catalog = api.NewCatalog(append(catalog.Objects(), children...))
		return compilation.NewState(catalog, catalog, nil)
	}
	if got := New(api.StorageCluster, nil).Unsupported(graph(nil)); len(got) != 0 {
		t.Fatalf("the plain graph refuses %+v", got)
	}
	if got := New(api.StorageCluster, nil).Unsupported(graph([]api.Value{nfsService("files")})); len(got) != 0 {
		t.Fatalf("a direct nfs service refuses %+v", got)
	}
	gateway := childOf(api.StorageObjectGateway, "objects", "serviceRef", "rgw.objects")
	if got := New(api.StorageCluster, nil).Unsupported(graph([]api.Value{httpRGWService("objects")}, gateway)); len(got) != 0 {
		t.Fatalf("a plain rgw service a gateway binds refuses %+v", got)
	}
	if got := New(api.StorageCluster, nil).Unsupported(graph([]api.Value{rgwService("objects")}, gateway)); len(got) != 0 {
		t.Fatalf("an ssl rgw service a gateway binds refuses %+v", got)
	}
	want := []lifecycle.Refusal{{Kind: string(api.StorageCluster), Name: "example",
		Reason:      "the ingress service ingress.rgw.objects needs cephadm's container_image_haproxy, and spec.ceph.images.haproxy is not set; Bootwright keeps no default",
		Remediation: "set spec.ceph.images.haproxy of StorageCluster example to an image beneath its Entitlement registry, or remove ingress.rgw.objects from its spec.ceph.services, together with its spec.ceph.serviceBindings entries"}}
	if got := New(api.StorageCluster, nil).Unsupported(graph([]api.Value{httpRGWService("objects"), ingressService("rgw.objects")}, gateway)); !reflect.DeepEqual(got, want) {
		t.Fatalf("ingress.rgw.objects: refuses %+v, want %+v", got, want)
	}
	nfsIngress := requestMap("service_type", "ingress", "service_id", "nfs.files", "placement", requestMap("label", "mon", "count", 2),
		"spec", requestMap("backend_service", "nfs.files", "virtual_ip", "192.0.2.50/24", "frontend_port", 2049, "monitor_port", 9049, "enable_haproxy_protocol", true))
	want = []lifecycle.Refusal{{Kind: string(api.StorageCluster), Name: "example",
		Reason:      "the ingress service ingress.nfs.files needs cephadm's container_image_haproxy, and spec.ceph.images.haproxy is not set; Bootwright keeps no default",
		Remediation: "set spec.ceph.images.haproxy of StorageCluster example to an image beneath its Entitlement registry, or remove ingress.nfs.files from its spec.ceph.services, together with its spec.ceph.serviceBindings entries"}}
	if got := New(api.StorageCluster, nil).Unsupported(graph([]api.Value{nfsService("files"), nfsIngress})); !reflect.DeepEqual(got, want) {
		t.Fatalf("ingress.nfs.files: refuses %+v, want %+v", got, want)
	}
}
