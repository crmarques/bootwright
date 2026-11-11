package ceph

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestRequestServicesAreCanonicalAcrossAuthoredOrder(t *testing.T) {
	catalog := poolRequestCatalog(3, 3)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	encode := func(object api.Object) []byte {
		t.Helper()
		objects := catalog.Objects()
		for i := range objects {
			if objects[i].Identity() == object.Identity() {
				objects[i] = object
			}
		}
		r, err := New(api.CephCluster, nil).request(api.NewCatalog(objects), object, "synthetic")
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		types := []string{}
		for _, raw := range r.Services {
			var service nativeService
			if err := json.Unmarshal(raw, &service); err != nil {
				t.Fatal(err)
			}
			types = append(types, service.ServiceType)
		}
		if !slices.Equal(types, []string{"mon", "mgr", "osd"}) {
			t.Fatalf("request services are not in MON, MGR, OSD order: %v", types)
		}
		data, err := r.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	authored := encode(cluster)
	services := cluster.Spec().Get("services").Items()
	slices.Reverse(services)
	reversed := encode(cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services")))
	if !bytes.Equal(authored, reversed) {
		t.Fatal("authored service order changed the frozen request")
	}
}

func osdRefusal(t *testing.T, change func(api.Value) api.Value) diagnostics.Diagnostic {
	t.Helper()
	catalog := poolRequestCatalog(3, 3)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	services := cluster.Spec().Get("services").Items()
	services[2] = change(services[2])
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	objects := catalog.Objects()
	for i := range objects {
		if objects[i].Identity() == changed.Identity() {
			objects[i] = changed
		}
	}
	_, err := New(api.CephCluster, nil).request(api.NewCatalog(objects), changed, "synthetic")
	d := diagnostics.Of(err)
	if len(d) != 1 {
		t.Fatalf("OSD refusal: %+v", d)
	}
	return d[0]
}

func TestNonStringOSDPathRemedyNamesOwnedDataDiskPaths(t *testing.T) {
	d := osdRefusal(t, func(osd api.Value) api.Value {
		return osd.WithPath(api.ListValue(api.IntegerValue("1")), "spec", "data_devices", "paths")
	})
	if d.Message != "OSD service osd.data declares data_devices.paths[0] as something other than a plain device string" || d.Remediation != "use plain owned data-disk paths: /dev/sd* for explicit-media disks, /dev/vd* otherwise" {
		t.Fatalf("OSD path refusal: %+v", d)
	}
}

func TestACustomCRUSHDeviceClassPlansIntoTheNativeOSDSpecGolden(t *testing.T) {
	for _, test := range []struct {
		golden string
		spec   func(path string) api.Value
	}{
		{"request-osd-class", func(path string) api.Value {
			return requestMap("crush_device_class", "fast", "data_devices", requestMap("paths", api.StringList(path)))
		}},
		{"request-osd-path-class", func(path string) api.Value {
			return requestMap("data_devices", requestMap("paths", api.ListValue(requestMap("path", path, "crush_device_class", "fast"))))
		}},
	} {
		t.Run(test.golden, func(t *testing.T) {
			policy := childOf(api.CephPlacementPolicy, "fast", "ruleName", "fast-hosts", "crushDeviceClass", "fast", "replicated", requestMap("size", 2, "minSize", 2))
			catalog := planCatalog(3, 3, func(spec api.Value) api.Value {
				services := spec.Get("services").Items()
				services[2] = services[2].With("spec", test.spec(services[2].Get("spec", "data_devices", "paths").Items()[0].Text()))
				return withCorePair(spec.WithPath(api.ListValue(services...), "services"))
			}, policy)
			r, err := planned(t, catalog, api.CephCluster, "example")
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			matchesGolden(t, test.golden, r.Services[2], false)
			var service nativeService
			if err := json.Unmarshal(r.Services[2], &service); err != nil {
				t.Fatal(err)
			}
			again, err := json.Marshal(service)
			if err != nil || !bytes.Equal(again, r.Services[2]) || len(dataDeviceSet(r)) != 3 {
				t.Fatalf("the native OSD document does not read back as itself: %s (%v)", again, err)
			}
			placed, err := planned(t, catalog, api.CephPlacementPolicy, "fast")
			if err != nil || placed.Policy == nil || placed.Policy.DeviceClass != "fast" {
				t.Fatalf("a policy on a declared class planned as %+v (%v)", placed.Policy, diagnostics.Of(err))
			}
		})
	}
}

func TestOSDClassesRefuseWhereCephadmCannotRealizeThem(t *testing.T) {
	d := osdRefusal(t, func(osd api.Value) api.Value {
		path := osd.Get("spec", "data_devices", "paths").Items()[0].Text()
		return osd.WithPath(requestMap("crush_device_class", "fast", "data_devices", requestMap("paths", api.ListValue(requestMap("path", path, "crush_device_class", "slow"), api.StringValue("/dev/vdc"))),
			"db_devices", requestMap("paths", api.StringList("/dev/vdd"))), "spec")
	})
	if d.Code != "lifecycle.unsupported" || !strings.Contains(d.Message, "OSD service osd.data gives its data devices more than one CRUSH device class and declares db_devices or wal_devices") ||
		d.Remediation != "declare one OSD service per device class, each with its own db_devices and wal_devices" {
		t.Fatalf("a multi-class OSD service with a DB device planned as %+v", d)
	}
	d = osdRefusal(t, func(osd api.Value) api.Value {
		return osd.WithPath(requestMap("paths", api.ListValue(requestMap("path", "/dev/vdd", "crush_device_class", "fast"))), "spec", "db_devices")
	})
	if d.Message != "OSD service osd.data declares db_devices.paths[0] as something other than a plain device string" {
		t.Fatalf("a DB path record planned as %+v", d)
	}
}

func TestCatalogClusterRequestGolden(t *testing.T) {
	catalog := poolRequestCatalog(3, 3)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	services := cluster.Spec().Get("services").Items()
	slices.Reverse(services)
	authored := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	objects := catalog.Objects()
	for i := range objects {
		if objects[i].Identity() == authored.Identity() {
			objects[i] = authored
		}
	}
	r, err := New(api.CephCluster, nil).request(api.NewCatalog(objects), authored, "synthetic")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-cluster-catalog", data, false)
}
