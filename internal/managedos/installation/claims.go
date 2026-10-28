package installation

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func (Capability) Claimed(catalog api.Catalog) []string {
	var claimed []string
	for _, machine := range InstalledMachines(catalog) {
		claimed = append(claimed, machine.Identity())
	}
	return claimed
}

func (EraseCapability) Claimed(catalog api.Catalog) []string {
	var claimed []string
	for _, machine := range InstalledMachines(catalog) {
		if machine.Spec().Has("hardware", "eraseOnRemoval") {
			claimed = append(claimed, machine.Identity())
		}
	}
	return claimed
}

func (TreeCapability) Claimed(catalog api.Catalog) []string {
	used := map[string]bool{}
	for _, machine := range InstalledMachines(catalog) {
		used[machine.Spec().Get("os", "installProfileRef").Text()] = true
	}
	var claimed []string
	for _, profile := range catalog.OfKind(api.MachineInstallProfile) {
		if used[profile.Name()] && profile.Spec().Has("installer", "anaconda", "packageSource", "hostedTree") {
			claimed = append(claimed, profile.Identity())
		}
	}
	return claimed
}
