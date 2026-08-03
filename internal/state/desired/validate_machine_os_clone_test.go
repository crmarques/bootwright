package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestTemplateCloneRefusesEveryCustomizationItCannotHonour(t *testing.T) {
	enabled := true
	cases := []struct {
		name   string
		mutate func(*v1alpha1.MachineInstallCustomizations)
		want   string
	}{
		{
			name:   "storage.rootDevice",
			mutate: func(c *v1alpha1.MachineInstallCustomizations) { c.Storage.RootDevice.Source = "byPath" },
			want:   "storage.rootDevice has no effect under installer.templateClone",
		},
		{
			name:   "packages",
			mutate: func(c *v1alpha1.MachineInstallCustomizations) { c.Packages.Environment = "minimal-environment" },
			want:   "packages has no effect under installer.templateClone",
		},
		{
			name:   "localization",
			mutate: func(c *v1alpha1.MachineInstallCustomizations) { c.Localization.Timezone = "UTC" },
			want:   "localization has no effect under installer.templateClone",
		},
		{
			name:   "security.selinux",
			mutate: func(c *v1alpha1.MachineInstallCustomizations) { c.Security.SELinux.Mode = "enforcing" },
			want:   "security.selinux has no effect under installer.templateClone",
		},
		{
			name:   "security.firewall",
			mutate: func(c *v1alpha1.MachineInstallCustomizations) { c.Security.Firewall.Enabled = &enabled },
			want:   "security.firewall has no effect under installer.templateClone",
		},
		{
			name:   "security.fips",
			mutate: func(c *v1alpha1.MachineInstallCustomizations) { c.Security.FIPS.Enabled = true },
			want:   "security.fips.enabled is not supported under installer.templateClone",
		},
		{
			name: "security.diskEncryption",
			mutate: func(c *v1alpha1.MachineInstallCustomizations) {
				c.Security.DiskEncryption = &v1alpha1.MachineInstallDiskEncryption{}
			},
			want: "security.diskEncryption is not supported under installer.templateClone",
		},
		{
			name: "ssh.initialPassword",
			mutate: func(c *v1alpha1.MachineInstallCustomizations) {
				c.SSH.InitialPassword = &v1alpha1.MachineInstallInitialPassword{}
			},
			want: "ssh.initialPassword is not supported under installer.templateClone",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c v1alpha1.MachineInstallCustomizations
			tc.mutate(&c)
			errs := validateMachineInstallCloneRefusals("spec.customizations", c)
			if len(errs) != 1 {
				t.Fatalf("%s must produce exactly one refusal, got %v", tc.name, errs)
			}
			if !strings.Contains(errs[0], tc.want) {
				t.Fatalf("refusal must name the unhonoured field, got %q", errs[0])
			}
			if !strings.Contains(errs[0], "spec.customizations.") {
				t.Fatalf("refusal must name the owning field path, got %q", errs[0])
			}
		})
	}
}

func TestTemplateCloneInitialPasswordRefusalNamesThePlaintextLeak(t *testing.T) {
	var c v1alpha1.MachineInstallCustomizations
	c.SSH.InitialPassword = &v1alpha1.MachineInstallInitialPassword{}
	errs := validateMachineInstallCloneRefusals("spec.customizations", c)
	if len(errs) != 1 {
		t.Fatalf("initialPassword must be refused, got %v", errs)
	}
	for _, want := range []string{"extraConfig", "plaintext", "installer.anaconda"} {
		if !strings.Contains(errs[0], want) {
			t.Fatalf("the initialPassword refusal is the only thing keeping a console password out of the VMX, so it must name %q; got %q", want, errs[0])
		}
	}
}

func TestTemplateCloneHonouredCustomizationsAreNotRefused(t *testing.T) {
	var c v1alpha1.MachineInstallCustomizations
	c.Repositories.Configure = []v1alpha1.MachineInstallRepositoryFile{{ID: "baseos", BaseURL: "https://example.test/baseos"}}
	if errs := validateMachineInstallCloneRefusals("spec.customizations", c); len(errs) != 0 {
		t.Fatalf("a clone honours day-2 repositories, so they must not be refused, got %v", errs)
	}
}
