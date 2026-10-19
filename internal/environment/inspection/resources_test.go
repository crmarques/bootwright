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
