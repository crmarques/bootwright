package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestValidateMachineAccessProvidedOptionalSSH(t *testing.T) {
	provided := true
	notProvided := false
	prefix := "Machine/bastion spec.access"

	localBastion := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "bastion"},
		Spec:     v1alpha1.MachineSpec{OS: v1alpha1.MachineOSSpec{Provided: &provided}},
	}
	if errs := validateMachineAccess(prefix, localBastion); len(errs) != 0 {
		t.Fatalf("provided-OS machine without ssh should validate: %v", errs)
	}

	managed := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "node"},
		Spec: v1alpha1.MachineSpec{OS: v1alpha1.MachineOSSpec{
			Provided:          &notProvided,
			InstallProfileRef: v1alpha1.LocalObjectReference{Name: "profile"},
		}},
	}
	if !containsSubstring(validateMachineAccess(prefix, managed), "ssh is required when os.installProfileRef is set") {
		t.Fatalf("managed-OS node without ssh should require ssh")
	}

	badSSH := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "bastion"},
		Spec: v1alpha1.MachineSpec{
			OS: v1alpha1.MachineOSSpec{Provided: &provided},
			Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
				AddressRef: v1alpha1.LocalObjectReference{Name: "missing"},
				Auth:       v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "k"}},
			}},
		},
	}
	if !containsSubstring(validateMachineAccess(prefix, badSSH), `addressRef "missing" does not resolve`) {
		t.Fatalf("present ssh block should still be validated")
	}

	ready := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "node"},
		Spec:     v1alpha1.MachineSpec{OS: v1alpha1.MachineOSSpec{Provided: &notProvided}},
	}
	if errs := validateMachineAccess(prefix, ready); len(errs) != 0 {
		t.Fatalf("ready node without ssh should validate: %v", errs)
	}
}
