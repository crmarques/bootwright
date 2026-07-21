package entitlements

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func TestResolveRHELCarriesOwnRHSMAndIBMCarriesNone(t *testing.T) {
	ents := []v1alpha1.Entitlement{
		{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.EntitlementSpec{
				Type: v1alpha1.EntitlementTypeRedHatRHEL,
				RHSM: &v1alpha1.EntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "rhel-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "rhel-key"},
				},
			},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "ibm-ceph"},
			Spec: v1alpha1.EntitlementSpec{
				Type:     v1alpha1.EntitlementTypeIBMStorageCeph,
				Registry: &v1alpha1.EntitlementRegistry{CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"}},
				License:  &v1alpha1.EntitlementLicense{Accept: true},
			},
		},
	}

	rhel, ok := Resolve(ents, secret.Index{}, "rhel", "registry.redhat.io", "/secrets")
	if !ok {
		t.Fatal("Resolve(rhel) not found")
	}
	if rhel.RHSM.OrganizationPath != "/secrets/rhel-org" || rhel.RHSM.ActivationKeyPath != "/secrets/rhel-key" {
		t.Fatalf("rhel rhsm = %#v", rhel.RHSM)
	}

	ibm, ok := Resolve(ents, secret.Index{}, "ibm-ceph", "cp.icr.io/cp", "/secrets")
	if !ok {
		t.Fatal("Resolve(ibm-ceph) not found")
	}
	if ibm.RHSM.OrganizationPath != "" || ibm.RHSM.ActivationKeyPath != "" || ibm.RHSM.Management != "" {
		t.Fatalf("ibm-ceph must carry no inline rhsm; RHEL registration comes from the profile subscription, got %#v", ibm.RHSM)
	}
	if ibm.Registry.CredentialsPath != "/secrets/ibm-registry" || !ibm.License.Accepted {
		t.Fatalf("ibm registry/license = %#v %#v", ibm.Registry, ibm.License)
	}
}

func TestResolveCarriesSatellite(t *testing.T) {
	ents := []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.EntitlementSpec{
			Type: v1alpha1.EntitlementTypeRedHatRHEL,
			RHSM: &v1alpha1.EntitlementRHSM{
				OrganizationRef:  v1alpha1.SecretRef{Name: "rhel-org"},
				ActivationKeyRef: v1alpha1.SecretRef{Name: "rhel-key"},
				Satellite: &v1alpha1.EntitlementRHSMSatellite{
					Hostname:       "satellite.corp.example.com",
					ContentBaseURL: "https://satellite.corp.example.com/pulp/content",
					TrustBundleRef: v1alpha1.SecretRef{Name: "corp-satellite-ca"},
				},
			},
		},
	}}

	rhel, ok := Resolve(ents, secret.Index{}, "rhel", "registry.redhat.io", "/secrets")
	if !ok {
		t.Fatal("Resolve(rhel) not found")
	}
	if sat := rhel.RHSM.Satellite; sat.Hostname != "satellite.corp.example.com" ||
		sat.ContentBaseURL != "https://satellite.corp.example.com/pulp/content" ||
		sat.TrustBundlePath != "/secrets/corp-satellite-ca" {
		t.Fatalf("rhel satellite = %#v", rhel.RHSM.Satellite)
	}

	bare := []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.EntitlementSpec{
			Type: v1alpha1.EntitlementTypeRedHatRHEL,
			RHSM: &v1alpha1.EntitlementRHSM{
				OrganizationRef:  v1alpha1.SecretRef{Name: "rhel-org"},
				ActivationKeyRef: v1alpha1.SecretRef{Name: "rhel-key"},
			},
		},
	}}
	resolved, ok := Resolve(bare, secret.Index{}, "rhel", "registry.redhat.io", "/secrets")
	if !ok {
		t.Fatal("Resolve(bare rhel) not found")
	}
	if resolved.RHSM.Satellite.Hostname != "" || resolved.RHSM.Satellite.TrustBundlePath != "" {
		t.Fatalf("bare rhsm should carry no satellite, got %#v", resolved.RHSM.Satellite)
	}
}

func TestResolveExternalManagementCarriesNoMaterial(t *testing.T) {
	ents := []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.EntitlementSpec{
			Type: v1alpha1.EntitlementTypeRedHatRHEL,
			RHSM: &v1alpha1.EntitlementRHSM{Management: v1alpha1.EntitlementRHSMManagementExternal},
		},
	}}

	rhel, ok := Resolve(ents, secret.Index{}, "rhel", "registry.redhat.io", "/secrets")
	if !ok {
		t.Fatal("Resolve(rhel) not found")
	}
	if rhel.RHSM.Management != v1alpha1.EntitlementRHSMManagementExternal {
		t.Fatalf("rhel rhsm management = %q, want external", rhel.RHSM.Management)
	}
	if rhel.RHSM.OrganizationPath != "" || rhel.RHSM.ActivationKeyPath != "" || rhel.RHSM.Satellite.Hostname != "" {
		t.Fatalf("external rhsm must resolve no material, got %#v", rhel.RHSM)
	}
}

func TestResolveManagedManagementDefault(t *testing.T) {
	ents := []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.EntitlementSpec{
			Type: v1alpha1.EntitlementTypeRedHatRHEL,
			RHSM: &v1alpha1.EntitlementRHSM{
				OrganizationRef:  v1alpha1.SecretRef{Name: "rhel-org"},
				ActivationKeyRef: v1alpha1.SecretRef{Name: "rhel-key"},
			},
		},
	}}
	resolved, ok := Resolve(ents, secret.Index{}, "rhel", "registry.redhat.io", "/secrets")
	if !ok {
		t.Fatal("Resolve(rhel) not found")
	}
	if resolved.RHSM.Management != v1alpha1.EntitlementRHSMManagementManaged {
		t.Fatalf("unset management must resolve managed, got %q", resolved.RHSM.Management)
	}
}
