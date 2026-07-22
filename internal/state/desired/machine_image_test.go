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

func machineInstallPackageSourceState(source *v1alpha1.MachineInstallPackageSource) v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					ArtifactServers: []v1alpha1.EnvironmentArtifactServerComponent{{
						Name:         "default",
						Management:   v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "artifact-server"},
					}},
				},
			},
		}},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "artifact-server"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotArtifactServer,
				ArtifactServer: &v1alpha1.ArtifactServerComponent{
					Listeners: []v1alpha1.ArtifactServerListener{{
						Name:     "http",
						Protocol: v1alpha1.ArtifactServerProtocolHTTP,
						Port:     8080,
					}},
					Endpoints: []v1alpha1.ArtifactServerEndpoint{{
						Name:        "tree",
						ListenerRef: v1alpha1.LocalObjectReference{Name: "http"},
						AddressRef:  v1alpha1.LocalObjectReference{Name: "artifact"},
					}},
				},
			},
		}},
		MachineImages: []v1alpha1.MachineImage{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.MachineImageSpec{
				BootMedia: "local-media:rhel-9.8-x86_64-boot.iso",
			},
		}},
		MachineInstallProfiles: []v1alpha1.MachineInstallProfile{{
			Metadata: v1alpha1.Metadata{Name: "rhel-profile"},
			Spec: v1alpha1.MachineInstallProfileSpec{
				OS: v1alpha1.MachineInstallOS{
					Family:       "rhel",
					Version:      "9.8",
					Architecture: "x86_64",
				},
				Installer: v1alpha1.MachineInstallProfileInstaller{
					Anaconda: &v1alpha1.MachineInstallAnaconda{
						ImageRef:      v1alpha1.LocalObjectReference{Name: "rhel"},
						PackageSource: source,
					},
				},
			},
		}},
	}
}

func TestMachineInstallPackageSourceRequiresExactlyOneArm(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(&v1alpha1.MachineInstallPackageSource{}))
	if len(errs) == 0 {
		t.Fatal("validateMachineInstallProfiles accepted a packageSource with no arm set")
	}
	if !strings.Contains(errs[0], "packageSource must set exactly one of: mirror, fromSubscription, hostedTree") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineInstallPackageSourceMirrorAcceptsRepositories(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(&v1alpha1.MachineInstallPackageSource{
		Mirror: &v1alpha1.MachineInstallPackageMirror{
			BaseURL: "https://repos.example.test/rhel/9/BaseOS/x86_64/os/",
			Repositories: []v1alpha1.MachineInstallRepository{
				{ID: "appstream", BaseURL: "https://repos.example.test/rhel/9/AppStream/x86_64/os/"},
			},
		},
	}))
	if len(errs) != 0 {
		t.Fatalf("validateMachineInstallProfiles errors = %v", errs)
	}
}

func TestMachineInstallPackageSourceMirrorRejectsNonHTTPRepositoryBaseURL(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(&v1alpha1.MachineInstallPackageSource{
		Mirror: &v1alpha1.MachineInstallPackageMirror{
			BaseURL: "https://repos.example.test/rhel/9/BaseOS/x86_64/os/",
			Repositories: []v1alpha1.MachineInstallRepository{
				{ID: "baseos", BaseURL: "ftp://repos.example.test/rhel/9/BaseOS/x86_64/os/"},
			},
		},
	}))
	if len(errs) == 0 {
		t.Fatal("validateMachineInstallProfiles accepted non-http repository baseURL")
	}
	if !strings.Contains(errs[0], "packageSource.mirror.repositories[0].baseURL must be http:// or https://") {
		t.Fatalf("error = %q", errs[0])
	}
}

func TestMachineInstallPackageSourceMirrorRequiresBaseURL(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(&v1alpha1.MachineInstallPackageSource{
		Mirror: &v1alpha1.MachineInstallPackageMirror{},
	}))
	if !containsSubstring(errs, "packageSource.mirror.baseURL is required") {
		t.Fatalf("errors = %v, want mirror.baseURL requirement", errs)
	}
}

func TestMachineInstallPackageSourceFromSubscriptionAcceptsEntitlementRef(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(&v1alpha1.MachineInstallPackageSource{
		FromSubscription: &v1alpha1.MachineInstallPackageFromSubscription{
			EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
		},
	}))
	if len(errs) != 0 {
		t.Fatalf("validateMachineInstallProfiles errors = %v", errs)
	}
}

func TestMachineInstallPackageSourceFromSubscriptionRequiresEntitlementRef(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallPackageSourceState(&v1alpha1.MachineInstallPackageSource{
		FromSubscription: &v1alpha1.MachineInstallPackageFromSubscription{},
	}))
	if len(errs) == 0 {
		t.Fatal("validateMachineInstallProfiles accepted fromSubscription source without entitlementRef")
	}
	if !strings.Contains(strings.Join(errs, "\n"), "packageSource.fromSubscription.entitlementRef is required") {
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
		MachineImages: machineInstallPackageSourceState(nil).MachineImages,
		MachineInstallProfiles: []v1alpha1.MachineInstallProfile{{
			Metadata: v1alpha1.Metadata{Name: "rhel-profile"},
			Spec: v1alpha1.MachineInstallProfileSpec{
				OS: v1alpha1.MachineInstallOS{
					Family:       "rhel",
					Version:      "9.8",
					Architecture: "x86_64",
				},
				Installer: v1alpha1.MachineInstallProfileInstaller{
					Anaconda: &v1alpha1.MachineInstallAnaconda{
						ImageRef: v1alpha1.LocalObjectReference{Name: "rhel"},
						PackageSource: &v1alpha1.MachineInstallPackageSource{
							FromSubscription: &v1alpha1.MachineInstallPackageFromSubscription{
								EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
							},
						},
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
