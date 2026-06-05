package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/support"
	"github.com/crmarques/bootwright/internal/render"
	"go.yaml.in/yaml/v3"
)

const bootwrightCollectionRoleRoot = "ansible/collections/ansible_collections/bootwright/core/roles"

func TestBootRedfishLibvirtVirtualMediaDetachFallback(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/eject.yml")
	task := tasks[findAnsibleTask(t, tasks, "Eject source-backed virtual media from {{ bootwright_libvirt_media_scope }} domain")]
	script := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/files/eject_libvirt_media.sh")

	if strings.Contains(script, "${#") {
		t.Fatalf("libvirt media cleanup script uses Bash syntax that Ansible parses as a Jinja comment")
	}

	for _, want := range []string{
		"change-media \"$domain\" \"$target\" --eject --force \"$state_arg\"",
		"change-media \"$domain\" \"$target\" --eject --force \"$state_arg\" --print-xml",
		"update-device \"$domain\" \"$tmp\" \"$state_arg\" --force",
		"detach-disk \"$domain\" \"$target\" \"$state_arg\"",
		"domblklist \"$domain\" \"${domblklist_args[@]}\" --details",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("libvirt media cleanup script missing %q", want)
		}
	}

	bootRedfish := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml")
	if strings.Contains(bootRedfish, "eject_libvirt_media") || strings.Contains(bootRedfish, "boot.media.libvirt") {
		t.Fatalf("boot_redfish must dispatch media cleanup through mediaPrepareRole")
	}

	command, ok := task["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", task["name"])
	}
	argv, ok := command["argv"].(string)
	if !ok || !strings.Contains(argv, "/tmp/bootwright-libvirt-media-eject.sh") {
		t.Fatalf("libvirt media cleanup command must invoke installed helper script, got %v", command["argv"])
	}
}

func TestClustersApplyRunsPreflightBeforeInfraAndInstall(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/workflow_clusters_apply.yml")
	preflight := strings.Index(body, "check_preflight.yml")
	prepare := strings.Index(body, "task_machine_infra_prepare.yml")
	infra := strings.Index(body, "task_machine_infra_apply.yml")
	finalize := strings.Index(body, "task_machine_infra_finalize.yml")
	install := strings.Index(body, "task_container_cluster_agent_install.yml")
	if preflight < 0 || prepare < 0 || infra < 0 || finalize < 0 || install < 0 || preflight > prepare || prepare > infra || infra > finalize || finalize > install {
		t.Fatalf("clusters apply must run preflight before machine-infra prepare/apply/finalize and install-agent")
	}
}

func TestContainerClusterApplyRunsPreflightBeforeInstall(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/workflow_container_cluster_apply.yml")
	preflight := strings.Index(body, "check_preflight.yml")
	install := strings.Index(body, "task_container_cluster_agent_install.yml")
	if preflight < 0 || install < 0 || preflight > install {
		t.Fatalf("container-cluster apply must run preflight before install-agent")
	}
}

func TestStorageOperationsUseExplicitIdempotencyContract(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/run.yml")
	for _, want := range []string{
		"bootwright_ceph_op_idempotency_kind",
		"bootwright_ceph_op_idempotency_name",
		"bootwright_ceph_op_idempotency_kind == 'ceph-pool'",
		"bootwright_ceph_op_idempotency_kind == 'rgw-user'",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("storage operation runner missing explicit idempotency fragment %q", want)
		}
	}
	for _, forbidden := range []string{
		".startswith('create-",
		"bootwright_ceph_op_command[",
		".index('--uid')",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("storage operation runner must not infer idempotency from %q", forbidden)
		}
	}
}

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

func TestHostProxyPersistenceKeepsNoProxyOnlyConfiguration(t *testing.T) {
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

	for _, name := range []string{"Configure dnf proxy", "Configure yum proxy"} {
		task := block[findAnsibleTask(t, block, name)]
		if !stringListContains(task["when"], "bootwright_proxy_has_url") {
			t.Fatalf("%s must require a proxy URL, got when=%v", name, task["when"])
		}
	}
	for _, name := range []string{
		"Strip stale dnf proxy line when URL proxy is disabled",
		"Strip stale yum proxy line when URL proxy is disabled",
		"Remove stale pip proxy config when URL proxy is disabled",
	} {
		task := block[findAnsibleTask(t, block, name)]
		if !stringListContains(task["when"], "not bootwright_proxy_has_url") {
			t.Fatalf("%s must run when URL proxy is disabled, got when=%v", name, task["when"])
		}
	}
	for _, name := range []string{"Ensure pip config directory", "Render pip proxy config", "Verify proxy TCP reachability"} {
		task := block[findAnsibleTask(t, block, name)]
		if got := task["when"]; got != "bootwright_proxy_has_url" {
			t.Fatalf("%s when got %v, want proxy URL guard", name, got)
		}
	}
}

func TestRedfishURIRequestsDoNotOverrideProxyEnvironment(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/check_external_reachability/tasks/bmc.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/eject.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/post_boot.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_state_probe.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/validation/macs.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml",
	} {
		for _, task := range readAnsibleTasks(t, path) {
			uri, ok := task["ansible.builtin.uri"].(map[string]any)
			if !ok {
				continue
			}
			if _, ok := uri["use_proxy"]; ok {
				t.Fatalf("%s task %q must honor bootwright_proxy_env instead of overriding use_proxy", path, task["name"])
			}
		}
	}
}

func TestManagedOSSSHTrustKeyscanWaitsForHostKeys(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/ssh_trust.yml")
	scan := tasks[findAnsibleTask(t, tasks, "Scan managed OS SSH host key")]
	if _, ok := scan["retries"]; !ok {
		t.Fatalf("%s must retry because port 22 can open before sshd returns host keys", scan["name"])
	}
	if _, ok := scan["delay"]; !ok {
		t.Fatalf("%s must set retry delay", scan["name"])
	}
	until := fmt.Sprint(scan["until"])
	for _, want := range []string{
		"bootwright_os_ssh_keyscan_required",
		"stdout_lines",
		"reject('match', '^#')",
	} {
		if !strings.Contains(until, want) {
			t.Fatalf("%s until missing %q: %s", scan["name"], want, until)
		}
	}
	failedWhen := fmt.Sprint(scan["failed_when"])
	if !strings.Contains(failedWhen, "not in [0, 1]") {
		t.Fatalf("%s must tolerate ssh-keyscan rc=1 while waiting for keys, got %s", scan["name"], failedWhen)
	}

	mainTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/main.yml")
	pre := mainTasks[findAnsibleTask(t, mainTasks, "Record managed OS SSH host key before install when reachable")]
	assertIncludeTasksFile(t, pre, "ssh_trust.yml")
	preVars, ok := pre["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass keyscan vars", pre["name"])
	}
	if got := fmt.Sprint(preVars["bootwright_os_ssh_keyscan_required"]); got != "false" {
		t.Fatalf("%s keyscan required = %s, want false", pre["name"], got)
	}

	post := mainTasks[findAnsibleTask(t, mainTasks, "Record managed OS SSH host key")]
	assertIncludeTasksFile(t, post, "ssh_trust.yml")
	postVars, ok := post["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s must pass keyscan vars", post["name"])
	}
	for key, want := range map[string]string{
		"bootwright_os_ssh_keyscan_required": "true",
		"bootwright_os_ssh_keyscan_retries":  "60",
		"bootwright_os_ssh_keyscan_delay":    "10",
	} {
		if got := fmt.Sprint(postVars[key]); got != want {
			t.Fatalf("%s %s = %s, want %s", post["name"], key, got, want)
		}
	}
}

func TestMachineInfraPreparePreparesHostPackages(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_prepare.yml")
	if len(plays) != 1 {
		t.Fatalf("machine infra prepare play count = %d, want 1", len(plays))
	}
	rawTasks, ok := plays[0]["tasks"].([]any)
	if !ok {
		t.Fatalf("machine infra prepare play missing tasks: %v", plays[0])
	}
	tasks := make([]map[string]any, 0, len(rawTasks))
	for _, raw := range rawTasks {
		task, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("task is not a map: %v", raw)
		}
		tasks = append(tasks, task)
	}

	baseIdx := findAnsibleTask(t, tasks, "Apply base host packages")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve selected container cluster")
	if baseIdx >= resolveIdx {
		t.Fatalf("machine infra prepare must apply base packages before resolving selected work")
	}
	importRole, ok := tasks[baseIdx]["ansible.builtin.import_role"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an import_role task", tasks[baseIdx]["name"])
	}
	if got := importRole["name"]; got != "bootwright.core.machine_base" {
		t.Fatalf("%s imports %v, want bootwright.core.machine_base", tasks[baseIdx]["name"], got)
	}
}

func TestManagedOSAnacondaInstallsMkksisoPackage(t *testing.T) {
	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/tasks/main.yml")
	validateSourceIdx := findAnsibleTask(t, topTasks, "Validate managed Anaconda install source")
	resolvePathsIdx := findAnsibleTask(t, topTasks, "Resolve managed OS install paths")
	resolveFilesIdx := findAnsibleTask(t, topTasks, "Resolve managed OS install files")
	resolveSourcePathIdx := findAnsibleTask(t, topTasks, "Resolve managed OS install source path")
	if validateSourceIdx >= resolvePathsIdx {
		t.Fatalf("Anaconda role must validate install source before resolving install paths")
	}
	if !(resolvePathsIdx < resolveFilesIdx && resolveFilesIdx < resolveSourcePathIdx) {
		t.Fatalf("Anaconda role must resolve install files before the effective source path")
	}
	validateSource, ok := topTasks[validateSourceIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an assert task", topTasks[validateSourceIdx]["name"])
	}
	if got := fmt.Sprint(validateSource["that"]); !strings.Contains(got, "mediaType") || !strings.Contains(got, "installer.sourceURL") || !strings.Contains(got, "installer.rhsm.enabled") {
		t.Fatalf("Anaconda install source validation must reject boot media without sourceURL or RHSM, got %v", validateSource["that"])
	}
	resolveFiles, ok := topTasks[resolveFilesIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", topTasks[resolveFilesIdx]["name"])
	}
	for _, want := range []string{"bootwright_os_source_iso", "bootwright_os_source_id_path", "bootwright_os_install_iso", "bootwright_os_install_tmpdir"} {
		if _, ok := resolveFiles[want]; !ok {
			t.Fatalf("%s missing %s", topTasks[resolveFilesIdx]["name"], want)
		}
	}
	resolveSourcePath, ok := topTasks[resolveSourcePathIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", topTasks[resolveSourcePathIdx]["name"])
	}
	sourcePathExpr := fmt.Sprint(resolveSourcePath["bootwright_os_source_iso_effective"])
	for _, want := range []string{"sourceOnTarget", "bootwright_component.osInstall.image.path", "bootwright_os_source_iso"} {
		if !strings.Contains(sourcePathExpr, want) {
			t.Fatalf("%s effective source path missing %q: %s", topTasks[resolveSourcePathIdx]["name"], want, sourcePathExpr)
		}
	}

	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Install managed OS from virtual media")], "block")
	loadIdx := findAnsibleTask(t, tasks, "Load Anaconda package list")
	installIdx := findAnsibleTask(t, tasks, "Install Anaconda ISO tooling packages")
	verifyIdx := findAnsibleTask(t, tasks, "Verify mkksiso is available")
	if !(loadIdx < installIdx && installIdx < verifyIdx) {
		t.Fatalf("Anaconda role must install mkksiso packages before verifying mkksiso")
	}
	pkg, ok := tasks[installIdx]["ansible.builtin.package"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a package task", tasks[installIdx]["name"])
	}
	if got, _ := pkg["name"].(string); got != "{{ bootwright_machine_os_install_anaconda_packages }}" {
		t.Fatalf("%s package name got %q", tasks[installIdx]["name"], got)
	}
	copyIdx := findAnsibleTask(t, tasks, "Copy managed ISO source to provider host")
	copyTask, ok := tasks[copyIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a copy task", tasks[copyIdx]["name"])
	}
	if got := copyTask["remote_src"]; got != false {
		t.Fatalf("%s must not remote-copy provider-local sources, got remote_src=%v", tasks[copyIdx]["name"], got)
	}
	if got := tasks[copyIdx]["when"]; !stringListContains(got, "bootwright_component.osInstall.image.kind in ['media', 'file']") || !stringListContains(got, "not (bootwright_component.osInstall.image.sourceOnTarget | default(false) | bool)") {
		t.Fatalf("%s must skip sources already present on the provider host, got when=%v", tasks[copyIdx]["name"], got)
	}
	downloadIdx := findAnsibleTask(t, tasks, "Download managed ISO source on provider host")
	sourceStatIdx := findAnsibleTask(t, tasks, "Stat managed ISO source")
	sourceIdentityIdx := findAnsibleTask(t, tasks, "Record managed ISO source identity")
	checksumIdx := findAnsibleTask(t, tasks, "Verify managed ISO checksum")
	statISOIdx := findAnsibleTask(t, tasks, "Stat managed OS install ISO")
	rebuildStateIdx := findAnsibleTask(t, tasks, "Resolve managed OS install ISO rebuild state")
	removeStaleIdx := findAnsibleTask(t, tasks, "Remove stale managed OS install ISO before rebuild")
	resetTmpIdx := findAnsibleTask(t, tasks, "Reset managed OS install ISO temp directory before rebuild")
	createTmpIdx := findAnsibleTask(t, tasks, "Create managed OS install ISO temp directory")
	buildISOIdx := findAnsibleTask(t, tasks, "Build managed OS install ISO")
	if !(copyIdx < sourceStatIdx && downloadIdx < sourceStatIdx && sourceStatIdx < sourceIdentityIdx && sourceIdentityIdx < checksumIdx && sourceIdentityIdx < statISOIdx) {
		t.Fatalf("Anaconda role must resolve source metadata before checksum and rebuild decisions")
	}
	sourceStat, ok := tasks[sourceStatIdx]["ansible.builtin.stat"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a stat task", tasks[sourceStatIdx]["name"])
	}
	if sourceStat["path"] != "{{ bootwright_os_source_iso_effective }}" || sourceStat["get_checksum"] != false {
		t.Fatalf("%s must stat the effective source without checksumming it, got %v", tasks[sourceStatIdx]["name"], sourceStat)
	}
	sourceIdentity, ok := tasks[sourceIdentityIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a copy task", tasks[sourceIdentityIdx]["name"])
	}
	sourceIdentityContent := fmt.Sprint(sourceIdentity["content"])
	for _, want := range []string{"bootwright_os_source_iso_effective", "bootwright_os_source_stat.stat.size", "bootwright_os_source_stat.stat.mtime"} {
		if !strings.Contains(sourceIdentityContent, want) {
			t.Fatalf("%s source identity missing %q: %s", tasks[sourceIdentityIdx]["name"], want, sourceIdentityContent)
		}
	}
	checksumCommand, ok := tasks[checksumIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", tasks[checksumIdx]["name"])
	}
	if got := fmt.Sprint(checksumCommand["argv"]); !strings.Contains(got, "bootwright_os_source_iso_effective") {
		t.Fatalf("%s must checksum the effective source ISO, got argv=%v", tasks[checksumIdx]["name"], checksumCommand["argv"])
	}
	statISO, ok := tasks[statISOIdx]["ansible.builtin.stat"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a stat task", tasks[statISOIdx]["name"])
	}
	if statISO["path"] != "{{ bootwright_os_install_iso }}" || statISO["get_checksum"] != false {
		t.Fatalf("%s must stat install.iso without checksumming it, got %v", tasks[statISOIdx]["name"], statISO)
	}
	if !(statISOIdx < rebuildStateIdx && rebuildStateIdx < removeStaleIdx && removeStaleIdx < resetTmpIdx && resetTmpIdx < createTmpIdx && createTmpIdx < buildISOIdx) {
		t.Fatalf("Anaconda role must remove stale install.iso and reset temp state before mkksiso")
	}
	rebuildFact, ok := tasks[rebuildStateIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", tasks[rebuildStateIdx]["name"])
	}
	rebuildExpr := fmt.Sprint(rebuildFact["bootwright_os_install_iso_rebuild_needed"])
	for _, want := range []string{
		"bootwright_os_source_copy.changed",
		"bootwright_os_source_download.changed",
		"bootwright_os_source_identity.changed",
		"bootwright_os_kickstart.changed",
		"bootwright_os_install_iso_stat.stat.exists",
	} {
		if !strings.Contains(rebuildExpr, want) {
			t.Fatalf("%s rebuild expression missing %q: %s", tasks[rebuildStateIdx]["name"], want, rebuildExpr)
		}
	}
	if got := tasks[rebuildStateIdx]["changed_when"]; got != false {
		t.Fatalf("%s must not report changes, got %v", tasks[rebuildStateIdx]["name"], got)
	}
	removeStale, ok := tasks[removeStaleIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a file task", tasks[removeStaleIdx]["name"])
	}
	if removeStale["path"] != "{{ bootwright_os_install_iso }}" || removeStale["state"] != "absent" {
		t.Fatalf("%s must remove bootwright_os_install_iso, got %v", tasks[removeStaleIdx]["name"], removeStale)
	}
	if got := tasks[removeStaleIdx]["when"]; !stringListContains(got, "bootwright_os_install_iso_rebuild_needed | bool") || !stringListContains(got, "bootwright_os_install_iso_stat.stat.exists | default(false)") {
		t.Fatalf("%s must only remove an existing ISO when rebuild is needed, got when=%v", tasks[removeStaleIdx]["name"], got)
	}
	if got := tasks[buildISOIdx]["when"]; got != "bootwright_os_install_iso_rebuild_needed | bool" {
		t.Fatalf("%s must use resolved rebuild state, got when=%v", tasks[buildISOIdx]["name"], got)
	}
	buildCommand, ok := tasks[buildISOIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", tasks[buildISOIdx]["name"])
	}
	if got := fmt.Sprint(buildCommand["argv"]); !strings.Contains(got, "bootwright_os_source_iso_effective") {
		t.Fatalf("%s must use the effective source ISO, got argv=%v", tasks[buildISOIdx]["name"], buildCommand["argv"])
	}
	buildEnv, ok := tasks[buildISOIdx]["environment"].(map[string]any)
	if !ok {
		t.Fatalf("%s must set mkksiso temp environment", tasks[buildISOIdx]["name"])
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if got := buildEnv[key]; got != "{{ bootwright_os_install_tmpdir }}" {
			t.Fatalf("%s environment %s got %v, want bootwright_os_install_tmpdir", tasks[buildISOIdx]["name"], key, got)
		}
	}
	if findAnsibleTaskIndex(tasks, "Stage managed OS install ISO for virtual media") >= 0 {
		t.Fatalf("Anaconda role must not stage install.iso with ansible.builtin.copy")
	}
	stageSourceStatIdx := findAnsibleTask(t, tasks, "Stat managed OS install ISO for virtual media stage")
	stageStatIdx := findAnsibleTask(t, tasks, "Stat managed OS staged virtual media ISO")
	stageStateIdx := findAnsibleTask(t, tasks, "Resolve managed OS virtual media stage state")
	stageLinkIdx := findAnsibleTask(t, tasks, "Link managed OS install ISO into virtual media stage")
	stageCopyIdx := findAnsibleTask(t, tasks, "Copy managed OS install ISO into virtual media stage when linking is unsupported")
	stagePermsIdx := findAnsibleTask(t, tasks, "Set managed OS virtual media stage permissions")
	if !(stageSourceStatIdx < stageStatIdx && stageStatIdx < stageStateIdx && stageStateIdx < stageLinkIdx && stageLinkIdx < stageCopyIdx && stageCopyIdx < stagePermsIdx) {
		t.Fatalf("Anaconda role must hardlink stage ISO before falling back to reflink copy")
	}
	stageSourceStat, ok := tasks[stageSourceStatIdx]["ansible.builtin.stat"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a stat task", tasks[stageSourceStatIdx]["name"])
	}
	if stageSourceStat["path"] != "{{ bootwright_os_install_iso }}" || stageSourceStat["get_checksum"] != false {
		t.Fatalf("%s must stat install.iso without checksumming it, got %v", tasks[stageSourceStatIdx]["name"], stageSourceStat)
	}
	stageLink, ok := tasks[stageLinkIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", tasks[stageLinkIdx]["name"])
	}
	if got := fmt.Sprint(stageLink["argv"]); !strings.Contains(got, "ln") || !strings.Contains(got, "-f") || !strings.Contains(got, "bootwright_os_install_iso") || !strings.Contains(got, "bootwright_os_stage_path") {
		t.Fatalf("%s must hardlink install.iso into the stage path, got argv=%v", tasks[stageLinkIdx]["name"], stageLink["argv"])
	}
	if got := tasks[stageLinkIdx]["failed_when"]; got != false {
		t.Fatalf("%s must allow fallback after hardlink failure, got failed_when=%v", tasks[stageLinkIdx]["name"], got)
	}
	stageCopy, ok := tasks[stageCopyIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", tasks[stageCopyIdx]["name"])
	}
	for _, want := range []string{"cp", "--reflink=auto", "--remove-destination", "bootwright_os_install_iso", "bootwright_os_stage_path"} {
		if got := fmt.Sprint(stageCopy["argv"]); !strings.Contains(got, want) {
			t.Fatalf("%s fallback copy missing %q, got argv=%v", tasks[stageCopyIdx]["name"], want, stageCopy["argv"])
		}
	}
	if got := tasks[stageCopyIdx]["when"]; !stringListContains(got, "not (bootwright_os_stage_iso_linked | bool)") || !stringListContains(got, "(bootwright_os_stage_link.rc | default(1) | int) != 0") {
		t.Fatalf("%s must only copy when hardlinking was needed and failed, got when=%v", tasks[stageCopyIdx]["name"], got)
	}
	redHat := readAnsibleStringListVar(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/vars/os/RedHat.yml", "bootwright_machine_os_install_anaconda_packages")
	assertContainsAll(t, redHat, []string{"lorax"})
}

func TestManagedOSKickstartTemplateKeepsSSHKeyConditionalParseable(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_os_install_anaconda/templates/ks.cfg.j2")
	if strings.Contains(body, "lookup('ansible.builtin.file', ks.sshPublicKeyPath) if") {
		t.Fatalf("Kickstart template must not use an inline conditional around the SSH key lookup")
	}
	for _, want := range []string{
		"{% set rhsm = installer.rhsm | default({}) %}",
		"{% if rhsm.enabled | default(false) %}",
		"rhsm --organization=\"{{ lookup('ansible.builtin.file', rhsm.organizationPath) | trim }}\" --activation-key=\"{{ lookup('ansible.builtin.file', rhsm.activationKeyPath) | trim }}\"",
		"{% elif installer.sourceURL | default('') | length > 0 %}",
		"url --url={{ installer.sourceURL }}",
		"{% else %}",
		"cdrom",
		"{% set ssh_key = '' %}",
		"{% if (ks.authorizeMachineSSHKey | default(false)) and (ks.sshPublicKeyPath | default('') | length > 0) %}",
		"{% set ssh_key = lookup('ansible.builtin.file', ks.sshPublicKeyPath) %}",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Kickstart template missing %q", want)
		}
	}
}

func TestBootAgentMachinePlaybookUsesAgentNodeFanout(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_container_cluster_boot_agent_machine.yml")
	if len(plays) != 1 {
		t.Fatalf("boot-agent-machine play count = %d, want 1", len(plays))
	}
	play := plays[0]
	if got := play["hosts"]; got != "bootwright_agent_node_hosts" {
		t.Fatalf("boot-agent-machine hosts = %v, want bootwright_agent_node_hosts", got)
	}
	rawTasks, ok := play["tasks"].([]any)
	if !ok {
		t.Fatalf("boot-agent-machine play missing tasks: %v", play)
	}
	tasks := make([]map[string]any, 0, len(rawTasks))
	for _, raw := range rawTasks {
		task, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("task is not a map: %v", raw)
		}
		tasks = append(tasks, task)
	}
	resolveIdx := findAnsibleTask(t, tasks, "Resolve selected cluster")
	bootIdx := findAnsibleTask(t, tasks, "Boot selected agent machine")
	if !(resolveIdx < bootIdx) {
		t.Fatalf("boot playbook must resolve the per-node cluster before including install_agent")
	}
	if _, ok := tasks[bootIdx]["loop"]; ok {
		t.Fatalf("boot playbook must not loop serially over nodes: %v", tasks[bootIdx])
	}
	assertIncludeRoleName(t, tasks[bootIdx], "bootwright.core.container_cluster_agent_install")
}

func TestBootRedfishSSHAuthProbeUsesCallerDuringInternalSudo(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/post_boot.yml")
	contextIdx := findAnsibleTask(t, tasks, "Resolve node SSH auth probe key execution context")
	prefixIdx := findAnsibleTask(t, tasks, "Resolve node SSH auth probe key caller command prefix")
	checkIdx := findAnsibleTask(t, tasks, "Check node SSH auth probe key")
	authIdx := findAnsibleTask(t, tasks, "Wait for node SSH to accept configured key")
	if !(contextIdx < prefixIdx && prefixIdx < checkIdx && checkIdx < authIdx) {
		t.Fatalf("SSH auth probe command prefix must resolve before key access tasks")
	}

	setFact, ok := tasks[contextIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("execution context task is not set_fact: %v", tasks[contextIdx])
	}
	expr := fmt.Sprint(setFact["bootwright_node_ready_ssh_key_exec_user"])
	for _, want := range []string{"BOOTWRIGHT_INTERNAL_LOCAL_ROOT", "SUDO_UID", "'#'"} {
		if !strings.Contains(expr, want) {
			t.Fatalf("execution user expression missing %q: %s", want, expr)
		}
	}
	prefixFact, ok := tasks[prefixIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("command prefix task is not set_fact: %v", tasks[prefixIdx])
	}
	prefix, ok := prefixFact["bootwright_node_ready_ssh_key_command_prefix"].([]any)
	if !ok {
		t.Fatalf("command prefix is not a list: %v", prefixFact)
	}
	for _, want := range []string{"sudo", "-n", "-u", "{{ bootwright_node_ready_ssh_key_exec_user }}", "--"} {
		if !slices.Contains(prefix, any(want)) {
			t.Fatalf("command prefix missing %q: %v", want, prefix)
		}
	}
	checkCommand, ok := tasks[checkIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("key check task is not command: %v", tasks[checkIdx])
	}
	checkArgv := fmt.Sprint(checkCommand["argv"])
	for _, want := range []string{"bootwright_node_ready_ssh_key_command_prefix", "'test'", "'-f'", "bootwright_node_ready_ssh_key_path"} {
		if !strings.Contains(checkArgv, want) {
			t.Fatalf("key check argv missing %q: %s", want, checkArgv)
		}
	}
	authCommand, ok := tasks[authIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("SSH auth task is not command: %v", tasks[authIdx])
	}
	authArgv := fmt.Sprint(authCommand["argv"])
	for _, want := range []string{"bootwright_node_ready_ssh_key_command_prefix", "'ssh'", "bootwright_node_ready_ssh_key_path"} {
		if !strings.Contains(authArgv, want) {
			t.Fatalf("SSH auth argv missing %q: %s", want, authArgv)
		}
	}
	for _, task := range []map[string]any{tasks[checkIdx], tasks[authIdx]} {
		if _, ok := task["become_user"]; ok {
			t.Fatalf("%s must not use Ansible become_user for caller key access", task["name"])
		}
	}
}

func TestBootRedfishDispatchesMediaBackendBeforeInsert(t *testing.T) {
	mainTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml")
	prepareIdx := findAnsibleTask(t, mainTasks, "Prepare Redfish virtual media")
	validateMACsIdx := findAnsibleTask(t, mainTasks, "Validate declared MACs against Redfish inventory")
	bootSequenceIdx := findAnsibleTask(t, mainTasks, "Boot node from Redfish virtual media")
	bootSequenceTasks := nestedAnsibleTasks(t, mainTasks[bootSequenceIdx], "block")
	bootSequenceAlways := nestedAnsibleTasks(t, mainTasks[bootSequenceIdx], "always")
	powerIdx := findAnsibleTask(t, bootSequenceTasks, "Power node from virtual media")
	postIdx := findAnsibleTask(t, bootSequenceTasks, "Set post-boot Redfish boot device")
	restoreIdx := findAnsibleTask(t, bootSequenceAlways, "Restore Redfish certificate verification settings")
	if !(prepareIdx < validateMACsIdx && validateMACsIdx < bootSequenceIdx) {
		t.Fatalf("boot_redfish imports must run media_prepare and MAC validation before boot sequence")
	}
	if !(powerIdx < postIdx) {
		t.Fatalf("boot_redfish boot sequence must run power before post_boot")
	}
	for _, want := range []string{
		"bootwright_redfish_action_effective == 'boot'",
		"bootwright_component.boot.redfish.setBootSource | default(true) | bool",
		"(bootwright_component.interfaces | default([]) | length) > 0",
	} {
		if !stringListContains(mainTasks[validateMACsIdx]["when"], want) {
			t.Fatalf("boot_redfish MAC validation when missing %q: %v", want, mainTasks[validateMACsIdx]["when"])
		}
	}
	if got := mainTasks[bootSequenceIdx]["when"]; got != "bootwright_redfish_action_effective == 'boot'" {
		t.Fatalf("boot_redfish boot sequence must only run for boot action, got when=%v", got)
	}
	assertIncludeTasksFile(t, bootSequenceTasks[powerIdx], "boot/power.yml")
	assertIncludeTasksFile(t, bootSequenceTasks[postIdx], "boot/post_boot.yml")
	assertIncludeTasksFile(t, bootSequenceAlways[restoreIdx], "media/restore_certificate_verification.yml")
	if len(bootSequenceAlways) != 1 {
		t.Fatalf("boot_redfish boot sequence always block should only restore Redfish certificate settings, got %d tasks", len(bootSequenceAlways))
	}

	prepareTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml")
	mediaTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/main.yml")
	powerTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power.yml")
	powerStateTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_state_probe.yml")
	insertAttemptTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml")
	ejectTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/eject.yml")
	postTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/post_boot.yml")
	restoreTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/restore_certificate_verification.yml")
	managerListIdx := findAnsibleTask(t, prepareTasks, "List Redfish managers")
	managerMediaIdx := findAnsibleTask(t, prepareTasks, "List VirtualMedia members for Redfish managers")
	probeMediaIdx := findAnsibleTask(t, prepareTasks, "Probe Redfish VirtualMedia members")
	confirmMediaCandidatesIdx := findAnsibleTask(t, prepareTasks, "Confirm Redfish VirtualMedia candidates were found")
	resolveMediaIdx := findAnsibleTask(t, prepareTasks, "Resolve VirtualMedia member")
	resolveManagerIdx := findAnsibleTask(t, prepareTasks, "Resolve Redfish manager member for VirtualMedia")
	resolveSecurityServiceIdx := findAnsibleTask(t, prepareTasks, "Resolve Redfish manager SecurityService member")
	resolveActionIdx := findAnsibleTask(t, prepareTasks, "Resolve VirtualMedia action URLs")
	resolveActionCandidatesIdx := findAnsibleTask(t, prepareTasks, "Resolve VirtualMedia standard and VMM action candidates")
	actionInfoIdx := findAnsibleTask(t, prepareTasks, "Probe Redfish VMM ActionInfo")
	supportedVMMIdx := findAnsibleTask(t, prepareTasks, "Resolve supported Redfish VMM actions")
	effectiveActionIdx := findAnsibleTask(t, prepareTasks, "Resolve effective VirtualMedia action targets")
	redfishEjectIdx := findAnsibleTask(t, prepareTasks, "Eject any previously inserted virtual media (idempotent)")
	mediaPrepareIdx := findAnsibleTask(t, prepareTasks, "Run virtual-media backend preparation")
	preLiveIdx := findAnsibleTask(t, mediaTasks, "Clean stale running virtual media before insert")
	preConfigIdx := findAnsibleTask(t, mediaTasks, "Clean stale persistent virtual media before insert")
	protocolIdx := findAnsibleTask(t, powerTasks, "Resolve virtual media transfer protocol")
	initInsertIdx := findAnsibleTask(t, powerTasks, "Initialize Redfish virtual media insertion status")
	securityRefreshIdx := findAnsibleTask(t, powerTasks, "Refresh Redfish manager SecurityService for HTTPS file transfer")
	securityResolveIdx := findAnsibleTask(t, powerTasks, "Resolve Redfish HTTPS transfer certificate verification setting")
	securityPatchIdx := findAnsibleTask(t, powerTasks, "Disable HTTPS transfer certificate verification for BMC media fetch")
	securityCaptureIdx := findAnsibleTask(t, powerTasks, "Capture Redfish HTTPS transfer certificate verification status")
	retryInsertIdx := findAnsibleTask(t, powerTasks, "Retry Redfish virtual media insertion until attached")
	confirmMediaIdx := findAnsibleTask(t, powerTasks, "Confirm agent ISO is attached as virtual media")
	systemRefreshIdx := findAnsibleTask(t, powerTasks, "Refresh Redfish system metadata before reset and boot override")
	systemPreconditionIdx := findAnsibleTask(t, powerTasks, "Resolve Redfish system PATCH precondition")
	resetActionsIdx := findAnsibleTask(t, powerTasks, "Resolve Redfish reset action metadata")
	resetTargetIdx := findAnsibleTask(t, powerTasks, "Resolve Redfish reset action target")
	resetActionInfoIdx := findAnsibleTask(t, powerTasks, "Probe Redfish Reset ActionInfo")
	resolvePowerOnResetTypeIdx := findAnsibleTask(t, powerTasks, "Resolve Redfish power-on reset type")
	cdBootIdx := findAnsibleTask(t, powerTasks, "Set one-time boot to CD")
	confirmCDBootIdx := findAnsibleTask(t, powerTasks, "Confirm one-time CD boot override was accepted")
	forceOffIdx := findAnsibleTask(t, powerTasks, "Force power off (tolerate already-off)")
	initPowerOffIdx := findAnsibleTask(t, powerTasks, "Initialize Redfish power state wait for PowerState=Off")
	waitPowerOffIdx := findAnsibleTask(t, powerTasks, "Wait for BMC to report PowerState=Off")
	confirmPowerOffIdx := findAnsibleTask(t, powerTasks, "Confirm BMC reports PowerState=Off before power on")
	powerOnIdx := findAnsibleTask(t, powerTasks, "Power on")
	confirmPowerOnRequestIdx := findAnsibleTask(t, powerTasks, "Confirm Redfish power-on request can be verified")
	initPowerOnIdx := findAnsibleTask(t, powerTasks, "Initialize Redfish power state wait for PowerState=On")
	waitPowerOnIdx := findAnsibleTask(t, powerTasks, "Wait for BMC to report PowerState=On")
	confirmPowerOnIdx := findAnsibleTask(t, powerTasks, "Confirm BMC reports PowerState=On")
	powerStateProbeIdx := findAnsibleTask(t, powerStateTasks, "Probe Redfish system power state")
	powerStateCaptureIdx := findAnsibleTask(t, powerStateTasks, "Capture Redfish system power state status")
	powerStateResolveIdx := findAnsibleTask(t, powerStateTasks, "Resolve Redfish system power state result")
	powerStateDelayIdx := findAnsibleTask(t, powerStateTasks, "Wait before retrying Redfish power state probe")
	retryDelayIdx := findAnsibleTask(t, insertAttemptTasks, "Wait before retrying Redfish virtual media insertion")
	retryEjectIdx := findAnsibleTask(t, insertAttemptTasks, "Eject virtual media before retrying insertion")
	refreshMediaIdx := findAnsibleTask(t, insertAttemptTasks, "Refresh virtual media metadata before PATCH operations")
	mediaPreconditionIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve virtual media PATCH precondition")
	verifyCertIdx := findAnsibleTask(t, insertAttemptTasks, "Disable HTTPS certificate verification for virtual-media fetch")
	standardBodyIdx := findAnsibleTask(t, insertAttemptTasks, "Build standard Redfish InsertMedia request body")
	vmmBodyIdx := findAnsibleTask(t, insertAttemptTasks, "Build Redfish VMM virtual media request body")
	insertIdx := findAnsibleTask(t, insertAttemptTasks, "Insert the agent ISO as virtual media")
	requestStatusIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve Redfish InsertMedia request status")
	taskRefIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve Redfish InsertMedia task reference")
	taskURLIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve Redfish InsertMedia task URL")
	waitTaskIdx := findAnsibleTask(t, insertAttemptTasks, "Wait for Redfish InsertMedia task to complete")
	captureTaskIdx := findAnsibleTask(t, insertAttemptTasks, "Capture Redfish InsertMedia task status")
	taskResultIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve Redfish InsertMedia task result")
	failedTaskProbeIdx := findAnsibleTask(t, insertAttemptTasks, "Probe virtual media after failed InsertMedia task")
	mountedTaskProbeIdx := findAnsibleTask(t, insertAttemptTasks, "Probe virtual media after mounted InsertMedia task")
	waitMediaIdx := findAnsibleTask(t, insertAttemptTasks, "Wait for virtual media to report inserted agent ISO")
	resolveProbeAfterInsertIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve virtual media probe after InsertMedia")
	patchPreconditionIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve virtual media PATCH fallback precondition")
	patchAttemptIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve virtual media PATCH fallback attempt")
	patchMediaIdx := findAnsibleTask(t, insertAttemptTasks, "Patch virtual media attachment when InsertMedia action is not reflected")
	waitPatchMediaIdx := findAnsibleTask(t, insertAttemptTasks, "Wait for virtual media to report inserted agent ISO after PATCH fallback")
	resolveProbeAfterPatchIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve virtual media probe after PATCH fallback")
	captureMediaIdx := findAnsibleTask(t, insertAttemptTasks, "Capture virtual media attachment status")
	resolveAttachmentSourcesIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve virtual media attachment sources")
	resolveAttachedIdx := findAnsibleTask(t, insertAttemptTasks, "Resolve virtual media attachment result")
	waitSSHIdx := findAnsibleTask(t, postTasks, "Wait for node SSH to confirm live ISO boot complete")
	sshAuthIdx := findAnsibleTask(t, postTasks, "Wait for node SSH to accept configured key")
	sshAuthConfirmIdx := findAnsibleTask(t, postTasks, "Confirm node SSH accepted configured key")
	diskBootRefreshIdx := findAnsibleTask(t, postTasks, "Refresh Redfish system metadata before disk boot override")
	diskBootPreconditionIdx := findAnsibleTask(t, postTasks, "Resolve Redfish system PATCH precondition")
	diskBootIdx := findAnsibleTask(t, postTasks, "Set subsequent boots to disk after live ISO boot")
	diskBootConfirmIdx := findAnsibleTask(t, postTasks, "Confirm subsequent disk boot override was accepted")
	restoreVMediaRefreshIdx := findAnsibleTask(t, restoreTasks, "Refresh virtual media metadata before restoring fetch certificate verification")
	restoreVMediaPreconditionIdx := findAnsibleTask(t, restoreTasks, "Resolve virtual media restore PATCH precondition")
	restoreVMediaIdx := findAnsibleTask(t, restoreTasks, "Restore virtual media fetch certificate verification")
	restoreSecurityRefreshIdx := findAnsibleTask(t, restoreTasks, "Refresh Redfish manager SecurityService before restoring HTTPS transfer certificate verification")
	restoreSecurityPreconditionIdx := findAnsibleTask(t, restoreTasks, "Resolve Redfish manager SecurityService restore PATCH precondition")
	restoreSecurityIdx := findAnsibleTask(t, restoreTasks, "Restore Redfish HTTPS transfer certificate verification")

	if !(managerListIdx < managerMediaIdx && managerMediaIdx < probeMediaIdx && probeMediaIdx < resolveMediaIdx && resolveMediaIdx < resolveManagerIdx && resolveManagerIdx < resolveSecurityServiceIdx && resolveSecurityServiceIdx < resolveActionIdx && resolveActionIdx < resolveActionCandidatesIdx && resolveActionCandidatesIdx < actionInfoIdx && actionInfoIdx < supportedVMMIdx && supportedVMMIdx < effectiveActionIdx && effectiveActionIdx < redfishEjectIdx && redfishEjectIdx < mediaPrepareIdx) {
		t.Fatalf("boot_redfish must discover manager-scoped virtual media and action targets before eject/prep")
	}
	if !(probeMediaIdx < confirmMediaCandidatesIdx && confirmMediaCandidatesIdx < resolveMediaIdx) {
		t.Fatalf("boot_redfish must report failed VirtualMedia discovery before selecting media")
	}
	if !(redfishEjectIdx < mediaPrepareIdx) {
		t.Fatalf("media backend preparation must run after Redfish eject")
	}
	if !(preLiveIdx < preConfigIdx) {
		t.Fatalf("media cleanup must process running state before persistent state")
	}
	if !(protocolIdx < initInsertIdx && initInsertIdx < securityRefreshIdx && securityRefreshIdx < securityResolveIdx && securityResolveIdx < securityPatchIdx && securityPatchIdx < securityCaptureIdx && securityCaptureIdx < retryInsertIdx && retryInsertIdx < confirmMediaIdx && confirmMediaIdx < systemRefreshIdx && systemRefreshIdx < systemPreconditionIdx && systemPreconditionIdx < resetActionsIdx && resetActionsIdx < resetTargetIdx && resetTargetIdx < resetActionInfoIdx && resetActionInfoIdx < resolvePowerOnResetTypeIdx && resolvePowerOnResetTypeIdx < cdBootIdx && cdBootIdx < confirmCDBootIdx) {
		t.Fatalf("boot_redfish must retry virtual media insertion before setting CD boot")
	}
	if !(confirmCDBootIdx < forceOffIdx && forceOffIdx < initPowerOffIdx && initPowerOffIdx < waitPowerOffIdx && waitPowerOffIdx < confirmPowerOffIdx && confirmPowerOffIdx < powerOnIdx && powerOnIdx < confirmPowerOnRequestIdx && confirmPowerOnRequestIdx < initPowerOnIdx && initPowerOnIdx < waitPowerOnIdx && waitPowerOnIdx < confirmPowerOnIdx) {
		t.Fatalf("boot_redfish must wait for power-off before power-on and then confirm PowerState=On")
	}
	if !(powerStateProbeIdx < powerStateCaptureIdx && powerStateCaptureIdx < powerStateResolveIdx && powerStateResolveIdx < powerStateDelayIdx) {
		t.Fatalf("boot_redfish power state probe must capture sanitized status before resolving retry state")
	}
	if !(retryDelayIdx < retryEjectIdx && retryEjectIdx < refreshMediaIdx && refreshMediaIdx < mediaPreconditionIdx && mediaPreconditionIdx < verifyCertIdx && verifyCertIdx < standardBodyIdx && standardBodyIdx < vmmBodyIdx && vmmBodyIdx < insertIdx && insertIdx < requestStatusIdx && requestStatusIdx < taskRefIdx && taskRefIdx < taskURLIdx && taskURLIdx < waitTaskIdx && waitTaskIdx < captureTaskIdx && captureTaskIdx < taskResultIdx && taskResultIdx < failedTaskProbeIdx && failedTaskProbeIdx < mountedTaskProbeIdx && mountedTaskProbeIdx < waitMediaIdx && waitMediaIdx < resolveProbeAfterInsertIdx && resolveProbeAfterInsertIdx < patchPreconditionIdx && patchPreconditionIdx < patchAttemptIdx && patchAttemptIdx < patchMediaIdx && patchMediaIdx < waitPatchMediaIdx && waitPatchMediaIdx < resolveProbeAfterPatchIdx && resolveProbeAfterPatchIdx < captureMediaIdx && captureMediaIdx < resolveAttachmentSourcesIdx && resolveAttachmentSourcesIdx < resolveAttachedIdx) {
		t.Fatalf("boot_redfish insert attempt must verify async task and virtual media insertion before reporting success")
	}
	if !(waitSSHIdx < sshAuthIdx && sshAuthIdx < sshAuthConfirmIdx && sshAuthConfirmIdx < diskBootRefreshIdx && diskBootRefreshIdx < diskBootPreconditionIdx && diskBootPreconditionIdx < diskBootIdx && diskBootIdx < diskBootConfirmIdx) {
		t.Fatalf("boot_redfish must verify SSH auth before switching subsequent boots back to disk")
	}
	if !(restoreVMediaRefreshIdx < restoreVMediaPreconditionIdx && restoreVMediaPreconditionIdx < restoreVMediaIdx && restoreVMediaIdx < restoreSecurityRefreshIdx && restoreSecurityRefreshIdx < restoreSecurityPreconditionIdx && restoreSecurityPreconditionIdx < restoreSecurityIdx) {
		t.Fatalf("boot_redfish restore tasks must restore virtual media then SecurityService certificate verification")
	}

	assertIncludeRoleName(t, prepareTasks[mediaPrepareIdx], "{{ bootwright_component.mediaPrepareRole }}")
	assertIncludeTasksFile(t, mediaTasks[preLiveIdx], "eject.yml")
	assertIncludeTasksFile(t, mediaTasks[preConfigIdx], "eject.yml")
	assertIncludeTasksFile(t, prepareTasks[redfishEjectIdx], "eject.yml")
	assertIncludeTasksFile(t, powerTasks[retryInsertIdx], "../media/insert_attempt.yml")
	assertIncludeTasksApplyWhen(t, powerTasks[retryInsertIdx], "not (bootwright_redfish_vmedia_attached | bool)")
	for _, idx := range []int{waitPowerOffIdx, waitPowerOnIdx} {
		assertIncludeTasksFile(t, powerTasks[idx], "power_state_probe.yml")
		assertIncludeTasksApplyWhen(t, powerTasks[idx], "not (bootwright_redfish_power_state_reached | bool)")
		if got := powerTasks[idx]["when"]; got != "not (bootwright_redfish_power_state_reached | bool)" {
			t.Fatalf("%s must stop once the expected power state is reached, got when=%v", powerTasks[idx]["name"], got)
		}
		if got, ok := powerTasks[idx]["loop"].(string); !ok || !strings.Contains(got, "bootwright_redfish_power_state_retries") {
			t.Fatalf("%s must use configured power-state retries, got loop=%v", powerTasks[idx]["name"], powerTasks[idx]["loop"])
		}
	}
	if got := powerTasks[retryInsertIdx]["when"]; got != "not (bootwright_redfish_vmedia_attached | bool)" {
		t.Fatalf("virtual media retry loop must stop once attached, got when=%v", got)
	}
	if got, ok := powerTasks[retryInsertIdx]["loop"].(string); !ok || !strings.Contains(got, "bootwright_redfish_insert_media_retries") {
		t.Fatalf("virtual media retry loop must use configured retry count, got loop=%v", powerTasks[retryInsertIdx]["loop"])
	}
	assertSleepCommand(t, insertAttemptTasks[retryDelayIdx], "{{ bootwright_redfish_insert_media_retry_delay_seconds }}")
	assertSleepCommand(t, powerStateTasks[powerStateDelayIdx], "{{ bootwright_redfish_power_state_delay_seconds }}")
	resetActionsFact, ok := powerTasks[resetActionsIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", powerTasks[resetActionsIdx]["name"])
	}
	if got := resetActionsFact["bootwright_redfish_system_reset_actions"].(string); !strings.Contains(got, "bootwright_redfish_action_descriptors") || !strings.Contains(got, "#ComputerSystem.Reset") {
		t.Fatalf("reset action metadata must use ComputerSystem.Reset descriptors, got %v", got)
	}
	if got := resetActionsFact["bootwright_redfish_system_default_reset_target"].(string); !strings.Contains(got, "/Actions/ComputerSystem.Reset") {
		t.Fatalf("reset action fallback target must use the standard ComputerSystem.Reset path, got %v", got)
	}
	resetTargetFact, ok := powerTasks[resetTargetIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", powerTasks[resetTargetIdx]["name"])
	}
	resetTarget, ok := resetTargetFact["bootwright_redfish_system_reset_target"].(string)
	if !ok || !strings.Contains(resetTarget, "selectattr('source', 'equalto', 'standard')") || !strings.Contains(resetTarget, "bootwright_redfish_system_default_reset_target") {
		t.Fatalf("reset target must prefer advertised standard action and keep fallback, got %v", resetTarget)
	}
	resetActionInfoURI, ok := powerTasks[resetActionInfoIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", powerTasks[resetActionInfoIdx]["name"])
	}
	if got, ok := resetActionInfoURI["url"].(string); !ok || !strings.Contains(got, "item.actionInfo") {
		t.Fatalf("reset ActionInfo probe must use the advertised ActionInfo URL, got %v", resetActionInfoURI["url"])
	}
	if got, ok := powerTasks[resetActionInfoIdx]["loop"].(string); !ok || !strings.Contains(got, "selectattr('actionInfo', 'defined')") {
		t.Fatalf("reset ActionInfo probe must only fetch actions that advertise ActionInfo, got %v", powerTasks[resetActionInfoIdx]["loop"])
	}
	resetTypeFact, ok := powerTasks[resolvePowerOnResetTypeIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", powerTasks[resolvePowerOnResetTypeIdx]["name"])
	}
	if got := resetTypeFact["bootwright_redfish_power_on_reset_type"].(string); !strings.Contains(got, "bootwright_redfish_power_on_reset_type") || !strings.Contains(got, "bootwright_redfish_reset_action_info_probe") {
		t.Fatalf("power-on reset type must resolve from system metadata and ActionInfo, got %v", got)
	}
	for _, idx := range []int{forceOffIdx, powerOnIdx} {
		resetURI, ok := powerTasks[idx]["ansible.builtin.uri"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no uri body", powerTasks[idx]["name"])
		}
		if got := resetURI["url"]; got != "{{ bootwright_redfish_system_reset_target | bootwright.core.bootwright_redfish_url(bootwright_component.boot.redfish.baseUrl) }}" {
			t.Fatalf("%s must use the resolved Reset action target, got %v", powerTasks[idx]["name"], got)
		}
	}
	powerOnURI, ok := powerTasks[powerOnIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", powerTasks[powerOnIdx]["name"])
	}
	powerOnBody, ok := powerOnURI["body"].(map[string]any)
	if !ok || powerOnBody["ResetType"] != "{{ bootwright_redfish_power_on_reset_type }}" {
		t.Fatalf("power-on body must use resolved ResetType, got %v", powerOnURI["body"])
	}
	if got := powerOnURI["status_code"]; !intListEqual(got, []int{200, 202, 204, 409, 500}) {
		t.Fatalf("Power on status_code got %v, want [200 202 204 409 500]", got)
	}
	if got := powerTasks[powerOnIdx]["failed_when"]; got != false {
		t.Fatalf("Power on must let PowerState polling decide final success, got failed_when=%v", got)
	}
	if got, _ := powerTasks[powerOnIdx]["changed_when"].(string); !strings.Contains(got, "in [200, 202, 204]") {
		t.Fatalf("Power on changed_when got %q, want normal Redfish success codes", got)
	}
	powerOnAssert, ok := powerTasks[confirmPowerOnRequestIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert body", powerTasks[confirmPowerOnRequestIdx]["name"])
	}
	if !strings.Contains(fmt.Sprint(powerOnAssert["that"]), "[200, 202, 204, 409, 500]") {
		t.Fatalf("Power on request assertion must tolerate retriable Redfish statuses, got %v", powerOnAssert["that"])
	}
	if !strings.Contains(powerOnAssert["fail_msg"].(string), "ResetType") || !strings.Contains(powerOnAssert["success_msg"].(string), "ResetType") {
		t.Fatalf("power-on request assertion must report the selected ResetType, got %v", powerOnAssert)
	}
	mediaCandidatesAssert, ok := prepareTasks[confirmMediaCandidatesIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert task", prepareTasks[confirmMediaCandidatesIdx]["name"])
	}
	for _, want := range []string{"System VirtualMedia URL", "bootwright_redfish_system_vmedia_url", "Manager VirtualMedia URLs", "bootwright_redfish_manager_vmedia_urls", "VirtualMedia member URLs", "bootwright_redfish_vmedia_member_urls", "status="} {
		if !strings.Contains(mediaCandidatesAssert["fail_msg"].(string), want) {
			t.Fatalf("VirtualMedia discovery assertion must include %q, got %v", want, mediaCandidatesAssert["fail_msg"])
		}
	}
	securityServiceFact, ok := prepareTasks[resolveSecurityServiceIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", prepareTasks[resolveSecurityServiceIdx]["name"])
	}
	if got := securityServiceFact["bootwright_redfish_security_service_member"].(string); !strings.Contains(got, "bootwright_redfish_manager_member") || !strings.Contains(got, "/SecurityService") {
		t.Fatalf("SecurityService member must derive from resolved manager, got %v", got)
	}
	actionFact, ok := prepareTasks[resolveActionIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", prepareTasks[resolveActionIdx]["name"])
	}
	for _, want := range []string{"#VirtualMedia.VmmControl", "#VirtualMedia.InsertMedia", "#VirtualMedia.EjectMedia"} {
		if !strings.Contains(actionFact["bootwright_redfish_vmedia_insert_actions"].(string)+actionFact["bootwright_redfish_vmedia_eject_actions"].(string)+actionFact["bootwright_redfish_vmedia_vmm_control_actions"].(string), want) {
			t.Fatalf("virtual media action resolver must support %q, got %v", want, actionFact)
		}
	}
	if !strings.Contains(actionFact["bootwright_redfish_vmedia_vmm_control_actions"].(string), "bootwright_redfish_action_descriptors") {
		t.Fatalf("virtual media action resolver must discover VMM actions as descriptors, got %v", actionFact["bootwright_redfish_vmedia_vmm_control_actions"])
	}
	candidateFact, ok := prepareTasks[resolveActionCandidatesIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", prepareTasks[resolveActionCandidatesIdx]["name"])
	}
	if !strings.Contains(candidateFact["bootwright_redfish_vmedia_standard_insert_target"].(string), "selectattr('source', 'equalto', 'standard')") {
		t.Fatalf("standard InsertMedia target must come from standard action descriptors, got %v", candidateFact["bootwright_redfish_vmedia_standard_insert_target"])
	}
	if !strings.Contains(candidateFact["bootwright_redfish_vmedia_vmm_actions"].(string), "bootwright_redfish_vmedia_vmm_control_actions") {
		t.Fatalf("VMM candidates must come from bounded action descriptors, got %v", candidateFact["bootwright_redfish_vmedia_vmm_actions"])
	}
	actionInfoURI, ok := prepareTasks[actionInfoIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", prepareTasks[actionInfoIdx]["name"])
	}
	if got, ok := actionInfoURI["url"].(string); !ok || !strings.Contains(got, "item.actionInfo") {
		t.Fatalf("VMM ActionInfo probe must use the advertised ActionInfo URL, got %v", actionInfoURI["url"])
	}
	if got, ok := prepareTasks[actionInfoIdx]["loop"].(string); !ok || !strings.Contains(got, "selectattr('actionInfo', 'defined')") {
		t.Fatalf("VMM ActionInfo probe must only fetch actions that advertise ActionInfo, got %v", prepareTasks[actionInfoIdx]["loop"])
	}
	supportedVMMFact, ok := prepareTasks[supportedVMMIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", prepareTasks[supportedVMMIdx]["name"])
	}
	if !strings.Contains(supportedVMMFact["bootwright_redfish_vmedia_vmm_supported_actions"].(string), "bootwright_redfish_vmm_control_actions") {
		t.Fatalf("VMM support must be validated from ActionInfo before use, got %v", supportedVMMFact["bootwright_redfish_vmedia_vmm_supported_actions"])
	}
	effectiveActionFact, ok := prepareTasks[effectiveActionIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", prepareTasks[effectiveActionIdx]["name"])
	}
	insertTarget := effectiveActionFact["bootwright_redfish_vmedia_insert_target"].(string)
	standardInsertIdx := strings.Index(insertTarget, "bootwright_redfish_vmedia_standard_insert_target")
	vmmSupportedIdx := strings.Index(insertTarget, "bootwright_redfish_vmedia_vmm_supported_actions")
	if standardInsertIdx < 0 || vmmSupportedIdx < 0 || standardInsertIdx > vmmSupportedIdx {
		t.Fatalf("virtual media action resolver must prefer standard InsertMedia before validated VMM, got %v", insertTarget)
	}
	vmmControl := effectiveActionFact["bootwright_redfish_vmedia_vmm_control"].(string)
	if !strings.Contains(vmmControl, "bootwright_redfish_vmedia_standard_insert_target") || !strings.Contains(vmmControl, "bootwright_redfish_vmedia_vmm_supported_actions") {
		t.Fatalf("virtual media action resolver must enable VMM only without standard InsertMedia, got %v", vmmControl)
	}
	if got := effectiveActionFact["bootwright_redfish_vmedia_action_body_style"]; got != "{{ 'standard-insert-media' if (bootwright_redfish_vmedia_standard_insert_target | length) > 0 else 'vmm-control' if (bootwright_redfish_vmedia_vmm_supported_actions | length) > 0 else 'standard-insert-media' }}" {
		t.Fatalf("virtual media body style must come from the selected action kind, got %v", got)
	}
	if _, ok := insertAttemptTasks[insertIdx]["register"].(string); !ok {
		t.Fatalf("%s must register the InsertMedia response", insertAttemptTasks[insertIdx]["name"])
	}
	if got := insertAttemptTasks[retryEjectIdx]["when"]; got != "(bootwright_redfish_insert_media_attempt | int) > 1" {
		t.Fatalf("retry eject must only run after the first attempt, got when=%v", got)
	}
	verifyCert, ok := insertAttemptTasks[verifyCertIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", insertAttemptTasks[verifyCertIdx]["name"])
	}
	if got := insertAttemptTasks[verifyCertIdx]["when"]; got != "bootwright_redfish_vmedia_transfer_protocol == 'HTTPS'" {
		t.Fatalf("VerifyCertificate patch must only run for HTTPS media, got when=%v", got)
	}
	verifyBody, ok := verifyCert["body"].(map[string]any)
	if !ok || verifyBody["VerifyCertificate"] != false {
		t.Fatalf("VerifyCertificate patch body got %v", verifyCert["body"])
	}
	mediaPreconditionFact, ok := insertAttemptTasks[mediaPreconditionIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[mediaPreconditionIdx]["name"])
	}
	mediaPrecondition, ok := mediaPreconditionFact["bootwright_redfish_vmedia_if_match"].(string)
	if !ok || !strings.Contains(mediaPrecondition, "@odata.etag") || !strings.Contains(mediaPrecondition, "ETag") || !strings.Contains(mediaPrecondition, "bootwright_redfish_vmedia_selected") {
		t.Fatalf("virtual media PATCH precondition must prefer current ETag with selected-resource fallback, got %v", mediaPreconditionFact["bootwright_redfish_vmedia_if_match"])
	}
	assertURIHeader(t, insertAttemptTasks[verifyCertIdx], "If-Match", "{{ bootwright_redfish_vmedia_if_match }}")
	securityRefresh, ok := powerTasks[securityRefreshIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", powerTasks[securityRefreshIdx]["name"])
	}
	if got := securityRefresh["url"]; got != "{{ bootwright_redfish_security_service_member | bootwright.core.bootwright_redfish_url(bootwright_component.boot.redfish.baseUrl) }}" {
		t.Fatalf("SecurityService probe must use discovered manager SecurityService, got %v", got)
	}
	if got := powerTasks[securityRefreshIdx]["when"]; !stringListContains(got, "bootwright_redfish_vmedia_transfer_protocol == 'HTTPS'") || !stringListContains(got, "(bootwright_redfish_security_service_member | default('') | length) > 0") {
		t.Fatalf("SecurityService probe must only run for HTTPS media with a discovered manager, got when=%v", got)
	}
	securityResolveFact, ok := powerTasks[securityResolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", powerTasks[securityResolveIdx]["name"])
	}
	if !strings.Contains(securityResolveFact["bootwright_redfish_security_service_https_transfer_supported"].(string), "HttpsTransferCertVerification") {
		t.Fatalf("SecurityService resolver must discover HttpsTransferCertVerification support, got %v", securityResolveFact["bootwright_redfish_security_service_https_transfer_supported"])
	}
	if !strings.Contains(securityResolveFact["bootwright_redfish_security_service_if_match"].(string), "@odata.etag") || !strings.Contains(securityResolveFact["bootwright_redfish_security_service_if_match"].(string), "ETag") {
		t.Fatalf("SecurityService PATCH precondition must prefer current ETag, got %v", securityResolveFact["bootwright_redfish_security_service_if_match"])
	}
	securityPatch, ok := powerTasks[securityPatchIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", powerTasks[securityPatchIdx]["name"])
	}
	securityPatchBody, ok := securityPatch["body"].(map[string]any)
	if !ok || securityPatchBody["HttpsTransferCertVerification"] != false {
		t.Fatalf("SecurityService PATCH must disable HttpsTransferCertVerification, got %v", securityPatch["body"])
	}
	assertURIHeader(t, powerTasks[securityPatchIdx], "If-Match", "{{ bootwright_redfish_security_service_if_match }}")
	if got := powerTasks[securityPatchIdx]["when"]; !stringListContains(got, "bootwright_redfish_vmedia_transfer_protocol == 'HTTPS'") || !stringListContains(got, "bootwright_redfish_security_service_https_transfer_supported | default(false) | bool") || !stringListContains(got, "bootwright_redfish_security_service_https_transfer_enabled | default(false) | bool") {
		t.Fatalf("SecurityService PATCH must only run when supported and enabled, got when=%v", got)
	}
	powerStateURI, ok := powerStateTasks[powerStateProbeIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", powerStateTasks[powerStateProbeIdx]["name"])
	}
	if got := powerStateTasks[powerStateProbeIdx]["no_log"]; !strings.Contains(fmt.Sprint(got), "bootwright_redfish_cred_path") {
		t.Fatalf("power state probe must hide uri output when credentials are used, got no_log=%v", got)
	}
	if got := powerStateURI["url"]; got != "{{ ('/redfish/v1/Systems/' ~ bootwright_redfish_system_id) | bootwright.core.bootwright_redfish_url(bootwright_component.boot.redfish.baseUrl) }}" {
		t.Fatalf("power state probe must use the resolved system resource, got %v", got)
	}
	if _, ok := powerStateTasks[powerStateProbeIdx]["until"]; ok {
		t.Fatalf("power state probe must not fail with a censored until result")
	}
	powerStateFact, ok := powerStateTasks[powerStateCaptureIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", powerStateTasks[powerStateCaptureIdx]["name"])
	}
	powerStateStatus, ok := powerStateFact["bootwright_redfish_power_state_status"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(powerStateStatus["powerState"]), "PowerState") || !strings.Contains(fmt.Sprint(powerStateStatus["httpStatus"]), "status") {
		t.Fatalf("power state status must capture sanitized PowerState and HTTP status, got %v", powerStateFact)
	}
	restoreVMediaURI, ok := restoreTasks[restoreVMediaIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", restoreTasks[restoreVMediaIdx]["name"])
	}
	restoreVMediaBody, ok := restoreVMediaURI["body"].(map[string]any)
	if !ok || restoreVMediaBody["VerifyCertificate"] != true {
		t.Fatalf("virtual media restore PATCH must re-enable VerifyCertificate, got %v", restoreVMediaURI["body"])
	}
	assertURIHeader(t, restoreTasks[restoreVMediaIdx], "If-Match", "{{ bootwright_redfish_vmedia_restore_if_match }}")
	if got := restoreTasks[restoreVMediaRefreshIdx]["when"]; !stringListContains(got, "(bootwright_redfish_vmedia_transfer_protocol | default('')) == 'HTTPS'") || !stringListContains(got, "bootwright_redfish_vmedia_verify_certificate_patch is defined") || !stringListContains(got, "(bootwright_redfish_vmedia_verify_certificate_patch.status | default(0) | int) in [200, 202, 204]") {
		t.Fatalf("virtual media restore probe must only run after a successful disable PATCH, got when=%v", got)
	}
	restoreSecurityURI, ok := restoreTasks[restoreSecurityIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", restoreTasks[restoreSecurityIdx]["name"])
	}
	restoreSecurityBody, ok := restoreSecurityURI["body"].(map[string]any)
	if !ok || restoreSecurityBody["HttpsTransferCertVerification"] != true {
		t.Fatalf("SecurityService restore PATCH must re-enable HttpsTransferCertVerification, got %v", restoreSecurityURI["body"])
	}
	assertURIHeader(t, restoreTasks[restoreSecurityIdx], "If-Match", "{{ bootwright_redfish_security_service_restore_if_match }}")
	if got := restoreTasks[restoreSecurityRefreshIdx]["when"]; !stringListContains(got, "(bootwright_redfish_vmedia_transfer_protocol | default('')) == 'HTTPS'") || !stringListContains(got, "bootwright_redfish_security_service_https_transfer_patch is defined") || !stringListContains(got, "(bootwright_redfish_security_service_https_transfer_patch.status | default(0) | int) in [200, 202, 204]") {
		t.Fatalf("SecurityService restore probe must only run after a successful disable PATCH, got when=%v", got)
	}
	securityCaptureFact, ok := powerTasks[securityCaptureIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", powerTasks[securityCaptureIdx]["name"])
	}
	securityStatus, ok := securityCaptureFact["bootwright_redfish_security_service_status"].(map[string]any)
	if !ok || !strings.Contains(securityStatus["httpsTransferCertVerificationPatchStatus"].(string), "bootwright_redfish_security_service_https_transfer_patch") {
		t.Fatalf("SecurityService status must capture transfer cert verification PATCH result, got %v", securityCaptureFact["bootwright_redfish_security_service_status"])
	}
	standardBodyFact, ok := insertAttemptTasks[standardBodyIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[standardBodyIdx]["name"])
	}
	standardBody, ok := standardBodyFact["bootwright_redfish_insert_media_body"].(map[string]any)
	if !ok {
		t.Fatalf("%s body fact got %v", insertAttemptTasks[standardBodyIdx]["name"], standardBodyFact["bootwright_redfish_insert_media_body"])
	}
	if standardBody["TransferProtocolType"] != "{{ bootwright_redfish_vmedia_transfer_protocol }}" || standardBody["Inserted"] != true || standardBody["Image"] != "{{ bootwright_component.boot.agentIso.fetchUrl }}" {
		t.Fatalf("standard InsertMedia body must include Image, Inserted, and protocol, got %v", standardBody)
	}
	if got := insertAttemptTasks[standardBodyIdx]["when"]; got != "(bootwright_redfish_vmedia_action_body_style | default('standard-insert-media')) == 'standard-insert-media'" {
		t.Fatalf("standard InsertMedia body must be selected by action body style, got %v", got)
	}
	if _, ok := standardBody["TransferMethod"]; ok {
		t.Fatalf("standard InsertMedia body must stay minimal unless action metadata requires more, got %v", standardBody)
	}
	if _, ok := standardBody["WriteProtected"]; ok {
		t.Fatalf("standard InsertMedia body must not send WriteProtected by default, got %v", standardBody)
	}
	vmmBodyFact, ok := insertAttemptTasks[vmmBodyIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[vmmBodyIdx]["name"])
	}
	vmmBody, ok := vmmBodyFact["bootwright_redfish_insert_media_body"].(map[string]any)
	if !ok || vmmBody["VmmControlType"] != "Connect" {
		t.Fatalf("VMM virtual media body must use VmmControlType=Connect, got %v", vmmBodyFact["bootwright_redfish_insert_media_body"])
	}
	if got := insertAttemptTasks[vmmBodyIdx]["when"]; got != "(bootwright_redfish_vmedia_action_body_style | default('standard-insert-media')) == 'vmm-control'" {
		t.Fatalf("VMM body must be selected by action body style, got %v", got)
	}
	insertURI, ok := insertAttemptTasks[insertIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", insertAttemptTasks[insertIdx]["name"])
	}
	insertBody, ok := insertURI["body"].(string)
	if !ok || insertBody != "{{ bootwright_redfish_insert_media_body }}" {
		t.Fatalf("InsertMedia must include derived TransferProtocolType, got %v", insertURI["body"])
	}
	if got := insertURI["url"]; got != "{{ bootwright_redfish_vmedia_insert_url }}" {
		t.Fatalf("InsertMedia must use discovered action URL, got %v", got)
	}
	if got := insertAttemptTasks[insertIdx]["failed_when"]; got != false {
		t.Fatalf("InsertMedia failures must be captured for retry instead of failing the play, got failed_when=%v", got)
	}
	patchTask := insertAttemptTasks[patchMediaIdx]
	patchPreconditionFact, ok := insertAttemptTasks[patchPreconditionIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[patchPreconditionIdx]["name"])
	}
	patchPrecondition, ok := patchPreconditionFact["bootwright_redfish_vmedia_patch_if_match"].(string)
	if !ok || !strings.Contains(patchPrecondition, "@odata.etag") || !strings.Contains(patchPrecondition, "bootwright_redfish_vmedia_if_match") {
		t.Fatalf("virtual media PATCH fallback precondition must use refreshed media ETag, got %v", patchPreconditionFact["bootwright_redfish_vmedia_patch_if_match"])
	}
	assertURIHeader(t, patchTask, "If-Match", "{{ bootwright_redfish_vmedia_patch_if_match }}")
	patchAttemptFact, ok := insertAttemptTasks[patchAttemptIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[patchAttemptIdx]["name"])
	}
	patchAttempt, ok := patchAttemptFact["bootwright_redfish_vmedia_patch_attempted"].(string)
	if !ok || !strings.Contains(patchAttempt, "bootwright_redfish_vmedia_vmm_control") || !strings.Contains(patchAttempt, "Inserted") {
		t.Fatalf("PATCH attachment fallback attempt must exclude VMM control and inserted media, got %v", patchAttemptFact["bootwright_redfish_vmedia_patch_attempted"])
	}
	if got := patchTask["when"]; got != "bootwright_redfish_vmedia_patch_attempted | bool" {
		t.Fatalf("PATCH attachment fallback must use resolved attempt guard, got when=%v", got)
	}
	if got := insertAttemptTasks[waitPatchMediaIdx]["when"]; !stringListContains(got, "bootwright_redfish_vmedia_patch_attempted | bool") {
		t.Fatalf("PATCH attachment wait must use resolved attempt guard, got when=%v", got)
	}
	taskRef, ok := insertAttemptTasks[taskRefIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[taskRefIdx]["name"])
	}
	if !strings.Contains(taskRef["bootwright_redfish_insert_task_ref"].(string), "bootwright_redfish_insert_media.location") || !strings.Contains(taskRef["bootwright_redfish_insert_task_ref"].(string), "@odata.id") || !strings.Contains(taskRef["bootwright_redfish_insert_task_ref"].(string), "/TaskService/TaskMonitors/") || !strings.Contains(taskRef["bootwright_redfish_insert_task_ref"].(string), "/TaskService/Tasks/") || !strings.Contains(taskRef["bootwright_redfish_insert_task_ref"].(string), "/Monitor$") {
		t.Fatalf("InsertMedia task reference must support Location and task JSON, got %v", taskRef["bootwright_redfish_insert_task_ref"])
	}
	if got := insertAttemptTasks[waitTaskIdx]["when"]; !stringListContains(got, "bootwright_redfish_insert_request_succeeded | bool") || !stringListContains(got, "bootwright_redfish_insert_task_url | length > 0") {
		t.Fatalf("InsertMedia task wait must be optional and skipped after failed POSTs, got when=%v", got)
	}
	taskResultFact, ok := insertAttemptTasks[taskResultIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[taskResultIdx]["name"])
	}
	taskResult, ok := taskResultFact["bootwright_redfish_insert_task_succeeded"].(string)
	if !ok || !strings.Contains(taskResult, "bootwright_redfish_insert_request_succeeded") || !strings.Contains(taskResult, "httpStatus") || !strings.Contains(taskResult, "taskStatus") {
		t.Fatalf("InsertMedia task result must reject failed async tasks without ending the retry loop, got %v", taskResultFact["bootwright_redfish_insert_task_succeeded"])
	}
	taskMounted, ok := taskResultFact["bootwright_redfish_insert_task_mounted"].(string)
	if !ok || !strings.Contains(taskMounted, "virtualmediamounted") || !strings.Contains(taskMounted, "virtual media is successfully mounted") || !strings.Contains(taskMounted, "Completed") || !strings.Contains(taskMounted, "OK") {
		t.Fatalf("InsertMedia task result must detect mounted async tasks from task messages, got %v", taskResultFact["bootwright_redfish_insert_task_mounted"])
	}
	if got := insertAttemptTasks[failedTaskProbeIdx]["when"]; got != "not (bootwright_redfish_insert_task_succeeded | bool)" {
		t.Fatalf("failed InsertMedia tasks must still probe media state, got when=%v", got)
	}
	if got := insertAttemptTasks[failedTaskProbeIdx]["register"]; got != "bootwright_redfish_vmedia_failed_task_probe" {
		t.Fatalf("failed InsertMedia probe must not reuse the effective media probe register, got register=%v", got)
	}
	if got := insertAttemptTasks[mountedTaskProbeIdx]["register"]; got != "bootwright_redfish_vmedia_mounted_task_probe" {
		t.Fatalf("mounted InsertMedia task probe must keep a separate register, got register=%v", got)
	}
	if got := insertAttemptTasks[mountedTaskProbeIdx]["when"]; !stringListContains(got, "bootwright_redfish_insert_task_succeeded | bool") || !stringListContains(got, "bootwright_redfish_insert_task_mounted | bool") {
		t.Fatalf("mounted InsertMedia task probe must only run for mounted task evidence, got when=%v", got)
	}
	if got := insertAttemptTasks[waitMediaIdx]["when"]; !stringListContains(got, "bootwright_redfish_insert_task_succeeded | bool") || !stringListContains(got, "not (bootwright_redfish_insert_task_mounted | bool)") {
		t.Fatalf("successful InsertMedia tasks must wait for reflected media state only without mounted task evidence, got when=%v", got)
	}
	if got := insertAttemptTasks[waitMediaIdx]["register"]; got != "bootwright_redfish_vmedia_insert_probe" {
		t.Fatalf("successful InsertMedia probe must not reuse the effective media probe register, got register=%v", got)
	}
	if got := insertAttemptTasks[waitMediaIdx]["until"]; !stringListItemContains(got, "bootwright_redfish_vmedia_attached") {
		t.Fatalf("successful InsertMedia media wait must use normalized attachment matching, got until=%v", got)
	}
	if got := insertAttemptTasks[waitMediaIdx]["until"]; !stringListItemContains(got, "bootwright_redfish_vmedia_insert_probe") {
		t.Fatalf("successful InsertMedia media wait must test its own register, got until=%v", got)
	}
	if got := insertAttemptTasks[waitPatchMediaIdx]["until"]; !stringListItemContains(got, "bootwright_redfish_vmedia_attached") {
		t.Fatalf("PATCH attachment wait must use normalized attachment matching, got until=%v", got)
	}
	if got := insertAttemptTasks[waitPatchMediaIdx]["register"]; got != "bootwright_redfish_vmedia_patch_probe" {
		t.Fatalf("PATCH attachment probe must not overwrite the effective media probe when skipped, got register=%v", got)
	}
	if got := insertAttemptTasks[waitPatchMediaIdx]["until"]; !stringListItemContains(got, "bootwright_redfish_vmedia_patch_probe") {
		t.Fatalf("PATCH attachment wait must test its own register, got until=%v", got)
	}
	resolveProbeAfterInsertFact, ok := insertAttemptTasks[resolveProbeAfterInsertIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[resolveProbeAfterInsertIdx]["name"])
	}
	if got := resolveProbeAfterInsertFact["bootwright_redfish_vmedia_probe"].(string); !strings.Contains(got, "bootwright_redfish_vmedia_insert_probe") || !strings.Contains(got, "bootwright_redfish_vmedia_failed_task_probe") {
		t.Fatalf("effective media probe must select the real InsertMedia probe result, got %v", got)
	}
	if got := resolveProbeAfterInsertFact["bootwright_redfish_vmedia_probe"].(string); !strings.Contains(got, "bootwright_redfish_vmedia_mounted_task_probe") || !strings.Contains(got, "bootwright_redfish_insert_task_mounted") {
		t.Fatalf("effective media probe must select mounted-task probe results when task messages prove the mount, got %v", got)
	}
	resolveProbeAfterPatchFact, ok := insertAttemptTasks[resolveProbeAfterPatchIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[resolveProbeAfterPatchIdx]["name"])
	}
	if got := resolveProbeAfterPatchFact["bootwright_redfish_vmedia_probe"].(string); !strings.Contains(got, "bootwright_redfish_vmedia_patch_probe") || !strings.Contains(got, "skipped") {
		t.Fatalf("effective media probe must ignore skipped PATCH wait registers, got %v", got)
	}
	resolveAttachmentSourcesFact, ok := insertAttemptTasks[resolveAttachmentSourcesIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[resolveAttachmentSourcesIdx]["name"])
	}
	if got := resolveAttachmentSourcesFact["bootwright_redfish_vmedia_resource_attached"].(string); !strings.Contains(got, "bootwright_redfish_vmedia_attached") {
		t.Fatalf("resource attachment source must use normalized attachment matching, got %v", got)
	}
	if got := resolveAttachmentSourcesFact["bootwright_redfish_vmedia_task_attached"].(string); !strings.Contains(got, "bootwright_redfish_insert_task_succeeded") || !strings.Contains(got, "bootwright_redfish_insert_task_mounted") || !strings.Contains(got, "Inserted") {
		t.Fatalf("task attachment source must accept completed async InsertMedia tasks without hiding explicit mismatches, got %v", got)
	}
	resolveAttachedFact, ok := insertAttemptTasks[resolveAttachedIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", insertAttemptTasks[resolveAttachedIdx]["name"])
	}
	if got, ok := resolveAttachedFact["bootwright_redfish_vmedia_attached"].(string); !ok || !strings.Contains(got, "bootwright_redfish_vmedia_resource_attached") || !strings.Contains(got, "bootwright_redfish_vmedia_task_attached") {
		t.Fatalf("attachment result must use normalized attachment matching, got %v", resolveAttachedFact["bootwright_redfish_vmedia_attached"])
	}
	if got := powerTasks[cdBootIdx]["when"]; got != "bootwright_component.boot.redfish.setBootSource | default(true) | bool" {
		t.Fatalf("Redfish CD boot override must be controlled by rendered setBootSource, got when=%v", got)
	}
	if got := powerTasks[confirmCDBootIdx]["when"]; got != "bootwright_component.boot.redfish.setBootSource | default(true) | bool" {
		t.Fatalf("Redfish CD boot confirmation must be controlled by rendered setBootSource, got when=%v", got)
	}
	systemPreconditionFact, ok := powerTasks[systemPreconditionIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", powerTasks[systemPreconditionIdx]["name"])
	}
	systemPrecondition, ok := systemPreconditionFact["bootwright_redfish_system_if_match"].(string)
	if !ok || !strings.Contains(systemPrecondition, "@odata.etag") || !strings.Contains(systemPrecondition, "ETag") {
		t.Fatalf("system PATCH precondition must prefer current ETag, got %v", systemPreconditionFact["bootwright_redfish_system_if_match"])
	}
	assertURIHeader(t, powerTasks[cdBootIdx], "If-Match", "{{ bootwright_redfish_system_if_match }}")
	mediaAssert, ok := powerTasks[confirmMediaIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert task", powerTasks[confirmMediaIdx]["name"])
	}
	if !strings.Contains(mediaAssert["fail_msg"].(string), "did not attach the requested agent ISO") {
		t.Fatalf("virtual media assertion must explain attachment mismatch, got %v", mediaAssert["fail_msg"])
	}
	for _, want := range []string{"InsertMedia attempt", "Task messages", "TaskMounted", "Requested TransferProtocolType", "Observed TransferProtocolType", "Observed VerifyCertificate", "HttpsTransferCertVerification PATCH status", "InsertMedia task attachment accepted"} {
		if !strings.Contains(mediaAssert["fail_msg"].(string), want) {
			t.Fatalf("virtual media assertion must include %q, got %v", want, mediaAssert["fail_msg"])
		}
	}
	cdAssert, ok := powerTasks[confirmCDBootIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert task", powerTasks[confirmCDBootIdx]["name"])
	}
	if !strings.Contains(cdAssert["fail_msg"].(string), "may continue booting from disk") {
		t.Fatalf("CD boot assertion must explain disk-boot risk, got %v", cdAssert["fail_msg"])
	}
	domainXML := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/templates/domain.xml.j2")
	if hdIdx, cdIdx := strings.Index(domainXML, "<boot dev='hd'/>"), strings.Index(domainXML, "<boot dev='cdrom'/>"); hdIdx < 0 || cdIdx < 0 || hdIdx > cdIdx {
		t.Fatalf("libvirt domain must keep disk-first, CD-fallback boot order")
	}

	for _, task := range postTasks {
		if _, ok := task["ansible.builtin.include_tasks"]; ok {
			t.Fatalf("post-boot Redfish media eject must not dispatch media backend include_tasks: %v", task["name"])
		}
		if _, ok := task["ansible.builtin.include_role"]; ok {
			t.Fatalf("post-boot Redfish media eject must not dispatch media backend include_role: %v", task["name"])
		}
	}

	uri, ok := postTasks[diskBootIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri task", postTasks[diskBootIdx]["name"])
	}
	body, ok := uri["body"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", postTasks[diskBootIdx]["name"])
	}
	boot, ok := body["Boot"].(map[string]any)
	if !ok {
		t.Fatalf("%s body has no Boot map", postTasks[diskBootIdx]["name"])
	}
	if got := boot["BootSourceOverrideTarget"]; got != "Hdd" {
		t.Fatalf("disk boot override target got %v, want Hdd", got)
	}
	assertURIHeader(t, postTasks[diskBootIdx], "If-Match", "{{ bootwright_redfish_system_if_match }}")
	if got := postTasks[diskBootIdx]["register"]; got != "bootwright_redfish_disk_boot_patch" {
		t.Fatalf("disk boot override must register the Redfish PATCH response, got %v", got)
	}
	if got := postTasks[diskBootIdx]["changed_when"]; got != "(bootwright_redfish_disk_boot_patch.status | default(0) | int) in [200, 202, 204]" {
		t.Fatalf("disk boot override must mark accepted PATCH responses changed, got %v", got)
	}
	sshAuth, ok := postTasks[sshAuthIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command task", postTasks[sshAuthIdx]["name"])
	}
	if argv := fmt.Sprint(sshAuth["argv"]); !strings.Contains(argv, "BatchMode=yes") || !strings.Contains(argv, "bootwright_node_ready_ssh_user") || !strings.Contains(argv, "bootwright_node_probe_address") || !strings.Contains(argv, "bootwright_node_ready_probe_port_effective") {
		t.Fatalf("SSH auth probe must use noninteractive rendered SSH readiness, got %v", sshAuth["argv"])
	}
	sshAuthAssert, ok := postTasks[sshAuthConfirmIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert task", postTasks[sshAuthConfirmIdx]["name"])
	}
	if failMsg := fmt.Sprint(sshAuthAssert["fail_msg"]); !strings.Contains(failMsg, "rejected the") || !strings.Contains(failMsg, "declared MAC/IP mapping") {
		t.Fatalf("SSH auth failure must explain stale boot and mapping risk, got %v", sshAuthAssert["fail_msg"])
	}
	diskAssert, ok := postTasks[diskBootConfirmIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert task", postTasks[diskBootConfirmIdx]["name"])
	}
	if !stringListContains(diskAssert["that"], "(bootwright_redfish_disk_boot_patch.status | default(0) | int) in [200, 202, 204]") {
		t.Fatalf("disk boot assertion must verify accepted response codes, got %v", diskAssert["that"])
	}
	if !strings.Contains(diskAssert["fail_msg"].(string), "future reboots may keep using") {
		t.Fatalf("disk boot assertion must explain rejected override risk, got %v", diskAssert["fail_msg"])
	}
	for _, task := range []string{
		"Eject virtual media after live ISO boot",
		"Wait for virtual media to report ejected agent ISO",
		"Confirm virtual media is ejected after live ISO boot",
	} {
		if idx := findAnsibleTaskIndex(postTasks, task); idx >= 0 {
			t.Fatalf("post-boot task %q must not run while the agent ISO can still be read", task)
		}
	}
	ejectIdx := findAnsibleTask(t, ejectTasks, "Eject virtual media")
	waitEjectIdx := findAnsibleTask(t, ejectTasks, "Wait for virtual media to report ejected")
	captureEjectIdx := findAnsibleTask(t, ejectTasks, "Capture virtual media eject status")
	confirmEjectIdx := findAnsibleTask(t, ejectTasks, "Confirm virtual media is ejected")
	if !(ejectIdx < waitEjectIdx && waitEjectIdx < captureEjectIdx && captureEjectIdx < confirmEjectIdx) {
		t.Fatalf("virtual media cleanup must eject, wait, redact status, then assert detached state")
	}
	ejectURI, ok := ejectTasks[ejectIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri task", ejectTasks[ejectIdx]["name"])
	}
	if got := ejectURI["url"]; got != "{{ bootwright_redfish_vmedia_eject_url }}" {
		t.Fatalf("virtual media eject must use resolved Redfish URL, got %v", got)
	}
	waitURI, ok := ejectTasks[waitEjectIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri task", ejectTasks[waitEjectIdx]["name"])
	}
	if got := waitURI["url"]; got != "{{ bootwright_redfish_vmedia_member | bootwright.core.bootwright_redfish_url(bootwright_component.boot.redfish.baseUrl) }}" {
		t.Fatalf("virtual media eject wait must poll VirtualMedia member, got %v", got)
	}
	if got := ejectTasks[waitEjectIdx]["register"]; got != "bootwright_redfish_vmedia_eject_probe" {
		t.Fatalf("virtual media eject wait must register probe, got %v", got)
	}
	if got := ejectTasks[waitEjectIdx]["retries"]; got != "{{ bootwright_redfish_vmedia_eject_retries }}" {
		t.Fatalf("virtual media eject wait must use eject retries default, got %v", got)
	}
	if got := ejectTasks[waitEjectIdx]["delay"]; got != "{{ bootwright_redfish_vmedia_eject_delay_seconds }}" {
		t.Fatalf("virtual media eject wait must use eject delay default, got %v", got)
	}
	until := fmt.Sprint(ejectTasks[waitEjectIdx]["until"])
	for _, want := range []string{"Inserted", "default(false)", "not ("} {
		if !strings.Contains(until, want) {
			t.Fatalf("virtual media eject wait missing %q in until=%v", want, until)
		}
	}
	ejectAssert, ok := ejectTasks[confirmEjectIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert task", ejectTasks[confirmEjectIdx]["name"])
	}
	if !stringListContains(ejectAssert["that"], "not (bootwright_redfish_vmedia_eject_probe.json.Inserted | default(false) | bool)") {
		t.Fatalf("virtual media eject assertion must require detached media, got %v", ejectAssert["that"])
	}
	captureEjectFact, ok := ejectTasks[captureEjectIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact task", ejectTasks[captureEjectIdx]["name"])
	}
	if got := fmt.Sprint(captureEjectFact["bootwright_redfish_vmedia_eject_image_redacted"]); !strings.Contains(got, "replace(bootwright_agent_iso_publish_token, '<redacted>')") {
		t.Fatalf("virtual media eject status must redact the agent ISO token, got %v", got)
	}
	if got := ejectTasks[captureEjectIdx]["no_log"]; got != true {
		t.Fatalf("virtual media eject redaction fact must stay no_log, got %v", got)
	}
	failMsg := fmt.Sprint(ejectAssert["fail_msg"])
	if !strings.Contains(failMsg, "bootwright_redfish_vmedia_eject_image_redacted") || strings.Contains(failMsg, "Image={{ bootwright_redfish_vmedia_eject_probe.json.Image") {
		t.Fatalf("virtual media eject assertion must use redacted image status, got %v", failMsg)
	}
	defaults := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/defaults/main.yml")
	for _, want := range []string{"bootwright_redfish_vmedia_eject_retries", "bootwright_redfish_vmedia_eject_delay_seconds"} {
		if !strings.Contains(defaults, want) {
			t.Fatalf("boot_redfish defaults missing %q", want)
		}
	}
}

func TestInstallAgentPublishesArtifactThroughDeclaredStageHost(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/iso/publish_target.yml")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve agent ISO publish target")
	validateIdx := findAnsibleTask(t, tasks, "Validate agent ISO publish target")
	dirIdx := findAnsibleTask(t, tasks, "Resolve agent ISO staging directory")
	localityIdx := findAnsibleTask(t, tasks, "Determine agent ISO publish locality")
	alreadyStagedIdx := findAnsibleTask(t, tasks, "Determine whether agent ISO is already staged")
	createDirIdx := findAnsibleTask(t, tasks, "Create agent ISO staging directory")
	linkIdx := findAnsibleTask(t, tasks, "Link agent ISO at the BMC fetch location")
	sameHostCopyIdx := findAnsibleTask(t, tasks, "Copy agent ISO within the BMC fetch host")
	crossHostCopyIdx := findAnsibleTask(t, tasks, "Copy agent ISO to the BMC fetch host")
	restrictIdx := findAnsibleTask(t, tasks, "Restrict staged agent ISO permissions")
	dirLabelIdx := findAnsibleTask(t, tasks, "Align agent ISO staging directory label with publish root")
	fileLabelIdx := findAnsibleTask(t, tasks, "Align staged agent ISO label with staging directory")
	fetchProbeIdx := findAnsibleTask(t, tasks, "Probe staged agent ISO fetch URL")
	rangeProbeIdx := findAnsibleTask(t, tasks, "Probe staged agent ISO byte-range fetch")
	fetchConfirmIdx := findAnsibleTask(t, tasks, "Confirm staged agent ISO fetch URL is reachable")
	if !(resolveIdx < validateIdx && validateIdx < dirIdx && dirIdx < localityIdx && localityIdx < alreadyStagedIdx && alreadyStagedIdx < createDirIdx && createDirIdx < linkIdx && linkIdx < sameHostCopyIdx && sameHostCopyIdx < crossHostCopyIdx && crossHostCopyIdx < restrictIdx && restrictIdx < dirLabelIdx && dirLabelIdx < fileLabelIdx && fileLabelIdx < fetchProbeIdx && fetchProbeIdx < rangeProbeIdx && rangeProbeIdx < fetchConfirmIdx) {
		t.Fatalf("install_agent must validate the target, stage the ISO, and probe fetch reachability before node boot")
	}

	resolveFact, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[resolveIdx]["name"])
	}
	for _, key := range []string{"bootwright_agent_iso_stage_path", "bootwright_agent_iso_fetch_url"} {
		if !strings.Contains(fmt.Sprint(resolveFact[key]), "replace(bootwright_agent_iso_publish_token_placeholder, bootwright_agent_iso_publish_token)") {
			t.Fatalf("%s must substitute the generated publish token, got %v", key, resolveFact[key])
		}
	}

	targetAssert, ok := tasks[validateIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert body", tasks[validateIdx]["name"])
	}
	if !stringListContains(targetAssert["that"], "(bootwright_agent_iso_path | default('') | length) > 0") {
		t.Fatalf("publish target validation must require the generated ISO path, got %v", targetAssert["that"])
	}
	if !stringListContains(targetAssert["that"], "(bootwright_agent_iso_stage_path | default('') | length) > 0") {
		t.Fatalf("publish target validation must require the resolved stage path, got %v", targetAssert["that"])
	}
	if !stringListContains(targetAssert["that"], "(bootwright_agent_iso_fetch_url | default('') | length) > 0") {
		t.Fatalf("publish target validation must require the resolved fetch URL, got %v", targetAssert["that"])
	}
	if !stringListContains(targetAssert["that"], "(bootwright_agent_iso_publish_target.stageHost == (bootwright_host_name | default(inventory_hostname))) or ((bootwright_agent_iso_local_path | default('') | length) > 0)") {
		t.Fatalf("cross-host publish must require a controller-side transfer copy, got %v", targetAssert["that"])
	}
	if !stringListContains(targetAssert["that"], "(not (bootwright_agent_iso_publish_target.requiresHTTPS | default(false) | bool)) or ((bootwright_agent_iso_fetch_url | ansible.builtin.urlsplit('scheme') | lower) == 'https')") {
		t.Fatalf("real Redfish ISO URL scheme guard must require HTTPS while allowing emulated Redfish, got %v", targetAssert["that"])
	}

	stageDir, ok := tasks[createDirIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", tasks[createDirIdx]["name"])
	}
	if got := stageDir["path"]; got != "{{ bootwright_agent_iso_stage_dir }}" {
		t.Fatalf("stage directory path got %v", got)
	}
	if got := tasks[createDirIdx]["delegate_to"]; got != "{{ bootwright_agent_iso_publish_target.stageHost }}" {
		t.Fatalf("stage directory delegate got %v", got)
	}
	if got := tasks[createDirIdx]["become"]; got != true {
		t.Fatalf("stage directory must use remote become, got %v", got)
	}

	stageLink, ok := tasks[linkIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", tasks[linkIdx]["name"])
	}
	for _, want := range []string{"ln", "{{ bootwright_agent_iso_path }}", "{{ bootwright_agent_iso_stage_path }}"} {
		if !stringListContains(stageLink["argv"], want) {
			t.Fatalf("same-host stage link command missing %q: %v", want, stageLink["argv"])
		}
	}
	if got := tasks[linkIdx]["delegate_to"]; got != "{{ bootwright_agent_iso_publish_target.stageHost }}" {
		t.Fatalf("stage link delegate got %v", got)
	}
	if got := tasks[linkIdx]["become"]; got != true {
		t.Fatalf("stage link must use remote become, got %v", got)
	}
	if !stringListContains(tasks[linkIdx]["when"], "bootwright_agent_iso_stage_local_to_install_host | bool") {
		t.Fatalf("stage link when got %v", tasks[linkIdx]["when"])
	}
	if !stringListContains(tasks[linkIdx]["when"], "not (bootwright_agent_iso_already_staged | bool)") {
		t.Fatalf("stage link must skip direct-generated ISOs, got %v", tasks[linkIdx]["when"])
	}

	sameHostCopy, ok := tasks[sameHostCopyIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", tasks[sameHostCopyIdx]["name"])
	}
	for _, want := range []string{"install", "-m", "0600", "-o", "root", "-g", "root", "{{ bootwright_agent_iso_path }}", "{{ bootwright_agent_iso_stage_path }}"} {
		if !stringListContains(sameHostCopy["argv"], want) {
			t.Fatalf("same-host stage copy command missing %q: %v", want, sameHostCopy["argv"])
		}
	}
	if got := tasks[sameHostCopyIdx]["delegate_to"]; got != "{{ bootwright_agent_iso_publish_target.stageHost }}" {
		t.Fatalf("same-host stage copy delegate got %v", got)
	}
	if !stringListContains(tasks[sameHostCopyIdx]["when"], "bootwright_agent_iso_stage_local_to_install_host | bool") {
		t.Fatalf("same-host stage copy must require same-host publishing, got %v", tasks[sameHostCopyIdx]["when"])
	}
	if !stringListContains(tasks[sameHostCopyIdx]["when"], "not (bootwright_agent_iso_already_staged | bool)") {
		t.Fatalf("same-host stage copy must skip direct-generated ISOs, got %v", tasks[sameHostCopyIdx]["when"])
	}
	if !stringListContains(tasks[sameHostCopyIdx]["when"], "bootwright_agent_iso_stage_link.rc | default(1) != 0") {
		t.Fatalf("same-host stage copy must only run after hard-link fallback, got %v", tasks[sameHostCopyIdx]["when"])
	}

	crossHostCopy, ok := tasks[crossHostCopyIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no copy body", tasks[crossHostCopyIdx]["name"])
	}
	if got := crossHostCopy["src"]; got != "{{ bootwright_agent_iso_local_path }}" {
		t.Fatalf("stage copy source got %v", got)
	}
	if got := crossHostCopy["dest"]; got != "{{ bootwright_agent_iso_stage_path }}" {
		t.Fatalf("stage destination got %v", got)
	}
	if got := tasks[crossHostCopyIdx]["delegate_to"]; got != "{{ bootwright_agent_iso_publish_target.stageHost }}" {
		t.Fatalf("stage copy delegate got %v", got)
	}
	if got := tasks[crossHostCopyIdx]["become"]; got != true {
		t.Fatalf("stage copy must use remote become, got %v", got)
	}
	if !stringListContains(tasks[crossHostCopyIdx]["when"], "not (bootwright_agent_iso_stage_local_to_install_host | bool)") {
		t.Fatalf("cross-host stage copy when got %v", tasks[crossHostCopyIdx]["when"])
	}
	if !stringListContains(tasks[crossHostCopyIdx]["when"], "not (bootwright_agent_iso_already_staged | bool)") {
		t.Fatalf("cross-host stage copy must skip already-staged ISOs, got %v", tasks[crossHostCopyIdx]["when"])
	}

	restrict, ok := tasks[restrictIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", tasks[restrictIdx]["name"])
	}
	if got := restrict["path"]; got != "{{ bootwright_agent_iso_stage_path }}" {
		t.Fatalf("restrict path got %v", got)
	}

	for _, tc := range []struct {
		idx       int
		reference string
		target    string
	}{
		{
			idx:       dirLabelIdx,
			reference: "--reference={{ bootwright_agent_iso_stage_dir | dirname }}",
			target:    "{{ bootwright_agent_iso_stage_dir }}",
		},
		{
			idx:       fileLabelIdx,
			reference: "--reference={{ bootwright_agent_iso_stage_dir }}",
			target:    "{{ bootwright_agent_iso_stage_path }}",
		},
	} {
		label, ok := tasks[tc.idx]["ansible.builtin.command"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no command body", tasks[tc.idx]["name"])
		}
		for _, want := range []string{"chcon", tc.reference, tc.target} {
			if !stringListContains(label["argv"], want) {
				t.Fatalf("%s command missing %q: %v", tasks[tc.idx]["name"], want, label["argv"])
			}
		}
		if got := tasks[tc.idx]["delegate_to"]; got != "{{ bootwright_agent_iso_publish_target.stageHost }}" {
			t.Fatalf("%s delegate got %v", tasks[tc.idx]["name"], got)
		}
		if got := tasks[tc.idx]["become"]; got != true {
			t.Fatalf("%s must use remote become, got %v", tasks[tc.idx]["name"], got)
		}
		if got := tasks[tc.idx]["failed_when"]; got != false {
			t.Fatalf("%s must tolerate hosts without chcon, got failed_when=%v", tasks[tc.idx]["name"], got)
		}
	}

	fetchProbe, ok := tasks[fetchProbeIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", tasks[fetchProbeIdx]["name"])
	}
	if got := fetchProbe["url"]; got != "{{ bootwright_agent_iso_fetch_url }}" {
		t.Fatalf("fetch URL probe target got %v", got)
	}
	if got := fetchProbe["method"]; got != "HEAD" {
		t.Fatalf("fetch URL probe must avoid downloading the ISO, got method=%v", got)
	}
	if got := tasks[fetchProbeIdx]["delegate_to"]; got != "{{ bootwright_agent_iso_publish_target.stageHost }}" {
		t.Fatalf("fetch URL probe delegate got %v", got)
	}
	if got := fetchProbe["validate_certs"]; got != false {
		t.Fatalf("fetch URL probe must allow generated self-signed certs, got %v", got)
	}
	rangeProbe, ok := tasks[rangeProbeIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", tasks[rangeProbeIdx]["name"])
	}
	if got := rangeProbe["method"]; got != "GET" {
		t.Fatalf("range probe must issue a byte-range GET, got method=%v", got)
	}
	if got := rangeProbe["url"]; got != "{{ bootwright_agent_iso_fetch_url }}" {
		t.Fatalf("range probe target got %v", got)
	}
	headers, ok := rangeProbe["headers"].(map[string]any)
	if !ok || headers["Range"] != "bytes=0-0" {
		t.Fatalf("range probe must request one byte, got headers=%v", rangeProbe["headers"])
	}
	if got := rangeProbe["status_code"]; !intListEqual(got, []int{206}) {
		t.Fatalf("range probe must require HTTP 206, got %v", got)
	}
	if got := tasks[rangeProbeIdx]["when"]; got != "bootwright_agent_iso_publish_target.requiresByteRange | default(false) | bool" {
		t.Fatalf("range probe must only be required for real BMCs, got when=%v", got)
	}
	fetchConfirm, ok := tasks[fetchConfirmIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert body", tasks[fetchConfirmIdx]["name"])
	}
	if !stringListContains(fetchConfirm["that"], "(bootwright_agent_iso_fetch_probe.status | default(0) | int) in [200, 206]") {
		t.Fatalf("fetch URL confirmation must reject unreachable staged ISOs, got %v", fetchConfirm["that"])
	}
	if !stringListContains(fetchConfirm["that"], "(not (bootwright_agent_iso_publish_target.requiresByteRange | default(false) | bool)) or ((bootwright_agent_iso_range_probe.status | default(0) | int) == 206)") {
		t.Fatalf("fetch URL confirmation must require byte ranges for real BMCs, got %v", fetchConfirm["that"])
	}
}

func TestInstallAgentResolvesAgentISOPublishTokenPlaceholder(t *testing.T) {
	defaults := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/defaults/main.yml")
	if !strings.Contains(defaults, `bootwright_agent_iso_publish_token_placeholder: "__BOOTWRIGHT_AGENT_ISO_PUBLISH_TOKEN__"`) {
		t.Fatalf("install_agent defaults must define the publish token placeholder")
	}

	cleanupTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/iso/cleanup_target.yml")
	resolveCleanup := cleanupTasks[findAnsibleTask(t, cleanupTasks, "Resolve staged agent ISO publish path")]
	cleanupFact, ok := resolveCleanup["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", resolveCleanup["name"])
	}
	if !strings.Contains(fmt.Sprint(cleanupFact["bootwright_agent_iso_publish_stage_path"]), "replace(bootwright_agent_iso_publish_token_placeholder, bootwright_agent_iso_publish_token)") {
		t.Fatalf("cleanup must resolve the tokenized stage path, got %v", cleanupFact)
	}

	bootTopTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/boot_machine.yml")
	bootTasks := nestedAnsibleTasks(t, bootTopTasks[findAnsibleTask(t, bootTopTasks, "Boot selected machine when install is not already complete")], "block")
	resolveBoot := bootTasks[findAnsibleTask(t, bootTasks, "Resolve selected machine agent ISO target")]
	bootFact, ok := resolveBoot["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", resolveBoot["name"])
	}
	body := fmt.Sprint(bootFact["bootwright_selected_component"])
	if !strings.Contains(body, "replace(bootwright_agent_iso_publish_token_placeholder, bootwright_agent_iso_publish_token)") {
		t.Fatalf("boot_machine must resolve tokenized agent ISO URLs before boot role dispatch, got %v", body)
	}
}

func TestAgentISOPublishTokenizedValuesAreRedactedFromMessages(t *testing.T) {
	for _, rel := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/iso/cleanup_target.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/create_iso.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/iso/publish_target.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power.yml",
	} {
		tasks := readAnsibleTasks(t, rel)
		var messages []string
		collectAnsibleMessages(tasks, &messages)
		for _, msg := range messages {
			for _, forbidden := range []string{
				"{{ bootwright_agent_iso_fetch_url }}",
				"{{ bootwright_agent_iso_stage_path }}",
				"{{ bootwright_agent_iso_direct_stage_path |",
				"{{ bootwright_agent_iso_publish_stage_dir }}",
				"{{ bootwright_component.boot.agentIso.fetchUrl }}",
				"{{ bootwright_redfish_vmedia_status.image |",
				"{{ bootwright_redfish_vmedia_status.insertTaskMessages }}",
			} {
				if strings.Contains(msg, forbidden) {
					t.Fatalf("%s message prints tokenized agent ISO value %q:\n%s", rel, forbidden, msg)
				}
			}
		}
	}
}

func TestBootRedfishValidatesDeclaredMACsFromInventory(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/validation/macs.yml")
	systemIdx := findAnsibleTask(t, tasks, "Refresh Redfish system metadata for declared MAC validation")
	collectionIdx := findAnsibleTask(t, tasks, "List Redfish EthernetInterface members")
	memberIdx := findAnsibleTask(t, tasks, "Probe Redfish EthernetInterface members")
	compareIdx := findAnsibleTask(t, tasks, "Compare declared interface MACs with Redfish inventory")
	reportIdx := findAnsibleTask(t, tasks, "Report unsupported Redfish EthernetInterface MAC inventory")
	confirmIdx := findAnsibleTask(t, tasks, "Confirm declared interface MACs exist in Redfish inventory")
	if !(systemIdx < collectionIdx && collectionIdx < memberIdx && memberIdx < compareIdx && compareIdx < reportIdx && reportIdx < confirmIdx) {
		t.Fatalf("MAC validation must discover system/collection/members, compare, then report")
	}
	for _, idx := range []int{systemIdx, collectionIdx, memberIdx} {
		uri, ok := tasks[idx]["ansible.builtin.uri"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no uri body", tasks[idx]["name"])
		}
		for _, want := range []string{
			"bootwright_redfish_credentials.username",
			"bootwright_redfish_credentials.password",
			"bootwright_redfish_cred_path",
		} {
			if !strings.Contains(fmt.Sprint(uri), want) && !strings.Contains(fmt.Sprint(tasks[idx]["no_log"]), want) {
				t.Fatalf("%s must use credential-safe Redfish request fields containing %q", tasks[idx]["name"], want)
			}
		}
		if !strings.Contains(fmt.Sprint(tasks[idx]["no_log"]), "bootwright_redfish_cred_path") {
			t.Fatalf("%s must hide uri output when credentials are used", tasks[idx]["name"])
		}
	}
	collectionURI := tasks[collectionIdx]["ansible.builtin.uri"].(map[string]any)
	if !strings.Contains(fmt.Sprint(collectionURI["url"]), "bootwright_redfish_ethernet_interfaces_path") {
		t.Fatalf("EthernetInterface collection URL must use resolved collection path, got %v", collectionURI["url"])
	}
	memberURI := tasks[memberIdx]["ansible.builtin.uri"].(map[string]any)
	if !strings.Contains(fmt.Sprint(memberURI["url"]), "item['@odata.id']") {
		t.Fatalf("EthernetInterface member probe must dereference member @odata.id, got %v", memberURI["url"])
	}
	compare, ok := tasks[compareIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[compareIdx]["name"])
	}
	if !strings.Contains(fmt.Sprint(compare["bootwright_redfish_mac_validation"]), "bootwright_redfish_mac_validation") {
		t.Fatalf("MAC comparison must use Redfish MAC validation filter, got %v", compare)
	}
	assertBody, ok := tasks[confirmIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert body", tasks[confirmIdx]["name"])
	}
	if !stringListContains(assertBody["that"], "(bootwright_redfish_mac_validation.missing | default([]) | length) == 0") {
		t.Fatalf("MAC confirmation must fail on missing declared MACs, got %v", assertBody["that"])
	}
	failMsg := fmt.Sprint(assertBody["fail_msg"])
	if !strings.Contains(failMsg, "Observed Redfish MACs") || !strings.Contains(failMsg, "bootwright_redfish_mac_validation.observed") {
		t.Fatalf("MAC failure message must include observed Redfish MACs, got %v", assertBody["fail_msg"])
	}
	if !strings.Contains(failMsg, "baremetal.interfaces") {
		t.Fatalf("MAC failure message must point operators to baremetal.interfaces, got %v", assertBody["fail_msg"])
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

func TestBootRedfishHasNoMediaBackendSpecificReferences(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/defaults/main.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/validation/macs.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_state_probe.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/post_boot.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/stage/validate.yml",
	} {
		body := readRepoFile(t, path)
		for _, forbidden := range []string{"libvirt", "eject_libvirt_media"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s must not contain media-backend-specific text %q", path, forbidden)
			}
		}
	}
}

func TestArtifactsHTTPServiceUsesContainerNginxWithTLS(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_artifact_server_http/tasks/main.yml")
	validateIdx := findAnsibleTask(t, tasks, "Validate boot artifact server settings")
	pathsIdx := findAnsibleTask(t, tasks, "Resolve boot artifact paths")
	packagesIdx := findAnsibleTask(t, tasks, "Install boot artifact server packages")
	dirsIdx := findAnsibleTask(t, tasks, "Create boot artifact directories")
	tlsCertIdx := findAnsibleTask(t, tasks, "Stat boot artifact TLS certificate")
	tlsKeyIdx := findAnsibleTask(t, tasks, "Stat boot artifact TLS key")
	tlsPresentIdx := findAnsibleTask(t, tasks, "Detect existing boot artifact TLS material")
	tlsConfigIdx := findAnsibleTask(t, tasks, "Render boot artifact TLS OpenSSL config")
	tlsGenerateIdx := findAnsibleTask(t, tasks, "Generate boot artifact TLS certificate")
	keyModeIdx := findAnsibleTask(t, tasks, "Restrict boot artifact TLS key")
	certModeIdx := findAnsibleTask(t, tasks, "Set boot artifact TLS certificate mode")
	nginxIdx := findAnsibleTask(t, tasks, "Render boot artifact nginx configuration")
	volumesIdx := findAnsibleTask(t, tasks, "Resolve boot artifact container volumes")
	containerIdx := findAnsibleTask(t, tasks, "Run boot artifact server container")
	firewallIdx := findAnsibleTask(t, tasks, "Open boot artifact ports on host firewall")
	waitIdx := findAnsibleTask(t, tasks, "Wait for boot artifact endpoints")
	if !(validateIdx < pathsIdx && pathsIdx < packagesIdx && packagesIdx < dirsIdx && dirsIdx < tlsCertIdx && tlsCertIdx < tlsKeyIdx && tlsKeyIdx < tlsPresentIdx && tlsPresentIdx < tlsConfigIdx && tlsConfigIdx < tlsGenerateIdx && tlsGenerateIdx < keyModeIdx && keyModeIdx < certModeIdx && certModeIdx < nginxIdx && nginxIdx < volumesIdx && volumesIdx < containerIdx && containerIdx < firewallIdx && firewallIdx < waitIdx) {
		t.Fatalf("artifacts_http must prepare TLS, render nginx, then start the container")
	}

	packages, ok := tasks[packagesIdx]["ansible.builtin.package"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no package body", tasks[packagesIdx]["name"])
	}
	if got := fmt.Sprint(packages["name"]); !strings.Contains(got, "podman") || !strings.Contains(got, "openssl") {
		t.Fatalf("artifact server packages must include podman and openssl, got %v", packages["name"])
	}

	nginx := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_artifact_server_http/templates/artifacts-nginx.conf.j2")
	for _, want := range []string{"user root;", "listen {{ listen_host }}:{{ listener.port }}", "ssl_certificate", "try_files $uri =404", "autoindex off"} {
		if !strings.Contains(nginx, want) {
			t.Fatalf("artifact nginx template missing %q", want)
		}
	}
	tlsTemplate := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_artifact_server_http/templates/artifacts-openssl.cnf.j2")
	for _, want := range []string{"subjectAltName", "bootwright_component.tls.dnsNames", "bootwright_component.tls.ipAddresses"} {
		if !strings.Contains(tlsTemplate, want) {
			t.Fatalf("artifact TLS template must render SANs; missing %q", want)
		}
	}
	for _, idx := range []int{tlsConfigIdx, tlsGenerateIdx} {
		when := fmt.Sprint(tasks[idx]["when"])
		if !strings.Contains(when, "not (bootwright_artifacts_tls_material_present") {
			t.Fatalf("%s must preserve existing TLS material, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}

	container, ok := tasks[containerIdx]["containers.podman.podman_container"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no podman_container body", tasks[containerIdx]["name"])
	}
	if got := container["network"]; got != "host" {
		t.Fatalf("artifact container must use host networking, got %v", got)
	}
	recreate := fmt.Sprint(container["recreate"])
	if !strings.Contains(recreate, "bootwright_artifacts_config.changed") || !strings.Contains(recreate, "bootwright_artifacts_tls_generated.changed") {
		t.Fatalf("artifact container must recreate on config or TLS changes, got %v", container["recreate"])
	}
	volumes, ok := tasks[volumesIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(volumes["bootwright_artifacts_volumes"]), "/usr/share/nginx/html") {
		t.Fatalf("artifact container volumes must mount artifact root into nginx, got %v", tasks[volumesIdx])
	}
	waitURI, ok := tasks[waitIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", tasks[waitIdx]["name"])
	}
	if got := fmt.Sprint(waitURI["url"]); !strings.Contains(got, "{{ item.protocol }}://") || !strings.Contains(got, "{{ item.port | int }}") {
		t.Fatalf("artifact readiness probe must follow rendered listeners, got %v", got)
	}
	if got := waitURI["validate_certs"]; got != false {
		t.Fatalf("artifact readiness probe must allow self-signed certs, got %v", got)
	}
	if got := waitURI["status_code"]; got != 404 {
		t.Fatalf("artifact readiness probe must expect directory listing rejection, got %v", got)
	}
}

func TestRegisteredAdapterRolesExist(t *testing.T) {
	roles := registeredAdapterRoles()
	for role := range roles {
		if !ansibleRoleDirExists(t, role) {
			t.Fatalf("registered Ansible adapter role %q has no role directory", role)
		}
	}
}

func TestAdapterRoleDirectoriesAreRegistered(t *testing.T) {
	registered := registeredAdapterRoles()
	for _, role := range ansibleAdapterRoleDirs(t) {
		if !registered[role] {
			t.Fatalf("Ansible adapter role %q exists but is not in the driver registry", role)
		}
	}
}

func TestMachineServicePlaybooksDispatchRenderedRoles(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_provider_services_apply.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_provider_services_destroy.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_infra_component_services_apply.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_infra_component_services_destroy.yml",
	} {
		body := readRepoFile(t, path)
		for _, forbidden := range []string{
			"load_balancer_haproxy",
			"artifacts_http",
			"proxy_squid",
			"dns_dnsmasq",
			"ntp_chrony",
			"mirror_registry",
			"bmc_{{",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s hardcodes provider service role %q", path, forbidden)
			}
		}
		for _, want := range []string{"bootwright_component.applyRole", "bootwright_component.destroyRole"} {
			if strings.Contains(path, "apply.yml") && want == "bootwright_component.destroyRole" {
				continue
			}
			if strings.Contains(path, "destroy.yml") && want == "bootwright_component.applyRole" {
				continue
			}
			if !strings.Contains(body, want) {
				t.Fatalf("%s must dispatch using rendered %s", path, want)
			}
		}
	}
}

func TestInfraComponentDestroyCleanupUsesBootwrightPodmanLabels(t *testing.T) {
	roles := map[string]string{
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_artifact_server_http/tasks/destroy.yml":    "artifacts",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_load_balancer_haproxy/tasks/destroy.yml":   "load-balancer",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_name_resolution_dnsmasq/tasks/destroy.yml": "nameResolution",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_proxy_squid/tasks/destroy.yml":             "proxy",
		"ansible/collections/ansible_collections/bootwright/core/roles/infra_component_registry_mirror/tasks/destroy.yml":         "registry",
	}
	for path, kind := range roles {
		body := readRepoFile(t, path)
		if strings.Contains(body, "bootwright_process_cleanup_pattern") {
			t.Fatalf("%s must not cleanup provider containers by process pattern", path)
		}
		for _, want := range []string{
			"bootwright_process_cleanup_podman_filters:",
			"label=bootwright.kind=" + kind,
			"label=bootwright.provider={{ bootwright_component.providerName }}",
			"label=bootwright.name={{ bootwright_component.name }}",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s cleanup missing %q", path, want)
			}
		}
	}
}

func TestLibvirtNetworkUsesResolvedNTPIPv4Sources(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/templates/network.xml.j2")
	for _, want := range []string{
		"bootwright_resolved_ntp_sources",
		"select('match', '^[0-9]+\\\\.[0-9]+\\\\.[0-9]+\\\\.[0-9]+$')",
		"dhcp-option=option:ntp-server",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("libvirt network template missing %q", want)
		}
	}
}

func TestLibvirtNetworkEntrypointBuildsMachineListLocally(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/network.yml")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve libvirt network machines")
	renderIdx := findAnsibleTask(t, tasks, "Render libvirt network XML")
	if resolveIdx > renderIdx {
		t.Fatalf("libvirt network machines must be resolved before rendering network XML")
	}
	setFact, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[resolveIdx]["name"])
	}
	expr, ok := setFact["bootwright_libvirt_network_machines"].(string)
	if !ok {
		t.Fatalf("bootwright_libvirt_network_machines fact = %v", setFact["bootwright_libvirt_network_machines"])
	}
	for _, want := range []string{"bootwright_current_machines", "bootwright_prepare_components", "[bootwright_component]"} {
		if !strings.Contains(expr, want) {
			t.Fatalf("libvirt network machine list expression missing %q: %s", want, expr)
		}
	}
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/templates/network.xml.j2")
	if !strings.Contains(body, "bootwright_libvirt_network_machines") {
		t.Fatalf("libvirt network template must iterate the local network machine list")
	}
	if strings.Contains(body, "bootwright_current_machines") {
		t.Fatalf("libvirt network template must not depend on caller-scoped bootwright_current_machines")
	}
}

func TestLibvirtStorageStaysOutOfPrivateBootwrightState(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/tasks/machine.yml")
	domainXML := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_libvirt/templates/domain.xml.j2")

	probeIdx := findAnsibleTask(t, tasks, "Probe libvirt runtime users")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve libvirt runtime storage access")
	selectIdx := findAnsibleTask(t, tasks, "Select libvirt runtime user")
	assertIdx := findAnsibleTask(t, tasks, "Assert libvirt runtime user")
	migrateDiskIdx := findAnsibleTask(t, tasks, "Migrate machine disk to libvirt storage")
	migrateDataIdx := findAnsibleTask(t, tasks, "Migrate machine data disks to libvirt storage")
	createDiskIdx := findAnsibleTask(t, tasks, "Create machine disk")
	createDataDiskIdx := findAnsibleTask(t, tasks, "Create machine data disks")
	ownDiskIdx := findAnsibleTask(t, tasks, "Align libvirt disk image ownership")
	renderDomainIdx := findAnsibleTask(t, tasks, "Render libvirt domain XML")

	if !(probeIdx < resolveIdx && resolveIdx < selectIdx && selectIdx < assertIdx && assertIdx < migrateDiskIdx && migrateDiskIdx < createDiskIdx && createDataDiskIdx < ownDiskIdx && ownDiskIdx < renderDomainIdx) {
		t.Fatalf("libvirt storage tasks must migrate/create/own disks before domain definition")
	}
	if migrateDataIdx > createDataDiskIdx {
		t.Fatalf("data disk migration must run before data disk creation")
	}

	probe, ok := tasks[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", tasks[probeIdx]["name"])
	}
	for _, want := range []string{"getent", "passwd"} {
		if !strings.Contains(fmt.Sprint(probe["argv"]), want) {
			t.Fatalf("%s argv missing %q: %v", tasks[probeIdx]["name"], want, probe["argv"])
		}
	}
	for _, want := range []string{"qemu", "libvirt-qemu"} {
		if !strings.Contains(fmt.Sprint(tasks[probeIdx]["loop"]), want) {
			t.Fatalf("%s loop missing %q: %v", tasks[probeIdx]["name"], want, tasks[probeIdx]["loop"])
		}
	}

	resolve, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[resolveIdx]["name"])
	}
	for _, key := range []string{"bootwright_libvirt_runtime_users", "bootwright_libvirt_disk_paths"} {
		if _, ok := resolve[key]; !ok {
			t.Fatalf("%s missing %s", tasks[resolveIdx]["name"], key)
		}
	}
	if !strings.Contains(fmt.Sprint(resolve["bootwright_libvirt_disk_paths"]), "bootwright_component.profile.dataDisks") {
		t.Fatalf("%s must include data disk paths, got %v", tasks[resolveIdx]["name"], resolve["bootwright_libvirt_disk_paths"])
	}
	if !strings.Contains(fmt.Sprint(resolve["bootwright_libvirt_disk_paths"]), "bootwright_libvirt_storage_root") {
		t.Fatalf("%s must point disk paths at libvirt storage, got %v", tasks[resolveIdx]["name"], resolve["bootwright_libvirt_disk_paths"])
	}

	ownership, ok := tasks[ownDiskIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", tasks[ownDiskIdx]["name"])
	}
	if got := ownership["owner"]; got != "{{ bootwright_libvirt_runtime_user }}" {
		t.Fatalf("%s owner got %v", tasks[ownDiskIdx]["name"], got)
	}
	if got := ownership["mode"]; got != "0600" {
		t.Fatalf("%s mode got %v", tasks[ownDiskIdx]["name"], got)
	}
	if got := tasks[ownDiskIdx]["loop"]; got != "{{ bootwright_libvirt_disk_paths }}" {
		t.Fatalf("%s loop got %v", tasks[ownDiskIdx]["name"], got)
	}

	if !strings.Contains(domainXML, "<source file='{{ bootwright_libvirt_storage_root }}/disk.qcow2'/>") {
		t.Fatalf("domain XML must source root disk from libvirt storage")
	}
	if !strings.Contains(domainXML, "<source file='{{ bootwright_libvirt_storage_root }}/data-{{ disk.name }}.qcow2'/>") {
		t.Fatalf("domain XML must source data disks from libvirt storage")
	}
	if strings.Contains(domainXML, "<source file='{{ bootwright_libvirt_machine_root }}/disk.qcow2'/>") {
		t.Fatalf("domain XML must not source disks from private Bootwright state")
	}
	for _, want := range []string{
		"<metadata>",
		"bootwright:context",
		"{{ bootwright_clusters_dir | dirname | basename }}",
		"bootwright:cluster",
		"bootwright:machine",
	} {
		if !strings.Contains(domainXML, want) {
			t.Fatalf("domain XML missing context ownership metadata %q", want)
		}
	}
}

func TestInfraDestroySweepsCurrentContextLibvirtDomainsOnlyWhenUnscoped(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_destroy.yml")
	if len(plays) != 1 {
		t.Fatalf("task_machine_infra_destroy plays = %d, want 1", len(plays))
	}
	if got := plays[0]["hosts"]; got != "bootwright_provider_hosts:bootwright_infra_hosts" {
		t.Fatalf("machine infra destroy hosts = %v", got)
	}
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	sweep := tasks[findAnsibleTask(t, tasks, "Sweep current-context libvirt domains")]
	when := fmt.Sprint(sweep["when"])
	for _, want := range []string{
		"bootwright_infra_destroy_context_sweep",
		"bootwright_host_libvirt_providers",
	} {
		if !strings.Contains(when, want) {
			t.Fatalf("context sweep when missing %q: %v", want, sweep["when"])
		}
	}

	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra_destroy_libvirt_context.yml")
	for _, want := range []string{
		"/var/lib/libvirt/images/bootwright/{{ bootwright_clusters_dir | dirname | basename }}/clusters",
		"virsh",
		"domblklist",
		"bootwright_libvirt_context_storage_root ~ '/'",
		"destroy",
		"undefine",
		"Remove current-context libvirt storage directory",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("libvirt context sweep missing %q", want)
		}
	}
}

func TestKubeVirtResourcesCarryContextLabels(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/templates/virtualmachine.yaml.j2",
		"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/templates/datavolume-root.yaml.j2",
		"ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/templates/networkattachmentdefinition.yaml.j2",
	} {
		body := readRepoFile(t, path)
		if !strings.Contains(body, "bootwright.io/context: {{ bootwright_clusters_dir | dirname | basename }}") {
			t.Fatalf("%s missing context ownership label", path)
		}
	}
}

func TestEmulatedBMCVMediaUsesLibvirtStorageRoot(t *testing.T) {
	factsTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/facts.yml")
	packagesTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml")
	sushyTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml")
	destroyTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml")
	poolXML := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/templates/vmedia/pool.xml.j2")
	systemd := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/templates/vmedia/systemd.service.j2")

	factsIdx := findAnsibleTask(t, factsTasks, "Resolve BMC state paths")
	facts, ok := factsTasks[factsIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", factsTasks[factsIdx]["name"])
	}
	vmediaRoot := fmt.Sprint(facts["bootwright_bmc_vmedia_root"])
	if !strings.Contains(vmediaRoot, "/var/lib/libvirt/images/bootwright/") {
		t.Fatalf("emulated BMC vmedia root must live under libvirt images, got %v", vmediaRoot)
	}
	if strings.Contains(vmediaRoot, "bootwright_bmc_root") || strings.Contains(vmediaRoot, "bootwright_provider_state_dir }}/bmc") {
		t.Fatalf("emulated BMC vmedia root must not live under private provider state, got %v", vmediaRoot)
	}

	dirsIdx := findAnsibleTask(t, packagesTasks, "Create BMC state directories")
	if !stringListContains(packagesTasks[dirsIdx]["loop"], "{{ bootwright_bmc_vmedia_root }}") {
		t.Fatalf("%s must create the separate vmedia root, got %v", packagesTasks[dirsIdx]["name"], packagesTasks[dirsIdx]["loop"])
	}

	probeIdx := findAnsibleTask(t, sushyTasks, "Probe existing libvirt vmedia storage pool")
	decideIdx := findAnsibleTask(t, sushyTasks, "Determine whether libvirt vmedia storage pool must be redefined")
	stopIdx := findAnsibleTask(t, sushyTasks, "Stop sushy-emulator before redefining vmedia storage pool")
	inactiveIdx := findAnsibleTask(t, sushyTasks, "Deactivate mismatched libvirt vmedia storage pool")
	absentIdx := findAnsibleTask(t, sushyTasks, "Undefine mismatched libvirt vmedia storage pool")
	defineIdx := findAnsibleTask(t, sushyTasks, "Define libvirt vmedia storage pool")
	activateIdx := findAnsibleTask(t, sushyTasks, "Activate libvirt vmedia storage pool")
	ensureIdx := findAnsibleTask(t, sushyTasks, "Ensure sushy-emulator service is running")
	if !(probeIdx < decideIdx && decideIdx < stopIdx && stopIdx < inactiveIdx && inactiveIdx < absentIdx && absentIdx < defineIdx && defineIdx < activateIdx && activateIdx < ensureIdx) {
		t.Fatalf("sushy vmedia pool must be probed, redefined when mismatched, then activated before sushy starts")
	}
	probe, ok := sushyTasks[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", sushyTasks[probeIdx]["name"])
	}
	for _, want := range []string{"virsh", "-c", "{{ bootwright_bmc_libvirt_uri }}", "pool-dumpxml", "bootwright-{{ bootwright_bmc_provider_name }}-vmedia"} {
		if !stringListContains(probe["argv"], want) {
			t.Fatalf("%s command missing %q: %v", sushyTasks[probeIdx]["name"], want, probe["argv"])
		}
	}
	decide, ok := sushyTasks[decideIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", sushyTasks[decideIdx]["name"])
	}
	if got := fmt.Sprint(decide["bootwright_bmc_vmedia_pool_redefine"]); !strings.Contains(got, "bootwright_bmc_vmedia_root") || !strings.Contains(got, "<path>") {
		t.Fatalf("%s must compare existing pool target path to bootwright_bmc_vmedia_root, got %v", sushyTasks[decideIdx]["name"], got)
	}
	for _, idx := range []int{stopIdx, inactiveIdx, absentIdx} {
		if got := sushyTasks[idx]["when"]; got != "bootwright_bmc_vmedia_pool_redefine | bool" {
			t.Fatalf("%s when got %v", sushyTasks[idx]["name"], got)
		}
	}
	ensure, ok := sushyTasks[ensureIdx]["ansible.builtin.systemd_service"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no systemd body", sushyTasks[ensureIdx]["name"])
	}
	if got := fmt.Sprint(ensure["state"]); !strings.Contains(got, "bootwright_bmc_vmedia_pool_redefine") || !strings.Contains(got, "bootwright_bmc_vmedia_pool_define.changed") || !strings.Contains(got, "bootwright_bmc_vmedia_pool_activate.changed") {
		t.Fatalf("%s must restart on vmedia pool replacement, got %v", sushyTasks[ensureIdx]["name"], got)
	}

	for _, body := range []string{poolXML, systemd} {
		if !strings.Contains(body, "{{ bootwright_bmc_vmedia_root }}") {
			t.Fatalf("emulated BMC vmedia templates must use bootwright_bmc_vmedia_root")
		}
		if strings.Contains(body, "<path>{{ bootwright_bmc_root }}/vmedia</path>") || strings.Contains(body, "--directory {{ bootwright_bmc_root }}/vmedia") {
			t.Fatalf("emulated BMC vmedia templates must not use bootwright_bmc_root/vmedia")
		}
	}

	destroyIdx := findAnsibleTask(t, destroyTasks, "Remove BMC state directory")
	if !stringListContains(destroyTasks[destroyIdx]["loop"], "{{ bootwright_bmc_vmedia_root }}") {
		t.Fatalf("%s must remove the separate vmedia root, got %v", destroyTasks[destroyIdx]["name"], destroyTasks[destroyIdx]["loop"])
	}
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

func TestStoragePlaybookDispatchesCephadmRole(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_apply.yml")
	if len(plays) != 1 {
		t.Fatalf("storage apply playbook has %d plays, want 1", len(plays))
	}
	if got := plays[0]["hosts"]; got != "bootwright_storage_hosts" {
		t.Fatalf("storage apply play hosts = %v, want bootwright_storage_hosts", got)
	}
	tasks, ok := plays[0]["tasks"].([]any)
	if !ok {
		t.Fatalf("storage apply play has no tasks")
	}
	decoded := make([]map[string]any, 0, len(tasks))
	for i, task := range tasks {
		item, ok := task.(map[string]any)
		if !ok {
			t.Fatalf("storage apply task %d is not a map", i)
		}
		decoded = append(decoded, item)
	}
	assertIncludeRoleName(t, decoded[findAnsibleTask(t, decoded, "Apply Ceph storage cluster")], "bootwright.core.storage_cluster_cephadm")
}

func TestStorageCephadmRoleKeepsSecretsAndArtifactsBounded(t *testing.T) {
	mainTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/main.yml")
	registryLogin := mainTasks[findAnsibleTask(t, mainTasks, "Log storage node into cephadm registry")]
	if got := registryLogin["no_log"]; got != true {
		t.Fatalf("registry login must be no_log, got %v", got)
	}
	if _, ok := registryLogin["containers.podman.podman_login"].(map[string]any); !ok {
		t.Fatalf("registry login must use podman_login, got %v", registryLogin)
	}

	prereqs := mainTasks[findAnsibleTask(t, mainTasks, "Install Ceph prerequisites on storage node")]
	packages, ok := prereqs["ansible.builtin.package"].(map[string]any)
	if !ok {
		t.Fatalf("Ceph prerequisite task missing package module: %v", prereqs)
	}
	for _, name := range []string{"cephadm", "podman", "lvm2", "chrony", "firewalld"} {
		if !stringListContains(packages["name"], name) {
			t.Fatalf("Ceph prerequisite packages missing %s: %v", name, packages["name"])
		}
	}
	if got := fmt.Sprint(prereqs["delegate_to"]); strings.Contains(got, "item.inventoryHost") || got != "<nil>" {
		t.Fatalf("Ceph prerequisites must run on the current storage host, got delegate_to %v", got)
	}

	block := nestedAnsibleTasks(t, mainTasks[findAnsibleTask(t, mainTasks, "Apply managed Ceph cluster through cephadm")], "block")
	for _, name := range []string{
		"Copy cephadm cluster private SSH key",
		"Copy cephadm cluster public SSH key",
		"Capture base Data Foundation external-cluster secrets",
		"Write captured storage result",
	} {
		task := block[findAnsibleTask(t, block, name)]
		if got := task["no_log"]; got != true {
			t.Fatalf("%s must be no_log, got %v", name, got)
		}
	}
	coreIdx := findAnsibleTask(t, block, "Apply Ceph core service spec")
	topologyIdx := findAnsibleTask(t, block, "Run rendered Ceph topology and storage operations")
	lateIdx := findAnsibleTask(t, block, "Apply Ceph late service spec")
	lateOpsIdx := findAnsibleTask(t, block, "Run rendered Ceph late operations")
	if !(coreIdx < topologyIdx && topologyIdx < lateIdx && lateIdx < lateOpsIdx) {
		t.Fatalf("storage operations must be ordered core -> topology/storage -> late services -> late operations")
	}
	if got := fmt.Sprint(block[topologyIdx]["when"]); !strings.Contains(got, "topology") || !strings.Contains(got, "storage") {
		t.Fatalf("topology/storage operation loop has unexpected when=%v", block[topologyIdx]["when"])
	}
	if got := fmt.Sprint(block[lateOpsIdx]["when"]); !strings.Contains(got, "object-gateway") || !strings.Contains(got, "data-foundation") {
		t.Fatalf("late operation loop has unexpected when=%v", block[lateOpsIdx]["when"])
	}

	write := block[findAnsibleTask(t, block, "Write captured storage result")]
	copyTask, ok := write["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("Write captured storage result has no copy body")
	}
	if got := copyTask["dest"]; !strings.Contains(fmt.Sprint(got), "bootwright_selected_storage_cluster.resultPath") {
		t.Fatalf("storage result must write to rendered resultPath, got %v", got)
	}
	if got := copyTask["mode"]; got != "0600" {
		t.Fatalf("storage result mode = %v, want 0600", got)
	}
	if got := write["delegate_to"]; got != "localhost" {
		t.Fatalf("storage result must be written locally, got delegate_to=%v", got)
	}

	always := nestedAnsibleTasks(t, mainTasks[findAnsibleTask(t, mainTasks, "Apply managed Ceph cluster through cephadm")], "always")
	cleanup := always[findAnsibleTask(t, always, "Remove managed Ceph work directory")]
	fileTask, ok := cleanup["ansible.builtin.file"].(map[string]any)
	if !ok || fileTask["state"] != "absent" {
		t.Fatalf("storage role must clean the remote work directory, got %v", cleanup)
	}

	operationTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/run.yml")
	run := operationTasks[findAnsibleTask(t, operationTasks, "Run Ceph operation")]
	command, ok := run["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("Run Ceph operation has no command body")
	}
	if got := command["argv"]; got != "{{ bootwright_ceph_op_command }}" {
		t.Fatalf("Run Ceph operation must consume rendered argv, got %v", got)
	}
	if _, ok := run["changed_when"]; !ok {
		t.Fatalf("Run Ceph operation must declare changed_when")
	}
	if _, ok := run["no_log"]; !ok {
		t.Fatalf("Run Ceph operation must redact captured output")
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

func TestInstallAgentControllerDNSDoesNotMutateHostsFile(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/stage/controller_dns.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_destroy/tasks/main.yml",
	} {
		body := readRepoFile(t, path)
		for _, forbidden := range []string{"/etc/hosts", "blockinfile", "unsafe_writes"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s must not mutate controller hosts file; found %q", path, forbidden)
			}
		}
	}
}

func TestInstallAgentOverrideBypassesExistingClusterSkipGuard(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/stage/skip_guard.yml")
	for _, want := range []string{
		"not (bootwright_install_override | default(false) | bool)",
		"bootwright_install_already_complete",
		"bootwright_install_cluster_available.stdout",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("skip guard missing %q", want)
		}
	}
}

func TestInstallAgentValidatesActionBeforeInputs(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/main.yml")
	selectIdx := findAnsibleTask(t, tasks, "Select agent install action")
	validateActionIdx := findAnsibleTask(t, tasks, "Validate selected agent install action")
	validateInputsIdx := findAnsibleTask(t, tasks, "Validate installer inputs")
	runIdx := findAnsibleTask(t, tasks, "Run selected agent install action")
	if !(selectIdx < validateActionIdx && validateActionIdx < validateInputsIdx && validateInputsIdx < runIdx) {
		t.Fatalf("install_agent must select and validate action before validating action-specific inputs")
	}
}

func TestInstallAgentSkipsConsumedInstallConfigForBootAction(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/stage/validate.yml")
	installStat := tasks[findAnsibleTask(t, tasks, "Verify rendered install-config exists")]
	installFail := tasks[findAnsibleTask(t, tasks, "Fail when install-config is missing")]
	tokenStat := tasks[findAnsibleTask(t, tasks, "Verify generated agent ISO publish token exists")]
	tokenFail := tasks[findAnsibleTask(t, tasks, "Fail when generated agent ISO publish token is missing")]

	if got, _ := installStat["when"].(string); got != "bootwright_install_agent_action_effective in ['run', 'create_iso']" {
		t.Fatalf("install-config stat when got %v", installStat["when"])
	}
	when, ok := installFail["when"].(string)
	if !ok {
		t.Fatalf("install-config failure when got %v", installFail["when"])
	}
	for _, want := range []string{
		"bootwright_install_agent_action_effective in ['run', 'create_iso']",
		"not bootwright_install_config_stat.stat.exists",
	} {
		if !strings.Contains(when, want) {
			t.Fatalf("install-config failure when missing %q: %v", want, installFail["when"])
		}
	}
	failBody := fmt.Sprint(installFail["ansible.builtin.fail"])
	if strings.Contains(failBody, "--resolve-secrets") || !strings.Contains(failBody, "--sensitive") {
		t.Fatalf("install-config failure message must mention --sensitive and not removed --resolve-secrets: %v", installFail["ansible.builtin.fail"])
	}

	if findAnsibleTaskIndex(tasks, "Verify generated agent ISO path exists") >= 0 {
		t.Fatalf("boot action must not require a local ISO path marker")
	}
	if !stringListContains(tokenStat["when"], "bootwright_install_agent_action_effective == 'boot_machine'") {
		t.Fatalf("agent ISO token stat when got %v", tokenStat["when"])
	}
	for _, want := range []string{
		"bootwright_install_agent_action_effective == 'boot_machine'",
		"not bootwright_install_already_complete",
		"not bootwright_agent_iso_publish_token_stat.stat.exists",
	} {
		if !stringListContains(tokenFail["when"], want) {
			t.Fatalf("agent ISO token failure when missing %q: %v", want, tokenFail["when"])
		}
	}
}

func TestInstallAgentStagesExtraManifestsWhenPresent(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/stage/inputs.yml")
	stat := tasks[findAnsibleTask(t, tasks, "Check local installer extra manifests")]
	remove := tasks[findAnsibleTask(t, tasks, "Remove stale remote installer extra manifests")]
	stage := tasks[findAnsibleTask(t, tasks, "Stage installer extra manifests on controller")]

	if got := fmt.Sprint(stat["delegate_to"]); got != "localhost" {
		t.Fatalf("extra manifest stat delegate_to = %v", got)
	}
	copyTask, ok := stage["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("stage extra manifests has no copy body: %v", stage)
	}
	if got := copyTask["src"]; got != "{{ bootwright_install_local_work_dir }}/openshift/" {
		t.Fatalf("extra manifest copy src = %v", got)
	}
	if got := copyTask["dest"]; got != "{{ bootwright_install_work_dir }}/openshift/" {
		t.Fatalf("extra manifest copy dest = %v", got)
	}
	if got := copyTask["directory_mode"]; got != "0700" {
		t.Fatalf("extra manifest directory_mode = %v", got)
	}
	if got := fmt.Sprint(remove["when"]); !strings.Contains(got, "run") || !strings.Contains(got, "create_iso") {
		t.Fatalf("remove stale manifests when = %v", remove["when"])
	}
	if !stringListContains(stage["when"], "bootwright_install_extra_manifests_stat.stat.isdir | default(false)") {
		t.Fatalf("extra manifest stage when = %v", stage["when"])
	}
}

func TestInstallAgentFetchesAgentISOWithoutBecome(t *testing.T) {
	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/create_iso.yml")
	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Create cluster agent ISO when install is not already complete")], "block")
	localDirIdx := findAnsibleTask(t, tasks, "Create local installer artifact directory")
	previousTokenIdx := findAnsibleTask(t, tasks, "Read previous agent ISO publish token")
	previousCleanupIdx := findAnsibleTask(t, tasks, "Remove previous staged agent ISO publish directories")
	generateTokenIdx := findAnsibleTask(t, tasks, "Generate agent ISO publish token")
	recordTokenIdx := findAnsibleTask(t, tasks, "Record agent ISO publish token")
	directTargetIdx := findAnsibleTask(t, tasks, "Resolve direct agent ISO publish target")
	directPathIdx := findAnsibleTask(t, tasks, "Resolve direct agent ISO stage path")
	directDirIdx := findAnsibleTask(t, tasks, "Resolve direct agent ISO staging directory")
	createDirectDirIdx := findAnsibleTask(t, tasks, "Create direct agent ISO staging directory")
	removeDirectPathIdx := findAnsibleTask(t, tasks, "Remove direct staged agent ISO output path")
	linkDirectPathIdx := findAnsibleTask(t, tasks, "Link installer agent ISO output to direct publish path")
	createISOIdx := findAnsibleTask(t, tasks, "Create agent ISO")
	statDirectIdx := findAnsibleTask(t, tasks, "Stat direct generated agent ISO")
	locateISOIdx := findAnsibleTask(t, tasks, "Locate generated agent ISO")
	setPathIdx := findAnsibleTask(t, tasks, "Set agent ISO path")
	decisionIdx := findAnsibleTask(t, tasks, "Determine whether local agent ISO transfer is needed")
	userIdx := findAnsibleTask(t, tasks, "Resolve SSH transfer user")
	transferIdx := findAnsibleTask(t, tasks, "Transfer generated agent ISO to local runtime state")
	recordIdx := findAnsibleTask(t, tasks, "Record local generated agent ISO path")
	setLocalIdx := findAnsibleTask(t, tasks, "Set local generated agent ISO path")
	publishIdx := findAnsibleTask(t, tasks, "Publish generated agent ISO")
	removeLocalIdx := findAnsibleTask(t, tasks, "Remove local generated agent ISO transfer copy")
	if !(localDirIdx < previousTokenIdx && previousTokenIdx < previousCleanupIdx && previousCleanupIdx < generateTokenIdx && generateTokenIdx < recordTokenIdx && recordTokenIdx < directTargetIdx && directTargetIdx < directPathIdx && directPathIdx < directDirIdx && directDirIdx < createDirectDirIdx && createDirectDirIdx < removeDirectPathIdx && removeDirectPathIdx < linkDirectPathIdx && linkDirectPathIdx < createISOIdx && createISOIdx < statDirectIdx && statDirectIdx < locateISOIdx && locateISOIdx < setPathIdx && setPathIdx < decisionIdx && decisionIdx < userIdx && userIdx < transferIdx && transferIdx < recordIdx && recordIdx < setLocalIdx && setLocalIdx < publishIdx && publishIdx < removeLocalIdx) {
		t.Fatalf("install_agent must clean stale publish state, transfer only when needed, publish, then remove local ISO copies")
	}

	if got := tasks[previousTokenIdx]["delegate_to"]; got != "localhost" {
		t.Fatalf("previous token read must run locally, got %v", got)
	}
	if got := tasks[previousTokenIdx]["failed_when"]; got != false {
		t.Fatalf("previous token read must tolerate absent state, got %v", got)
	}
	if got := tasks[previousCleanupIdx]["ansible.builtin.include_tasks"]; got != "../iso/cleanup_target.yml" {
		t.Fatalf("previous publish cleanup must reuse iso/cleanup_target.yml, got %v", got)
	}

	if got := tasks[recordTokenIdx]["delegate_to"]; got != "localhost" {
		t.Fatalf("publish token record must run locally, got %v", got)
	}
	if got := tasks[recordTokenIdx]["no_log"]; got != true {
		t.Fatalf("publish token record must be hidden, got %v", got)
	}
	directTarget, ok := tasks[directTargetIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[directTargetIdx]["name"])
	}
	if !strings.Contains(fmt.Sprint(directTarget["bootwright_agent_iso_direct_publish_target"]), "selectattr('stageHost', 'equalto', bootwright_host_name | default(inventory_hostname))") {
		t.Fatalf("direct publish target must select same-host stage targets, got %v", directTarget)
	}
	directPath, ok := tasks[directPathIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[directPathIdx]["name"])
	}
	if !strings.Contains(fmt.Sprint(directPath["bootwright_agent_iso_direct_stage_path"]), "replace(bootwright_agent_iso_publish_token_placeholder, bootwright_agent_iso_publish_token)") {
		t.Fatalf("direct stage path must substitute the generated token, got %v", directPath)
	}
	linkDirect, ok := tasks[linkDirectPathIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", tasks[linkDirectPathIdx]["name"])
	}
	if got := linkDirect["src"]; got != "{{ bootwright_agent_iso_direct_stage_path }}" {
		t.Fatalf("direct ISO link src got %v", got)
	}
	if got := linkDirect["dest"]; got != "{{ bootwright_install_work_dir }}/agent.x86_64.iso" {
		t.Fatalf("direct ISO link dest got %v", got)
	}
	if got := linkDirect["state"]; got != "link" {
		t.Fatalf("direct ISO output must be a symlink, got %v", got)
	}
	if got := linkDirect["force"]; got != true {
		t.Fatalf("direct ISO output link must replace stale paths, got %v", got)
	}
	pathFact, ok := tasks[setPathIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[setPathIdx]["name"])
	}
	if !strings.Contains(fmt.Sprint(pathFact["bootwright_agent_iso_path"]), "bootwright_agent_iso_direct_stage_path") {
		t.Fatalf("generated ISO path must prefer direct publish output, got %v", pathFact)
	}

	decision, ok := tasks[decisionIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[decisionIdx]["name"])
	}
	if !strings.Contains(fmt.Sprint(decision["bootwright_agent_iso_requires_local_copy"]), "rejectattr('stageHost', 'equalto', bootwright_host_name | default(inventory_hostname))") {
		t.Fatalf("local copy decision must only require transfer for off-host publish targets, got %v", decision)
	}

	if got := tasks[userIdx]["become"]; got != false {
		t.Fatalf("transfer user detection must run without become, got %v", got)
	}
	if got := tasks[userIdx]["when"]; got != "bootwright_agent_iso_requires_local_copy | bool" {
		t.Fatalf("transfer user detection must only run when a local copy is needed, got %v", got)
	}
	userCommand, ok := tasks[userIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", tasks[userIdx]["name"])
	}
	if !stringListContains(userCommand["argv"], "id") || !stringListContains(userCommand["argv"], "-un") {
		t.Fatalf("transfer user detection must resolve the SSH user with id -un, got %v", userCommand["argv"])
	}
	if got := tasks[transferIdx]["when"]; got != "bootwright_agent_iso_requires_local_copy | bool" {
		t.Fatalf("agent ISO transfer must only run when a local copy is needed, got %v", got)
	}

	transferTasks := nestedAnsibleTasks(t, tasks[transferIdx], "block")
	tempIdx := findAnsibleTask(t, transferTasks, "Create remote agent ISO transfer file")
	stageIdx := findAnsibleTask(t, transferTasks, "Stage generated agent ISO for unprivileged transfer")
	fetchIdx := findAnsibleTask(t, transferTasks, "Fetch generated agent ISO to local runtime state")
	chmodIdx := findAnsibleTask(t, transferTasks, "Restrict local generated agent ISO permissions")
	if !(tempIdx < stageIdx && stageIdx < fetchIdx && fetchIdx < chmodIdx) {
		t.Fatalf("agent ISO transfer block must create temp file, stage readable copy, fetch, then restrict local permissions")
	}

	stageCommand, ok := transferTasks[stageIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", transferTasks[stageIdx]["name"])
	}
	for _, want := range []string{"install", "-m", "0600", "-o", "{{ bootwright_iso_transfer_user.stdout | trim }}", "{{ bootwright_agent_iso_path }}", "{{ bootwright_agent_iso_transfer_file.path }}"} {
		if !stringListContains(stageCommand["argv"], want) {
			t.Fatalf("agent ISO staging command missing %q: %v", want, stageCommand["argv"])
		}
	}

	fetch, ok := transferTasks[fetchIdx]["ansible.builtin.fetch"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no fetch body", transferTasks[fetchIdx]["name"])
	}
	if got := transferTasks[fetchIdx]["become"]; got != false {
		t.Fatalf("large ISO fetch must run without become, got %v", got)
	}
	if got := fetch["src"]; got != "{{ bootwright_agent_iso_transfer_file.path }}" {
		t.Fatalf("large ISO fetch must read the unprivileged transfer file, got %v", got)
	}

	cleanupTasks := nestedAnsibleTasks(t, tasks[transferIdx], "always")
	cleanupIdx := findAnsibleTask(t, cleanupTasks, "Remove remote agent ISO transfer file")
	cleanup, ok := cleanupTasks[cleanupIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", cleanupTasks[cleanupIdx]["name"])
	}
	if cleanup["path"] != "{{ bootwright_agent_iso_transfer_file.path }}" || cleanup["state"] != "absent" {
		t.Fatalf("agent ISO transfer cleanup got %v", cleanup)
	}

	if got := tasks[setLocalIdx]["when"]; got != "bootwright_agent_iso_requires_local_copy | bool" {
		t.Fatalf("local ISO path fact must only be set when transfer ran, got %v", got)
	}
	removeLocal, ok := tasks[removeLocalIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", tasks[removeLocalIdx]["name"])
	}
	if got := removeLocal["path"]; got != "{{ bootwright_agent_iso_local_path | default('') }}" {
		t.Fatalf("local ISO cleanup path got %v", got)
	}
	if got := tasks[removeLocalIdx]["delegate_to"]; got != "localhost" {
		t.Fatalf("local ISO cleanup must run locally, got %v", got)
	}
}

func TestInstallAgentCleansGeneratedISOArtifactsAfterSuccessfulWait(t *testing.T) {
	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/wait_install.yml")
	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Wait for agent install completion when install is not already complete")], "block")
	recordIdx := findAnsibleTask(t, tasks, "Record local kubeconfig path")
	cleanRedfishIdx := findAnsibleTask(t, tasks, "Clean Redfish virtual media after successful install")
	findRemoteIdx := findAnsibleTask(t, tasks, "Find remote generated agent ISO files")
	removeRemoteIdx := findAnsibleTask(t, tasks, "Remove remote generated agent ISO files")
	removeBootArtifactsIdx := findAnsibleTask(t, tasks, "Remove remote generated boot artifacts")
	findLocalIdx := findAnsibleTask(t, tasks, "Find local generated agent ISO files")
	removeLocalIdx := findAnsibleTask(t, tasks, "Remove local generated agent ISO files")
	removeRemotePathIdx := findAnsibleTask(t, tasks, "Remove remote agent ISO path record")
	removeLocalPathIdx := findAnsibleTask(t, tasks, "Remove local agent ISO path record")
	if !(recordIdx < cleanRedfishIdx && cleanRedfishIdx < findRemoteIdx && findRemoteIdx < removeRemoteIdx && removeRemoteIdx < removeBootArtifactsIdx && removeBootArtifactsIdx < findLocalIdx && findLocalIdx < removeLocalIdx && removeLocalIdx < removeRemotePathIdx && removeRemotePathIdx < removeLocalPathIdx) {
		t.Fatalf("wait_install must fetch credentials before removing generated ISO artifacts")
	}
	assertIncludeRoleName(t, tasks[cleanRedfishIdx], "bootwright.core.container_cluster_boot_redfish")
	if got := tasks[cleanRedfishIdx]["loop"]; !strings.Contains(fmt.Sprint(got), "bootApplyRole") || !strings.Contains(fmt.Sprint(got), "bootwright.core.container_cluster_boot_redfish") {
		t.Fatalf("Redfish cleanup must loop over boot_redfish components, got %v", got)
	}
	cleanVars, ok := tasks[cleanRedfishIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no vars", tasks[cleanRedfishIdx]["name"])
	}
	if got := cleanVars["bootwright_component"]; got != "{{ bootwright_cleanup_redfish_component }}" {
		t.Fatalf("Redfish cleanup component var got %v", got)
	}
	if got := cleanVars["bootwright_redfish_action"]; got != "cleanup_media" {
		t.Fatalf("Redfish cleanup action got %v", got)
	}
	cleanLoopControl, ok := tasks[cleanRedfishIdx]["loop_control"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no loop_control", tasks[cleanRedfishIdx]["name"])
	}
	if got := cleanLoopControl["loop_var"]; got != "bootwright_cleanup_redfish_component" {
		t.Fatalf("Redfish cleanup loop_var got %v", got)
	}
	if got := cleanLoopControl["label"]; got != "{{ bootwright_cleanup_redfish_component.name }}" {
		t.Fatalf("Redfish cleanup label got %v", got)
	}

	remoteFind, ok := tasks[findRemoteIdx]["ansible.builtin.find"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no find body", tasks[findRemoteIdx]["name"])
	}
	if remoteFind["paths"] != "{{ bootwright_install_work_dir }}" || remoteFind["patterns"] != "agent.*.iso" {
		t.Fatalf("remote ISO cleanup find got %v", remoteFind)
	}
	bootArtifacts, ok := tasks[removeBootArtifactsIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", tasks[removeBootArtifactsIdx]["name"])
	}
	if bootArtifacts["path"] != "{{ bootwright_install_work_dir }}/boot-artifacts" || bootArtifacts["state"] != "absent" {
		t.Fatalf("boot artifact cleanup got %v", bootArtifacts)
	}
	if got := tasks[findLocalIdx]["delegate_to"]; got != "localhost" {
		t.Fatalf("local ISO cleanup find must run locally, got %v", got)
	}
	if got := tasks[removeLocalIdx]["delegate_to"]; got != "localhost" {
		t.Fatalf("local ISO cleanup must run locally, got %v", got)
	}
	if got := tasks[removeLocalPathIdx]["delegate_to"]; got != "localhost" {
		t.Fatalf("local ISO path cleanup must run locally, got %v", got)
	}
	for _, idx := range []int{cleanRedfishIdx, findRemoteIdx, removeRemoteIdx, removeBootArtifactsIdx, findLocalIdx, removeLocalIdx, removeRemotePathIdx, removeLocalPathIdx} {
		if got := tasks[idx]["when"]; got != "bootwright_install_wait.rc == 0" {
			t.Fatalf("%s must only clean after successful wait, got when=%v", tasks[idx]["name"], got)
		}
	}
}

func TestInstallAgentWaitPollsPromptly(t *testing.T) {
	var defaults map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/defaults/main.yml")), &defaults); err != nil {
		t.Fatalf("decode install_agent defaults: %v", err)
	}
	if got := defaults["bootwright_install_wait_poll_seconds"]; got != 15 {
		t.Fatalf("install wait poll default got %v, want 15", got)
	}

	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/wait_install.yml")
	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Wait for agent install completion when install is not already complete")], "block")
	wait := tasks[findAnsibleTask(t, tasks, "Wait for agent install completion")]
	if got := wait["async"]; got != "{{ bootwright_install_timeout_seconds }}" {
		t.Fatalf("install wait async got %v", got)
	}
	if got := wait["poll"]; got != "{{ bootwright_install_wait_poll_seconds }}" {
		t.Fatalf("install wait poll got %v", got)
	}
}

func TestInstallAgentSavesKubeadminPasswordAsClusterSecret(t *testing.T) {
	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/wait_install.yml")
	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Wait for agent install completion when install is not already complete")], "block")

	ensureIdx := findAnsibleTask(t, tasks, "Create local cluster secrets directory")
	ensure, ok := tasks[ensureIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", tasks[ensureIdx]["name"])
	}
	if got := ensure["path"]; got != "{{ bootwright_cluster_secrets_dir }}" {
		t.Fatalf("cluster secrets dir path got %v", got)
	}
	if got := ensure["mode"]; got != "0700" {
		t.Fatalf("cluster secrets dir mode got %v", got)
	}

	saveIdx := findAnsibleTask(t, tasks, "Store kubeadmin password in cluster secrets")
	copyTask, ok := tasks[saveIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no copy body", tasks[saveIdx]["name"])
	}
	if got := copyTask["src"]; got != "{{ bootwright_install_local_work_dir }}/auth/kubeadmin-password" {
		t.Fatalf("kubeadmin password source got %v", got)
	}
	if got := copyTask["dest"]; got != "{{ bootwright_cluster_secrets_dir }}/kubeadmin-password" {
		t.Fatalf("kubeadmin password dest got %v", got)
	}
	if got := copyTask["mode"]; got != "0600" {
		t.Fatalf("kubeadmin password mode got %v", got)
	}
	for _, idx := range []int{ensureIdx, saveIdx} {
		if got := tasks[idx]["delegate_to"]; got != "localhost" {
			t.Fatalf("%s must run locally, got %v", tasks[idx]["name"], got)
		}
		if got := tasks[idx]["become"]; got != false {
			t.Fatalf("%s must not become remotely, got %v", tasks[idx]["name"], got)
		}
		if got := tasks[idx]["when"]; got != "bootwright_install_wait.rc == 0" {
			t.Fatalf("%s must only run after successful wait, got %v", tasks[idx]["name"], got)
		}
	}
}

func TestDestroyClusterRemovesClusterInstallerRuntimeDir(t *testing.T) {
	body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_destroy/tasks/main.yml")
	for _, want := range []string{
		"bootwright_cluster_installer_runtime_dir: \"{{ bootwright_clusters_dir }}/{{ bootwright_current_cluster.name }}/runtime/installer\"",
		"bootwright_process_cleanup_pattern: \"clusters/{{ bootwright_current_cluster.name }}/runtime/installer/\"",
		"path: \"{{ bootwright_cluster_installer_runtime_dir }}\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("destroy_agent missing %q", want)
		}
	}
}

func TestHostBaseFirewalldAvailabilityRequiresRunningDaemon(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_base/tasks/main.yml")
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

func ansibleCfgValue(body, section, key string) (string, bool) {
	current := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			continue
		}
		if current != section {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == key {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

func readAnsibleTasks(t *testing.T, rel string) []map[string]any {
	t.Helper()
	var tasks []map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, rel)), &tasks); err != nil {
		t.Fatalf("%s: decode YAML: %v", rel, err)
	}
	return tasks
}

func readAnsiblePlays(t *testing.T, rel string) []map[string]any {
	t.Helper()
	var plays []map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, rel)), &plays); err != nil {
		t.Fatalf("%s: decode YAML: %v", rel, err)
	}
	return plays
}

func nestedAnsibleTasks(t *testing.T, task map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := task[key].([]any)
	if !ok {
		t.Fatalf("%s has no %s task list", task["name"], key)
	}
	tasks := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		child, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s %s[%d] is not a task map", task["name"], key, i)
		}
		tasks = append(tasks, child)
	}
	return tasks
}

func checkShellTaskGuards(t *testing.T, rel string, tasks []map[string]any) {
	t.Helper()
	for _, task := range tasks {
		if _, ok := task["ansible.builtin.shell"]; ok {
			if _, ok := task["changed_when"]; !ok {
				t.Errorf("%s task %q uses ansible.builtin.shell without changed_when", rel, task["name"])
			}
			if _, ok := task["failed_when"]; !ok {
				t.Errorf("%s task %q uses ansible.builtin.shell without failed_when", rel, task["name"])
			}
		}
		for _, key := range []string{"block", "rescue", "always"} {
			raw, ok := task[key].([]any)
			if !ok {
				continue
			}
			children := make([]map[string]any, 0, len(raw))
			for i, item := range raw {
				child, ok := item.(map[string]any)
				if !ok {
					t.Fatalf("%s task %q %s[%d] is not a task map", rel, task["name"], key, i)
				}
				children = append(children, child)
			}
			checkShellTaskGuards(t, rel, children)
		}
	}
}

func collectAnsibleMessages(tasks []map[string]any, out *[]string) {
	for _, task := range tasks {
		for _, module := range []string{"ansible.builtin.assert", "ansible.builtin.debug", "ansible.builtin.fail"} {
			switch body := task[module].(type) {
			case map[string]any:
				for _, key := range []string{"fail_msg", "success_msg", "msg"} {
					if value, ok := body[key]; ok {
						*out = append(*out, fmt.Sprint(value))
					}
				}
			case string:
				*out = append(*out, body)
			}
		}
		for _, key := range []string{"block", "rescue", "always"} {
			raw, ok := task[key].([]any)
			if !ok {
				continue
			}
			children := make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				child, ok := item.(map[string]any)
				if ok {
					children = append(children, child)
				}
			}
			collectAnsibleMessages(children, out)
		}
	}
}

func hasHostProxyFactsImport(tasks []any) bool {
	for _, raw := range tasks {
		task, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if task["name"] != "Resolve proxy environment" {
			continue
		}
		importRole, ok := task["ansible.builtin.import_role"].(map[string]any)
		if !ok {
			continue
		}
		if importRole["name"] == "bootwright.core.machine_proxy" && importRole["tasks_from"] == "facts" {
			return true
		}
	}
	return false
}

func findAnsibleTask(t *testing.T, tasks []map[string]any, name string) int {
	t.Helper()
	if idx := findAnsibleTaskIndex(tasks, name); idx >= 0 {
		return idx
	}
	t.Fatalf("missing Ansible task %q", name)
	return -1
}

func findAnsibleTaskIndex(tasks []map[string]any, name string) int {
	for i, task := range tasks {
		if got, _ := task["name"].(string); got == name {
			return i
		}
	}
	return -1
}

func assertIncludeTasksFile(t *testing.T, task map[string]any, want string) {
	t.Helper()
	include := task["ansible.builtin.include_tasks"]
	if include == nil {
		include = task["ansible.builtin.import_tasks"]
	}
	switch got := include.(type) {
	case string:
		if strings.TrimSpace(got) != want {
			t.Fatalf("%s tasks file got %q, want %q", task["name"], got, want)
		}
	case map[string]any:
		file, ok := got["file"].(string)
		if !ok {
			t.Fatalf("%s tasks include/import has no file", task["name"])
		}
		if strings.TrimSpace(file) != want {
			t.Fatalf("%s tasks file got %q, want %q", task["name"], file, want)
		}
	default:
		t.Fatalf("%s is not an include_tasks or import_tasks task", task["name"])
	}
}

func assertIncludeTasksApplyWhen(t *testing.T, task map[string]any, want string) {
	t.Helper()
	include, ok := task["ansible.builtin.include_tasks"].(map[string]any)
	if !ok {
		t.Fatalf("%s include_tasks has no apply block", task["name"])
	}
	apply, ok := include["apply"].(map[string]any)
	if !ok {
		t.Fatalf("%s include_tasks has no apply block", task["name"])
	}
	when, ok := apply["when"].(string)
	if !ok {
		t.Fatalf("%s include_tasks apply block has no when", task["name"])
	}
	if got := strings.TrimSpace(when); got != want {
		t.Fatalf("%s include_tasks apply.when got %q, want %q", task["name"], got, want)
	}
}

func assertIncludeRoleName(t *testing.T, task map[string]any, want string) {
	t.Helper()
	include, ok := task["ansible.builtin.include_role"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an include_role task", task["name"])
	}
	if got := strings.TrimSpace(include["name"].(string)); got != want {
		t.Fatalf("%s include_role got %q", task["name"], got)
	}
}

func assertSleepCommand(t *testing.T, task map[string]any, seconds string) {
	t.Helper()
	if _, ok := task["ansible.builtin.pause"]; ok {
		t.Fatalf("%s must not use pause because free strategy does not support host-loop-bypass modules", task["name"])
	}
	command, ok := task["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", task["name"])
	}
	argv, ok := command["argv"].([]any)
	if !ok || len(argv) != 2 || fmt.Sprint(argv[0]) != "sleep" || fmt.Sprint(argv[1]) != seconds {
		t.Fatalf("%s command argv got %v, want sleep %s", task["name"], command["argv"], seconds)
	}
	if changed, ok := task["changed_when"].(bool); !ok || changed {
		t.Fatalf("%s must set changed_when: false, got %v", task["name"], task["changed_when"])
	}
}

func assertURIHeader(t *testing.T, task map[string]any, name, want string) {
	t.Helper()
	uri, ok := task["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", task["name"])
	}
	headers, ok := uri["headers"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no headers", task["name"])
	}
	if got := headers[name]; got != want {
		t.Fatalf("%s header %s got %v, want %q", task["name"], name, got, want)
	}
}

func stringListContains(v any, want string) bool {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if item == want {
				return true
			}
		}
	case []string:
		for _, item := range x {
			if item == want {
				return true
			}
		}
	case string:
		return x == want
	}
	return false
}

func stringListItemContains(v any, want string) bool {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if text, ok := item.(string); ok && strings.Contains(text, want) {
				return true
			}
		}
	case []string:
		for _, item := range x {
			if strings.Contains(item, want) {
				return true
			}
		}
	case string:
		return strings.Contains(x, want)
	}
	return false
}

func intListEqual(v any, want []int) bool {
	var got []int
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			n, ok := item.(int)
			if !ok {
				return false
			}
			got = append(got, n)
		}
	case []int:
		got = x
	default:
		return false
	}
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func registeredAdapterRoles() map[string]bool {
	out := map[string]bool{}
	add := func(role string) {
		if role != "" {
			out[role] = true
		}
	}
	for _, entry := range support.Entries() {
		for _, role := range entry.Roles.MachineSetupRoles {
			add(role)
		}
		add(entry.Roles.SubstrateApplyRole)
		add(entry.Roles.SubstrateDestroyRole)
		add(entry.Roles.BMCApplyRole)
		add(entry.Roles.BMCDestroyRole)
		add(entry.Roles.BootApplyRole)
		add(entry.Roles.MediaPrepareRole)
	}
	for _, entry := range support.ServiceEntries() {
		add(entry.ApplyRole)
		add(entry.DestroyRole)
	}
	return out
}

func ansibleRoleDirExists(t *testing.T, role string) bool {
	t.Helper()
	info, err := os.Stat(filepath.Join(repoRoot(t), filepath.FromSlash(bootwrightCollectionRoleRoot), strings.TrimPrefix(role, "bootwright.core.")))
	return err == nil && info.IsDir()
}

func ansibleAdapterRoleDirs(t *testing.T) []string {
	t.Helper()
	type rule struct {
		base     string
		prefixes []string
	}
	rules := []rule{
		{base: bootwrightCollectionRoleRoot, prefixes: []string{"machine_setup_libvirt"}},
		{base: bootwrightCollectionRoleRoot, prefixes: []string{"provider_service_bmc_"}},
		{base: bootwrightCollectionRoleRoot, prefixes: []string{"infra_component_"}},
		{base: bootwrightCollectionRoleRoot, prefixes: []string{"machine_substrate_"}},
		{base: bootwrightCollectionRoleRoot, prefixes: []string{"container_cluster_boot_", "container_cluster_media_"}},
	}
	var out []string
	for _, r := range rules {
		entries, err := os.ReadDir(filepath.Join(repoRoot(t), r.base))
		if err != nil {
			t.Fatalf("read role dir %s: %v", r.base, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			for _, prefix := range r.prefixes {
				if prefix == "" || strings.HasPrefix(entry.Name(), prefix) {
					out = append(out, "bootwright.core."+entry.Name())
					break
				}
			}
		}
	}
	return out
}
