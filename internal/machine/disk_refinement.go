package machine

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/substrate"
)

func RefinesUnownedDataTargets(before, after api.Object, oldClaims, newClaims []diskselection.DiskIdentity, inventory []diskselection.Disk) bool {
	if before.Kind() != api.Machine || after.Kind() != api.Machine || before.Name() != after.Name() {
		return false
	}
	if len(inventory) == 0 || len(inventory) > diskselection.MaxDisks || len(oldClaims) > diskselection.MaxDisks || len(newClaims) > diskselection.MaxDisks {
		return false
	}
	old, fresh := before.Spec().Get("hardware", "dataDisks"), after.Spec().Get("hardware", "dataDisks")
	if old.Equal(fresh) {
		return before.Value().Equal(after.Value())
	}
	prior, current := old.Items(), fresh.Items()
	if !refinementActions(prior, current) || !refinementTargets(prior, oldClaims, nil) || !refinementTargets(current, newClaims, inventory) {
		return false
	}
	left := before.Value().WithPath(api.ListValue(), "spec", "hardware", "dataDisks")
	right := after.Value().WithPath(api.ListValue(), "spec", "hardware", "dataDisks")
	return left.Equal(right)
}

func refinementActions(before, after []api.Value) bool {
	if len(before) == 0 || len(after) == 0 || len(before) > MaxDataDisks || len(after) > MaxDataDisks {
		return false
	}
	classify := func(disk api.Value) string {
		install := disk.Get("onInstall").Text()
		if install == "" {
			install = "retain"
		}
		return install + "/" + substrate.RemovalAction(disk)
	}
	if len(before) == len(after) {
		for i := range before {
			if classify(before[i]) != classify(after[i]) {
				return false
			}
		}
		return true
	}
	wanted := classify(before[0])
	return !slices.ContainsFunc(append(slices.Clone(before), after...), func(disk api.Value) bool { return classify(disk) != wanted })
}

func refinementTargets(targets []api.Value, claims []diskselection.DiskIdentity, inventory []diskselection.Disk) bool {
	if len(targets) == 0 || len(claims) == 0 {
		return false
	}
	for _, target := range targets {
		for _, field := range target.Fields() {
			if !slices.Contains([]string{"wwn", "serialNumber", "sizeBytes", "onInstall", "onRemoval"}, field.Name) {
				return false
			}
		}
		size, ok := target.Get("sizeBytes").Int64()
		disk := diskselection.DiskIdentity{WWN: target.Get("wwn").Text(), SerialNumber: target.Get("serialNumber").Text(), SizeBytes: size}
		if !ok || disk.Validate() != nil {
			return false
		}
		if disk.WWN != "" && substrate.WWNDevicePath(disk.WWN) == "" {
			return false
		}
		if !slices.ContainsFunc(claims, func(claim diskselection.DiskIdentity) bool {
			return claim.ValidateProtection() == nil && (claim.WWN == "" || claim.WWN == disk.WWN) && (claim.SerialNumber == "" || claim.SerialNumber == disk.SerialNumber) && (claim.SizeBytes == 0 || claim.SizeBytes == disk.SizeBytes)
		}) {
			return false
		}
		if inventory != nil {
			count := 0
			for _, observed := range inventory {
				if observed.Fixed && observed.Writable && observed.Whole && !observed.Hidden && observed.Transport != "usb" && observed.Transport != "unknown" && observed.SizeBytes == disk.SizeBytes && (disk.WWN == "" || disk.WWN == observed.WWN) && (disk.SerialNumber == "" || disk.SerialNumber == observed.SerialNumber) {
					count++
				}
			}
			if count != 1 {
				return false
			}
		}
	}
	return true
}
