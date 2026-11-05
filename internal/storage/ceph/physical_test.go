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
// which node-2 declares as an erase target without deviceName.
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
			targets := api.ListValue(requestMap("serialNumber", "ROOT-2", "sizeBytes", 1<<39), requestMap("serialNumber", "SER-2", "sizeBytes", 1<<40))
			spec := object.Spec().WithPath(requestMap("method", "logicalOverwrite", "targets", targets), "hardware", "eraseOnRemoval")
			if machineChange != nil {
				spec = machineChange(spec)
			}
			objects[i] = object.WithSpec(spec)
		}
	}
	return api.NewCatalog(objects), cluster
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
	if retirement.Hosts[1].EraseTargets[1] != (PhysicalEraseTarget{Serial: "SER-2", SizeBytes: serialSize}) {
		t.Fatalf("erase targets %+v", retirement.Hosts[1].EraseTargets)
	}
}

func TestPhysicalCephRefusesASerialSelectionItsMachineDoesNotDeclare(t *testing.T) {
	const undeclared = "the serial number SER-2 that this OSD selection names is no eraseOnRemoval target of Machine node-2 declared without deviceName, which would give its exact size"
	const undeclaredRemedy = "declare the disk in spec.hardware.eraseOnRemoval.targets of Machine node-2 with its serialNumber and sizeBytes and no deviceName, or select it by that target's deviceName"
	byDevice := func(spec api.Value) api.Value {
		targets := api.ListValue(requestMap("deviceName", "/dev/disk/by-id/wwn-0x7000000000000001", "wwn", "0x7000000000000001", "serialNumber", "SER-2", "sizeBytes", 1<<40))
		return spec.WithPath(targets, "hardware", "eraseOnRemoval", "targets")
	}
	for _, test := range []struct {
		name           string
		machine        func(api.Value) api.Value
		path           string
		reason, remedy string
	}{
		{"undeclared", func(spec api.Value) api.Value {
			return spec.WithPath(api.ListValue(requestMap("serialNumber", "OTHER", "sizeBytes", 1<<40)), "hardware", "eraseOnRemoval", "targets")
		}, "", undeclared, undeclaredRemedy},
		{"declared by deviceName", byDevice, "", undeclared, undeclaredRemedy},
		{"root disk", func(spec api.Value) api.Value {
			return spec.WithPath(requestMap("serialNumber", "SER-2", "sizeBytes", 1<<40), "substrate", "baremetal", "rootDevice")
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
			targets := api.ListValue(requestMap("serialNumber", "SER-2", "sizeBytes", 1<<40))
			objects[i] = object.WithSpec(object.Spec().WithPath(requestMap("method", "logicalOverwrite", "targets", targets), "hardware", "eraseOnRemoval"))
		}
	}
	_, err := baseRequest(api.NewCatalog(objects), cluster, "synthetic")
	refusedWith(t, err, "api.invariant", "a physical WWN or disk serial number is selected on multiple hosts", "select independently owned local disks")
}
