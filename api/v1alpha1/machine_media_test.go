package v1alpha1

import (
	"slices"
	"testing"
)

func TestDiskMediaIsOptionalAndSpecificToLibvirtDataDisks(t *testing.T) {
	for _, variant := range []string{"libvirt", "vsphere", "kubevirt"} {
		shape := machineProfileSchema(variant).Element
		disks, found := shape.Field("dataDisks")
		if variant == "kubevirt" {
			if found {
				t.Fatal("KubeVirt unexpectedly accepts data disks")
			}
			continue
		}
		media, found := disks.Shape.Element.Field("media")
		if found != (variant == "libvirt") || found && (media.Required || media.Default.Present() || !slices.Equal(media.Shape.Enums, []string{"ssd", "hdd"})) {
			t.Fatal("disk media expanded another substrate or changed omitted defaults")
		}
		if _, found := shape.Field("media"); found {
			t.Fatal("data-disk media became a root-disk switch")
		}
	}
}
