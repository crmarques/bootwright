package repocheck

import (
	"fmt"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestKickstartGatesSubscriptionRepositoriesOnInstallTimeRegistration(t *testing.T) {
	template := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/templates/ks.cfg.j2")
	if !strings.Contains(template, "{% if (rhsm.enabled | default(false)) and (subscription_repos | length > 0) %}") {
		t.Fatal("ks.cfg.j2 must gate the subscription repository block on rhsm.enabled; subscription-manager has no identity in the installer chroot when the profile registers day-2 instead")
	}
	disableAt := strings.Index(template, `subscription-manager repos --disable=`)
	enableAt := strings.Index(template, `subscription-manager repos --enable=`)
	if disableAt < 0 || enableAt < 0 {
		t.Fatal("ks.cfg.j2 must select subscription repositories with subscription-manager repos")
	}
	if disableAt > enableAt {
		t.Fatal("ks.cfg.j2 must emit --disable before --enable, otherwise a wildcard disable would switch off the repositories just enabled")
	}
}

func TestKickstartPersistsConfiguredRepositoryFiles(t *testing.T) {
	template := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/templates/ks.cfg.j2")
	for _, want := range []string{
		"{% for repo in repositories.configure | default([]) %}",
		"cat > {{ repo.path }} <<'BOOTWRIGHT_REPO_FILE'",
		"enabled={{ 1 if repo.enabled else 0 }}",
		"gpgcheck={{ 1 if repo.gpgCheck else 0 }}",
	} {
		if !strings.Contains(template, want) {
			t.Fatalf("ks.cfg.j2 must persist configured repositories; missing %q", want)
		}
	}
}

func TestMachineRepositoriesPlaybookSkipsProvidedAndUndeclaredHosts(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_repositories_apply.yml"
	var plays []map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, path)), &plays); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(plays) != 1 {
		t.Fatalf("%s has %d plays, want 1", path, len(plays))
	}
	var tasks []map[string]any
	for _, raw := range plays[0]["tasks"].([]any) {
		task := map[string]any{}
		for key, value := range raw.(map[string]any) {
			task[fmt.Sprint(key)] = value
		}
		tasks = append(tasks, task)
	}
	for _, name := range []string{"End provided-OS hosts", "End hosts declaring no repositories"} {
		task := tasks[findAnsibleTask(t, tasks, name)]
		if fmt.Sprint(task["ansible.builtin.meta"]) != "end_host" {
			t.Fatalf("%s must end the host, got %v", name, task)
		}
	}
}

func TestMachineRepositoriesRoleRequiresRegistrationBeforeSubscriptionSelection(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_repositories/tasks/subscription.yml")
	assert := tasks[findAnsibleTask(t, tasks, "Require RHSM registration before selecting subscription repositories")]
	if !strings.Contains(fmt.Sprint(assert["ansible.builtin.assert"]), "rc == 0") {
		t.Fatalf("subscription selection must assert a successful subscription-manager identity probe, got %v", assert["ansible.builtin.assert"])
	}
	enable := tasks[findAnsibleTask(t, tasks, "Enable declared subscription repositories")]
	if !strings.Contains(fmt.Sprint(enable["community.general.rhsm_repository"]), "purge") {
		t.Fatalf("enable task must honour the rendered purge flag, got %v", enable)
	}
}
