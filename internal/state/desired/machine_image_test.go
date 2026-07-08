package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestMachineImageBootMediaAcceptsLocalMediaReference(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			BootMedia: "local-media:rhel.iso",
		},
	}}})
	if len(errs) != 0 {
		t.Fatalf("validateMachineImages errors = %v", errs)
	}
}

func TestMachineImageBootMediaRejectsInvalidLocalMediaReference(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			BootMedia: "local-media:../rhel.iso",
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted invalid media reference")
	}
	if !strings.Contains(errs[0], "must be a filename") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImageBootMediaRejectsRetiredMediaReference(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			BootMedia: "media:rhel.iso",
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted retired media reference")
	}
	if !strings.Contains(errs[0], "must use local-media:") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImagePackageSourceRequiresExactlyOneArm(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			BootMedia:     "local-media:rhel-9.8-x86_64-boot.iso",
			PackageSource: &v1alpha1.MachinePackageSource{},
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted a packageSource with no arm set")
	}
	if !strings.Contains(errs[0], "packageSource must set exactly one of: mirror, redhatCDN, hostedTree") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImagePackageSourceMirrorAcceptsRepositories(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			BootMedia: "local-media:rhel-9.8-x86_64-boot.iso",
			PackageSource: &v1alpha1.MachinePackageSource{
				Mirror: &v1alpha1.MachinePackageMirror{
					BaseURL: "https://repos.example.test/rhel/9/BaseOS/x86_64/os/",
					Repositories: []v1alpha1.MachineInstallRepository{
						{ID: "appstream", BaseURL: "https://repos.example.test/rhel/9/AppStream/x86_64/os/"},
					},
				},
			},
		},
	}}})
	if len(errs) != 0 {
		t.Fatalf("validateMachineImages errors = %v", errs)
	}
}

func TestMachineImagePackageSourceMirrorRejectsNonHTTPRepositoryBaseURL(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			BootMedia: "local-media:rhel-9.8-x86_64-boot.iso",
			PackageSource: &v1alpha1.MachinePackageSource{
				Mirror: &v1alpha1.MachinePackageMirror{
					BaseURL: "https://repos.example.test/rhel/9/BaseOS/x86_64/os/",
					Repositories: []v1alpha1.MachineInstallRepository{
						{ID: "baseos", BaseURL: "ftp://repos.example.test/rhel/9/BaseOS/x86_64/os/"},
					},
				},
			},
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted non-http repository baseURL")
	}
	if !strings.Contains(errs[0], "packageSource.mirror.repositories[0].baseURL must be http:// or https://") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineImagePackageSourceMirrorRequiresBaseURL(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			BootMedia: "local-media:rhel-9.8-x86_64-boot.iso",
			PackageSource: &v1alpha1.MachinePackageSource{
				Mirror: &v1alpha1.MachinePackageMirror{},
			},
		},
	}}})
	if !containsSubstring(errs, "packageSource.mirror.baseURL is required") {
		t.Fatalf("errors = %v, want mirror.baseURL requirement", errs)
	}
}

func TestMachineImagePackageSourceRedhatCDNAcceptsEntitlementRef(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			BootMedia: "local-media:rhel-9.8-x86_64-boot.iso",
			PackageSource: &v1alpha1.MachinePackageSource{
				RedhatCDN: &v1alpha1.MachinePackageRedhatCDN{
					EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
				},
			},
		},
	}}})
	if len(errs) != 0 {
		t.Fatalf("validateMachineImages errors = %v", errs)
	}
}

func TestMachineImagePackageSourceRedhatCDNRequiresEntitlementRef(t *testing.T) {
	errs := validateMachineImages(v1alpha1.State{MachineImages: []v1alpha1.MachineImage{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.MachineImageSpec{
			BootMedia: "local-media:rhel-9.8-x86_64-boot.iso",
			PackageSource: &v1alpha1.MachinePackageSource{
				RedhatCDN: &v1alpha1.MachinePackageRedhatCDN{},
			},
		},
	}}})
	if len(errs) == 0 {
		t.Fatal("validateMachineImages accepted redhatCDN source without entitlementRef")
	}
	if !strings.Contains(strings.Join(errs, "\n"), "packageSource.redhatCDN.entitlementRef is required") {
		t.Fatalf("errors = %v", errs)
	}
}

func TestEntitlementRHSMSecretRefsMustBeDeclared(t *testing.T) {
	errs := validateSecretReferences(v1alpha1.State{
		Secrets: []v1alpha1.Secret{{
			Metadata: v1alpha1.Metadata{Name: "redhat-org"},
			Spec:     v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque},
		}},
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec:     v1alpha1.EnvironmentSpec{},
		}},
		Entitlements: []v1alpha1.Entitlement{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.EntitlementSpec{
				Type: v1alpha1.EntitlementTypeRedHatRHEL,
				RHSM: &v1alpha1.EntitlementRHSM{
					OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
					ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-activation-key"},
				},
			},
		}},
		MachineImages: []v1alpha1.MachineImage{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.MachineImageSpec{
				BootMedia: "local-media:rhel-9.8-x86_64-boot.iso",
				PackageSource: &v1alpha1.MachinePackageSource{
					RedhatCDN: &v1alpha1.MachinePackageRedhatCDN{
						EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
					},
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
