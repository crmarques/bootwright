package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestEntitlementValidation(t *testing.T) {
	rhel := func() v1alpha1.Entitlement {
		return v1alpha1.Entitlement{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.EntitlementSpec{
				Type: v1alpha1.EntitlementTypeRedHatRHEL,
				RHSM: &v1alpha1.EntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "rhel-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "rhel-activation-key"},
				},
			},
		}
	}
	rhelWithSatellite := func(sat *v1alpha1.EntitlementRHSMSatellite) []v1alpha1.Entitlement {
		e := rhel()
		e.Spec.RHSM.Satellite = sat
		return []v1alpha1.Entitlement{e}
	}
	cases := []struct {
		name         string
		entitlements []v1alpha1.Entitlement
		want         string
	}{
		{
			name: "redhat-ceph-valid",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "rhcs"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeRedHatCeph,
					RHSM: &v1alpha1.EntitlementRHSM{
						OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
						ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
					},
					Registry: &v1alpha1.EntitlementRegistry{
						CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
					},
				},
			}},
		},
		{
			name:         "rhel-valid",
			entitlements: []v1alpha1.Entitlement{rhel()},
		},
		{
			name: "type-required",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "bad"},
			}},
			want: "spec.type is required",
		},
		// The former invalid-provider / invalid-product / invalid-provider-product
		// cases collapsed into a single spec.type enum check: the provider/product
		// axis no longer exists, so an unknown discriminator is the only shape.
		{
			name: "invalid-type",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "bad"},
				Spec:     v1alpha1.EntitlementSpec{Type: "vendor"},
			}},
			want: `spec.type "vendor" must be one of {redhat-rhel, redhat-ceph, ibm-storage-ceph}`,
		},
		{
			name: "redhat-ceph-missing-registry",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "rhcs"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeRedHatCeph,
					RHSM: &v1alpha1.EntitlementRHSM{
						OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
						ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
					},
				},
			}},
			want: "registry.credentialsRef is required",
		},
		{
			name: "ibm-valid",
			entitlements: []v1alpha1.Entitlement{rhel(), {
				Metadata: v1alpha1.Metadata{Name: "ibm-ceph"},
				Spec: v1alpha1.EntitlementSpec{
					Type:               v1alpha1.EntitlementTypeIBMStorageCeph,
					RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
					Registry: &v1alpha1.EntitlementRegistry{
						CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
					},
					License: &v1alpha1.EntitlementLicense{Accept: true},
				},
			}},
		},
		{
			name: "ibm-license-not-accepted",
			entitlements: []v1alpha1.Entitlement{rhel(), {
				Metadata: v1alpha1.Metadata{Name: "ibm-ceph"},
				Spec: v1alpha1.EntitlementSpec{
					Type:               v1alpha1.EntitlementTypeIBMStorageCeph,
					RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
					Registry: &v1alpha1.EntitlementRegistry{
						CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
					},
				},
			}},
			want: "license.accept must be true",
		},
		{
			name: "ibm-inline-rhsm-rejected",
			entitlements: []v1alpha1.Entitlement{rhel(), {
				Metadata: v1alpha1.Metadata{Name: "ibm-ceph"},
				Spec: v1alpha1.EntitlementSpec{
					Type:               v1alpha1.EntitlementTypeIBMStorageCeph,
					RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
					RHSM: &v1alpha1.EntitlementRHSM{
						OrganizationRef:  v1alpha1.SecretRef{Name: "ibm-org"},
						ActivationKeyRef: v1alpha1.SecretRef{Name: "ibm-key"},
					},
					Registry: &v1alpha1.EntitlementRegistry{
						CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
					},
					License: &v1alpha1.EntitlementLicense{Accept: true},
				},
			}},
			want: "rhsm is not allowed for ibm-storage-ceph",
		},
		{
			name: "ibm-missing-rhel-ref",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "ibm-ceph"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeIBMStorageCeph,
					Registry: &v1alpha1.EntitlementRegistry{
						CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
					},
					License: &v1alpha1.EntitlementLicense{Accept: true},
				},
			}},
			want: "rhelEntitlementRef is required for ibm-storage-ceph",
		},
		{
			name: "ibm-rhel-ref-unknown",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "ibm-ceph"},
				Spec: v1alpha1.EntitlementSpec{
					Type:               v1alpha1.EntitlementTypeIBMStorageCeph,
					RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "absent"},
					Registry: &v1alpha1.EntitlementRegistry{
						CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
					},
					License: &v1alpha1.EntitlementLicense{Accept: true},
				},
			}},
			want: `does not match any Entitlement`,
		},
		{
			// Was ibm-rhel-ref-wrong-product referencing a community/ceph item;
			// community no longer exists, so the wrong-type target is now a
			// redhat-ceph entitlement (still not the redhat-rhel type IBM needs).
			name: "ibm-rhel-ref-wrong-type",
			entitlements: []v1alpha1.Entitlement{
				{
					Metadata: v1alpha1.Metadata{Name: "rhcs"},
					Spec: v1alpha1.EntitlementSpec{
						Type: v1alpha1.EntitlementTypeRedHatCeph,
						RHSM: &v1alpha1.EntitlementRHSM{
							OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
							ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
						},
						Registry: &v1alpha1.EntitlementRegistry{
							CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
						},
					},
				},
				{
					Metadata: v1alpha1.Metadata{Name: "ibm-ceph"},
					Spec: v1alpha1.EntitlementSpec{
						Type:               v1alpha1.EntitlementTypeIBMStorageCeph,
						RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
						Registry: &v1alpha1.EntitlementRegistry{
							CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
						},
						License: &v1alpha1.EntitlementLicense{Accept: true},
					},
				},
			},
			want: `resolves to type "redhat-ceph", want "redhat-rhel"`,
		},
		{
			name: "non-ibm-rhel-ref-rejected",
			entitlements: []v1alpha1.Entitlement{rhel(), {
				Metadata: v1alpha1.Metadata{Name: "rhcs"},
				Spec: v1alpha1.EntitlementSpec{
					Type:               v1alpha1.EntitlementTypeRedHatCeph,
					RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
					RHSM: &v1alpha1.EntitlementRHSM{
						OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
						ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
					},
					Registry: &v1alpha1.EntitlementRegistry{
						CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
					},
				},
			}},
			want: "rhelEntitlementRef is only valid for the ibm-storage-ceph type",
		},
		{
			name: "registry-url-credentials",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "rhcs"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeRedHatCeph,
					RHSM: &v1alpha1.EntitlementRHSM{
						OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
						ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
					},
					Registry: &v1alpha1.EntitlementRegistry{
						URL:            "user:pass@registry.example.test",
						CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
					},
				},
			}},
			want: "registry.url must not embed credentials; use credentialsRef",
		},
		{
			name: "satellite-valid",
			entitlements: rhelWithSatellite(&v1alpha1.EntitlementRHSMSatellite{
				Hostname:       "satellite.corp.example.com",
				TrustBundleRef: v1alpha1.SecretRef{Name: "corp-satellite-ca"},
			}),
		},
		{
			name: "satellite-missing-hostname",
			entitlements: rhelWithSatellite(&v1alpha1.EntitlementRHSMSatellite{
				TrustBundleRef: v1alpha1.SecretRef{Name: "corp-satellite-ca"},
			}),
			want: "satellite.hostname is required when satellite is set",
		},
		{
			name: "satellite-hostname-is-url",
			entitlements: rhelWithSatellite(&v1alpha1.EntitlementRHSMSatellite{
				Hostname: "https://satellite.corp.example.com",
			}),
			want: "satellite.hostname must be a bare host",
		},
		{
			name: "satellite-bad-content-url",
			entitlements: rhelWithSatellite(&v1alpha1.EntitlementRHSMSatellite{
				Hostname:       "satellite.corp.example.com",
				ContentBaseURL: "ftp://satellite.corp.example.com/pulp",
			}),
			want: "satellite.contentBaseURL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := v1alpha1.State{Entitlements: tc.entitlements}
			got := strings.Join(validateEntitlements(state), "; ")
			if tc.want == "" {
				if got != "" {
					t.Fatalf("validateEntitlements errors = %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateEntitlements errors = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMachineInstallProfileRedHatCDNRequiresRHELEntitlement(t *testing.T) {
	state := v1alpha1.State{
		Entitlements: []v1alpha1.Entitlement{{
			Metadata: v1alpha1.Metadata{Name: "rhcs"},
			Spec:     v1alpha1.EntitlementSpec{Type: v1alpha1.EntitlementTypeRedHatCeph},
		}},
		MachineInstallProfiles: []v1alpha1.MachineInstallProfile{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.MachineInstallProfileSpec{
				Installer: v1alpha1.MachineInstallProfileInstaller{
					Anaconda: &v1alpha1.MachineInstallAnaconda{
						PackageSource: &v1alpha1.MachineInstallPackageSource{
							RedhatCDN: &v1alpha1.MachineInstallPackageRedhatCDN{
								EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
							},
						},
					},
				},
			},
		}},
	}
	errs := validateMachineInstallProfileEntitlements(state)
	if got := strings.Join(errs, "; "); !strings.Contains(got, `resolves to type "redhat-ceph", want "redhat-rhel"`) {
		t.Fatalf("validateMachineInstallProfileEntitlements errors = %q", got)
	}
}
