package substrate

import (
	"regexp"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// RootDeviceHints are the root-device hints a Machine declares, each kept as
// admission left it so no consumer loses one before deciding whether it can
// carry it. A string hint is empty when absent, the sizes are the declared
// decimal text or empty, and Rotational is nil when absent.
type RootDeviceHints struct {
	DeviceName, HCTL, Model, Vendor, SerialNumber, WWN string
	MinSizeGigabytes, SizeBytes                        string
	Rotational                                         *bool
	VolumeRef                                          *VolumeRef
}

// Names are the admitted names of the hints present, sorted.
func (h RootDeviceHints) Names() []string {
	var names []string
	for _, hint := range []struct{ name, value string }{
		{"deviceName", h.DeviceName}, {"hctl", h.HCTL}, {"model", h.Model}, {"vendor", h.Vendor},
		{"serialNumber", h.SerialNumber}, {"wwn", h.WWN}, {"minSizeGigabytes", h.MinSizeGigabytes},
		{"sizeBytes", h.SizeBytes},
	} {
		if hint.value != "" {
			names = append(names, hint.name)
		}
	}
	if h.VolumeRef != nil {
		if h.VolumeRef.Firmware {
			names = append(names, "bootVolume")
		} else {
			names = append(names, "volumeRef")
		}
	}
	if h.Rotational != nil {
		names = append(names, "rotational")
	}
	slices.Sort(names)
	return names
}

func rootDeviceHints(machine api.Object) RootDeviceHints {
	declared := machine.Spec().Get("hardware", "rootDevice")
	physical := machine.Spec().Get("substrate", ArmBaremetal, "rootDevice")
	hints := RootDeviceHints{
		DeviceName: declared.Get("deviceName").Text(), HCTL: physical.Get("hctl").Text(),
		Model: declared.Get("model").Text(), Vendor: declared.Get("vendor").Text(),
		SerialNumber: physical.Get("serialNumber").Text(), WWN: physical.Get("wwn").Text(),
		MinSizeGigabytes: declared.Get("minSizeGigabytes").Text(), SizeBytes: physical.Get("sizeBytes").Text(),
	}
	if volume := physical.Get("volumeRef"); volume.Present() {
		hints.VolumeRef = &VolumeRef{StorageID: volume.Get("storageId").Text(), VolumeID: volume.Get("volumeId").Text()}
	}
	if physical.Has("bootVolume") {
		hints.VolumeRef = &VolumeRef{Firmware: true}
	}
	if rotational := declared.Get("rotational"); rotational.Present() {
		value := rotational.Bool()
		hints.Rotational = &value
	}
	return hints
}

func RootDeviceHintPath(name string) string {
	if slices.Contains([]string{"hctl", "serialNumber", "sizeBytes", "wwn", "volumeRef", "bootVolume"}, name) {
		return "spec.substrate." + ArmBaremetal + ".rootDevice." + name
	}
	return "spec.hardware.rootDevice." + name
}

func RootDevicePath(machine api.Object) string {
	return machine.Spec().Get("hardware", "rootDevice", "deviceName").Text()
}

func MachineRootDeviceHints(machine api.Object) RootDeviceHints { return rootDeviceHints(machine) }

const SerialDevicePrefix = "/dev/disk/by-serial/"

var serialDevicePath = regexp.MustCompile(`^/dev/disk/by-serial/([A-Za-z0-9._:+-]{1,128})$`)

func SerialDevicePath(path string) (string, bool) {
	match := serialDevicePath.FindStringSubmatch(path)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func SerialEraseTarget(machine api.Object, serial string) (size int64, byDevice bool, found bool) {
	for _, target := range machine.Spec().Get("hardware", "eraseOnRemoval", "targets").Items() {
		if target.Get("serialNumber").Text() != serial {
			continue
		}
		size, _ = target.Get("sizeBytes").Int64()
		return size, target.Has("deviceName"), true
	}
	return 0, false, false
}

type VolumeRef struct {
	Firmware  bool   `json:"firmware,omitempty"`
	StorageID string `json:"storageId,omitempty"`
	VolumeID  string `json:"volumeId,omitempty"`
}

var volumeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var volumeWWN = regexp.MustCompile(`^0x[0-9a-f]{16}(?:[0-9a-f]{16})?$`)

func (r VolumeRef) Valid() bool {
	return r.Firmware && r.StorageID == "" && r.VolumeID == "" || !r.Firmware && volumeID.MatchString(r.StorageID) && volumeID.MatchString(r.VolumeID)
}

type RootVolumeProof struct {
	SizeBytes int64  `json:"sizeBytes"`
	StorageID string `json:"storageId"`
	VolumeID  string `json:"volumeId"`
	WWN       string `json:"wwn"`
}

func (p RootVolumeProof) Ref() VolumeRef {
	return VolumeRef{StorageID: p.StorageID, VolumeID: p.VolumeID}
}
func (p RootVolumeProof) Valid() bool {
	return p.Ref().Valid() && volumeWWN.MatchString(p.WWN) && p.WWN != "0x0000000000000000" && p.WWN != "0x00000000000000000000000000000000" && p.SizeBytes >= 4096 && p.SizeBytes <= 64<<40 && p.SizeBytes%4096 == 0
}

func (r VolumeRef) Matches(p RootVolumeProof) bool {
	return r.Valid() && p.Valid() && (r.Firmware || p.Ref() == r)
}
