package v1alpha1

import (
	"reflect"
	"testing"
)

func TestAnacondaRootDiskSelector(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hints *RootDeviceHints
		want  string
		ok    bool
	}{
		{name: "nil hints select nothing", hints: nil},
		{name: "empty hints select nothing", hints: &RootDeviceHints{}},
		{name: "device name loses the /dev prefix", hints: &RootDeviceHints{DeviceName: "/dev/sda"}, want: "sda", ok: true},
		{name: "a bare kernel name passes through", hints: &RootDeviceHints{DeviceName: "sda"}, want: "sda", ok: true},
		{name: "a by-id device name stays a by-id path", hints: &RootDeviceHints{DeviceName: "/dev/disk/by-id/wwn-0x5000c500"}, want: "disk/by-id/wwn-0x5000c500", ok: true},
		{name: "wwn becomes a by-id path", hints: &RootDeviceHints{WWN: "0x5000c500"}, want: "disk/by-id/wwn-0x5000c500", ok: true},
		{name: "an already-prefixed wwn is not doubled", hints: &RootDeviceHints{WWN: "wwn-0x5000c500"}, want: "disk/by-id/wwn-0x5000c500", ok: true},
		{name: "device name wins over wwn", hints: &RootDeviceHints{DeviceName: "/dev/sda", WWN: "0x5000c500"}, want: "sda", ok: true},
		{name: "predicate-only hints select nothing", hints: &RootDeviceHints{Model: "MZ7LH960", MinSizeGigabytes: 400}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := AnacondaRootDiskSelector(tc.hints)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("AnacondaRootDiskSelector = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestAnacondaUnsupportedRootDeviceHints(t *testing.T) {
	rotational := false
	hints := &RootDeviceHints{
		DeviceName:       "/dev/sda",
		HCTL:             "0:0:0:0",
		Model:            "MZ7LH960",
		Vendor:           "SAMSUNG",
		SerialNumber:     "S3Z1NB0K",
		MinSizeGigabytes: 400,
		WWN:              "0x5000c500",
		Rotational:       &rotational,
	}
	want := []string{"hctl", "model", "vendor", "serialNumber", "minSizeGigabytes", "rotational"}
	if got := AnacondaUnsupportedRootDeviceHints(hints); !reflect.DeepEqual(got, want) {
		t.Fatalf("AnacondaUnsupportedRootDeviceHints = %v, want %v; deviceName and wwn are the only two a kickstart can name a disk with", got, want)
	}
	if got := AnacondaUnsupportedRootDeviceHints(&RootDeviceHints{DeviceName: "/dev/sda", WWN: "0x5"}); got != nil {
		t.Fatalf("AnacondaUnsupportedRootDeviceHints = %v, want nil for selectable-only hints", got)
	}
	if got := AnacondaUnsupportedRootDeviceHints(nil); got != nil {
		t.Fatalf("AnacondaUnsupportedRootDeviceHints(nil) = %v, want nil", got)
	}
}
