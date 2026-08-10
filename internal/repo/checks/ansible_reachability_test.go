package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

const reachabilityRoleTasks = bootwrightCollectionRoleRoot + "/check_external_reachability/tasks"

func TestReachabilityRoleIncludesAreFlat(t *testing.T) {
	if repoFileExists(t, reachabilityRoleTasks+"/per_cluster.yml") {
		t.Fatalf("the per-cluster include level must stay flattened away; BMC targets are computed once in tasks/main.yml")
	}
	tasks := readAnsibleTasks(t, reachabilityRoleTasks+"/main.yml")
	credsIdx := findAnsibleTask(t, tasks, "Load external BMC credentials once per credentials reference")
	bmcIdx := findAnsibleTask(t, tasks, "Validate external Redfish BMCs")
	if !(credsIdx < bmcIdx) {
		t.Fatalf("credentials must be loaded before any BMC probe runs")
	}
	assertIncludeTasksFile(t, tasks[credsIdx], "credentials.yml")
	assertIncludeTasksFile(t, tasks[bmcIdx], "bmc.yml")
	if got := fmt.Sprint(tasks[credsIdx]["loop"]); !strings.Contains(got, "bootwright_validate_bmc_cred_refs") {
		t.Fatalf("credentials load must loop over the distinct credentialsRef set, got loop=%v", tasks[credsIdx]["loop"])
	}
	refs, ok := tasks[findAnsibleTask(t, tasks, "Compute distinct external BMC credentials references")]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the credentialsRef set must be computed by set_fact")
	}
	expr := fmt.Sprint(refs["bootwright_validate_bmc_cred_refs"])
	for _, want := range []string{"boot.redfish.credentialsRef", "unique"} {
		if !strings.Contains(expr, want) {
			t.Fatalf("credentialsRef set expression missing %q: %s", want, expr)
		}
	}
	for _, task := range readAnsibleTasksFromFiles(t, reachabilityRoleTasks+"/main.yml", reachabilityRoleTasks+"/bmc.yml") {
		if _, ok := task["ansible.builtin.include_role"]; ok {
			t.Fatalf("task %q must not re-enter the credential loader per BMC", task["name"])
		}
	}
}

func TestReachabilityBMCProbeNamesItsBMCAndStaysRedacted(t *testing.T) {
	tasks := readAnsibleTasks(t, reachabilityRoleTasks+"/bmc.yml")
	probeIdx := findAnsibleTaskByPrefix(t, tasks, "[bmc {{ bootwright_validate_machine.name }}] Probe")
	probe := tasks[probeIdx]
	if _, ok := probe["loop"]; ok {
		t.Fatalf("the BMC probe must stay one task per include so its failure banner names the BMC; no_log censors a loop item label")
	}
	for _, key := range []string{"ignore_errors", "failed_when"} {
		if _, ok := probe[key]; ok {
			t.Fatalf("the BMC probe must stay fail-closed; %q would swallow an unreachable BMC", key)
		}
	}
	uri, ok := probe["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("the BMC probe is not a uri task: %v", probe)
	}
	if got := fmt.Sprint(uri["status_code"]); got != "200" {
		t.Fatalf("the BMC probe must require status 200, got %v", uri["status_code"])
	}
	if got := fmt.Sprint(uri["return_content"]); got != "true" {
		t.Fatalf("the BMC probe must retain the Systems collection needed to resolve a TPM target, got return_content=%s", got)
	}
	assertRedactsByDefault(t, fmt.Sprint(probe["name"]), probe["no_log"])
	if got := fmt.Sprint(probe["no_log"]); !strings.Contains(got, "bootwright_validate_bmc_credential") {
		t.Fatalf("the BMC probe no_log must stay gated on the resolved credential, got no_log=%v", probe["no_log"])
	}
	selectIdx := findAnsibleTaskByPrefix(t, tasks, "[bmc {{ bootwright_validate_machine.name }}] Select")
	if !(selectIdx < probeIdx) {
		t.Fatalf("the per-BMC credential selection must precede the probe")
	}
	assertRedactsByDefault(t, fmt.Sprint(tasks[selectIdx]["name"]), tasks[selectIdx]["no_log"])
}

func TestReachabilityTPM2ProofTargetsTheExactSystemAndFailsClosed(t *testing.T) {
	tasks := readAnsibleTasks(t, reachabilityRoleTasks+"/bmc.yml")
	collectionIdx := findAnsibleTask(t, tasks, "[bmc {{ bootwright_validate_machine.name }}] Probe Redfish /Systems")
	resolveIdx := findAnsibleTask(t, tasks, "[bmc {{ bootwright_validate_machine.name }}] Resolve TPM target system")
	requireTargetIdx := findAnsibleTask(t, tasks, "[bmc {{ bootwright_validate_machine.name }}] Require a resolvable TPM target system")
	probeIdx := findAnsibleTask(t, tasks, "[bmc {{ bootwright_validate_machine.name }}] Probe ComputerSystem TPM 2.0 inventory")
	evaluateIdx := findAnsibleTask(t, tasks, "[bmc {{ bootwright_validate_machine.name }}] Evaluate TPM 2.0 evidence")
	confirmIdx := findAnsibleTask(t, tasks, "[bmc {{ bootwright_validate_machine.name }}] Confirm required TPM 2.0")
	if !(collectionIdx < resolveIdx && resolveIdx < requireTargetIdx && requireTargetIdx < probeIdx && probeIdx < evaluateIdx && evaluateIdx < confirmIdx) {
		t.Fatalf("external TPM proof must resolve and probe one exact ComputerSystem before confirming evidence")
	}
	resolve := fmt.Sprint(tasks[resolveIdx]["ansible.builtin.set_fact"])
	if !strings.Contains(resolve, "bootwright_redfish_system_id") {
		t.Fatalf("external TPM target must use the shared fail-closed system resolver: %s", resolve)
	}
	requireTarget := fmt.Sprint(tasks[requireTargetIdx]["ansible.builtin.assert"])
	for _, want := range []string{"@odata.id", "machine", "/redfish/v1/Systems/<id>", "bootwright_mutating_invocation", "repeat the operation"} {
		if !strings.Contains(requireTarget, want) {
			t.Fatalf("unresolved TPM target refusal missing %q: %s", want, requireTarget)
		}
	}
	if strings.Contains(requireTarget, "bootwright apply") {
		t.Fatalf("unresolved TPM target refusal must not assemble argv: %s", requireTarget)
	}
	probe := tasks[probeIdx]
	uri, ok := probe["ansible.builtin.uri"].(map[string]any)
	if !ok {
		t.Fatalf("external TPM probe is not an uri task: %v", probe)
	}
	for _, want := range []string{"/redfish/v1/Systems/", "bootwright_validate_bmc_system_id", "bootwright_validate_machine.boot.redfish.baseUrl"} {
		if !strings.Contains(fmt.Sprint(uri["url"]), want) {
			t.Fatalf("external TPM probe URL missing %q: %v", want, uri["url"])
		}
	}
	if got := fmt.Sprint(uri["method"]); got != "GET" {
		t.Fatalf("external TPM proof must remain read-only, got method=%s", got)
	}
	if got := fmt.Sprint(uri["return_content"]); got != "true" {
		t.Fatalf("external TPM proof must read ComputerSystem inventory, got return_content=%s", got)
	}
	if got := fmt.Sprint(uri["status_code"]); got != "[200]" {
		t.Fatalf("external TPM proof must require HTTP 200, got status_code=%s", got)
	}
	if got := fmt.Sprint(uri["timeout"]); !strings.Contains(got, "bootwright_external_validate_bmc_timeout") {
		t.Fatalf("external TPM proof must stay time-bounded, got timeout=%s", got)
	}
	if got := fmt.Sprint(probe["failed_when"]); got != "false" {
		t.Fatalf("external TPM HTTP failure must reach the explicit refusal, got failed_when=%s", got)
	}
	assertRedactsByDefault(t, fmt.Sprint(probe["name"]), probe["no_log"])
	if got := fmt.Sprint(probe["no_log"]); !strings.Contains(got, "bootwright_validate_bmc_credential") {
		t.Fatalf("external TPM probe no_log must be credential-gated, got %v", probe["no_log"])
	}
	for _, idx := range []int{requireTargetIdx, resolveIdx, probeIdx, evaluateIdx, confirmIdx} {
		when := fmt.Sprint(tasks[idx]["when"])
		if !strings.Contains(when, "boot.redfish.requireTPM2") {
			t.Fatalf("external TPM task %q must be gated by the rendered requirement, got when=%s", tasks[idx]["name"], when)
		}
	}
	evaluate := fmt.Sprint(tasks[evaluateIdx]["ansible.builtin.set_fact"])
	if !strings.Contains(evaluate, "bootwright_redfish_tpm2_evidence") {
		t.Fatalf("external TPM response must pass through the sanitized evidence filter: %s", evaluate)
	}
	assertion, ok := tasks[confirmIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("external TPM confirmation is not an assert task: %v", tasks[confirmIdx])
	}
	conditions := fmt.Sprint(assertion["that"])
	for _, want := range []string{"status", "200", "bootwright_validate_bmc_tpm2_evidence.proven"} {
		if !strings.Contains(conditions, want) {
			t.Fatalf("external TPM refusal condition missing %q: %s", want, conditions)
		}
	}
	message := fmt.Sprint(assertion["fail_msg"])
	for _, want := range []string{"bootwright_validate_machine.name", "bootwright_validate_bmc_system_id", "HTTP status", "evidence:", "observed:", "Status.Health=OK", "bootwright_mutating_invocation", "repeat the operation"} {
		if !strings.Contains(message, want) {
			t.Fatalf("external TPM refusal missing %q: %s", want, message)
		}
	}
	if strings.Contains(message, "bootwright apply") {
		t.Fatalf("external TPM refusal must not assemble argv: %s", message)
	}
}

func TestReachabilityCredentialLoadStaysRedacted(t *testing.T) {
	tasks := readAnsibleTasks(t, reachabilityRoleTasks+"/credentials.yml")
	loadIdx := findAnsibleTaskByPrefix(t, tasks, "[bmc credentialsRef {{ bootwright_validate_cred_ref }}] Load")
	assertIncludeRoleName(t, tasks[loadIdx], "bootwright.core.support_credentials")
	loadVars, ok := tasks[loadIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("the credential load must pass loader vars: %v", tasks[loadIdx])
	}
	if got := fmt.Sprint(loadVars["bootwright_creds_label"]); !strings.Contains(got, "bootwright_validate_cred_ref") {
		t.Fatalf("the loader label must name the specific credentialsRef, got %v", loadVars["bootwright_creds_label"])
	}
	recordIdx := findAnsibleTaskByPrefix(t, tasks, "[bmc credentialsRef {{ bootwright_validate_cred_ref }}] Record")
	if !(loadIdx < recordIdx) {
		t.Fatalf("the loaded credential must be recorded after it is loaded")
	}
	record, ok := tasks[recordIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the credential record is not a set_fact task: %v", tasks[recordIdx])
	}
	if got := fmt.Sprint(record["bootwright_validate_bmc_credentials"]); !strings.Contains(got, "combine") {
		t.Fatalf("the credential map must accumulate per credentialsRef, got %v", record["bootwright_validate_bmc_credentials"])
	}
	assertRedactsByDefault(t, fmt.Sprint(tasks[recordIdx]["name"]), tasks[recordIdx]["no_log"])
	for _, task := range tasks {
		if _, ok := task["register"]; ok {
			t.Fatalf("task %q must leave credential registers inside the no_log loader", task["name"])
		}
	}
}
