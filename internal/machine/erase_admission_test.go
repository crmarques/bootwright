package machine

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestEraseAdmissionRetainsOmissionAndRequiresExplicitRootIdentity(t *testing.T) {
	machine, _ := fixture()
	machine = machine.WithSpec(machine.Spec().WithPath(api.StringValue("rhel"), "os", "installProfileRef"))
	machine = machine.WithSpec(machine.Spec().WithPath(api.BoolValue(true), "hardware", "management", "bmc", "tls", "verify").WithPath(api.StringValue("established"), "hardware", "management", "bmc", "virtualMedia", "tls", "trust"))
	if issues := validateErase(machine, substrate.ArmBaremetal); len(issues) != 0 {
		t.Fatal(issues)
	}
	target := m("deviceName", "/dev/sda", "wwn", "naa.example", "serialNumber", "EXAMPLE-01").With("sizeBytes", api.IntegerValue("8192"))
	policy := m("method", "logicalOverwrite", "targets", list(target))
	machine = machine.WithSpec(machine.Spec().WithPath(policy, "hardware", "eraseOnRemoval"))
	if issues := validateErase(machine, substrate.ArmBaremetal); len(issues) != 0 {
		t.Fatal(issues)
	}
	for _, variant := range []string{substrate.ArmLibvirt, substrate.ArmKubeVirt, ""} {
		if issues := validateErase(machine, variant); len(issues) == 0 {
			t.Fatalf("nonphysical %q admitted erase", variant)
		}
	}
	provided := machine.WithSpec(machine.Spec().WithPath(api.BoolValue(true), "os", "provided"))
	if issues := validateErase(provided, substrate.ArmBaremetal); len(issues) == 0 {
		t.Fatal("provided machine admitted erase")
	}
	for name, targets := range map[string]api.Value{
		"missing root": list(target.With("deviceName", api.StringValue("/dev/sdb"))),
		"duplicate":    list(target, target.With("deviceName", api.StringValue("/dev/sdb"))),
		"unaligned":    list(target.With("sizeBytes", api.IntegerValue("8193"))),
		"empty WWN":    list(target.With("wwn", api.StringValue(""))),
		"non ASCII":    list(target.With("serialNumber", api.StringValue("é"))),
	} {
		t.Run(name, func(t *testing.T) {
			changed := machine.WithSpec(machine.Spec().WithPath(targets, "hardware", "eraseOnRemoval", "targets"))
			if issues := validateErase(changed, substrate.ArmBaremetal); len(issues) == 0 {
				t.Fatal("unsafe erase declaration admitted")
			}
		})
	}
}
