package repocheck

import (
	"fmt"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

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

func TestInstallAgentRefusesProvidedOSNodeBeforeBootDriverDispatch(t *testing.T) {
	top := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/boot_machine.yml")
	tasks := nestedAnsibleTasks(t, top[findAnsibleTask(t, top, "Boot selected machine when install is not already complete")], "block")
	selected := findAnsibleTask(t, tasks, "Set selected machine component")
	refuse := findAnsibleTask(t, tasks, "Refuse installer boot for a provided-OS ContainerCluster node")
	dispatch := findAnsibleTask(t, tasks, "Boot selected machine via its substrate-specific driver")
	if !(selected < refuse && refuse < dispatch) {
		t.Fatalf("provided-OS refusal must run after component resolution and before every boot driver, got selected=%d refuse=%d dispatch=%d", selected, refuse, dispatch)
	}
	if _, ok := tasks[refuse]["when"]; ok {
		t.Fatalf("provided-OS refusal must have no provider or mode escape, got when=%v", tasks[refuse]["when"])
	}
	assertBody, ok := tasks[refuse]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("provided-OS refusal must be an ansible.builtin.assert, got %v", tasks[refuse])
	}
	for _, want := range []string{
		"bootwright_selected_component.osManaged | default(false) | bool",
		"not (bootwright_selected_component.osProvided | default(true) | bool)",
	} {
		if !stringListContains(assertBody["that"], want) {
			t.Fatalf("provided-OS refusal must fail closed on rendered OS mode %q, got %v", want, assertBody["that"])
		}
	}
	message := fmt.Sprint(assertBody["fail_msg"])
	for _, want := range []string{
		"ContainerCluster/",
		"Machine/",
		"spec.os.provided=true",
		"spec.os.provided=false",
		"overwrite",
		"before provider boot dispatch",
		"bootwright_mutating_invocation",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("provided-OS refusal message must contain %q, got %q", want, message)
		}
	}
	if strings.Contains(message, "bootwright apply") {
		t.Fatalf("provided-OS refusal must consume the resolved invocation instead of constructing one, got %q", message)
	}
}

func TestAgentISOPublishTokenizedValuesAreRedactedFromMessages(t *testing.T) {
	for _, rel := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/iso/cleanup_target.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/create_iso.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/iso/publish_target.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/media_insert.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_boot_redfish/tasks/boot/power_override.yml",
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

func TestContainerClusterDestroySkipUnreachable(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_container_cluster_agent_destroy.yml")
	if len(plays) != 1 {
		t.Fatalf("container destroy plays = %d, want 1", len(plays))
	}
	ignoreUnreachable, ok := plays[0]["ignore_unreachable"].(string)
	if !ok || !strings.Contains(ignoreUnreachable, "bootwright_destroy_skip_unreachable") {
		t.Fatalf("container destroy play must template ignore_unreachable from bootwright_destroy_skip_unreachable, got %v", plays[0]["ignore_unreachable"])
	}
}

func TestInstallAgentControllerDNSDoesNotMutateHostsFile(t *testing.T) {
	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/stage/controller_dns.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/destroy_records.yml",
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
		"(bootwright_apply_mode | default('reconcile')) != 'rebuild'",
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

func TestInstallAgentRequiresInstallStateForWaitAction(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/stage/validate.yml")
	stateStat := tasks[findAnsibleTask(t, tasks, "Verify agent install state exists for wait")]
	stateFail := tasks[findAnsibleTask(t, tasks, "Fail when agent install state is missing for wait")]

	statBody, ok := stateStat["ansible.builtin.stat"].(map[string]any)
	if !ok {
		t.Fatalf("wait state stat has no stat body: %v", stateStat)
	}
	if got := statBody["path"]; got != "{{ bootwright_install_work_dir }}/.openshift_install_state.json" {
		t.Fatalf("wait state stat path got %v", got)
	}
	for _, want := range []string{
		"bootwright_install_agent_action_effective == 'wait_install'",
		"not bootwright_install_already_complete",
	} {
		if !stringListContains(stateStat["when"], want) {
			t.Fatalf("wait state stat when missing %q: %v", want, stateStat["when"])
		}
	}
	for _, want := range []string{
		"bootwright_install_agent_action_effective == 'wait_install'",
		"not bootwright_install_already_complete",
		"not bootwright_install_state_stat.stat.exists",
	} {
		if !stringListContains(stateFail["when"], want) {
			t.Fatalf("wait state failure when missing %q: %v", want, stateFail["when"])
		}
	}
	if failBody := fmt.Sprint(stateFail["ansible.builtin.fail"]); !strings.Contains(failBody, "deps phase") {
		t.Fatalf("wait state failure message must point at the deps phase: %v", stateFail["ansible.builtin.fail"])
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
	assertRedactsByDefault(t, "publish token record", tasks[recordTokenIdx]["no_log"])
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

func TestInstallAgentCapturesCredentialsWhenAResumedWaitFindsTheClusterInstalled(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/wait_install.yml"
	tasks := readAnsibleTasks(t, path)
	waitBlock := tasks[findAnsibleTask(t, tasks, "Wait for agent install completion when install is not already complete")]
	nested := nestedAnsibleTasks(t, waitBlock, "block")
	for _, name := range []string{"Store kubeconfig in cluster secrets", "Store kubeadmin password in cluster secrets", "Clean virtual media after successful install"} {
		if findAnsibleTaskIndex(nested, name) >= 0 {
			t.Fatalf("%q sits inside the not-already-complete block, so a resumed wait against a cluster that finished installing on its own records the install as complete with no kubeconfig captured, and every add-on on that cluster then fails", name)
		}
		if findAnsibleTaskIndex(tasks, name) < 0 {
			t.Fatalf("missing top-level Ansible task %q", name)
		}
	}
	capture := tasks[findAnsibleTask(t, tasks, "Resolve whether the installer credentials must be captured")]
	facts, ok := capture["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("credential capture must resolve through a fact, got %v", capture)
	}
	condition := fmt.Sprint(facts["bootwright_install_credentials_capture"])
	for _, want := range []string{"bootwright_install_wait_succeeded", "bootwright_install_already_complete"} {
		if !strings.Contains(condition, want) {
			t.Fatalf("credential capture condition missing %q, got %v", want, condition)
		}
	}
	for _, name := range []string{"Remove staged agent ISO publish directories", "Remove local agent ISO publish token"} {
		idx := findAnsibleTask(t, tasks, name)
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "bootwright_install_credentials_capture") {
			t.Fatalf("%q must not run on a failed wait: purging the staged ISO while virtual media is still inserted kills an install that is still writing from it, and the timed-out hint tells the operator to resume; got when=%v", name, tasks[idx]["when"])
		}
	}
}

func TestInstallAgentCleansGeneratedISOArtifactsAfterSuccessfulWait(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/wait_install.yml")
	recordIdx := findAnsibleTask(t, tasks, "Record local kubeconfig path")
	cleanMediaIdx := findAnsibleTask(t, tasks, "Clean virtual media after successful install")
	findRemoteIdx := findAnsibleTask(t, tasks, "Find remote generated agent ISO files")
	removeRemoteIdx := findAnsibleTask(t, tasks, "Remove remote generated agent ISO files")
	removeBootArtifactsIdx := findAnsibleTask(t, tasks, "Remove remote generated boot artifacts")
	findLocalIdx := findAnsibleTask(t, tasks, "Find local generated agent ISO files")
	removeLocalIdx := findAnsibleTask(t, tasks, "Remove local generated agent ISO files")
	removeRemotePathIdx := findAnsibleTask(t, tasks, "Remove remote agent ISO path record")
	removeLocalPathIdx := findAnsibleTask(t, tasks, "Remove local agent ISO path record")
	if !(recordIdx < cleanMediaIdx && cleanMediaIdx < findRemoteIdx && findRemoteIdx < removeRemoteIdx && removeRemoteIdx < removeBootArtifactsIdx && removeBootArtifactsIdx < findLocalIdx && findLocalIdx < removeLocalIdx && removeLocalIdx < removeRemotePathIdx && removeRemotePathIdx < removeLocalPathIdx) {
		t.Fatalf("wait_install must fetch credentials before removing generated ISO artifacts")
	}
	assertIncludeRoleName(t, tasks[cleanMediaIdx], "{{ bootwright_cleanup_media_component.cleanupMediaRole }}")
	if got := tasks[cleanMediaIdx]["loop"]; !strings.Contains(fmt.Sprint(got), "selectattr('cleanupMediaRole', 'defined')") {
		t.Fatalf("media cleanup must loop over components with a cleanupMediaRole, got %v", got)
	}
	cleanVars, ok := tasks[cleanMediaIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no vars", tasks[cleanMediaIdx]["name"])
	}
	if got := cleanVars["bootwright_component"]; got != "{{ bootwright_cleanup_media_component }}" {
		t.Fatalf("media cleanup component var got %v", got)
	}
	if got := cleanVars["bootwright_vmedia_action"]; got != "cleanup_media" {
		t.Fatalf("media cleanup action got %v", got)
	}
	cleanLoopControl, ok := tasks[cleanMediaIdx]["loop_control"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no loop_control", tasks[cleanMediaIdx]["name"])
	}
	if got := cleanLoopControl["loop_var"]; got != "bootwright_cleanup_media_component" {
		t.Fatalf("media cleanup loop_var got %v", got)
	}
	if got := fmt.Sprint(cleanLoopControl["label"]); !strings.Contains(got, "bootwright_cleanup_media_component.name") {
		t.Fatalf("media cleanup label got %v", got)
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
	for _, idx := range []int{cleanMediaIdx, findRemoteIdx, removeRemoteIdx, removeBootArtifactsIdx, findLocalIdx, removeLocalIdx, removeRemotePathIdx, removeLocalPathIdx} {
		if !stringListContains(tasks[idx]["when"], "bootwright_install_credentials_capture | bool") {
			t.Fatalf("%s must only clean once the install is proven complete, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
		if !stringListContains(tasks[idx]["when"], "bootwright_install_wait_target == 'install'") {
			t.Fatalf("%s must not clean installer media at the bootstrap gate, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}
}

func TestInstallAgentWaitsForBootstrapBeforePublishingCredentials(t *testing.T) {
	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/wait_install.yml")
	guard := topTasks[findAnsibleTask(t, topTasks, "Validate selected agent wait target")]
	guardAssert, ok := guard["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert body", guard["name"])
	}
	if !stringListContains(guardAssert["that"], "bootwright_install_wait_target in ['bootstrap', 'install']") {
		t.Fatalf("wait target validation got %v", guardAssert["that"])
	}

	waitBlockIdx := findAnsibleTask(t, topTasks, "Wait for agent install completion when install is not already complete")
	tasks := nestedAnsibleTasks(t, topTasks[waitBlockIdx], "block")
	markerIdx := findAnsibleTask(t, tasks, "Check recorded agent bootstrap completion")
	bootstrapIdx := findAnsibleTask(t, tasks, "Wait for agent bootstrap completion")
	recordIdx := findAnsibleTask(t, tasks, "Record agent bootstrap completion")
	installIdx := findAnsibleTask(t, tasks, "Wait for agent install completion")
	outcomeIdx := findAnsibleTask(t, tasks, "Determine agent wait outcome")
	if !(markerIdx < bootstrapIdx && bootstrapIdx < recordIdx && recordIdx < installIdx && installIdx < outcomeIdx) {
		t.Fatalf("wait_install must probe the bootstrap marker, wait, record it, then resolve the outcome before publishing credentials")
	}
	kubeconfigIdx := findAnsibleTask(t, topTasks, "Store kubeconfig in cluster secrets")
	if kubeconfigIdx < waitBlockIdx {
		t.Fatalf("credentials must be published after the wait resolves, got kubeconfig=%d waitBlock=%d", kubeconfigIdx, waitBlockIdx)
	}

	bootstrap, ok := tasks[bootstrapIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", tasks[bootstrapIdx]["name"])
	}
	if !stringListContains(bootstrap["argv"], "bootstrap-complete") {
		t.Fatalf("bootstrap wait argv = %v", bootstrap["argv"])
	}
	if got := tasks[bootstrapIdx]["async"]; got != "{{ bootwright_install_bootstrap_timeout_seconds }}" {
		t.Fatalf("bootstrap wait async got %v", got)
	}
	for _, want := range []string{
		"bootwright_install_wait_target == 'bootstrap'",
		"not bootwright_install_bootstrap_marker_stat.stat.exists",
	} {
		if !stringListContains(tasks[bootstrapIdx]["when"], want) {
			t.Fatalf("bootstrap wait when missing %q: %v", want, tasks[bootstrapIdx]["when"])
		}
	}

	install, ok := tasks[installIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", tasks[installIdx]["name"])
	}
	if !stringListContains(install["argv"], "install-complete") {
		t.Fatalf("install wait argv = %v", install["argv"])
	}
	if got := tasks[installIdx]["when"]; got != "bootwright_install_wait_target == 'install'" {
		t.Fatalf("install wait when got %v", got)
	}
	if got := topTasks[kubeconfigIdx]["when"]; got != "bootwright_install_credentials_capture | bool" {
		t.Fatalf("credential publication when got %v", got)
	}

	for _, name := range []string{
		"Read agent ISO publish token for cleanup",
		"Set agent ISO publish cleanup token",
		"Remove staged agent ISO publish directories",
		"Remove local agent ISO publish token",
	} {
		idx := findAnsibleTask(t, topTasks, name)
		if !stringListContains(topTasks[idx]["when"], "bootwright_install_wait_target == 'install'") {
			t.Fatalf("%s must not unstage boot media at the bootstrap gate, got when=%v", name, topTasks[idx]["when"])
		}
	}
}

func TestInstallAgentRetriesTheStalledAgentWait(t *testing.T) {
	var defaults map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/defaults/main.yml")), &defaults); err != nil {
		t.Fatalf("decode install_agent defaults: %v", err)
	}
	pattern, _ := defaults["bootwright_install_stalled_wait_pattern"].(string)
	for _, want := range []string{"failed to progress after all hosts available", "failed to prepare cluster installation"} {
		if !strings.Contains(pattern, want) {
			t.Fatalf("stalled wait pattern must cover %q, got %v", want, defaults["bootwright_install_stalled_wait_pattern"])
		}
	}
	timedOut, _ := defaults["bootwright_install_timed_out_wait_pattern"].(string)
	if !strings.Contains(timedOut, "bootstrap process timed out") {
		t.Fatalf("timed-out wait pattern must cover the installer's own give-up, got %v", defaults["bootwright_install_timed_out_wait_pattern"])
	}
	clusterInit, _ := defaults["bootwright_install_cluster_init_wait_pattern"].(string)
	if !strings.Contains(clusterInit, "failed to initialize the cluster") {
		t.Fatalf("the cluster-initialization give-up is a give-up bootwright can resume and must be matched, got %v", defaults["bootwright_install_cluster_init_wait_pattern"])
	}
	resumable, _ := defaults["bootwright_install_resumable_wait_pattern"].(string)
	for _, want := range []string{"bootwright_install_stalled_wait_pattern", "bootwright_install_timed_out_wait_pattern", "bootwright_install_cluster_init_wait_pattern"} {
		if !strings.Contains(resumable, want) {
			t.Fatalf("resumable wait pattern must union %s, got %v", want, defaults["bootwright_install_resumable_wait_pattern"])
		}
	}
	hostError, _ := defaults["bootwright_install_host_error_pattern"].(string)
	for _, want := range []string{"has hosts in error", "to error"} {
		if !strings.Contains(hostError, want) {
			t.Fatalf("host-error pattern must cover %q, got %v", want, defaults["bootwright_install_host_error_pattern"])
		}
	}
	if strings.Contains(resumable, "bootwright_install_host_error_pattern") {
		t.Fatalf("a host assisted-service moved to status error has stopped the install and is not a give-up bootwright can resume, got %q", resumable)
	}
	if got, ok := defaults["bootwright_install_installer_wait_window_seconds"].(int); !ok || got != 3600 {
		t.Fatalf("installer wait window got %v, want 3600", defaults["bootwright_install_installer_wait_window_seconds"])
	}
	if got, ok := defaults["bootwright_install_cluster_init_wait_window_seconds"].(int); !ok || got != 2400 {
		t.Fatalf("the cluster-initialization wait is a separate installer cap and must not reuse the bootstrap window, got %v", defaults["bootwright_install_cluster_init_wait_window_seconds"])
	}
	budget, _ := defaults["bootwright_install_wait_budget_seconds"].(string)
	for _, want := range []string{"bootwright_install_bootstrap_timeout_seconds", "bootwright_install_timeout_seconds", "bootwright_install_wait_target"} {
		if !strings.Contains(budget, want) {
			t.Fatalf("wait budget must select per target using %s, got %q", want, budget)
		}
	}
	timedOutHint, _ := defaults["bootwright_install_timed_out_wait_hint"].(string)
	if !strings.Contains(timedOutHint, "bootwright_install_installer_wait_window_seconds") {
		t.Fatalf("timed-out wait hint must name the installer window, got %q", timedOutHint)
	}
	if !strings.Contains(timedOutHint, "bootwright_install_wait_budget_var") {
		t.Fatalf("the knob that buys another window differs per wait target, so the hint must name the selected one, got %q", timedOutHint)
	}
	budgetVar, _ := defaults["bootwright_install_wait_budget_var"].(string)
	for _, want := range []string{"bootwright_install_bootstrap_timeout_seconds", "bootwright_install_timeout_seconds", "bootwright_install_wait_target"} {
		if !strings.Contains(budgetVar, want) {
			t.Fatalf("the named budget knob must select per target using %s, got %q", want, budgetVar)
		}
	}
	hostErrorHint, _ := defaults["bootwright_install_host_error_hint"].(string)
	if !strings.Contains(hostErrorHint, "bootwright_apply_rebuild_invocation") {
		t.Fatalf("a cluster that diverted into recovery is not pivoted out of bootstrap, so its hint must name the rebuild, got %q", hostErrorHint)
	}
	if strings.Contains(hostErrorHint, "not proof of a failed install") {
		t.Fatalf("a declared host in status error is a real failure and its hint must not repeat the timed-out wording, got %q", hostErrorHint)
	}
	rendezvous, _ := defaults["bootwright_install_rendezvous_node"].(string)
	for _, want := range []string{"bootwright_current_cluster.nodes", "'master'", "sort", "first"} {
		if !strings.Contains(rendezvous, want) {
			t.Fatalf("the rendezvous node is the first master by sorted name, matching agentHosts, and must be derived with %s, got %q", want, rendezvous)
		}
	}
	if hint, _ := defaults["bootwright_install_rendezvous_missing_hint"].(string); !strings.Contains(hint, "bootwright_install_rendezvous_node") {
		t.Fatalf("the rendezvous hint must name the node it resolved, got %v", defaults["bootwright_install_rendezvous_missing_hint"])
	}
	if got, ok := defaults["bootwright_install_log_diagnostic_max_lines"].(int); !ok || got < 1 {
		t.Fatalf("the diagnostic excerpt must be bounded, got %v", defaults["bootwright_install_log_diagnostic_max_lines"])
	}
	clusterInitHint, _ := defaults["bootwright_install_cluster_init_wait_hint"].(string)
	if !strings.Contains(clusterInitHint, "bootwright_install_cluster_init_wait_window_seconds") {
		t.Fatalf("cluster-initialization hint must name its own window, not the bootstrap cap, got %q", clusterInitHint)
	}
	if strings.Contains(clusterInitHint, "not proof of a failed install") {
		t.Fatalf("the cluster-initialization give-up follows hosts that already hold their ignition, so a host in error is a real failure and the hint must not repeat the bootstrap wording, got %q", clusterInitHint)
	}
	if hint, _ := defaults["bootwright_install_unclassified_wait_hint"].(string); strings.TrimSpace(hint) == "" {
		t.Fatalf("an unclassified give-up must still carry a hint, got %v", defaults["bootwright_install_unclassified_wait_hint"])
	}
	if hint, _ := defaults["bootwright_install_missing_nodes_hint"].(string); !strings.Contains(hint, "bootwright_install_missing_declared_nodes") {
		t.Fatalf("missing-node hint must name the resolved absent nodes, got %v", defaults["bootwright_install_missing_nodes_hint"])
	}
	if hint, _ := defaults["bootwright_install_log_diagnostic_hint"].(string); !strings.Contains(hint, "bootwright_install_log_diagnostics") {
		t.Fatalf("installer-log hint must carry the selected log lines, got %v", defaults["bootwright_install_log_diagnostic_hint"])
	}
	if got, ok := defaults["bootwright_install_stalled_wait_retries"].(int); !ok || got < 2 {
		t.Fatalf("stalled wait retries got %v, want at least 2", defaults["bootwright_install_stalled_wait_retries"])
	}
	if _, ok := defaults["bootwright_install_stalled_wait_delay_seconds"].(int); !ok {
		t.Fatalf("stalled wait delay got %v, want an integer", defaults["bootwright_install_stalled_wait_delay_seconds"])
	}
	if hint, _ := defaults["bootwright_install_stalled_wait_hint"].(string); !strings.Contains(hint, "bootwright_current_cluster.nodes") {
		t.Fatalf("stalled wait hint must name the declared nodes, got %q", hint)
	}

	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/wait_install.yml")
	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Wait for agent install completion when install is not already complete")], "block")

	for _, tc := range []struct {
		wait     string
		report   string
		register string
	}{
		{"Wait for agent bootstrap completion", "Fail when the agent bootstrap wait did not complete", "bootwright_install_bootstrap_wait"},
		{"Wait for agent install completion", "Fail when the agent install wait did not complete", "bootwright_install_wait"},
	} {
		waitIdx := findAnsibleTask(t, tasks, tc.wait)
		reportIdx := findAnsibleTask(t, tasks, tc.report)
		if waitIdx > reportIdx {
			t.Fatalf("%s must be reported after it runs", tc.wait)
		}
		wait := tasks[waitIdx]
		until, _ := wait["until"].(string)
		if !strings.Contains(until, "bootwright_install_resumable_wait_pattern") || !strings.Contains(until, tc.register+".rc | default(1) | int) == 0") {
			t.Fatalf("%s must retry only a resumable give-up, got until=%v", tc.wait, wait["until"])
		}
		if !strings.Contains(until, "bootwright_install_wait_deadline") || !strings.Contains(until, "now(utc=true).timestamp()") {
			t.Fatalf("%s must stop retrying once the wait budget is spent, got until=%v", tc.wait, wait["until"])
		}
		if !strings.Contains(until, "bootwright_install_host_error_pattern") {
			t.Fatalf("%s must stop retrying once a declared host reached status error, since another window watches the same state, got until=%v", tc.wait, wait["until"])
		}
		if got := wait["retries"]; got != "{{ bootwright_install_stalled_wait_retries }}" {
			t.Fatalf("%s retries got %v", tc.wait, got)
		}
		if got := wait["delay"]; got != "{{ bootwright_install_stalled_wait_delay_seconds }}" {
			t.Fatalf("%s delay got %v", tc.wait, got)
		}
		if got := wait["failed_when"]; got != false {
			t.Fatalf("%s must defer failure to its report task, got failed_when=%v", tc.wait, got)
		}
		if !stringListContains(tasks[reportIdx]["when"], "("+tc.register+".rc | default(1) | int) != 0") {
			t.Fatalf("%s must fail on a non-zero wait, got when=%v", tc.report, tasks[reportIdx]["when"])
		}
		fail, ok := tasks[reportIdx]["ansible.builtin.fail"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no fail body", tc.report)
		}
		msg, _ := fail["msg"].(string)
		for _, want := range []string{
			"bootwright_install_stalled_wait_hint",
			"bootwright_install_stalled_wait_pattern",
			"bootwright_install_timed_out_wait_hint",
			"bootwright_install_timed_out_wait_pattern",
			"bootwright_install_cluster_init_wait_hint",
			"bootwright_install_cluster_init_wait_pattern",
			"bootwright_install_missing_nodes_hint",
			"bootwright_install_log_diagnostic_hint",
			"bootwright_install_host_error_hint",
			"bootwright_install_host_error_observed",
			"bootwright_install_rendezvous_missing_hint",
			"stderr_lines",
		} {
			if !strings.Contains(msg, want) {
				t.Fatalf("%s message must carry %q, got %q", tc.report, want, msg)
			}
		}
		if strings.Index(msg, "bootwright_install_host_error_hint") > strings.Index(msg, "bootwright_install_timed_out_wait_hint") {
			t.Fatalf("the same stderr carries both the host error and the installer's own timeout, so %s must classify the host error first, got %q", tc.report, msg)
		}
		if strings.Index(msg, "bootwright_install_host_error_hint") > strings.Index(msg, "did not complete for") {
			t.Fatalf("the run tree keeps only the head of this message, so %s must lead with the classification rather than the command line, got %q", tc.report, msg)
		}
		if other := otherWaitRegister(tc.register); strings.Contains(msg, other) {
			t.Fatalf("%s must classify from its own wait, got a reference to %s in %q", tc.report, other, msg)
		}
		if !strings.Contains(msg, "bootwright_install_log_diagnostic_max_lines") {
			t.Fatalf("%s must bound the installer stderr it quotes, got %q", tc.report, msg)
		}
		if !strings.Contains(msg, "else bootwright_install_unclassified_wait_hint") {
			t.Fatalf("%s must fall back to a real hint instead of an empty string when it does not recognise the give-up, got %q", tc.report, msg)
		}
	}

	deadlineIdx := findAnsibleTask(t, tasks, "Set the agent wait budget deadline")
	for _, name := range []string{"Wait for agent bootstrap completion", "Wait for agent install completion"} {
		if deadlineIdx > findAnsibleTask(t, tasks, name) {
			t.Fatalf("the wait budget deadline must be stamped before %s", name)
		}
	}
	deadline, ok := tasks[deadlineIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[deadlineIdx]["name"])
	}
	stamp := fmt.Sprint(deadline["bootwright_install_wait_deadline"])
	if !strings.Contains(stamp, "now(utc=true).timestamp()") || !strings.Contains(stamp, "bootwright_install_wait_budget_seconds") {
		t.Fatalf("wait budget deadline got %q", stamp)
	}

	observedIdx := findAnsibleTask(t, tasks, "Resolve whether a declared host reached assisted-service status error")
	for _, name := range []string{"Fail when the agent bootstrap wait did not complete", "Fail when the agent install wait did not complete"} {
		if observedIdx > findAnsibleTask(t, tasks, name) {
			t.Fatalf("the host-error classification must be resolved before %s reads it", name)
		}
	}
	observed, ok := tasks[observedIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[observedIdx]["name"])
	}
	resolved := fmt.Sprint(observed["bootwright_install_host_error_observed"])
	for _, want := range []string{"bootwright_install_bootstrap_wait.stderr", "bootwright_install_wait.stderr", "bootwright_install_log_tail.stdout"} {
		if !strings.Contains(resolved, want) {
			t.Fatalf("the installer writes the host transition to both streams and only the log keeps it at debug level, so the classification must read %s, got %q", want, resolved)
		}
	}

	diagnosticsIdx := findAnsibleTask(t, tasks, "Resolve agent install log diagnostics")
	diagnostics, ok := tasks[diagnosticsIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[diagnosticsIdx]["name"])
	}
	selected := fmt.Sprint(diagnostics["bootwright_install_log_diagnostics"])
	if !strings.Contains(selected, "bootwright_install_log_diagnostic_max_lines") {
		t.Fatalf("the log excerpt must be bounded or it buries every other hint, got %q", selected)
	}
	if strings.Index(selected, "bootwright_install_host_error_pattern") > strings.Index(selected, "bootwright_install_log_diagnostic_pattern") {
		t.Fatalf("the bound drops the tail of the excerpt, so the host-error lines must be selected ahead of the cluster-operator lines, got %q", selected)
	}
}

func otherWaitRegister(register string) string {
	if register == "bootwright_install_wait" {
		return "bootwright_install_bootstrap_wait."
	}
	return "bootwright_install_wait."
}

func TestInstallAgentClearsBootstrapMarkerBeforeISOGeneration(t *testing.T) {
	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/create_iso.yml")
	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Create cluster agent ISO when install is not already complete")], "block")
	stale := tasks[findAnsibleTask(t, tasks, "Remove stale installer state before ISO generation")]
	if !stringListContains(stale["loop"], "bootstrap-complete") {
		t.Fatalf("ISO generation must clear the recorded bootstrap completion, got loop=%v", stale["loop"])
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
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/wait_install.yml")

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
		if got := tasks[idx]["when"]; got != "bootwright_install_credentials_capture | bool" {
			t.Fatalf("%s must only run once the install is proven complete, got %v", tasks[idx]["name"], got)
		}
	}
}

func TestAgentInstallRefusesAnInstallerBinaryThatDoesNotMatchTheDeclaredRelease(t *testing.T) {
	const roleTasks = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/"
	tasks := readAnsibleTasks(t, roleTasks+"stage/validate.yml")

	pathIdx := findAnsibleTask(t, tasks, "Set openshift-install path")
	probeIdx := findAnsibleTask(t, tasks, "Read installed openshift-install version")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve installed openshift-install version")
	declaredIdx := findAnsibleTask(t, tasks, "Fail when the declared release version cannot be determined")
	installedIdx := findAnsibleTask(t, tasks, "Fail when the installed openshift-install version cannot be determined")
	assertIdx := findAnsibleTask(t, tasks, "Fail when the installer binary does not match the declared release")
	configIdx := findAnsibleTask(t, tasks, "Verify rendered install-config exists")
	if pathIdx > probeIdx || probeIdx > resolveIdx || resolveIdx > declaredIdx || declaredIdx > installedIdx || installedIdx > assertIdx || assertIdx > configIdx {
		t.Fatalf("the release-skew refusal must read the resolved binary and fail before the run reaches install-config (path=%d probe=%d resolve=%d declared=%d installed=%d assert=%d config=%d)", pathIdx, probeIdx, resolveIdx, declaredIdx, installedIdx, assertIdx, configIdx)
	}

	for _, idx := range []int{probeIdx, resolveIdx} {
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "wait_install") {
			t.Fatalf("%s must also read the binary on the wait path: every resumed apply skips the ISO task and would otherwise run a completely unchecked installer, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}

	const imagePinExemption = "(bootwright_declared_release_image | default('', true) | length) == 0 or (bootwright_declared_release_version | default('', true) | length) > 0"

	declaredFactIdx := findAnsibleTask(t, tasks, "Resolve declared release version")
	declaredFact, ok := tasks[declaredFactIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s must be a set_fact, got %v", tasks[declaredFactIdx]["name"], tasks[declaredFactIdx])
	}
	if got := fmt.Sprint(declaredFact["bootwright_declared_release_image"]); !strings.Contains(got, "'image'") {
		t.Fatalf("the release-skew guard must also resolve the pinned release image: a cluster may pin its release by image instead of version, and the version fact is empty in that case, got %v", declaredFact["bootwright_declared_release_image"])
	}

	for _, tc := range []struct {
		idx      int
		guard    string
		wants    []string
		unwanted []string
	}{
		{
			idx:      declaredIdx,
			guard:    "(bootwright_declared_release_version | default('', true) | length) == 0",
			wants:    []string{"distribution.release.version", "bootwright_current_cluster.name"},
			unwanted: []string{"release.image"},
		},
		{
			idx:   installedIdx,
			guard: "(bootwright_openshift_install_version | default('', true) | length) == 0",
			wants: []string{"bootwright bastion setup", "bootwright_install_version_probe.rc"},
		},
	} {
		if !stringListContains(tasks[tc.idx]["when"], "bootwright_install_agent_action_effective in ['run', 'create_iso']") {
			t.Fatalf("%s must run on the paths that build the ISO, got when=%v", tasks[tc.idx]["name"], tasks[tc.idx]["when"])
		}
		if !stringListContains(tasks[tc.idx]["when"], tc.guard) {
			t.Fatalf("an unreadable version must fail the run rather than silently skip the comparison (%s missing %q), got when=%v", tasks[tc.idx]["name"], tc.guard, tasks[tc.idx]["when"])
		}
		if !stringListContains(tasks[tc.idx]["when"], imagePinExemption) {
			t.Fatalf("a release pinned by image alone declares no version and the API accepts that, so %s must exempt it, but a cluster declaring both still pins a version and must still be checked: exempting on the image alone installs whatever the binary holds, got when=%v", tasks[tc.idx]["name"], tasks[tc.idx]["when"])
		}
		body, ok := tasks[tc.idx]["ansible.builtin.fail"].(map[string]any)
		if !ok {
			t.Fatalf("%s must be a fail, got %v", tasks[tc.idx]["name"], tasks[tc.idx])
		}
		msg := fmt.Sprint(body["msg"])
		for _, want := range tc.wants {
			if !strings.Contains(msg, want) {
				t.Fatalf("%s must name what it could not determine (missing %q), got %q", tasks[tc.idx]["name"], want, msg)
			}
		}
		for _, unwanted := range tc.unwanted {
			if strings.Contains(msg, unwanted) {
				t.Fatalf("no task reads the pinned release image on the install path, so %s must not offer %q as a remedy: an operator who takes it gets a green apply of the binary's own release with every skew guard exempted, got %q", tasks[tc.idx]["name"], unwanted, msg)
			}
		}
	}

	assertion, ok := tasks[assertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the release-skew refusal must be an assert, got %v", tasks[assertIdx])
	}
	if that := fmt.Sprint(assertion["that"]); !strings.Contains(that, "bootwright_openshift_install_version == bootwright_declared_release_version") {
		t.Fatalf("the release-skew refusal must compare the installed binary against the cluster's declared release: the agent ISO embeds the payload compiled into the binary, and /usr/local/bin holds one installer for every context that only `bootwright bastion setup` refreshes, so a release bump alone leaves the previous version building the ISO, got that=%v", assertion["that"])
	}
	failMsg := fmt.Sprint(assertion["fail_msg"])
	for _, want := range []string{"bootwright bastion setup", "bootwright_declared_release_version", "bootwright_openshift_install_path"} {
		if !strings.Contains(failMsg, want) {
			t.Fatalf("the release-skew refusal must name what was found and the exact command that repairs it (missing %q), got %q", want, failMsg)
		}
	}
	guard := fmt.Sprint(tasks[assertIdx]["when"])
	if strings.Contains(guard, "bootwright_declared_release_version | length > 0") {
		t.Fatalf("gating the refusal on a non-empty declared version makes an unreadable declaration a silent skip, which is exactly how an unchecked installer built prd, got when=%v", tasks[assertIdx]["when"])
	}
	if !strings.Contains(guard, "bootwright_install_agent_action_effective in ['run', 'create_iso']") {
		t.Fatalf("the refusal must cover every path that builds the ISO, got when=%v", tasks[assertIdx]["when"])
	}
	if !stringListContains(tasks[assertIdx]["when"], imagePinExemption) {
		t.Fatalf("spec.distribution.release.image alone pins a release without a version and is a supported, documented configuration, so the refusal must skip it instead of making it un-appliable, while a cluster declaring an image and a version must still be refused on skew, got when=%v", tasks[assertIdx]["when"])
	}

	warnIdx := findAnsibleTask(t, tasks, "Warn when the waited install was built by a mismatched installer binary")
	if warnIdx < assertIdx {
		t.Fatalf("the wait-path warning must follow the refusal it softens (warn=%d assert=%d)", warnIdx, assertIdx)
	}
	warn, ok := tasks[warnIdx]["ansible.builtin.debug"].(map[string]any)
	if !ok {
		t.Fatalf("the wait path must warn rather than fail: refusing there strands a cluster mid-install, got %v", tasks[warnIdx])
	}
	warnMsg := fmt.Sprint(warn["msg"])
	for _, want := range []string{
		"WARNING",
		"bootwright_agent_iso_installer_version",
		"bootwright_openshift_install_version",
		"bootwright_declared_release_version",
		"future rebuild",
	} {
		if !strings.Contains(warnMsg, want) {
			t.Fatalf("the wait-path warning must name both versions and the command that repairs them (missing %q), got %q", want, warnMsg)
		}
	}
	if !stringListContains(tasks[warnIdx]["when"], "bootwright_install_agent_action_effective == 'wait_install'") {
		t.Fatalf("the wait-path warning when got %v", tasks[warnIdx]["when"])
	}
	if !stringListContains(tasks[warnIdx]["when"], imagePinExemption) {
		t.Fatalf("the wait-path warning must carry the same exemption as the refusal, or a cluster declaring an image and a version loses the only report of the skew it is being built with, got when=%v", tasks[warnIdx]["when"])
	}

	pinIdx := findAnsibleTask(t, tasks, "Warn that a pinned release image is not honoured on the install path")
	if pinIdx < warnIdx || pinIdx > configIdx {
		t.Fatalf("the image-pin notice must follow the guards it explains and precede install-config (warn=%d pin=%d config=%d)", warnIdx, pinIdx, configIdx)
	}
	pin, ok := tasks[pinIdx]["ansible.builtin.debug"].(map[string]any)
	if !ok {
		t.Fatalf("an image pin is a supported shape, so the notice must be a debug rather than a refusal, got %v", tasks[pinIdx])
	}
	pinMsg := fmt.Sprint(pin["msg"])
	for _, want := range []string{
		"WARNING",
		"bootwright_declared_release_image",
		"bootwright_openshift_install_version",
		"spec.distribution.release.version",
	} {
		if !strings.Contains(pinMsg, want) {
			t.Fatalf("no install task reads the pinned release image, so the exempted path must say plainly that the installer binary's own release is what gets installed (missing %q), got %q", want, pinMsg)
		}
	}
	for _, want := range []string{
		"(bootwright_declared_release_image | default('', true) | length) > 0",
		"(bootwright_declared_release_version | default('', true) | length) == 0",
	} {
		if !stringListContains(tasks[pinIdx]["when"], want) {
			t.Fatalf("the image-pin notice must cover exactly the shape every release guard exempts (missing %q), got when=%v", want, tasks[pinIdx]["when"])
		}
	}
}

func TestInstallAgentReportsTheInstallerLogAndMissingDeclaredNodes(t *testing.T) {
	const roleTasks = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/"
	topTasks := readAnsibleTasks(t, roleTasks+"actions/wait_install.yml")
	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Wait for agent install completion when install is not already complete")], "block")

	outcomeIdx := findAnsibleTask(t, tasks, "Determine agent wait outcome")
	logIdx := findAnsibleTask(t, tasks, "Read the tail of the agent install log after a failed wait")
	logSelectIdx := findAnsibleTask(t, tasks, "Resolve agent install log diagnostics")
	kubeconfigIdx := findAnsibleTask(t, tasks, "Stat installer kubeconfig after a failed wait")
	probeIdx := findAnsibleTask(t, tasks, "Probe cluster nodes after a failed wait")
	missingIdx := findAnsibleTask(t, tasks, "Resolve declared nodes absent from the cluster")
	bootstrapReportIdx := findAnsibleTask(t, tasks, "Fail when the agent bootstrap wait did not complete")
	installReportIdx := findAnsibleTask(t, tasks, "Fail when the agent install wait did not complete")
	if !(outcomeIdx < logIdx && logIdx < logSelectIdx && logSelectIdx < kubeconfigIdx && kubeconfigIdx < probeIdx && probeIdx < missingIdx && missingIdx < bootstrapReportIdx && bootstrapReportIdx < installReportIdx) {
		t.Fatalf("wait_install must collect the installer log and the missing declared nodes before it reports either wait as failed")
	}

	logTail, ok := tasks[logIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", tasks[logIdx]["name"])
	}
	for _, want := range []string{"tail", "-n", "{{ bootwright_install_log_tail_lines }}", "{{ bootwright_install_work_dir }}/.openshift_install.log"} {
		if !stringListContains(logTail["argv"], want) {
			t.Fatalf("installer log tail missing %q: %v", want, logTail["argv"])
		}
	}

	probe, ok := tasks[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", tasks[probeIdx]["name"])
	}
	for _, want := range []string{"oc", "--kubeconfig", "{{ bootwright_install_work_dir }}/auth/kubeconfig", "get", "nodes"} {
		if !stringListContains(probe["argv"], want) {
			t.Fatalf("node probe missing %q: %v", want, probe["argv"])
		}
	}
	if !stringListContains(tasks[probeIdx]["when"], "bootwright_install_diagnostic_kubeconfig_stat.stat.exists | default(false)") {
		t.Fatalf("the node probe must only run once a kubeconfig exists, got when=%v", tasks[probeIdx]["when"])
	}

	for _, idx := range []int{logIdx, probeIdx} {
		if got := tasks[idx]["failed_when"]; got != false {
			t.Fatalf("%s must never turn a recoverable wait into a hard error, got failed_when=%v", tasks[idx]["name"], got)
		}
		if got := tasks[idx]["changed_when"]; got != false {
			t.Fatalf("%s is read-only, got changed_when=%v", tasks[idx]["name"], got)
		}
	}
	for _, idx := range []int{logIdx, logSelectIdx, kubeconfigIdx, probeIdx, missingIdx} {
		if !stringListContains(tasks[idx]["when"], "not (bootwright_install_wait_succeeded | bool)") {
			t.Fatalf("%s must only run after a failed wait, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}

	selection, ok := tasks[logSelectIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[logSelectIdx]["name"])
	}
	if got := fmt.Sprint(selection["bootwright_install_log_diagnostics"]); !strings.Contains(got, "bootwright_install_log_diagnostic_pattern") {
		t.Fatalf("installer log selection got %v", got)
	}

	missing, ok := tasks[missingIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no set_fact body", tasks[missingIdx]["name"])
	}
	got := fmt.Sprint(missing["bootwright_install_missing_declared_nodes"])
	for _, want := range []string{"bootwright_current_cluster.nodes", "bootwright_install_node_probe.stdout"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the absent-node report must subtract the observed nodes from the declared ones (missing %q), got %v", want, got)
		}
	}
	if !stringListContains(tasks[missingIdx]["when"], "(bootwright_install_node_probe.rc | default(1) | int) == 0") {
		t.Fatalf("an unreachable API must leave the absent-node report unset rather than name every declared node, got when=%v", tasks[missingIdx]["when"])
	}
}

func TestDestroyClusterRemovesClusterInstallerRuntimeDir(t *testing.T) {
	const roleTasks = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/"
	paths := readRepoFile(t, roleTasks+"destroy_paths.yml")
	for _, want := range []string{
		"bootwright_cluster_installer_runtime_dir: \"{{ bootwright_clusters_dir }}/{{ bootwright_current_cluster.name }}/runtime/installer\"",
		"bootwright_cluster_addon_runtime_dir: \"{{ bootwright_clusters_dir }}/{{ bootwright_current_cluster.name }}/runtime/addons\"",
		"bootwright_cluster_generated_addon_secrets_dir: \"{{ bootwright_clusters_dir }}/{{ bootwright_current_cluster.name }}/secrets/addons\"",
	} {
		if !strings.Contains(paths, want) {
			t.Fatalf("destroy_paths missing %q", want)
		}
	}
	runtime := readRepoFile(t, roleTasks+"destroy_runtime.yml")
	for _, want := range []string{
		"bootwright_process_cleanup_pattern: \"clusters/{{ bootwright_current_cluster.name }}/runtime/installer/\"",
		"- \"{{ bootwright_cluster_installer_runtime_dir }}\"",
		"- \"{{ bootwright_cluster_addon_runtime_dir }}\"",
		"- \"{{ bootwright_cluster_generated_addon_secrets_dir }}\"",
	} {
		if !strings.Contains(runtime, want) {
			t.Fatalf("destroy_runtime missing %q", want)
		}
	}
}

func TestContainerClusterCredentialRemovalStaysInTheRecordsHalf(t *testing.T) {
	const roleTasks = "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/"
	records := readRepoFile(t, roleTasks+"destroy_records.yml")
	runtime := readRepoFile(t, roleTasks+"destroy_runtime.yml")
	for _, want := range []string{
		"{{ bootwright_cluster_secrets_dir }}/kubeconfig",
		"{{ bootwright_cluster_secrets_dir }}/kubeadmin-password",
		"{{ bootwright_cluster_install_record_path }}",
		"{{ bootwright_cluster_connection_record_path }}",
	} {
		if !strings.Contains(records, want) {
			t.Fatalf("destroy_records missing %q", want)
		}
		if strings.Contains(runtime, want) {
			t.Fatalf("destroy_runtime must not carry %q: the runtime half is the graph root and runs before machine teardown, which still needs the host kubeconfig to reach KubeVirt and the controller resolver to reach every SSH target", want)
		}
	}
	for _, stale := range []string{"bootwright_controller_resolver_dropin_path", "controller-name-resolver", "systemd-resolved"} {
		if strings.Contains(records, stale) || strings.Contains(runtime, stale) {
			t.Fatalf("ContainerCluster teardown still owns %q; controller DNS must survive until the managed name-resolution owner is removed after machine teardown", stale)
		}
	}
}

func TestManagedControllerResolverRemovalStaysWithDNSOwner(t *testing.T) {
	const roleRoot = "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_name_resolution_dnsmasq/"
	controllerPlay := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_controller_name_resolution_apply.yml")
	for _, want := range []string{"bootwright_controller_name_resolution_work | length == 1", "`{{ bootwright_mutating_invocation }}`"} {
		if !strings.Contains(controllerPlay, want) {
			t.Fatalf("controller resolver play can silently omit or merge selected managed services: missing %q", want)
		}
	}
	if strings.Contains(controllerPlay, "end_host") {
		t.Fatal("controller resolver play silently skips an empty desired projection instead of failing closed before machine work")
	}
	destroy := readRepoFile(t, roleRoot+"tasks/destroy.yml")
	container := strings.Index(destroy, "support_component_teardown")
	evidence := strings.LastIndex(destroy, "support_component_teardown")
	if container < 0 || evidence < 0 || container >= evidence {
		t.Fatalf("DNS remote destroy must remove the service before its remote evidence: container=%d evidence=%d", container, evidence)
	}
	if strings.Contains(destroy, "controller_destroy") {
		t.Fatal("DNS remote destroy embeds controller cleanup instead of leaving the controller route inside the global preflight/cleanup bracket")
	}
	for _, port := range []string{"53/tcp", "53/udp"} {
		if !strings.Contains(destroy, port) {
			t.Fatalf("DNS remote destroy does not close %s", port)
		}
	}
	apply := readRepoFile(t, roleRoot+"tasks/main.yml")
	for _, port := range []string{"53/tcp", "53/udp"} {
		if !strings.Contains(apply, port) {
			t.Fatalf("DNS apply does not open and record %s", port)
		}
	}
	for _, attr := range []string{"port: 53/tcp", "udpPort: 53/udp"} {
		if !strings.Contains(apply, attr) {
			t.Fatalf("DNS ownership evidence does not retain scalar firewall endpoint %q for orphan cleanup", attr)
		}
	}
	if strings.Contains(destroy, "failed_when: false") {
		t.Fatal("DNS remote destroy suppresses firewall cleanup failure before service evidence removal")
	}
	controllerPreflight := readRepoFile(t, roleRoot+"tasks/controller_destroy_preflight.yml")
	if !strings.Contains(controllerPreflight, "controller_ownership_gate.yml") || !strings.Contains(controllerPreflight, "bootwright_mutating_invocation") {
		t.Fatal("controller destroy preflight does not prove ownership with the exact destroy retry")
	}
	for _, mutation := range []string{"ansible.builtin.file:", "ansible.builtin.systemd_service:", "tasks_from: remove_resource.yml"} {
		if strings.Contains(controllerPreflight, mutation) {
			t.Fatalf("controller destroy preflight contains mutation %q", mutation)
		}
	}
	controllerDestroy := readRepoFile(t, roleRoot+"tasks/controller_destroy.yml")
	for _, want := range []string{
		"bootwright_controller_resolver_dropin_path",
		"systemd-resolved",
		"controller-name-resolver",
		"bootwright_mutating_invocation",
		"throttle: 1",
		"bootwright_controller_resolver_destroy_resolved_active.rc == 0",
		"Any existing controller resolver ownership evidence was retained",
	} {
		if !strings.Contains(controllerDestroy, want) {
			t.Fatalf("controller_destroy missing %q", want)
		}
	}
	if strings.Contains(controllerDestroy, "failed_when: false") {
		t.Fatal("controller resolver teardown suppresses a remover failure and can discard retry evidence")
	}
	controllerGate := strings.Index(controllerDestroy, "controller_ownership_gate.yml")
	activeProbe := strings.Index(controllerDestroy, "Probe whether systemd-resolved is active before removal")
	dropinRemoval := strings.Index(controllerDestroy, "Remove controller resolver drop-in")
	restart := strings.Index(controllerDestroy, "Restart systemd-resolved after removing controller routing")
	recordRemoval := strings.Index(controllerDestroy, "Remove controller resolver ownership record")
	if controllerGate < 0 || activeProbe < 0 || dropinRemoval < 0 || restart < 0 || recordRemoval < 0 ||
		!(controllerGate < activeProbe && activeProbe < dropinRemoval && dropinRemoval < restart && restart < recordRemoval) {
		t.Fatalf("controller cleanup order must be gate, active probe, exact drop-in removal, conditional restart, evidence removal: gate=%d active=%d dropin=%d restart=%d record=%d", controllerGate, activeProbe, dropinRemoval, restart, recordRemoval)
	}
	for _, play := range []struct {
		path      string
		tasksFrom string
	}{
		{path: "ansible/collections/ansible_collections/bootwright/core/playbooks/task_controller_name_resolution_destroy_preflight.yml", tasksFrom: "controller_destroy_preflight.yml"},
		{path: "ansible/collections/ansible_collections/bootwright/core/playbooks/task_controller_name_resolution_destroy_cleanup.yml", tasksFrom: "controller_destroy.yml"},
	} {
		body := readRepoFile(t, play.path)
		for _, want := range []string{
			"bootwright_controller_name_resolution_destroy_targets",
			"bootwright_component.destroyRole",
			"tasks_from: " + play.tasksFrom,
			"item.kind | default('') == 'nameResolution'",
			"bootwright_mutating_invocation",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s does not fail closed and dispatch a registry-derived controller target: missing %q", play.path, want)
			}
		}
		for _, forbidden := range []string{"bootwright_ownership_record.role", "bootwright_ownership_record.paths"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s trusts ownership-record dispatch metadata %q", play.path, forbidden)
			}
		}
	}
	defaults := readRepoFile(t, roleRoot+"defaults/main.yml")
	for _, want := range []string{
		"bootwright_controller_resolver_context",
		"[bootwright_controller_resolver_context, bootwright_component.providerName, bootwright_component.name] | to_json",
		"hash('sha256')",
		"bootwright_controller_resolver_ownership_name }}.conf",
	} {
		if !strings.Contains(defaults, want) {
			t.Fatalf("controller resolver identity is not unique per context and managed service: missing %q", want)
		}
	}
	controllerApply := readRepoFile(t, roleRoot+"tasks/controller_apply.yml")
	gate := strings.Index(controllerApply, "controller_ownership_gate.yml")
	ownership := strings.Index(controllerApply, "Record controller resolver ownership before mutation")
	mutation := strings.Index(controllerApply, "Render controller split-DNS drop-in")
	if gate < 0 || ownership < 0 || mutation < 0 || !(gate < ownership && ownership < mutation) {
		t.Fatalf("controller resolver ownership gate and evidence must precede the first resolver mutation: gate=%d ownership=%d mutation=%d", gate, ownership, mutation)
	}
	for _, exact := range []string{"`{{ bootwright_apply_controller_dns_invocation }}`"} {
		if !strings.Contains(controllerApply, exact) {
			t.Fatalf("controller resolver refusal does not consume the exact controller-built invocation %q", exact)
		}
	}
	for _, closed := range []string{
		"Validate exact controller resolver probes",
		"item.addresses | default([]) | length > 0",
		"ahosts",
		"bootwright.core.bootwright_normalize_ip_set",
		"observed != expected",
		"bootwright_controller_resolver_initial_failures | length > 0",
		"bootwright_controller_resolver_managed_reconcile_required",
		"bootwright_controller_resolver_owned.stat.exists",
		"bootwright_controller_name_resolution_automatic_mutation | default(false) | bool",
		"Refuse ambiguous automatic controller split DNS",
		"systemd-resolved global DNS and routing-domain settings",
		"not bind one server set to one domain set",
	} {
		if !strings.Contains(controllerApply, closed) {
			t.Fatalf("controller resolver automatic mutation is not fail-closed: missing %q", closed)
		}
	}
	ownershipGate := readRepoFile(t, roleRoot+"tasks/controller_ownership_gate.yml")
	typeGate := strings.Index(ownershipGate, "Refuse unsafe controller resolver path types before reading")
	readRecord := strings.Index(ownershipGate, "Read controller resolver ownership record")
	if typeGate < 0 || readRecord < 0 || typeGate >= readRecord {
		t.Fatalf("controller resolver path-type gate must precede ownership-record slurp: type=%d read=%d", typeGate, readRecord)
	}
	for _, want := range []string{
		"bootwright-*.conf",
		"bootwright_controller_resolver_dropin_live.stat.exists",
		"bootwright_controller_resolver_ownership_files.files",
		"bootwright_controller_resolver_dropin_files.files",
		"bootwright_controller_resolver_ownership_files.skipped_paths",
		"bootwright_controller_resolver_dropin_files.skipped_paths",
		"file_type: any",
		"Refuse sibling Bootwright controller resolver state",
		"bootwright_controller_resolver_other_ownership_paths",
		"bootwright_controller_resolver_other_dropin_paths",
		"reject('equalto', bootwright_controller_resolver_ownership_path)",
		"reject('equalto', bootwright_controller_resolver_dropin_path)",
		"bootwright_controller_resolver_existing_ownership.owner",
		"bootwright_controller_resolver_existing_ownership.apiVersion",
		"bootwright.io/ownership/v1alpha1",
		"bootwright_controller_resolver_existing_ownership.role | default('owner', true)",
		"bootwright_controller_resolver_ownership_dir.stat.islnk",
		"bootwright_controller_resolver_dropin_dir.stat.islnk",
		"bootwright_controller_resolver_owned.stat.isreg",
		"bootwright_controller_resolver_owned.stat.islnk",
		"bootwright_controller_resolver_dropin_live.stat.isreg",
		"bootwright_controller_resolver_dropin_live.stat.islnk",
		"bootwright_controller_resolver_existing_ownership.context",
		"bootwright_controller_resolver_existing_ownership.provider",
		"bootwright_controller_resolver_existing_ownership.paths",
		"bootwright_controller_resolver_existing_ownership.attributes.resolver",
		"bootwright_controller_resolver_existing_ownership.attributes.component",
		"bootwright_controller_resolver_existing_ownership.attributes.machineRef",
		"bootwright_controller_resolver_existing_ownership.attributes.realisation",
		"bootwright_controller_resolver_existing_ownership.labels",
		"bootwright_apply_controller_dns_invocation",
		"bootwright_mutating_invocation",
		"Refusing to change resolver state or ownership evidence",
	} {
		if !strings.Contains(ownershipGate, want) {
			t.Fatalf("controller resolver ownership gate is not fail-closed: missing %q", want)
		}
	}
	for _, body := range []string{controllerApply, controllerPreflight, controllerDestroy, ownershipGate} {
		if strings.Contains(body, "bootwright_controller_resolver_retry_invocation") {
			t.Fatal("controller resolver role reintroduced an undocumented role-local retry invocation fact")
		}
	}
}
