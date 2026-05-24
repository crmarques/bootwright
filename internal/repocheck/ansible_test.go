package repocheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/support"
	"go.yaml.in/yaml/v3"
)

func TestBootRedfishLibvirtVirtualMediaDetachFallback(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/roles/openshift/media_libvirt/tasks/eject.yml")
	task := tasks[findAnsibleTask(t, tasks, "Eject source-backed virtual media from {{ bootwright_libvirt_media_scope }} domain")]
	script := readRepoFile(t, "ansible/roles/openshift/media_libvirt/files/eject_libvirt_media.sh")

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

	bootRedfish := readRepoFile(t, "ansible/roles/openshift/boot_redfish/tasks/media_prepare.yml")
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

func TestClusterApplyRunsPreflightBeforeInstall(t *testing.T) {
	body := readRepoFile(t, "ansible/playbooks/targets/clusters/apply.yml")
	preflight := strings.Index(body, "../../checks/preflight.yml")
	install := strings.Index(body, "../../layers/openshift/install-agent.yml")
	if preflight < 0 || install < 0 || preflight > install {
		t.Fatalf("cluster apply must run preflight before install-agent")
	}
}

func TestProxyEnvironmentPlaybooksResolveProxyFacts(t *testing.T) {
	for _, path := range []string{
		"ansible/playbooks/checks/become.yml",
		"ansible/playbooks/checks/preflight.yml",
		"ansible/playbooks/layers/cluster_infra/apply.yml",
		"ansible/playbooks/layers/openshift/boot-agent-machine.yml",
		"ansible/playbooks/layers/openshift/create-agent-iso.yml",
		"ansible/playbooks/layers/openshift/destroy-agent.yml",
		"ansible/playbooks/layers/openshift/install-agent.yml",
		"ansible/playbooks/layers/openshift/wait-agent-install.yml",
		"ansible/playbooks/layers/providers/apply.yml",
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
				t.Fatalf("%s play %q must import host_proxy facts before proxied tasks", path, play["name"])
			}
		}
	}
}

func TestBootRedfishDispatchesMediaBackendBeforeInsert(t *testing.T) {
	mainTasks := readAnsibleTasks(t, "ansible/roles/openshift/boot_redfish/tasks/main.yml")
	prepareIdx := findAnsibleTask(t, mainTasks, "Prepare Redfish virtual media")
	powerIdx := findAnsibleTask(t, mainTasks, "Power node from virtual media")
	postIdx := findAnsibleTask(t, mainTasks, "Set post-boot Redfish boot device")
	if !(prepareIdx < powerIdx && powerIdx < postIdx) {
		t.Fatalf("boot_redfish imports must run media_prepare, power, then post_boot")
	}

	prepareTasks := readAnsibleTasks(t, "ansible/roles/openshift/boot_redfish/tasks/media_prepare.yml")
	mediaTasks := readAnsibleTasks(t, "ansible/roles/openshift/media_libvirt/tasks/main.yml")
	powerTasks := readAnsibleTasks(t, "ansible/roles/openshift/boot_redfish/tasks/power.yml")
	insertAttemptTasks := readAnsibleTasks(t, "ansible/roles/openshift/boot_redfish/tasks/insert_media_attempt.yml")
	postTasks := readAnsibleTasks(t, "ansible/roles/openshift/boot_redfish/tasks/post_boot.yml")
	managerListIdx := findAnsibleTask(t, prepareTasks, "List Redfish managers")
	managerMediaIdx := findAnsibleTask(t, prepareTasks, "List VirtualMedia members for Redfish managers")
	probeMediaIdx := findAnsibleTask(t, prepareTasks, "Probe Redfish VirtualMedia members")
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
	systemRefreshIdx := findAnsibleTask(t, powerTasks, "Refresh Redfish system metadata before CD boot override")
	systemPreconditionIdx := findAnsibleTask(t, powerTasks, "Resolve Redfish system PATCH precondition")
	cdBootIdx := findAnsibleTask(t, powerTasks, "Set one-time boot to CD")
	confirmCDBootIdx := findAnsibleTask(t, powerTasks, "Confirm one-time CD boot override was accepted")
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
	diskBootRefreshIdx := findAnsibleTask(t, postTasks, "Refresh Redfish system metadata before disk boot override")
	diskBootPreconditionIdx := findAnsibleTask(t, postTasks, "Resolve Redfish system PATCH precondition")
	diskBootIdx := findAnsibleTask(t, postTasks, "Set subsequent boots to disk after live ISO boot")
	diskBootConfirmIdx := findAnsibleTask(t, postTasks, "Confirm subsequent disk boot override was accepted")

	if !(managerListIdx < managerMediaIdx && managerMediaIdx < probeMediaIdx && probeMediaIdx < resolveMediaIdx && resolveMediaIdx < resolveManagerIdx && resolveManagerIdx < resolveSecurityServiceIdx && resolveSecurityServiceIdx < resolveActionIdx && resolveActionIdx < resolveActionCandidatesIdx && resolveActionCandidatesIdx < actionInfoIdx && actionInfoIdx < supportedVMMIdx && supportedVMMIdx < effectiveActionIdx && effectiveActionIdx < redfishEjectIdx && redfishEjectIdx < mediaPrepareIdx) {
		t.Fatalf("boot_redfish must discover manager-scoped virtual media and action targets before eject/prep")
	}
	if !(redfishEjectIdx < mediaPrepareIdx) {
		t.Fatalf("media backend preparation must run after Redfish eject")
	}
	if !(preLiveIdx < preConfigIdx) {
		t.Fatalf("media cleanup must process running state before persistent state")
	}
	if !(protocolIdx < initInsertIdx && initInsertIdx < securityRefreshIdx && securityRefreshIdx < securityResolveIdx && securityResolveIdx < securityPatchIdx && securityPatchIdx < securityCaptureIdx && securityCaptureIdx < retryInsertIdx && retryInsertIdx < confirmMediaIdx && confirmMediaIdx < systemRefreshIdx && systemRefreshIdx < systemPreconditionIdx && systemPreconditionIdx < cdBootIdx && cdBootIdx < confirmCDBootIdx) {
		t.Fatalf("boot_redfish must retry virtual media insertion before setting CD boot")
	}
	if !(retryDelayIdx < retryEjectIdx && retryEjectIdx < refreshMediaIdx && refreshMediaIdx < mediaPreconditionIdx && mediaPreconditionIdx < verifyCertIdx && verifyCertIdx < standardBodyIdx && standardBodyIdx < vmmBodyIdx && vmmBodyIdx < insertIdx && insertIdx < requestStatusIdx && requestStatusIdx < taskRefIdx && taskRefIdx < taskURLIdx && taskURLIdx < waitTaskIdx && waitTaskIdx < captureTaskIdx && captureTaskIdx < taskResultIdx && taskResultIdx < failedTaskProbeIdx && failedTaskProbeIdx < mountedTaskProbeIdx && mountedTaskProbeIdx < waitMediaIdx && waitMediaIdx < resolveProbeAfterInsertIdx && resolveProbeAfterInsertIdx < patchPreconditionIdx && patchPreconditionIdx < patchAttemptIdx && patchAttemptIdx < patchMediaIdx && patchMediaIdx < waitPatchMediaIdx && waitPatchMediaIdx < resolveProbeAfterPatchIdx && resolveProbeAfterPatchIdx < captureMediaIdx && captureMediaIdx < resolveAttachmentSourcesIdx && resolveAttachmentSourcesIdx < resolveAttachedIdx) {
		t.Fatalf("boot_redfish insert attempt must verify async task and virtual media insertion before reporting success")
	}
	if !(waitSSHIdx < diskBootRefreshIdx && diskBootRefreshIdx < diskBootPreconditionIdx && diskBootPreconditionIdx < diskBootIdx && diskBootIdx < diskBootConfirmIdx) {
		t.Fatalf("boot_redfish must wait for SSH before switching subsequent boots back to disk")
	}

	assertIncludeRoleName(t, prepareTasks[mediaPrepareIdx], "{{ bootwright_component.mediaPrepareRole }}")
	assertIncludeTasksFile(t, mediaTasks[preLiveIdx], "eject.yml")
	assertIncludeTasksFile(t, mediaTasks[preConfigIdx], "eject.yml")
	assertIncludeTasksFile(t, powerTasks[retryInsertIdx], "insert_media_attempt.yml")
	assertIncludeTasksApplyWhen(t, powerTasks[retryInsertIdx], "not (bootwright_redfish_vmedia_attached | bool)")
	if got := powerTasks[retryInsertIdx]["when"]; got != "not (bootwright_redfish_vmedia_attached | bool)" {
		t.Fatalf("virtual media retry loop must stop once attached, got when=%v", got)
	}
	if got, ok := powerTasks[retryInsertIdx]["loop"].(string); !ok || !strings.Contains(got, "bootwright_redfish_insert_media_retries") {
		t.Fatalf("virtual media retry loop must use configured retry count, got loop=%v", powerTasks[retryInsertIdx]["loop"])
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
	if got := securityRefresh["url"]; got != "{{ bootwright_component.boot.redfish.baseUrl }}{{ bootwright_redfish_security_service_member }}" {
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
	domainXML := readRepoFile(t, "ansible/roles/cluster_infra/substrate_libvirt/templates/domain.xml.j2")
	if hdIdx, cdIdx := strings.Index(domainXML, "<boot dev='hd'/>"), strings.Index(domainXML, "<boot dev='cdrom'/>"); hdIdx < 0 || cdIdx < 0 || hdIdx > cdIdx {
		t.Fatalf("libvirt domain must keep disk-first, CD-fallback boot order")
	}
	for _, forbidden := range []string{
		"Detach virtual media after live ISO boot",
		"Wait for virtual media to report detached",
		"Eject running virtual media after live ISO boot",
		"Eject persistent virtual media after live ISO boot",
		"Verify running virtual media source is absent",
		"Verify persistent virtual media source is absent",
	} {
		if findAnsibleTaskIndex(prepareTasks, forbidden) >= 0 || findAnsibleTaskIndex(mediaTasks, forbidden) >= 0 || findAnsibleTaskIndex(powerTasks, forbidden) >= 0 || findAnsibleTaskIndex(insertAttemptTasks, forbidden) >= 0 || findAnsibleTaskIndex(postTasks, forbidden) >= 0 {
			t.Fatalf("boot_redfish must not remove live ISO media before install completion: found %q", forbidden)
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
}

func TestBootRedfishStagesArtifactThroughDeclaredStageHost(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/roles/openshift/boot_redfish/tasks/validate_stage.yml")
	validateIdx := findAnsibleTask(t, tasks, "Validate boot_redfish inputs")
	schemeIdx := findAnsibleTask(t, tasks, "Validate Redfish virtual-media ISO URL scheme")
	dirIdx := findAnsibleTask(t, tasks, "Resolve agent ISO staging directory")
	createDirIdx := findAnsibleTask(t, tasks, "Create agent ISO staging directory")
	stageIdx := findAnsibleTask(t, tasks, "Stage agent ISO at the BMC fetch location")
	fetchProbeIdx := findAnsibleTask(t, tasks, "Probe staged agent ISO fetch URL")
	rangeProbeIdx := findAnsibleTask(t, tasks, "Probe staged agent ISO byte-range fetch")
	fetchConfirmIdx := findAnsibleTask(t, tasks, "Confirm staged agent ISO fetch URL is reachable")
	if !(validateIdx < schemeIdx && schemeIdx < dirIdx && dirIdx < createDirIdx && createDirIdx < stageIdx && stageIdx < fetchProbeIdx && fetchProbeIdx < rangeProbeIdx && rangeProbeIdx < fetchConfirmIdx) {
		t.Fatalf("boot_redfish must validate URL scheme, stage the ISO, and probe fetch reachability before Redfish insertion")
	}

	schemeAssert, ok := tasks[schemeIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert body", tasks[schemeIdx]["name"])
	}
	if !stringListContains(schemeAssert["that"], "(not (bootwright_component.boot.redfish.setBootSource | default(true) | bool)) or ((bootwright_component.boot.agentIso.fetchUrl | ansible.builtin.urlsplit('scheme') | lower) == 'https')") {
		t.Fatalf("real Redfish ISO URL scheme guard must require HTTPS while allowing emulated Redfish, got %v", schemeAssert["that"])
	}

	stageDir, ok := tasks[createDirIdx]["ansible.builtin.file"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no file body", tasks[createDirIdx]["name"])
	}
	if got := stageDir["path"]; got != "{{ bootwright_agent_iso_stage_dir }}" {
		t.Fatalf("stage directory path got %v", got)
	}
	if got := tasks[createDirIdx]["delegate_to"]; got != "{{ bootwright_component.boot.agentIso.stageHost }}" {
		t.Fatalf("stage directory delegate got %v", got)
	}
	if got := tasks[createDirIdx]["become"]; got != true {
		t.Fatalf("stage directory must use remote become, got %v", got)
	}

	stageCopy, ok := tasks[stageIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no copy body", tasks[stageIdx]["name"])
	}
	if got := stageCopy["src"]; got != "{{ bootwright_agent_iso_path }}" {
		t.Fatalf("stage copy source got %v", got)
	}
	if got := stageCopy["dest"]; got != "{{ bootwright_component.boot.agentIso.stagePath }}" {
		t.Fatalf("stage destination got %v", got)
	}
	if got := tasks[stageIdx]["delegate_to"]; got != "{{ bootwright_component.boot.agentIso.stageHost }}" {
		t.Fatalf("stage copy delegate got %v", got)
	}
	if got := tasks[stageIdx]["become"]; got != true {
		t.Fatalf("stage copy must use remote become, got %v", got)
	}

	fetchProbe, ok := tasks[fetchProbeIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", tasks[fetchProbeIdx]["name"])
	}
	if got := fetchProbe["url"]; got != "{{ bootwright_component.boot.agentIso.fetchUrl }}" {
		t.Fatalf("fetch URL probe target got %v", got)
	}
	if got := fetchProbe["method"]; got != "HEAD" {
		t.Fatalf("fetch URL probe must avoid downloading the ISO, got method=%v", got)
	}
	if got := tasks[fetchProbeIdx]["delegate_to"]; got != "{{ bootwright_component.boot.agentIso.stageHost }}" {
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
	headers, ok := rangeProbe["headers"].(map[string]any)
	if !ok || headers["Range"] != "bytes=0-0" {
		t.Fatalf("range probe must request one byte, got headers=%v", rangeProbe["headers"])
	}
	if got := rangeProbe["status_code"]; !intListEqual(got, []int{206}) {
		t.Fatalf("range probe must require HTTP 206, got %v", got)
	}
	if got := tasks[rangeProbeIdx]["when"]; got != "bootwright_component.boot.redfish.setBootSource | default(true) | bool" {
		t.Fatalf("range probe must only be required for real BMCs, got when=%v", got)
	}
	fetchConfirm, ok := tasks[fetchConfirmIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no assert body", tasks[fetchConfirmIdx]["name"])
	}
	if !stringListContains(fetchConfirm["that"], "(bootwright_agent_iso_fetch_probe.status | default(0) | int) in [200, 206]") {
		t.Fatalf("fetch URL confirmation must reject unreachable staged ISOs, got %v", fetchConfirm["that"])
	}
	if !stringListContains(fetchConfirm["that"], "(not (bootwright_component.boot.redfish.setBootSource | default(true) | bool)) or ((bootwright_agent_iso_range_probe.status | default(0) | int) == 206)") {
		t.Fatalf("fetch URL confirmation must require byte ranges for real BMCs, got %v", fetchConfirm["that"])
	}
}

func TestBootRedfishHasNoMediaBackendSpecificReferences(t *testing.T) {
	for _, path := range []string{
		"ansible/roles/openshift/boot_redfish/defaults/main.yml",
		"ansible/roles/openshift/boot_redfish/tasks/main.yml",
		"ansible/roles/openshift/boot_redfish/tasks/media_prepare.yml",
		"ansible/roles/openshift/boot_redfish/tasks/power.yml",
		"ansible/roles/openshift/boot_redfish/tasks/post_boot.yml",
		"ansible/roles/openshift/boot_redfish/tasks/validate_stage.yml",
	} {
		body := readRepoFile(t, path)
		for _, forbidden := range []string{"libvirt", "eject_libvirt_media"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s must not contain media-backend-specific text %q", path, forbidden)
			}
		}
	}
}

func TestArtifactsHTTPServiceSupportsRangeRequests(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/roles/providers/artifacts_http/tasks/main.yml")
	pathsIdx := findAnsibleTask(t, tasks, "Resolve boot artifact paths")
	helperDirIdx := findAnsibleTask(t, tasks, "Create boot artifact helper directory")
	helperIdx := findAnsibleTask(t, tasks, "Install boot artifact HTTP server")
	stateDirIdx := findAnsibleTask(t, tasks, "Create boot artifact state directory")
	tlsDirIdx := findAnsibleTask(t, tasks, "Create boot artifact TLS directory")
	tlsCertIdx := findAnsibleTask(t, tasks, "Stat boot artifact TLS certificate")
	tlsKeyIdx := findAnsibleTask(t, tasks, "Stat boot artifact TLS key")
	tlsPresentIdx := findAnsibleTask(t, tasks, "Detect existing boot artifact TLS material")
	tlsConfigIdx := findAnsibleTask(t, tasks, "Render boot artifact TLS OpenSSL config")
	tlsGenerateIdx := findAnsibleTask(t, tasks, "Generate boot artifact TLS certificate")
	unitIdx := findAnsibleTask(t, tasks, "Install boot artifact HTTP unit")
	serviceIdx := findAnsibleTask(t, tasks, "Ensure boot artifact HTTP service is running")
	waitIdx := findAnsibleTask(t, tasks, "Wait for boot artifact HTTPS endpoint")
	if !(pathsIdx < helperDirIdx && helperDirIdx < helperIdx && helperIdx < stateDirIdx && stateDirIdx < tlsDirIdx && tlsDirIdx < tlsCertIdx && tlsCertIdx < tlsKeyIdx && tlsKeyIdx < tlsPresentIdx && tlsPresentIdx < tlsConfigIdx && tlsConfigIdx < tlsGenerateIdx && tlsGenerateIdx < unitIdx && unitIdx < serviceIdx && serviceIdx < waitIdx) {
		t.Fatalf("artifacts_http must prepare TLS and the HTTP helper before managing the systemd unit")
	}

	copyTask, ok := tasks[helperIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no copy body", tasks[helperIdx]["name"])
	}
	if got := copyTask["src"]; got != "artifacts_http_server.py" {
		t.Fatalf("artifact HTTP helper source got %v, want artifacts_http_server.py", got)
	}

	unit := readRepoFile(t, "ansible/roles/providers/artifacts_http/templates/artifacts-http.service.j2")
	for _, want := range []string{"artifacts-http-server.py", "--directory", "--certfile", "--keyfile"} {
		if !strings.Contains(unit, want) {
			t.Fatalf("artifact HTTP unit must use TLS range-capable helper; missing %q", want)
		}
	}

	script := readRepoFile(t, "ansible/roles/providers/artifacts_http/files/artifacts_http_server.py")
	for _, want := range []string{
		"Accept-Ranges",
		"Content-Range",
		"send_response(206)",
		"Range",
		"ssl.SSLContext",
		"ConnectionError",
		"ssl.SSLError",
		"Connection\", \"close",
		"close_connection",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("artifact HTTP helper must support TLS byte ranges; missing %q", want)
		}
	}
	if strings.Contains(script, "super().copyfile") {
		t.Fatalf("artifact HTTP helper must guard full-file streaming instead of delegating to SimpleHTTPRequestHandler.copyfile")
	}
	tlsTemplate := readRepoFile(t, "ansible/roles/providers/artifacts_http/templates/artifacts-openssl.cnf.j2")
	for _, want := range []string{"subjectAltName", "bootwright_component.tls.dnsNames", "bootwright_component.tls.ipAddresses"} {
		if !strings.Contains(tlsTemplate, want) {
			t.Fatalf("artifact TLS template must render SANs; missing %q", want)
		}
	}
	for _, idx := range []int{tlsConfigIdx, tlsGenerateIdx} {
		when, ok := tasks[idx]["when"].(string)
		if !ok || !strings.Contains(when, "not (bootwright_artifacts_tls_material_present | bool)") {
			t.Fatalf("%s must preserve existing TLS material, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}

	service, ok := tasks[serviceIdx]["ansible.builtin.systemd_service"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no systemd_service body", tasks[serviceIdx]["name"])
	}
	state, ok := service["state"].(string)
	if !ok || !strings.Contains(state, "bootwright_artifacts_helper.changed") || !strings.Contains(state, "bootwright_artifacts_tls_generated.changed") {
		t.Fatalf("artifact HTTP service must restart when helper changes, got %v", service["state"])
	}
	waitURI, ok := tasks[waitIdx]["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no uri body", tasks[waitIdx]["name"])
	}
	if got := waitURI["url"]; !strings.HasPrefix(got.(string), "https://") {
		t.Fatalf("artifact readiness probe must use HTTPS, got %v", got)
	}
	if got := waitURI["validate_certs"]; got != false {
		t.Fatalf("artifact readiness probe must allow self-signed certs, got %v", got)
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

func TestProviderPlaybooksDispatchRenderedRoles(t *testing.T) {
	for _, path := range []string{
		"ansible/playbooks/layers/providers/apply.yml",
		"ansible/playbooks/layers/providers/destroy.yml",
	} {
		body := readRepoFile(t, path)
		for _, forbidden := range []string{
			"load_balancer_haproxy",
			"artifacts_http",
			"proxy_squid",
			"dns_dnsmasq",
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

func TestAnsibleRemoteBecomeTempConfig(t *testing.T) {
	for _, path := range []string{
		"ansible/ansible.cfg",
		"internal/embedded/bundle/ansible.cfg",
	} {
		if !repoFileExists(t, path) {
			continue
		}
		cfg := readRepoFile(t, path)
		localTmp, ok := ansibleCfgValue(cfg, "defaults", "local_tmp")
		if !ok {
			t.Fatalf("%s must explicitly configure local_tmp for controller temp files", path)
		}
		if localTmp != "/tmp" {
			t.Fatalf("%s local_tmp must use /tmp so controller Ansible does not depend on writable home dirs; got %q", path, localTmp)
		}
		tmp, ok := ansibleCfgValue(cfg, "defaults", "remote_tmp")
		if !ok {
			t.Fatalf("%s must explicitly configure remote_tmp for remote become tasks", path)
		}
		if tmp != "/tmp" {
			t.Fatalf("%s remote_tmp must use /tmp so sudo-root modules are readable outside restricted home dirs; got %q", path, tmp)
		}
		value, ok := ansibleCfgValue(cfg, "ssh_connection", "pipelining")
		if !ok {
			t.Fatalf("%s must explicitly configure SSH pipelining for remote become tasks", path)
		}
		if strings.ToLower(value) != "false" {
			t.Fatalf("%s must disable SSH pipelining; remote sudo requiretty hosts fail before fact gathering", path)
		}
	}
}

func TestInstallAgentControllerDNSDoesNotMutateHostsFile(t *testing.T) {
	for _, path := range []string{
		"ansible/roles/openshift/install_agent/tasks/controller_dns.yml",
		"ansible/roles/openshift/destroy_agent/tasks/main.yml",
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
	body := readRepoFile(t, "ansible/roles/openshift/install_agent/tasks/skip_guard.yml")
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
	tasks := readAnsibleTasks(t, "ansible/roles/openshift/install_agent/tasks/main.yml")
	selectIdx := findAnsibleTask(t, tasks, "Select agent install action")
	validateActionIdx := findAnsibleTask(t, tasks, "Validate selected agent install action")
	validateInputsIdx := findAnsibleTask(t, tasks, "Validate installer inputs")
	runIdx := findAnsibleTask(t, tasks, "Run selected agent install action")
	if !(selectIdx < validateActionIdx && validateActionIdx < validateInputsIdx && validateInputsIdx < runIdx) {
		t.Fatalf("install_agent must select and validate action before validating action-specific inputs")
	}
}

func TestInstallAgentSkipsConsumedInstallConfigForBootAction(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/roles/openshift/install_agent/tasks/validate.yml")
	installStat := tasks[findAnsibleTask(t, tasks, "Verify rendered install-config exists")]
	installFail := tasks[findAnsibleTask(t, tasks, "Fail when install-config is missing")]
	isoPathStat := tasks[findAnsibleTask(t, tasks, "Verify generated agent ISO path exists")]
	isoPathFail := tasks[findAnsibleTask(t, tasks, "Fail when generated agent ISO path is missing")]

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

	if got, _ := isoPathStat["when"].(string); got != "bootwright_install_agent_action_effective == 'boot_machine'" {
		t.Fatalf("agent ISO path stat when got %v", isoPathStat["when"])
	}
	when, ok = isoPathFail["when"].(string)
	if !ok {
		t.Fatalf("agent ISO path failure when got %v", isoPathFail["when"])
	}
	for _, want := range []string{
		"bootwright_install_agent_action_effective == 'boot_machine'",
		"not bootwright_agent_iso_path_stat.stat.exists",
	} {
		if !strings.Contains(when, want) {
			t.Fatalf("agent ISO path failure when missing %q: %v", want, isoPathFail["when"])
		}
	}
}

func TestInstallAgentFetchesAgentISOWithoutBecome(t *testing.T) {
	topTasks := readAnsibleTasks(t, "ansible/roles/openshift/install_agent/tasks/create_iso.yml")
	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Create cluster agent ISO when install is not already complete")], "block")
	localDirIdx := findAnsibleTask(t, tasks, "Create local installer artifact directory")
	userIdx := findAnsibleTask(t, tasks, "Resolve SSH transfer user")
	transferIdx := findAnsibleTask(t, tasks, "Transfer generated agent ISO to local runtime state")
	recordIdx := findAnsibleTask(t, tasks, "Record local generated agent ISO path")
	if !(localDirIdx < userIdx && userIdx < transferIdx && transferIdx < recordIdx) {
		t.Fatalf("install_agent must prepare the local directory, resolve transfer user, transfer ISO, then record the local path")
	}

	if got := tasks[userIdx]["become"]; got != false {
		t.Fatalf("transfer user detection must run without become, got %v", got)
	}
	userCommand, ok := tasks[userIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no command body", tasks[userIdx]["name"])
	}
	if !stringListContains(userCommand["argv"], "id") || !stringListContains(userCommand["argv"], "-un") {
		t.Fatalf("transfer user detection must resolve the SSH user with id -un, got %v", userCommand["argv"])
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
}

func TestDestroyClusterRemovesWholeClusterRuntimeDir(t *testing.T) {
	body := readRepoFile(t, "ansible/roles/openshift/destroy_agent/tasks/main.yml")
	for _, want := range []string{
		"bootwright_cluster_runtime_dir: \"{{ bootwright_state_dir }}/runtime/{{ bootwright_current_cluster.name }}\"",
		"bootwright_process_cleanup_pattern: \"runtime/{{ bootwright_current_cluster.name }}/\"",
		"path: \"{{ bootwright_cluster_runtime_dir }}\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("destroy_agent missing %q", want)
		}
	}
}

func TestHostBaseFirewalldAvailabilityRequiresRunningDaemon(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/roles/shared/host_base/tasks/main.yml")
	binaryIdx := findAnsibleTask(t, tasks, "Detect firewall-cmd binary")
	stateIdx := findAnsibleTask(t, tasks, "Detect running firewalld daemon")
	factIdx := findAnsibleTask(t, tasks, "Set firewalld availability fact")
	if !(binaryIdx < stateIdx && stateIdx < factIdx) {
		t.Fatalf("host_base must detect firewall-cmd before probing daemon state and setting the availability fact")
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
	tasks := readAnsibleTasks(t, "ansible/roles/shared/host_base/tasks/main.yml")
	dnfStatIdx := findAnsibleTask(t, tasks, "Stat dnf.conf")
	dnfIdx := findAnsibleTask(t, tasks, "Allow unavailable DNF repositories")
	yumStatIdx := findAnsibleTask(t, tasks, "Stat yum.conf")
	yumIdx := findAnsibleTask(t, tasks, "Allow unavailable YUM repositories")
	installIdx := findAnsibleTask(t, tasks, "Install base host packages")
	if !(dnfStatIdx < dnfIdx && dnfIdx < installIdx && yumStatIdx < yumIdx && yumIdx < installIdx) {
		t.Fatalf("host_base must allow unavailable Red Hat repos before installing packages")
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
	tasks := readAnsibleTasks(t, "ansible/roles/bastion/ocp_clis/tasks/main.yml")
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
	tasks := readAnsibleTasks(t, "ansible/roles/bastion/ocp_clis/tasks/main.yml")
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
		if importRole["name"] == "host_proxy" && importRole["tasks_from"] == "facts" {
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
	switch got := include.(type) {
	case string:
		if strings.TrimSpace(got) != want {
			t.Fatalf("%s include_tasks got %q, want %q", task["name"], got, want)
		}
	case map[string]any:
		file, ok := got["file"].(string)
		if !ok {
			t.Fatalf("%s include_tasks has no file", task["name"])
		}
		if strings.TrimSpace(file) != want {
			t.Fatalf("%s include_tasks file got %q, want %q", task["name"], file, want)
		}
	default:
		t.Fatalf("%s is not an include_tasks task", task["name"])
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
		for _, role := range entry.Roles.HostSetupRoles {
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
	for _, base := range []string{
		"ansible/roles/shared",
		"ansible/roles/providers",
		"ansible/roles/cluster_infra",
		"ansible/roles/openshift",
	} {
		info, err := os.Stat(filepath.Join(repoRoot(t), base, role))
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func ansibleAdapterRoleDirs(t *testing.T) []string {
	t.Helper()
	type rule struct {
		base     string
		prefixes []string
	}
	rules := []rule{
		{base: "ansible/roles/shared", prefixes: []string{"host_libvirt"}},
		{base: "ansible/roles/providers", prefixes: []string{""}},
		{base: "ansible/roles/cluster_infra", prefixes: []string{"substrate_"}},
		{base: "ansible/roles/openshift", prefixes: []string{"boot_", "media_"}},
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
					out = append(out, entry.Name())
					break
				}
			}
		}
	}
	return out
}
