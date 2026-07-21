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
		},
		{
			name: "ibm-license-not-accepted",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "ibm-ceph"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeIBMStorageCeph,
					Registry: &v1alpha1.EntitlementRegistry{
						CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
					},
				},
			}},
			want: "license.accept must be true",
		},
		{
			name: "ibm-inline-rhsm-rejected",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "ibm-ceph"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeIBMStorageCeph,
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
			name: "rhsm-management-external-valid",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "rhel"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeRedHatRHEL,
					RHSM: &v1alpha1.EntitlementRHSM{Management: v1alpha1.EntitlementRHSMManagementExternal},
				},
			}},
		},
		{
			name: "rhsm-management-external-redhat-ceph-valid",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "rhcs"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeRedHatCeph,
					RHSM: &v1alpha1.EntitlementRHSM{Management: v1alpha1.EntitlementRHSMManagementExternal},
					Registry: &v1alpha1.EntitlementRegistry{
						CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
					},
				},
			}},
		},
		{
			name: "rhsm-management-invalid",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "rhel"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeRedHatRHEL,
					RHSM: &v1alpha1.EntitlementRHSM{Management: "operator"},
				},
			}},
			want: `rhsm.management "operator" must be one of {managed, external}`,
		},
		{
			name: "rhsm-management-external-rejects-refs",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "rhel"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeRedHatRHEL,
					RHSM: &v1alpha1.EntitlementRHSM{
						Management:      v1alpha1.EntitlementRHSMManagementExternal,
						OrganizationRef: v1alpha1.SecretRef{Name: "rhel-org"},
					},
				},
			}},
			want: "organizationRef must be unset when management is external",
		},
		{
			name: "rhsm-management-external-rejects-satellite",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "rhel"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeRedHatRHEL,
					RHSM: &v1alpha1.EntitlementRHSM{
						Management: v1alpha1.EntitlementRHSMManagementExternal,
						Satellite:  &v1alpha1.EntitlementRHSMSatellite{Hostname: "satellite.corp.example.com"},
					},
				},
			}},
			want: "satellite must be unset when management is external",
		},
		{
			name: "rhsm-management-external-rejects-insights",
			entitlements: []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "rhel"},
				Spec: v1alpha1.EntitlementSpec{
					Type: v1alpha1.EntitlementTypeRedHatRHEL,
					RHSM: &v1alpha1.EntitlementRHSM{
						Management:        v1alpha1.EntitlementRHSMManagementExternal,
						ConnectToInsights: true,
					},
				},
			}},
			want: "connectToInsights must be unset when management is external",
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

func TestMachineInstallProfileFromSubscriptionRequiresRHELEntitlement(t *testing.T) {
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
							FromSubscription: &v1alpha1.MachineInstallPackageFromSubscription{
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

func TestMachineInstallProfileFromSubscriptionRejectsExternalManagement(t *testing.T) {
	state := v1alpha1.State{
		Entitlements: []v1alpha1.Entitlement{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.EntitlementSpec{
				Type: v1alpha1.EntitlementTypeRedHatRHEL,
				RHSM: &v1alpha1.EntitlementRHSM{Management: v1alpha1.EntitlementRHSMManagementExternal},
			},
		}},
		MachineInstallProfiles: []v1alpha1.MachineInstallProfile{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.MachineInstallProfileSpec{
				Installer: v1alpha1.MachineInstallProfileInstaller{
					Anaconda: &v1alpha1.MachineInstallAnaconda{
						PackageSource: &v1alpha1.MachineInstallPackageSource{
							FromSubscription: &v1alpha1.MachineInstallPackageFromSubscription{
								EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
							},
						},
					},
				},
			},
		}},
	}
	errs := validateMachineInstallProfileEntitlements(state)
	if got := strings.Join(errs, "; "); !strings.Contains(got, "cannot be delegated to a provisioning playbook") {
		t.Fatalf("validateMachineInstallProfileEntitlements errors = %q", got)
	}
}
