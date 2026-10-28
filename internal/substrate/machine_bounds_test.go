package substrate

import (
	"testing"
	"time"
)

func TestMachinePortsPublishBoundsForEveryRealizedArm(t *testing.T) {
	for _, arm := range Realized() {
		bounds, ok := MachineBoundsFor(arm)
		if !ok || bounds.StateRead <= 0 || bounds.PreBoot <= 0 || bounds.MediaInsert <= 0 || bounds.MediaEject <= 0 || bounds.Power <= 0 || bounds.BootMedia <= 0 || bounds.BootDisk <= 0 || bounds.Fanout < 1 {
			t.Fatalf("%s: %+v", arm, bounds)
		}
	}
	if _, ok := MachineBoundsFor(ArmKubeVirt); ok {
		t.Fatal("unqualified arm published bounds")
	}
	if LibvirtNodeMargin != 11*time.Minute {
		t.Fatalf("emulated margin = %s", LibvirtNodeMargin)
	}
	if BaremetalNodeMargin != 2*BaremetalStateReadBound+BaremetalMediaEjectBound+ControllerBootSelectionBound {
		t.Fatal("physical round omits a port call")
	}
}

func TestOnlyThePhysicalArmPublishesAFirmwareBound(t *testing.T) {
	libvirt, _ := MachineBoundsFor(ArmLibvirt)
	baremetal, _ := MachineBoundsFor(ArmBaremetal)
	if libvirt.Firmware != 0 {
		t.Fatalf("emulated firmware bound = %s", libvirt.Firmware)
	}
	if baremetal.Firmware != BaremetalFirmwareBound || BaremetalFirmwareBound != 74*time.Minute+30*time.Second {
		t.Fatalf("physical firmware bound = %s, constant %s", baremetal.Firmware, BaremetalFirmwareBound)
	}
}
