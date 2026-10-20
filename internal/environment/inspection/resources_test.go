package inspection

import (
	"context"
	"errors"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestClusterInfoCarriesResourceRows(t *testing.T) {
	f := newFixture(t)
	f.catalog = api.NewCatalog(append(f.catalog.Objects(), storageCluster("backup", "managed", "rhel-01", node("rhel-01", "rhel-01.lab.test", "machine-a"))))
	ready := []Resource{
		{Kind: "StoragePool", Name: "rbd-data", Status: ResourceDone, Scope: "replicated size 2/min 2, application rbd"},
		{Kind: "StorageClient", Name: "rbd-canary", Status: ResourceDone, Scope: "client.rbd-canary read-write on pool rbd-data"},
	}
	unavailable := []Resource{{Kind: "StoragePool", Name: "cold", Status: ResourcePending}}
	f.rows = map[string][]Resource{"storage": ready, "backup": unavailable}
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]Resource{}
	for _, entry := range result.Storage {
		if entry.Ceph != nil {
			got[entry.Name+"/"+entry.Ceph.Status] = entry.Ceph.Resources
		}
	}
	want := map[string][]Resource{"storage/" + StatusReady: ready, "backup/" + StatusUnavailable: unavailable}
	if !reflect.DeepEqual(got, want) || f.rowCalls != 1 {
		t.Fatalf("resources = %+v after %d reads, want %+v after one", got, f.rowCalls, want)
	}
	f.rows = nil
	if result, err = f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}}); err != nil ||
		result.Storage[0].Ceph.Resources == nil || len(result.Storage[0].Ceph.Resources) != 0 || f.rowCalls != 2 {
		t.Fatalf("a cluster without rows = %+v (%v) after %d reads", result, err, f.rowCalls)
	}
	external := newFixture(t)
	if _, err := external.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"archive", "edge"}}); err != nil || external.rowCalls != 0 {
		t.Fatalf("an external-only selection read resources %d times (%v)", external.rowCalls, err)
	}
	failing := newFixture(t)
	failing.rowError = errors.New("the lifecycle state is unreadable")
	if _, err := failing.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{}); !errors.Is(err, failing.rowError) {
		t.Fatalf("a resource read failure = %v", err)
	}
}

func TestClusterInfoCarriesEndpointRows(t *testing.T) {
	f := newFixture(t)
	f.catalog = api.NewCatalog(append(f.catalog.Objects(), storageCluster("backup", "managed", "rhel-01", node("rhel-01", "rhel-01.lab.test", "machine-a"))))
	endpoints := []Endpoint{
		{Service: "mgmt-gateway", URLs: []string{"https://192.0.2.60:9443/"}, CertificateSHA256: "ab", Issuer: IssuerCephadm, Status: EndpointReady},
		{Service: "nfs.files", URLs: []string{"nfs://192.0.2.50:2049/"}, Status: EndpointUnproved, Reason: "not proved"},
	}
	proof := f.cephFor["storage"]
	proof.Endpoints = endpoints
	f.cephFor["storage"] = proof
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]Endpoint{}
	for _, entry := range result.Storage {
		if entry.Ceph != nil {
			got[entry.Name+"/"+entry.Ceph.Status] = entry.Ceph.Endpoints
		}
	}
	if len(got) != 2 || !reflect.DeepEqual(got["storage/"+StatusReady], endpoints) || got["backup/"+StatusUnavailable] == nil || len(got["backup/"+StatusUnavailable]) != 0 {
		t.Fatalf("endpoints = %+v, want the proof's rows on the ready entry and [] on the unavailable one", got)
	}
	proof.Endpoints = nil
	f.cephFor["storage"] = proof
	if result, err = f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}}); err != nil ||
		result.Storage[0].Ceph.Endpoints == nil || len(result.Storage[0].Ceph.Endpoints) != 0 {
		t.Fatalf("a proof without endpoints = %#v (%v)", result.Storage[0].Ceph.Endpoints, err)
	}
}
