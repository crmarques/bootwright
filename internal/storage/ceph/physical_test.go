package ceph

import (
	"context"
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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
		if object.Kind() != api.StorageCluster {
			continue
		}
		services := object.Spec().Get("ceph", "services").Items()[:2]
		for n := 1; n <= 3; n++ {
			services = append(services, requestMap("service_type", "osd", "service_id", fmt.Sprintf("data-%d", n), "placement", requestMap("hosts", api.StringList(fmt.Sprintf("node-%d", n))), "spec", requestMap("data_devices", requestMap("paths", api.StringList(fmt.Sprintf("/dev/disk/by-id/wwn-0x500000000000000%d", n))))))
		}
		cluster = object.WithSpec(object.Spec().WithPath(api.ListValue(services...), "ceph", "services"))
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
		services := cluster.Spec().Get("ceph", "services").Items()
		services[2] = services[2].WithPath(api.StringList(path), "spec", "data_devices", "paths")
		changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services"))
		if _, err := baseRequest(catalog, changed, "synthetic"); err == nil {
			t.Fatal("unsafe physical selector admitted", path)
		}
	}
	services := cluster.Spec().Get("ceph", "services").Items()
	services[3] = services[3].WithPath(services[2].Get("spec", "data_devices", "paths"), "spec", "data_devices", "paths")
	if _, err := baseRequest(catalog, cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services")), "synthetic"); err == nil {
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
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: string(api.StorageCluster), Object: "example", Request: data}}
	q, err := (Capability{}).Quiescent(context.Background(), lifecycle.Probe{Block: block, RetiredWithHosts: true})
	if err != nil || q.State != lifecycle.Unproved || !strings.Contains(q.Reason, "B491") {
		t.Fatalf("physical claim release counted as retirement: %+v %v", q, err)
	}
}
