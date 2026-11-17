package machine

import (
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	rootSizePath   = "$." + substrate.RootDiskPath + ".sizeBytes"
	rootSerialPath = "$." + substrate.RootDiskPath + ".serialNumber"
)

func validateRootSize(o api.Object) []api.Issue {
	size := substrate.MachineRootDeviceHints(o).SizeBytes
	if size == "" {
		return nil
	}
	agentProof := !api.MachineProvided(o) && substrate.MachineRootDeviceHints(o).SerialNumber != ""
	if !installed(o) && !agentProof {
		return []api.Issue{invariant(rootSizePath, "sizeBytes is the exact capacity a Bootwright installation proves its root disk by, and nothing installs this Machine with Bootwright",
			"remove "+rootSizePath[2:])}
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
	for index, disk := range substrate.MachineDataDisks(o) {
		if serial := disk.Get("serialNumber").Text(); !disk.Has("wwn") && serial != "" {
			selecting = append(selecting, selectingSerial{"$." + substrate.DataDiskPath(index, "serialNumber"), serial})
		}
	}
	return selecting
}

func declaredSerials(o api.Object) map[string]bool {
	declared := map[string]bool{}
	if serial := substrate.MachineRootDeviceHints(o).SerialNumber; serial != "" {
		declared[serial] = true
	}
	for _, disk := range substrate.MachineDataDisks(o) {
		if serial := disk.Get("serialNumber").Text(); serial != "" {
			declared[serial] = true
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
				remedy := "correct the serial number here or on " + other.Identity() + ", or declare " + substrate.RootDiskPath + ".wwn as the disk's stable WWN"
				if entry.field != rootSerialPath {
					remedy = "correct the serial number here or on " + other.Identity() + ", or declare the data disk's wwn"
				}
				issues = append(issues, invariant(entry.field, "a disk selected by its serial number needs a serial number no other Machine declares, and "+other.Identity()+" declares "+entry.serial,
					remedy))
			}
		}
	}
	return issues
}
