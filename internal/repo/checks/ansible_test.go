package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func TestProxyEnvironmentPlaybooksResolveProxyFacts(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/playbooks/check_become.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/check_preflight.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_prepare.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_apply.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_finalize.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_managed_machine_os_apply.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_container_cluster_boot_agent_machine.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_container_cluster_create_agent_iso.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_container_cluster_agent_destroy.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_container_cluster_agent_install.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_container_cluster_wait_agent_install.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_provider_services_apply.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_apply.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_destroy.yml",
	} {
		for _, play := range readAnsiblePlays(t, path) {
			env, _ := play["environment"].(string)
			if !strings.Contains(env, "bootwright_proxy_env") {
				continue
			}
			preTasks, ok := play["pre_tasks"].([]any)
			if !ok {
				t.Fatalf("%s play %q uses bootwright_proxy_env without pre_tasks", path, play["name"])
			}
			if !hasHostProxyFactsImport(preTasks) {
				t.Fatalf("%s play %q must import machine_proxy facts before proxied tasks", path, play["name"])
			}
		}
	}
}

func TestShellTasksDeclareChangeAndFailure(t *testing.T) {
	root := filepath.Join(repoRoot(t), filepath.FromSlash(bootwrightCollectionRoleRoot))
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".yml" || !strings.Contains(path, string(filepath.Separator)+"tasks"+string(filepath.Separator)) {
			return nil
		}
		rel, err := filepath.Rel(repoRoot(t), path)
		if err != nil {
			return err
		}
		tasks := readAnsibleTasks(t, rel)
		checkShellTaskGuards(t, rel, tasks)
		return nil
	})
	if err != nil {
		t.Fatalf("walk ansible roles: %v", err)
	}
}

func TestRoleTemplateLookupsReferenceExistingTemplates(t *testing.T) {
	lookupRE := regexp.MustCompile(`lookup\(\s*['"](?:ansible\.builtin\.)?template['"]\s*,\s*['"]([^'"]+)['"]`)
	root := filepath.Join(repoRoot(t), filepath.FromSlash(bootwrightCollectionRoleRoot))
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".yml" || !strings.Contains(path, string(filepath.Separator)+"tasks"+string(filepath.Separator)) {
			return nil
		}
		rel, err := filepath.Rel(repoRoot(t), path)
		if err != nil {
			return err
		}
		roleRoot := strings.Split(rel, "/tasks/")[0]
		for _, match := range lookupRE.FindAllStringSubmatch(readRepoFile(t, rel), -1) {
			templateRel := filepath.ToSlash(filepath.Clean(match[1]))
			if strings.HasPrefix(templateRel, "../") || filepath.IsAbs(templateRel) {
				t.Fatalf("%s references template outside role templates: %s", rel, match[1])
			}
			target := filepath.ToSlash(filepath.Join(roleRoot, "templates", templateRel))
			if !repoFileExists(t, target) {
				t.Fatalf("%s references missing role template %s", rel, target)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk ansible roles: %v", err)
	}
}

func TestHostProxyFactsHonorNoProxyOnlyConfiguration(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_proxy/tasks/facts.yml")
	resolveTask := tasks[findAnsibleTask(t, tasks, "Resolve proxy desired state")]
	facts, ok := resolveTask["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s set_fact missing", resolveTask["name"])
	}
	if enabled := fmt.Sprint(facts["bootwright_proxy_enabled"]); !strings.Contains(enabled, "bootwright_proxy_no | length") {
		t.Fatalf("bootwright_proxy_enabled must treat noProxy as configured, got %q", enabled)
	}
	for _, name := range []string{"bootwright_proxy_has_url", "bootwright_proxy_credentialed_url"} {
		if _, ok := facts[name]; !ok {
			t.Fatalf("Resolve proxy desired state missing %s", name)
		}
	}

	loadTask := tasks[findAnsibleTask(t, tasks, "Load proxy credentials")]
	if got := loadTask["when"]; got != "bootwright_proxy_credentialed_url" {
		t.Fatalf("%s when got %v, want credentialed URL guard", loadTask["name"], got)
	}
	for _, name := range []string{
		"Build credentialed proxy URLs",
		"Select effective proxy URLs",
		"Select primary proxy URL",
	} {
		task := tasks[findAnsibleTask(t, tasks, name)]
		if got := task["when"]; got != "bootwright_proxy_has_url" {
			t.Fatalf("%s when got %v, want URL guard", name, got)
		}
	}

	envTask := tasks[findAnsibleTask(t, tasks, "Build proxy environment facts")]
	envFacts, ok := envTask["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s set_fact missing", envTask["name"])
	}
	envExpr := fmt.Sprint(envFacts["bootwright_proxy_env"])
	for _, want := range []string{"'NO_PROXY': bootwright_proxy_no", "'no_proxy': bootwright_proxy_no", "if bootwright_proxy_enabled"} {
		if !strings.Contains(envExpr, want) {
			t.Fatalf("%s missing %q in proxy env expression: %s", envTask["name"], want, envExpr)
		}
	}
}

func TestHostProxyPersistenceDrivesPackageManagersFromEnvironment(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_proxy/tasks/persist.yml")
	persist := tasks[findAnsibleTask(t, tasks, "Persist proxy settings on host")]
	if got := persist["when"]; got != "bootwright_proxy_enabled" {
		t.Fatalf("%s when got %v, want bootwright_proxy_enabled", persist["name"], got)
	}
	block := nestedAnsibleTasks(t, persist, "block")

	envTask := block[findAnsibleTask(t, block, "Render /etc/environment proxy lines")]
	if got := envTask["when"]; got != "not bootwright_proxy_credentialed_url" {
		t.Fatalf("%s when got %v, want non-credentialed guard", envTask["name"], got)
	}
	envBody := fmt.Sprint(envTask["ansible.builtin.blockinfile"])
	if !strings.Contains(envBody, "NO_PROXY={{ bootwright_proxy_no }}") {
		t.Fatalf("%s must persist NO_PROXY, got %s", envTask["name"], envBody)
	}

	for _, task := range block {
		line, ok := task["ansible.builtin.lineinfile"].(map[string]any)
		if !ok {
			continue
		}
		path := fmt.Sprint(line["path"])
		if path != "/etc/dnf/dnf.conf" && path != "/etc/yum.conf" {
			continue
		}
		if _, ok := line["line"]; ok {
			t.Fatalf("%s must not write a proxy= line into %s; the proxy comes from the environment", task["name"], path)
		}
		if got := fmt.Sprint(line["state"]); got != "absent" {
			t.Fatalf("%s must strip the proxy= line (state: absent), got state=%v", task["name"], line["state"])
		}
	}

	for _, name := range []string{"Strip dnf proxy line", "Strip yum proxy line (RHEL 7 / legacy)"} {
		task := block[findAnsibleTask(t, block, name)]
		if got := fmt.Sprint(task["when"]); !strings.Contains(got, ".stat.exists") {
			t.Fatalf("%s must run when the config file exists, got when=%v", name, task["when"])
		}
	}

	if task := block[findAnsibleTask(t, block, "Remove stale pip proxy config when URL proxy is disabled")]; !stringListContains(task["when"], "not bootwright_proxy_has_url") {
		t.Fatalf("pip strip must run when URL proxy is disabled, got when=%v", task["when"])
	}
	for _, name := range []string{"Ensure pip config directory", "Render pip proxy config", "Verify proxy TCP reachability"} {
		task := block[findAnsibleTask(t, block, name)]
		if got := task["when"]; got != "bootwright_proxy_has_url" {
			t.Fatalf("%s when got %v, want proxy URL guard", name, got)
		}
	}
}

func TestBootwrightCollectionFiltersUseFQCN(t *testing.T) {
	root := filepath.Join(repoRoot(t), "ansible", "collections", "ansible_collections", "bootwright", "core")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yml" && ext != ".yaml" && ext != ".j2" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if idx := shortBootwrightFilterIndex(string(body)); idx >= 0 {
			rel, _ := filepath.Rel(repoRoot(t), path)
			t.Fatalf("%s uses short Bootwright filter name near byte %d; use bootwright.core.<filter>", filepath.ToSlash(rel), idx)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Bootwright collection: %v", err)
	}
}

func shortBootwrightFilterIndex(body string) int {
	for i := 0; i < len(body); i++ {
		if body[i] != '|' {
			continue
		}
		j := i + 1
		for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '\r' || body[j] == '\n') {
			j++
		}
		if strings.HasPrefix(body[j:], "bootwright_") {
			return i
		}
	}
	return -1
}

func TestAnsibleCorePinsStayAligned(t *testing.T) {
	pin := ""
	for _, component := range render.ComponentPins(v1alpha1.State{}) {
		if component.Name == "ansible-core" {
			pin = component.Version
			break
		}
	}
	if pin == "" {
		t.Fatal("ansible-core component pin missing")
	}
	for _, path := range []string{
		".github/workflows/checks.yml",
		".github/workflows/release.yml",
	} {
		if body := readRepoFile(t, path); !strings.Contains(body, "ansible-core=="+pin) {
			t.Fatalf("%s must install ansible-core==%s", path, pin)
		}
	}
	if body := readRepoFile(t, "Containerfile"); !strings.Contains(body, "ARG ANSIBLE_CORE_VERSION="+pin) {
		t.Fatalf("Containerfile must set ANSIBLE_CORE_VERSION=%s", pin)
	}
}

func TestAnsibleRemoteBecomeTempConfig(t *testing.T) {
	for _, path := range []string{
		"ansible/ansible.cfg",
	} {
		if !repoFileExists(t, path) {
			continue
		}
		cfg := readRepoFile(t, path)
		localTmp, ok := ansibleCfgValue(cfg, "defaults", "local_tmp")
		if !ok {
			t.Fatalf("%s must explicitly configure local_tmp for controller temp files", path)
		}
		if localTmp != "/var/tmp" {
			t.Fatalf("%s local_tmp must use /var/tmp so controller Ansible does not depend on writable home dirs or tmpfs capacity; got %q", path, localTmp)
		}
		tmp, ok := ansibleCfgValue(cfg, "defaults", "remote_tmp")
		if !ok {
			t.Fatalf("%s must explicitly configure remote_tmp for remote become tasks", path)
		}
		if tmp != "/var/tmp" {
			t.Fatalf("%s remote_tmp must use /var/tmp so sudo-root modules are readable outside restricted home dirs without depending on small tmpfs capacity; got %q", path, tmp)
		}
		value, ok := ansibleCfgValue(cfg, "ssh_connection", "pipelining")
		if !ok {
			t.Fatalf("%s must explicitly configure SSH pipelining for remote become tasks", path)
		}
		if strings.ToLower(value) != "false" {
			t.Fatalf("%s must disable SSH pipelining; remote sudo requiretty hosts fail before fact gathering", path)
		}
		args, ok := ansibleCfgValue(cfg, "ssh_connection", "ssh_common_args")
		if !ok {
			t.Fatalf("%s must explicitly configure SSH common args", path)
		}
		if !strings.Contains(args, "StrictHostKeyChecking=accept-new") {
			t.Fatalf("%s SSH common args must accept new host keys without accepting changed keys; got %q", path, args)
		}
		collectionsPath, ok := ansibleCfgValue(cfg, "defaults", "collections_path")
		if !ok {
			t.Fatalf("%s must explicitly configure collections_path", path)
		}
		if collectionsPath != "./collections" {
			t.Fatalf("%s collections_path got %q, want ./collections", path, collectionsPath)
		}
	}
}

func TestHostBaseFirewalldAvailabilityRequiresRunningDaemon(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_base/tasks/firewalld_probe.yml")
	binaryIdx := findAnsibleTask(t, tasks, "Detect firewall-cmd binary")
	stateIdx := findAnsibleTask(t, tasks, "Detect running firewalld daemon")
	factIdx := findAnsibleTask(t, tasks, "Set firewalld availability fact")
	if !(binaryIdx < stateIdx && stateIdx < factIdx) {
		t.Fatalf("machine_base must detect firewall-cmd before probing daemon state and setting the availability fact")
	}

	command, ok := tasks[stateIdx]["ansible.builtin.command"].(string)
	if !ok || command != "/usr/bin/firewall-cmd --state" {
		t.Fatalf("firewalld daemon probe command got %v, want firewall-cmd --state", tasks[stateIdx]["ansible.builtin.command"])
	}
	if got := tasks[stateIdx]["failed_when"]; got != false {
		t.Fatalf("firewalld daemon probe must not fail stopped-daemon hosts, got failed_when=%v", got)
	}
	if got := tasks[stateIdx]["changed_when"]; got != false {
		t.Fatalf("firewalld daemon probe must not report changed, got changed_when=%v", got)
	}

	setFact, ok := tasks[factIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[factIdx]["name"])
	}
	fact, ok := setFact["bootwright_firewalld_available"].(string)
	if !ok {
		t.Fatalf("bootwright_firewalld_available fact got %v", setFact["bootwright_firewalld_available"])
	}
	for _, want := range []string{"bootwright_firewalld_state.rc", "bootwright_firewalld_state.stdout", "running"} {
		if !strings.Contains(fact, want) {
			t.Fatalf("bootwright_firewalld_available fact must depend on %q, got %q", want, fact)
		}
	}
}

func TestHostBaseAllowsUnavailableRedHatReposBeforePackageInstall(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_base/tasks/main.yml")
	dnfStatIdx := findAnsibleTask(t, tasks, "Stat dnf.conf")
	dnfIdx := findAnsibleTask(t, tasks, "Allow unavailable DNF repositories")
	yumStatIdx := findAnsibleTask(t, tasks, "Stat yum.conf")
	yumIdx := findAnsibleTask(t, tasks, "Allow unavailable YUM repositories")
	installIdx := findAnsibleTask(t, tasks, "Install base host packages")
	if !(dnfStatIdx < dnfIdx && dnfIdx < installIdx && yumStatIdx < yumIdx && yumIdx < installIdx) {
		t.Fatalf("machine_base must allow unavailable Red Hat repos before installing packages")
	}

	for _, idx := range []int{dnfIdx, yumIdx} {
		task := tasks[idx]
		lineinfile, ok := task["ansible.builtin.lineinfile"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no lineinfile body", task["name"])
		}
		if got, _ := lineinfile["line"].(string); got != "skip_if_unavailable=True" {
			t.Fatalf("%s line got %q, want skip_if_unavailable=True", task["name"], got)
		}
		if got, _ := lineinfile["insertafter"].(string); got != "^\\[main\\]" {
			t.Fatalf("%s insertafter got %q, want main section", task["name"], got)
		}
	}
}

func TestOCPCLIsDownloadOnlyWhenClientToolsNeedInstall(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/controller_openshift_tools/tasks/main.yml")
	ocProbeIdx := findAnsibleTask(t, tasks, "Probe installed oc version")
	kubectlProbeIdx := findAnsibleTask(t, tasks, "Probe installed kubectl")
	decideIdx := findAnsibleTask(t, tasks, "Decide whether each CLI needs install (require exact version)")
	downloadClientIdx := findAnsibleTask(t, tasks, "Download openshift-client tarball")
	extractClientIdx := findAnsibleTask(t, tasks, "Extract oc and kubectl into install dir")
	if !(ocProbeIdx < kubectlProbeIdx && kubectlProbeIdx < decideIdx && decideIdx < downloadClientIdx && downloadClientIdx < extractClientIdx) {
		t.Fatalf("ocp_clis must probe oc and kubectl before deciding whether to download the client tarball")
	}

	setFact, ok := tasks[decideIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[decideIdx]["name"])
	}
	client, ok := setFact["bootwright_clis_install_client"].(string)
	if !ok {
		t.Fatalf("bootwright_clis_install_client got %v", setFact["bootwright_clis_install_client"])
	}
	for _, want := range []string{"bootwright_clis_oc_probe.rc", "bootwright_clis_oc_version", "bootwright_openshift_release_version", "bootwright_clis_kubectl_probe.rc"} {
		if !strings.Contains(client, want) {
			t.Fatalf("client install decision must depend on %q, got %q", want, client)
		}
	}
	for _, idx := range []int{downloadClientIdx, extractClientIdx} {
		when, ok := tasks[idx]["when"].(string)
		if !ok || when != "bootwright_clis_install_client | bool" {
			t.Fatalf("%s when got %v, want bootwright_clis_install_client | bool", tasks[idx]["name"], tasks[idx]["when"])
		}
	}
}

func TestOCPCLIsRemoveStaleBinariesAndVerifyFinalVersions(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/controller_openshift_tools/tasks/main.yml")
	downloadClientIdx := findAnsibleTask(t, tasks, "Download openshift-client tarball")
	removeClientIdx := findAnsibleTask(t, tasks, "Remove stale oc and kubectl from install dir")
	extractClientIdx := findAnsibleTask(t, tasks, "Extract oc and kubectl into install dir")
	downloadInstallerIdx := findAnsibleTask(t, tasks, "Download openshift-install tarball")
	removeInstallerIdx := findAnsibleTask(t, tasks, "Remove stale openshift-install from install dir")
	extractInstallerIdx := findAnsibleTask(t, tasks, "Extract openshift-install into install dir")
	finalAssertIdx := findAnsibleTask(t, tasks, "Verify final OpenShift CLI versions match requested release")
	if !(downloadClientIdx < removeClientIdx && removeClientIdx < extractClientIdx && extractClientIdx < finalAssertIdx) {
		t.Fatalf("ocp_clis must remove stale oc/kubectl before extract and verify final versions")
	}
	if !(downloadInstallerIdx < removeInstallerIdx && removeInstallerIdx < extractInstallerIdx && extractInstallerIdx < finalAssertIdx) {
		t.Fatalf("ocp_clis must remove stale openshift-install before extract and verify final versions")
	}
	for _, tc := range []struct {
		idx  int
		want string
	}{
		{idx: removeClientIdx, want: "bootwright_clis_install_client | bool"},
		{idx: removeInstallerIdx, want: "bootwright_clis_install_oi | bool"},
	} {
		if got, _ := tasks[tc.idx]["when"].(string); got != tc.want {
			t.Fatalf("%s when got %v, want %q", tasks[tc.idx]["name"], tasks[tc.idx]["when"], tc.want)
		}
	}
	assertTask, ok := tasks[finalAssertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert body", tasks[finalAssertIdx]["name"])
	}
	that, ok := assertTask["that"].([]any)
	if !ok {
		t.Fatalf("%s assert.that got %v", tasks[finalAssertIdx]["name"], assertTask["that"])
	}
	joined := fmt.Sprint(that)
	for _, want := range []string{
		"bootwright_clis_final_oi_version == bootwright_openshift_release_version",
		"bootwright_clis_final_oc_version == bootwright_openshift_release_version",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("%s assert must contain %q, got %v", tasks[finalAssertIdx]["name"], want, that)
		}
	}
}

func TestOCPCLIsInstallFIPSInstallerWhenRequired(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/controller_openshift_tools/tasks/main.yml")

	probe := tasks[findAnsibleTask(t, tasks, "Probe installed openshift-install-fips version")]
	if got, _ := probe["when"].(string); got != "bootwright_clis_fips_required | default(false) | bool" {
		t.Fatalf("FIPS probe must gate on bootwright_clis_fips_required, got %v", probe["when"])
	}

	downloadIdx := findAnsibleTask(t, tasks, "Download openshift-install FIPS tarball")
	removeIdx := findAnsibleTask(t, tasks, "Remove stale openshift-install-fips from install dir")
	extractIdx := findAnsibleTask(t, tasks, "Extract openshift-install-fips into install dir")
	finalAssertIdx := findAnsibleTask(t, tasks, "Verify final OpenShift CLI versions match requested release")
	if !(downloadIdx < removeIdx && removeIdx < extractIdx && extractIdx < finalAssertIdx) {
		t.Fatalf("FIPS installer must download, remove stale, then extract before the final version check")
	}
	for _, idx := range []int{downloadIdx, removeIdx, extractIdx} {
		if got, _ := tasks[idx]["when"].(string); got != "bootwright_clis_install_oi_fips | bool" {
			t.Fatalf("%s must gate on bootwright_clis_install_oi_fips, got %v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}

	get, ok := tasks[downloadIdx]["ansible.builtin.get_url"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no get_url body", tasks[downloadIdx]["name"])
	}
	if url := fmt.Sprint(get["url"]); !strings.HasSuffix(url, "/openshift-install-rhel9-amd64.tar.gz") {
		t.Fatalf("FIPS installer must come from the RHEL9 archive, got url=%v", get["url"])
	}
	if !strings.Contains(fmt.Sprint(get["checksum"]), "bootwright_clis_installer_fips_checksum") {
		t.Fatalf("FIPS installer download must verify its own checksum, got %v", get["checksum"])
	}

	extract, ok := tasks[extractIdx]["ansible.builtin.unarchive"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no unarchive body", tasks[extractIdx]["name"])
	}
	if !stringListContains(extract["include"], "openshift-install-fips") {
		t.Fatalf("FIPS extract must select the openshift-install-fips member, got %v", extract["include"])
	}

	defaults := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/defaults/main.yml")
	for _, want := range []string{"openshift-install-fips", "bootwright_current_cluster.fips"} {
		if !strings.Contains(defaults, want) {
			t.Fatalf("agent install default must select %q, got:\n%s", want, defaults)
		}
	}
}
