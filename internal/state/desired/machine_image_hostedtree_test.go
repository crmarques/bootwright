package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func hostedTreeImage() v1alpha1.MachineImage {
	return v1alpha1.MachineImage{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			BootMedia: "local-media:rhel-9.7-x86_64-boot.iso",
			PackageSource: &v1alpha1.MachinePackageSource{
				HostedTree: &v1alpha1.MachinePackageHostedTree{
					FromMedia: "local-media:rhel-9.7-x86_64-dvd.iso",
				},
			},
		},
	}
}

func TestMachineImageHostedTreeAcceptsFromMedia(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{hostedTreeImage()}})
	if len(errs) != 0 {
		t.Fatalf("validateMachineImages errors = %v", errs)
	}
}

func TestMachineImageHostedTreeRequiresFromMedia(t *testing.T) {
	image := hostedTreeImage()
	image.Spec.PackageSource.HostedTree.FromMedia = ""
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{image}})
	if !containsSubstring(errs, "packageSource.hostedTree.fromMedia is required") {
		t.Fatalf("errors = %v, want fromMedia requirement", errs)
	}
}

func TestMachineImageHostedTreeRejectsFromMediaEqualToBootMedia(t *testing.T) {
	image := hostedTreeImage()
	image.Spec.PackageSource.HostedTree.FromMedia = image.Spec.BootMedia
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{image}})
	if !containsSubstring(errs, "must reference the DVD, not the boot ISO") {
		t.Fatalf("errors = %v, want fromMedia!=bootMedia requirement", errs)
	}
}

func TestMachineImageHostedTreeRejectsURLFromMedia(t *testing.T) {
	image := hostedTreeImage()
	image.Spec.PackageSource.HostedTree.FromMedia = "https://mirror.example.test/rhel/9/rhel-9.7-dvd.iso"
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{image}})
	if !containsSubstring(errs, "fromMedia must reference local media") {
		t.Fatalf("errors = %v, want local-media requirement", errs)
	}
}
