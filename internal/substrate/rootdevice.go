package substrate

import (
	"regexp"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
)

// RootDeviceHints are the root-device hints a Machine declares, each kept as
// admission left it so no consumer loses one before deciding whether it can
// carry it. A string hint is empty when absent, the sizes are the declared
// decimal text or empty, and Rotational is nil when absent.
type RootDeviceHints struct {
	Discovery                                          bool
	DeviceName, HCTL, Model, Vendor, SerialNumber, WWN string
	MinSizeGigabytes, SizeBytes                        string
	Rotational                                         *bool
	VolumeRef                                          *VolumeRef
}

func MachineDiscoveryPolicy(machine api.Object) api.Value {
	return machine.Spec().Get("hardware", "rootDisk", "discover")
}

func MachineLinuxPreflight(machine api.Object) api.Value {
	return MachineRootDisk(machine).Get("linuxPreflight")
}

func WithMachineDiscoveryPolicy(machine api.Object, policy api.Value) api.Object {
	return machine.WithSpec(machine.Spec().WithPath(policy, "hardware", "rootDisk", "discover"))
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
	if h.Discovery {
		names = append(names, "discover")
	}
	if h.Rotational != nil {
		names = append(names, "rotational")
	}
	slices.Sort(names)
	return names
}

func rootDeviceHints(machine api.Object) RootDeviceHints {
	physical := MachineRootDisk(machine)
	declared := physical.Get("hints")
	hints := RootDeviceHints{
		Discovery:  physical.Has("discover"),
		DeviceName: declared.Get("deviceName").Text(), HCTL: declared.Get("hctl").Text(),
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
	if slices.Contains([]string{"serialNumber", "sizeBytes", "wwn", "volumeRef", "bootVolume", "discover", "onRemoval"}, name) {
		return RootDiskPath + "." + name
	}
	return RootDiskPath + ".hints." + name
}

// DeclaredRootDevicePath is the path a Machine's declared root disk is written
// through: its hints.deviceName, or the /dev/disk/by-id path its wwn derives.
func DeclaredRootDevicePath(machine api.Object) string {
	if declared := MachineRootDisk(machine).Get("hints", "deviceName").Text(); declared != "" {
		return declared
	}
	return WWNDevicePath(MachineRootDisk(machine).Get("wwn").Text())
}

// WWNDevicePath is the /dev/disk/by-id path of a root wwn, or nothing when
// the wwn is not 0x and 16 or 32 lowercase hexadecimal digits.
func WWNDevicePath(wwn string) string {
	if !ValidRootWWN(wwn) {
		return ""
	}
	return "/dev/disk/by-id/wwn-" + wwn
}

func ValidRootWWN(wwn string) bool { return volumeWWN.MatchString(wwn) }

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
	for _, target := range EraseTargets(machine) {
		if target.SerialNumber == serial {
			return target.SizeBytes, target.DeviceName != "", true
		}
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
	return p.Ref().Valid() && diskselection.ValidWWN(p.WWN) && diskselection.ValidSize(p.SizeBytes)
}

func (r VolumeRef) Matches(p RootVolumeProof) bool {
	return r.Valid() && p.Valid() && (r.Firmware || p.Ref() == r)
}
