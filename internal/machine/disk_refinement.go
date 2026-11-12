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
	left, right := before.Value(), after.Value()
	for _, key := range []string{"eraseOnRemoval", "clearOnInstall"} {
		old, fresh := before.Spec().Get("hardware", key), after.Spec().Get("hardware", key)
		if old.Equal(fresh) {
			continue
		}
		if !old.Has("targets") || !fresh.Has("targets") || !old.Without("targets").Equal(fresh.Without("targets")) {
			return false
		}
		prior, current := old.Get("targets").Items(), fresh.Get("targets").Items()
		if !refinementMethods(old, prior, current) || !refinementTargets(prior, oldClaims, nil) || !refinementTargets(current, newClaims, inventory) {
			return false
		}
		left = left.WithPath(api.ListValue(), "spec", "hardware", key, "targets")
		right = right.WithPath(api.ListValue(), "spec", "hardware", key, "targets")
	}
	return left.Equal(right)
}

func refinementMethods(policy api.Value, before, after []api.Value) bool {
	if len(before) == 0 || len(after) == 0 || len(before) > MaxEraseTargets || len(after) > MaxEraseTargets {
		return false
	}
	classify := func(target api.Value) string {
		method, global := target.Get("method").Text(), policy.Get("method").Text()
		size, _ := target.Get("sizeBytes").Int64()
		if method == "" || method == global || method == "metadataClear" && global == "logicalOverwrite" && size > MaxLogicalOverwriteBytes {
			return "policy"
		}
		return "override:" + method
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
	return !slices.ContainsFunc(append(slices.Clone(before), after...), func(target api.Value) bool { return classify(target) != wanted })
}

func refinementTargets(targets []api.Value, claims []diskselection.DiskIdentity, inventory []diskselection.Disk) bool {
	if len(targets) == 0 || len(claims) == 0 {
		return false
	}
	for _, target := range targets {
		for _, field := range target.Fields() {
			if !slices.Contains([]string{"deviceName", "wwn", "serialNumber", "sizeBytes", "method"}, field.Name) {
				return false
			}
		}
		size, ok := target.Get("sizeBytes").Int64()
		disk := diskselection.DiskIdentity{WWN: target.Get("wwn").Text(), SerialNumber: target.Get("serialNumber").Text(), SizeBytes: size}
		if !ok || disk.Validate() != nil {
			return false
		}
		path := target.Get("deviceName").Text()
		if path != "" && (disk.WWN == "" || path != substrate.WWNDevicePath(disk.WWN)) {
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
