package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func machineInstallRepositoriesState(repos v1alpha1.MachineInstallRepositories) v1alpha1.State {
	state := machineInstallPackageSourceState(&v1alpha1.MachineInstallPackageSource{
		Mirror: &v1alpha1.MachineInstallPackageMirror{BaseURL: "https://mirror.test/BaseOS"},
	})
	state.MachineInstallProfiles[0].Spec.Customizations.Repositories = repos
	return state
}

func machineInstallSubscribedRepositoriesState(repos v1alpha1.MachineInstallRepositories) v1alpha1.State {
	state := machineInstallRepositoriesState(repos)
	state.MachineInstallProfiles[0].Spec.Installer.Anaconda.PackageSource = &v1alpha1.MachineInstallPackageSource{
		FromSubscription: &v1alpha1.MachineInstallPackageFromSubscription{
			EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
		},
	}
	return state
}

func boolPtr(v bool) *bool { return &v }

func TestMachineInstallRepositoriesAcceptsConfigureEntry(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallRepositoriesState(v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{{
			ID:        "tools",
			BaseURL:   "https://mirror.test/tools",
			GPGKeyURL: "https://mirror.test/RPM-GPG-KEY",
		}},
	}))
	if len(errs) != 0 {
		t.Fatalf("validateMachineInstallProfiles errors = %v", errs)
	}
}

func TestMachineInstallRepositoriesRequiresIDAndBaseURL(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallRepositoriesState(v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{{GPGCheck: boolPtr(false)}},
	}))
	if !containsSubstring(errs, "configure[0].id is required") {
		t.Fatalf("errors = %v, want id requirement", errs)
	}
	if !containsSubstring(errs, "configure[0].baseURL is required") {
		t.Fatalf("errors = %v, want baseURL requirement", errs)
	}
}

func TestMachineInstallRepositoriesRejectsSlashInID(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallRepositoriesState(v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{{
			ID:       "tools/9",
			BaseURL:  "https://mirror.test/tools",
			GPGCheck: boolPtr(false),
		}},
	}))
	if !containsSubstring(errs, "must not contain whitespace, quotes, or slashes") {
		t.Fatalf("errors = %v, want id charset rejection", errs)
	}
}

func TestMachineInstallRepositoriesRejectsDuplicateID(t *testing.T) {
	entry := v1alpha1.MachineInstallRepositoryFile{
		ID:       "tools",
		BaseURL:  "https://mirror.test/tools",
		GPGCheck: boolPtr(false),
	}
	errs := validateMachineInstallProfiles(machineInstallRepositoriesState(v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{entry, entry},
	}))
	if !containsSubstring(errs, `configure[1].id "tools" is duplicated`) {
		t.Fatalf("errors = %v, want duplicate id rejection", errs)
	}
}

func TestMachineInstallRepositoriesRequiresGPGKeyWhenChecking(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallRepositoriesState(v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{{
			ID:      "tools",
			BaseURL: "https://mirror.test/tools",
		}},
	}))
	if !containsSubstring(errs, "gpgKeyURL is required when gpgCheck is enabled") {
		t.Fatalf("errors = %v, want gpgKeyURL requirement", errs)
	}
}

func TestMachineInstallRepositoriesAllowsUnsignedWithoutKey(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallRepositoriesState(v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{{
			ID:       "tools",
			BaseURL:  "https://mirror.test/tools",
			GPGCheck: boolPtr(false),
		}},
	}))
	if len(errs) != 0 {
		t.Fatalf("validateMachineInstallProfiles errors = %v", errs)
	}
}

func TestMachineInstallRepositoriesRejectsNonHTTPBaseURL(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallRepositoriesState(v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{{
			ID:       "tools",
			BaseURL:  "file:///srv/tools",
			GPGCheck: boolPtr(false),
		}},
	}))
	if !containsSubstring(errs, "baseURL must be http:// or https://") {
		t.Fatalf("errors = %v, want baseURL scheme rejection", errs)
	}
}

func TestMachineInstallRepositoriesAcceptsFileGPGKey(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallRepositoriesState(v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{{
			ID:        "tools",
			BaseURL:   "https://mirror.test/tools",
			GPGKeyURL: "file:///etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release",
		}},
	}))
	if len(errs) != 0 {
		t.Fatalf("validateMachineInstallProfiles errors = %v", errs)
	}
}

func TestMachineInstallSubscriptionRepositoriesRequireRegistration(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallRepositoriesState(v1alpha1.MachineInstallRepositories{
		Subscription: &v1alpha1.MachineInstallSubscriptionRepositories{
			Enable: []string{"rhel-9-for-x86_64-baseos-rpms"},
		},
	}))
	if !containsSubstring(errs, "requires the node to be registered with RHSM") {
		t.Fatalf("errors = %v, want registration requirement", errs)
	}
}

func TestMachineInstallSubscriptionRepositoriesAcceptedWhenRegistered(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallSubscribedRepositoriesState(v1alpha1.MachineInstallRepositories{
		Subscription: &v1alpha1.MachineInstallSubscriptionRepositories{
			Enable:  []string{"rhel-9-for-x86_64-baseos-rpms"},
			Disable: []string{"*"},
		},
	}))
	if len(errs) != 0 {
		t.Fatalf("validateMachineInstallProfiles errors = %v", errs)
	}
}

func TestMachineInstallSubscriptionRepositoriesRejectWildcardEnable(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallSubscribedRepositoriesState(v1alpha1.MachineInstallRepositories{
		Subscription: &v1alpha1.MachineInstallSubscriptionRepositories{Enable: []string{"*"}},
	}))
	if !containsSubstring(errs, "enable must name concrete repository ids") {
		t.Fatalf("errors = %v, want wildcard enable rejection", errs)
	}
}

func TestMachineInstallSubscriptionRepositoriesRejectConflictingID(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallSubscribedRepositoriesState(v1alpha1.MachineInstallRepositories{
		Subscription: &v1alpha1.MachineInstallSubscriptionRepositories{
			Enable:  []string{"rhel-9-for-x86_64-baseos-rpms"},
			Disable: []string{"rhel-9-for-x86_64-baseos-rpms"},
		},
	}))
	if !containsSubstring(errs, "is also listed in") {
		t.Fatalf("errors = %v, want enable/disable conflict rejection", errs)
	}
}

func TestMachineInstallSubscriptionRepositoriesRejectEmptyBlock(t *testing.T) {
	errs := validateMachineInstallProfiles(machineInstallSubscribedRepositoriesState(v1alpha1.MachineInstallRepositories{
		Subscription: &v1alpha1.MachineInstallSubscriptionRepositories{},
	}))
	if !containsSubstring(errs, "must set at least one of: enable, disable") {
		t.Fatalf("errors = %v, want empty subscription rejection", errs)
	}
}
