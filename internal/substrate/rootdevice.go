package substrate

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// RootDeviceHints are the root-device hints a Machine declares, each kept as
// admission left it so no consumer loses one before deciding whether it can
// carry it. A string hint is empty when absent, MinSizeGigabytes is the
// declared decimal text or empty, and Rotational is nil when absent.
type RootDeviceHints struct {
	DeviceName, HCTL, Model, Vendor, SerialNumber, WWN string
	MinSizeGigabytes                                   string
	Rotational                                         *bool
}

// Names are the admitted names of the hints present, sorted.
func (h RootDeviceHints) Names() []string {
	var names []string
	for _, hint := range []struct{ name, value string }{
		{"deviceName", h.DeviceName}, {"hctl", h.HCTL}, {"model", h.Model}, {"vendor", h.Vendor},
		{"serialNumber", h.SerialNumber}, {"wwn", h.WWN}, {"minSizeGigabytes", h.MinSizeGigabytes},
	} {
		if hint.value != "" {
			names = append(names, hint.name)
		}
	}
	if h.Rotational != nil {
		names = append(names, "rotational")
	}
	slices.Sort(names)
	return names
}

// UnmatchableRootDeviceHints are the admitted names, sorted, of the hints no
// disk the provider creates can match. A libvirt domain presents file-backed
// virtio disks that carry no WWN, SCSI address or serial number, while a
// physical machine's own disks may carry any of them.
func UnmatchableRootDeviceHints(provider api.Object) []string {
	if Variant(provider) == ArmLibvirt {
		return []string{"hctl", "serialNumber", "wwn"}
	}
	return nil
}

func rootDeviceHints(machine api.Object) RootDeviceHints {
	declared := machine.Spec().Get("os", "install", "rootDeviceHints")
	hints := RootDeviceHints{
		DeviceName: declared.Get("deviceName").Text(), HCTL: declared.Get("hctl").Text(),
		Model: declared.Get("model").Text(), Vendor: declared.Get("vendor").Text(),
		SerialNumber: declared.Get("serialNumber").Text(), WWN: declared.Get("wwn").Text(),
		MinSizeGigabytes: declared.Get("minSizeGigabytes").Text(),
	}
	if rotational := declared.Get("rotational"); rotational.Present() {
		value := rotational.Bool()
		hints.Rotational = &value
	}
	return hints
}
