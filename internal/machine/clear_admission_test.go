package machine

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func clearMachine(targets ...api.Value) api.Object {
	machine, _ := fixture()
	spec := machine.Spec().WithPath(api.StringValue("rhel"), "os", "installProfileRef").
		WithPath(api.StringValue("/dev/disk/by-id/wwn-0x6000000000000001"), "hardware", "rootDevice", "deviceName").
		WithPath(api.StringValue("SYNTHETIC-ROOT-01"), "substrate", "baremetal", "rootDevice", "serialNumber")
	if len(targets) != 0 {
		spec = spec.WithPath(m("targets", list(targets...)), "hardware", "clearOnInstall")
	}
	return machine.WithSpec(spec)
}

func clearTarget(hex, serial string) api.Value {
	return m("deviceName", "/dev/disk/by-id/wwn-0x"+hex, "wwn", "0x"+hex, "serialNumber", serial).With("sizeBytes", api.IntegerValue("107374182400"))
}

func TestClearOnInstallIsAdmittedOnlyOnAnInstalledPhysicalMachine(t *testing.T) {
	if issues := validateClear(clearMachine(), true); len(issues) != 0 {
		t.Fatal(issues)
	}
	machine := clearMachine(clearTarget("5000000000000001", "SYNTHETIC-CEPH-01"), clearTarget("5000000000000002", "SYNTHETIC-CEPH-02"))
	if issues := validateClear(machine, true); len(issues) != 0 {
		t.Fatal(issues)
	}
	upper := clearTarget("5000000000000001", "SYNTHETIC-CEPH-01").With("wwn", api.StringValue("0X5000000000000001"))
	if issues := validateClear(clearMachine(upper), true); len(issues) != 0 {
		t.Fatalf("a WWN in another case was refused: %v", issues)
	}
	for _, variant := range []string{substrate.ArmLibvirt, substrate.ArmKubeVirt, ""} {
		issues := validateClear(machine, variant == substrate.ArmBaremetal)
		if len(issues) != 1 || issues[0].Field != "$.spec.hardware.clearOnInstall" {
			t.Fatalf("nonphysical %q admitted clearOnInstall: %v", variant, issues)
		}
	}
	provided := machine.WithSpec(machine.Spec().WithPath(api.BoolValue(true), "os", "provided"))
	if issues := validateClear(provided, true); len(issues) != 1 {
		t.Fatalf("an OS-ready Machine admitted clearOnInstall: %v", issues)
	}
}

func TestClearOnInstallRefusesEveryTargetItCannotProve(t *testing.T) {
	good := clearTarget("5000000000000001", "SYNTHETIC-CEPH-01")
	sixteen := []api.Value{}
	for index := range 16 {
		sixteen = append(sixteen, clearTarget(fmt.Sprintf("50000000000000%02x", index+16), fmt.Sprintf("SYNTHETIC-CEPH-%02d", index)))
	}
	for name, test := range map[string]struct {
		targets []api.Value
		field   string
	}{
		"a kernel name":              {[]api.Value{good.With("deviceName", api.StringValue("/dev/sdb"))}, ".targets[0].deviceName"},
		"an uppercase by-id path":    {[]api.Value{good.With("deviceName", api.StringValue("/dev/disk/by-id/wwn-0x5000000000000ABC"))}, ".targets[0].deviceName"},
		"a short by-id path":         {[]api.Value{good.With("deviceName", api.StringValue("/dev/disk/by-id/wwn-0x50000000"))}, ".targets[0].deviceName"},
		"a WWN naming another disk":  {[]api.Value{good.With("wwn", api.StringValue("0x5000000000000009"))}, ".targets[0].wwn"},
		"a serial with a space":      {[]api.Value{good.With("serialNumber", api.StringValue("SYNTHETIC CEPH"))}, ".targets[0].serialNumber"},
		"a serial with a quote":      {[]api.Value{good.With("serialNumber", api.StringValue("SYNTHETIC'CEPH"))}, ".targets[0].serialNumber"},
		"a non-ASCII serial":         {[]api.Value{good.With("serialNumber", api.StringValue("é"))}, ".targets[0].serialNumber"},
		"a serial beyond its bound":  {[]api.Value{good.With("serialNumber", api.StringValue(strings.Repeat("Z", 257)))}, ".targets[0].serialNumber"},
		"an unaligned size":          {[]api.Value{good.With("sizeBytes", api.IntegerValue("107374182401"))}, ".targets[0].sizeBytes"},
		"a size past 64 TiB":         {[]api.Value{good.With("sizeBytes", api.IntegerValue("70368744181760"))}, ".targets[0].sizeBytes"},
		"a duplicate serial":         {[]api.Value{good, clearTarget("5000000000000002", "SYNTHETIC-CEPH-01")}, ".targets[1]"},
		"the root deviceName":        {[]api.Value{clearTarget("6000000000000001", "SYNTHETIC-CEPH-01")}, ".targets[0]"},
		"the root serial number":     {[]api.Value{clearTarget("5000000000000001", "SYNTHETIC-ROOT-01")}, ".targets[0]"},
		"sixteen targets":            {sixteen, ".targets"},
		"the root WWN in other case": {[]api.Value{m("deviceName", "/dev/disk/by-id/wwn-0x6000000000000001", "wwn", "0X6000000000000001", "serialNumber", "X").With("sizeBytes", api.IntegerValue("4096"))}, ".targets[0]"},
	} {
		t.Run(name, func(t *testing.T) {
			issues := validateClear(clearMachine(test.targets...), true)
			for _, issue := range issues {
				if issue.Field == "$.spec.hardware.clearOnInstall"+test.field && issue.Code == "api.invariant" {
					return
				}
			}
			t.Fatalf("unsafe clear declaration admitted at %s: %v", test.field, issues)
		})
	}
}

func TestClearOnInstallRefusesTheRootDiskByItsDeclaredWWN(t *testing.T) {
	machine := clearMachine(clearTarget("5000000000000001", "SYNTHETIC-CEPH-01"))
	machine = machine.WithSpec(machine.Spec().WithPath(api.StringValue("/dev/sda"), "hardware", "rootDevice", "deviceName").
		WithPath(api.StringValue("0x5000000000000001"), "substrate", "baremetal", "rootDevice", "wwn"))
	issues := validateClear(machine, true)
	if len(issues) != 1 || issues[0].Field != "$.spec.hardware.clearOnInstall.targets[0]" {
		t.Fatalf("the root disk's WWN was admitted as a clear target: %v", issues)
	}
}

// A clear target declared without deviceName and wwn is named by its serial
// number and size alone, which the installation resolves on the live host; a
// target naming only one of deviceName and wwn refuses, and one declaring the
// root disk's serial refuses as the root disk.
func TestClearOnInstallAdmitsATargetNamedBySerialNumberAndSize(t *testing.T) {
	serial := m("serialNumber", "SYNTHETIC-NVME-01").With("sizeBytes", api.IntegerValue("107374182400"))
	if issues := validateClear(clearMachine(serial, clearTarget("5000000000000001", "SYNTHETIC-CEPH-01")), true); len(issues) != 0 {
		t.Fatalf("a target named by serial number and size was refused: %v", issues)
	}
	for name, test := range map[string]struct {
		target api.Value
		field  string
	}{
		"a lone deviceName":      {serial.With("deviceName", api.StringValue("/dev/disk/by-id/wwn-0x5000000000000003")), ".targets[0]"},
		"a lone wwn":             {serial.With("wwn", api.StringValue("0x5000000000000003")), ".targets[0]"},
		"the root serial number": {serial.With("serialNumber", api.StringValue("SYNTHETIC-ROOT-01")), ".targets[0]"},
	} {
		t.Run(name, func(t *testing.T) {
			issues := validateClear(clearMachine(test.target), true)
			if len(issues) != 1 || issues[0].Field != "$.spec.hardware.clearOnInstall"+test.field || issues[0].Code != "api.invariant" {
				t.Fatalf("issues = %+v", issues)
			}
		})
	}
	issues := validateClear(clearMachine(serial.With("wwn", api.StringValue("0x5000000000000003"))), true)
	if !strings.Contains(issues[0].Message, "by serialNumber and sizeBytes alone") || issues[0].Remediation != "declare both deviceName and wwn, or neither" {
		t.Fatalf("issue = %+v", issues[0])
	}
}
