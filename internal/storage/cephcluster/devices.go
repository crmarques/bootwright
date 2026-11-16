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

func rootDevice(machine api.Object, device string) bool {
	hints := substrate.MachineRootDeviceHints(machine)
	if hints.DeviceName != "" && device == hints.DeviceName || hints.SerialNumber != "" && device == substrate.SerialDevicePrefix+hints.SerialNumber {
		return true
	}
	wwn := strings.ToLower(hints.WWN)
	return wwn != "" && strings.ToLower(device) == "/dev/disk/by-id/wwn-0x"+strings.TrimPrefix(wwn, "0x")
}
