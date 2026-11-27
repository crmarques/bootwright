package ceph

import (
	"context"
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func physicalCatalog() (api.Catalog, api.Object) {
	catalog := poolRequestCatalog(3, 3)
	objects := catalog.Objects()
	var cluster api.Object
	for i, object := range objects {
		if object.Kind() == api.InfraProvider {
			objects[i] = object.WithSpec(requestMap("baremetal", requestMap()))
		}
		if object.Kind() == api.Machine {
			objects[i] = withoutProfile(object)
		}
		if object.Kind() != api.CephCluster {
			continue
		}
		services := object.Spec().Get("services").Items()[:2]
		for n := 1; n <= 3; n++ {
			services = append(services, requestMap("service_type", "osd", "service_id", fmt.Sprintf("data-%d", n), "placement", requestMap("hosts", api.StringList(fmt.Sprintf("node-%d", n))), "spec", requestMap("data_devices", requestMap("paths", api.StringList(fmt.Sprintf("/dev/disk/by-id/wwn-0x500000000000000%d", n))))))
		}
		cluster = object.WithSpec(object.Spec().WithPath(api.ListValue(services...), "services"))
		objects[i] = cluster
	}
	return api.NewCatalog(objects), cluster
}

func withoutProfile(machine api.Object) api.Object {
	hardware := machine.Spec().Get("hardware").Without("profileRef")
	return machine.WithSpec(machine.Spec().With("hardware", hardware))
}

func TestPhysicalCephPlansOnlyExplicitWWNIntent(t *testing.T) {
	catalog, cluster := physicalCatalog()
	r, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range r.Hosts {
		if !h.Physical || h.Hardware != nil || len(h.Devices) != 1 || h.Devices[0].WWN == "" || h.Devices[0].Serial != "" || h.Devices[0].SizeBytes != 0 {
			t.Fatalf("unproved device facts invented: %+v", h)
		}
	}
	for _, path := range []string{"/dev/sda", "/dev/disk/by-id/wwn-0x5000000000000001-part1", "/dev/disk/by-id/wwn-0x500000000000000A"} {
		services := cluster.Spec().Get("services").Items()
		services[2] = services[2].WithPath(api.StringList(path), "spec", "data_devices", "paths")
		changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
		if _, err := baseRequest(catalog, changed, "synthetic"); err == nil {
			t.Fatal("unsafe physical selector admitted", path)
		}
	}
	services := cluster.Spec().Get("services").Items()
	services[3] = services[3].WithPath(services[2].Get("spec", "data_devices", "paths"), "spec", "data_devices", "paths")
	if _, err := baseRequest(catalog, cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services")), "synthetic"); err == nil {
		t.Fatal("shared physical WWN admitted")
	}
}

func TestPhysicalCephPreservesPerDeviceCRUSHClass(t *testing.T) {
	catalog, cluster := physicalCatalog()
	services := cluster.Spec().Get("services").Items()
	path := services[2].Get("spec", "data_devices", "paths").Items()[0].Text()
	services[2] = services[2].WithPath(api.ListValue(requestMap("path", path, "crush_device_class", "fast")), "spec", "data_devices", "paths")
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	request, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Hosts[0].Devices) != 1 || request.Hosts[0].Devices[0].Path != path {
		t.Fatalf("physical device projection lost %q: %+v", path, request.Hosts[0].Devices)
	}
	for _, raw := range request.Services {
		service, err := reconciliation.DecodeEvidence[nativeService](raw, 60*1024, "Ceph native service")
		if err != nil {
			t.Fatal(err)
		}
		if service.ServiceType == "osd" && service.Spec != nil && service.Spec.DataDevices != nil {
			for _, device := range service.Spec.DataDevices.Paths {
				if device.Path == path && device.Class == "fast" {
					return
				}
			}
		}
	}
	t.Fatal("native OSD request lost the per-device class")
}

type physicalProofs struct {
	dependencyProofs
	identity PhysicalIdentity
}

func (p physicalProofs) PhysicalMachine(block reconciliation.Block, _ []byte) (PhysicalIdentity, bool, error) {
	if block.Implementation != "machine-baremetal-v1" {
		return PhysicalIdentity{}, false, nil
	}
	return p.identity, true, nil
}

func TestPhysicalCephRequiresOneOriginalHardwareProof(t *testing.T) {
	proof := lifecycle.BlockEvidence{Kind: "Machine", Object: "node-1", Implementation: "machine-baremetal-v1", Verb: reconciliation.Apply, State: reconciliation.BlockDone, Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: "Machine", Object: "node-1", Implementation: "machine-baremetal-v1"}}}
	installation := proof
	installation.Implementation = "managed-installation"
	installation.Block.Implementation = installation.Implementation
	identity := PhysicalIdentity{UUID: "11111111-1111-4111-8111-111111111111", MACs: []string{"02:00:00:00:00:01"}}
	for _, test := range []struct {
		name     string
		proofs   []lifecycle.BlockEvidence
		identity PhysicalIdentity
		ok       bool
	}{
		{"original", []lifecycle.BlockEvidence{proof}, identity, true},
		{"installation peer", []lifecycle.BlockEvidence{installation, proof}, identity, true},
		{"absent", nil, identity, false},
		{"duplicate", []lifecycle.BlockEvidence{proof, proof}, identity, false},
		{"no uuid", []lifecycle.BlockEvidence{proof}, PhysicalIdentity{MACs: identity.MACs}, false},
		{"no inventory", []lifecycle.BlockEvidence{proof}, PhysicalIdentity{UUID: identity.UUID}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			hosts := []Host{{Physical: true, Machine: "node-1"}}
			err := bindPhysicalHosts(hosts, lifecycle.Execution{ApplyProved: test.proofs}, physicalProofs{identity: test.identity})
			if (err == nil) != test.ok {
				t.Fatal(err)
			}
			if test.ok && hosts[0].Hardware == nil {
				t.Fatal("hardware not bound")
			}
		})
	}
}

func TestPhysicalCephCannotUseVirtualHostRetirement(t *testing.T) {
	catalog, cluster := physicalCatalog()
	r, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: string(api.CephCluster), Object: "example", Request: data}}
	q, err := (Capability{}).Quiescent(context.Background(), lifecycle.Probe{Block: block, RetiredWithHosts: true})
	if err != nil || q.State != lifecycle.Unproved || !strings.Contains(q.Reason, "B491") {
		t.Fatalf("physical claim release counted as retirement: %+v %v", q, err)
	}
}

const serialSize = int64(1 << 40)

// physicalSerialCatalog selects node-2's disk by /dev/disk/by-serial/SER-2,
// which node-2 declares as a data disk without wwn.
func physicalSerialCatalog(machineChange func(api.Value) api.Value) (api.Catalog, api.Object) {
	catalog, cluster := physicalCatalog()
	services := cluster.Spec().Get("services").Items()
	services[3] = services[3].WithPath(api.StringList("/dev/disk/by-serial/SER-2"), "spec", "data_devices", "paths")
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	objects := catalog.Objects()
	for i, object := range objects {
		switch {
		case object.Kind() == api.CephCluster:
			objects[i] = cluster
		case object.Kind() == api.Machine && object.Name() == "node-2":
			spec := object.Spec().WithPath(api.ListValue(erasedDataDisk("SER-2", 1<<40, "")), "hardware", "dataDisks")
			if machineChange != nil {
				spec = machineChange(spec)
			}
			objects[i] = object.WithSpec(spec)
		}
	}
	return api.NewCatalog(objects), cluster
}

func erasedDataDisk(serial string, size int64, wwn string) api.Value {
	disk := requestMap("serialNumber", serial, "sizeBytes", size, "onRemoval", "logicalOverwrite")
	if wwn != "" {
		disk = disk.With("wwn", api.StringValue(wwn))
	}
	return disk
}

func TestPhysicalCephPlansSerialIntentFromTheMachineErasePolicy(t *testing.T) {
	catalog, cluster := physicalSerialCatalog(nil)
	r, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	want := Device{Path: "/dev/disk/by-serial/SER-2", Serial: "SER-2", SizeBytes: serialSize}
	for _, host := range r.Hosts {
		if host.Machine == "node-2" && (len(host.Devices) != 1 || host.Devices[0] != want) {
			t.Fatalf("serial intent %+v, want %+v", host.Devices, want)
		}
		if host.Machine != "node-2" && (len(host.Devices) != 1 || host.Devices[0].WWN == "" || host.Devices[0].Serial != "") {
			t.Fatalf("WWN intent changed: %+v", host.Devices)
		}
	}
	frozen, err := r.Canonical()
	if err != nil || !strings.Contains(string(frozen), `{"path":"/dev/disk/by-serial/SER-2","serial":"SER-2","sizeBytes":1099511627776}`) {
		t.Fatalf("the frozen request lacks the serial intent: %v", err)
	}
	retirement := physicalRetirementRequest(catalog, r)
	if decoded, err := DecodePhysicalRetirementRequest(mustCanonical(t, retirement)); err != nil || decoded.Hosts[1].Devices[0] != want || decoded.authorized() == nil {
		t.Fatalf("serial retirement policy %+v: %v", decoded, err)
	}
	if retirement.Hosts[1].EraseTargets[0] != (PhysicalEraseTarget{Serial: "SER-2", SizeBytes: serialSize}) {
		t.Fatalf("erase targets %+v", retirement.Hosts[1].EraseTargets)
	}
}

func TestPhysicalCephFreezesTheRootSerialItsMachineDeclares(t *testing.T) {
	for name, test := range map[string]struct {
		machine func(api.Value) api.Value
		want    string
	}{
		"declared by serial": {func(spec api.Value) api.Value {
			return spec.WithPath(requestMap("serialNumber", "ROOT-2", "sizeBytes", 1<<39), "hardware", "rootDisk")
		}, "ROOT-2"},
		"not declared": {nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			catalog, cluster := physicalSerialCatalog(test.machine)
			r, err := baseRequest(catalog, cluster, "synthetic")
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			for _, host := range r.Hosts {
				if want := test.want; host.Machine != "node-2" && host.RootSerial != "" || host.Machine == "node-2" && host.RootSerial != want {
					t.Fatalf("host %s freezes root serial %q, want %q on node-2 only", host.Machine, host.RootSerial, want)
				}
			}
			frozen, err := r.Canonical()
			if err != nil || strings.Contains(string(frozen), `"rootSerial"`) != (test.want != "") {
				t.Fatalf("the frozen request root serial does not follow the Machine: %v", err)
			}
		})
	}
}

// A root its Machine declares by wwn is the root the Ceph host excludes from
// its OSD devices, by that WWN, and a root whose onRemoval erases it leaves the
// host no root to retain.
func TestPhysicalCephFreezesTheRootWWNItsMachineDeclares(t *testing.T) {
	const wwn = "0x7000000000000002"
	for name, root := range map[string]api.Value{
		"root retained": requestMap("wwn", wwn),
		"root erased":   requestMap("wwn", wwn, "serialNumber", "ROOT-2", "sizeBytes", 1<<39, "onRemoval", "logicalOverwrite"),
	} {
		erased := root.Has("onRemoval")
		t.Run(name, func(t *testing.T) {
			catalog, cluster := physicalSerialCatalog(func(spec api.Value) api.Value {
				return spec.WithPath(root, "hardware", "rootDisk")
			})
			r, err := baseRequest(catalog, cluster, "synthetic")
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			for _, host := range r.Hosts {
				if host.Machine != "node-2" {
					continue
				}
				if host.RootWWN != wwn || host.RootSerial != root.Get("serialNumber").Text() || host.RetainRoot != !erased {
					t.Fatalf("node-2 root = wwn %q serial %q retained %v", host.RootWWN, host.RootSerial, host.RetainRoot)
				}
				if _, refused := rootDeviceRefusals(host)["device-root"]; !refused {
					t.Fatal("a wwn-declared root is not excluded from the host's OSD devices")
				}
			}
		})
	}
}

func TestPhysicalCephRefusesASerialSelectionItsMachineDoesNotDeclare(t *testing.T) {
	const undeclared = "the serial number SER-2 that this OSD selection names is no hardware.dataDisks entry of Machine node-2 with a non-retain onRemoval and no wwn, which would give its exact size"
	const undeclaredRemedy = "declare the disk in spec.hardware.dataDisks of Machine node-2 with its serialNumber, sizeBytes and a non-retain onRemoval and no wwn, or select it by its wwn path"
	byWWN := func(spec api.Value) api.Value {
		return spec.WithPath(api.ListValue(erasedDataDisk("SER-2", 1<<40, "0x7000000000000001")), "hardware", "dataDisks")
	}
	for _, test := range []struct {
		name           string
		machine        func(api.Value) api.Value
		path           string
		reason, remedy string
	}{
		{"undeclared", func(spec api.Value) api.Value {
			return spec.WithPath(api.ListValue(erasedDataDisk("OTHER", 1<<40, "")), "hardware", "dataDisks")
		}, "", undeclared, undeclaredRemedy},
		{"declared with a wwn", byWWN, "", undeclared, undeclaredRemedy},
		{"root disk", func(spec api.Value) api.Value {
			return spec.WithPath(requestMap("serialNumber", "SER-2", "sizeBytes", 1<<40), "hardware", "rootDisk")
		}, "", "explicit OSD allocation cannot use the Machine root device", "remove the Machine root device from the OSD selector and select a dedicated data device"},
		{"grammar", nil, "/dev/disk/by-serial/SER=2", "a /dev/disk/by-serial/ path carries a serial number of 1 to 128 ASCII letters, digits and ._:+- characters", "select this disk by its /dev/disk/by-id/wwn-0x path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog, cluster := physicalSerialCatalog(test.machine)
			if test.path != "" {
				services := cluster.Spec().Get("services").Items()
				services[3] = services[3].WithPath(api.StringList(test.path), "spec", "data_devices", "paths")
				cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
			}
			_, err := baseRequest(catalog, cluster, "synthetic")
			refusedWith(t, err, "api.invariant", test.reason, test.remedy)
		})
	}
}

func TestPhysicalCephRefusesOneSerialNumberSelectedOnTwoHosts(t *testing.T) {
	catalog, cluster := physicalSerialCatalog(nil)
	services := cluster.Spec().Get("services").Items()
	services[4] = services[4].WithPath(api.StringList("/dev/disk/by-serial/SER-2"), "spec", "data_devices", "paths")
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "node-3" {
			objects[i] = object.WithSpec(object.Spec().WithPath(api.ListValue(erasedDataDisk("SER-2", 1<<40, "")), "hardware", "dataDisks"))
		}
	}
	_, err := baseRequest(api.NewCatalog(objects), cluster, "synthetic")
	refusedWith(t, err, "api.invariant", "a physical WWN or disk serial number is selected on multiple hosts", "select independently owned local disks")
}
