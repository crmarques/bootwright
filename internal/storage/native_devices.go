package storage

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func nativeDevicePaths(selector api.Value) []string {
	paths := []string{}
	for _, value := range selector.Get("paths").Items() {
		if value.Type() == api.String {
			paths = append(paths, value.Text())
		} else {
			paths = append(paths, textOf(value, "path"))
		}
	}
	return paths
}

func (a *admission) nativeSelector(selector api.Value, field string, data, partial bool) {
	paths, all := selector.Has("paths"), selector.Get("all").Bool()
	filters := selector.Has("model") || selector.Has("vendor") || selector.Has("rotational") || selector.Has("size")
	if (paths && all) || ((paths || all) && filters) {
		a.invalid(field, "native paths, all-device selection and filters are mutually exclusive", "keep one selector form: paths, all: true, or device filters")
	}
	if all && !data {
		a.invalid(field+".all", "all-device selection is valid only for data devices", "remove all: true from DB/WAL selectors and select explicit paths or filters")
	}
	if !partial && len(nativeDevicePaths(selector)) == 0 && !all && !filters {
		a.invalid(field, "native device selector must select paths, all data devices or a filter", "add paths, set all to true for data devices, or add a device filter")
	}
	if selector.Has("size") {
		a.lexical(selector.Get("size"), field+".size", validDeviceRange(textOf(selector, "size")))
	}
	seen := map[string]bool{}
	for _, device := range nativeDevicePaths(selector) {
		if !cleanDevice(device) {
			a.invalid(field+".paths", "native device paths must be clean absolute descendants of /dev", "replace paths with absolute paths beneath /dev without dot or parent components")
		}
		if seen[device] {
			a.invalid(field+".paths", "native device path identities must be unique", "remove duplicate paths from this device selector")
		}
		seen[device] = true
	}
}

func (a *admission) nativeOSD(spec api.Value, field string, nodes []api.Value, partial bool) {
	for _, key := range []string{"data_devices", "db_devices", "wal_devices"} {
		selector := spec.Get(key)
		if !selector.Present() {
			continue
		}
		a.nativeSelector(selector, field+"."+key, key == "data_devices", partial)
		if partial {
			continue
		}
		for _, node := range nodes {
			machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
			if !ok {
				continue
			}
			root := textOf(machine.Spec(), "os", "install", "rootDeviceHints", "deviceName")
			for _, device := range nativeDevicePaths(selector) {
				if root != "" && device == root {
					a.invalid(field+"."+key, "explicit OSD allocation cannot use the Machine root device", "remove the Machine root device from the OSD selector and select a dedicated data device")
				}
			}
		}
	}
	for _, key := range []string{"db_devices", "wal_devices"} {
		for _, device := range nativeDevicePaths(spec.Get(key)) {
			if slices.Contains(nativeDevicePaths(spec.Get("data_devices")), device) {
				a.invalid(field+"."+key, "OSD data allocation cannot overlap its DB or WAL devices", "select disjoint data_devices and db_devices/wal_devices paths")
			}
		}
	}
	if spec.Get("tpm2").Bool() && (!partial || spec.Has("encrypted")) && !spec.Get("encrypted").Bool() {
		a.invalid(field+".tpm2", "TPM2 OSD unlock requires encryption", "set encrypted to true or remove tpm2")
	}
	if spec.Get("tpm2").Bool() && !partial {
		for _, node := range nodes {
			machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
			if !ok {
				continue
			}
			provider, ok := a.catalog.Find(api.InfraProvider, textOf(machine.Spec(), "substrate", "providerRef"))
			if !ok || provider.Spec().Has("baremetal") {
				continue
			}
			for _, variant := range []string{"libvirt", "vsphere", "kubevirt"} {
				for _, profile := range provider.Spec().Get(variant, "machineProfiles").Items() {
					if textOf(profile, "name") == textOf(machine.Spec(), "substrate", "profileRef") && !profile.Has("tpm") {
						a.invalid(field+".tpm2", "every selected virtual Machine profile must declare TPM support", "enable TPM support in every selected virtual Machine profile or remove tpm2")
					}
				}
			}
		}
	}
}

func nativeMember(cluster api.Object, serviceType, nodeName string) bool {
	for _, service := range cluster.Spec().Get("ceph", "services").Items() {
		if textOf(service, "service_type") != serviceType {
			continue
		}
		names, issues := ResolveServicePlacement(cluster, service)
		if len(issues) == 0 && slices.Contains(names, nodeName) {
			return true
		}
	}
	return false
}

func (a *admission) nativeAllocationConflicts(ceph api.Value) {
	owned := map[string]bool{}
	for i, service := range ceph.Get("services").Items() {
		if textOf(service, "service_type") != "osd" {
			continue
		}
		names, issues := ResolveServicePlacement(a.object, service)
		if len(issues) != 0 {
			continue
		}
		for _, name := range names {
			for _, device := range nativeDevicePaths(service.Get("spec", "data_devices")) {
				key := name + "\x00" + device
				if owned[key] {
					a.invalid(indexed("$.spec.ceph.services", i)+".spec.data_devices", "multiple native OSD services cannot own the same host data device", "remove the overlapping data device from one OSD service or select disjoint hosts")
				}
				owned[key] = true
			}
		}
	}
}
