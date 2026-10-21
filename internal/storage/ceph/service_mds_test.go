package ceph

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func mdsRequestService(id string) api.Value {
	return requestMap("service_type", "mds", "service_id", id, "placement", requestMap("label", "mon", "count", 2))
}

func clusterRequestWith(t *testing.T, services func([]api.Value) []api.Value) (Request, error) {
	t.Helper()
	catalog := poolRequestCatalog(3, 3)
	cluster, _ := catalog.Find(api.StorageCluster, "example")
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services(cluster.Spec().Get("ceph", "services").Items())...), "ceph", "services"))
	objects := catalog.Objects()
	for i := range objects {
		if objects[i].Identity() == changed.Identity() {
			objects[i] = changed
		}
	}
	return New(api.StorageCluster, nil).request(api.NewCatalog(objects), changed, "synthetic")
}

func TestProjectServicesKeepsMDSIdentityAndHosts(t *testing.T) {
	r, err := clusterRequestWith(t, func(services []api.Value) []api.Value { return append(services, mdsRequestService("files")) })
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(r.Services) != 4 {
		t.Fatalf("projected services: %d", len(r.Services))
	}
	matchesGolden(t, "services-mds", r.Services[3], false)
}

func TestProjectionOrderMonMgrOsdThenName(t *testing.T) {
	r, err := clusterRequestWith(t, func(services []api.Value) []api.Value {
		return []api.Value{mdsRequestService("zeta"), services[2], mdsRequestService("alpha"), services[0], services[1]}
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
		got = append(got, service.ServiceType+"."+service.ServiceID)
	}
	if want := []string{"mon.", "mgr.", "osd.data", "mds.alpha", "mds.zeta"}; !slices.Equal(got, want) {
		t.Fatalf("projected order %v, want %v", got, want)
	}
}

func TestMDSProjectionRefusesSpecAndMissingID(t *testing.T) {
	for name, service := range map[string]api.Value{
		"spec":       mdsRequestService("files").With("spec", requestMap("x", 1)),
		"missing id": requestMap("service_type", "mds", "placement", requestMap("label", "mon", "count", 2)),
	} {
		_, err := clusterRequestWith(t, func(services []api.Value) []api.Value { return append(services, service) })
		d := diagnostics.Of(err)
		if len(d) != 1 || d[0].Code != "lifecycle.unsupported" || d[0].Message != "core MDS services carry only service_id and placement" {
			t.Fatalf("%s: %+v", name, d)
		}
	}
}

func TestClusterDeadlineCountsMDSService(t *testing.T) {
	plain, err := clusterRequestWith(t, func(services []api.Value) []api.Value { return services })
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	withMDS, err := clusterRequestWith(t, func(services []api.Value) []api.Value { return append(services, mdsRequestService("files")) })
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	before, beforeErr := cephDeadline(plain, "apply")
	after, afterErr := cephDeadline(withMDS, "apply")
	if beforeErr != nil || afterErr != nil || after != before+660*time.Second || len(withMDS.Services) != len(plain.Services)+1 {
		t.Fatalf("cluster deadline did not count an MDS service: %v (%v) -> %v (%v)", before, beforeErr, after, afterErr)
	}
}
