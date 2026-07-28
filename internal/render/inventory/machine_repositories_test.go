package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func repoBoolPtr(v bool) *bool { return &v }

func TestMachineInstallRepositoriesVarsDefaultsEnabledAndGPGCheck(t *testing.T) {
	vars := machineInstallRepositoriesVars(v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{{
			ID:        "tools",
			BaseURL:   "https://mirror.test/tools",
			GPGKeyURL: "https://mirror.test/KEY",
		}},
	}, nil)
	configure, ok := vars["configure"].([]any)
	if !ok || len(configure) != 1 {
		t.Fatalf("configure = %#v, want one entry", vars["configure"])
	}
	entry := configure[0].(map[string]any)
	if entry["enabled"] != true {
		t.Fatalf("enabled = %#v, want true by default", entry["enabled"])
	}
	if entry["gpgCheck"] != true {
		t.Fatalf("gpgCheck = %#v, want true by default", entry["gpgCheck"])
	}
	if entry["name"] != "tools" {
		t.Fatalf("name = %#v, want the id as the display-name fallback", entry["name"])
	}
	if entry["path"] != "/etc/yum.repos.d/bootwright-tools.repo" {
		t.Fatalf("path = %#v", entry["path"])
	}
}

func TestMachineInstallRepositoriesVarsCarryExplicitFalses(t *testing.T) {
	vars := machineInstallRepositoriesVars(v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{{
			ID:          "tools",
			DisplayName: "Vendor tools",
			BaseURL:     "https://mirror.test/tools",
			Enabled:     repoBoolPtr(false),
			GPGCheck:    repoBoolPtr(false),
		}},
	}, nil)
	entry := vars["configure"].([]any)[0].(map[string]any)
	if entry["enabled"] != false || entry["gpgCheck"] != false {
		t.Fatalf("entry = %#v, want explicit false preserved", entry)
	}
	if entry["name"] != "Vendor tools" {
		t.Fatalf("name = %#v, want the display name", entry["name"])
	}
	if _, ok := entry["gpgKeyURL"]; ok {
		t.Fatalf("entry = %#v, want no gpgKeyURL key when unset", entry)
	}
}

func TestMachineInstallRepositoriesVarsMarkWildcardDisableAsPurge(t *testing.T) {
	vars := machineInstallRepositoriesVars(v1alpha1.MachineInstallRepositories{
		Subscription: &v1alpha1.MachineInstallSubscriptionRepositories{
			Enable:  []string{"rhel-9-for-x86_64-baseos-rpms"},
			Disable: []string{"*"},
		},
	}, nil)
	subscription, ok := vars["subscription"].(map[string]any)
	if !ok {
		t.Fatalf("subscription = %#v, want a map", vars["subscription"])
	}
	if subscription["purge"] != true {
		t.Fatalf("purge = %#v, want true for a wildcard disable", subscription["purge"])
	}
}

func TestMachineInstallRepositoriesVarsWithoutWildcardDoNotPurge(t *testing.T) {
	vars := machineInstallRepositoriesVars(v1alpha1.MachineInstallRepositories{
		Subscription: &v1alpha1.MachineInstallSubscriptionRepositories{
			Enable:  []string{"rhel-9-for-x86_64-baseos-rpms"},
			Disable: []string{"rhel-9-for-x86_64-supplementary-rpms"},
		},
	}, nil)
	subscription := vars["subscription"].(map[string]any)
	if subscription["purge"] != false {
		t.Fatalf("purge = %#v, want false without a wildcard disable", subscription["purge"])
	}
}

func TestMachineInstallRepositoriesVarsEmptyForEmptyBlock(t *testing.T) {
	vars := machineInstallRepositoriesVars(v1alpha1.MachineInstallRepositories{}, nil)
	if len(vars) != 0 {
		t.Fatalf("vars = %#v, want an empty map so hosts declaring nothing are skipped", vars)
	}
}
