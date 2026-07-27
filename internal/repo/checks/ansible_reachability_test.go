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
