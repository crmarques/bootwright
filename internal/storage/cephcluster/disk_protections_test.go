package cephcluster

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func protectedCatalog(t *testing.T) (api.Catalog, api.Object, api.Object) {
	t.Helper()
	catalog := fixtureCatalog()
	machine := find(t, catalog, api.Machine, "machine-1")
	cluster := find(t, catalog, api.CephCluster, "storage")
	services := cluster.Spec().Get("services").Items()
	for i, key := range []string{"data_devices", "db_devices", "wal_devices"} {
		path := []string{"/dev/disk/by-id/wwn-0x5000000000000001", "/dev/disk/by-id/wwn-0x5000000000000002", "/dev/disk/by-id/wwn-0x5000000000000003"}[i]
		services[2] = services[2].WithPath(m("paths", api.StringList(path)), "spec", key)
	}
	cluster = cluster.WithSpec(cluster.Spec().With("services", api.ListValue(services...)))
	return replace(catalog, cluster), machine, cluster
}

func TestDiscoveryProtectsEveryCephDataDBAndWALClaim(t *testing.T) {
	catalog, machine, _ := protectedCatalog(t)
	claims, err := RootDiskProtections(catalog, machine)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 3 {
		t.Fatalf("claims %+v", claims)
	}
	for _, wwn := range []string{"0x5000000000000001", "0x5000000000000002", "0x5000000000000003"} {
		if !slices.Contains(claims, diskselection.DiskIdentity{WWN: wwn}) {
			t.Fatalf("unprotected %s", wwn)
		}
	}
	foreign := api.NewObject(api.Machine, "unrelated-machine", api.MapValue(), machine.Spec())
	if claims, err := RootDiskProtections(catalog, foreign); err != nil || len(claims) != 0 {
		t.Fatalf("foreign claims %+v %v", claims, err)
	}
}

func TestDiscoveryRefusesUnprovableCephSelectors(t *testing.T) {
	catalog, machine, cluster := protectedCatalog(t)
	for _, key := range []string{"data_devices", "db_devices", "wal_devices"} {
		for _, selector := range []api.Value{m("all", true), m("paths", api.StringList("/dev/sda")), m("model", "SSD"), m("paths", api.StringList("/dev/disk/by-serial/UNDECLARED"))} {
			services := cluster.Spec().Get("services").Items()
			services[2] = services[2].WithPath(selector, "spec", key)
			changed := cluster.WithSpec(cluster.Spec().With("services", api.ListValue(services...)))
			if _, err := RootDiskProtections(replace(catalog, changed), machine); err == nil {
				t.Fatalf("accepted %s: %v", key, selector)
			}
		}
	}
}

func TestDiscoveryRefinesOnlyUnownedCephDiskSelectors(t *testing.T) {
	_, _, before := protectedCatalog(t)
	services := before.Spec().Get("services").Items()
	services[2] = services[2].WithPath(m("paths", api.StringList("/dev/disk/by-id/wwn-0x5000000000000004")), "spec", "data_devices")
	after := before.WithSpec(before.Spec().With("services", api.ListValue(services...)))
	if !RootDiskRefinement(before, after, reconciliation.Plan{}) {
		t.Fatal("unowned disk refinement refused")
	}
	owned := reconciliation.Plan{Blocks: []reconciliation.Block{{BlockDefinition: reconciliation.BlockDefinition{Kind: string(api.CephCluster), Object: before.Name()}}}}
	if RootDiskRefinement(before, after, owned) {
		t.Fatal("owned disk refinement admitted")
	}
	changed := after.WithSpec(after.Spec().With("containerImage", api.StringValue("example.invalid/changed:1")))
	if RootDiskRefinement(before, changed, reconciliation.Plan{}) {
		t.Fatal("unrelated configuration drift admitted")
	}
}
