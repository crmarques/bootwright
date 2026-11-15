package ceph

import (
	"bytes"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func singleHostCatalog(disks int, enabled bool) api.Catalog {
	catalog := poolRequestCatalog(1, 1)
	objects := catalog.Objects()
	data, paths := []api.Value{}, []string{}
	for i := 0; i < disks; i++ {
		data = append(data, requestMap("name", string(rune('a'+i)), "sizeGiB", 50))
		paths = append(paths, "/dev/vd"+string(rune('b'+i)))
	}
	for i, object := range objects {
		spec := object.Spec()
		switch object.Kind() {
		case api.InfraProvider:
			profiles := spec.Get("libvirt", "machineProfiles").Items()
			profiles[0] = profiles[0].With("dataDisks", api.ListValue(data...))
			spec = spec.WithPath(api.ListValue(profiles...), "libvirt", "machineProfiles")
		case api.CephCluster:
			services := spec.Get("services").Items()
			for j, service := range services {
				if service.Get("service_type").Text() == "osd" {
					service = service.WithPath(api.StringList(paths...), "spec", "data_devices", "paths")
				} else {
					service = service.WithPath(api.IntegerValue("1"), "placement", "count")
				}
				services[j] = service
			}
			spec = spec.WithPath(api.ListValue(services...), "services").WithPath(api.BoolValue(enabled), "cephadm", "bootstrap", "singleHostDefaults")
		}
		objects[i] = object.WithSpec(spec)
	}
	return api.NewCatalog(append(objects, childOf(api.CephPool, "volumes", "role", "rbd")))
}

func TestSingleHostTestClusterFreezesIntentAndDiskReplication(t *testing.T) {
	catalog := singleHostCatalog(3, true)
	for _, kindName := range []struct {
		kind api.Kind
		name string
	}{{api.CephCluster, "example"}, {api.CephPool, "volumes"}} {
		r, err := planned(t, catalog, kindName.kind, kindName.name)
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		encoded, err := r.Canonical()
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		decoded, err := DecodeRequest(encoded)
		if err != nil || !bytes.Contains(encoded, []byte(`"singleHostDefaults":true`)) || len(decoded.Hosts) != 1 || len(decoded.Hosts[0].Devices) != 3 {
			t.Fatalf("single-host intent lost: %+v %v", decoded, err)
		}
		if kindName.kind == api.CephPool && (r.Pool.Size != 2 || r.Pool.MinSize != 2 || r.Pool.CrushRule != defaultRule) {
			t.Fatalf("unsafe single-host pool: %+v", r.Pool)
		}
	}
}

func TestSingleHostTestClusterRequiresExplicitOptInAndTwoDisks(t *testing.T) {
	for _, tc := range []struct {
		disks   int
		enabled bool
	}{{3, false}, {1, true}} {
		if _, err := planned(t, singleHostCatalog(tc.disks, tc.enabled), api.CephCluster, "example"); err == nil {
			t.Fatalf("accepted unsafe single-host profile: %+v", tc)
		}
	}
}

func TestSingleHostClusterRequestGolden(t *testing.T) {
	r, err := planned(t, singleHostCatalog(2, true), api.CephCluster, "example")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	r.Owner = "apply-synthetic"
	r.FSID = FSIDFor(r.Owner, r.Cluster)
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-cluster-single-host", data, false)
}
