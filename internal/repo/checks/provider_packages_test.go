package repocheck

import (
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestProviderPackageTasksUseOSVars(t *testing.T) {
	cases := []struct {
		base    string
		role    string
		tasks   string
		load    string
		install string
		varName string
	}{
		{
			base:    "providers",
			role:    "bmc_emulated",
			tasks:   "packages.yml",
			load:    "Load BMC package list",
			install: "Install BMC packages",
			varName: "{{ bootwright_bmc_packages }}",
		},
		{
			base:    "infra_components",
			role:    "proxy_squid",
			tasks:   "main.yml",
			load:    "Load Squid package list",
			install: "Install Squid packages",
			varName: "{{ bootwright_squid_packages }}",
		},
		{
			base:    "infra_components",
			role:    "mirror_registry",
			tasks:   "main.yml",
			load:    "Load mirror registry package list",
			install: "Install mirror registry packages",
			varName: "{{ bootwright_mr_packages }}",
		},
		{
			base:    "infra_components",
			role:    "ntp_chrony",
			tasks:   "main.yml",
			load:    "Load chrony package list",
			install: "Install chrony packages",
			varName: "{{ bootwright_chrony_packages }}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			tasks := readAnsibleTasks(t, "ansible/roles/"+tc.base+"/"+tc.role+"/tasks/"+tc.tasks)
			loadIdx := findAnsibleTask(t, tasks, tc.load)
			installIdx := findAnsibleTask(t, tasks, tc.install)
			if loadIdx >= installIdx {
				t.Fatalf("%s must load OS package vars before installing packages", tc.role)
			}
			pkg, ok := tasks[installIdx]["ansible.builtin.package"].(map[string]any)
			if !ok {
				t.Fatalf("%s is not a package task", tc.install)
			}
			if got, _ := pkg["name"].(string); got != tc.varName {
				t.Fatalf("%s package name got %q, want %q", tc.install, got, tc.varName)
			}
		})
	}
}

func TestProviderHtpasswdPackagesAreOSSpecific(t *testing.T) {
	cases := []struct {
		base       string
		role       string
		varName    string
		debianWant []string
		redHatWant []string
	}{
		{
			base:       "providers",
			role:       "bmc_emulated",
			varName:    "bootwright_bmc_packages",
			debianWant: []string{"apache2-utils", "python3-venv"},
			redHatWant: []string{"httpd-tools"},
		},
		{
			base:       "infra_components",
			role:       "proxy_squid",
			varName:    "bootwright_squid_packages",
			debianWant: []string{"apache2-utils"},
			redHatWant: []string{"httpd-tools"},
		},
		{
			base:       "infra_components",
			role:       "mirror_registry",
			varName:    "bootwright_mr_packages",
			debianWant: []string{"apache2-utils"},
			redHatWant: []string{"httpd-tools"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			debian := readAnsibleStringListVar(t, "ansible/roles/"+tc.base+"/"+tc.role+"/vars/Debian.yml", tc.varName)
			redHat := readAnsibleStringListVar(t, "ansible/roles/"+tc.base+"/"+tc.role+"/vars/RedHat.yml", tc.varName)

			assertContainsAll(t, debian, tc.debianWant)
			assertContainsNone(t, debian, []string{"httpd-tools"})
			assertContainsAll(t, redHat, tc.redHatWant)
			assertContainsNone(t, redHat, []string{"apache2-utils"})
		})
	}
}

func readAnsibleStringListVar(t *testing.T, rel, name string) []string {
	t.Helper()
	var vars map[string][]string
	if err := yaml.Unmarshal([]byte(readRepoFile(t, rel)), &vars); err != nil {
		t.Fatalf("%s: decode YAML: %v", rel, err)
	}
	got, ok := vars[name]
	if !ok {
		t.Fatalf("%s missing %s", rel, name)
	}
	return got
}

func assertContainsAll(t *testing.T, got, want []string) {
	t.Helper()
	for _, item := range want {
		if !containsString(got, item) {
			t.Fatalf("list %v missing %q", got, item)
		}
	}
}

func assertContainsNone(t *testing.T, got, want []string) {
	t.Helper()
	for _, item := range want {
		if containsString(got, item) {
			t.Fatalf("list %v must not contain %q", got, item)
		}
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
