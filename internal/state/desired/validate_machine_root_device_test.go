package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func rootDeviceHintState(hints *v1alpha1.RootDeviceHints, installsOS bool) v1alpha1.State {
	m := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "bastion-0"}}
	m.Spec.OS.Provided = v1alpha1.BoolPtr(!installsOS)
	if installsOS {
		m.Spec.OS.InstallProfileRef = v1alpha1.LocalObjectReference{Name: "rhel"}
	}
	m.Spec.OS.Install.RootDeviceHints = hints
	return v1alpha1.State{Machines: []v1alpha1.Machine{m}}
}

func TestValidateAnacondaRootDeviceHintsRefusesPredicateOnlyHints(t *testing.T) {
	rotational := false
	errs := validateAnacondaRootDeviceHintsAreUsable(rootDeviceHintState(&v1alpha1.RootDeviceHints{
		Model:      "MZ7LH960HAJR",
		Rotational: &rotational,
	}, true))
	if len(errs) != 1 {
		t.Fatalf("a managed-OS machine whose only hints are predicates must refuse, got %v", errs)
	}
	for _, want := range []string{"Machine/bastion-0", "model", "rotational", "clearpart --all", "deviceName", "wwn"} {
		if !strings.Contains(errs[0], want) {
			t.Errorf("the refusal must name %q, got %q", want, errs[0])
		}
	}
}

func TestValidateAnacondaRootDeviceHintsAcceptsSelectableHints(t *testing.T) {
	for _, hints := range []*v1alpha1.RootDeviceHints{
		{DeviceName: "/dev/sda"},
		{WWN: "0x5000c500"},
		{DeviceName: "/dev/disk/by-id/wwn-0x5000c500", Model: "MZ7LH960HAJR"},
	} {
		if errs := validateAnacondaRootDeviceHintsAreUsable(rootDeviceHintState(hints, true)); len(errs) != 0 {
			t.Errorf("hints %+v resolve to a kickstart disk selector and must pass, got %v", hints, errs)
		}
	}
}

func TestValidateAnacondaRootDeviceHintsIgnoresAbsentHintsAndProvidedMachines(t *testing.T) {
	if errs := validateAnacondaRootDeviceHintsAreUsable(rootDeviceHintState(nil, true)); len(errs) != 0 {
		t.Errorf("omitting rootDeviceHints is a deliberate whole-machine autopart, not an error, got %v", errs)
	}
	if errs := validateAnacondaRootDeviceHintsAreUsable(rootDeviceHintState(&v1alpha1.RootDeviceHints{}, true)); len(errs) != 0 {
		t.Errorf("an empty rootDeviceHints block declares nothing to honour, got %v", errs)
	}
	if errs := validateAnacondaRootDeviceHintsAreUsable(rootDeviceHintState(&v1alpha1.RootDeviceHints{Model: "MZ7LH960HAJR"}, false)); len(errs) != 0 {
		t.Errorf("a machine whose OS is provided renders no kickstart, got %v", errs)
	}
}
