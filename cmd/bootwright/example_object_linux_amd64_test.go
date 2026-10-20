//go:build linux && amd64

package main

import (
	"reflect"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

const objectExample = "libvirt-3-nodes-ceph-object"

// TestExampleObjectPlansTheGatewayBlocks plans the object gateway example
// through the production capabilities: no capability refuses it, and the
// gateway capability plans exactly the eight derived zone pool blocks and the
// gateway block, which depends on each of them.
func TestExampleObjectPlansTheGatewayBlocks(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, objectExample))
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		t.Fatalf("the capabilities refuse %+v", refused)
	}
	definitions := productionDefinitions(t, state, objectExample)
	if _, err := reconciliation.NewPlan(reconciliation.Apply, definitions); err != nil {
		t.Fatal(err)
	}
	var ids []string
	var gateway reconciliation.BlockDefinition
	for _, definition := range definitions {
		if definition.Implementation == ceph.Implementation && definition.Kind == string(api.StorageObjectGateway) {
			ids = append(ids, definition.ID)
			if definition.ID == ceph.GatewayBlockID("objects") {
				gateway = definition
			}
		}
	}
	slices.Sort(ids)
	pools := []string{
		"ceph-gateway-objects-pool-buckets-data", "ceph-gateway-objects-pool-buckets-index", "ceph-gateway-objects-pool-buckets-non-ec",
		"ceph-gateway-objects-pool-control", "ceph-gateway-objects-pool-dedup", "ceph-gateway-objects-pool-log", "ceph-gateway-objects-pool-meta",
		"ceph-gateway-objects-pool-otp",
	}
	if want := append([]string{"ceph-gateway-objects"}, pools...); !slices.Equal(ids, want) {
		t.Fatalf("the gateway capability plans %q, want %q", ids, want)
	}
	for _, pool := range pools {
		if !slices.Contains(gateway.Dependencies, pool) {
			t.Errorf("the gateway block depends on %q, want it to include %q", gateway.Dependencies, pool)
		}
	}
}

// TestExampleObjectServesItsEndpointOverTLS pins the example's direct TLS: the
// gateway request binds both rgw TLS leaves to one generated Secret, and that
// Secret names the endpoint the certificate proof and readiness check.
func TestExampleObjectServesItsEndpointOverTLS(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, objectExample))
	definitions := productionDefinitions(t, state, objectExample)
	index := slices.IndexFunc(definitions, func(d reconciliation.BlockDefinition) bool { return d.ID == ceph.GatewayBlockID("objects") })
	if index < 0 {
		t.Fatal("the example plans no gateway block")
	}
	request, err := ceph.DecodeRequest(definitions[index].Request)
	if err != nil {
		t.Fatal(err)
	}
	want := []ceph.GatewayBinding{{Path: "/spec/ssl_cert", SecretRef: "rgw-objects-tls", ServiceRef: "rgw.objects"}, {Path: "/spec/ssl_key", SecretRef: "rgw-objects-tls", ServiceRef: "rgw.objects"}}
	if request.Gateway == nil || !reflect.DeepEqual(request.Gateway.Bindings, want) || request.Gateway.Endpoint != "s3.lab.example.test" {
		t.Fatalf("the gateway request is %+v, want bindings %+v and endpoint s3.lab.example.test", request.Gateway, want)
	}
	secret := requireObject(t, state.Effective(), api.Secret, "rgw-objects-tls")
	if secret.Spec().Get("type").Text() != "tlsCertificate" || !slices.ContainsFunc(secret.Spec().Get("source", "generated", "dnsNames").Items(), func(v api.Value) bool { return v.Text() == request.Gateway.Endpoint }) {
		t.Fatalf("the Secret rgw-objects-tls does not generate a certificate that names %s", request.Gateway.Endpoint)
	}
}
