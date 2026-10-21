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
