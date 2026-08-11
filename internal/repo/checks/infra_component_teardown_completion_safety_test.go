package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestInfraComponentTeardownFailsClosedBeforeEvidenceRemoval(t *testing.T) {
	cases := []struct {
		role  string
		block string
	}{
		{role: "infra_component_artifact_server_http", block: "Tear down the artifact server before deleting its ownership evidence"},
		{role: "infra_component_load_balancer_haproxy", block: "Tear down HAProxy before deleting its ownership evidence"},
		{role: "infra_component_name_resolution_dnsmasq", block: "Tear down dnsmasq before deleting its ownership evidence"},
		{role: "infra_component_ntp_chrony", block: "Tear down NTP before deleting its ownership evidence"},
		{role: "infra_component_proxy_squid", block: "Tear down Squid before deleting its ownership evidence"},
		{role: "infra_component_registry_mirror", block: "Tear down the mirror registry before deleting its ownership evidence"},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			path := "ansible/collections/ansible_collections/bootwright/core/roles/" + tc.role + "/tasks/destroy.yml"
			tasks := readAnsibleTasks(t, path)
			validateIdx := findTaskContainingName(t, tasks, "Validate the live")
			phaseResolveIdx := findTaskContainingName(t, tasks, "external cleanup phase")
			ownershipIdx := findTaskContainingName(t, tasks, "Prove declared")
			boundaryIdx := findTaskContainingName(t, tasks, "ambiguous replay boundary")
			firewalldGateIdx := findTaskContainingName(t, tasks, "conclusive firewalld probe")
			blockIdx := findAnsibleTask(t, tasks, tc.block)
			if !(validateIdx < phaseResolveIdx && phaseResolveIdx < ownershipIdx && ownershipIdx < boundaryIdx && boundaryIdx < firewalldGateIdx && firewalldGateIdx < blockIdx) {
				t.Fatalf("%s must validate the host record, resolve its durable cleanup phase, prove the live marker, refuse an ambiguous replay, and prove firewalld before teardown: record=%d phase=%d live=%d boundary=%d firewall=%d teardown=%d", path, validateIdx, phaseResolveIdx, ownershipIdx, boundaryIdx, firewalldGateIdx, blockIdx)
			}
			boundary := tasks[boundaryIdx]["ansible.builtin.assert"].(map[string]any)
			for _, want := range []string{"bootwright_infra_destroy_state_stat.stat.exists", "bootwright_ownership_validated_stat.stat.exists", "bootwright_infra_destroy_phase_complete", "bootwright_mutating_invocation", "No "} {
				if !strings.Contains(fmt.Sprint(boundary), want) {
					t.Errorf("%s replay-boundary refusal missing %q: %v", path, want, boundary)
				}
			}
			assertion, ok := tasks[firewalldGateIdx]["ansible.builtin.assert"].(map[string]any)
			if !ok {
				t.Fatalf("%s firewalld gate must be a hard assertion, got %v", path, tasks[firewalldGateIdx])
			}
			conditions := fmt.Sprint(assertion["that"])
			for _, want := range []string{"stat.exists is defined", "stat.isreg", "stat.islnk", "stat.executable", "rc is defined", "stdout is defined", "stderr is defined", "rc | int == 252", "bootwright_firewalld_available"} {
				if !strings.Contains(conditions, want) {
					t.Errorf("%s firewalld gate does not prove %q: %v", path, want, assertion["that"])
				}
			}
			if failure := fmt.Sprint(assertion["fail_msg"]); !strings.Contains(failure, "bootwright_mutating_invocation") || !strings.Contains(failure, "No ") {
				t.Errorf("%s firewalld refusal must retain state and name the exact retry: %v", path, assertion["fail_msg"])
			}
			firewalldWhen := fmt.Sprint(tasks[firewalldGateIdx]["when"])
			if !strings.Contains(firewalldWhen, "bootwright_infra_destroy_state_stat.stat.exists") || !strings.Contains(firewalldWhen, "bootwright_infra_destroy_phase_complete") {
				t.Errorf("%s can block an all-members-absent replay on irrelevant firewalld state: when=%v", path, tasks[firewalldGateIdx]["when"])
			}

			teardown := nestedAnsibleTasks(t, tasks[blockIdx], "block")
			completeIdx := findIncludeRoleTasksFrom(t, teardown, "complete_infra_component_destroy.yml")
			if completeIdx != len(teardown)-1 {
				t.Fatalf("%s must delegate evidence-last retirement only after external cleanup: complete=%d tasks=%d", path, completeIdx, len(teardown))
			}
			completeVars, ok := teardown[completeIdx]["vars"].(map[string]any)
			if !ok || fmt.Sprint(completeVars["bootwright_ownership_expected_record"]) != "{{ bootwright_infra_destroy_expected_owner }}" {
				t.Fatalf("%s destroy completion must compare-and-swap the validated owner snapshot: %v", path, teardown[completeIdx])
			}
			for _, task := range teardown[:completeIdx] {
				if _, mutation := firstInfraTeardownMutation(task); mutation && task["failed_when"] == false {
					t.Errorf("%s task %q suppresses a state-changing failure before evidence removal", path, task["name"])
				}
			}
			for _, task := range teardown[:completeIdx] {
				if _, ok := task["ansible.posix.firewalld"]; !ok {
					continue
				}
				when := fmt.Sprint(task["when"])
				if !strings.Contains(when, "bootwright_infra_destroy_state_stat.stat.exists") {
					t.Errorf("%s task %q can remove a same-port foreign firewall rule without the exact current-context live marker: when=%v", path, task["name"], task["when"])
				}
			}

			rescue := nestedAnsibleTasks(t, tasks[blockIdx], "rescue")
			if len(rescue) != 1 {
				t.Fatalf("%s teardown must have one terminal refusal, got %d rescue tasks", path, len(rescue))
			}
			failure, ok := rescue[0]["ansible.builtin.fail"].(map[string]any)
			if !ok {
				t.Fatalf("%s teardown rescue must hard-fail, got %v", path, rescue[0])
			}
			message := fmt.Sprint(failure["msg"])
			for _, want := range []string{"ansible_failed_task.name", "ansible_failed_result.msg", "ownership record or final context marker", "bootwright_mutating_invocation"} {
				if !strings.Contains(message, want) {
					t.Errorf("%s teardown refusal missing %q: %v", path, want, failure["msg"])
				}
			}
		})
	}

	completion := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_destroy.yml")
	phaseIdx := findIncludeTasksFrom(t, completion, "mark_infra_component_external_cleanup.yml")
	stateIdx := findAnsibleTask(t, completion, "Remove final context-marked infra-component state directory")
	transitionIdx := findIncludeTasksFrom(t, completion, "retire_infra_component_destroy_transition.yml")
	globalIdx := findIncludeTasksFrom(t, completion, "retire_infra_component_global_claim.yml")
	recordIdx := findIncludeTasksFrom(t, completion, "remove_resource.yml")
	if !(phaseIdx < stateIdx && stateIdx < transitionIdx && transitionIdx < globalIdx && globalIdx < recordIdx && recordIdx == len(completion)-1) {
		t.Fatalf("shared destroy completion must persist cleanup phase, remove state and exact transition/global authority, then remove the controller owner last: phase=%d state=%d transition=%d global=%d record=%d tasks=%d", phaseIdx, stateIdx, transitionIdx, globalIdx, recordIdx, len(completion))
	}
	recordVars, ok := completion[recordIdx]["vars"].(map[string]any)
	if !ok || fmt.Sprint(recordVars["bootwright_ownership_expected_record"]) != "{{ bootwright_infra_destroy_phase_record }}" {
		t.Fatalf("shared destroy completion must compare-and-swap the exact cleanup-phase owner last: %v", completion[recordIdx])
	}
}

func TestInfraComponentApplyTransitionsBracketEveryRoleMutation(t *testing.T) {
	containerGate := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/infra_component_container_gate.yml")
	modeIdx := findIncludeTasksFrom(t, containerGate, "apply_mode_gate.yml")
	beginIdx := findIncludeTasksFrom(t, containerGate, "begin_infra_component_transition.yml")
	claimIdx := findTaskContainingName(t, containerGate, "Acquire service claim atomically before mutation")
	winnerIdx := findTaskContainingName(t, containerGate, "Require exact atomic service claim winner")
	normalizeIdx := findTaskContainingName(t, containerGate, "Normalize service claim directory after exact claim")
	if !(modeIdx < beginIdx && beginIdx < claimIdx && claimIdx < winnerIdx && winnerIdx < normalizeIdx) {
		t.Fatalf("container apply gate must publish transition authority, atomically acquire and re-read the claim, then normalize its directory: mode=%d begin=%d claim=%d winner=%d normalize=%d", modeIdx, beginIdx, claimIdx, winnerIdx, normalizeIdx)
	}

	begin := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_transition.yml")
	publishIdx := findAnsibleTask(t, begin, "Publish durable infra-component transition atomically before role mutation")
	retireDecisionIdx := findAnsibleTask(t, begin, "Require exact old-only infra-component endpoint decision")
	bindIdx := findIncludeTasksFrom(t, begin, "bind_infra_component_host_operation.yml")
	preflightIdx := findIncludeTasksFrom(t, begin, "preflight_host_shared_service_consequences.yml")
	if !(retireDecisionIdx < bindIdx && bindIdx < preflightIdx && preflightIdx < publishIdx) {
		t.Fatalf("begin helper must close its read-only retirement decision, bind and symmetrically preflight the exact selection, then publish durable transition evidence: decision=%d bind=%d preflight=%d publish=%d", retireDecisionIdx, bindIdx, preflightIdx, publishIdx)
	}
	destroyBegin := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/begin_infra_component_destroy_transition.yml")
	destroyBindIdx := findIncludeTasksFrom(t, destroyBegin, "bind_infra_component_host_operation.yml")
	destroyPreflightIdx := findIncludeTasksFrom(t, destroyBegin, "preflight_host_shared_service_consequences.yml")
	destroyPublishIdx := findAnsibleTask(t, destroyBegin, "Publish authoritative host-global infra-component destroying claim atomically")
	if !(destroyBindIdx < destroyPreflightIdx && destroyPreflightIdx < destroyPublishIdx) {
		t.Fatalf("destroy helper must bind and symmetrically preflight the exact selection before publishing host-global authority: bind=%d preflight=%d publish=%d", destroyBindIdx, destroyPreflightIdx, destroyPublishIdx)
	}

	complete := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/ownership_record/tasks/complete_infra_component_transition.yml")
	ownerGateIdx := findAnsibleTask(t, complete, "Require completed infra-component owner before claim retirement")
	recoveryGateIdx := findAnsibleTask(t, complete, "Require exact recovery record before claim retirement")
	claimGateIdx := findAnsibleTask(t, complete, "Require unchanged durable infra-component transition before retirement")
	retireRecoveryIdx := findIncludeTasksFrom(t, complete, "remove_resource.yml")
	retireClaimIdx := findAnsibleTask(t, complete, "Retire completed durable local infra-component transition atomically")
	claimRetiredGateIdx := findAnsibleTask(t, complete, "Require exact durable local infra-component transition retirement")
	retireEndpointsIdx := findIncludeTasksFrom(t, complete, "retire_infra_component_endpoint_claims.yml")
	settleGlobalIdx := findIncludeTasksFrom(t, complete, "settle_infra_component_global_claim.yml")
	if !(ownerGateIdx < recoveryGateIdx && recoveryGateIdx < claimGateIdx && claimGateIdx < retireRecoveryIdx && retireRecoveryIdx < retireClaimIdx && retireClaimIdx < claimRetiredGateIdx && claimRetiredGateIdx < retireEndpointsIdx && retireEndpointsIdx < settleGlobalIdx && settleGlobalIdx == len(complete)-1) {
		t.Fatalf("complete helper must prove exact owner/recovery/local authority, retire recovery and local claims, then endpoints and host-global authority last: owner=%d recovery=%d local=%d recovery-remove=%d local-remove=%d local-proof=%d endpoints=%d global=%d tasks=%d", ownerGateIdx, recoveryGateIdx, claimGateIdx, retireRecoveryIdx, retireClaimIdx, claimRetiredGateIdx, retireEndpointsIdx, settleGlobalIdx, len(complete))
	}

	cases := []struct {
		role          string
		containerGate bool
	}{
		{role: "infra_component_artifact_server_http", containerGate: true},
		{role: "infra_component_load_balancer_haproxy", containerGate: true},
		{role: "infra_component_name_resolution_dnsmasq", containerGate: true},
		{role: "infra_component_ntp_chrony"},
		{role: "infra_component_proxy_squid", containerGate: true},
		{role: "infra_component_registry_mirror", containerGate: true},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			path := "ansible/collections/ansible_collections/bootwright/core/roles/" + tc.role + "/tasks/main.yml"
			tasks := readAnsibleTasks(t, path)
			desiredIdx := findTaskSettingFact(t, tasks, "bootwright_infra_transition_desired")
			desiredFacts := tasks[desiredIdx]["ansible.builtin.set_fact"].(map[string]any)
			desired, ok := desiredFacts["bootwright_infra_transition_desired"].(map[string]any)
			if !ok {
				t.Fatalf("%s desired transition must be one exact mapping: %v", path, desiredFacts["bootwright_infra_transition_desired"])
			}
			for _, key := range []string{"kind", "provider", "name", "paths", "labels", "attributes"} {
				if _, found := desired[key]; !found {
					t.Errorf("%s desired transition is missing %q: %v", path, key, desired)
				}
			}
			if len(desired) != 6 {
				t.Errorf("%s desired transition contains fields outside the exact composite: %v", path, desired)
			}
			labels, ok := desired["labels"].(map[string]any)
			if !ok || len(labels) != 3 {
				t.Fatalf("%s desired transition labels must be the three exact ownership labels: %v", path, desired["labels"])
			}
			for _, key := range []string{"bootwright.kind", "bootwright.provider", "bootwright.name"} {
				if _, found := labels[key]; !found {
					t.Errorf("%s desired transition labels are missing %q: %v", path, key, labels)
				}
			}

			var boundaryIdx int
			if tc.containerGate {
				boundaryIdx = findIncludeRoleTasksFrom(t, tasks, "infra_component_container_gate.yml")
				if desiredIdx >= boundaryIdx {
					t.Fatalf("%s must resolve the exact desired transition before the container gate publishes it: desired=%d gate=%d", path, desiredIdx, boundaryIdx)
				}
			} else {
				modeIdx := findIncludeRoleTasksFrom(t, tasks, "apply_mode_gate.yml")
				boundaryIdx = findIncludeRoleTasksFrom(t, tasks, "begin_infra_component_transition.yml")
				if !(modeIdx < desiredIdx && desiredIdx < boundaryIdx) {
					t.Fatalf("%s must finish its read-only mode gate, resolve desired state, then publish the transition: mode=%d desired=%d begin=%d", path, modeIdx, desiredIdx, boundaryIdx)
				}
			}
			for i, task := range tasks[:boundaryIdx] {
				if module, mutation := firstInfraApplyMutation(task); mutation {
					t.Errorf("%s task %q mutates through %s before the durable transition boundary at task %d", path, task["name"], module, i)
				}
			}

			firstMutationIdx := -1
			for i, task := range tasks[boundaryIdx+1:] {
				if _, mutation := firstInfraApplyMutation(task); mutation {
					firstMutationIdx = boundaryIdx + 1 + i
					break
				}
			}
			if firstMutationIdx < 0 {
				t.Fatalf("%s has no role mutation after its transition boundary", path)
			}

			retireIdx := findTaskLoopingOver(t, tasks, "bootwright_infra_transition_retire_ports")
			recordIdx := findIncludeRoleTasksFrom(t, tasks, "resource.yml")
			completeIdx := findIncludeRoleTasksFrom(t, tasks, "complete_infra_component_transition.yml")
			if !(boundaryIdx < firstMutationIdx && firstMutationIdx < retireIdx && retireIdx < recordIdx && recordIdx < completeIdx && completeIdx == len(tasks)-1) {
				t.Fatalf("%s must publish transition authority before mutation, retire old endpoints after convergence, write the exact owner, then complete the transition last: boundary=%d mutation=%d retire=%d record=%d complete=%d tasks=%d", path, boundaryIdx, firstMutationIdx, retireIdx, recordIdx, completeIdx, len(tasks))
			}
			if _, ok := tasks[retireIdx]["ansible.posix.firewalld"]; !ok || !strings.Contains(fmt.Sprint(tasks[retireIdx]["when"]), "bootwright_firewalld_available") {
				t.Errorf("%s must retire old endpoints only through conclusively available firewalld: %v", path, tasks[retireIdx])
			}
			recordVars, ok := tasks[recordIdx]["vars"].(map[string]any)
			if !ok {
				t.Fatalf("%s owner writer must receive exact transition fields: %v", path, tasks[recordIdx])
			}
			fields, ok := recordVars["bootwright_ownership_fields"].(map[string]any)
			if !ok {
				t.Fatalf("%s owner writer fields must be a mapping: %v", path, recordVars["bootwright_ownership_fields"])
			}
			for _, key := range []string{"provider", "paths", "labels", "attributes"} {
				want := "{{ bootwright_infra_transition_desired." + key + " }}"
				if got := fmt.Sprint(fields[key]); got != want {
					t.Errorf("%s owner writer %s = %q, want exact transition field %q", path, key, got, want)
				}
			}
		})
	}
}

func TestHAProxyTeardownProvesSharedStateBeforeGlobalMutation(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_load_balancer_haproxy/tasks/destroy.yml"
	tasks := readAnsibleTasks(t, path)
	statIdx := findAnsibleTask(t, tasks, "Probe the shared HAProxy sysctl drop-in")
	readIdx := findAnsibleTask(t, tasks, "Read the shared HAProxy sysctl drop-in")
	sysctlGateIdx := findAnsibleTask(t, tasks, "Require conclusive shared HAProxy sysctl ownership")
	preflightIdx := findAnsibleTask(t, tasks, "Prove Podman can enumerate HAProxy containers before teardown")
	preflightGateIdx := findAnsibleTask(t, tasks, "Require conclusive Podman access before HAProxy teardown")
	declaredProbeIdx := findAnsibleTask(t, tasks, "Probe declared HAProxy consumers before teardown")
	recordedProbeIdx := findAnsibleTask(t, tasks, "Probe recorded HAProxy consumers before teardown")
	declaredGateIdx := findAnsibleTask(t, tasks, "Require conclusive declared HAProxy consumer probes")
	recordedGateIdx := findAnsibleTask(t, tasks, "Require conclusive recorded HAProxy consumer probes")
	containerSetGateIdx := findAnsibleTask(t, tasks, "Require every HAProxy container to have exact current-context evidence")
	portsIdx := findAnsibleTask(t, tasks, "Canonicalize surviving HAProxy frontend ports")
	blockIdx := findAnsibleTask(t, tasks, "Tear down HAProxy before deleting its ownership evidence")
	if !(statIdx < readIdx && readIdx < sysctlGateIdx && sysctlGateIdx < preflightIdx && preflightIdx < preflightGateIdx && preflightGateIdx < declaredProbeIdx && declaredProbeIdx < recordedProbeIdx && recordedProbeIdx < declaredGateIdx && declaredGateIdx < recordedGateIdx && recordedGateIdx < containerSetGateIdx && containerSetGateIdx < portsIdx && portsIdx < blockIdx) {
		t.Fatalf("HAProxy must prove the global sysctl, every live consumer, and each surviving port before its first mutation: stat=%d read=%d sysctl-gate=%d podman=%d podman-gate=%d declared=%d recorded=%d declared-gate=%d recorded-gate=%d set-gate=%d ports=%d teardown=%d", statIdx, readIdx, sysctlGateIdx, preflightIdx, preflightGateIdx, declaredProbeIdx, recordedProbeIdx, declaredGateIdx, recordedGateIdx, containerSetGateIdx, portsIdx, blockIdx)
	}
	if tasks[statIdx]["failed_when"] != false || tasks[readIdx]["failed_when"] != false || tasks[preflightIdx]["failed_when"] != false || tasks[declaredProbeIdx]["failed_when"] != false || tasks[recordedProbeIdx]["failed_when"] != false {
		t.Fatal("HAProxy read-only probes must reach their actionable assertions")
	}
	sysctlGate := tasks[sysctlGateIdx]["ansible.builtin.assert"].(map[string]any)
	for _, want := range []string{"stat.exists is defined", "stat.isreg", "stat.islnk", "content is defined", "ip_nonlocal_bind", "bootwright_mutating_invocation"} {
		if !strings.Contains(fmt.Sprint(sysctlGate), want) {
			t.Errorf("HAProxy global-state gate missing %q: %v", want, sysctlGate)
		}
	}
	assertConclusiveCommandResult(t, tasks[preflightGateIdx], "bootwright_haproxy_preflight")
	if !strings.Contains(fmt.Sprint(tasks[preflightIdx]), "--no-trunc") {
		t.Errorf("HAProxy inventory must expose full IDs for exact set comparison: %v", tasks[preflightIdx])
	}
	for _, idx := range []int{declaredGateIdx, recordedGateIdx} {
		gate := fmt.Sprint(tasks[idx])
		for _, want := range []string{"bootwright_podman_container_identity", "bootwright_podman_explicit_absence", "bootwright.context", "bootwright.provider", "bootwright.name", "bootwright_mutating_invocation"} {
			if !strings.Contains(gate, want) {
				t.Errorf("HAProxy consumer gate %q missing %q: %v", tasks[idx]["name"], want, tasks[idx])
			}
		}
	}
	for _, want := range []string{"bootwright_haproxy_preflight.stdout_lines", "difference", "bootwright_haproxy_known_container_ids", "bootwright_mutating_invocation"} {
		if !strings.Contains(fmt.Sprint(tasks[containerSetGateIdx]), want) {
			t.Errorf("HAProxy exact-container-set gate missing %q: %v", want, tasks[containerSetGateIdx])
		}
	}
	for _, want := range []string{"frontendPorts", "is string", "is match", "split(',')", "regex_replace", "max", "unique", "sort"} {
		if !strings.Contains(fmt.Sprint(tasks[recordedGateIdx]), want) {
			t.Errorf("HAProxy recorded consumer gate does not require canonical exact ports %q: %v", want, tasks[recordedGateIdx])
		}
	}

	teardown := nestedAnsibleTasks(t, tasks[blockIdx], "block")
	removeIdx := findAnsibleTask(t, teardown, "Remove HAProxy container and reap orphans")
	remainingIdx := findAnsibleTask(t, teardown, "List remaining bootwright HAProxy containers")
	remainingGateIdx := findAnsibleTask(t, teardown, "Require conclusive remaining HAProxy container state")
	sysctlRemoveIdx := findAnsibleTask(t, teardown, "Remove HAProxy sysctl drop-in when no instances remain")
	firewallIdx := findAnsibleTask(t, teardown, "Close HAProxy frontend ports on host firewall (firewalld)")
	completeIdx := findIncludeRoleTasksFrom(t, teardown, "complete_infra_component_destroy.yml")
	if !(removeIdx < remainingIdx && remainingIdx < remainingGateIdx && remainingGateIdx < sysctlRemoveIdx && sysctlRemoveIdx < firewallIdx && firewallIdx < completeIdx && completeIdx == len(teardown)-1) {
		t.Fatalf("HAProxy must re-probe Podman and prove success before global mutation, then delegate evidence-last completion: remove=%d list=%d gate=%d sysctl=%d firewall=%d complete=%d tasks=%d", removeIdx, remainingIdx, remainingGateIdx, sysctlRemoveIdx, firewallIdx, completeIdx, len(teardown))
	}
	if teardown[remainingIdx]["failed_when"] != false {
		t.Fatal("the HAProxy post-removal probe must reach its actionable assertion")
	}
	assertConclusiveCommandResult(t, teardown[remainingGateIdx], "bootwright_haproxy_remaining")
	for _, want := range []string{"stdout_lines", "difference", "bootwright_haproxy_surviving_container_ids"} {
		if !strings.Contains(fmt.Sprint(teardown[remainingGateIdx]), want) {
			t.Errorf("HAProxy post-removal gate does not refuse a new or selected prefixed container via %q: %v", want, teardown[remainingGateIdx])
		}
	}
	if !strings.Contains(fmt.Sprint(teardown[remainingIdx]), "--no-trunc") {
		t.Errorf("HAProxy post-removal inventory must expose full IDs: %v", teardown[remainingIdx])
	}
	for _, want := range []string{"bootwright_infra_destroy_state_stat.stat.exists", "bootwright_haproxy_remaining.stdout"} {
		if !strings.Contains(fmt.Sprint(teardown[sysctlRemoveIdx]["when"]), want) {
			t.Errorf("HAProxy sysctl teardown is not last-consumer-only via %q: when=%v", want, teardown[sysctlRemoveIdx]["when"])
		}
	}
	firewallWhen := fmt.Sprint(teardown[firewallIdx]["when"])
	for _, want := range []string{"bootwright_infra_destroy_state_stat.stat.exists", "item | int not in bootwright_haproxy_surviving_frontend_ports"} {
		if !strings.Contains(firewallWhen, want) {
			t.Errorf("HAProxy firewall teardown does not preserve a surviving exact port via %q: when=%v", want, teardown[firewallIdx]["when"])
		}
	}
	if strings.Contains(firewallWhen, "bootwright_haproxy_remaining.stdout") {
		t.Errorf("HAProxy firewall teardown still leaks every selected port while an unrelated HAProxy survives: when=%v", teardown[firewallIdx]["when"])
	}
}

func TestHAProxyOwnershipRecordsCanonicalFrontendPorts(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_load_balancer_haproxy/tasks/main.yml"
	tasks := readAnsibleTasks(t, path)
	desiredTask := tasks[findTaskSettingFact(t, tasks, "bootwright_infra_transition_desired")]
	desiredFacts, ok := desiredTask["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("HAProxy desired transition task must set facts: %v", desiredTask)
	}
	desired, ok := desiredFacts["bootwright_infra_transition_desired"].(map[string]any)
	if !ok {
		t.Fatalf("HAProxy desired transition must be a mapping: %v", desiredFacts["bootwright_infra_transition_desired"])
	}
	attributes, ok := desired["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("HAProxy desired transition attributes must be a mapping: %v", desired["attributes"])
	}
	text := fmt.Sprint(attributes)
	for _, want := range []string{"frontendPorts", "listenPort", "map('int')", "unique", "sort", "map('string')", "regex_replace", "join(',')"} {
		if !strings.Contains(text, want) {
			t.Errorf("HAProxy desired transition does not persist canonical exact frontend ports via %q: %v", want, attributes)
		}
	}
	record := tasks[findAnsibleTask(t, tasks, "Record HAProxy ownership")]
	if !strings.Contains(fmt.Sprint(record), "{{ bootwright_infra_transition_desired.attributes }}") {
		t.Errorf("HAProxy ownership writer must consume the exact desired transition attributes: %v", record)
	}
}

func TestHAProxyTeardownUnionsRecordedAndDesiredPortsAcrossDrift(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_load_balancer_haproxy/tasks/destroy.yml"
	tasks := readAnsibleTasks(t, path)
	consumerResolveIdx := findAnsibleTask(t, tasks, "Resolve declared HAProxy consumers before teardown")
	recordedGateIdx := findAnsibleTask(t, tasks, "Require conclusive recorded HAProxy consumer probes")
	desiredIdx := findAnsibleTask(t, tasks, "Resolve selected HAProxy frontend ports")
	recordedIdx := findAnsibleTask(t, tasks, "Include recorded selected HAProxy frontend ports")
	blockIdx := findAnsibleTask(t, tasks, "Tear down HAProxy before deleting its ownership evidence")
	if !(consumerResolveIdx < recordedGateIdx && recordedGateIdx < desiredIdx && desiredIdx < recordedIdx && recordedIdx < blockIdx) {
		t.Fatalf("HAProxy must load and validate the selected record, then union recorded and desired ports before teardown: consumers=%d gate=%d desired=%d recorded=%d teardown=%d", consumerResolveIdx, recordedGateIdx, desiredIdx, recordedIdx, blockIdx)
	}
	for _, want := range []string{"bootwright_ownership_validated_record", "bootwright_ownership_validated_stat.stat.exists"} {
		if !strings.Contains(fmt.Sprint(tasks[consumerResolveIdx]), want) {
			t.Errorf("HAProxy selected-port recovery can miss the exact live owner record without %q: %v", want, tasks[consumerResolveIdx])
		}
	}
	for _, want := range []string{"bootwright_component.frontends", "listenPort", "map('int')", "unique", "sort"} {
		if !strings.Contains(fmt.Sprint(tasks[desiredIdx]), want) {
			t.Errorf("HAProxy desired selected-port set missing %q: %v", want, tasks[desiredIdx])
		}
	}
	for _, want := range []string{"bootwright_haproxy_selected_frontend_ports", "attributes.frontendPorts", "split(',')", "regex_replace", "map('int')", "unique", "sort", "bootwright_component.providerName", "bootwright_component.name"} {
		if !strings.Contains(fmt.Sprint(tasks[recordedIdx]), want) {
			t.Errorf("HAProxy recorded selected-port union missing %q: %v", want, tasks[recordedIdx])
		}
	}
	if strings.Contains(fmt.Sprint(tasks[recordedIdx]["when"]), "item.rc | int == 0") {
		t.Errorf("HAProxy retry would drop recorded ports after its container was already removed: when=%v", tasks[recordedIdx]["when"])
	}
	teardown := nestedAnsibleTasks(t, tasks[blockIdx], "block")
	firewall := teardown[findAnsibleTask(t, teardown, "Close HAProxy frontend ports on host firewall (firewalld)")]
	if got := fmt.Sprint(firewall["loop"]); !strings.Contains(got, "bootwright_haproxy_selected_frontend_ports") {
		t.Errorf("HAProxy firewall teardown does not iterate the recorded/desired union: loop=%v", firewall["loop"])
	}
}

func TestNTPTeardownProvesServiceAndContextBeforeExternalMutation(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_ntp_chrony/tasks/destroy.yml"
	tasks := readAnsibleTasks(t, path)
	ownershipIdx := findAnsibleTask(t, tasks, "Prove declared NTP live ownership before teardown")
	configProbeIdx := findAnsibleTask(t, tasks, "Probe exact NTP config before teardown")
	configGateIdx := findAnsibleTask(t, tasks, "Require exact NTP config live authority")
	probeIdx := findAnsibleTask(t, tasks, "Probe chrony service and durable restart boundary")
	markerProbeIdx := findAnsibleTask(t, tasks, "Probe durable chrony restart boundary")
	markerReadIdx := findAnsibleTask(t, tasks, "Read durable chrony restart boundary")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve exact chrony service observations")
	stateGateIdx := findAnsibleTask(t, tasks, "Require conclusive chrony service and restart-boundary evidence")
	blockIdx := findAnsibleTask(t, tasks, "Tear down NTP before deleting its ownership evidence")
	if !(ownershipIdx < configProbeIdx && configProbeIdx < configGateIdx && configGateIdx < probeIdx && probeIdx < markerProbeIdx && markerProbeIdx < markerReadIdx && markerReadIdx < resolveIdx && resolveIdx < stateGateIdx && stateGateIdx < blockIdx) {
		t.Fatalf("NTP must prove live ownership, exact config, service state, and durable restart boundary before mutation: ownership=%d config=%d config-gate=%d probe=%d marker=%d read=%d resolve=%d gate=%d teardown=%d", ownershipIdx, configProbeIdx, configGateIdx, probeIdx, markerProbeIdx, markerReadIdx, resolveIdx, stateGateIdx, blockIdx)
	}
	if tasks[probeIdx]["failed_when"] != false || tasks[markerProbeIdx]["failed_when"] != false || tasks[markerReadIdx]["failed_when"] != false {
		t.Fatal("the NTP service probe must reach its actionable assertion")
	}
	for _, idx := range []int{configGateIdx, stateGateIdx} {
		if !strings.Contains(fmt.Sprint(tasks[idx]), "bootwright_mutating_invocation") {
			t.Errorf("NTP service refusal %q does not name the exact retry", tasks[idx]["name"])
		}
	}
	teardown := nestedAnsibleTasks(t, tasks[blockIdx], "block")
	markerWriteIdx := findAnsibleTask(t, teardown, "Persist required chrony restart before config removal")
	configIdx := findAnsibleTask(t, teardown, "Remove exact Bootwright chrony config")
	restartIdx := findAnsibleTask(t, teardown, "Restart chrony service after config removal")
	verificationIdx := findAnsibleTask(t, teardown, "Verify chrony after required restart")
	verificationGateIdx := findAnsibleTask(t, teardown, "Require chrony running after required restart")
	markerClearIdx := findAnsibleTask(t, teardown, "Clear completed chrony restart boundary")
	if !(markerWriteIdx < configIdx && configIdx < restartIdx && restartIdx < verificationIdx && verificationIdx < verificationGateIdx && verificationGateIdx < markerClearIdx) {
		t.Fatalf("NTP must persist restart intent before config deletion and clear it only after verified restart: marker=%d config=%d restart=%d verify=%d gate=%d clear=%d", markerWriteIdx, configIdx, restartIdx, verificationIdx, verificationGateIdx, markerClearIdx)
	}
	for _, name := range []string{"Close authoritative NTP ports on host firewall"} {
		task := teardown[findAnsibleTask(t, teardown, name)]
		if !strings.Contains(fmt.Sprint(task["when"]), "bootwright_infra_destroy_state_stat.stat.exists") {
			t.Errorf("NTP task %q can mutate a foreign same-path resource without a current-context live marker: when=%v", name, task["when"])
		}
	}
	restart := teardown[restartIdx]
	restartWhen := fmt.Sprint(restart["when"])
	if restart["failed_when"] == false || !strings.Contains(restartWhen, "bootwright_chrony_restart_marker_stat.stat.exists") || !strings.Contains(restartWhen, "bootwright_chrony_restart_marker_written.changed") {
		t.Errorf("NTP restart must hard-fail and replay either durable restart boundary: %v", restart)
	}
	if !strings.Contains(fmt.Sprint(teardown[markerWriteIdx]), "state', 'equalto', 'running") {
		t.Errorf("NTP must persist restart intent only for a service observed running: %v", teardown[markerWriteIdx])
	}
	for _, want := range []string{"ansible_facts.services is defined", "state', 'equalto', 'running'", "bootwright_mutating_invocation"} {
		if !strings.Contains(fmt.Sprint(teardown[verificationGateIdx]), want) {
			t.Errorf("NTP restart verification gate missing %q: %v", want, teardown[verificationGateIdx])
		}
	}
}

func TestRegistryTeardownPersistsExactTrustRefreshBoundary(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_registry_mirror/tasks/destroy.yml"
	tasks := readAnsibleTasks(t, path)
	resolveIdx := findAnsibleTask(t, tasks, "Resolve mirror-registry trust teardown authority")
	anchorProbeIdx := findAnsibleTask(t, tasks, "Probe the recorded mirror-registry trust anchor")
	markerProbeIdx := findAnsibleTask(t, tasks, "Probe the durable mirror-registry trust refresh boundary")
	markerReadIdx := findAnsibleTask(t, tasks, "Read the durable mirror-registry trust refresh boundary")
	gateIdx := findAnsibleTask(t, tasks, "Require exact mirror-registry trust teardown evidence")
	identityIdx := findAnsibleTask(t, tasks, "Resolve exact mirror-registry trust removal identity")
	contentIdx := findAnsibleTask(t, tasks, "Resolve durable mirror-registry trust removal content")
	blockIdx := findAnsibleTask(t, tasks, "Tear down the mirror registry before deleting its ownership evidence")
	if !(resolveIdx < anchorProbeIdx && anchorProbeIdx < markerProbeIdx && markerProbeIdx < markerReadIdx && markerReadIdx < gateIdx && gateIdx < identityIdx && identityIdx < contentIdx && contentIdx < blockIdx) {
		t.Fatalf("registry teardown must prove the exact anchor and shared durable refresh marker before resolving removal: resolve=%d anchor=%d marker=%d read=%d gate=%d identity=%d content=%d teardown=%d", resolveIdx, anchorProbeIdx, markerProbeIdx, markerReadIdx, gateIdx, identityIdx, contentIdx, blockIdx)
	}
	for _, idx := range []int{anchorProbeIdx, markerProbeIdx, markerReadIdx} {
		if tasks[idx]["failed_when"] != false {
			t.Errorf("registry trust probe %q must reach its actionable assertion", tasks[idx]["name"])
		}
	}
	if !strings.Contains(fmt.Sprint(tasks[resolveIdx]), ".ca-trust-refresh-pending") {
		t.Errorf("registry destroy does not consume the apply-side durable trust transition marker: %v", tasks[resolveIdx])
	}
	for _, want := range []string{"trust_anchor_recorded", "trust_checksum_recorded", "stat.isreg", "stat.islnk", "stat.pw_name", "stat.gr_name", "stat.mode", "stat.checksum", "b64decode", "(install|remove)", "bootwright_mutating_invocation"} {
		if !strings.Contains(fmt.Sprint(tasks[gateIdx]), want) {
			t.Errorf("registry trust evidence gate missing %q: %v", want, tasks[gateIdx])
		}
	}

	teardown := nestedAnsibleTasks(t, tasks[blockIdx], "block")
	markerWriteIdx := findAnsibleTask(t, teardown, "Persist required mirror-registry trust refresh before anchor removal")
	anchorRemoveIdx := findAnsibleTask(t, teardown, "Remove the exact mirror-registry trust anchor")
	refreshIdx := findAnsibleTask(t, teardown, "Refresh host trust after mirror-registry anchor removal")
	verifyProbeIdx := findAnsibleTask(t, teardown, "Probe removed mirror-registry trust anchor")
	verifyIdx := findAnsibleTask(t, teardown, "Require removed mirror-registry trust anchor")
	markerClearIdx := findAnsibleTask(t, teardown, "Clear the completed mirror-registry trust refresh boundary")
	completeIdx := findIncludeRoleTasksFrom(t, teardown, "complete_infra_component_destroy.yml")
	if !(markerWriteIdx < anchorRemoveIdx && anchorRemoveIdx < refreshIdx && refreshIdx < verifyProbeIdx && verifyProbeIdx < verifyIdx && verifyIdx < markerClearIdx && markerClearIdx < completeIdx && completeIdx == len(teardown)-1) {
		t.Fatalf("registry teardown must persist refresh intent before anchor removal, prove refreshed absence, clear the boundary, then delegate evidence-last completion: marker=%d anchor=%d refresh=%d probe=%d verify=%d clear=%d complete=%d tasks=%d", markerWriteIdx, anchorRemoveIdx, refreshIdx, verifyProbeIdx, verifyIdx, markerClearIdx, completeIdx, len(teardown))
	}
	if teardown[refreshIdx]["failed_when"] == false {
		t.Error("registry trust refresh must retain its durable boundary on command failure")
	}

	for _, want := range []string{"bootwright_registry_trust_managed", "bootwright_registry_trust_refresh_identity"} {
		if !strings.Contains(fmt.Sprint(teardown[markerWriteIdx]), want) {
			t.Errorf("registry destroy marker does not durably convert an apply transition to exact removal via %q: %v", want, teardown[markerWriteIdx])
		}
	}
}

func TestRegistryApplyPersistsTrustRefreshAcrossFirstFailureAndReapply(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/infra_component_registry_mirror/tasks/main.yml"
	tasks := readAnsibleTasks(t, path)
	validateIdx := findAnsibleTask(t, tasks, "Validate the existing mirror-registry ownership record before trust mutation")
	desiredIdx := findTaskSettingFact(t, tasks, "bootwright_infra_transition_desired")
	applyGateIdx := findAnsibleTask(t, tasks, "Enforce apply mode for this infra-component")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve desired and recorded mirror-registry trust evidence")
	anchorProbeIdx := findAnsibleTask(t, tasks, "Probe the mirror-registry trust anchor before apply")
	markerProbeIdx := findAnsibleTask(t, tasks, "Probe the durable mirror-registry trust transition before apply")
	markerReadIdx := findAnsibleTask(t, tasks, "Read the durable mirror-registry trust transition before apply")
	gateIdx := findAnsibleTask(t, tasks, "Require exact mirror-registry trust transition evidence before apply")
	stateIdx := findAnsibleTask(t, tasks, "Resolve mirror-registry trust transition state")
	identityIdx := findAnsibleTask(t, tasks, "Resolve exact mirror-registry trust transition identity")
	contentIdx := findAnsibleTask(t, tasks, "Resolve durable mirror-registry trust transition content")
	markerWriteIdx := findAnsibleTask(t, tasks, "Persist mirror-registry trust transition before host trust mutation")
	installIdx := findAnsibleTask(t, tasks, "Install exact mirror-registry trust anchor")
	removeIdx := findAnsibleTask(t, tasks, "Remove previously managed mirror-registry trust anchor")
	refreshIdx := findAnsibleTask(t, tasks, "Refresh host trust for the pending mirror-registry transition")
	verifyProbeIdx := findAnsibleTask(t, tasks, "Probe completed mirror-registry trust transition")
	verifyIdx := findAnsibleTask(t, tasks, "Require completed mirror-registry trust transition")
	containerIdx := findAnsibleTask(t, tasks, "Run mirror registry container")
	recordIdx := findAnsibleTask(t, tasks, "Record mirror registry ownership")
	completeIdx := findIncludeRoleTasksFrom(t, tasks, "complete_infra_component_transition.yml")
	if !(validateIdx < desiredIdx && desiredIdx < resolveIdx && resolveIdx < anchorProbeIdx && anchorProbeIdx < markerProbeIdx && markerProbeIdx < markerReadIdx && markerReadIdx < gateIdx && gateIdx < stateIdx && stateIdx < identityIdx && identityIdx < contentIdx && contentIdx < applyGateIdx && applyGateIdx < markerWriteIdx && markerWriteIdx < installIdx && markerWriteIdx < removeIdx && installIdx < refreshIdx && removeIdx < refreshIdx && refreshIdx < verifyProbeIdx && verifyProbeIdx < verifyIdx && verifyIdx < containerIdx && containerIdx < recordIdx && recordIdx < completeIdx) {
		t.Fatalf("registry apply must prove trust evidence, publish shared transition authority before mutation, persist trust intent before either anchor mutation, refresh and verify, write ownership, then delegate boundary cleanup and transition settlement last: validate=%d desired=%d resolve=%d anchor=%d marker=%d read=%d trust-gate=%d state=%d identity=%d content=%d apply-gate=%d persist=%d install=%d remove=%d refresh=%d probe=%d verify=%d container=%d record=%d complete=%d", validateIdx, desiredIdx, resolveIdx, anchorProbeIdx, markerProbeIdx, markerReadIdx, gateIdx, stateIdx, identityIdx, contentIdx, applyGateIdx, markerWriteIdx, installIdx, removeIdx, refreshIdx, verifyProbeIdx, verifyIdx, containerIdx, recordIdx, completeIdx)
	}
	completeVars, ok := tasks[completeIdx]["vars"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(completeVars["bootwright_infra_completion_boundary_path"]), "bootwright_registry_trust_refresh_marker") {
		t.Fatalf("registry apply completion must carry the exact durable trust boundary into shared settlement: %v", tasks[completeIdx])
	}
	for _, idx := range []int{anchorProbeIdx, markerProbeIdx, markerReadIdx, verifyProbeIdx} {
		if tasks[idx]["failed_when"] != false {
			t.Errorf("registry apply trust probe %q must reach its actionable assertion", tasks[idx]["name"])
		}
	}
	if !strings.Contains(fmt.Sprint(tasks[resolveIdx]), ".ca-trust-refresh-pending") {
		t.Errorf("registry apply does not publish the shared durable trust transition marker: %v", tasks[resolveIdx])
	}
	for _, want := range []string{"stat.isreg", "stat.islnk", "stat.pw_name", "stat.gr_name", "stat.mode", "stat.checksum", "b64decode", "regex_escape", "(install|remove)", "bootwright_mutating_invocation", "No trust anchor or trust store was changed"} {
		if !strings.Contains(fmt.Sprint(tasks[gateIdx]), want) {
			t.Errorf("registry apply foreign/symlink evidence gate missing %q: %v", want, tasks[gateIdx])
		}
	}
	state := fmt.Sprint(tasks[stateIdx])
	for _, want := range []string{"trust_marker_before_apply.stat.exists", "trust_recorded_checksum", "trust_desired_checksum", "pw_name", "gr_name", "mode"} {
		if !strings.Contains(state, want) {
			t.Errorf("registry apply transition state does not make exact reapply idempotent via %q: %v", want, tasks[stateIdx])
		}
	}
	if tasks[refreshIdx]["failed_when"] == false || strings.Contains(fmt.Sprint(tasks[refreshIdx]["when"]), "changed") {
		t.Errorf("registry apply refresh must retry from durable intent even when anchor copy is unchanged: %v", tasks[refreshIdx])
	}
	for _, idx := range []int{installIdx, removeIdx} {
		if tasks[idx]["failed_when"] == false {
			t.Errorf("registry apply anchor mutation %q suppresses failure", tasks[idx]["name"])
		}
	}
	desired := fmt.Sprint(tasks[desiredIdx])
	for _, want := range []string{"trustAnchor", "trustBundleSHA256", "bootwright_registry_trust_desired_checksum"} {
		if !strings.Contains(desired, want) {
			t.Errorf("registry desired transition does not persist exact trust evidence via %q: %v", want, tasks[desiredIdx])
		}
	}
	for _, want := range []string{"rstrip=False", "hash('sha256')"} {
		if !strings.Contains(fmt.Sprint(tasks[resolveIdx]), want) {
			t.Errorf("registry desired trust checksum is not byte-exact via %q: %v", want, tasks[resolveIdx])
		}
	}
}

func findTaskContainingName(t *testing.T, tasks []map[string]any, fragment string) int {
	t.Helper()
	for i, task := range tasks {
		if strings.Contains(fmt.Sprint(task["name"]), fragment) {
			return i
		}
	}
	t.Fatalf("task name containing %q not found", fragment)
	return -1
}

func findIncludeRoleTasksFrom(t *testing.T, tasks []map[string]any, tasksFrom string) int {
	t.Helper()
	for i, task := range tasks {
		include, ok := task["ansible.builtin.include_role"].(map[string]any)
		if ok && fmt.Sprint(include["tasks_from"]) == tasksFrom {
			return i
		}
	}
	t.Fatalf("include_role tasks_from %q not found", tasksFrom)
	return -1
}

func findIncludeTasksFrom(t *testing.T, tasks []map[string]any, tasksFrom string) int {
	t.Helper()
	for i, task := range tasks {
		if fmt.Sprint(task["ansible.builtin.include_tasks"]) == tasksFrom {
			return i
		}
	}
	t.Fatalf("include_tasks %q not found", tasksFrom)
	return -1
}

func findTaskSettingFact(t *testing.T, tasks []map[string]any, fact string) int {
	t.Helper()
	for i, task := range tasks {
		setFact, ok := task["ansible.builtin.set_fact"].(map[string]any)
		if _, found := setFact[fact]; ok && found {
			return i
		}
	}
	t.Fatalf("set_fact task defining %q not found", fact)
	return -1
}

func findTaskLoopingOver(t *testing.T, tasks []map[string]any, expression string) int {
	t.Helper()
	for i, task := range tasks {
		if strings.Contains(fmt.Sprint(task["loop"]), expression) {
			return i
		}
	}
	t.Fatalf("task looping over %q not found", expression)
	return -1
}

func firstInfraTeardownMutation(task map[string]any) (string, bool) {
	for _, module := range []string{"ansible.builtin.file", "ansible.builtin.service", "ansible.posix.firewalld", "containers.podman.podman_container"} {
		if _, ok := task[module]; ok {
			return module, true
		}
	}
	return "", false
}

func firstInfraApplyMutation(task map[string]any) (string, bool) {
	for _, module := range []string{
		"ansible.builtin.package",
		"ansible.builtin.file",
		"ansible.builtin.copy",
		"ansible.builtin.template",
		"ansible.builtin.lineinfile",
		"ansible.builtin.service",
		"ansible.builtin.systemd_service",
		"ansible.posix.sysctl",
		"ansible.posix.firewalld",
		"containers.podman.podman_container",
	} {
		if _, ok := task[module]; ok {
			return module, true
		}
	}
	if _, ok := task["ansible.builtin.command"]; ok && task["changed_when"] != false {
		return "ansible.builtin.command", true
	}
	return "", false
}

func assertConclusiveCommandResult(t *testing.T, task map[string]any, result string) {
	t.Helper()
	assertion, ok := task["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("task %q must be a hard assertion, got %v", task["name"], task)
	}
	conditions := fmt.Sprint(assertion["that"])
	for _, want := range []string{result + ".rc is defined", result + ".rc | int == 0", result + ".stdout is defined", result + ".stderr is defined"} {
		if !strings.Contains(conditions, want) {
			t.Errorf("task %q does not prove %q: %v", task["name"], want, assertion["that"])
		}
	}
	if !strings.Contains(fmt.Sprint(assertion["fail_msg"]), "bootwright_mutating_invocation") {
		t.Errorf("task %q does not name the exact retry: %v", task["name"], assertion["fail_msg"])
	}
}
