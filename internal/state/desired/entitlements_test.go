package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestEnvironmentEntitlementValidation(t *testing.T) {
	cases := []struct {
		name        string
		entitlement v1alpha1.EnvironmentEntitlement
		want        string
	}{
		{
			name: "redhat-ceph-valid",
			entitlement: v1alpha1.EnvironmentEntitlement{
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
			},
		},
		{
			name: "invalid-provider",
			entitlement: v1alpha1.EnvironmentEntitlement{
				Name:     "bad",
				Provider: "vendor",
				Product:  v1alpha1.EntitlementProductCeph,
			},
			want: `provider "vendor" must be one of`,
		},
		{
			name: "invalid-product",
			entitlement: v1alpha1.EnvironmentEntitlement{
				Name:     "bad",
				Provider: v1alpha1.EntitlementProviderRedHat,
				Product:  "storage",
			},
			want: `product "storage" must be one of`,
		},
		{
			name: "invalid-provider-product",
			entitlement: v1alpha1.EnvironmentEntitlement{
				Name:     "bad",
				Provider: v1alpha1.EntitlementProviderIBM,
				Product:  v1alpha1.EntitlementProductRHEL,
			},
			want: "provider/product ibm/rhel is not supported",
		},
		{
			name: "redhat-ceph-missing-registry",
			entitlement: v1alpha1.EnvironmentEntitlement{
				Name:     "rhcs",
				Provider: v1alpha1.EntitlementProviderRedHat,
				Product:  v1alpha1.EntitlementProductCeph,
				RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
				},
			},
			want: "registry.credentialsRef is required",
		},
		{
			name: "ibm-license-not-accepted",
			entitlement: v1alpha1.EnvironmentEntitlement{
				Name:     "ibm-ceph",
				Provider: v1alpha1.EntitlementProviderIBM,
				Product:  v1alpha1.EntitlementProductIBMStorageCeph,
				RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "ibm-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "ibm-key"},
				},
				Registry: &v1alpha1.EnvironmentEntitlementRegistry{
					CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
				},
			},
			want: "license.accept must be true",
		},
		{
			name: "registry-url-credentials",
			entitlement: v1alpha1.EnvironmentEntitlement{
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
			},
			want: "registry.url must not embed credentials; use credentialsRef",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := v1alpha1.Environment{
				Metadata: v1alpha1.Metadata{Name: "env"},
				Spec: v1alpha1.EnvironmentSpec{
					Entitlements: []v1alpha1.EnvironmentEntitlement{tc.entitlement},
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
