package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func rhelSubscriptionState(sub *v1alpha1.MachineOSSubscription, source *v1alpha1.MachineInstallPackageSource, entType string) v1alpha1.State {
	state := machineInstallPackageSourceState(source)
	state.MachineInstallProfiles[0].Spec.Subscription = sub
	if entType != "" {
		state.Entitlements = []v1alpha1.Entitlement{{
			Metadata: v1alpha1.Metadata{Name: "rhel-satellite"},
			Spec: v1alpha1.EntitlementSpec{
				Type: entType,
				RHSM: &v1alpha1.EntitlementRHSM{Management: v1alpha1.EntitlementRHSMManagementManaged},
			},
		}}
	}
	return state
}

func TestMachineInstallSubscriptionAcceptsManagedRHEL(t *testing.T) {
	state := rhelSubscriptionState(
		&v1alpha1.MachineOSSubscription{EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel-satellite"}},
		&v1alpha1.MachineInstallPackageSource{Mirror: &v1alpha1.MachineInstallPackageMirror{BaseURL: "http://mirror.example.com/rhel9"}},
		v1alpha1.EntitlementTypeRedHatRHEL,
	)
	for _, e := range validateMachineInstallProfiles(state) {
		if strings.Contains(e, ".subscription") {
			t.Fatalf("unexpected subscription error: %s", e)
		}
	}
}

func TestMachineInstallSubscriptionRejectsUnknownEntitlement(t *testing.T) {
	state := rhelSubscriptionState(
		&v1alpha1.MachineOSSubscription{EntitlementRef: v1alpha1.LocalObjectReference{Name: "missing"}},
		&v1alpha1.MachineInstallPackageSource{Mirror: &v1alpha1.MachineInstallPackageMirror{BaseURL: "http://mirror.example.com/rhel9"}},
		v1alpha1.EntitlementTypeRedHatRHEL,
	)
	if !containsSubstring(validateMachineInstallProfiles(state), "subscription.entitlementRef \"missing\" does not match any Entitlement") {
		t.Fatal("expected unknown subscription entitlement to be rejected")
	}
}

func TestMachineInstallSubscriptionRejectsNonRHELType(t *testing.T) {
	state := rhelSubscriptionState(
		&v1alpha1.MachineOSSubscription{EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel-satellite"}},
		&v1alpha1.MachineInstallPackageSource{Mirror: &v1alpha1.MachineInstallPackageMirror{BaseURL: "http://mirror.example.com/rhel9"}},
		v1alpha1.EntitlementTypeRedHatCeph,
	)
	if !containsSubstring(validateMachineInstallProfiles(state), "want \""+v1alpha1.EntitlementTypeRedHatRHEL+"\"") {
		t.Fatal("expected non-redhat-rhel subscription entitlement to be rejected")
	}
}

func TestMachineInstallSubscriptionRejectsRedhatCDNCombination(t *testing.T) {
	state := rhelSubscriptionState(
		&v1alpha1.MachineOSSubscription{EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel-satellite"}},
		&v1alpha1.MachineInstallPackageSource{RedhatCDN: &v1alpha1.MachineInstallPackageRedhatCDN{EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel-satellite"}}},
		v1alpha1.EntitlementTypeRedHatRHEL,
	)
	if !containsSubstring(validateMachineInstallProfiles(state), "subscription cannot be combined with installer.anaconda.packageSource.redhatCDN") {
		t.Fatal("expected subscription+redhatCDN combination to be rejected")
	}
}
