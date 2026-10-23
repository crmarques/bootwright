//go:build linux && amd64

package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// Adapt the canonical profiles through authored admission, keeping clients on
// their original substrate. This qualifies composition, never physical effects.
func TestPhysicalCephMatrixPreservesCanonicalPlans(t *testing.T) {
	profiles := linkedProfiles(t)
	if len(profiles) != 8 {
		t.Fatal("physical matrix needs the complete P0-P7 catalog")
	}
	for _, profile := range profiles {
		if profile.id == "P0" {
			continue
		}
		name := profile.dir
		t.Run(profile.id, func(t *testing.T) {
			original, _ := compileAcceptance(t, exampleDirectory(t, name))
			physical := physicalMatrixState(t, name, original)
			if refused := productionUnsupported(t, physical); len(refused) != 0 {
				t.Fatalf("physical profile refused: %+v", refused)
			}
			definitions := productionDefinitions(t, physical, name)
			plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
			if err != nil {
				t.Fatal(err)
			}
			inverse, err := plan.Inverse()
			if err != nil {
				t.Fatal(err)
			}
			physicalHosts := map[string]bool{}
			for _, cluster := range original.Effective().OfKind(api.StorageCluster) {
				block, found := plan.Block(ceph.ClusterBlockID(cluster.Name()))
				if !found {
					t.Fatal("missing physical cluster", cluster.Name())
				}
				request, err := ceph.DecodeRequest(block.Request)
				if err != nil || len(request.Hosts) != len(cluster.Spec().Get("ceph", "topology", "nodes").Items()) {
					t.Fatalf("physical host projection: %+v %v", request, err)
				}
				for _, host := range request.Hosts {
					physicalHosts[host.Machine] = true
					if !host.Physical || host.Hardware != nil || host.HostKeyRef == "" {
						t.Fatalf("physical host lost its original-evidence boundary: %+v", host)
					}
					erase, ok := plan.Block(installation.EraseBlockID(host.Machine))
					if !ok {
						t.Fatal("missing explicit erase policy", host.Machine)
					}
					policy, err := installation.DecodeEraseRequest(erase.Request)
					if err != nil || len(policy.Targets) != len(host.Devices)+1 || !policy.Install.Target.Physical || policy.Install.Target.Hardware == nil || len(policy.Install.Target.Hardware.Interfaces) == 0 || policy.Install.Private == nil || !policy.Install.Target.Controller.TLSVerify || policy.Install.Target.Controller.TrustBundleRef == "" {
						t.Fatalf("incomplete original root/data erase policy: %+v %v", policy, err)
					}
					install, ok := plan.Block(installation.BlockID(host.Machine))
					removal, err := installation.NewErase(nil).Removal(context.Background(), erase)
					if !ok || err != nil || !slices.Contains(install.Consumes, "data-loss") || !slices.Contains(removal.Consumes, "data-loss") || len(erase.Consumes) != 0 {
						t.Fatal("physical installation or erase lost data-loss authorization", host.Machine)
					}
					for _, device := range host.Devices {
						if device.WWN == "" || device.Serial != "" || device.SizeBytes != 0 || !slices.ContainsFunc(policy.Targets, func(target installation.EraseTarget) bool {
							return target.DeviceName == device.Path && target.WWN == device.WWN && target.SerialNumber != "" && target.SizeBytes > 0
						}) {
							t.Fatalf("selected disk lacks explicit erasure authority: %+v", device)
						}
					}
					chain := []string{ceph.ClusterBlockID(cluster.Name()), ceph.HostBlockID(cluster.Name(), host.Machine), installation.BlockID(host.Machine), installation.EraseBlockID(host.Machine), ceph.PhysicalRetirementBlockID(cluster.Name()), baremetal.BlockID(host.Machine)}
					for index := 1; index < len(chain); index++ {
						if !physicalCephAncestors(inverse, chain[index])[chain[index-1]] {
							t.Fatalf("physical inverse lacks %s before %s", chain[index-1], chain[index])
						}
					}
				}
			}
			for _, object := range original.Authored().Objects() {
				if strings.HasPrefix(string(object.Kind()), "Storage") && object.Kind() != api.StorageCluster {
					if got := requireObject(t, physical.Authored(), object.Kind(), object.Name()); !got.Spec().Equal(object.Spec()) {
						t.Fatal("physical adaptation changed storage resource intent", object.Identity())
					}
				}
				if object.Kind() == api.Machine && !physicalHosts[object.Name()] {
					if got := requireObject(t, physical.Authored(), api.Machine, object.Name()); !got.Spec().Equal(object.Spec()) {
						t.Fatal("physical adaptation changed an independently owned client or controller", object.Identity())
					}
				}
			}
		})
	}
}

func physicalMatrixState(t *testing.T, name string, original *compilation.State) *compilation.State {
	t.Helper()
	objects := original.Authored().Objects()
	virtual, err := libvirt.MachineRequests(original.Effective(), "controller", name)
	if err != nil {
		t.Fatal(err)
	}
	requests := map[string]libvirt.MachineRequest{}
	for _, request := range virtual {
		requests[request.Identity.Object] = request
	}
	hosts := map[string]int{}
	paths := map[string]map[string]string{}
	for _, cluster := range original.Effective().OfKind(api.StorageCluster) {
		for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
			host := node.Get("machineRef").Text()
			hosts[host] = len(hosts) + 1
			paths[host] = map[string]string{}
			for index, disk := range requests[host].Disks[1:] {
				paths[host]["/dev/"+disk.Target] = fmt.Sprintf("/dev/disk/by-id/wwn-0x5%015x", hosts[host]*16+index)
			}
		}
	}
	base, _ := compileAcceptance(t, exampleDirectory(t, "physical-3-nodes-ceph"))
	provider := requireObject(t, base.Authored(), api.InfraProvider, "lab-metal")
	attachments := []api.Value{}
	for _, object := range objects {
		if object.Kind() == api.InfraProvider {
			for _, attachment := range object.Spec().Get("networkAttachments").Items() {
				attachments = append(attachments, api.MapValue(api.FieldValue{Name: "name", Value: attachment.Get("name")}, api.FieldValue{Name: "baremetal", Value: api.MapValue()}))
			}
		}
	}
	provider = provider.WithSpec(provider.Spec().With("networkAttachments", api.ListValue(attachments...)))
	objects = append(objects, provider, requireObject(t, base.Authored(), api.Secret, "lab-bmc-ca"))
	for index, object := range objects {
		spec := object.Spec()
		if object.Kind() == api.Machine && hosts[object.Name()] != 0 {
			number, virtual := hosts[object.Name()], requests[object.Name()]
			key := "physical-" + object.Name() + "-host-key"
			secret := requireObject(t, base.Authored(), api.Secret, "metal-01-host-key")
			objects = append(objects, api.NewObject(api.Secret, key, api.Value{}, secret.Spec().WithPath(api.StringValue("ecdsa-p256"), "source", "generated", "keyType")))
			spec = spec.With("substrate", api.MapValue(api.FieldValue{Name: "providerRef", Value: api.StringValue(provider.Name())}))
			spec = spec.WithPath(api.StringValue(key), "os", "install", "hostKeyRef").WithPath(api.StringValue("/dev/sdz"), "os", "install", "rootDeviceHints", "deviceName")
			nics := []api.Value{}
			for _, nic := range virtual.Interfaces {
				nics = append(nics, api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(nic.Name)}, api.FieldValue{Name: "macAddress", Value: api.StringValue(nic.MACAddress)}))
			}
			targets := []api.Value{physicalMatrixTarget("/dev/sdz", fmt.Sprintf("0x6%015x", number), number*16, int64(virtual.Disks[0].SizeGiB)<<30)}
			for _, disk := range virtual.Disks[1:] {
				path := paths[object.Name()]["/dev/"+disk.Target]
				targets = append(targets, physicalMatrixTarget(path, strings.TrimPrefix(path, "/dev/disk/by-id/wwn-"), number*16+len(targets), int64(disk.SizeGiB)<<30))
			}
			spec = spec.WithPath(api.ListValue(nics...), "hardware", "nics").WithPath(api.StringValue(fmt.Sprintf("https://bmc-%02d.physical.example.test/redfish/v1/Systems/1", number)), "hardware", "management", "bmc", "address")
			spec = spec.WithPath(api.MapValue(api.FieldValue{Name: "method", Value: api.StringValue("logicalOverwrite")}, api.FieldValue{Name: "targets", Value: api.ListValue(targets...)}), "hardware", "eraseOnRemoval")
		}
		if object.Kind() == api.StorageCluster {
			nodes := spec.Get("ceph", "topology", "nodes").Items()
			services := []api.Value{}
			for _, service := range spec.Get("ceph", "services").Items() {
				if service.Get("service_type").Text() != "osd" {
					services = append(services, service)
					continue
				}
				placement := service.Get("placement")
				for _, node := range nodes {
					fqdn := node.Get("fqdn").Text()
					if placement.Has("hosts") && !slices.Contains(placement.Get("hosts").Strings(), fqdn) || placement.Has("label") && !slices.Contains(node.Get("labels").Strings(), placement.Get("label").Text()) {
						continue
					}
					host := node.Get("machineRef").Text()
					converted := service.With("service_id", api.StringValue(service.Get("service_id").Text()+"-"+node.Get("name").Text())).With("placement", api.MapValue(api.FieldValue{Name: "hosts", Value: api.StringList(fqdn)}))
					for _, member := range []string{"data_devices", "db_devices", "wal_devices"} {
						if !service.Has("spec", member) {
							continue
						}
						selected := []string{}
						for _, path := range service.Get("spec", member, "paths").Strings() {
							physical, ok := paths[host][path]
							if !ok {
								t.Fatalf("%s selects undeclared path %s", host, path)
							}
							selected = append(selected, physical)
						}
						converted = converted.WithPath(api.StringList(selected...), "spec", member, "paths")
					}
					services = append(services, converted)
				}
			}
			spec = spec.WithPath(api.ListValue(services...), "ceph", "services")
		}
		objects[index] = object.WithSpec(spec)
	}
	data, err := encoding.YAML(context.Background(), api.NewCatalog(objects))
	if err != nil {
		t.Fatal(err)
	}
	// Compile the converted declarations, including selection and normalization.
	sources := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("physical-matrix.yaml", data)}}
	state, _ := compileAcceptance(t, sources)
	return state
}

func physicalMatrixTarget(path, wwn string, serial int, size int64) api.Value {
	return api.MapValue(api.FieldValue{Name: "deviceName", Value: api.StringValue(path)}, api.FieldValue{Name: "wwn", Value: api.StringValue(wwn)}, api.FieldValue{Name: "serialNumber", Value: api.StringValue(fmt.Sprintf("SYNTHETIC-%04d", serial))}, api.FieldValue{Name: "sizeBytes", Value: api.IntegerValue(fmt.Sprint(size))})
}
