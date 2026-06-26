package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestEnvironmentEntitlementValidation(t *testing.T) {
	rhel := func() v1alpha1.EnvironmentEntitlement {
		return v1alpha1.EnvironmentEntitlement{
			Name:     "rhel",
			Provider: v1alpha1.EntitlementProviderRedHat,
			Product:  v1alpha1.EntitlementProductRHEL,
			RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
				OrganizationRef:  v1alpha1.SecretRef{Name: "rhel-org"},
				ActivationKeyRef: v1alpha1.SecretRef{Name: "rhel-activation-key"},
			},
		}
	}
	rhelWithSatellite := func(sat *v1alpha1.EnvironmentEntitlementRHSMSatellite) []v1alpha1.EnvironmentEntitlement {
		e := rhel()
		e.RHSM.Satellite = sat
		return []v1alpha1.EnvironmentEntitlement{e}
	}
	cases := []struct {
		name         string
		entitlements []v1alpha1.EnvironmentEntitlement
		want         string
	}{
		{
			name: "redhat-ceph-valid",
			entitlements: []v1alpha1.EnvironmentEntitlement{{
				Name:     "rhcs",
				Provider: v1alpha1.EntitlementProviderRedHat,
				Product:  v1alpha1.EntitlementProductCeph,
				RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
				},
				Registry: &v1alpha1.EnvironmentEntitlementRegistry{
					CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
				},
			}},
		},
		{
			name:         "rhel-valid",
			entitlements: []v1alpha1.EnvironmentEntitlement{rhel()},
		},
		{
			name: "invalid-provider",
			entitlements: []v1alpha1.EnvironmentEntitlement{{
				Name:     "bad",
				Provider: "vendor",
				Product:  v1alpha1.EntitlementProductCeph,
			}},
			want: `provider "vendor" must be one of`,
		},
		{
			name: "invalid-product",
			entitlements: []v1alpha1.EnvironmentEntitlement{{
				Name:     "bad",
				Provider: v1alpha1.EntitlementProviderRedHat,
				Product:  "storage",
			}},
			want: `product "storage" must be one of`,
		},
		{
			name: "invalid-provider-product",
			entitlements: []v1alpha1.EnvironmentEntitlement{{
				Name:     "bad",
				Provider: v1alpha1.EntitlementProviderIBM,
				Product:  v1alpha1.EntitlementProductRHEL,
			}},
			want: "provider/product ibm/rhel is not supported",
		},
		{
			name: "redhat-ceph-missing-registry",
			entitlements: []v1alpha1.EnvironmentEntitlement{{
				Name:     "rhcs",
				Provider: v1alpha1.EntitlementProviderRedHat,
				Product:  v1alpha1.EntitlementProductCeph,
				RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
				},
			}},
			want: "registry.credentialsRef is required",
		},
		{
			name: "ibm-valid",
			entitlements: []v1alpha1.EnvironmentEntitlement{rhel(), {
				Name:               "ibm-ceph",
				Provider:           v1alpha1.EntitlementProviderIBM,
				Product:            v1alpha1.EntitlementProductIBMStorageCeph,
				RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
				Registry: &v1alpha1.EnvironmentEntitlementRegistry{
					CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
				},
				License: &v1alpha1.EnvironmentEntitlementLicense{Accept: true},
			}},
		},
		{
			name: "ibm-license-not-accepted",
			entitlements: []v1alpha1.EnvironmentEntitlement{rhel(), {
				Name:               "ibm-ceph",
				Provider:           v1alpha1.EntitlementProviderIBM,
				Product:            v1alpha1.EntitlementProductIBMStorageCeph,
				RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
				Registry: &v1alpha1.EnvironmentEntitlementRegistry{
					CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
				},
			}},
			want: "license.accept must be true",
		},
		{
			name: "ibm-inline-rhsm-rejected",
			entitlements: []v1alpha1.EnvironmentEntitlement{rhel(), {
				Name:               "ibm-ceph",
				Provider:           v1alpha1.EntitlementProviderIBM,
				Product:            v1alpha1.EntitlementProductIBMStorageCeph,
				RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
				RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "ibm-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "ibm-key"},
				},
				Registry: &v1alpha1.EnvironmentEntitlementRegistry{
					CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
				},
				License: &v1alpha1.EnvironmentEntitlementLicense{Accept: true},
			}},
			want: "rhsm is not allowed for IBM Storage Ceph",
		},
		{
			name: "ibm-missing-rhel-ref",
			entitlements: []v1alpha1.EnvironmentEntitlement{{
				Name:     "ibm-ceph",
				Provider: v1alpha1.EntitlementProviderIBM,
				Product:  v1alpha1.EntitlementProductIBMStorageCeph,
				Registry: &v1alpha1.EnvironmentEntitlementRegistry{
					CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
				},
				License: &v1alpha1.EnvironmentEntitlementLicense{Accept: true},
			}},
			want: "rhelEntitlementRef is required for IBM Storage Ceph",
		},
		{
			name: "ibm-rhel-ref-unknown",
			entitlements: []v1alpha1.EnvironmentEntitlement{{
				Name:               "ibm-ceph",
				Provider:           v1alpha1.EntitlementProviderIBM,
				Product:            v1alpha1.EntitlementProductIBMStorageCeph,
				RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "absent"},
				Registry: &v1alpha1.EnvironmentEntitlementRegistry{
					CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
				},
				License: &v1alpha1.EnvironmentEntitlementLicense{Accept: true},
			}},
			want: "does not match any Environment.spec.entitlements[].name",
		},
		{
			name: "ibm-rhel-ref-wrong-product",
			entitlements: []v1alpha1.EnvironmentEntitlement{
				{Name: "comm", Provider: v1alpha1.EntitlementProviderCommunity, Product: v1alpha1.EntitlementProductCeph},
				{
					Name:               "ibm-ceph",
					Provider:           v1alpha1.EntitlementProviderIBM,
					Product:            v1alpha1.EntitlementProductIBMStorageCeph,
					RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "comm"},
					Registry: &v1alpha1.EnvironmentEntitlementRegistry{
						CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
					},
					License: &v1alpha1.EnvironmentEntitlementLicense{Accept: true},
				},
			},
			want: "resolves to community/ceph, want redhat/rhel",
		},
		{
			name: "non-ibm-rhel-ref-rejected",
			entitlements: []v1alpha1.EnvironmentEntitlement{rhel(), {
				Name:               "rhcs",
				Provider:           v1alpha1.EntitlementProviderRedHat,
				Product:            v1alpha1.EntitlementProductCeph,
				RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
				RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
				},
				Registry: &v1alpha1.EnvironmentEntitlementRegistry{
					CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
				},
			}},
			want: "rhelEntitlementRef is only valid for the ibm/ibm-storage-ceph product",
		},
		{
			name: "registry-url-credentials",
			entitlements: []v1alpha1.EnvironmentEntitlement{{
				Name:     "rhcs",
				Provider: v1alpha1.EntitlementProviderRedHat,
				Product:  v1alpha1.EntitlementProductCeph,
				RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
				},
				Registry: &v1alpha1.EnvironmentEntitlementRegistry{
					URL:            "user:pass@registry.example.test",
					CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
				},
			}},
			want: "registry.url must not embed credentials; use credentialsRef",
		},
		{
			name: "satellite-valid",
			entitlements: rhelWithSatellite(&v1alpha1.EnvironmentEntitlementRHSMSatellite{
				Hostname:       "satellite.corp.example.com",
				TrustBundleRef: v1alpha1.SecretRef{Name: "corp-satellite-ca"},
			}),
		},
		{
			name: "satellite-missing-hostname",
			entitlements: rhelWithSatellite(&v1alpha1.EnvironmentEntitlementRHSMSatellite{
				TrustBundleRef: v1alpha1.SecretRef{Name: "corp-satellite-ca"},
			}),
			want: "satellite.hostname is required when satellite is set",
		},
		{
			name: "satellite-hostname-is-url",
			entitlements: rhelWithSatellite(&v1alpha1.EnvironmentEntitlementRHSMSatellite{
				Hostname: "https://satellite.corp.example.com",
			}),
			want: "satellite.hostname must be a bare host",
		},
		{
			name: "satellite-bad-content-url",
			entitlements: rhelWithSatellite(&v1alpha1.EnvironmentEntitlementRHSMSatellite{
				Hostname:       "satellite.corp.example.com",
				ContentBaseURL: "ftp://satellite.corp.example.com/pulp",
			}),
			want: "satellite.contentBaseURL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := v1alpha1.Environment{
				Metadata: v1alpha1.Metadata{Name: "env"},
				Spec: v1alpha1.EnvironmentSpec{
					Entitlements: tc.entitlements,
				},
			}
			got := strings.Join(validateEnvironmentEntitlements(env), "; ")
			if tc.want == "" {
				if got != "" {
					t.Fatalf("validateEnvironmentEntitlements errors = %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateEnvironmentEntitlements errors = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMachineImageRedHatCDNRequiresRHELEntitlement(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				Entitlements: []v1alpha1.EnvironmentEntitlement{{
					Name:     "rhcs",
					Provider: v1alpha1.EntitlementProviderRedHat,
					Product:  v1alpha1.EntitlementProductCeph,
				}},
			},
		}},
		MachineImages: []v1alpha1.MachineImage{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.MachineImageSpec{
				Type:      v1alpha1.MachineImageTypeISO,
				MediaType: v1alpha1.MachineImageMediaTypeBoot,
				URL:       "local-media:rhel-9.8-x86_64-boot.iso",
				InstallSource: v1alpha1.MachineImageInstallSource{
					Type:           v1alpha1.MachineImageInstallSourceTypeRHSM,
					EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
				},
			},
		}},
	}
	errs := validateMachineImageEntitlements(state)
	if got := strings.Join(errs, "; "); !strings.Contains(got, `resolves to product "ceph", want "rhel"`) {
		t.Fatalf("validateMachineImageEntitlements errors = %q", got)
	}
}
