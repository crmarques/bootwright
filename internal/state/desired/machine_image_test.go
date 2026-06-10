package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestMachineImageURLAcceptsLocalMediaReference(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type: v1alpha1.MachineImageTypeISO,
			URL:  "local-media:rhel.iso",
		},
	}}})
	if len(errs) != 0 {
		t.Fatalf("validateMachineImages errors = %v", errs)
	}
}

func TestMachineImageURLRejectsInvalidLocalMediaReference(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type: v1alpha1.MachineImageTypeISO,
			URL:  "local-media:../rhel.iso",
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted invalid media reference")
	}
	if !strings.Contains(errs[0], "must be a filename") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImageURLRejectsRetiredMediaReference(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type: v1alpha1.MachineImageTypeISO,
			URL:  "media:rhel.iso",
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted retired media reference")
	}
	if !strings.Contains(errs[0], "must use local-media:") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImageBootISORequiresInstallSource(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type:      v1alpha1.MachineImageTypeISO,
			MediaType: v1alpha1.MachineImageMediaTypeBoot,
			URL:       "local-media:rhel-9.8-x86_64-boot.iso",
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted boot ISO without install source")
	}
	if !strings.Contains(errs[0], "installSource is required") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImageBootISOAcceptsInstallSourceRepositories(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type:      v1alpha1.MachineImageTypeISO,
			MediaType: v1alpha1.MachineImageMediaTypeBoot,
			URL:       "local-media:rhel-9.8-x86_64-boot.iso",
			InstallSource: v1alpha1.MachineImageInstallSource{
				Type: v1alpha1.MachineImageInstallSourceTypeURL,
				Repositories: []v1alpha1.MachineInstallRepository{
					{ID: "baseos", BaseURL: "https://repos.example.test/rhel/9/BaseOS/x86_64/os/"},
					{ID: "appstream", BaseURL: "https://repos.example.test/rhel/9/AppStream/x86_64/os/"},
				},
			},
		},
	}}})
	if len(errs) != 0 {
		t.Fatalf("validateMachineImages errors = %v", errs)
	}
}

func TestMachineImageInstallSourceRepositoryRejectsNonHTTPBaseURL(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type:      v1alpha1.MachineImageTypeISO,
			MediaType: v1alpha1.MachineImageMediaTypeBoot,
			URL:       "local-media:rhel-9.8-x86_64-boot.iso",
			InstallSource: v1alpha1.MachineImageInstallSource{
				Type: v1alpha1.MachineImageInstallSourceTypeURL,
				Repositories: []v1alpha1.MachineInstallRepository{
					{ID: "baseos", BaseURL: "ftp://repos.example.test/rhel/9/BaseOS/x86_64/os/"},
				},
			},
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted non-http repository baseURL")
	}
	if !strings.Contains(errs[0], "installSource.repositories[0].baseURL must be http:// or https://") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImageBootISOAcceptsRedHatCDNEntitlementRef(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type:      v1alpha1.MachineImageTypeISO,
			MediaType: v1alpha1.MachineImageMediaTypeBoot,
			URL:       "local-media:rhel-9.8-x86_64-boot.iso",
			InstallSource: v1alpha1.MachineImageInstallSource{
				Type:           v1alpha1.MachineImageInstallSourceTypeRHSM,
				EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
			},
		},
	}}})
	if len(errs) != 0 {
		t.Fatalf("validateMachineImages errors = %v", errs)
	}
}

func TestMachineImageRedHatCDNInstallSourceRequiresEntitlementRef(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type:      v1alpha1.MachineImageTypeISO,
			MediaType: v1alpha1.MachineImageMediaTypeBoot,
			URL:       "local-media:rhel-9.8-x86_64-boot.iso",
			InstallSource: v1alpha1.MachineImageInstallSource{
				Type: v1alpha1.MachineImageInstallSourceTypeRHSM,
			},
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted redhatCDN source without entitlementRef")
	}
	if !strings.Contains(strings.Join(errs, "\n"), "entitlementRef is required") {
		t.Fatalf("errors = %v", errs)
	}
}

func TestEntitlementRHSMSecretRefsMustBeDeclared(t *testing.T) {
	errs := validateSecretReferences(v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				Secrets: v1alpha1.EnvironmentSecrets{
					"redhat-org": {},
				},
				Entitlements: []v1alpha1.EnvironmentEntitlement{{
					Name:     "rhel",
					Provider: v1alpha1.EntitlementProviderRedHat,
					Product:  v1alpha1.EntitlementProductRHEL,
					RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
						OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
						ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
					},
				}},
			},
		}},
		MachineImages: []v1alpha1.MachineImage{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.MachineImageSpec{
				InstallSource: v1alpha1.MachineImageInstallSource{
					EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
				},
			},
		}},
	})
	if len(errs) == 0 {
		t.Fatal("validateSecretReferences accepted undeclared entitlement RHSM secret ref")
	}
	if !strings.Contains(errs[0], "redhat-activation-key") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImageBootISOInferredFromFilename(t *testing.T) {
	state := v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type: v1alpha1.MachineImageTypeISO,
			URL:  "local-media:rhel-9.8-x86_64-boot.iso",
		},
	}}}
	Normalize(&state)
	if got := state.MachineImages[0].Spec.MediaType; got != v1alpha1.MachineImageMediaTypeBoot {
		t.Fatalf("materialized mediaType = %q, want %q", got, v1alpha1.MachineImageMediaTypeBoot)
	}
	errs := validateMachineImages(state)
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted inferred boot ISO without install source")
	}
	if !strings.Contains(errs[0], "installSource is required") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImageRejectsUnknownMediaType(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type:      v1alpha1.MachineImageTypeISO,
			MediaType: "cdrom",
			URL:       "local-media:rhel.iso",
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted unknown mediaType")
	}
	if !strings.Contains(errs[0], `mediaType "cdrom" must be one of: dvd, boot`) {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImageRejectsUnknownInstallSourceType(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			Type:      v1alpha1.MachineImageTypeISO,
			MediaType: v1alpha1.MachineImageMediaTypeBoot,
			URL:       "local-media:rhel-9.8-x86_64-boot.iso",
			InstallSource: v1alpha1.MachineImageInstallSource{
				Type: "cdn",
			},
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted unknown installSource.type")
	}
	if !strings.Contains(errs[0], `installSource.type "cdn" must be one of: url, redhatCDN`) {
		t.Fatalf("error = %q", errs[0])
	}
}
