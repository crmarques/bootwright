package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func hostedTreeSource() *v1alpha1.MachineInstallPackageSource {
	return &v1alpha1.MachineInstallPackageSource{
		HostedTree: &v1alpha1.MachineInstallPackageHostedTree{
			FromMedia: "local-media:rhel-9.7-x86_64-dvd.iso",
		},
	}
}

func TestMachineInstallHostedTreeAcceptsFromMedia(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(hostedTreeSource()))
	if len(errs) != 0 {
		t.Fatalf("validateMachineInstallProfiles errors = %v", errs)
	}
}

func TestMachineInstallHostedTreeRequiresFromMedia(t *testing.T) {
	source := hostedTreeSource()
	source.HostedTree.FromMedia = ""
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(source))
	if !containsSubstring(errs, "packageSource.hostedTree.fromMedia is required") {
		t.Fatalf("errors = %v, want fromMedia requirement", errs)
	}
}

func TestMachineInstallHostedTreeRejectsFromMediaEqualToBootMedia(t *testing.T) {
	source := hostedTreeSource()
	state := machineInstallPackageSourceState(source)
	source.HostedTree.FromMedia = state.MachineImages[0].Spec.BootMedia
	errs := validateMachineInstallProfiles(state)
	if !containsSubstring(errs, "must reference the DVD, not the boot ISO") {
		t.Fatalf("errors = %v, want fromMedia!=bootMedia requirement", errs)
	}
}

func TestMachineInstallHostedTreeRejectsURLFromMedia(t *testing.T) {
	source := hostedTreeSource()
	source.HostedTree.FromMedia = "https://mirror.example.test/rhel/9/rhel-9.7-dvd.iso"
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(source))
	if !containsSubstring(errs, "fromMedia must reference local media") {
		t.Fatalf("errors = %v, want local-media requirement", errs)
	}
}
