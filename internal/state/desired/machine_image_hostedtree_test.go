package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func hostedTreeImage() v1alpha1.MachineImage {
	return v1alpha1.MachineImage{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type:      v1alpha1.MachineImageTypeISO,
			MediaType: v1alpha1.MachineImageMediaTypeBoot,
			URL:       "local-media:rhel-9.7-x86_64-boot.iso",
			InstallSource: v1alpha1.MachineImageInstallSource{
				Type:      v1alpha1.MachineImageInstallSourceTypeHostedTree,
				FromMedia: "local-media:rhel-9.7-x86_64-dvd.iso",
			},
		},
	}
}

func TestNormalizeDerivesHostedTreeFromFromMedia(t *testing.T) {
	state := v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "tree"},
		Spec: v1alpha1.MachineImageSpec{
			Type:      v1alpha1.MachineImageTypeISO,
			MediaType: v1alpha1.MachineImageMediaTypeBoot,
			URL:       "local-media:rhel-9.7-x86_64-boot.iso",
			InstallSource: v1alpha1.MachineImageInstallSource{
				FromMedia: "local-media:rhel-9.7-x86_64-dvd.iso",
			},
		},
	}}}

	Normalize(&state)

	if got := state.MachineImages[0].Spec.InstallSource.Type; got != v1alpha1.MachineImageInstallSourceTypeHostedTree {
		t.Fatalf("fromMedia-only installSource.type = %q, want %q", got, v1alpha1.MachineImageInstallSourceTypeHostedTree)
	}
}

func TestMachineImageHostedTreeAcceptsFromMedia(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{hostedTreeImage()}})
	if len(errs) != 0 {
		t.Fatalf("validateMachineImages errors = %v", errs)
	}
}

func TestMachineImageHostedTreeRequiresBootMediaType(t *testing.T) {
	image := hostedTreeImage()
	image.Spec.MediaType = v1alpha1.MachineImageMediaTypeDVD
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{image}})
	if !containsSubstring(errs, "hostedTree requires mediaType: boot") {
		t.Fatalf("errors = %v, want mediaType: boot requirement", errs)
	}
}

func TestMachineImageHostedTreeRequiresFromMedia(t *testing.T) {
	image := hostedTreeImage()
	image.Spec.InstallSource.FromMedia = ""
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{image}})
	if !containsSubstring(errs, "fromMedia is required when installSource.type is hostedTree") {
		t.Fatalf("errors = %v, want fromMedia requirement", errs)
	}
}

func TestMachineImageHostedTreeRejectsFromMediaEqualToURL(t *testing.T) {
	image := hostedTreeImage()
	image.Spec.InstallSource.FromMedia = image.Spec.URL
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{image}})
	if !containsSubstring(errs, "must reference the DVD, not the boot ISO") {
		t.Fatalf("errors = %v, want fromMedia!=url requirement", errs)
	}
}

func TestMachineImageHostedTreeRejectsAuthoredURLAndRepositories(t *testing.T) {
	image := hostedTreeImage()
	image.Spec.InstallSource.URL = "https://mirror.example.test/rhel/9/BaseOS/x86_64/os/"
	image.Spec.InstallSource.Repositories = []v1alpha1.MachineInstallRepository{
		{ID: "appstream", BaseURL: "https://mirror.example.test/rhel/9/AppStream/x86_64/os/"},
	}
	image.Spec.InstallSource.EntitlementRef = v1alpha1.LocalObjectReference{Name: "rhel"}
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{image}})
	for _, want := range []string{
		"url must be empty when installSource.type is hostedTree",
		"repositories must be empty when installSource.type is hostedTree",
		"entitlementRef must be empty when installSource.type is hostedTree",
	} {
		if !containsSubstring(errs, want) {
			t.Fatalf("errors = %v, want %q", errs, want)
		}
	}
}

func TestMachineImageHostedTreeRejectsURLFromMedia(t *testing.T) {
	image := hostedTreeImage()
	image.Spec.InstallSource.FromMedia = "https://mirror.example.test/rhel/9/rhel-9.7-dvd.iso"
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{image}})
	if !containsSubstring(errs, "fromMedia must reference local media") {
		t.Fatalf("errors = %v, want local-media requirement", errs)
	}
}

func TestMachineImageRejectsFromMediaOnURLType(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type:      v1alpha1.MachineImageTypeISO,
			MediaType: v1alpha1.MachineImageMediaTypeBoot,
			URL:       "local-media:rhel-9.7-x86_64-boot.iso",
			InstallSource: v1alpha1.MachineImageInstallSource{
				Type:      v1alpha1.MachineImageInstallSourceTypeURL,
				URL:       "https://mirror.example.test/rhel/9/BaseOS/x86_64/os/",
				FromMedia: "local-media:rhel-9.7-x86_64-dvd.iso",
			},
		},
	}}})
	if !containsSubstring(errs, "fromMedia is only valid when installSource.type is hostedTree") {
		t.Fatalf("errors = %v, want fromMedia-only-hostedTree guard", errs)
	}
}
