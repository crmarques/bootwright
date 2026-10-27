package cephcluster

import (
	"path"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
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
			for _, device := range paths {
				if rootDevice(machine, device) {
					a.invalid(field+"."+key, "an explicit OSD device must not be the Machine root device", "remove the Machine root device from the OSD selector and select a dedicated data device")
				}
			}
		}
	}
	if osd.Get("tpm2").Bool() {
		if !osd.Get("encrypted").Bool() {
			a.invalid(field+".tpm2", "TPM2 OSD unlock requires encryption", "set encrypted to true or remove tpm2")
		}
		for _, node := range nodes {
			machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
			if !ok {
				continue
			}
			if _, profile, found := substrate.SelectedProfile(a.catalog, machine); found && !profile.Has("tpm") {
				a.invalid(field+".tpm2", "every covered virtual Machine profile must declare TPM support", "enable TPM support in every covered virtual Machine profile or remove tpm2")
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
	paths, pathSpecs, all := selector.Has("paths"), selector.Has("pathSpecs"), selector.Get("all").Bool()
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
		a.invalid(field, "device paths, expanded paths and all selection are mutually exclusive", "keep only one of paths, pathSpecs or all: true in this device selector")
	}
	filters := selector.Has("model") || selector.Has("vendor") || selector.Has("rotational") || selector.Has("size")
	if (paths || pathSpecs || all) && filters {
		a.invalid(field, "explicit or all-device selection cannot be combined with filters", "remove device filters when selecting explicit paths or all devices")
	}
	if all && selector.Get("all").Bool() && !data {
		a.invalid(field+".all", "all-device selection is valid only for data devices", "remove all: true from DB/WAL selectors and select explicit paths or filters")
	}
	if len(selectorPaths(selector)) == 0 && !selector.Get("all").Bool() && !filters {
		a.invalid(field, "a device selector must select at least one path, all data devices, or a filter", "add a device path, set all to true for data devices, or add a filter")
	}
	seen := map[string]bool{}
	for _, value := range selectorPaths(selector) {
		if !cleanDevice(value) {
			a.issue("api.value", field, "device paths must be clean absolute descendants of /dev", "declare a literal device path beneath /dev")
		}
		if seen[value] {
			a.invalid(field, "device paths must be unique within one selector", "remove duplicate paths from this device selector")
		}
		seen[value] = true
	}
}

func rootDevice(machine api.Object, device string) bool {
	hints := substrate.MachineRootDeviceHints(machine)
	if hints.DeviceName != "" && device == hints.DeviceName {
		return true
	}
	wwn := strings.ToLower(hints.WWN)
	return wwn != "" && strings.ToLower(device) == "/dev/disk/by-id/wwn-0x"+strings.TrimPrefix(wwn, "0x")
}
