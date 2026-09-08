package storage

import (
	"path"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func cleanAbsolute(value string) bool {
	return strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}
func cleanDevice(value string) bool {
	return cleanAbsolute(value) && strings.HasPrefix(value, "/dev/") && value != "/dev/"
}

func selectorPaths(selector api.Value) []string {
	paths := selector.Get("paths").Strings()
	for _, item := range selector.Get("pathSpecs").Items() {
		paths = append(paths, textOf(item, "path"))
	}
	return paths
}

func (a *admission) osd(osd api.Value, field string, nodes []api.Value) {
	if osd.Type() != api.Mapping {
		return
	}
	for _, key := range []string{"dataDevices", "dbDevices", "walDevices"} {
		selector := osd.Get(key)
		if !selector.Present() {
			continue
		}
		a.selector(selector, field+"."+key, key == "dataDevices")
		paths := selectorPaths(selector)
		for _, node := range nodes {
			machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
			if !ok {
				continue
			}
			root := textOf(machine.Spec(), "os", "install", "rootDeviceHints", "deviceName")
			for _, device := range paths {
				if root != "" && device == root {
					a.invalid(field+"."+key, "an explicit OSD device must not be the Machine root device")
				}
			}
		}
	}
	if osd.Get("tpm2").Bool() {
		if !osd.Get("encrypted").Bool() {
			a.invalid(field+".tpm2", "TPM2 OSD unlock requires encryption")
		}
		for _, node := range nodes {
			machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
			if !ok {
				continue
			}
			provider, ok := a.catalog.Find(api.InfraProvider, textOf(machine.Spec(), "substrate", "providerRef"))
			if !ok {
				continue
			}
			if provider.Spec().Has("baremetal") {
				continue
			}
			for _, variant := range []string{"libvirt", "vsphere", "kubevirt"} {
				if !provider.Spec().Has(variant) {
					continue
				}
				for _, profile := range provider.Spec().Get(variant, "machineProfiles").Items() {
					if textOf(profile, "name") == textOf(machine.Spec(), "substrate", "profileRef") && !profile.Has("tpm") {
						a.invalid(field+".tpm2", "every covered virtual Machine profile must declare TPM support")
					}
				}
			}
		}
	}
	for i, config := range osd.Get("serviceOverrides", "customConfigs").Items() {
		if value := config.Get("mountPath"); value.Type() == api.String && !cleanAbsolute(value.Text()) {
			a.issue("api.value", indexed(field+".serviceOverrides.customConfigs", i)+".mountPath", "mount path must be clean and absolute", "use one clean absolute path")
		}
	}
}

func (a *admission) selector(selector api.Value, field string, data bool) {
	paths, pathSpecs, all := selector.Has("paths"), selector.Has("pathSpecs"), selector.Has("all")
	modes := 0
	if paths {
		modes++
	}
	if pathSpecs {
		modes++
	}
	if all {
		modes++
	}
	if modes > 1 {
		a.invalid(field, "device paths, expanded paths and all selection are mutually exclusive")
	}
	filters := selector.Has("model") || selector.Has("vendor") || selector.Has("rotational") || selector.Has("size")
	if (paths || pathSpecs || all) && filters {
		a.invalid(field, "explicit or all-device selection cannot be combined with filters")
	}
	if all && selector.Get("all").Bool() && !data {
		a.invalid(field+".all", "all-device selection is valid only for data devices")
	}
	if len(selectorPaths(selector)) == 0 && !selector.Get("all").Bool() && !filters {
		a.invalid(field, "a device selector must select at least one path, all data devices, or a filter")
	}
	seen := map[string]bool{}
	for _, value := range selectorPaths(selector) {
		if !cleanDevice(value) {
			a.issue("api.value", field, "device paths must be clean absolute descendants of /dev", "declare a literal device path beneath /dev")
		}
		if seen[value] {
			a.invalid(field, "device paths must be unique within one selector")
		}
		seen[value] = true
	}
}
