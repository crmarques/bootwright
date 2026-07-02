package repocheck

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestBootRedfishLibvirtVirtualMediaDetachFallback(t *testing.T) {
	ejectTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/eject.yml")
	mainTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/main.yml")
	task := ejectTasks[findAnsibleTask(t, ejectTasks, "Clean libvirt virtual media from {{ bootwright_libvirt_media_scope }} domain")]
	script := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/files/eject_libvirt_media.sh")
	insertScript := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/files/insert_libvirt_media.py")

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

	// The final cleanup must detach the whole optical drive, not just eject the
	// medium, so the provisioned guest is left with no leftover /dev/sr0. The
	// "all" mode drops the source requirement so the now-empty drive is also
	// detached; live hot-unplug stays non-fatal while the persistent config is
	// authoritative.
	for _, want := range []string{
		"mode=$7",
		"if [ \"$mode\" = \"all\" ]; then",
		"awk '$2 == \"cdrom\" { print $3 }'",
		"if [ \"$mode\" = \"all\" ] && [ \"$state_arg\" = \"--live\" ]; then",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("libvirt media cleanup script missing detach-drive mode marker %q", want)
		}
	}
	if argv, ok := task["ansible.builtin.command"].(map[string]any); ok {
		if got := fmt.Sprint(argv["argv"]); !strings.Contains(got, "ternary('all', 'source')") {
			t.Fatalf("libvirt media cleanup must pass the detach-drive mode arg, got %v", argv["argv"])
		}
	}
	for _, name := range []string{
		"Clean stale running virtual media before insert",
		"Clean stale persistent virtual media before insert",
	} {
		cleanVars, ok := mainTasks[findAnsibleTask(t, mainTasks, name)]["vars"].(map[string]any)
		if !ok {
			t.Fatalf("%s must pass eject vars", name)
		}
		if got := cleanVars["bootwright_libvirt_media_detach_drive"]; got != "{{ bootwright_libvirt_media_action == 'cleanup' }}" {
			t.Fatalf("%s must detach the drive only on the final cleanup, got %v", name, got)
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
	if !ok || !strings.Contains(argv, "/var/tmp/bootwright-libvirt-media-eject.sh") {
		t.Fatalf("libvirt media cleanup command must invoke installed helper script, got %v", command["argv"])
	}

	for _, want := range []string{
		"tempfile.NamedTemporaryFile(",
		"dir=tmpdir",
		"virsh(uri, \"define\", tmp.name)",
		"remove_cdroms(devices)",
		"add_cdrom(devices, source_iso",
		"set_boot_order(root, boot_order)",
		"devices = [\"cdrom\", \"hd\"] if boot_order == \"cdrom-first\" else [\"hd\", \"cdrom\"]",
		// The parse/serialize round-trip must keep the bootwright metadata prefix,
		// or it re-emits as ns0: and the destroy ownership guard stops recognizing
		// the marker it just round-tripped.
		"ET.register_namespace(\"bootwright\", \"https://bootwright.io/libvirt/metadata/1.0\")",
	} {
		if !strings.Contains(insertScript, want) {
			t.Fatalf("libvirt media insert helper missing %q", want)
		}
	}

	actionIdx := findAnsibleTask(t, mainTasks, "Resolve direct libvirt virtual media action")
	validateActionIdx := findAnsibleTask(t, mainTasks, "Validate direct libvirt virtual media action")
	runningCleanIdx := findAnsibleTask(t, mainTasks, "Clean stale running virtual media before insert")
	persistentCleanIdx := findAnsibleTask(t, mainTasks, "Clean stale persistent virtual media before insert")
	resolveIdx := findAnsibleTask(t, mainTasks, "Resolve direct libvirt virtual media insert state")
	installIdx := findAnsibleTask(t, mainTasks, "Install virtual media insert helper")
	insertIdx := findAnsibleTask(t, mainTasks, "Insert staged virtual media directly into libvirt domain")
	recordIdx := findAnsibleTask(t, mainTasks, "Record direct libvirt virtual media attachment")
	if !(actionIdx < validateActionIdx && validateActionIdx < runningCleanIdx && runningCleanIdx < persistentCleanIdx && persistentCleanIdx < resolveIdx && resolveIdx < installIdx && installIdx < insertIdx && insertIdx < recordIdx) {
		t.Fatalf("libvirt media role must resolve, install helper, insert media, then record backend attachment")
	}
	actionFacts, ok := mainTasks[actionIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", mainTasks[actionIdx]["name"])
	}
	if got := actionFacts["bootwright_libvirt_media_action"]; got != "{{ bootwright_redfish_action_effective | default('boot') }}" {
		t.Fatalf("%s must default to boot action, got %v", mainTasks[actionIdx]["name"], got)
	}
	validateAction, ok := mainTasks[validateActionIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an assert task", mainTasks[validateActionIdx]["name"])
	}
	if got := fmt.Sprint(validateAction["that"]); !strings.Contains(got, "cleanup_persistent") {
		t.Fatalf("%s must allow persistent-only cleanup, got %v", mainTasks[validateActionIdx]["name"], validateAction["that"])
	}
	if got := mainTasks[runningCleanIdx]["when"]; got != "bootwright_libvirt_media_action in ['boot', 'cleanup']" {
		t.Fatalf("%s must not run for persistent-only cleanup, got when=%v", mainTasks[runningCleanIdx]["name"], got)
	}
	if got := mainTasks[persistentCleanIdx]["when"]; got != "bootwright_libvirt_media_action in ['boot', 'cleanup', 'cleanup_persistent']" {
		t.Fatalf("%s must run for persistent-only cleanup, got when=%v", mainTasks[persistentCleanIdx]["name"], got)
	}
	resolveFacts, ok := mainTasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", mainTasks[resolveIdx]["name"])
	}
	if got := fmt.Sprint(resolveFacts["bootwright_libvirt_media_insert_requested"]); !strings.Contains(got, "bootwright_libvirt_media_action") {
		t.Fatalf("%s must only request direct insert for boot actions, got %s", mainTasks[resolveIdx]["name"], got)
	}
	if got := fmt.Sprint(resolveFacts["bootwright_libvirt_media_boot_order"]); !strings.Contains(got, "bootOrder") || !strings.Contains(got, "disk-first") {
		t.Fatalf("%s must resolve optional libvirt media boot order with disk-first default, got %s", mainTasks[resolveIdx]["name"], got)
	}
	insertCommand, ok := mainTasks[insertIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a command task", mainTasks[insertIdx]["name"])
	}
	// stagePath embeds the agent-ISO publish token, so the insert command must
	// redact its output with no_log to keep the token out of stdout/logs.
	assertRedactsByDefault(t, fmt.Sprint(mainTasks[insertIdx]["name"]), mainTasks[insertIdx]["no_log"])
	insertArgv := fmt.Sprint(insertCommand["argv"])
	for _, want := range []string{"/var/tmp/bootwright-libvirt-media-insert.py", "bootwright_component.boot.agentIso.stagePath", "bootwright_libvirt_media_boot_order"} {
		if !strings.Contains(insertArgv, want) {
			t.Fatalf("%s argv missing %q: %v", mainTasks[insertIdx]["name"], want, insertCommand["argv"])
		}
	}
	insertEnv, ok := mainTasks[insertIdx]["environment"].(map[string]any)
	if !ok {
		t.Fatalf("%s must set helper temp environment", mainTasks[insertIdx]["name"])
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if got := insertEnv[key]; got != "{{ bootwright_libvirt_media_stage_dir }}" {
			t.Fatalf("%s environment %s got %v, want bootwright_libvirt_media_stage_dir", mainTasks[insertIdx]["name"], key, got)
		}
	}
	recordFacts, ok := mainTasks[recordIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", mainTasks[recordIdx]["name"])
	}
	if recordFacts["bootwright_redfish_vmedia_backend_attached"] != true || recordFacts["bootwright_redfish_vmedia_backend"] != "libvirt-direct" {
		t.Fatalf("%s must mark direct libvirt virtual media attached, got %v", mainTasks[recordIdx]["name"], recordFacts)
	}
}

func TestRedfishURIRequestsDoNotOverrideProxyEnvironment(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/check_external_reachability/tasks/bmc.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/eject.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/import_certificate.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/import_certificate/standard.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/import_certificate/security_service.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/remove_certificate.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/remove_certificate/standard.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/remove_certificate/security_service.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/post_boot.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/media_insert.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_override.yml",
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

func assertRedfishURIModuleDefaults(t *testing.T, block map[string]any) {
	t.Helper()
	defaults, ok := block["module_defaults"].(map[string]any)
	if !ok {
		t.Fatalf("redfish boot block must set module_defaults, got %v", block["module_defaults"])
	}
	uri, ok := defaults["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("redfish boot block module_defaults must cover ansible.builtin.uri, got %v", defaults)
	}
	for key, want := range map[string]string{
		"url_username":     "bootwright_redfish_credentials.username",
		"url_password":     "bootwright_redfish_credentials.password",
		"validate_certs":   "bootwright_component.boot.redfish.validateCerts",
		"timeout":          "bootwright_redfish_request_timeout",
		"force_basic_auth": "bootwright_redfish_cred_path",
	} {
		if !strings.Contains(fmt.Sprint(uri[key]), want) {
			t.Fatalf("redfish uri module_defaults %q must reference %q, got %v", key, want, uri[key])
		}
	}
}

func TestBootRedfishDispatchesMediaBackendBeforeInsert(t *testing.T) {
	mainFileTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml")
	outerBlockIdx := findAnsibleTask(t, mainFileTasks, "Boot via Redfish with shared request defaults")
	assertRedfishURIModuleDefaults(t, mainFileTasks[outerBlockIdx])
	mainTasks := nestedAnsibleTasks(t, mainFileTasks[outerBlockIdx], "block")
	validateActionIdx := findAnsibleTask(t, mainTasks, "Validate selected Redfish boot action")
	systemIdx := findAnsibleTask(t, mainTasks, "Resolve Redfish system")
	prepareIdx := findAnsibleTask(t, mainTasks, "Prepare Redfish virtual media")
	validateMACsIdx := findAnsibleTask(t, mainTasks, "Validate declared MACs against Redfish inventory")
	bootSequenceIdx := findAnsibleTask(t, mainTasks, "Boot node from Redfish virtual media")
	bootSequenceTasks := nestedAnsibleTasks(t, mainTasks[bootSequenceIdx], "block")
	bootSequenceAlways := nestedAnsibleTasks(t, mainTasks[bootSequenceIdx], "always")
	powerIdx := findAnsibleTask(t, bootSequenceTasks, "Power node from virtual media")
	postIdx := findAnsibleTask(t, bootSequenceTasks, "Set post-boot Redfish boot device")
	restoreIdx := findAnsibleTask(t, bootSequenceAlways, "Restore Redfish certificate verification settings")
	if !(validateActionIdx < systemIdx && systemIdx < prepareIdx && prepareIdx < validateMACsIdx && validateMACsIdx < bootSequenceIdx) {
		t.Fatalf("boot_redfish imports must resolve the system, run media_prepare, and boot sequence in order")
	}
	if !(powerIdx < postIdx) {
		t.Fatalf("boot_redfish boot sequence must run power before post_boot")
	}
	validateAction, ok := mainTasks[validateActionIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an assert task", mainTasks[validateActionIdx]["name"])
	}
	for _, want := range []string{"boot", "cleanup_media"} {
		if !strings.Contains(fmt.Sprint(validateAction["that"]), want) {
			t.Fatalf("boot_redfish action validation missing %q: %v", want, validateAction["that"])
		}
	}
	for _, forbidden := range []string{"wait_power_off", "power_on_disk"} {
		if strings.Contains(fmt.Sprint(validateAction["that"]), forbidden) {
			t.Fatalf("boot_redfish action validation must not keep managed-only action %q: %v", forbidden, validateAction["that"])
		}
	}
	if got := mainTasks[prepareIdx]["when"]; got != "bootwright_redfish_action_effective in ['boot', 'cleanup_media']" {
		t.Fatalf("boot_redfish media preparation must only run for media actions, got when=%v", got)
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
	assertIncludeTasksFile(t, mainTasks[systemIdx], "stage/system.yml")
	if len(bootSequenceAlways) != 1 {
		t.Fatalf("boot_redfish boot sequence always block should only restore Redfish certificate settings, got %d tasks", len(bootSequenceAlways))
	}

	prepareTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml")
	systemTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/stage/system.yml")
	mediaTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_media_libvirt/tasks/main.yml")
	powerTasks := redfishPowerTasks(t)
	powerStateTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_state_probe.yml")
	insertAttemptTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/insert_attempt.yml")
	ejectTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/eject.yml")
	postTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/post_boot.yml")
	restoreTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/restore_certificate_verification.yml")
	discoverSystemIdx := findAnsibleTask(t, systemTasks, "Discover Redfish System ID")
	resolveSystemIdx := findAnsibleTask(t, systemTasks, "Resolve effective Redfish System ID")
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
	resolveDirectMediaIdx := findAnsibleTask(t, mediaTasks, "Resolve direct libvirt virtual media insert state")
	installDirectMediaIdx := findAnsibleTask(t, mediaTasks, "Install virtual media insert helper")
	insertDirectMediaIdx := findAnsibleTask(t, mediaTasks, "Insert staged virtual media directly into libvirt domain")
	recordDirectMediaIdx := findAnsibleTask(t, mediaTasks, "Record direct libvirt virtual media attachment")
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
	powerStateReportIdx := findAnsibleTask(t, powerStateTasks, "Report Redfish system power state wait status")
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

	if !(discoverSystemIdx < resolveSystemIdx) {
		t.Fatalf("boot_redfish must discover Redfish system ID before media and power actions")
	}
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
	if !(preConfigIdx < resolveDirectMediaIdx && resolveDirectMediaIdx < installDirectMediaIdx && installDirectMediaIdx < insertDirectMediaIdx && insertDirectMediaIdx < recordDirectMediaIdx) {
		t.Fatalf("libvirt media preparation must clean stale media before direct insert and backend attachment recording")
	}
	if !(protocolIdx < initInsertIdx && initInsertIdx < securityRefreshIdx && securityRefreshIdx < securityResolveIdx && securityResolveIdx < securityPatchIdx && securityPatchIdx < securityCaptureIdx && securityCaptureIdx < retryInsertIdx && retryInsertIdx < confirmMediaIdx && confirmMediaIdx < systemRefreshIdx && systemRefreshIdx < systemPreconditionIdx && systemPreconditionIdx < resetActionsIdx && resetActionsIdx < resetTargetIdx && resetTargetIdx < resetActionInfoIdx && resetActionInfoIdx < resolvePowerOnResetTypeIdx && resolvePowerOnResetTypeIdx < cdBootIdx && cdBootIdx < confirmCDBootIdx) {
		t.Fatalf("boot_redfish must retry virtual media insertion before setting CD boot")
	}
	if !(confirmCDBootIdx < forceOffIdx && forceOffIdx < initPowerOffIdx && initPowerOffIdx < waitPowerOffIdx && waitPowerOffIdx < confirmPowerOffIdx && confirmPowerOffIdx < powerOnIdx && powerOnIdx < confirmPowerOnRequestIdx && confirmPowerOnRequestIdx < initPowerOnIdx && initPowerOnIdx < waitPowerOnIdx && waitPowerOnIdx < confirmPowerOnIdx) {
		t.Fatalf("boot_redfish must wait for power-off before power-on and then confirm PowerState=On")
	}
	if !(powerStateProbeIdx < powerStateCaptureIdx && powerStateCaptureIdx < powerStateResolveIdx && powerStateResolveIdx < powerStateReportIdx && powerStateReportIdx < powerStateDelayIdx) {
		t.Fatalf("boot_redfish power state probe must capture and report sanitized status before sleeping")
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
	if got := mediaTasks[insertDirectMediaIdx]["when"]; got != "bootwright_libvirt_media_insert_requested | bool" {
		t.Fatalf("%s must only run for boot actions, got when=%v", mediaTasks[insertDirectMediaIdx]["name"], got)
	}
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
	initInsertFacts, ok := powerTasks[initInsertIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not a set_fact task", powerTasks[initInsertIdx]["name"])
	}
	for _, want := range []string{
		"bootwright_redfish_vmedia_backend_attached",
		"bootwright_redfish_vmedia_backend | default",
		"skipped-direct-backend",
		"pre-attached",
	} {
		if got := fmt.Sprint(initInsertFacts); !strings.Contains(got, want) {
			t.Fatalf("%s must honor direct backend attachment, missing %q in %v", powerTasks[initInsertIdx]["name"], want, initInsertFacts)
		}
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
	powerStateReport, ok := powerStateTasks[powerStateReportIdx]["ansible.builtin.debug"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no debug body", powerStateTasks[powerStateReportIdx]["name"])
	}
	powerStateReportMsg := fmt.Sprint(powerStateReport["msg"])
	for _, want := range []string{"bootwright_redfish_power_state_wait_label", "expected=", "observed=", "attempt="} {
		if !strings.Contains(powerStateReportMsg, want) {
			t.Fatalf("power state wait report missing %q: %s", want, powerStateReportMsg)
		}
	}
	if _, ok := powerStateTasks[powerStateReportIdx]["when"]; ok {
		t.Fatalf("power state wait report must print the reached attempt before later loop iterations skip the host")
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
	// The standard InsertMedia body must stay canonical (Image, Inserted,
	// TransferProtocolType). VerifyCertificate must never be injected as an action
	// parameter: xFusion/iBMC exposes the resource property but 501s a PATCH to it
	// and 400s an InsertMedia action carrying it. Verification is disabled via the
	// per-resource and SecurityService PATCHes instead.
	if _, ok := standardBody["VerifyCertificate"]; ok {
		t.Fatalf("standard InsertMedia body must not carry VerifyCertificate (iBMC 400s on it), got %v", standardBody)
	}
	for _, task := range insertAttemptTasks {
		if name, _ := task["name"].(string); name == "Skip BMC certificate verification on the InsertMedia fetch when opted out" {
			t.Fatalf("InsertMedia body must not inject VerifyCertificate as an action parameter; disable verification via the resource and SecurityService PATCHes")
		}
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
	assertRedactsByDefault(t, "virtual media eject redaction fact", ejectTasks[captureEjectIdx]["no_log"])
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
		// Credentials/TLS are supplied role-wide via ansible.builtin.uri module_defaults
		// (asserted in TestBootRedfishDispatchesMediaBackendBeforeInsert); each MAC-probe
		// uri must still reference the credential path so its output stays hidden.
		for _, want := range []string{
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

func TestBootRedfishHasNoMediaBackendSpecificReferences(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/defaults/main.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/main.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/media/prepare.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/validation/macs.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/media_insert.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_override.yml",
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

func TestRedfishVirtualMediaCertificateTrust(t *testing.T) {
	root := "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks"

	mediaInsert := readRepoFile(t, root+"/boot/media_insert.yml")
	for _, want := range []string{
		"../media/import_certificate.yml",
		"bootwright_component.boot.redfish.artifactCertificate.import | default(false) | bool",
		"bootwright_redfish_vmedia_transfer_protocol == 'HTTPS'",
	} {
		if !strings.Contains(mediaInsert, want) {
			t.Fatalf("media_insert.yml must gate certificate import on the operator flag; missing %q", want)
		}
	}

	// The shared import file owns discovery of both trust-store mechanisms and
	// dispatches to a per-method task file; it must not inline a vendor branch.
	imp := readRepoFile(t, root+"/media/import_certificate.yml")
	for _, want := range []string{
		// Retrieve the artifact cert with the openssl client (no cryptography
		// Python library on the executor) and extract the leaf PEM.
		"s_client",
		"-----BEGIN CERTIFICATE-----",
		// Discover both mechanisms: DMTF collection and the SecurityService OEM
		// import/delete actions.
		"#SecurityService.ImportRemoteHttpsServerRootCA",
		"#SecurityService.DeleteRemoteHttpsServerRootCA",
		// Dispatch to the discovered method's dedicated task file.
		`include_tasks: "import_certificate/{{ bootwright_redfish_artifact_cert_method }}.yml"`,
	} {
		if !strings.Contains(imp, want) {
			t.Fatalf("import_certificate.yml missing %q", want)
		}
	}

	// Standard DMTF method: POST the PEM to the VirtualMedia Certificates
	// collection and record the reference for cleanup.
	impStd := readRepoFile(t, root+"/media/import_certificate/standard.yml")
	for _, want := range []string{
		"CertificateString",
		"CertificateType: PEM",
		"bootwright_redfish_artifact_cert_ref",
	} {
		if !strings.Contains(impStd, want) {
			t.Fatalf("import_certificate/standard.yml missing %q", want)
		}
	}

	// SecurityService OEM method: import the PEM to a fixed RootCertId slot and
	// record it for cleanup.
	impOEM := readRepoFile(t, root+"/media/import_certificate/security_service.yml")
	for _, want := range []string{
		"'RootCertId': (bootwright_redfish_security_service_root_cert_id | int)",
		"bootwright_redfish_artifact_root_cert_imported_id",
	} {
		if !strings.Contains(impOEM, want) {
			t.Fatalf("import_certificate/security_service.yml missing %q", want)
		}
	}
	// The SecurityService import action takes RootCertId and Usage as mutually
	// exclusive forms; sending both is non-conformant and rejected with HTTP 403.
	// We pin a fixed RootCertId slot for deterministic cleanup, so Usage must not
	// appear.
	if strings.Contains(impOEM, "'Usage'") {
		t.Fatalf("import_certificate/security_service.yml must not send Usage alongside RootCertId; the iBMC rejects the conflicting pair (403)")
	}

	// The shared remove file dispatches to the same discovered method.
	rem := readRepoFile(t, root+"/media/remove_certificate.yml")
	if want := `include_tasks: "remove_certificate/{{ bootwright_redfish_artifact_cert_method }}.yml"`; !strings.Contains(rem, want) {
		t.Fatalf("remove_certificate.yml missing %q", want)
	}

	remStd := readRepoFile(t, root+"/media/remove_certificate/standard.yml")
	for _, want := range []string{
		"method: DELETE",
		"(bootwright_redfish_artifact_cert_ref | default('') | length) > 0",
	} {
		if !strings.Contains(remStd, want) {
			t.Fatalf("remove_certificate/standard.yml missing %q", want)
		}
	}

	remOEM := readRepoFile(t, root+"/media/remove_certificate/security_service.yml")
	for _, want := range []string{
		"bootwright_redfish_security_service_delete_target",
		"bootwright_redfish_artifact_root_cert_imported_id is defined",
	} {
		if !strings.Contains(remOEM, want) {
			t.Fatalf("remove_certificate/security_service.yml missing %q", want)
		}
	}

	restore := readRepoFile(t, root+"/media/restore_certificate_verification.yml")
	if strings.Count(restore, "not (bootwright_component.boot.redfish.artifactCertificate.ignoreVerification | default(false) | bool)") < 2 {
		t.Fatalf("both restore PATCHes must skip when ignoreVerification leaves BMC verification disabled")
	}
	for _, want := range []string{
		"remove_certificate.yml",
		"bootwright_component.boot.redfish.artifactCertificate.removeAfterBoot | default(false) | bool",
	} {
		if !strings.Contains(restore, want) {
			t.Fatalf("restore_certificate_verification.yml missing %q", want)
		}
	}
}

func TestEmulatedBMCVMediaUsesLibvirtStorageRoot(t *testing.T) {
	factsTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/facts.yml")
	packagesTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/packages.yml")
	sushyTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/apply/sushy.yml")
	destroyTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/tasks/destroy.yml")
	poolXML := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/templates/vmedia/pool.xml.j2")
	vmediaSystemd := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/templates/vmedia/systemd.service.j2")
	sushySystemd := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/provider_service_bmc_emulated/templates/sushy/systemd.service.j2")

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
	tempRoot := fmt.Sprint(facts["bootwright_bmc_temp_root"])
	if !strings.Contains(tempRoot, "bootwright_bmc_provider_name") || !strings.HasSuffix(tempRoot, "/tmp") {
		t.Fatalf("emulated BMC temp root must live under provider state tmp, got %v", tempRoot)
	}

	dirsIdx := findAnsibleTask(t, packagesTasks, "Create BMC state directories")
	if !stringListContains(packagesTasks[dirsIdx]["loop"], "{{ bootwright_bmc_vmedia_root }}") {
		t.Fatalf("%s must create the separate vmedia root, got %v", packagesTasks[dirsIdx]["name"], packagesTasks[dirsIdx]["loop"])
	}
	tempDirIdx := findAnsibleTask(t, packagesTasks, "Create BMC emulator temp directory")
	tempDir, ok := packagesTasks[tempDirIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", packagesTasks[tempDirIdx]["name"])
	}
	if tempDir["path"] != "{{ bootwright_bmc_temp_root }}" || tempDir["mode"] != "0700" {
		t.Fatalf("%s must create private BMC temp root, got %v", packagesTasks[tempDirIdx]["name"], tempDir)
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

	for _, body := range []string{poolXML, vmediaSystemd} {
		if !strings.Contains(body, "{{ bootwright_bmc_vmedia_root }}") {
			t.Fatalf("emulated BMC vmedia templates must use bootwright_bmc_vmedia_root")
		}
		if strings.Contains(body, "<path>{{ bootwright_bmc_root }}/vmedia</path>") || strings.Contains(body, "--directory {{ bootwright_bmc_root }}/vmedia") {
			t.Fatalf("emulated BMC vmedia templates must not use bootwright_bmc_root/vmedia")
		}
	}
	for _, want := range []string{
		"Environment=TMPDIR={{ bootwright_bmc_temp_root }}",
		"Environment=TMP={{ bootwright_bmc_temp_root }}",
		"Environment=TEMP={{ bootwright_bmc_temp_root }}",
	} {
		if !strings.Contains(sushySystemd, want) {
			t.Fatalf("sushy systemd unit must pin Python temp files outside /tmp, missing %q", want)
		}
	}

	destroyIdx := findAnsibleTask(t, destroyTasks, "Remove BMC state directory")
	if !stringListContains(destroyTasks[destroyIdx]["loop"], "{{ bootwright_bmc_vmedia_root }}") {
		t.Fatalf("%s must remove the separate vmedia root, got %v", destroyTasks[destroyIdx]["name"], destroyTasks[destroyIdx]["loop"])
	}
}

func redfishPowerTasks(t *testing.T) []map[string]any {
	t.Helper()
	base := "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/"
	return readAnsibleTasksFromFiles(t,
		base+"media_insert.yml",
		base+"power_override.yml",
	)
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
