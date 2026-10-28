package agentinstall

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func firmwareCatalog(firmware map[string]api.Value) api.Catalog {
	objects := physicalCatalog().Objects()
	for index, object := range objects {
		if declared, ok := firmware[object.Name()]; ok && object.Kind() == api.Machine {
			objects[index] = object.WithSpec(object.Spec().WithPath(declared, "substrate", "baremetal", "firmware"))
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
			number("PowerCycleDelay", "5"),
			field("AcPwrRcvryUserDelay", api.BoolValue(false)),
		)),
	)
}

func provedFirmware() api.Value {
	return api.MapValue(text("mode", substrate.FirmwareModeProve), field("secureBoot", api.BoolValue(true)), text("bootMode", "UEFI"))
}

func TestAPhysicalNodesFirmwareIsFrozenWithEveryFieldInItsGolden(t *testing.T) {
	_, request, _ := onlyRequests(t, firmwareCatalog(map[string]api.Value{"metal-01": enforcedFirmware()}))
	enabled, disabled, delay, virtualization := true, false, int64(5), "Enabled"
	want := &Firmware{
		BIOSAttributes: []BIOSAttribute{
			{Name: "AcPwrRcvryUserDelay", Boolean: &disabled},
			{Name: "PowerCycleDelay", Integer: &delay},
			{Name: "ProcVirtualization", String: &virtualization},
		},
		BootMode: "UEFI", Mode: substrate.FirmwareModeEnforce, SecureBoot: &enabled,
	}
	if request.Nodes[0].Machine != "metal-01" || !reflect.DeepEqual(request.Nodes[0].Firmware, want) {
		t.Fatalf("node %s froze firmware %+v, want %+v", request.Nodes[0].Machine, request.Nodes[0].Firmware, want)
	}
	for _, node := range request.Nodes[1:] {
		if node.Firmware != nil {
			t.Fatalf("node %s declares no firmware and froze %+v", node.Machine, node.Firmware)
		}
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatalf("freezing: %v", diagnostics.Of(err))
	}
	matchesGolden(t, "request-install-physical-firmware", canonical)
	golden, err := os.ReadFile(filepath.Join("testdata", "request-install-physical-firmware.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, golden); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeInstallRequest(compact.Bytes())
	if err != nil {
		t.Fatalf("decoding the golden: %v", diagnostics.Of(err))
	}
	if !reflect.DeepEqual(decoded.Nodes[0].Firmware, want) {
		t.Fatalf("the golden decodes firmware %+v, want %+v", decoded.Nodes[0].Firmware, want)
	}
	_, absent, _ := onlyRequests(t, physicalCatalog())
	without := request
	without.Nodes = slices.Clone(request.Nodes)
	without.Nodes[0].Firmware = nil
	without.Budgets = absent.Budgets
	stripped, err := without.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	plain, err := absent.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stripped, plain) {
		t.Fatal("a firmware block changed the request beyond the node's firmware and the boot budget")
	}
}

func TestOnlyEnforcedNodesAddTheirImpactAndBootShare(t *testing.T) {
	_, absent, _ := onlyRequests(t, physicalCatalog())
	for name, test := range map[string]struct {
		firmware  map[string]api.Value
		configure []string
		grown     time.Duration
	}{
		"absent":  {nil, nil, 0},
		"prove":   {map[string]api.Value{"metal-01": provedFirmware()}, nil, 0},
		"enforce": {map[string]api.Value{"metal-02": enforcedFirmware()}, []string{"configure-firmware metal-02"}, substrate.BaremetalFirmwareBound},
		"one round": {map[string]api.Value{"metal-01": enforcedFirmware(), "metal-02": provedFirmware(), "metal-03": enforcedFirmware()},
			[]string{"configure-firmware metal-01", "configure-firmware metal-03"}, substrate.BaremetalFirmwareBound},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := firmwareCatalog(test.firmware)
			_, request, _ := onlyRequests(t, catalog)
			var configured []string
			for _, impact := range installImpacts(reconciliation.Apply, request) {
				if strings.HasPrefix(impact, "configure-firmware ") {
					configured = append(configured, impact)
				}
			}
			if !slices.Equal(configured, test.configure) {
				t.Errorf("apply impacts configure %v, want %v", configured, test.configure)
			}
			for _, impact := range installImpacts(reconciliation.Destroy, request) {
				if !strings.HasPrefix(impact, "eject-media ") {
					t.Errorf("a removal claims %q", impact)
				}
			}
			if grown := seconds(request.Budgets.BootSeconds - absent.Budgets.BootSeconds); grown != test.grown {
				t.Errorf("the boot budget grew by %s, want %s", grown, test.grown)
			}
			if request.Budgets.RoundMarginSeconds != absent.Budgets.RoundMarginSeconds || request.Deadline()-absent.Deadline() != test.grown {
				t.Errorf("the deadline grew by %s with round margin %d, want %s with %d",
					request.Deadline()-absent.Deadline(), request.Budgets.RoundMarginSeconds, test.grown, absent.Budgets.RoundMarginSeconds)
			}
			cluster, _ := catalog.Find(api.OpenShiftCluster, "metal")
			if reason, _ := pastTheCeiling(catalog, cluster); reason != "" {
				t.Errorf("a three-node cluster refused its ceiling: %s", reason)
			}
		})
	}
}

// A round's nodes boot side by side, so the boot phase grows by one firmware
// bound for each round an enforced node can fall in, and by none for an arm
// that publishes no firmware bound.
func TestTheFirmwareBootShareGrowsWithTheRoundsEnforcedNodesCanFill(t *testing.T) {
	plain := installBudgets(40, nil, substrate.ArmBaremetal)
	for _, test := range []struct {
		enforced int
		rounds   int
	}{{0, 0}, {1, 1}, {2, 2}, {3, 3}, {40, 3}} {
		enforced := slices.Repeat([]string{substrate.ArmBaremetal}, test.enforced)
		budgets := installBudgets(40, enforced, substrate.ArmBaremetal)
		want := plain
		want.BootSeconds += test.rounds * int(substrate.BaremetalFirmwareBound/time.Second)
		if budgets != want {
			t.Errorf("%d enforced of 40 nodes: %+v, want %+v", test.enforced, budgets, want)
		}
	}
	if emulated := installBudgets(4, []string{substrate.ArmLibvirt}, substrate.ArmLibvirt); emulated != installBudgets(4, nil, substrate.ArmLibvirt) {
		t.Errorf("an emulated arm grew its boot budget by a firmware bound it does not publish: %+v", emulated)
	}
	if largestCluster(slices.Repeat([]string{substrate.ArmBaremetal}, 32), substrate.ArmBaremetal) >= largestCluster(nil, substrate.ArmBaremetal) {
		t.Error("enforced firmware did not shrink the largest cluster the ceiling admits")
	}
}
