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

func TestInstallAgentCleansGeneratedISOArtifactsAfterSuccessfulWait(t *testing.T) {
	topTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/container_cluster_agent_install/tasks/actions/wait_install.yml")
	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Wait for agent install completion when install is not already complete")], "block")
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
		if !stringListContains(tasks[idx]["when"], "bootwright_install_wait_succeeded | bool") {
			t.Fatalf("%s must only clean after a successful wait, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
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

	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Wait for agent install completion when install is not already complete")], "block")
	markerIdx := findAnsibleTask(t, tasks, "Check recorded agent bootstrap completion")
	bootstrapIdx := findAnsibleTask(t, tasks, "Wait for agent bootstrap completion")
	recordIdx := findAnsibleTask(t, tasks, "Record agent bootstrap completion")
	installIdx := findAnsibleTask(t, tasks, "Wait for agent install completion")
	outcomeIdx := findAnsibleTask(t, tasks, "Determine agent wait outcome")
	kubeconfigIdx := findAnsibleTask(t, tasks, "Store kubeconfig in cluster secrets")
	if !(markerIdx < bootstrapIdx && bootstrapIdx < recordIdx && recordIdx < installIdx && installIdx < outcomeIdx && outcomeIdx < kubeconfigIdx) {
		t.Fatalf("wait_install must probe the bootstrap marker, wait, record it, then resolve the outcome before publishing credentials")
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
	if got := tasks[kubeconfigIdx]["when"]; got != "bootwright_install_wait_succeeded | bool" {
		t.Fatalf("credential publication when got %v", got)
	}

	always := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Wait for agent install completion when install is not already complete")], "always")
	for _, name := range []string{
		"Read agent ISO publish token for cleanup",
		"Set agent ISO publish cleanup token",
		"Remove staged agent ISO publish directories",
		"Remove local agent ISO publish token",
	} {
		idx := findAnsibleTask(t, always, name)
		if !stringListContains(always[idx]["when"], "bootwright_install_wait_target == 'install'") {
			t.Fatalf("%s must not unstage boot media at the bootstrap gate, got when=%v", name, always[idx]["when"])
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
		if !strings.Contains(until, "bootwright_install_stalled_wait_pattern") || !strings.Contains(until, tc.register+".rc | default(1) | int) == 0") {
			t.Fatalf("%s must retry only the stalled give-up, got until=%v", tc.wait, wait["until"])
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
		for _, want := range []string{"bootwright_install_stalled_wait_hint", "bootwright_install_stalled_wait_pattern", "stderr_lines"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("%s message must carry %q, got %q", tc.report, want, msg)
			}
		}
	}
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
		if got := tasks[idx]["when"]; got != "bootwright_install_wait_succeeded | bool" {
			t.Fatalf("%s must only run after successful wait, got %v", tasks[idx]["name"], got)
		}
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
		"bootwright_controller_resolver_dropin_path",
		"systemd-resolved",
	} {
		if !strings.Contains(records, want) {
			t.Fatalf("destroy_records missing %q", want)
		}
		if strings.Contains(runtime, want) {
			t.Fatalf("destroy_runtime must not carry %q: the runtime half is the graph root and runs before machine teardown, which still needs the host kubeconfig to reach KubeVirt and the controller resolver to reach every SSH target", want)
		}
	}
}
