package converge

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func operatorIdentityState() v1alpha1.State {
	return v1alpha1.State{Machines: []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "arbiter"},
		Spec: v1alpha1.MachineSpec{
			OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
			Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
				Auth: v1alpha1.MachineSSHAuth{OperatorIdentity: &v1alpha1.MachineSSHOperatorIdentity{}},
			}},
		},
	}}}
}

func declaredLoginState() v1alpha1.State {
	return v1alpha1.State{Machines: []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "node"},
		Spec: v1alpha1.MachineSpec{
			OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false), InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"}},
			Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
				User: v1alpha1.BootwrightSSHUser,
				Auth: v1alpha1.MachineSSHAuth{Provision: &v1alpha1.MachineSSHProvision{KeyRef: v1alpha1.SecretRef{Name: "machine-key"}}},
			}},
		},
	}}}
}

func TestSSHSudoPasswordIsRefusedWhenNoMachineUsesTheOperatorIdentity(t *testing.T) {
	t.Cleanup(func() { SetSSHSudoPassword(""); SetSSHUser("") })
	SetSSHUser("")
	SetSSHSudoPassword("secret")
	err := checkSSHUserScope(declaredLoginState())
	if err == nil {
		t.Fatal("a run with no operatorIdentity machine must refuse --ssh-ask-sudo-password rather than prompt for a password it will never offer")
	}
	if !strings.Contains(err.Error(), "--ssh-ask-sudo-password") {
		t.Fatalf("refusal = %q, want it to name the flag that does nothing", err)
	}
	if strings.Contains(err.Error(), "--ssh-user") {
		t.Fatalf("refusal = %q, want the password refusal, not the identity one; the operator set no --ssh-user", err)
	}
}

func TestSSHSudoPasswordIsAcceptedForAnOperatorIdentityMachine(t *testing.T) {
	t.Cleanup(func() { SetSSHSudoPassword(""); SetSSHUser("") })
	SetSSHSudoPassword("secret")
	if err := checkSSHUserScope(operatorIdentityState()); err != nil {
		t.Fatalf("checkSSHUserScope = %v, want the flag accepted for a machine the operator administers", err)
	}
}

func TestSSHUserRefusalStillWinsWhenBothFlagsAreSet(t *testing.T) {
	t.Cleanup(func() { SetSSHSudoPassword(""); SetSSHUser("") })
	SetSSHUser("carmj")
	SetSSHSudoPassword("secret")
	err := checkSSHUserScope(declaredLoginState())
	if err == nil || !strings.Contains(err.Error(), "--ssh-user") {
		t.Fatalf("refusal = %v, want the identity refusal named first; the password only answers sudo for the account --ssh-user names", err)
	}
}

func TestNoPasswordAndNoUserLeavesTheRunAlone(t *testing.T) {
	t.Cleanup(func() { SetSSHSudoPassword(""); SetSSHUser("") })
	SetSSHUser("")
	SetSSHSudoPassword("")
	if err := checkSSHUserScope(declaredLoginState()); err != nil {
		t.Fatalf("checkSSHUserScope = %v, want no refusal when neither flag is set", err)
	}
}

func TestSSHUserForProvisionedMakesTheOverrideApplyEverywhere(t *testing.T) {
	t.Cleanup(func() { SetSSHSudoPassword(""); SetSSHUser(""); SetSSHUserForProvisioned(false) })
	SetSSHUser("carmj")
	SetSSHUserForProvisioned(true)
	if err := checkSSHUserScope(declaredLoginState()); err != nil {
		t.Fatalf("checkSSHUserScope = %v; --ssh-user-for-provisioned widens the override to every machine, so a run without an operatorIdentity machine is exactly what it is for", err)
	}
}
