package entitlements

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestResolveFollowsRHELEntitlementRef verifies that an ibm/ibm-storage-ceph
// entitlement, which carries no inline rhsm arm, resolves its RHSM material
// from the referenced redhat/rhel entitlement while keeping its own registry
// and license arms.
func TestResolveFollowsRHELEntitlementRef(t *testing.T) {
	env := &v1alpha1.Environment{Spec: v1alpha1.EnvironmentSpec{Entitlements: []v1alpha1.EnvironmentEntitlement{
		{
			Name:     "rhel",
			Provider: v1alpha1.EntitlementProviderRedHat,
			Product:  v1alpha1.EntitlementProductRHEL,
			RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
				OrganizationRef:  v1alpha1.SecretRef{Name: "ibm-org"},
				ActivationKeyRef: v1alpha1.SecretRef{Name: "ibm-key"},
			},
		},
		{
			Name:               "ibm-ceph",
			Provider:           v1alpha1.EntitlementProviderIBM,
			Product:            v1alpha1.EntitlementProductIBMStorageCeph,
			RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
			Registry: &v1alpha1.EnvironmentEntitlementRegistry{
				CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
			},
			License: &v1alpha1.EnvironmentEntitlementLicense{Accept: true},
		},
	}}}

	resolved, ok := Resolve(env, "ibm-ceph", "cp.icr.io/cp", "/secrets")
	if !ok {
		t.Fatal("Resolve(ibm-ceph) not found")
	}
	if resolved.RHSM.OrganizationPath != "/secrets/ibm-org" || resolved.RHSM.ActivationKeyPath != "/secrets/ibm-key" {
		t.Fatalf("rhsm sourced via rhelEntitlementRef = %#v", resolved.RHSM)
	}
	if resolved.Registry.CredentialsPath != "/secrets/ibm-registry" {
		t.Fatalf("registry credentials = %q", resolved.Registry.CredentialsPath)
	}
	if !resolved.License.Accepted {
		t.Fatalf("license accepted = %v", resolved.License.Accepted)
	}

	// The referenced redhat/rhel entitlement still resolves its rhsm inline.
	rhel, ok := Resolve(env, "rhel", "registry.redhat.io", "/secrets")
	if !ok {
		t.Fatal("Resolve(rhel) not found")
	}
	if rhel.RHSM.OrganizationPath != "/secrets/ibm-org" {
		t.Fatalf("rhel inline rhsm = %#v", rhel.RHSM)
	}
}
