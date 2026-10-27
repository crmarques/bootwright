package ceph

import (
	"encoding/json"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestOSDVariantsRequestGolden(t *testing.T) {
	catalog := poolRequestCatalog(3, 3)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	r := coreRequest()
	r.Packages = packageRequest(t).Packages
	for i := range r.Hosts {
		r.Hosts[i].Devices[0].Rotational = true
		r.Hosts[i].Devices[0].Path = "/dev/sda"
		r.Hosts[i].Devices = append(r.Hosts[i].Devices, Device{Path: "/dev/sdb", Serial: r.Hosts[i].Name + "-db", SizeBytes: 1 << 30}, Device{Path: "/dev/sdc", Serial: r.Hosts[i].Name + "-wal", SizeBytes: 1 << 30})
	}
	services := cluster.Spec().Get("services").Items()
	for i := range services {
		if services[i].Get("service_type").Text() == "osd" {
			services[i] = services[i].With("spec", requestMap("data_devices", requestMap("paths", api.StringList("/dev/sda")), "db_devices", requestMap("paths", api.StringList("/dev/sdb")), "wal_devices", requestMap("paths", api.StringList("/dev/sdc")), "encrypted", true, "tpm2", true, "osds_per_device", 1))
		}
	}
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	r.Services = nil
	if err := projectServices(&r, cluster); err != nil {
		t.Fatal(err)
	}
	selectDevices(&r)
	if len(r.Hosts[0].Devices) != 3 || len(dataDeviceSet(r)) != 3 {
		t.Fatal("DB/WAL custody changed the OSD count")
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-osd-variants", data, false)
	if _, err := DecodeRequest(data); err != nil {
		t.Fatal(err)
	}
	host := HostRequest{Cluster: scopeOf(r), Host: r.Hosts[0].Name, Version: HostVersion}
	hostData, err := host.canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-host-osd-variants", hostData, false)
	if _, err := DecodeHostRequest(hostData); err != nil {
		t.Fatal(err)
	}
	evidence := readyEvidence(r)
	for i := range evidence.OSDs {
		evidence.OSDs[i].Path = "/dev/sda"
	}
	if !validOSDs(evidence, r, true) {
		t.Fatal("auxiliary devices counted as OSDs")
	}
	evidence.OSDs[0].Path = "/dev/sdb"
	if validOSDs(evidence, r, true) {
		t.Fatal("auxiliary device accepted as a data OSD")
	}
	var service nativeService
	if err := json.Unmarshal(r.Services[2], &service); err != nil {
		t.Fatal(err)
	}
	if !service.Spec.Encrypted || !service.Spec.TPM2 || service.Spec.DBDevices == nil || service.Spec.WALDevices == nil {
		t.Fatal("variant lost in projection")
	}
}

func TestOSDVariantsRefuseOverlappingOrUnboundedAllocation(t *testing.T) {
	catalog := poolRequestCatalog(3, 3)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	for _, spec := range []api.Value{
		requestMap("data_devices", requestMap("paths", api.StringList("/dev/vdb")), "db_devices", requestMap("paths", api.StringList("/dev/vdb"))),
		requestMap("data_devices", requestMap("all", true)),
		requestMap("data_devices", requestMap("paths", api.StringList("/dev/vdb")), "osds_per_device", 2),
		requestMap("data_devices", requestMap("paths", api.StringList("/dev/vdb")), "tpm2", true),
	} {
		service := requestMap("spec", spec)
		if checkDevices(coreRequest(), []string{"node-1"}, cluster, service) == nil {
			t.Fatal("unsafe allocation admitted")
		}
	}
}

func TestOSDServicesCannotShareAuxiliarySelections(t *testing.T) {
	catalog := poolRequestCatalog(3, 3)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	services := cluster.Spec().Get("services").Items()
	r := coreRequest()
	for i := range r.Hosts {
		r.Hosts[i].Devices = append(r.Hosts[i].Devices, Device{Path: "/dev/vdc", Serial: "db", SizeBytes: 1 << 30}, Device{Path: "/dev/vdd", Serial: "other", SizeBytes: 1 << 30})
	}
	services[2] = services[2].WithPath(requestMap("paths", api.StringList("/dev/vdc")), "spec", "db_devices")
	other := services[2].With("service_id", api.StringValue("other")).WithPath(api.StringList("/dev/vdd"), "spec", "data_devices", "paths")
	services = append(services, other)
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	r.Services = nil
	if projectServices(&r, cluster) == nil {
		t.Fatal("shared auxiliary selection admitted")
	}
}
