package installation

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func firmwareCatalog(firmware api.Value) api.Catalog {
	catalog := physicalVariantCatalog(installProfile(), "ed25519")
	if !firmware.Present() {
		return catalog
	}
	objects := catalog.Objects()
	for index, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "metal-01" {
			objects[index] = object.WithSpec(object.Spec().WithPath(firmware, "substrate", "baremetal", "firmware"))
		}
	}
	return api.NewCatalog(objects)
}

func enforcedFirmware() api.Value {
	return api.MapValue(
		text("mode", substrate.FirmwareModeEnforce),
		field("secureBoot", api.BoolValue(true)),
		text("bootMode", "UEFI"),
		field("biosAttributes", api.MapValue(
			text("ProcVirtualization", "Enabled"),
			field("PowerCycleDelay", api.IntegerValue("5")),
			field("AcPwrRcvryUserDelay", api.BoolValue(false)),
		)),
	)
}

func TestAPhysicalFirmwareRequestFreezesEveryFieldInItsGolden(t *testing.T) {
	request, _ := onlyRequest(t, firmwareCatalog(enforcedFirmware()))
	enabled, disabled, delay, virtualization := true, false, int64(5), "Enabled"
	want := &Firmware{
		BIOSAttributes: []BIOSAttribute{
			{Name: "AcPwrRcvryUserDelay", Boolean: &disabled},
			{Name: "PowerCycleDelay", Integer: &delay},
			{Name: "ProcVirtualization", String: &virtualization},
		},
		BootMode: "UEFI", Mode: substrate.FirmwareModeEnforce, SecureBoot: &enabled,
	}
	if !reflect.DeepEqual(request.Target.Firmware, want) {
		t.Fatalf("frozen firmware = %+v, want %+v", request.Target.Firmware, want)
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatalf("freezing: %v", diagnostics.Of(err))
	}
	matchesGolden(t, "request-physical-firmware", canonical)
	golden, err := os.ReadFile(filepath.Join("testdata", "request-physical-firmware.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, golden); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(compact.Bytes())
	if err != nil {
		t.Fatalf("decoding the golden: %v", diagnostics.Of(err))
	}
	if !reflect.DeepEqual(decoded.Target.Firmware, want) {
		t.Fatalf("the golden decodes firmware %+v, want %+v", decoded.Target.Firmware, want)
	}
	absent, _ := onlyRequest(t, firmwareCatalog(api.Value{}))
	if absent.Target.Firmware != nil {
		t.Fatalf("a Machine with no firmware block froze %+v", absent.Target.Firmware)
	}
	without := request
	without.Target.Firmware = nil
	stripped, err := without.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	plain, err := absent.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stripped, plain) {
		t.Fatal("a firmware block changed the request beyond target.firmware")
	}
}

func TestOnlyEnforcedFirmwareAddsItsImpactAndBound(t *testing.T) {
	prove := api.MapValue(text("mode", substrate.FirmwareModeProve), field("secureBoot", api.BoolValue(true)), text("bootMode", "UEFI"))
	absent, _ := onlyRequest(t, firmwareCatalog(api.Value{}))
	for name, test := range map[string]struct {
		firmware  api.Value
		configure bool
		margin    bool
	}{
		"absent":  {api.Value{}, false, false},
		"prove":   {prove, false, false},
		"enforce": {enforcedFirmware(), true, true},
	} {
		t.Run(name, func(t *testing.T) {
			request, _ := onlyRequest(t, firmwareCatalog(test.firmware))
			if got := slices.Contains(impacts(reconciliation.Apply, request), "configure-firmware metal-01"); got != test.configure {
				t.Errorf("configure-firmware impact = %t, want %t", got, test.configure)
			}
			if slices.Contains(impacts(reconciliation.Destroy, request), "configure-firmware metal-01") {
				t.Error("a removal claims to configure firmware")
			}
			want := time.Duration(0)
			if test.margin {
				want = substrate.BaremetalFirmwareBound
			}
			if grown := request.Deadline() - absent.Deadline(); grown != want {
				t.Errorf("the deadline grew by %s, want %s", grown, want)
			}
		})
	}
	libvirt := absent
	libvirt.Target.Substrate = substrate.ArmLibvirt
	enforced := libvirt
	enforced.Target.Firmware = &Firmware{Mode: substrate.FirmwareModeEnforce}
	if enforced.Deadline() != libvirt.Deadline() {
		t.Error("an emulated target's deadline grew by a firmware bound its arm does not publish")
	}
}
