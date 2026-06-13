package repocheck

import (
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestProviderPackageTasksUseOSVars(t *testing.T) {
	cases := []struct {
		role    string
		path    string
		tasks   string
		load    string
		install string
		varName string
	}{
		{
			role:    "provider_service_bmc_emulated",
			path:    "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated",
			tasks:   "apply/packages.yml",
			load:    "Load BMC package list",
			install: "Install BMC packages",
			varName: "{{ bootwright_bmc_packages }}",
		},
		{
			role:    "infra_component_proxy_squid",
			path:    "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_proxy_squid",
			tasks:   "main.yml",
			load:    "Load Squid package list",
			install: "Install Squid packages",
			varName: "{{ bootwright_squid_packages }}",
		},
		{
			role:    "infra_component_registry_mirror",
			path:    "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_registry_mirror",
			tasks:   "main.yml",
			load:    "Load mirror registry package list",
			install: "Install mirror registry packages",
			varName: "{{ bootwright_mr_packages }}",
		},
		{
			role:    "infra_component_ntp_chrony",
			path:    "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_ntp_chrony",
			tasks:   "main.yml",
			load:    "Load chrony package list",
			install: "Install chrony packages",
			varName: "{{ bootwright_chrony_packages }}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			tasks := readAnsibleTasks(t, tc.path+"/tasks/"+tc.tasks)
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
		role       string
		path       string
		varName    string
		debianWant []string
		redHatWant []string
	}{
		{
			role:       "provider_service_bmc_emulated",
			path:       "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated",
			varName:    "bootwright_bmc_packages",
			debianWant: []string{"apache2-utils", "python3-venv"},
			redHatWant: []string{"httpd-tools"},
		},
		{
			role:       "infra_component_proxy_squid",
			path:       "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_proxy_squid",
			varName:    "bootwright_squid_packages",
			debianWant: []string{"apache2-utils"},
			redHatWant: []string{"httpd-tools"},
		},
		{
			role:       "infra_component_registry_mirror",
			path:       "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_registry_mirror",
			varName:    "bootwright_mr_packages",
			debianWant: []string{"apache2-utils"},
			redHatWant: []string{"httpd-tools"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			debian := readAnsibleStringListVar(t, tc.path+"/vars/os/Debian.yml", tc.varName)
			redHat := readAnsibleStringListVar(t, tc.path+"/vars/os/RedHat.yml", tc.varName)

			assertContainsAll(t, debian, tc.debianWant)
			assertContainsNone(t, debian, []string{"httpd-tools"})
			assertContainsAll(t, redHat, tc.redHatWant)
			assertContainsNone(t, redHat, []string{"apache2-utils"})
		})
	}
}

func TestLibvirtHostPackagesCoverCommandDependencies(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/provider_host_libvirt"
	debian := readAnsibleStringListVar(t, path+"/vars/os/Debian.yml", "bootwright_provider_host_libvirt_packages")
	redHat := readAnsibleStringListVar(t, path+"/vars/os/RedHat.yml", "bootwright_provider_host_libvirt_packages")

	assertContainsAll(t, debian, []string{"qemu-system-x86", "qemu-utils", "libvirt-daemon-system", "libvirt-clients", "virtinst", "python3-libvirt"})
	assertContainsAll(t, redHat, []string{"qemu-kvm", "qemu-img", "libvirt", "libvirt-client", "virt-install", "python3-libvirt"})
	assertContainsNone(t, debian, []string{"qemu-img", "libvirt-client"})
	assertContainsNone(t, redHat, []string{"qemu-utils", "libvirt-clients"})
}

func TestControllerToolPackagesCoverArchiveAndSSHDependencies(t *testing.T) {
	packages := readAnsibleStringListVar(t, "ansible/collections/ansible_collections/bootwright/core/roles/controller_openshift_tools/defaults/main.yml", "bootwright_clis_packages")

	assertContainsAll(t, packages, []string{"openssh-clients", "nmstate", "procps-ng", "tar", "gzip"})
}

func TestAnacondaPackagesCoverMkksisoOnRedHat(t *testing.T) {
	packages := readAnsibleStringListVar(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/vars/os/RedHat.yml", "bootwright_machine_os_install_anaconda_packages")

	assertContainsAll(t, packages, []string{"lorax"})
}

func readAnsibleStringListVar(t *testing.T, rel, name string) []string {
	t.Helper()
	var vars map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, rel)), &vars); err != nil {
		t.Fatalf("%s: decode YAML: %v", rel, err)
	}
	got, ok := vars[name]
	if !ok {
		t.Fatalf("%s missing %s", rel, name)
	}
	items, ok := got.([]any)
	if !ok {
		t.Fatalf("%s %s is %T, want list", rel, name, got)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("%s %s contains %T, want string", rel, name, item)
		}
		out = append(out, text)
	}
	return out
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
