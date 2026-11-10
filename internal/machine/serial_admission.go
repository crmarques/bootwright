package machine

import (
	"fmt"
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	rootSizePath   = "$.spec.substrate.baremetal.rootDevice.sizeBytes"
	rootSerialPath = "$.spec.substrate.baremetal.rootDevice.serialNumber"
)

// ValidDiskSerial accepts the serial grammar the physical disk workers prove.
func ValidDiskSerial(value string) bool { return clearSerial.MatchString(value) }

func validateRootSize(o api.Object) []api.Issue {
	size := substrate.MachineRootDeviceHints(o).SizeBytes
	if size == "" {
		return nil
	}
	agentProof := !o.Spec().Get("os", "provided").Bool() && substrate.MachineRootDeviceHints(o).SerialNumber != ""
	if !installed(o) && !agentProof {
		return []api.Issue{invariant(rootSizePath, "sizeBytes is the exact capacity a Bootwright installation proves its root disk by, and nothing installs this Machine with Bootwright",
			"remove spec.substrate.baremetal.rootDevice.sizeBytes")}
	}
	if bytes, err := strconv.ParseInt(size, 10, 64); err != nil || bytes%4096 != 0 {
		return []api.Issue{invariant(rootSizePath, "a root disk's sizeBytes is its exact logical capacity, aligned to 4096 bytes",
			"set sizeBytes to the root disk's exact logical capacity, as blockdev --getsize64 reports it")}
	}
	return nil
}

type selectingSerial struct{ field, serial string }

func selectingSerials(o api.Object) []selectingSerial {
	var selecting []selectingSerial
	hints := substrate.MachineRootDeviceHints(o)
	if hints.DeviceName == "" && (hints.WWN == "" || !installed(o)) && hints.SerialNumber != "" && hints.SizeBytes != "" {
		selecting = append(selecting, selectingSerial{rootSerialPath, hints.SerialNumber})
	}
	for _, policy := range []string{"eraseOnRemoval", "clearOnInstall"} {
		for index, target := range o.Spec().Get("hardware", policy, "targets").Items() {
			if serial := target.Get("serialNumber").Text(); !target.Has("deviceName") && serial != "" {
				selecting = append(selecting, selectingSerial{fmt.Sprintf("$.spec.hardware.%s.targets[%d].serialNumber", policy, index), serial})
			}
		}
	}
	return selecting
}

func declaredSerials(o api.Object) map[string]bool {
	declared := map[string]bool{}
	if serial := substrate.MachineRootDeviceHints(o).SerialNumber; serial != "" {
		declared[serial] = true
	}
	for _, policy := range []string{"eraseOnRemoval", "clearOnInstall"} {
		for _, target := range o.Spec().Get("hardware", policy, "targets").Items() {
			if serial := target.Get("serialNumber").Text(); serial != "" {
				declared[serial] = true
			}
		}
	}
	return declared
}

func validateSelectingSerials(o api.Object, c api.Catalog) []api.Issue {
	selecting := selectingSerials(o)
	if len(selecting) == 0 {
		return nil
	}
	issues := []api.Issue{}
	for _, other := range c.OfKind(api.Machine) {
		if other.Identity() == o.Identity() {
			continue
		}
		declared := declaredSerials(other)
		for _, entry := range selecting {
			if declared[entry.serial] {
				remedy := "correct the serial number here or on " + other.Identity() + ", or declare spec.substrate.baremetal.rootDevice.wwn as the disk's stable WWN"
				if entry.field != rootSerialPath {
					remedy = "correct the serial number here or on " + other.Identity() + ", or name the disk by its /dev/disk/by-id/wwn-0x deviceName with its wwn"
				}
				issues = append(issues, invariant(entry.field, "a disk selected by its serial number needs a serial number no other Machine declares, and "+other.Identity()+" declares "+entry.serial,
					remedy))
			}
		}
	}
	return issues
}
