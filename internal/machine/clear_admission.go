package machine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

const MaxClearTargets = 15

const MaxClearBytes = 64 * 1024 * 1024 * 1024 * 1024

var (
	clearDevicePath = regexp.MustCompile(`^/dev/disk/by-id/wwn-0x([0-9a-f]{16}(?:[0-9a-f]{16})?)$`)
	clearSerial     = regexp.MustCompile(`^[A-Za-z0-9._:+/=-]{1,256}$`)
)

func validateClear(o api.Object, physical bool) []api.Issue {
	policy := o.Spec().Get("hardware", "clearOnInstall")
	if !policy.Present() {
		return nil
	}
	const path = "$.spec.hardware.clearOnInstall"
	if !installed(o) || !physical {
		return []api.Issue{invariant(path, "clearOnInstall requires a Bootwright-installed physical Machine", "remove spec.hardware.clearOnInstall to leave its data disks untouched")}
	}
	issues := []api.Issue{}
	targets := policy.Get("targets").Items()
	if len(targets) < 1 || len(targets) > MaxClearTargets {
		issues = append(issues, invariant(path+".targets", "an install-time clear names 1 to 15 explicit whole data disks", "list each data disk to clear once, and never the root device"))
	}
	root := substrate.RootDevicePath(o)
	hints := substrate.MachineRootDeviceHints(o)
	rootWWN, rootSerial := clearWWN(hints.WWN), hints.SerialNumber
	if hex := clearDevicePath.FindStringSubmatch(root); hex != nil {
		rootWWN = hex[1]
	}
	seen := [3]map[string]bool{{}, {}, {}}
	for index, target := range targets {
		field := fmt.Sprintf("%s.targets[%d]", path, index)
		device, wwn, serial := target.Get("deviceName").Text(), target.Get("wwn").Text(), target.Get("serialNumber").Text()
		hex := clearDevicePath.FindStringSubmatch(device)
		if hex == nil {
			issues = append(issues, invariant(field+".deviceName", "a clear target is named by its /dev/disk/by-id/wwn-0x path, 16 or 32 lowercase hexadecimal digits", "set deviceName to the data disk's /dev/disk/by-id/wwn-0x path"))
		}
		if hex != nil && clearWWN(wwn) != hex[1] {
			issues = append(issues, invariant(field+".wwn", "a clear target's wwn must be the WWN its deviceName names", "set wwn to 0x and the hexadecimal digits of the target's deviceName"))
		}
		if !clearSerial.MatchString(serial) {
			issues = append(issues, invariant(field+".serialNumber", "a clear target's serial number must be 1 to 256 ASCII letters, digits and ._:+/=- characters", "set serialNumber to the disk's serial number as its sysfs serial attribute or VPD page 0x80 reports it"))
		}
		for slot, value := range []string{device, clearWWN(wwn), serial} {
			if value != "" && seen[slot][value] {
				issues = append(issues, invariant(field, "clear target identities must be distinct", "list each data disk once"))
			}
			seen[slot][value] = true
		}
		if device == root || clearWWN(wwn) == rootWWN && rootWWN != "" || serial == rootSerial && rootSerial != "" {
			issues = append(issues, invariant(field, "a clear target must not be the root device, by deviceName, wwn or serialNumber", "remove the root disk from spec.hardware.clearOnInstall.targets; the installation already clears it"))
		}
		size, err := strconv.ParseInt(target.Get("sizeBytes").Text(), 10, 64)
		if err != nil || size < 4096 || size > MaxClearBytes || size%4096 != 0 {
			issues = append(issues, invariant(field+".sizeBytes", "clear size must be the exact capacity, aligned to 4096 bytes and at most 64 TiB", "set sizeBytes to the target's exact logical capacity, as blockdev --getsize64 reports it"))
		}
	}
	return issues
}

func clearWWN(declared string) string {
	return strings.TrimPrefix(strings.ToLower(declared), "0x")
}
