package v1alpha1

import (
	"slices"
	"testing"
)

func TestEveryDiskActionSitsBesideTheIdentityItDestroys(t *testing.T) {
	hardware, _ := machineSchema().Field("hardware")
	root, _ := hardware.Shape.Field("rootDisk")
	removal, found := root.Shape.Field("onRemoval")
	if !found || removal.Required || removal.Default.Present() || !slices.Equal(removal.Shape.Enums, []string{"retain", "metadataClear", "logicalOverwrite"}) {
		t.Fatalf("rootDisk.onRemoval = %+v, want an optional retain, metadataClear or logicalOverwrite that materializes nothing", removal)
	}
	size, ok := root.Shape.Field("sizeBytes")
	if !ok || size.Required || size.Shape.Minimum != "4096" || size.Shape.Maximum != "70368744177664" {
		t.Fatalf("rootDisk.sizeBytes = %+v, want an optional integer from 4096 to 64 TiB", size)
	}
	hints, _ := root.Shape.Field("hints")
	for _, name := range []string{"deviceName", "model", "vendor", "minSizeGigabytes", "rotational", "hctl"} {
		if _, ok := hints.Shape.Field(name); !ok {
			t.Errorf("rootDisk.hints lacks %s", name)
		}
	}
	disks, _ := hardware.Shape.Field("dataDisks")
	if disks.Shape.NameKey != "serialNumber" || !disks.Shape.Atomic {
		t.Fatalf("dataDisks are keyed by %q, want an atomic set keyed by serialNumber", disks.Shape.NameKey)
	}
	for key, required := range map[string]bool{"serialNumber": true, "sizeBytes": true, "wwn": false, "onInstall": false, "onRemoval": false} {
		field, ok := disks.Shape.Element.Field(key)
		if !ok || field.Required != required || field.Default.Present() {
			t.Fatalf("data disk %s: present %t, required %t, want required %t and no default", key, ok, field.Required, required)
		}
	}
	install, _ := disks.Shape.Element.Field("onInstall")
	if !slices.Equal(install.Shape.Enums, []string{"retain", "metadataClear"}) {
		t.Fatalf("dataDisks[].onInstall = %v", install.Shape.Enums)
	}
	for _, retired := range []string{"eraseOnRemoval", "clearOnInstall", "rootDevice"} {
		if _, found := hardware.Shape.Field(retired); found {
			t.Errorf("hardware still declares %s", retired)
		}
	}
}

func TestTheOperatingSystemIsOneOfThreeLifecycles(t *testing.T) {
	os, _ := machineSchema().Field("os")
	if !os.Required || !slices.Equal(os.Shape.Arms, []string{"provided", "install", "clusterInstalled"}) || os.Shape.AllowEmpty {
		t.Fatalf("os = %+v, want a required choice of provided, install and clusterInstalled", os)
	}
	install, _ := os.Shape.Field("install")
	profile, _ := install.Shape.Field("profileRef")
	if !profile.Required || !slices.Equal(profile.Shape.Reference, []Kind{MachineInstallProfile}) {
		t.Fatalf("os.install.profileRef = %+v", profile)
	}
}

func TestBMCTrustHasOneVerificationSwitch(t *testing.T) {
	hardware, _ := machineSchema().Field("hardware")
	bmc, _ := hardware.Shape.Field("bmc")
	tls, _ := bmc.Shape.Field("tls")
	for _, retired := range []string{"verify"} {
		if _, found := tls.Shape.Field(retired); found {
			t.Errorf("bmc.tls still declares %s", retired)
		}
	}
	if _, found := bmc.Shape.Field("protocol"); found {
		t.Error("bmc still declares protocol")
	}
	provider := machineProviderSchema()
	baremetal, _ := provider.Field("baremetal")
	defaults, _ := baremetal.Shape.Field("defaults")
	inherited, _ := defaults.Shape.Field("bmc")
	defaultTLS, _ := inherited.Shape.Field("tls")
	if len(defaultTLS.Shape.Fields) != 1 || defaultTLS.Shape.Fields[0].Name != "trustBundleRef" {
		t.Fatalf("provider defaults.bmc.tls = %+v, want trustBundleRef alone", defaultTLS.Shape.Fields)
	}
}
