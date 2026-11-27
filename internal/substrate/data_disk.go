package substrate

import api "github.com/crmarques/bootwright/api/v1alpha1"

type DataDiskPresentation struct {
	Target, Bus, Media string
	RotationRate       int
}

func DataDiskPresentations(disks []api.Value) ([]DataDiskPresentation, bool) {
	if len(disks) > MaxDataDisks {
		return nil, false
	}
	result := make([]DataDiskPresentation, 0, len(disks))
	virtio, scsi := 0, 0
	for _, disk := range disks {
		if disk.Get("name").Text() == "root" {
			return nil, false
		}
		presentation := DataDiskPresentation{}
		if disk.Has("media") {
			if disk.Get("media").Text() != "ssd" && disk.Get("media").Text() != "hdd" {
				return nil, false
			}
			presentation = DataDiskPresentation{Target: string([]byte{'s', 'd', byte('a' + scsi)}), Bus: "scsi", Media: disk.Get("media").Text(), RotationRate: 1}
			if presentation.Media == "hdd" {
				presentation.RotationRate = 7200
			}
			scsi++
		} else {
			presentation.Target = string([]byte{'v', 'd', byte('b' + virtio)})
			virtio++
		}
		result = append(result, presentation)
	}
	return result, true
}

type GuestDataDisk struct {
	Path, Serial string
	SizeBytes    int64
	Rotational   bool
}

func GuestDataDisks(catalog api.Catalog, machine api.Object, contextName string) ([]GuestDataDisk, bool) {
	_, profile, _ := SelectedProfile(catalog, machine)
	disks := profile.Get("dataDisks").Items()
	presentations, valid := DataDiskPresentations(disks)
	if !valid {
		return nil, false
	}
	result := make([]GuestDataDisk, 0, len(disks))
	for index, disk := range disks {
		size, _ := disk.Get("sizeGiB").Int64()
		result = append(result, GuestDataDisk{
			Path:      "/dev/" + presentations[index].Target,
			Serial:    DataDiskSerial(contextName, machine.Name(), disk.Get("name").Text()),
			SizeBytes: size << 30, Rotational: presentations[index].Media == "hdd",
		})
	}
	return result, true
}

func VirtioDiskAlias(serial string) string { return "/dev/disk/by-id/virtio-" + serial }
