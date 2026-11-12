package cephcluster

import (
	"fmt"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func RootDiskProtections(catalog api.Catalog, m api.Object) ([]diskselection.DiskIdentity, error) {
	var protected []diskselection.DiskIdentity
	for _, cluster := range catalog.OfKind(api.CephCluster) {
		var nodes []string
		for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
			if node.Get("machineRef").Text() == m.Name() {
				nodes = append(nodes, node.Get("name").Text())
			}
		}
		if len(nodes) == 0 {
			continue
		}
		for _, service := range cluster.Spec().Get("services").Items() {
			if service.Get("service_type").Text() != "osd" {
				continue
			}
			placed, issues := ResolveServicePlacement(cluster, service)
			if len(issues) != 0 {
				return nil, fmt.Errorf("Ceph OSD placement cannot be resolved before root discovery")
			}
			if !slices.ContainsFunc(nodes, func(name string) bool { return slices.Contains(placed, name) }) {
				continue
			}
			for _, key := range []string{"data_devices", "db_devices", "wal_devices"} {
				selector := service.Get("spec", key)
				if !selector.Present() {
					continue
				}
				paths := nativeDevicePaths(selector)
				if len(paths) == 0 || len(selector.Fields()) != 1 || !selector.Has("paths") {
					return nil, fmt.Errorf("%s %s must use stable explicit paths before automatic root discovery", cluster.Identity(), key)
				}
				for _, path := range paths {
					claim, err := protectedDisk(catalog, m, path)
					if err != nil {
						return nil, err
					}
					protected = append(protected, claim)
				}
			}
		}
	}
	return protected, nil
}

func protectedDisk(catalog api.Catalog, m api.Object, path string) (diskselection.DiskIdentity, error) {
	if strings.HasPrefix(path, "/dev/disk/by-id/wwn-") {
		claim := diskselection.DiskIdentity{WWN: strings.TrimPrefix(path, "/dev/disk/by-id/wwn-")}
		if claim.ValidateProtection() == nil {
			return claim, nil
		}
	}
	if serial, ok := substrate.SerialDevicePath(path); ok {
		value, size, reason, _ := SerialSelection(catalog, m, path)
		if reason == "" && serial == value && size > 0 {
			return diskselection.DiskIdentity{SerialNumber: serial, SizeBytes: size}, nil
		}
	}
	return diskselection.DiskIdentity{}, fmt.Errorf("Ceph data, DB and WAL paths on %s must have a qualified WWN, or a serial and exact declared size, before root discovery", m.Identity())
}

func RootDiskRefinement(before, after api.Object, owned reconciliation.Plan) bool {
	if before.Kind() != api.CephCluster || after.Kind() != api.CephCluster || before.Name() != after.Name() {
		return false
	}
	for _, block := range owned.Blocks {
		if block.Kind == string(api.CephCluster) && block.Object == after.Name() {
			return false
		}
	}
	mask := func(object api.Object) api.Value {
		services := object.Spec().Get("services").Items()
		for i, service := range services {
			if service.Get("service_type").Text() != "osd" {
				continue
			}
			spec := service.Get("spec")
			for _, key := range []string{"data_devices", "db_devices", "wal_devices"} {
				spec = spec.Without(key)
			}
			services[i] = service.With("spec", spec)
		}
		return object.Value().WithPath(api.ListValue(services...), "spec", "services")
	}
	return mask(before).Equal(mask(after))
}
