package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func physicalCatalogPerHost(hosts, disks int) (api.Catalog, api.Object) {
	objects := poolRequestCatalog(hosts, hosts).Objects()
	var cluster api.Object
	for i, object := range objects {
		if object.Kind() == api.InfraProvider {
			objects[i] = object.WithSpec(requestMap("baremetal", requestMap()))
		}
		if object.Kind() != api.CephCluster {
			continue
		}
		services := object.Spec().Get("services").Items()[:2]
		for node := 1; node <= hosts; node++ {
			paths := []string{}
			for disk := 0; disk < disks; disk++ {
				paths = append(paths, fmt.Sprintf("/dev/disk/by-id/wwn-0x5%015x", node*64+disk))
			}
			services = append(services, requestMap("service_type", "osd", "service_id", fmt.Sprintf("data-%d", node), "placement", requestMap("hosts", api.StringList(fmt.Sprintf("node-%d", node))), "spec", requestMap("data_devices", requestMap("paths", api.StringList(paths...)))))
		}
		cluster = object.WithSpec(object.Spec().WithPath(api.ListValue(services...), "services"))
		objects[i] = cluster
	}
	return api.NewCatalog(objects), cluster
}

func TestPhysicalDeviceBoundIsTheMachineErasePolicyLessItsRootDisk(t *testing.T) {
	targets, devices := maxEraseTargets, maxPhysicalDevices
	if targets != machine.MaxEraseTargets || devices != targets-1 || devices != 15 {
		t.Fatalf("physical device bound %d of %d erase targets", devices, targets)
	}
}

func TestPhysicalHostsPlanEverySelectedDiskUpToTheBound(t *testing.T) {
	for _, disks := range []int{3, 5, maxPhysicalDevices} {
		for name, catalogOf := range map[string]func(int, int) (api.Catalog, api.Object){"one service per disk": physicalCatalogOf, "one service per host": physicalCatalogPerHost} {
			catalog, cluster := catalogOf(3, disks)
			r, err := baseRequest(catalog, cluster, "synthetic")
			if err != nil {
				t.Fatalf("%d disks, %s: %v", disks, name, diagnostics.Of(err))
			}
			frozen, err := r.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			for node, host := range r.Hosts {
				if !host.Physical || len(host.Devices) != disks || !slices.IsSortedFunc(host.Devices, func(a, b Device) int { return strings.Compare(a.Path, b.Path) }) {
					t.Fatalf("%d disks, %s: host %s planned %+v", disks, name, host.Name, host.Devices)
				}
				for disk, device := range host.Devices {
					path := fmt.Sprintf("/dev/disk/by-id/wwn-0x5%015x", (node+1)*64+disk)
					if device.Path != path || device.WWN != strings.TrimPrefix(path, "/dev/disk/by-id/wwn-") || device.Serial != "" || device.SizeBytes != 0 || device.Rotational {
						t.Fatalf("%d disks, %s: WWN intent %+v, want %s", disks, name, device, path)
					}
					if !strings.Contains(string(frozen), `"path":"`+path+`","serial":"","sizeBytes":0,"wwn":"`+device.WWN+`"`) {
						t.Fatalf("%d disks, %s: the frozen request lacks the WWN intent of %s", disks, name, path)
					}
				}
				target, found := (HostRequest{Version: HostVersion, Cluster: scopeOf(r), Host: host.Name}).target()
				if !found || !slices.Equal(target.Devices, host.Devices) {
					t.Fatalf("%d disks, %s: the host block binds %+v", disks, name, target.Devices)
				}
			}
			if deadline, err := cephDeadline(r, "apply"); err != nil || deadline != clusterApplyDeadline(3) {
				t.Fatalf("%d disks, %s: the device count moved the cluster deadline to %s: %v", disks, name, deadline, err)
			}
		}
	}
}

func TestPhysicalHostOneDiskPastTheBoundRefusesByTheErasePolicy(t *testing.T) {
	reason := fmt.Sprintf("CephCluster example selects %d disks on physical Ceph host node-2, past the 15 that its Machine's eraseOnRemoval policy can cover beside the root disk within its 16 targets", maxPhysicalDevices+1)
	const remedy = "select at most 15 distinct WWN disks for host node-2 across the data_devices, db_devices and wal_devices paths of the OSD services of CephCluster example"
	for name, catalogOf := range map[string]func(int, int) (api.Catalog, api.Object){"one service per disk": physicalCatalogOf, "one service per host": physicalCatalogPerHost} {
		catalog, cluster := catalogOf(3, maxPhysicalDevices)
		services := cluster.Spec().Get("services").Items()
		extra := "/dev/disk/by-id/wwn-0x7000000000000002"
		for i, service := range services {
			if !slices.Equal(service.Get("placement", "hosts").Strings(), []string{"node-2"}) {
				continue
			}
			paths := append(service.Get("spec", "data_devices", "paths").Strings(), extra)
			services[i] = service.WithPath(api.StringList(paths...), "spec", "data_devices", "paths")
			break
		}
		changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
		objects := catalog.Objects()
		for i, object := range objects {
			if object.Kind() == api.CephCluster {
				objects[i] = changed
			}
		}
		_, err := baseRequest(api.NewCatalog(objects), changed, "synthetic")
		t.Run(name, func(t *testing.T) { refusedWith(t, err, "lifecycle.unsupported", reason, remedy) })
	}
}

func TestPhysicalHostsWithManyDisksStillRefuseSharedWWNsAndPartitions(t *testing.T) {
	catalog, cluster := physicalCatalogPerHost(3, 5)
	services := cluster.Spec().Get("services").Items()
	shared := slices.Clone(services[3].Get("spec", "data_devices", "paths").Strings())
	shared[4] = services[2].Get("spec", "data_devices", "paths").Strings()[0]
	partition := slices.Clone(services[3].Get("spec", "data_devices", "paths").Strings())
	partition[4] += "-part1"
	for name, paths := range map[string][]string{"shared": shared, "partition": partition} {
		changed := slices.Clone(services)
		changed[3] = changed[3].WithPath(api.StringList(paths...), "spec", "data_devices", "paths")
		if _, err := baseRequest(catalog, cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(changed...), "services")), "synthetic"); err == nil {
			t.Fatalf("a %s WWN on a five-disk host was admitted", name)
		}
	}
}

func TestPhysicalClusterAtBothBoundsKeepsItsServicesRequestAndEvidenceBounds(t *testing.T) {
	catalog, cluster := physicalCatalogOf(maxClusterHosts, maxPhysicalDevices)
	_, err := baseRequest(catalog, cluster, "synthetic")
	refusedWith(t, err, "lifecycle.unsupported", fmt.Sprintf("CephCluster example declares %d MON, MGR, OSD and MDS services, past the 128 one services call submits", 2+maxClusterHosts*maxPhysicalDevices),
		"list a host's disks in one OSD service of CephCluster example instead of one service per disk")
	catalog, cluster = physicalCatalogPerHost(maxClusterHosts, maxPhysicalDevices)
	r, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	for i := range r.Hosts {
		r.Hosts[i].Hardware = &PhysicalIdentity{UUID: "11111111-1111-4111-8111-111111111111", MACs: slices.Repeat([]string{"02:00:00:00:00:01"}, 32)}
		r.Hosts[i].KnownHosts = r.Hosts[i].Name + " ecdsa-sha2-nistp256 " + strings.Repeat("A", 180) + "\n"
	}
	data, err := r.Canonical()
	if err != nil || len(data) > reconciliation.MaxRequestBytes/4 {
		t.Fatalf("the largest physical request takes %d bytes: %v", len(data), err)
	}
	retirement := physicalRetirementRequest(catalog, r)
	frozen, _ := retirement.Canonical()
	if decoded, err := DecodePhysicalRetirementRequest(frozen); err != nil || len(decoded.Hosts) != maxClusterHosts || len(decoded.Hosts[0].Devices) != maxPhysicalDevices {
		t.Fatalf("the retirement policy at both bounds did not decode: %v", err)
	}
	retirement.Hosts[0].Devices = append(retirement.Hosts[0].Devices, Device{Path: "/dev/disk/by-id/wwn-0x7000000000000001", WWN: "0x7000000000000001"})
	frozen, _ = retirement.Canonical()
	if _, err := DecodePhysicalRetirementRequest(frozen); err == nil {
		t.Fatal("a retirement policy one disk past the bound decoded")
	}
	e := readyEvidence(r)
	e.OSDs = []OSDEvidence{}
	for node, host := range r.Hosts {
		for disk, device := range host.Devices {
			index := node*maxPhysicalDevices + disk
			e.OSDs = append(e.OSDs, OSDEvidence{Host: host.Name, Path: device.Path, ID: 10000 + index, UUID: fmt.Sprintf("00000000-0000-4000-8000-%012d", index)})
		}
	}
	evidence := evidenceBytes(t, e)
	long := e
	long.OSDs = slices.Clone(e.OSDs)
	for i := range long.OSDs {
		long.OSDs[i].Host = strings.Repeat("h", 125) + long.OSDs[i].Host
	}
	for _, value := range [][]byte{evidence, evidenceBytes(t, long)} {
		if len(value) > 60000 {
			t.Fatalf("ready evidence of %d OSDs takes %d bytes", len(e.OSDs), len(value))
		}
	}
	t.Logf("%d hosts of %d disks: request %d bytes, ready evidence %d bytes", len(r.Hosts), maxPhysicalDevices, len(data), len(evidence))
}

func TestPhysicalRetirementOfFiveDiskHostsNeedsEveryDiskReceipt(t *testing.T) {
	fiveDisks := func() (api.Catalog, api.Object) { return physicalCatalogPerHost(3, 5) }
	capability, execution, _, _ := physicalRetirementFixtureFrom(t, fiveDisks)
	request, err := DecodePhysicalRetirementRequest(execution.Block.Request)
	if err != nil || len(request.Hosts[0].Devices) != 5 || len(request.Hosts[0].EraseWWNs) != 5 {
		t.Fatalf("five-disk policy %+v: %v", request, err)
	}
	if err := request.authorized(); err != nil {
		t.Fatal(err)
	}
	request.Hosts[1].EraseWWNs = request.Hosts[1].EraseWWNs[:4]
	if err := request.authorized(); err == nil {
		t.Fatal("a fifth disk outside the original erase policy was admitted")
	}
	result, err := capability.Destroy(context.Background(), execution)
	var proof PhysicalRetirementEvidence
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || json.Unmarshal(result.Evidence, &proof) != nil || !proof.Retired || len(proof.Receipts) != 3 {
		t.Fatalf("complete five-disk retirement refused: %+v %v", proof, err)
	}
	for _, damage := range []string{"missing-fifth", "wrong-fifth-serial"} {
		t.Run(damage, func(t *testing.T) {
			capability, execution, _, proofs := physicalRetirementFixtureFrom(t, fiveDisks)
			value := proofs["node-2"]
			if damage == "missing-fifth" {
				value.Targets = value.Targets[:4]
			} else {
				value.Targets[4].Serial = "replacement"
			}
			proofs["node-2"] = value
			result, err := capability.Destroy(context.Background(), execution)
			if err == nil || result.Outcome == reconciliation.OutcomeChanged || result.Outcome == reconciliation.OutcomeUnchanged {
				t.Fatal("an incomplete five-disk erasure counted as retirement")
			}
		})
	}
}
