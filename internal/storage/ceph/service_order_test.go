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

func TestACustomCRUSHDeviceClassRefusesAtPlanByName(t *testing.T) {
	for _, test := range []struct {
		name, member string
		change       func(api.Value) api.Value
	}{
		{"service level", "sets spec.crush_device_class", func(osd api.Value) api.Value {
			return osd.WithPath(api.StringValue("fast"), "spec", "crush_device_class")
		}},
		{"path record", "declares data_devices.paths[0] as a {path, crush_device_class} record", func(osd api.Value) api.Value {
			path := osd.Get("spec", "data_devices", "paths").Items()[0]
			record := requestMap("path", path.Text(), "crush_device_class", "fast")
			return osd.WithPath(api.ListValue(record), "spec", "data_devices", "paths")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := osdRefusal(t, test.change)
			if d.Code != "lifecycle.unsupported" || !strings.Contains(d.Message, "OSD service osd.data "+test.member) || !strings.Contains(d.Message, "custom CRUSH device class") || !strings.Contains(d.Remediation, "hdd, ssd or nvme") {
				t.Fatalf("device class refusal: %+v", d)
			}
		})
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
