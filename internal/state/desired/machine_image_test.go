package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestMachineImageURLAcceptsLocalMediaReference(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type: v1alpha1.MachineImageTypeISO,
			URL:  "local-media:rhel.iso",
		},
	}}})
	if len(errs) != 0 {
		t.Fatalf("validateMachineImages errors = %v", errs)
	}
}

func TestMachineImageURLRejectsInvalidLocalMediaReference(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type: v1alpha1.MachineImageTypeISO,
			URL:  "local-media:../rhel.iso",
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted invalid media reference")
	}
	if !strings.Contains(errs[0], "must be a filename") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImageURLRejectsRetiredMediaReference(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type: v1alpha1.MachineImageTypeISO,
			URL:  "media:rhel.iso",
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted retired media reference")
	}
	if !strings.Contains(errs[0], "must use local-media:") {
		t.Fatalf("error = %q", errs[0])
	}
}
