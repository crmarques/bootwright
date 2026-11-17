package substrate

import (
	"reflect"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Every admitted root-device hint reaches the realized target with the type it
// was declared with; the identity a physical disk carries (wwn, serialNumber,
// sizeBytes) is declared on rootDisk itself and the other hints under
// rootDisk.hints. A Machine its provider creates declares no root disk, so its
// target carries none. A hint declared as zero or false is present, so a
// consumer never reads it as one never declared, and a size no integer type
// holds keeps its text.
func TestTheRootDeviceHintsAreReadAsDeclared(t *testing.T) {
	rotational, fixed := true, false
	for name, test := range map[string]struct {
		declared api.Value
		want     RootDeviceHints
		names    []string
	}{
		"every hint": {
			m("deviceName", "/dev/disk/by-path/pci-0000:00:04.0", "hctl", "1:0:0:0", "model", "1e3", "vendor", "0o17",
				"serialNumber", "0987654321", "wwn", "0x5000c500a1b2c3d4", "minSizeGigabytes", api.IntegerValue("0"),
				"rotational", false, "sizeBytes", api.IntegerValue("480103981056")),
			RootDeviceHints{
				DeviceName: "/dev/disk/by-path/pci-0000:00:04.0", HCTL: "1:0:0:0", Model: "1e3", Vendor: "0o17",
				SerialNumber: "0987654321", WWN: "0x5000c500a1b2c3d4", MinSizeGigabytes: "0", SizeBytes: "480103981056",
				Rotational: &fixed,
			},
			[]string{"deviceName", "hctl", "minSizeGigabytes", "model", "rotational", "serialNumber", "sizeBytes", "vendor", "wwn"},
		},
		"none":           {m(), RootDeviceHints{}, nil},
		"zero size":      {m("minSizeGigabytes", api.IntegerValue("0")), RootDeviceHints{MinSizeGigabytes: "0"}, []string{"minSizeGigabytes"}},
		"not rotational": {m("rotational", false), RootDeviceHints{Rotational: &fixed}, []string{"rotational"}},
		"wwn and rotational": {
			m("wwn", "0x5000c500a1b2c3d4", "rotational", true),
			RootDeviceHints{WWN: "0x5000c500a1b2c3d4", Rotational: &rotational}, []string{"rotational", "wwn"},
		},
		"a size past every integer type": {
			m("minSizeGigabytes", api.IntegerValue("100000000000000000000000")),
			RootDeviceHints{MinSizeGigabytes: "100000000000000000000000"}, []string{"minSizeGigabytes"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			objects := targetCatalog().Objects()
			hints, identity := m(), m()
			for _, hint := range test.declared.Fields() {
				if slices.Contains([]string{"serialNumber", "sizeBytes", "wwn"}, hint.Name) {
					identity = identity.With(hint.Name, hint.Value)
				} else {
					hints = hints.With(hint.Name, hint.Value)
				}
			}
			for index, object := range objects {
				if object.Kind() == api.Machine && object.Name() == "server" {
					objects[index] = object.WithSpec(object.Spec().WithPath(identity.With("hints", hints), "hardware", "rootDisk"))
				}
			}
			catalog := api.NewCatalog(objects)
			subject, _ := catalog.Find(api.Machine, "server")
			target, err := TargetFor(catalog, subject, "lab", "controller")
			if err != nil {
				t.Fatalf("deriving: %v", diagnostics.Of(err))
			}
			if !reflect.DeepEqual(target.RootDeviceHints, test.want) {
				t.Fatalf("hints = %+v, want %+v", target.RootDeviceHints, test.want)
			}
			if names := target.RootDeviceHints.Names(); !slices.Equal(names, test.names) {
				t.Fatalf("names = %v, want %v", names, test.names)
			}
		})
	}
	if hints := targetOf(t, "guest").RootDeviceHints; !reflect.DeepEqual(hints, RootDeviceHints{}) {
		t.Fatalf("a Machine its provider creates carries hints %+v", hints)
	}
}
