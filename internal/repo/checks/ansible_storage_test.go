package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestStorageNodeAccessDestroySelectsAReachableIdentity(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_node_access_destroy.yml"
	plays := readAnsiblePlays(t, path)
	if len(plays) != 1 {
		t.Fatalf("%s has %d plays, want 1", path, len(plays))
	}
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	selectIdx := findAnsibleTask(t, tasks, "Select storage node teardown connection")
	endIdx := findAnsibleTask(t, tasks, "End nodes with no reachable storage node identity")
	pingIdx := findAnsibleTask(t, tasks, "Probe node reachability and escalation before revoking node access")
	revokeIdx := findAnsibleTask(t, tasks, "Revoke storage node orchestration access")
	if !(selectIdx < endIdx && endIdx < pingIdx && pingIdx < revokeIdx) {
		t.Fatalf("node-access destroy must select a reachable identity, verify escalation, then revoke (select=%d end=%d ping=%d revoke=%d)", selectIdx, endIdx, pingIdx, revokeIdx)
	}
	include, ok := tasks[selectIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok || include["name"] != "bootwright.core.storage_node_access" || include["tasks_from"] != "select_connection.yml" {
		t.Fatalf("node-access destroy must use the shared teardown connection selector, got %v", tasks[selectIdx])
	}
	if got := fmt.Sprint(tasks[endIdx]["when"]); !strings.Contains(got, "bootwright_node_access_connection_available") {
		t.Fatalf("node-access destroy must end hosts when neither identity answers, got when=%v", tasks[endIdx]["when"])
	}
}

func TestStorageNodeAccessDestroyToleratesMissingOrchestrationAccount(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/revoke_node_access.yml"
	tasks := readAnsibleTasks(t, path)
	probeIdx := findAnsibleTask(t, tasks, "Probe the storage node orchestration account before deauthorizing keys")
	machineKeyIdx := findAnsibleTask(t, tasks, "Deauthorize the machine access key for the storage node orchestration account")
	clusterKeyIdx := findAnsibleTask(t, tasks, "Deauthorize the cephadm cluster key for the storage node orchestration account")
	if !(probeIdx < machineKeyIdx && probeIdx < clusterKeyIdx) {
		t.Fatalf("node-access destroy must probe the orchestration account before removing its keys")
	}
	probe, ok := tasks[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok || fmt.Sprint(probe["argv"]) != "[getent passwd {{ bootwright_node_access.user }}]" {
		t.Fatalf("node-access destroy must probe the declared orchestration account, got %v", tasks[probeIdx])
	}
	if tasks[probeIdx]["changed_when"] != false || tasks[probeIdx]["failed_when"] != false {
		t.Fatalf("missing orchestration account probe must be a read-only tolerated absence, got %v", tasks[probeIdx])
	}
	for _, idx := range []int{machineKeyIdx, clusterKeyIdx} {
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "bootwright_node_access_destroy_account_probe.rc") || !strings.Contains(got, "== 0") {
			t.Fatalf("orchestration key removal must require a present account, got when=%v", tasks[idx]["when"])
		}
	}
}

func TestStorageNodeAccessGrantsSudoBeforeDroppingWheel(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/"
	const wheelTask = "Remove the storage node orchestration account from wheel"

	account := readAnsibleTasks(t, base+"account.yml")
	for _, task := range account {
		if task["name"] == wheelTask {
			t.Fatal("account.yml must not drop wheel membership: when the orchestration account IS the install-window identity, wheel is the only thing making the connection Bootwright is already using privileged, and the named sudoers grant has not been written yet")
		}
	}

	sudoers := readAnsibleTasks(t, base+"sudoers.yml")
	reconcileIdx := findAnsibleTask(t, sudoers, "Reconcile the storage node orchestration sudoers grant")
	wheelIdx := findAnsibleTask(t, sudoers, wheelTask)
	if reconcileIdx >= wheelIdx {
		t.Fatalf("the named sudoers grant must be installed before wheel is dropped (reconcile=%d wheel=%d)", reconcileIdx, wheelIdx)
	}

	main := readAnsibleTasks(t, base+"main.yml")
	block := nestedAnsibleTasks(t, main[0], "block")
	accountIdx := findAnsibleTask(t, block, "Ensure the storage node orchestration account")
	sudoIdx := findAnsibleTask(t, block, "Ensure the storage node orchestration sudo policy")
	verifyIdx := findAnsibleTask(t, block, "Verify the storage node orchestration account")
	revokeIdx := findAnsibleTask(t, block, "Revoke root SSH on the storage node")
	if !(accountIdx < sudoIdx && sudoIdx < verifyIdx && verifyIdx < revokeIdx) {
		t.Fatalf("node access must reconcile the account, then sudo, then verify, then revoke (account=%d sudo=%d verify=%d revoke=%d)", accountIdx, sudoIdx, verifyIdx, revokeIdx)
	}
}

func TestStorageNodeAccessAcceptsTheInstallIdentityAsTheAccount(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/"
	context := readRepoFile(t, base+"context.yml")
	if strings.Contains(context, "bootwright_node_access.user != bootwright_node_access.installUser") {
		t.Fatal("the node-access context must not assert the two identities differ; a Machine whose access.ssh.user IS the orchestration account is a supported shape (ADR 0019) and would fail this assert at apply time while passing bootwright validate")
	}
	if !strings.Contains(context, "bootwright_node_access.installIdentity") {
		t.Fatalf("%scontext.yml must resolve the identities to probe from installIdentity, so a collapsed identity is not reported as a second account to fall back to", base)
	}
	probe := readRepoFile(t, base+"probe.yml")
	if !strings.Contains(probe, "bootwright_node_access_identities") {
		t.Fatalf("%sprobe.yml must name the identities it actually tried when refusing", base)
	}
}

func TestStorageNodeTeardownConnectionSelectorRepairsCanonicalTrust(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/select_connection.yml"
	tasks := readAnsibleTasks(t, path)
	contextIdx := findAnsibleTask(t, tasks, "Resolve storage node access context for teardown")
	repairIdx := findAnsibleTask(t, tasks, "Repair managed storage node connection trust for teardown")
	endpointIdx := findAnsibleTask(t, tasks, "Resolve canonical storage node access endpoints for teardown")
	probeIdx := findAnsibleTask(t, tasks, "Probe storage node access identities for teardown")
	availableIdx := findAnsibleTask(t, tasks, "Record whether a storage node identity is reachable for teardown")
	userIdx := findAnsibleTask(t, tasks, "Select the reachable storage node identity for teardown")
	resetIdx := findAnsibleTask(t, tasks, "Reset the storage node connection after teardown identity selection")
	if !(contextIdx < repairIdx && repairIdx < endpointIdx && endpointIdx < probeIdx && probeIdx < availableIdx && availableIdx < userIdx && userIdx < resetIdx) {
		t.Fatalf("teardown connection selector must repair trust, probe both identities, select a user, then reset the connection")
	}
	endpoints, ok := tasks[endpointIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("canonical endpoint selection must be a set_fact, got %v", tasks[endpointIdx])
	}
	for _, name := range []string{"bootwright_node_access_target_endpoint", "bootwright_node_access_install_endpoint"} {
		if got := fmt.Sprint(endpoints[name]); !strings.Contains(got, "bootwright_node_access.connectionAddress") {
			t.Fatalf("%s must use the canonical connection address, got %v", name, got)
		}
	}
	connection, ok := tasks[userIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("reachable identity selection must be a set_fact, got %v", tasks[userIdx])
	}
	if _, changesHost := connection["ansible_host"]; changesHost {
		t.Fatalf("teardown selection must preserve the rendered canonical ansible_host, got %v", connection)
	}
	user := fmt.Sprint(connection["ansible_user"])
	for _, want := range []string{"bootwright_node_access_ready", "bootwright_node_access.user", "bootwright_node_access.installUser"} {
		if !strings.Contains(user, want) {
			t.Fatalf("teardown selected user missing %q: %s", want, user)
		}
	}

	repairPath := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/repair_connection_alias.yml"
	repair := readRepoFile(t, repairPath)
	for _, want := range []string{"flock", "ssh-keygen -F", "bootwright_node_access.address", "bootwright_node_access.connectionAddress", "knownHostsManaged"} {
		if !strings.Contains(repair, want) {
			t.Fatalf("%s must contain %q", repairPath, want)
		}
	}
	for _, forbidden := range []string{"ssh-keyscan", "StrictHostKeyChecking=accept-new", "ecdsa", "ed25519", "rsa"} {
		if strings.Contains(strings.ToLower(repair), strings.ToLower(forbidden)) {
			t.Fatalf("%s must remain crypto-policy-neutral and must not acquire new trust with %q", repairPath, forbidden)
		}
	}
}

func TestStorageNodeTeardownSelectorOffersTheOrchestrationAccountKey(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/select_connection.yml"
	tasks := readAnsibleTasks(t, path)
	userIdx := findAnsibleTask(t, tasks, "Select the reachable storage node identity for teardown")
	keyIdx := findAnsibleTask(t, tasks, "Offer the storage node orchestration account credential for teardown")
	resetIdx := findAnsibleTask(t, tasks, "Reset the storage node connection after teardown identity selection")
	if !(userIdx < keyIdx && keyIdx < resetIdx) {
		t.Fatalf("the teardown selector must offer the orchestration account credential after selecting that identity and before the connection is reset (user=%d key=%d reset=%d)", userIdx, keyIdx, resetIdx)
	}
	fact, ok := tasks[keyIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the orchestration account credential must be a set_fact, got %v", tasks[keyIdx])
	}
	args := fmt.Sprint(fact["ansible_ssh_common_args"])
	for _, want := range []string{"IdentityFile=", "bootwright_node_access.accountPrivateKeyPath", "ansible_ssh_common_args | default('')"} {
		if !strings.Contains(args, want) {
			t.Fatalf("selecting the orchestration account without offering %q leaves the play connecting as that account with the machine key it does not authorize, and sshd refuses the login: %s", want, args)
		}
	}
	if _, replaced := fact["ansible_ssh_private_key_file"]; replaced {
		t.Fatalf("the account key must be added to the offered identities, not replace the machine key the install identity still needs, got %v", fact)
	}
	when := fmt.Sprint(tasks[keyIdx]["when"])
	for _, want := range []string{"bootwright_node_access_ready", "connectionOverride", "accountPrivateKeyPath not in"} {
		if !strings.Contains(when, want) {
			t.Fatalf("the account credential must be offered only when that identity was selected, and only once per host: when=%s missing %q", when, want)
		}
	}
}

func TestManagedCephCommandsUseCephadmShell(t *testing.T) {
	paths := []string{
		"ansible/collections/ansible_collections/bootwright/core/playbooks/check_storage_health.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/cluster_gate.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/idempotency.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/override_rebuild.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/container_image_base.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/ibm_call_home.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/late_service_specs.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/management_services.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/mon_readiness.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/network_config.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_coverage_report.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_readiness.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/registry_login.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/service_readiness.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/service_specs.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/step.yml",
	}
	for _, path := range paths {
		lines := strings.Split(readRepoFile(t, path), "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) != "argv:" || i+1 >= len(lines) {
				continue
			}
			next := strings.TrimSpace(lines[i+1])
			if next == "- ceph" || next == "- radosgw-admin" {
				t.Fatalf("%s:%d invokes host-installed %s instead of cephadm shell", path, i+2, strings.TrimPrefix(next, "- "))
			}
		}
	}
	execute := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/execute.yml")
	if !strings.Contains(execute, "['cephadm', 'shell', '--'] + bootwright_ceph_op_command") {
		t.Fatalf("rendered Ceph operations must run inside cephadm shell")
	}
	for path, mountedPaths := range map[string][]string{
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/step.yml": {
			"/mnt/{{ bootwright_ceph_step.batch }}",
		},
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/idempotency.yml": {
			"/mnt/stretch-crushmap.bin",
			"/mnt/stretch-crushmap-new.bin",
		},
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/registry_login.yml": {
			"/mnt/registry-login.json",
		},
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/service_specs.yml": {
			"/mnt/ssh_config",
			"/mnt/bootstrap-spec.yaml",
			"/mnt/core-services.yaml",
		},
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/late_service_specs.yml": {
			"/mnt/late-services.yaml",
		},
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/management_services.yml": {
			"/mnt/management-services.yaml",
		},
	} {
		body := readRepoFile(t, path)
		if !strings.Contains(body, "- --mount") || !strings.Contains(body, "bootwright_ceph_remote_work_dir }}:/mnt") {
			t.Fatalf("%s must mount the staged work directory into cephadm shell", path)
		}
		for _, mountedPath := range mountedPaths {
			if !strings.Contains(body, mountedPath) {
				t.Fatalf("%s must address staged input as %s inside cephadm shell", path, mountedPath)
			}
		}
	}
}

func TestStorageOperationBatchesNeverReplaceTheOverridePath(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/"
	support := readAnsibleTasks(t, base+"phases/bootstrap_steps/batch_support.yml")
	probe := support[findAnsibleTask(t, support, "Probe the Ceph container for the batched operation interpreter")]
	if got := fmt.Sprint(probe["ansible.builtin.command"]); !strings.Contains(got, "command -v python3") {
		t.Fatalf("the batch path must preflight the interpreter its guards need, got %v", probe["ansible.builtin.command"])
	}
	if probe["failed_when"] != false || probe["changed_when"] != false {
		t.Fatalf("the interpreter preflight must be a read-only probe, got changed_when=%v failed_when=%v", probe["changed_when"], probe["failed_when"])
	}
	decide := support[findAnsibleTask(t, support, "Decide whether the rendered Ceph operations run one container per phase")]
	facts, ok := decide["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("batch enablement must be a set_fact, got %v", decide)
	}
	enabled := fmt.Sprint(facts["bootwright_ceph_batch_enabled"])
	for _, want := range []string{"!= 'rebuild'", "bootwright_ceph_batch_probe.rc", "bootwright_ceph_batch_files"} {
		if !strings.Contains(enabled, want) {
			t.Fatalf("batching must be withheld from the --mode rebuild rebuild path and from a container without python3 (missing %q), got %v", want, enabled)
		}
	}

	for _, item := range []struct {
		file    string
		perOp   string
		batched string
		group   string
		phases  []string
	}{
		{
			file:    "phases/bootstrap_steps/topology_operations.yml",
			perOp:   "Run rendered Ceph topology and storage operations one container per operation",
			batched: "Run rendered Ceph topology and storage operations in batches",
			group:   "'main'",
			phases:  []string{"topology", "storage"},
		},
		{
			file:    "phases/bootstrap_steps/late_operations.yml",
			perOp:   "Run rendered Ceph late operations one container per operation",
			batched: "Run rendered Ceph late operations in batches",
			group:   "'late'",
			phases:  []string{"object-gateway", "late-topology"},
		},
	} {
		tasks := readAnsibleTasks(t, base+item.file)
		perOp := tasks[findAnsibleTask(t, tasks, item.perOp)]
		if got := fmt.Sprint(perOp["when"]); !strings.Contains(got, "not (bootwright_ceph_batch_enabled") {
			t.Fatalf("%s: the per-operation loop must remain the fallback path, got when=%v", item.file, perOp["when"])
		}
		for _, phase := range item.phases {
			if got := fmt.Sprint(perOp["when"]); !strings.Contains(got, phase) {
				t.Fatalf("%s: the per-operation loop must keep its phase filter %q, got when=%v", item.file, phase, perOp["when"])
			}
		}
		batched := tasks[findAnsibleTask(t, tasks, item.batched)]
		if got := fmt.Sprint(batched["when"]); !strings.Contains(got, "bootwright_ceph_batch_enabled") || strings.Contains(got, "not (bootwright_ceph_batch_enabled") {
			t.Fatalf("%s: the batched path must run only when batching is enabled, got when=%v", item.file, batched["when"])
		}
		loop := fmt.Sprint(batched["loop"])
		if !strings.Contains(loop, "bootwright_ceph_plan") || !strings.Contains(loop, item.group) {
			t.Fatalf("%s: the batched path must walk the rendered plan for its own phase group, got loop=%v", item.file, batched["loop"])
		}
	}

	step := readAnsibleTasks(t, base+"operations/step.yml")
	batch := step[findAnsibleTask(t, step, "Run a batch of rendered Ceph operations")]
	if got := fmt.Sprint(batch["when"]); !strings.Contains(got, "bootwright_ceph_step.batch is defined") {
		t.Fatalf("a plan step must dispatch on whether it names a batch, got when=%v", batch["when"])
	}
	inner := nestedAnsibleTasks(t, batch, "block")
	run := inner[findAnsibleTask(t, inner, "Run the staged batch of rendered Ceph operations")]
	if run["failed_when"] != false {
		t.Fatalf("the batch must be evaluated by the assertion that names the failing operation, got failed_when=%v", run["failed_when"])
	}
	assertion, ok := inner[findAnsibleTask(t, inner, "Require every operation in the batch to have applied")]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the batch must fail closed through an assertion")
	}
	if got := fmt.Sprint(assertion["that"]); !strings.Contains(got, "bootwright_ceph_batch_run.rc") {
		t.Fatalf("the batch assertion must fail on a non-zero batch exit, got that=%v", assertion["that"])
	}
	if got := fmt.Sprint(assertion["fail_msg"]); !strings.Contains(got, "BOOTWRIGHT_CEPH_OP_FAILED") {
		t.Fatalf("the batch assertion must name the failing operation from the marker the batch echoes, got fail_msg=%v", got)
	}
	unbatched := step[findAnsibleTask(t, step, "Run a rendered Ceph operation that must not be batched")]
	if got := fmt.Sprint(unbatched["ansible.builtin.include_tasks"]); !strings.Contains(got, "run.yml") {
		t.Fatalf("an unbatched plan step must go through the per-operation runner, got %v", unbatched)
	}
	if got := fmt.Sprint(unbatched["vars"]); !strings.Contains(got, "bootwright_ceph_operations[bootwright_ceph_step.operation") {
		t.Fatalf("an unbatched plan step must resolve its operation from the rendered list, got vars=%v", unbatched["vars"])
	}

	stage := readAnsibleTasks(t, base+"phases/bootstrap_steps/stage_inputs.yml")
	staged := stage[findAnsibleTask(t, stage, "Stage the batched Ceph operation scripts")]
	copyTask, ok := staged["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("batch scripts must be staged with copy, got %v", staged)
	}
	if got := fmt.Sprint(copyTask["dest"]); !strings.Contains(got, "bootwright_ceph_remote_work_dir") {
		t.Fatalf("batch scripts must be staged into the work directory the cleanup step removes, got dest=%v", copyTask["dest"])
	}
	if got := fmt.Sprint(staged["loop"]); !strings.Contains(got, "bootwright_ceph_batch_files") {
		t.Fatalf("batch scripts must come from the rendered manifest, got loop=%v", staged["loop"])
	}
}

func TestStorageManagementRestartsOnlyTheHostsRunningAManager(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/management_services.yml"
	tasks := readAnsibleTasks(t, path)

	dump := tasks[findAnsibleTask(t, tasks, "Read the Ceph configuration database for the dashboard settings")]
	if got := fmt.Sprint(dump["ansible.builtin.command"]); !strings.Contains(got, "dump") {
		t.Fatalf("the dashboard SSL and port settings must come from one configuration read, got %v", dump["ansible.builtin.command"])
	}
	for _, gone := range []string{"Check current Ceph dashboard SSL setting", "Check current Ceph dashboard HTTP port setting"} {
		if findAnsibleTaskIndex(tasks, gone) >= 0 {
			t.Fatalf("%q must be served by the merged configuration read, not its own container", gone)
		}
	}
	resolve, ok := tasks[findAnsibleTask(t, tasks, "Resolve the live Ceph dashboard SSL and HTTP port settings")]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the merged configuration read must resolve both settings in a set_fact")
	}
	for name, want := range map[string]string{
		"bootwright_ceph_dashboard_ssl_current":  "mgr/dashboard/ssl",
		"bootwright_ceph_dashboard_port_current": "mgr/dashboard/server_port",
	} {
		if got := fmt.Sprint(resolve[name]); !strings.Contains(got, want) {
			t.Fatalf("%s must be read from %q, got %v", name, want, resolve[name])
		}
	}

	hosts, ok := tasks[findAnsibleTask(t, tasks, "Resolve the storage nodes that actually run a Ceph manager daemon")]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the manager restart must resolve its hosts in a set_fact")
	}
	expr := fmt.Sprint(hosts["bootwright_ceph_mgr_restart_hosts"])
	for _, want := range []string{"cephHostname", "inventoryHost", "bootwright_ceph_mgr_daemon_hosts"} {
		if !strings.Contains(expr, want) {
			t.Fatalf("manager hosts must be derived by matching declared nodes against the live manager daemons (missing %q), got %v", want, expr)
		}
	}
	restart := tasks[findAnsibleTask(t, tasks, "Restart the Ceph manager systemd units so every dashboard releases its ports")]
	if got := fmt.Sprint(restart["loop"]); !strings.Contains(got, "bootwright_ceph_mgr_restart_hosts") {
		t.Fatalf("the manager restart must iterate only the hosts that run a manager, got loop=%v", restart["loop"])
	}
	if got := fmt.Sprint(restart["loop"]); strings.Contains(got, "bootwright_selected_storage_cluster.hosts") {
		t.Fatalf("the manager restart must not iterate every storage node, got loop=%v", restart["loop"])
	}

	findAnsibleTask(t, tasks, "Check which endpoint the Ceph dashboard currently serves")
	turnover, ok := tasks[findAnsibleTask(t, tasks, "Assert every Ceph manager daemon restarted onto the new dashboard settings")]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the manager turnover assertion must survive the narrowed restart loop")
	}
	if got := fmt.Sprint(turnover["that"]); !strings.Contains(got, "bootwright_ceph_mgr_restart_complete") {
		t.Fatalf("the manager turnover assertion must still gate on the recorded verdict, got that=%v", turnover["that"])
	}
	if got := fmt.Sprint(turnover["fail_msg"]); !strings.Contains(got, "bootwright_ceph_mgr_restart_hosts") {
		t.Fatalf("a narrowed restart that matched no host must be visible in the failure, got fail_msg=%v", got)
	}
}

func TestStoragePreflightRefusesUnprobeableDevicesFromPerDeviceExitStatus(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/check_storage_preflight/tasks/main.yml"
	tasks := readAnsibleTasks(t, path)

	sweep := tasks[findAnsibleTask(t, tasks, "Probe declared storage OSD devices for signatures")]
	shell, ok := sweep["ansible.builtin.shell"].(map[string]any)
	if !ok {
		t.Fatalf("the device sweep must be a shell loop, got %v", sweep)
	}
	cmd := fmt.Sprint(shell["cmd"])
	for _, want := range []string{
		`for device in "${devices[@]}"`,
		`wipefs --no-act --noheadings "$device"`,
		"rc=$?",
		`"$device" "$rc"`,
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("the device sweep must emit a per-device exit status (missing %q): one exit status for a set of devices turns \"could not probe\" into \"clean\" and a refusal into a wipe:\n%s", want, cmd)
		}
	}
	if strings.Contains(cmd, `wipefs --no-act --noheadings "${devices[@]}"`) {
		t.Fatalf("wipefs must never be handed the whole device set: its single exit status cannot say which device could not be probed:\n%s", cmd)
	}
	if _, looped := sweep["loop"]; looped {
		t.Fatalf("the device sweep must not also loop in ansible, got loop=%v", sweep["loop"])
	}

	classify, ok := tasks[findAnsibleTask(t, tasks, "Classify the declared storage OSD device probes")]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the sweep output must be classified in a set_fact")
	}
	if got := fmt.Sprint(classify["bootwright_preflight_storage_device_probes"]); !strings.Contains(got, "from_json") {
		t.Fatalf("each probed device must be recovered as its own record, got %v", classify["bootwright_preflight_storage_device_probes"])
	}
	unprobeable, ok := tasks[findAnsibleTask(t, tasks, "Resolve declared storage OSD devices that could not be probed")]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("unprobeable devices must be resolved in a set_fact")
	}
	if got := fmt.Sprint(unprobeable["bootwright_preflight_unprobeable_probes"]); !strings.Contains(got, "rejectattr('rc', 'equalto', 0)") {
		t.Fatalf("a device whose probe did not exit zero must land in the unprobeable list, got %v", unprobeable["bootwright_preflight_unprobeable_probes"])
	}
	refuse, ok := tasks[findAnsibleTask(t, tasks, "Refuse declared storage OSD devices that could not be probed")]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("unprobeable devices must trip an assertion")
	}
	if got := fmt.Sprint(refuse["that"]); !strings.Contains(got, "bootwright_preflight_unprobeable_probes") || !strings.Contains(got, "length == 0") {
		t.Fatalf("the refusal must fail closed on any unprobeable device, got that=%v", refuse["that"])
	}

	repos := tasks[findAnsibleTask(t, tasks, "Check storage package repositories")]
	if got := fmt.Sprint(repos["ansible.builtin.command"]); strings.Contains(got, "--cacheonly") {
		t.Fatalf("the repository check exit status IS the reachability gate; --cacheonly downgrades it to \"a repo file exists\", got %v", repos["ansible.builtin.command"])
	}
	if _, ok := repos["failed_when"]; ok {
		t.Fatalf("the repository check must keep failing on its own exit status, got failed_when=%v", repos["failed_when"])
	}
}

func TestStorageOSDReadinessRequiresInOSDPerDynamicHost(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_readiness.yml"
	tasks := readAnsibleTasks(t, path)
	resolveIdx := findAnsibleTask(t, tasks, "Resolve OSD readiness expectation")
	globalWaitIdx := findAnsibleTask(t, tasks, "Wait for declared Ceph OSDs to be created and in")
	evaluateIdx := findAnsibleTask(t, tasks, "Evaluate whether declared Ceph OSDs became ready")
	perHostIdx := findAnsibleTask(t, tasks, "Wait for an in OSD on every dynamic-selection host")
	perHostEvaluateIdx := findAnsibleTask(t, tasks, "Evaluate whether every dynamic-selection host has an in OSD")
	deviceDiagIdx := findAnsibleTask(t, tasks, "Collect Ceph device inventory when OSDs did not become ready")
	serviceDiagIdx := findAnsibleTask(t, tasks, "Collect declared OSD service status when OSDs did not become ready")
	globalAssertIdx := findAnsibleTask(t, tasks, "Assert declared Ceph OSDs were created")
	dynamicAssertIdx := findAnsibleTask(t, tasks, "Assert an in OSD exists on every dynamic-selection host")
	healthIdx := findAnsibleTask(t, tasks, "Capture observed Ceph health for the storage result")
	if !(resolveIdx < globalWaitIdx &&
		globalWaitIdx < evaluateIdx &&
		evaluateIdx < perHostIdx &&
		perHostIdx < perHostEvaluateIdx &&
		perHostEvaluateIdx < deviceDiagIdx &&
		deviceDiagIdx < serviceDiagIdx &&
		serviceDiagIdx < globalAssertIdx &&
		globalAssertIdx < dynamicAssertIdx &&
		dynamicAssertIdx < healthIdx) {
		t.Fatalf("OSD readiness must evaluate the global gate, poll the CRUSH tree once for every dynamic host, diagnose, then assert the global gate and each dynamic host before capturing health")
	}

	if findAnsibleTaskIndex(tasks, "Assert every dynamic-selection host was probed") >= 0 {
		t.Fatalf("dynamic-host OSD readiness must probe the CRUSH tree once, not loop a per-host coverage assertion")
	}

	resolve, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(resolve["bootwright_ceph_osd_dynamic_hosts"]), "dynamicHosts") {
		t.Fatalf("OSD readiness must resolve rendered dynamic hostnames, got %v", tasks[resolveIdx])
	}
	crushNames := fmt.Sprint(resolve["bootwright_ceph_osd_dynamic_crush_names"])
	if !strings.Contains(crushNames, "crushNames") || !strings.Contains(crushNames, "flatten") {
		t.Fatalf("OSD readiness must resolve CRUSH bucket names separately from the orchestrator hostnames: ceph shortens the hostname for the CRUSH map, got %v", crushNames)
	}

	globalUntil := fmt.Sprint(tasks[globalWaitIdx]["until"])
	for _, want := range []string{"num_in_osds", "bootwright_ceph_osd_readiness_mode == 'exact'", "bootwright_ceph_osd_expected_count", "bootwright_ceph_osd_stat.attempts", "bootwright_ceph_osd_readiness_retries"} {
		if !strings.Contains(globalUntil, want) {
			t.Fatalf("global OSD readiness must preserve exact static-path behavior %q, got %v", want, globalUntil)
		}
	}
	if strings.Count(globalUntil, "bootwright_ceph_osd_expected_count") < 2 {
		t.Fatalf("global OSD readiness must enforce expectedCount for exact and dynamic selections, got %v", globalUntil)
	}
	readyFacts, ok := tasks[evaluateIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("global OSD readiness must evaluate the exhausted poll before diagnostics, got %v", tasks[evaluateIdx])
	}
	ready := fmt.Sprint(readyFacts["bootwright_ceph_osd_ready"])
	for _, want := range []string{"bootwright_ceph_osd_readiness_mode == 'atLeastOne'", "bootwright_ceph_osd_expected_count"} {
		if !strings.Contains(ready, want) {
			t.Fatalf("global OSD readiness evaluation missing %q, got %v", want, ready)
		}
	}
	if strings.Count(ready, "bootwright_ceph_osd_expected_count") < 2 {
		t.Fatalf("global OSD readiness evaluation must preserve expectedCount for exact and dynamic selections, got %v", ready)
	}
	globalAssert, ok := tasks[globalAssertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(globalAssert["that"]), "bootwright_ceph_osd_ready") {
		t.Fatalf("global OSD readiness must fail through the evaluated readiness fact after diagnostics, got %v", tasks[globalAssertIdx])
	}

	perHost := tasks[perHostIdx]
	command, ok := perHost["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("dynamic-host OSD readiness must use a command probe, got %v", perHost)
	}
	argv := fmt.Sprint(command["argv"])
	for _, want := range []string{"cephadm", "shell", "ceph", "osd", "tree", "--format", "json"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("dynamic-host OSD readiness argv missing %q: %v", want, argv)
		}
	}
	if _, looped := perHost["loop"]; looped {
		t.Fatalf("dynamic-host OSD readiness must poll the cluster-wide CRUSH tree once, not loop per host, got loop=%v", perHost["loop"])
	}
	until := fmt.Sprint(perHost["until"])
	for _, want := range []string{"type', 'equalto', 'osd", "reweight', 'gt', 0", "type', 'equalto', 'host", "name', 'in', bootwright_ceph_osd_dynamic_crush_names", "map('intersect'", "map('length')", "select('gt', 0)", "bootwright_ceph_osd_dynamic_hosts | length", "bootwright_ceph_osd_host_tree.attempts", "bootwright_ceph_osd_readiness_retries"} {
		if !strings.Contains(until, want) {
			t.Fatalf("collapsed dynamic-host OSD readiness condition missing %q: %v", want, until)
		}
	}
	if strings.Contains(until, "name', 'equalto', item") {
		t.Fatalf("dynamic-host OSD readiness must not re-poll per host, got %v", until)
	}
	if got := fmt.Sprint(perHost["when"]); !strings.Contains(got, "bootwright_ceph_osd_dynamic_hosts") || !strings.Contains(got, "bootwright_ceph_osd_ready") {
		t.Fatalf("dynamic-host OSD readiness must skip empty host sets and a failed global gate, got when=%v", perHost["when"])
	}
	if perHost["changed_when"] != false || perHost["failed_when"] != false {
		t.Fatalf("dynamic-host OSD readiness must be a read-only retry probe, got changed_when=%v failed_when=%v", perHost["changed_when"], perHost["failed_when"])
	}

	perHostEval, ok := tasks[perHostEvaluateIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("dynamic-host OSD readiness must evaluate the collapsed poll into a fact before diagnostics, got %v", tasks[perHostEvaluateIdx])
	}
	dynamicReady := fmt.Sprint(perHostEval["bootwright_ceph_osd_dynamic_ready"])
	for _, want := range []string{"bootwright_ceph_osd_host_tree.rc", "name', 'in', bootwright_ceph_osd_dynamic_crush_names", "map('intersect'", "bootwright_ceph_osd_dynamic_hosts | length"} {
		if !strings.Contains(dynamicReady, want) {
			t.Fatalf("dynamic-host readiness evaluation missing %q, got %v", want, dynamicReady)
		}
	}

	if got := fmt.Sprint(tasks[deviceDiagIdx]["when"]); !strings.Contains(got, "bootwright_ceph_osd_dynamic_ready") {
		t.Fatalf("readiness diagnostics must also run when a dynamic host has no in OSD, got when=%v", tasks[deviceDiagIdx]["when"])
	}

	dynamicAssert, ok := tasks[dynamicAssertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("dynamic-host OSD readiness must end with an actionable per-host assertion, got %v", tasks[dynamicAssertIdx])
	}
	dynamicThat := fmt.Sprint(dynamicAssert["that"])
	for _, want := range []string{"bootwright_ceph_osd_host_tree.rc", "bootwright_ceph_osd_host_tree.stdout", "bootwright_ceph_osd_dynamic_host.crushNames", "type', 'equalto', 'osd", "type', 'equalto', 'host", "intersect"} {
		if !strings.Contains(dynamicThat, want) {
			t.Fatalf("dynamic-host OSD assertion missing %q, got %v", want, dynamicThat)
		}
	}
	if got := fmt.Sprint(tasks[dynamicAssertIdx]["loop"]); !strings.Contains(got, "bootwright_ceph_osd_dynamic_hosts") {
		t.Fatalf("dynamic-host OSD assertion must evaluate the single snapshot for every rendered host, got loop=%v", got)
	}
	loopControl, ok := tasks[dynamicAssertIdx]["loop_control"].(map[string]any)
	if !ok || loopControl["loop_var"] != "bootwright_ceph_osd_dynamic_host" {
		t.Fatalf("dynamic-host OSD assertion must use a collision-safe loop variable, got %v", tasks[dynamicAssertIdx]["loop_control"])
	}
	if got := fmt.Sprint(dynamicAssert["fail_msg"]); !strings.Contains(got, "ceph orch device ls --wide") || !strings.Contains(got, "CRUSH") || !strings.Contains(got, "stray") || !strings.Contains(got, "ceph orch ps") {
		t.Fatalf("dynamic-host OSD assertion must distinguish stray/down daemons from rejected devices and provide device, daemon, and CRUSH diagnostics, got fail_msg=%v", got)
	}
	dynamicVars, ok := tasks[dynamicAssertIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("dynamic-host OSD assertion must derive its stray/down counts from a vars block, got %v", tasks[dynamicAssertIdx]["vars"])
	}
	if got := fmt.Sprint(dynamicVars["bootwright_ceph_osd_tree_doc"]); !strings.Contains(got, "from_json") || !strings.Contains(got, "default('{}', true)") {
		t.Fatalf("dynamic-host OSD assertion fail_msg parses the CRUSH tree eagerly, so it must guard from_json against an empty (rc!=0) stdout with default('{}', true), got %v", got)
	}
}

func TestStorageMonReadinessGatesEveryDeclaredMonBeforeTopologyOperations(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/"
	phase := readRepoFile(t, base+"phases/bootstrap.yml")
	monInclude := strings.Index(phase, "bootstrap_steps/mon_readiness.yml")
	topology := strings.Index(phase, "bootstrap_steps/topology_operations.yml")
	if monInclude < 0 || topology < 0 || monInclude > topology {
		t.Fatalf("the monmap gate must run before the topology operations that address mons by name, got mon=%d topology=%d", monInclude, topology)
	}
	for _, gone := range []string{"bootwright_ceph_stretch_tiebreaker", "Wait for the stretch tiebreaker mon to join the monmap"} {
		if strings.Contains(phase, gone) {
			t.Fatalf("the tiebreaker-only monmap gate must be replaced by the every-declared-mon gate, still found %q", gone)
		}
	}

	tasks := readAnsibleTasks(t, base+"phases/bootstrap_steps/mon_readiness.yml")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve the Ceph mons the topology operations address")
	waitIdx := findAnsibleTask(t, tasks, "Wait for every declared Ceph mon to join the monmap")
	missingIdx := findAnsibleTask(t, tasks, "Resolve the declared Ceph mons still absent from the monmap")
	assertIdx := findAnsibleTask(t, tasks, "Assert every declared Ceph mon joined the monmap")
	if !(resolveIdx < waitIdx && waitIdx < missingIdx && missingIdx < assertIdx) {
		t.Fatalf("the monmap gate must resolve the declared mons, poll, evaluate what is missing, then assert")
	}
	for _, diagnostic := range []string{
		"Collect the declared Ceph mon service when mons did not join the monmap",
		"Collect Ceph mon daemon placement when mons did not join the monmap",
		"Collect Ceph orchestrator host status when mons did not join the monmap",
		"Collect the configured Ceph public network when mons did not join the monmap",
		"Collect recent cephadm events when mons did not join the monmap",
	} {
		idx := findAnsibleTask(t, tasks, diagnostic)
		if idx < missingIdx || idx > assertIdx {
			t.Fatalf("%q must run between the readiness verdict and the assertion, got %d", diagnostic, idx)
		}
	}

	resolve, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the monmap gate must resolve the declared mons in a set_fact, got %v", tasks[resolveIdx])
	}
	daemons := fmt.Sprint(resolve["bootwright_ceph_declared_mon_daemons"])
	for _, want := range []string{"monReadiness", "mons", "daemon"} {
		if !strings.Contains(daemons, want) {
			t.Fatalf("the monmap gate must match the rendered mon daemon names (ceph shortens the hostname for mon names), missing %q, got %v", want, daemons)
		}
	}

	wait := tasks[waitIdx]
	command, ok := wait["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("the monmap gate must poll with a command probe, got %v", wait)
	}
	argv := fmt.Sprint(command["argv"])
	for _, want := range []string{"cephadm", "shell", "ceph", "mon", "dump", "--format", "json"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("the monmap gate argv missing %q: %v", want, argv)
		}
	}
	until := fmt.Sprint(wait["until"])
	for _, want := range []string{"bootwright_ceph_declared_mon_daemons", "difference", "bootwright_ceph_mon_dump.attempts", "bootwright_ceph_mon_readiness_retries"} {
		if !strings.Contains(until, want) {
			t.Fatalf("the monmap poll must compare every declared mon and escape on its own attempt count, missing %q, got %v", want, until)
		}
	}

	assertion, ok := tasks[assertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the monmap gate must fail through an assert, got %v", tasks[assertIdx])
	}
	if got := fmt.Sprint(assertion["that"]); !strings.Contains(got, "bootwright_ceph_mons_ready") {
		t.Fatalf("the monmap assertion must gate on the evaluated verdict, got that=%v", got)
	}
	failMsg := fmt.Sprint(assertion["fail_msg"])
	for _, want := range []string{
		"bootwright_ceph_missing_mon_daemons",
		"set_location",
		"public_network",
		"3300",
		"ceph orch ls --service_type mon",
		"ceph log last 100 cephadm",
	} {
		if !strings.Contains(failMsg, want) {
			t.Fatalf("the monmap failure must name what is absent, the operation it breaks and the causes that keep a mon out of the monmap, missing %q, got %v", want, failMsg)
		}
	}
}

func TestStorageOperationsUseExplicitIdempotencyContract(t *testing.T) {
	body := strings.Join([]string{
		readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/classify.yml"),
		readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/idempotency.yml"),
	}, "\n")
	for _, want := range []string{
		"bootwright_ceph_op_idempotency_kind",
		"bootwright_ceph_op_idempotency_name",
		"bootwright_ceph_op_idempotency_kind == 'ceph-pool'",
		"bootwright_ceph_op_idempotency_kind == 'rgw-user'",
		"bootwright_ceph_op_idempotency_kind == 'stretch-internal-pools'",
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

func TestStorageStretchInternalPoolsReconcileOffDeclaredPools(t *testing.T) {
	body := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/idempotency.yml")
	reconcile := body[findAnsibleTask(t, body, "Place Ceph internal pools on the stretch placement")]
	if got := fmt.Sprint(reconcile["when"]); !strings.Contains(got, "stretch-internal-pools") {
		t.Fatalf("internal-pool reconcile must be gated on its idempotency kind, got when=%v", reconcile["when"])
	}
	tasks := nestedAnsibleTasks(t, reconcile, "block")

	selectIdx := findAnsibleTask(t, tasks, "Select the Ceph internal pools nobody declares")
	selectFact, ok := tasks[selectIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("internal-pool selection must be a set_fact, got %v", tasks[selectIdx])
	}
	selection := fmt.Sprint(selectFact["bootwright_ceph_stretch_internal_pools"])
	if !strings.Contains(selection, "structural.poolPattern") {
		t.Fatalf("internal-pool selection must use the rendered pattern rather than a duplicated name list, got %v", selection)
	}
	if !strings.Contains(selection, "selectattr('type', 'equalto', 1)") {
		t.Fatalf("internal-pool selection must skip erasure-coded pools, whose size is set by the profile, got %v", selection)
	}
	if !strings.Contains(selection, "default('[]', true)") {
		t.Fatalf("internal-pool selection parses pool detail eagerly, so from_json must be guarded against an empty (rc!=0) stdout, got %v", selection)
	}
	for name, setting := range map[string]string{
		"Set the stretch CRUSH rule on Ceph internal pools":           "crush_rule",
		"Set the stretch replica size on Ceph internal pools":         "size",
		"Set the stretch minimum replica size on Ceph internal pools": "min_size",
	} {
		task := tasks[findAnsibleTask(t, tasks, name)]
		if got := fmt.Sprint(task["ansible.builtin.command"]); !strings.Contains(got, setting) {
			t.Fatalf("%q must set %s, got %v", name, setting, got)
		}
		if got := fmt.Sprint(task["loop"]); !strings.Contains(got, "bootwright_ceph_stretch_internal_pools") {
			t.Fatalf("%q must iterate the selected internal pools, got loop=%v", name, task["loop"])
		}
	}
	handled := tasks[findAnsibleTask(t, tasks, "Mark the stretch internal-pool operation as handled")]
	if got := fmt.Sprint(handled["ansible.builtin.set_fact"]); !strings.Contains(got, "bootwright_ceph_op_skip") {
		t.Fatalf("the argv-less internal-pool operation must mark itself handled so execute.yml does not run an empty command, got %v", got)
	}
}

func TestStorageCephadmOverrideRebuildsStructurallyDriftedSubObjects(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/override_rebuild.yml")

	decide := tasks[findAnsibleTask(t, tasks, "Decide Ceph pool structural override rebuild")]
	decideFact, ok := decide["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("pool rebuild decision must be a set_fact, got %v", decide)
	}
	expr := fmt.Sprint(decideFact["bootwright_ceph_op_pool_recreate"])
	for _, want := range []string{"bootwright_ceph_op_pool_live", "bootwright_ceph_op.structural.type"} {
		if !strings.Contains(expr, want) {
			t.Fatalf("pool rebuild decision must compare live pool to desired structural %q, got %s", want, expr)
		}
	}
	if got := fmt.Sprint(decide["when"]); !strings.Contains(got, "ceph-pool") || !strings.Contains(got, "'rebuild'") {
		t.Fatalf("pool rebuild decision must be gated on the ceph-pool op under override, got %v", decide["when"])
	}

	ackFact := tasks[findAnsibleTask(t, tasks, "Decide whether the controller acknowledged destroying this Ceph pool")]
	ackFactMap, ok := ackFact["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("pool destroy acknowledgement must be a set_fact, got %v", ackFact)
	}
	ackExpr := fmt.Sprint(ackFactMap["bootwright_ceph_op_pool_rebuild_acked"])
	for _, want := range []string{"bootwright_ceph_subobject_rebuild_authorized", "bootwright_ceph_op_idempotency_name | default('') | length > 0"} {
		if !strings.Contains(ackExpr, want) {
			t.Fatalf("pool destroy acknowledgement must require %q, got %s", want, ackExpr)
		}
	}

	refuseIdx := findAnsibleTask(t, tasks, "Refuse unacknowledged Ceph pool destroy for override rebuild")
	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("unacknowledged pool destroy refusal must be an assert, got %v", tasks[refuseIdx])
	}
	if got := fmt.Sprint(refuse["that"]); !strings.Contains(got, "bootwright_ceph_op_pool_rebuild_acked") || !strings.Contains(got, "bootwright_ceph_op_pool_recreate") {
		t.Fatalf("pool destroy refusal must fail closed on recreate-without-acknowledgement, got %v", refuse["that"])
	}
	if got := fmt.Sprint(refuse["fail_msg"]); !strings.Contains(got, "--authorize data-loss") {
		t.Fatalf("pool destroy refusal must name the --authorize data-loss remedy, got %v", refuse["fail_msg"])
	}

	rebuildIdx := findAnsibleTask(t, tasks, "Rebuild structurally drifted Ceph pool for override")
	if !(refuseIdx < rebuildIdx) {
		t.Fatalf("must refuse an unacknowledged destroy before the pool rebuild (refuse=%d rebuild=%d)", refuseIdx, rebuildIdx)
	}
	rebuild := tasks[rebuildIdx]
	if got := fmt.Sprint(rebuild["when"]); !strings.Contains(got, "bootwright_ceph_op_pool_recreate") || !strings.Contains(got, "bootwright_ceph_op_pool_rebuild_acked") {
		t.Fatalf("pool rebuild must be gated on the structural-mismatch decision and the destroy acknowledgement, got %v", rebuild["when"])
	}
	block := nestedAnsibleTasks(t, rebuild, "block")
	allowIdx := findAnsibleTask(t, block, "Allow Ceph pool deletion for override rebuild")
	rmIdx := findAnsibleTask(t, block, "Destroy structurally drifted Ceph pool for override rebuild")
	if !(allowIdx < rmIdx) {
		t.Fatalf("must enable pool deletion before removing the pool (allow=%d rm=%d)", allowIdx, rmIdx)
	}
	rm, ok := block[rmIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("pool rm must be a command, got %v", block[rmIdx])
	}
	rmArgv := fmt.Sprint(rm["argv"])
	if !strings.Contains(rmArgv, "rm") || !strings.Contains(rmArgv, "--yes-i-really-really-mean-it") {
		t.Fatalf("pool rebuild must rm the pool with --yes-i-really-really-mean-it, got %v", rm["argv"])
	}
	always := nestedAnsibleTasks(t, rebuild, "always")
	disable := always[findAnsibleTask(t, always, "Re-disable Ceph pool deletion after override rebuild")]
	if got := fmt.Sprint(disable["ansible.builtin.command"]); !strings.Contains(got, "mon_allow_pool_delete") {
		t.Fatalf("always block must re-disable mon_allow_pool_delete, got %v", disable)
	}

	assertTask := tasks[findAnsibleTask(t, tasks, "Fail closed when override Ceph pool deletion failed")]
	if _, ok := assertTask["ansible.builtin.assert"].(map[string]any); !ok {
		t.Fatalf("pool deletion failure must be an assert, got %v", assertTask)
	}

	fsRefuseIdx := findAnsibleTask(t, tasks, "Refuse unacknowledged CephFS destroy for override rebuild")
	fsRefuse, ok := tasks[fsRefuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("unacknowledged CephFS destroy refusal must be an assert, got %v", tasks[fsRefuseIdx])
	}
	if got := fmt.Sprint(fsRefuse["that"]); !strings.Contains(got, "bootwright_ceph_op_fs_rebuild_acked") || !strings.Contains(got, "bootwright_ceph_op_fs_recreate") {
		t.Fatalf("CephFS destroy refusal must fail closed on recreate-without-acknowledgement, got %v", fsRefuse["that"])
	}
	if got := fmt.Sprint(fsRefuse["fail_msg"]); !strings.Contains(got, "--authorize data-loss") {
		t.Fatalf("CephFS destroy refusal must name the --authorize data-loss remedy, got %v", fsRefuse["fail_msg"])
	}
	fsRebuildIdx := findAnsibleTask(t, tasks, "Rebuild structurally drifted CephFS for override")
	if !(fsRefuseIdx < fsRebuildIdx) {
		t.Fatalf("must refuse an unacknowledged destroy before the CephFS rebuild (refuse=%d rebuild=%d)", fsRefuseIdx, fsRebuildIdx)
	}
	fsRebuild := tasks[fsRebuildIdx]
	if got := fmt.Sprint(fsRebuild["when"]); !strings.Contains(got, "bootwright_ceph_op_fs_rebuild_acked") {
		t.Fatalf("CephFS rebuild must be gated on the destroy acknowledgement, got %v", fsRebuild["when"])
	}
	fsAckFact := tasks[findAnsibleTask(t, tasks, "Decide whether the controller acknowledged destroying this CephFS")]
	fsAckMap, ok := fsAckFact["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("CephFS destroy acknowledgement must be a set_fact, got %v", fsAckFact)
	}
	if got := fmt.Sprint(fsAckMap["bootwright_ceph_op_fs_rebuild_acked"]); !strings.Contains(got, "bootwright_ceph_subobject_rebuild_authorized") {
		t.Fatalf("CephFS destroy acknowledgement must consult the authorized sub-object list, got %v", got)
	}
	fsBlock := nestedAnsibleTasks(t, fsRebuild, "block")
	failIdx := findAnsibleTask(t, fsBlock, "Fail drifted CephFS before override rebuild")
	fsRmIdx := findAnsibleTask(t, fsBlock, "Destroy structurally drifted CephFS for override rebuild")
	if !(failIdx < fsRmIdx) {
		t.Fatalf("must fail the CephFS before removing it (fail=%d rm=%d)", failIdx, fsRmIdx)
	}
	fsRm, ok := fsBlock[fsRmIdx]["ansible.builtin.command"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(fsRm["argv"]), "--yes-i-really-really-mean-it") {
		t.Fatalf("CephFS rebuild must rm with --yes-i-really-really-mean-it, got %v", fsBlock[fsRmIdx])
	}

	fsDecide := tasks[findAnsibleTask(t, tasks, "Decide CephFS structural override rebuild")]
	fsDecideFact, ok := fsDecide["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("CephFS rebuild decision must be a set_fact, got %v", fsDecide)
	}
	fsExpr := fmt.Sprint(fsDecideFact["bootwright_ceph_op_fs_recreate"])
	for _, want := range []string{"structural.metadataPool", "structural.defaultDataPool", "bootwright_ceph_op_fs_default_pool_live"} {
		if !strings.Contains(fsExpr, want) {
			t.Fatalf("CephFS rebuild decision must compare live to desired %q, got %s", want, fsExpr)
		}
	}
}

func TestStorageCephadmOverrideRebuildsDriftedECProfile(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/override_rebuild.yml")

	decide := tasks[findAnsibleTask(t, tasks, "Decide Ceph erasure-code profile structural override rebuild")]
	decideFact, ok := decide["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("ec-profile rebuild decision must be a set_fact, got %v", decide)
	}
	expr := fmt.Sprint(decideFact["bootwright_ceph_op_ec_recreate"])
	for _, want := range []string{"bootwright_ceph_op_ec_live", "bootwright_ceph_op.structural.fields"} {
		if !strings.Contains(expr, want) {
			t.Fatalf("ec-profile rebuild decision must compare live profile to desired structural %q, got %s", want, expr)
		}
	}
	if got := fmt.Sprint(decide["when"]); !strings.Contains(got, "ec-profile") || !strings.Contains(got, "'rebuild'") {
		t.Fatalf("ec-profile rebuild decision must be gated on the ec-profile op under override, got %v", decide["when"])
	}

	ackFact := tasks[findAnsibleTask(t, tasks, "Decide whether the controller acknowledged destroying the erasure-coded pool")]
	ackMap, ok := ackFact["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("ec-profile destroy acknowledgement must be a set_fact, got %v", ackFact)
	}
	ackExpr := fmt.Sprint(ackMap["bootwright_ceph_op_ec_rebuild_acked"])
	for _, want := range []string{"bootwright_ceph_subobject_rebuild_authorized", "bootwright_ceph_op.structural.pool | default('') | length > 0"} {
		if !strings.Contains(ackExpr, want) {
			t.Fatalf("ec-profile destroy acknowledgement must require %q, got %s", want, ackExpr)
		}
	}
	refuseIdx := findAnsibleTask(t, tasks, "Refuse unacknowledged erasure-coded pool destroy for override rebuild")
	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("unacknowledged ec-profile destroy refusal must be an assert, got %v", tasks[refuseIdx])
	}
	if got := fmt.Sprint(refuse["that"]); !strings.Contains(got, "bootwright_ceph_op_ec_rebuild_acked") || !strings.Contains(got, "bootwright_ceph_op_ec_recreate") {
		t.Fatalf("ec-profile destroy refusal must fail closed on recreate-without-acknowledgement, got %v", refuse["that"])
	}
	if got := fmt.Sprint(refuse["fail_msg"]); !strings.Contains(got, "--authorize data-loss") {
		t.Fatalf("ec-profile destroy refusal must name the --authorize data-loss remedy, got %v", refuse["fail_msg"])
	}

	rebuildIdx := findAnsibleTask(t, tasks, "Rebuild structurally drifted erasure-coded pool for override")
	if !(refuseIdx < rebuildIdx) {
		t.Fatalf("must refuse an unacknowledged destroy before the ec-profile rebuild (refuse=%d rebuild=%d)", refuseIdx, rebuildIdx)
	}
	rebuild := tasks[rebuildIdx]
	if got := fmt.Sprint(rebuild["when"]); !strings.Contains(got, "bootwright_ceph_op_ec_recreate") || !strings.Contains(got, "bootwright_ceph_op_ec_rebuild_acked") {
		t.Fatalf("ec-profile rebuild must be gated on the structural-mismatch decision and the destroy acknowledgement, got %v", rebuild["when"])
	}
	block := nestedAnsibleTasks(t, rebuild, "block")
	allowIdx := findAnsibleTask(t, block, "Allow Ceph pool deletion for erasure-code profile override rebuild")
	rmIdx := findAnsibleTask(t, block, "Destroy pool using structurally drifted erasure-code profile")
	if !(allowIdx < rmIdx) {
		t.Fatalf("must enable pool deletion before removing the dependent pool (allow=%d rm=%d)", allowIdx, rmIdx)
	}
	rm, ok := block[rmIdx]["ansible.builtin.command"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(rm["argv"]), "--yes-i-really-really-mean-it") {
		t.Fatalf("ec-profile rebuild must rm the dependent pool with --yes-i-really-really-mean-it, got %v", block[rmIdx])
	}
	always := nestedAnsibleTasks(t, rebuild, "always")
	disable := always[findAnsibleTask(t, always, "Re-disable Ceph pool deletion after erasure-code profile override rebuild")]
	if got := fmt.Sprint(disable["ansible.builtin.command"]); !strings.Contains(got, "mon_allow_pool_delete") {
		t.Fatalf("always block must re-disable mon_allow_pool_delete, got %v", disable)
	}

	assertTask := tasks[findAnsibleTask(t, tasks, "Fail closed when erasure-code profile override pool deletion failed")]
	if _, ok := assertTask["ansible.builtin.assert"].(map[string]any); !ok {
		t.Fatalf("ec-profile pool deletion failure must be an assert, got %v", assertTask)
	}

	profileRm := tasks[findAnsibleTask(t, tasks, "Delete structurally drifted erasure-code profile for override rebuild")]
	if got := fmt.Sprint(profileRm["when"]); !strings.Contains(got, "bootwright_ceph_op_ec_recreate") || !strings.Contains(got, "bootwright_ceph_op_ec_rebuild_acked") {
		t.Fatalf("ec-profile delete must be gated on the structural-mismatch decision and the destroy acknowledgement, got %v", profileRm["when"])
	}
}

func TestPreflightVerifiesStorageNodeHostnames(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/check_storage_preflight/tasks/main.yml"
	tasks := readAnsibleTasks(t, path)

	resolve := tasks[findAnsibleTask(t, tasks, "Resolve storage nodes on this host")]
	facts, ok := resolve["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("storage node selection must be a set_fact, got %v", resolve)
	}
	expr := fmt.Sprint(facts["bootwright_host_storage_nodes"])
	if !strings.Contains(expr, "map(attribute='hosts')") || strings.Contains(expr, "map(attribute='nodes')") {
		t.Fatalf("storage node selection must map the rendered hosts attribute, got %s", expr)
	}

	for _, task := range tasks {
		body, ok := task["ansible.builtin.assert"].(map[string]any)
		if !ok {
			continue
		}
		if strings.Contains(fmt.Sprint(body["that"]), "cephHostname") {
			t.Fatalf("preflight must not refuse a hostname apply rewrites; report it instead, got %v", task["name"])
		}
	}

	task := tasks[findAnsibleTask(t, tasks, "Report a storage node OS hostname apply will rewrite")]
	body, ok := task["ansible.builtin.debug"].(map[string]any)
	if !ok {
		t.Fatalf("storage hostname reporting must be a read-only debug, got %v", task)
	}
	msg := fmt.Sprint(body["msg"])
	for _, want := range []string{"{{ ansible_facts['nodename'] }}", "{{ item.cephHostname }}"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("hostname report must name both the real and the declared hostname, got %s", msg)
		}
	}
	if got := fmt.Sprint(task["loop"]); !strings.Contains(got, "bootwright_host_storage_nodes") {
		t.Fatalf("hostname report must loop this host's declared storage nodes, got %v", task["loop"])
	}
	when := fmt.Sprint(task["when"])
	if !strings.Contains(when, "bootwright_host_storage_nodes | length > 0") {
		t.Fatalf("hostname report must be gated on storage nodes, got when=%v", task["when"])
	}
	if !strings.Contains(when, "ansible_facts['nodename'] != item.cephHostname") {
		t.Fatalf("hostname report must stay silent when the name already matches, got when=%v", task["when"])
	}
}

func TestStorageCephadmApplyGatesNodeHostname(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/"
	tasks := readAnsibleTasks(t, base+"phases/node_identity.yml")

	gather := tasks[findAnsibleTask(t, tasks, "Gather storage node platform facts")]
	setup, ok := gather["ansible.builtin.setup"].(map[string]any)
	if !ok {
		t.Fatalf("storage node fact gathering must be a setup task, got %v", gather)
	}
	if subset := fmt.Sprint(setup["gather_subset"]); !strings.Contains(subset, "platform") {
		t.Fatalf("storage node facts must include the platform subset so nodename is defined, got %v", setup["gather_subset"])
	}

	gateIdx := findAnsibleTask(t, tasks, "Refuse a storage node whose OS hostname is not the name cephadm will register")
	resolveIdx := findAnsibleTask(t, tasks, "Set current storage node")
	if gateIdx <= resolveIdx {
		t.Fatalf("the hostname gate must run after the current storage node is resolved")
	}

	writeIdx := findAnsibleTask(t, tasks, "Write the OS hostname cephadm will register")
	if writeIdx <= resolveIdx || writeIdx >= gateIdx {
		t.Fatalf("the hostname write must run after the node is resolved and before the gate verifies it")
	}
	write := tasks[writeIdx]
	hostname, ok := write["ansible.builtin.hostname"].(map[string]any)
	if !ok {
		t.Fatalf("the hostname write must use the hostname module so it persists, got %v", write)
	}
	if got := fmt.Sprint(hostname["name"]); !strings.Contains(got, "bootwright_current_storage_host.cephHostname") {
		t.Fatalf("the hostname write must write the name cephadm registers, got %v", hostname["name"])
	}
	if got := fmt.Sprint(write["become"]); got != "true" {
		t.Fatalf("writing the OS hostname needs privilege, got become=%v", write["become"])
	}

	rereadIdx := findAnsibleTask(t, tasks, "Re-read storage node platform facts after writing the hostname")
	if rereadIdx <= writeIdx || rereadIdx >= gateIdx {
		t.Fatalf("the gate must read facts gathered after the write, not the stale ones")
	}
	reread, ok := tasks[rereadIdx]["ansible.builtin.setup"].(map[string]any)
	if !ok {
		t.Fatalf("re-reading the hostname must be a setup task, got %v", tasks[rereadIdx])
	}
	if subset := fmt.Sprint(reread["gather_subset"]); !strings.Contains(subset, "platform") {
		t.Fatalf("the re-read must include the platform subset so nodename is refreshed, got %v", reread["gather_subset"])
	}
	gate := tasks[gateIdx]
	body, ok := gate["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the hostname gate must be an assert, got %v", gate)
	}
	that := fmt.Sprint(body["that"])
	if !strings.Contains(that, "ansible_facts['nodename'] == bootwright_current_storage_host.cephHostname") {
		t.Fatalf("the hostname gate must compare the real OS hostname with the name cephadm registers, got %v", body["that"])
	}
	for _, reject := range []string{"ansible_facts['hostname']", "split('.')", "| lower"} {
		if strings.Contains(that, reject) {
			t.Fatalf("the hostname gate must not weaken the cephadm comparison with %s, got %v", reject, body["that"])
		}
	}
	for _, task := range tasks {
		if _, gated := task["when"]; gated {
			t.Fatalf("node identity must not be gated: --stage base sets skip_prereqs and would bypass the gate, got %v", task)
		}
	}

	main := readAnsibleTasks(t, base+"main.yml")
	identity := main[findAnsibleTask(t, main, "Resolve and gate Ceph storage node identity")]
	if _, gated := identity["when"]; gated {
		t.Fatalf("the node identity phase must run in every apply mode, got %v", identity["when"])
	}
	if got := fmt.Sprint(identity["ansible.builtin.include_tasks"]); !strings.Contains(got, "node_identity.yml") {
		t.Fatalf("the node identity phase must include node_identity.yml, got %v", got)
	}
	if identityIdx, contextIdx := findAnsibleTask(t, main, "Resolve and gate Ceph storage node identity"), findAnsibleTask(t, main, "Prepare Ceph storage node context"); identityIdx >= contextIdx {
		t.Fatalf("node identity must be gated before the context phase mutates the node")
	}
}

func TestStorageCephadmSpecApplyRefusesNonZeroExit(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/service_specs.yml"
	tasks := readAnsibleTasks(t, path)

	apply := tasks[findAnsibleTask(t, tasks, "Apply Ceph host, mon, and mgr specs")]
	if got := fmt.Sprint(apply["failed_when"]); got != "false" {
		t.Fatalf("the spec apply must defer failure to the assert so its diagnostic is reachable, got failed_when=%v", apply["failed_when"])
	}

	body, ok := tasks[findAnsibleTask(t, tasks, "Refuse a bootstrap spec cephadm reported an error for")]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the bootstrap spec verdict must be an assert")
	}
	that := fmt.Sprint(body["that"])
	if !strings.Contains(that, "bootwright_ceph_host_spec_apply.rc | default(1) | int == 0") {
		t.Fatalf("deferring the failure requires the assert to carry the exit status, got %v", body["that"])
	}
	for _, stream := range []string{"stdout", "stderr"} {
		if !strings.Contains(that, "bootwright_ceph_host_spec_apply."+stream) {
			t.Fatalf("the assert must still read %s, which cephadm uses to reject one document while exiting zero, got %v", stream, body["that"])
		}
	}
}

func TestStorageCephadmDestroyRefusesUnsafeDevices(t *testing.T) {
	tasks := storageCephDestroyTasks(t)

	validate := tasks[findAnsibleTask(t, tasks, "Validate declared Ceph destroy devices")]
	assertBlock, ok := validate["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("device validation must be an assert, got %v", validate)
	}
	if got := fmt.Sprint(assertBlock["that"]); !strings.Contains(got, "disk/by-id/[^/]+") || !strings.Contains(got, "disk/by-path/[^/]+") {
		t.Fatalf("device regex must anchor stable by-id/by-path paths, got %v", assertBlock["that"])
	}

	probeIdx := findAnsibleTask(t, tasks, "Probe declared Ceph destroy devices for active mounts")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to wipe mounted or in-use Ceph destroy devices")
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	if !(probeIdx < refuseIdx && refuseIdx < wipeIdx) {
		t.Fatalf("ceph destroy must probe and refuse mounted devices before wiping (probe=%d refuse=%d wipe=%d)", probeIdx, refuseIdx, wipeIdx)
	}
	probe, ok := tasks[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(probe["argv"]), "lsblk") {
		t.Fatalf("mount probe must run lsblk, got %v", tasks[probeIdx])
	}
}

func TestStorageCephadmReconcilesRegistryLogin(t *testing.T) {
	tasks := storageCephBootstrapTasks(t)
	idx := findAnsibleTask(t, tasks, "Reconcile cephadm registry login for credential rotation")
	step := tasks[idx]
	if got := fmt.Sprint(step["ansible.builtin.command"]); !strings.Contains(got, "registry-login") || !strings.Contains(got, "bootwright_ceph_remote_work_dir") || !strings.Contains(got, "/mnt/registry-login.json") {
		t.Fatalf("registry login must mount its staged JSON into cephadm shell, got %v", step["ansible.builtin.command"])
	}
	if got := fmt.Sprint(step["when"]); !strings.Contains(got, "bootwright_ceph_registry_credentials is defined") {
		t.Fatalf("registry login must be gated on resolved credentials, got when=%v", step["when"])
	}
	assertRedactsByDefault(t, "registry login", step["no_log"])
	pinIdx := findAnsibleTask(t, tasks, "Pin cephadm container image base to the distribution registry")
	if !(pinIdx < idx) {
		t.Fatalf("registry login reconcile must run after the cluster exists (pin=%d login=%d)", pinIdx, idx)
	}
}

func TestStorageCephadmPinsSidecarImagesEarlyAndConditionally(t *testing.T) {
	tasks := storageCephBootstrapTasks(t)

	bootstrapIdx := findAnsibleTask(t, tasks, "Resolve cephadm bootstrap command")
	readIdx := findAnsibleTask(t, tasks, "Read current cephadm sidecar container images")
	setIdx := findAnsibleTask(t, tasks, "Pin cephadm sidecar container images before the monitoring stack deploys")
	if readIdx >= setIdx {
		t.Fatalf("the sidecar pin must read the live value before setting it (read=%d set=%d)", readIdx, setIdx)
	}
	phase := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap.yml")
	pinInclude := strings.Index(phase, "bootstrap_steps/container_image_base.yml")
	opsInclude := strings.Index(phase, "bootstrap_steps/topology_operations.yml")
	if pinInclude < 0 || opsInclude < 0 || pinInclude > opsInclude {
		t.Fatalf("the container image pins must be included well before the generic config operations; cephadm bootstrap deploys the monitoring stack in-process, so a late pin means the first pull already went to cephadm's upstream defaults (pin=%d ops=%d)", pinInclude, opsInclude)
	}
	if got := fmt.Sprint(tasks[setIdx]["when"]); !strings.Contains(got, "item.stdout") {
		t.Fatalf("the sidecar pin must keep its ceph config get guard so re-apply is a no-op, got when=%v", tasks[setIdx]["when"])
	}

	resolve := fmt.Sprint(tasks[bootstrapIdx]["ansible.builtin.set_fact"])
	if !strings.Contains(resolve, "--skip-monitoring-stack") {
		t.Fatalf("bootstrap argv must still be able to skip the monitoring stack, got %v", resolve)
	}
	if !strings.Contains(resolve, "skipMonitoringStack") {
		t.Fatalf("--skip-monitoring-stack must stay conditional on the rendered skipMonitoringStack flag. Making it unconditional silently deletes monitoring for every zero-config cluster: cephadmMonitoringSpecs renders nothing without a declared role or monitoring block, and Bootwright never renders ceph-exporter or crash at all. Fix sidecar image pulls by seeding them, not by skipping the stack. Got %v", resolve)
	}
}

func TestStorageCephadmPinsDaemonImageOnEveryApply(t *testing.T) {
	tasks := storageCephBootstrapTasks(t)

	readIdx := findAnsibleTask(t, tasks, "Read the Ceph cluster configuration carrying the daemon container image")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve the current Ceph daemon container image")
	setIdx := findAnsibleTask(t, tasks, "Pin the Ceph daemon container image")
	if !(readIdx < resolveIdx && resolveIdx < setIdx) {
		t.Fatalf("the daemon image pin must read and resolve the live value before setting it (read=%d resolve=%d set=%d)", readIdx, resolveIdx, setIdx)
	}
	assertCephGlobalConfigRead(t, tasks[readIdx], "container_image")

	set := fmt.Sprint(tasks[setIdx]["ansible.builtin.command"])
	for _, want := range []string{"config", "set", "global", "container_image", "bootwright_ceph_bootstrap_image"} {
		if !strings.Contains(set, want) {
			t.Fatalf("the daemon image pin must run ceph config set global container_image <resolved image>; missing %q in %v", want, tasks[setIdx]["ansible.builtin.command"])
		}
	}

	when := fmt.Sprint(tasks[setIdx]["when"])
	if !strings.Contains(when, "bootwright_ceph_daemon_image_current") {
		t.Fatalf("the daemon image pin must keep its live-value guard so re-apply is a no-op, got when=%v", tasks[setIdx]["when"])
	}
	if !strings.Contains(when, "bootwright_ceph_bootstrap_image") || !strings.Contains(when, "length > 0") {
		t.Fatalf("the daemon image pin must be skipped when no image.version is pinned; writing an empty or tagless value would clear a pin an out-of-band ceph orch upgrade wrote. Got when=%v", tasks[setIdx]["when"])
	}

	phase := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap.yml")
	pinInclude := strings.Index(phase, "bootstrap_steps/container_image_base.yml")
	specsInclude := strings.Index(phase, "bootstrap_steps/service_specs.yml")
	if pinInclude < 0 || specsInclude < 0 || pinInclude > specsInclude {
		t.Fatalf("the daemon image pin must be asserted before the first ceph orch apply, or the services deployed by this very run still come up on the old image (pin=%d specs=%d)", pinInclude, specsInclude)
	}
}

func assertCephGlobalConfigRead(t *testing.T, task map[string]any, key string) {
	t.Helper()
	read := fmt.Sprint(task["ansible.builtin.command"])
	if strings.Contains(read, "get") && strings.Contains(read, "global") {
		t.Fatalf("reading a global Ceph option with `ceph config get global %s` fails with `Error EINVAL: unrecognized entity 'global'` — config get takes an entity (mon, osd, mgr), not a config section, so the read yields nothing, the guard never matches and the value is rewritten on every apply. Read `ceph config dump` and select the global section instead. Got %v", key, task["ansible.builtin.command"])
	}
	for _, want := range []string{"config", "dump", "json"} {
		if !strings.Contains(read, want) {
			t.Fatalf("the %s read must come from `ceph config dump --format json`; missing %q in %v", key, want, task["ansible.builtin.command"])
		}
	}
}

func TestStorageCephNetworksAreAssertedBeforeDaemonPlacement(t *testing.T) {
	tasks := storageCephBootstrapTasks(t)

	readIdx := findAnsibleTask(t, tasks, "Read the Ceph cluster configuration currently in force")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve the Ceph networks currently in force")
	setIdx := findAnsibleTask(t, tasks, "Reconcile the Ceph public network before any daemon placement is scheduled")
	if !(readIdx < resolveIdx && resolveIdx < setIdx) {
		t.Fatalf("the network write must read and resolve the live value before setting it (read=%d resolve=%d set=%d)", readIdx, resolveIdx, setIdx)
	}
	assertCephGlobalConfigRead(t, tasks[readIdx], "public_network")

	set := fmt.Sprint(tasks[setIdx]["ansible.builtin.command"])
	for _, want := range []string{"config", "set", "global", "public_network", "bootwright_ceph_public_network_declared"} {
		if !strings.Contains(set, want) {
			t.Fatalf("the public network must be written with ceph config set global public_network <declared>; missing %q in %v", want, tasks[setIdx]["ansible.builtin.command"])
		}
	}
	when := fmt.Sprint(tasks[setIdx]["when"])
	if !strings.Contains(when, "bootwright_ceph_public_network_current") {
		t.Fatalf("the public network write must keep its live-value guard so re-apply is a no-op, got when=%v", tasks[setIdx]["when"])
	}
	if !strings.Contains(when, "length > 0") {
		t.Fatalf("the public network write must be skipped when publicCIDRs is undeclared; writing an empty value would clear the network cephadm bootstrap derived, got when=%v", tasks[setIdx]["when"])
	}

	phase := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap.yml")
	networkInclude := strings.Index(phase, "bootstrap_steps/network_config.yml")
	specsInclude := strings.Index(phase, "bootstrap_steps/service_specs.yml")
	monInclude := strings.Index(phase, "bootstrap_steps/mon_readiness.yml")
	topologyInclude := strings.Index(phase, "bootstrap_steps/topology_operations.yml")
	if networkInclude < 0 || specsInclude < 0 || monInclude < 0 || topologyInclude < 0 {
		t.Fatalf("bootstrap phase is missing a step this ordering depends on (network=%d specs=%d mon=%d topology=%d)", networkInclude, specsInclude, monInclude, topologyInclude)
	}
	if networkInclude > specsInclude || networkInclude > monInclude {
		t.Fatalf("public_network must be asserted before the mon placement is applied and before the monmap gate waits on it; cephadm drops a mon host holding no address inside public_network, so a corrected publicCIDRs that lands only in the later topology operations can never converge (network=%d specs=%d mon=%d)", networkInclude, specsInclude, monInclude)
	}
	if topologyInclude < monInclude {
		t.Fatalf("this test assumes the topology operations still follow the monmap gate (topology=%d mon=%d)", topologyInclude, monInclude)
	}
}

func TestStorageCephadmOverrideRebuildVerifiesOwnershipMarker(t *testing.T) {
	tasks := storageCephBootstrapTasks(t)

	readIdx := findAnsibleTask(t, tasks, "Read Bootwright Ceph ownership marker for override rebuild")
	decideIdx := findAnsibleTask(t, tasks, "Decide override rebuild ownership")
	gateIdx := findAnsibleTask(t, tasks, "Enforce apply mode for the Ceph cluster")
	rebuildIdx := findAnsibleTask(t, tasks, "Decide whether this cluster requires an authorized override rebuild")
	deviceGateIdx := findAnsibleTask(t, tasks, "Refuse to wipe present Ceph devices without a valid Bootwright OSD record")
	zapIdx := findAnsibleTask(t, tasks, "Remove existing cephadm cluster for override rebuild on every topology host")
	if !(readIdx < decideIdx && decideIdx < gateIdx && gateIdx < rebuildIdx && rebuildIdx < deviceGateIdx && deviceGateIdx < zapIdx) {
		t.Fatalf("override rebuild must prove cluster and device ownership before zapping every host (read=%d decide=%d gate=%d rebuild=%d device=%d zap=%d)", readIdx, decideIdx, gateIdx, rebuildIdx, deviceGateIdx, zapIdx)
	}

	gate := tasks[gateIdx]
	gateInclude, ok := gate["ansible.builtin.include_role"].(map[string]any)
	if !ok {
		t.Fatalf("apply-mode gate must include the ownership_record role, got %v", gate)
	}
	if gateInclude["name"] != "bootwright.core.ownership_record" || gateInclude["tasks_from"] != "apply_mode_gate.yml" {
		t.Fatalf("apply-mode gate must include ownership_record apply_mode_gate.yml, got %v", gateInclude)
	}
	gateVars, ok := gate["vars"].(map[string]any)
	if !ok {
		t.Fatalf("apply-mode gate must pass gate vars, got %v", gate)
	}
	if got := fmt.Sprint(gateVars["bootwright_gate_owned"]); !strings.Contains(got, "bootwright_ceph_override_owned") {
		t.Fatalf("apply-mode gate must be fed the decided ownership, got %v", gateVars["bootwright_gate_owned"])
	}

	zap, ok := tasks[zapIdx]["ansible.builtin.command"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(zap["argv"]), "rm-cluster") || !strings.Contains(fmt.Sprint(zap["argv"]), "--zap-osds") {
		t.Fatalf("override rebuild must zap via cephadm rm-cluster --zap-osds, got %v", tasks[zapIdx])
	}
	if got := fmt.Sprint(tasks[zapIdx]["when"]); !strings.Contains(got, "hostvars[bootwright_selected_storage_cluster.seedHost]") || !strings.Contains(got, "bootwright_ceph_rebuild_cleanup_required") || strings.Contains(got, "inventory_hostname") {
		t.Fatalf("override rebuild zap must consume the seed authorization on every host, got %v", tasks[zapIdx]["when"])
	}
	rebuild, ok := tasks[rebuildIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("rebuild decision must be a set_fact, got %v", tasks[rebuildIdx])
	}
	rebuildExpr := fmt.Sprint(rebuild["bootwright_ceph_rebuild_cleanup_required"])
	for _, want := range []string{"bootwright_apply_mode", "bootwright_ceph_override_owned", "bootwright_ceph_rebuild_authorized", "bootwright_ceph_reconcilable_only", "bootwright_ceph_incomplete_bootstrap"} {
		if !strings.Contains(rebuildExpr, want) {
			t.Fatalf("rebuild decision must require %s, got %v", want, rebuildExpr)
		}
	}

	decide, ok := tasks[decideIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("ownership decision must be a set_fact, got %v", tasks[decideIdx])
	}
	owned := fmt.Sprint(decide["bootwright_ceph_override_owned"])
	for _, want := range []string{
		"bootwright_ceph_override_record.stat.exists",
		"bootwright_selected_storage_cluster.seedHost",
		"bootwright_ceph_override_fsid",
		"bootwright_ceph_override_owned_fsid",
	} {
		if !strings.Contains(owned, want) {
			t.Fatalf("ownership decision must require %s, got %v", want, decide["bootwright_ceph_override_owned"])
		}
	}

	recordIdx := findAnsibleTask(t, tasks, "Record storage cluster ownership early for recoverability")
	sshTrustIdx := findAnsibleTask(t, tasks, "Configure cephadm SSH trust")
	if !(recordIdx < sshTrustIdx) {
		t.Fatalf("ownership record must be written before the SSH-trust/service steps (record=%d ssh=%d)", recordIdx, sshTrustIdx)
	}
	recordRole, ok := tasks[recordIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok {
		t.Fatalf("early ownership record must use include_role, got %v", tasks[recordIdx])
	}
	recordApply, ok := recordRole["apply"].(map[string]any)
	if !ok || recordApply["delegate_to"] != "localhost" || recordApply["become"] != false {
		t.Fatalf("storage ownership record must be controller-local without become, got %v", recordRole["apply"])
	}
	overrideRecordIdx := findAnsibleTask(t, tasks, "Stat Bootwright storage cluster ownership record for override rebuild")
	if tasks[overrideRecordIdx]["delegate_to"] != "localhost" || tasks[overrideRecordIdx]["become"] != false {
		t.Fatalf("storage ownership probe must read controller-local evidence, got %v", tasks[overrideRecordIdx])
	}
	for _, name := range []string{
		"Pre-record storage cluster ownership before bootstrap",
		"Record storage cluster ownership",
	} {
		idx := findAnsibleTask(t, tasks, name)
		role, ok := tasks[idx]["ansible.builtin.include_role"].(map[string]any)
		if !ok {
			t.Fatalf("%s must use include_role, got %v", name, tasks[idx])
		}
		apply, ok := role["apply"].(map[string]any)
		if !ok || apply["delegate_to"] != "localhost" || apply["become"] != false {
			t.Fatalf("%s must be controller-local without become, got %v", name, role["apply"])
		}
	}

	stampIdx := findAnsibleTask(t, tasks, "Stamp Bootwright Ceph ownership marker")
	stamp, ok := tasks[stampIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("ownership marker must be written with copy, got %v", tasks[stampIdx])
	}
	if got := fmt.Sprint(stamp["dest"]); !strings.Contains(got, "bootwright_ceph_ownership_marker_path") {
		t.Fatalf("ownership marker must write the marker path, got %v", stamp["dest"])
	}
	if got := fmt.Sprint(stamp["mode"]); got != "0600" {
		t.Fatalf("ownership marker must be mode 0600, got %v", stamp["mode"])
	}
}

func TestStorageCephadmRecoversIncompleteBootstrapUnderOverride(t *testing.T) {
	tasks := storageCephBootstrapTasks(t)

	detectIdx := findAnsibleTask(t, tasks, "Detect an incomplete Bootwright bootstrap eligible for override rebuild")
	gateIdx := findAnsibleTask(t, tasks, "Enforce apply mode for the Ceph cluster")
	rebuildIdx := findAnsibleTask(t, tasks, "Decide whether this cluster requires an authorized override rebuild")
	zapIdx := findAnsibleTask(t, tasks, "Remove existing cephadm cluster for override rebuild on every topology host")
	if !(detectIdx < gateIdx && gateIdx < rebuildIdx && rebuildIdx < zapIdx) {
		t.Fatalf("incomplete-bootstrap detection must run before the gate, rebuild decision, and zap (detect=%d gate=%d rebuild=%d zap=%d)", detectIdx, gateIdx, rebuildIdx, zapIdx)
	}

	detect, ok := tasks[detectIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("incomplete-bootstrap detection must be a set_fact, got %v", tasks[detectIdx])
	}
	expr := fmt.Sprint(detect["bootwright_ceph_incomplete_bootstrap"])
	for _, want := range []string{
		"bootwright_ceph_override_record.stat.exists",
		"bootwright_ceph_override_owned_fsid | default('') | length == 0",
		"bootwright_ceph_override_reachable",
		"bootwright_selected_storage_cluster.seedHost",
	} {
		if !strings.Contains(expr, want) {
			t.Fatalf("incomplete-bootstrap detection must require %q, got %v", want, detect["bootwright_ceph_incomplete_bootstrap"])
		}
	}
	if !strings.Contains(expr, "not (bootwright_ceph_override_reachable") {
		t.Fatalf("incomplete-bootstrap detection must require the cluster to be unreachable, got %v", detect["bootwright_ceph_incomplete_bootstrap"])
	}

	rebuild, ok := tasks[rebuildIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(rebuild["bootwright_ceph_rebuild_cleanup_required"]), "bootwright_ceph_incomplete_bootstrap") {
		t.Fatalf("override rebuild decision must honor the incomplete-bootstrap decision, got %v", tasks[rebuildIdx])
	}
	if got := fmt.Sprint(tasks[zapIdx]["when"]); !strings.Contains(got, "bootwright_ceph_rebuild_cleanup_required") {
		t.Fatalf("override rebuild zap must consume the authorized rebuild decision, got %v", tasks[zapIdx]["when"])
	}

	refuseIdx := findAnsibleTask(t, tasks, "Refuse to touch a Bootwright Ceph cluster whose ownership marker is missing")
	if !(detectIdx < refuseIdx && refuseIdx < gateIdx) {
		t.Fatalf("missing-marker refusal must run after detection and before the gate (detect=%d refuse=%d gate=%d)", detectIdx, refuseIdx, gateIdx)
	}
	if _, ok := tasks[refuseIdx]["ansible.builtin.fail"].(map[string]any); !ok {
		t.Fatalf("missing-marker refusal must be a fail, got %v", tasks[refuseIdx])
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); !strings.Contains(got, "bootwright_ceph_override_owned_fsid | default('') | length == 0") {
		t.Fatalf("missing-marker refusal must be gated on the absent ownership marker, got %v", tasks[refuseIdx]["when"])
	}
}

func TestStorageCephadmOverrideRebuildRmClusterFailsClosed(t *testing.T) {
	tasks := storageCephBootstrapTasks(t)
	rm := tasks[findAnsibleTask(t, tasks, "Remove existing cephadm cluster for override rebuild on every topology host")]
	if got := fmt.Sprint(rm["failed_when"]); !strings.Contains(got, "!= 0") {
		t.Fatalf("override rebuild rm-cluster must fail closed before clearing config and re-bootstrapping, got failed_when=%v", rm["failed_when"])
	}
}

func TestStorageCephadmDestroyVerifiesOwnershipAndFailsClosed(t *testing.T) {
	tasks := storageCephDestroyTasks(t)

	confIdx := findAnsibleTask(t, tasks, "Resolve existing Ceph configuration fsid on seed host")
	resolveRecoveryIdx := findAnsibleTask(t, tasks, "Resolve confirmed Ceph ownership recovery fsid on seed host")
	validateRecoveryIdx := findAnsibleTask(t, tasks, "Refuse Ceph ownership recovery without matching evidence")
	recoverRecordIdx := findAnsibleTask(t, tasks, "Recover Bootwright storage cluster ownership record on controller")
	recoverIdx := findAnsibleTask(t, tasks, "Recover Bootwright Ceph ownership marker on seed host")
	recordIdx := findAnsibleTask(t, tasks, "Stat Bootwright storage cluster ownership record on controller")
	readIdx := findAnsibleTask(t, tasks, "Read Bootwright Ceph ownership marker on seed host")
	decideIdx := findAnsibleTask(t, tasks, "Decide Ceph destroy ownership on seed host")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to destroy a non-Bootwright Ceph cluster on seed host")
	rmIdx := findAnsibleTask(t, tasks, "Remove cephadm cluster on seed host")
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	if !(confIdx < resolveRecoveryIdx && resolveRecoveryIdx < validateRecoveryIdx && validateRecoveryIdx < recoverRecordIdx && recoverRecordIdx < recoverIdx && recoverIdx < recordIdx && recordIdx < readIdx && readIdx < decideIdx && decideIdx < refuseIdx && refuseIdx < rmIdx && rmIdx < wipeIdx) {
		t.Fatalf("ceph destroy must validate recovery, reconstruct and re-read ownership, and verify it before removing the cluster and wiping (conf=%d resolveRecovery=%d validateRecovery=%d recoverRecord=%d recoverMarker=%d record=%d read=%d decide=%d refuse=%d rm=%d wipe=%d)", confIdx, resolveRecoveryIdx, validateRecoveryIdx, recoverRecordIdx, recoverIdx, recordIdx, readIdx, decideIdx, refuseIdx, rmIdx, wipeIdx)
	}

	recoveryAssert, ok := tasks[validateRecoveryIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("ceph ownership recovery gate must be an assert, got %v", tasks[validateRecoveryIdx])
	}
	recoveryThat := fmt.Sprint(recoveryAssert["that"])
	for _, want := range []string{
		"bootwright_ceph_destroy_conf_fsid",
		"bootwright_ceph_destroy_confirmed_fsid",
		"bootwright_ceph_fsid.stdout",
		"bootwright_selected_storage_cluster.seedHost",
	} {
		if !strings.Contains(recoveryThat, want) {
			t.Fatalf("ceph ownership recovery gate must require %s, got %v", want, recoveryAssert["that"])
		}
	}
	if strings.Contains(recoveryThat, "bootwright_ceph_destroy_record.stat.exists") {
		t.Fatalf("ceph ownership recovery must be able to reconstruct a missing controller record, got %v", recoveryAssert["that"])
	}
	recoveryRole, ok := tasks[recoverRecordIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok {
		t.Fatalf("ceph controller ownership recovery must use include_role, got %v", tasks[recoverRecordIdx])
	}
	recoveryApply, ok := recoveryRole["apply"].(map[string]any)
	if !ok || recoveryApply["delegate_to"] != "localhost" || recoveryApply["become"] != false {
		t.Fatalf("ceph controller ownership recovery must be controller-local without become, got %v", recoveryRole["apply"])
	}
	if tasks[recordIdx]["delegate_to"] != "localhost" || tasks[recordIdx]["become"] != false {
		t.Fatalf("ceph destroy ownership probe must read controller-local evidence, got %v", tasks[recordIdx])
	}
	recoveryCopy, ok := tasks[recoverIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("ceph ownership recovery must use copy, got %v", tasks[recoverIdx])
	}
	if recoveryCopy["mode"] != "0600" {
		t.Fatalf("ceph ownership recovery marker mode = %v, want 0600", recoveryCopy["mode"])
	}
	recoveryContent := fmt.Sprint(recoveryCopy["content"])
	for _, want := range []string{"manager", "bootwright", "cluster", "bootwright_ceph_destroy_confirmed_fsid"} {
		if !strings.Contains(recoveryContent, want) {
			t.Fatalf("ceph ownership recovery marker must contain %s, got %v", want, recoveryCopy["content"])
		}
	}

	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("ceph destroy ownership guard must be an assert, got %v", tasks[refuseIdx])
	}
	if got := fmt.Sprint(refuse["that"]); !strings.Contains(got, "bootwright_ceph_destroy_owned") {
		t.Fatalf("ceph destroy guard must require proven Bootwright ownership, got %v", refuse["that"])
	}

	decide, ok := tasks[decideIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("ceph destroy ownership decision must be a set_fact, got %v", tasks[decideIdx])
	}
	owned := fmt.Sprint(decide["bootwright_ceph_destroy_owned"])
	for _, want := range []string{
		"bootwright_ceph_destroy_conf_check.rc",
		"bootwright_ceph_destroy_record.stat.exists",
		"bootwright_selected_storage_cluster.seedHost",
		"bootwright_ceph_destroy_conf_fsid",
		"bootwright_ceph_fsid.stdout",
	} {
		if !strings.Contains(owned, want) {
			t.Fatalf("ceph destroy ownership decision must require %s, got %v", want, decide["bootwright_ceph_destroy_owned"])
		}
	}
	if !strings.Contains(owned, "bootwright_ceph_destroy_owned_fsid | default('') | length > 0") {
		t.Fatalf("ceph destroy ownership decision must require a present on-host marker fsid (no empty-marker fail-open), got %v", decide["bootwright_ceph_destroy_owned"])
	}
	if strings.Contains(owned, "bootwright_ceph_destroy_owned_fsid | default('') | length == 0") {
		t.Fatalf("ceph destroy ownership decision must not treat an empty marker fsid as owned, got %v", decide["bootwright_ceph_destroy_owned"])
	}

	rm := tasks[rmIdx]
	if got := fmt.Sprint(rm["when"]); !strings.Contains(got, "bootwright_ceph_destroy_owned") {
		t.Fatalf("ceph destroy rm-cluster must be gated on proven ownership, got %v", rm["when"])
	}
	if got := fmt.Sprint(rm["failed_when"]); !strings.Contains(got, "!= 0") {
		t.Fatalf("ceph destroy rm-cluster must fail closed on error, got failed_when=%v", rm["failed_when"])
	}

	removeRecordIdx := findAnsibleTask(t, tasks, "Remove storage cluster ownership record")
	removeRole, ok := tasks[removeRecordIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok {
		t.Fatalf("storage ownership removal must use include_role, got %v", tasks[removeRecordIdx])
	}
	removeApply, ok := removeRole["apply"].(map[string]any)
	if !ok || removeApply["delegate_to"] != "localhost" || removeApply["become"] != false {
		t.Fatalf("storage ownership removal must be controller-local without become, got %v", removeRole["apply"])
	}
	removeWhen := fmt.Sprint(tasks[removeRecordIdx]["when"])
	if !strings.Contains(removeWhen, "bootwright_selected_storage_cluster.seedHost") || !strings.Contains(removeWhen, "bootwright_storage_cluster_partial") {
		t.Fatalf("storage ownership removal must run once on the seed and preserve partial state, got %v", tasks[removeRecordIdx]["when"])
	}

	zap := tasks[findAnsibleTask(t, tasks, "Zap declared Ceph device partition tables")]
	if got := fmt.Sprint(zap["failed_when"]); !strings.Contains(got, "!= 0") {
		t.Fatalf("ceph destroy sgdisk zap must fail closed on error, got failed_when=%v", zap["failed_when"])
	}
}

func TestStorageCephadmDestroyPlayAbortsOnSeedRefusal(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_destroy.yml")
	if len(plays) != 1 {
		t.Fatalf("storage destroy plays = %d, want 1", len(plays))
	}
	if got := plays[0]["any_errors_fatal"]; got != true {
		t.Fatalf("storage destroy play must run any_errors_fatal so a seed ownership refusal aborts before any node wipes devices, got %v", got)
	}
}

func TestStorageCephadmDestroySkipUnreachableGuards(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_destroy.yml")
	if len(plays) != 1 {
		t.Fatalf("storage destroy plays = %d, want 1", len(plays))
	}
	if got := plays[0]["any_errors_fatal"]; got != true {
		t.Fatalf("storage destroy play must keep any_errors_fatal literally true, got %v", got)
	}
	ignoreUnreachable, ok := plays[0]["ignore_unreachable"].(string)
	if !ok || !strings.Contains(ignoreUnreachable, "bootwright_destroy_skip_unreachable") {
		t.Fatalf("storage destroy play must template ignore_unreachable from bootwright_destroy_skip_unreachable so it is off by default, got %v", plays[0]["ignore_unreachable"])
	}
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	selectIdx := findAnsibleTask(t, tasks, "Select storage node teardown connection")
	probeIdx := findAnsibleTask(t, tasks, "Probe storage host reachability before teardown")
	recordIdx := findAnsibleTask(t, tasks, "Record storage host unreachable when no node identity answers")
	reachableIdx := findAnsibleTask(t, tasks, "Require storage hosts reachable unless --authorize unreachable-nodes")
	seedIdx := findAnsibleTask(t, tasks, "Require the Ceph seed host to be reachable before any device wipe")
	wipeIdx := findAnsibleTask(t, tasks, "Destroy Ceph storage cluster")
	if !(selectIdx < probeIdx && probeIdx < recordIdx && recordIdx < reachableIdx && reachableIdx < seedIdx) {
		t.Fatalf("storage destroy must select a recoverable identity and classify its failure before the fail-closed reachability gate")
	}
	if got := fmt.Sprint(tasks[selectIdx]["when"]); !strings.Contains(got, "bootwright_node_access is defined") {
		t.Fatalf("storage destroy must invoke the selector only for managed node accounts, got when=%v", tasks[selectIdx]["when"])
	}
	if got := fmt.Sprint(tasks[probeIdx]["when"]); !strings.Contains(got, "bootwright_node_access_connection_available") {
		t.Fatalf("storage destroy must not run a second SSH probe when neither node identity answers, got when=%v", tasks[probeIdx]["when"])
	}
	if got := fmt.Sprint(tasks[recordIdx]["when"]); !strings.Contains(got, "bootwright_node_access_connection_available") {
		t.Fatalf("storage destroy must classify an unavailable managed identity as unreachable, got when=%v", tasks[recordIdx]["when"])
	}
	if seedIdx >= wipeIdx {
		t.Fatalf("seed-reachability assert (idx %d) must run before the destroy include_role wipe (idx %d)", seedIdx, wipeIdx)
	}
	if _, ok := tasks[seedIdx]["ansible.builtin.assert"]; !ok {
		t.Fatalf("seed-reachability guard must be a hard assert so any_errors_fatal aborts all hosts, got %v", tasks[seedIdx])
	}

	destroyTasks := storageCephDestroyTasks(t)
	removeIdx := findAnsibleTask(t, destroyTasks, "Remove storage cluster ownership record")
	if got := fmt.Sprint(destroyTasks[removeIdx]["when"]); !strings.Contains(got, "bootwright_storage_cluster_partial") {
		t.Fatalf("storage cluster ownership-record removal must be gated on not-partial so a partial teardown keeps the record, got when=%v", destroyTasks[removeIdx]["when"])
	}
}

func TestMachineRegistrationDeregisterSelectsStorageTeardownConnection(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_registration_deregister.yml"
	plays := readAnsiblePlays(t, path)
	if len(plays) != 1 {
		t.Fatalf("%s has %d plays, want 1", path, len(plays))
	}
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	selectIdx := findAnsibleTask(t, tasks, "Select storage node teardown connection")
	probeIdx := findAnsibleTask(t, tasks, "Probe node reachability and escalation before deregistration")
	recordIdx := findAnsibleTask(t, tasks, "Record an unreachable node when no storage identity answers")
	endIdx := findAnsibleTask(t, tasks, "End nodes Bootwright cannot reach or escalate on")
	deregisterIdx := findAnsibleTask(t, tasks, "Deregister machine from RHSM")
	if !(selectIdx < probeIdx && probeIdx < recordIdx && recordIdx < endIdx && endIdx < deregisterIdx) {
		t.Fatalf("machine deregistration must select a recoverable identity and classify its failure before remote work")
	}
	if got := fmt.Sprint(tasks[selectIdx]["when"]); !strings.Contains(got, "bootwright_node_access is defined") {
		t.Fatalf("machine deregistration must invoke the selector only for managed node accounts, got when=%v", tasks[selectIdx]["when"])
	}
	if got := fmt.Sprint(tasks[probeIdx]["when"]); !strings.Contains(got, "bootwright_node_access_connection_available") {
		t.Fatalf("machine deregistration must skip its remote probe when neither identity answers, got when=%v", tasks[probeIdx]["when"])
	}
}

func TestStorageCephadmReclaimSkipsMarkerRecordedDevices(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve OSD devices to reclaim on this host")
	setFact, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("reclaim resolve must be a set_fact, got %v", tasks[resolveIdx])
	}
	here := fmt.Sprint(setFact["bootwright_ceph_reclaim_here"])
	if !strings.Contains(here, "difference(bootwright_ceph_owned_osd_devices") {
		t.Fatalf("reclaim resolve must subtract marker-recorded OSD devices so a uniform device name only reclaims the marker-lost host, got %v", here)
	}
}

func TestStorageCephadmReclaimSeparatesAbsentFromUnprobeableAndMounted(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml"
	tasks := readAnsibleTasks(t, path)
	classifyIdx := findAnsibleTask(t, tasks, "Classify reclaim devices by presence")
	unprobeableIdx := findAnsibleTask(t, tasks, "Refuse to reclaim a device that could not be probed")
	absentIdx := findAnsibleTask(t, tasks, "Report reclaim devices absent from this host")
	mountIdx := findAnsibleTask(t, tasks, "Refuse to reclaim a mounted or in-use device")
	if !(classifyIdx < unprobeableIdx && unprobeableIdx < absentIdx && absentIdx < mountIdx) {
		t.Fatalf("reclaim must classify presence before it refuses or reports (classify=%d unprobeable=%d absent=%d mount=%d)", classifyIdx, unprobeableIdx, absentIdx, mountIdx)
	}
	classify, ok := tasks[classifyIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("reclaim presence classification must be a set_fact, got %v", tasks[classifyIdx])
	}
	if got := fmt.Sprint(classify["bootwright_ceph_reclaim_absent"]); !strings.Contains(got, "not a block device") {
		t.Errorf("reclaim must recognize an absent device by the lsblk stderr the destroy path already keys on, got %v", got)
	}
	if _, isAssert := tasks[absentIdx]["ansible.builtin.assert"]; isAssert {
		t.Error("an absent declared device must be skipped and reported, not refused: --reclaim-devices has nothing to wipe there, and the OSD readiness count is the real diagnosis")
	}
	mount, ok := tasks[mountIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the mount refusal must be an assert, got %v", tasks[mountIdx])
	}
	if got := fmt.Sprint(mount["that"]); strings.Contains(got, "item.rc == 0") {
		t.Errorf("the mount refusal must not also carry the probe-failure condition: folding them together reported `lsblk: not a block device` as \"mounted/in use ()\" and sent the operator hunting a mount that never existed. got that=%v", got)
	}
	for _, name := range []string{"Refuse to reclaim a device that could not be probed", "Refuse to reclaim a mounted or in-use device"} {
		idx := findAnsibleTask(t, tasks, name)
		assertion, ok := tasks[idx]["ansible.builtin.assert"].(map[string]any)
		if !ok {
			t.Fatalf("%q must be an assert, got %v", name, tasks[idx])
		}
		if got := fmt.Sprint(assertion["that"]); strings.Contains(got, "bootwright_ceph_authorize_unowned_devices") {
			t.Errorf("%q is physical device safety and must be closed to every --authorize token (ADR 0034), got that=%v", name, got)
		}
	}
}

func TestStorageCephadmUnownedDeviceTokenGatesTheHoldersRefusal(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml"
	tasks := readAnsibleTasks(t, path)
	holdersIdx := findAnsibleTask(t, tasks, "Refuse to reclaim a device that still backs a live OSD")
	holders, ok := tasks[holdersIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the holders refusal must be an assert, got %v", tasks[holdersIdx])
	}
	that := fmt.Sprint(holders["that"])
	if !strings.Contains(that, "bootwright_ceph_authorize_unowned_devices") {
		t.Errorf("--authorize unowned-devices must relax the holders refusal (ADR 0034): without it an orphan LVM stack left by a destroyed cluster has no in-product remedy, got that=%v", that)
	}
	if !strings.Contains(that, "item.rc == 0") {
		t.Errorf("the holders refusal must still fail closed on a failed probe, got that=%v", that)
	}
	msg := fmt.Sprint(holders["fail_msg"])
	for _, want := range []string{"/var/lib/ceph", "orch osd rm", "--authorize data-loss,unowned-devices", "ORPHAN"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the holders refusal must name %q so it distinguishes a live OSD from an orphan and states the remedy for each, got fail_msg=%v", want, msg)
		}
	}
	if findAnsibleTaskIndex(tasks, "Probe this node for a live Ceph daemon tree") < 0 {
		t.Error("the holders refusal branches on whether this node runs Ceph at all; without the probe it would keep telling an operator to drain an OSD from a cluster that no longer exists")
	}
}

func TestStorageCephadmReclaimTearsDownLVMBeforeTheWipe(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml"
	tasks := readAnsibleTasks(t, path)
	vgchangeIdx := findAnsibleTask(t, tasks, "Deactivate the volume groups on the reclaimed OSD devices")
	vgremoveIdx := findAnsibleTask(t, tasks, "Remove the volume groups on the reclaimed OSD devices")
	pvremoveIdx := findAnsibleTask(t, tasks, "Remove the physical volume labels from the reclaimed OSD devices")
	wipeIdx := findAnsibleTask(t, tasks, "Reclaim named OSD devices by wiping signatures")
	if !(vgchangeIdx < vgremoveIdx && vgremoveIdx < pvremoveIdx && pvremoveIdx < wipeIdx) {
		t.Fatalf("an authorized reclaim must take the LVM stack down before wipefs: wipefs clears the PV label but leaves active LVs mapped, and ceph-volume refuses a device with holders regardless of its signatures, so the reclaim would clear the gate and still yield zero OSDs (vgchange=%d vgremove=%d pvremove=%d wipe=%d)", vgchangeIdx, vgremoveIdx, pvremoveIdx, wipeIdx)
	}
	for _, name := range []string{
		"Reclaim named OSD devices by wiping signatures",
		"Zap partition tables of reclaimed OSD devices",
	} {
		idx := findAnsibleTask(t, tasks, name)
		if got := fmt.Sprint(tasks[idx]["loop"]); !strings.Contains(got, "bootwright_ceph_reclaim_present") {
			t.Errorf("%q must wipe only the devices present on this host; looping the unfiltered reclaim set makes an absent device a wipe failure, got loop=%v", name, got)
		}
	}
}

func TestStorageCephadmDestroyUnownedDeviceTokenGatesTheSignatureRefusal(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/device_gates.yml"
	tasks := readAnsibleTasks(t, path)
	idx := findAnsibleTask(t, tasks, "Refuse to wipe present Ceph devices without a valid Bootwright OSD record")
	gate, ok := tasks[idx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the destroy device gate must be an assert, got %v", tasks[idx])
	}
	that := fmt.Sprint(gate["that"])
	if !strings.Contains(that, "bootwright_ceph_authorize_unowned_devices") {
		t.Errorf("--authorize unowned-devices must relax the destroy signature refusal too (ADR 0034): the same orphan blocks apply and destroy, and a token that cleared only one leaves the other as a wall, got that=%v", that)
	}
	if !strings.Contains(that, "item.rc == 0") {
		t.Errorf("the destroy device gate must still fail closed on a wipefs probe failure, got that=%v", that)
	}
	if got := fmt.Sprint(gate["fail_msg"]); !strings.Contains(got, "--authorize data-loss,unowned-devices") {
		t.Errorf("the destroy device refusal must name the token pair that proceeds, got fail_msg=%v", got)
	}
	mountIdx := findAnsibleTask(t, tasks, "Refuse to wipe mounted or in-use Ceph destroy devices")
	mount, ok := tasks[mountIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the destroy mount refusal must be an assert, got %v", tasks[mountIdx])
	}
	if got := fmt.Sprint(mount["that"]); strings.Contains(got, "bootwright_ceph_authorize_unowned_devices") {
		t.Errorf("no token relaxes the mounted-device refusal on destroy either, got that=%v", got)
	}
}

func TestStorageCephadmRecordsOSDDeviceMarkerOnApply(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml")
	readIdx := findAnsibleTask(t, tasks, "Read Bootwright OSD device ownership marker")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve devices recorded as Bootwright OSDs for this cluster")
	checkIdx := findAnsibleTask(t, tasks, "Check declared OSD devices are empty or Bootwright-owned")
	stampIdx := findAnsibleTask(t, tasks, "Stamp Bootwright OSD device ownership marker")
	if !(readIdx < resolveIdx && resolveIdx < checkIdx && checkIdx < stampIdx) {
		t.Fatalf("OSD device gate must read and resolve the marker before the device check and stamp after it (read=%d resolve=%d check=%d stamp=%d)", readIdx, resolveIdx, checkIdx, stampIdx)
	}
	if got := fmt.Sprint(tasks[readIdx]["failed_when"]); got != "false" {
		t.Fatalf("OSD marker read must tolerate a missing marker, got failed_when=%v", got)
	}
	if got := fmt.Sprint(tasks[resolveIdx]["when"]); !strings.Contains(got, "content is defined") {
		t.Fatalf("OSD owned-device resolution must be gated on marker content, got when=%v", got)
	}
	resolve, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("OSD owned-device resolution must be a set_fact, got %v", tasks[resolveIdx])
	}
	resolved := fmt.Sprint(resolve["bootwright_ceph_owned_osd_devices"])
	if !strings.Contains(resolved, "bootwright_selected_storage_cluster.name") || !strings.Contains(resolved, "bootwright_current_storage_host.hostname") {
		t.Fatalf("OSD owned-device resolution must require cluster and node to match the marker, got %v", resolved)
	}
	check, ok := tasks[checkIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("OSD device gate must be an assert with a remedial fail_msg, got %v", tasks[checkIdx])
	}
	if got := fmt.Sprint(check["that"]); !strings.Contains(got, "bootwright_ceph_owned_osd_devices") {
		t.Fatalf("OSD device check must exempt only recorded Bootwright devices, got that=%v", got)
	}
	msg := fmt.Sprint(check["fail_msg"])
	for _, want := range []string{"--reclaim-devices", "--authorize data-loss,unowned-devices"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the OSD device refusal must name %q: a bare --reclaim-devices on a signature-carrying disk is refused again by the data-loss gate and then by the holders gate (ADR 0034), so a remedy that omits the tokens sends the operator through two more walls, got fail_msg=%v", want, msg)
		}
	}
	stamp, ok := tasks[stampIdx]["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("OSD device marker must be written with copy, got %v", tasks[stampIdx])
	}
	if got := fmt.Sprint(stamp["dest"]); !strings.Contains(got, "bootwright_ceph_osd_marker_path") {
		t.Fatalf("OSD device marker must write the marker path, got %v", stamp["dest"])
	}
	if got := fmt.Sprint(stamp["content"]); !strings.Contains(got, "bootwright_current_storage_host.devices") {
		t.Fatalf("OSD device marker must record the node's declared devices, got %v", stamp["content"])
	}
	if got := fmt.Sprint(stamp["mode"]); got != "0600" {
		t.Fatalf("OSD device marker must be mode 0600, got %v", stamp["mode"])
	}
}

func TestStorageCephadmDestroyRefusesUnrecordedOSDDevices(t *testing.T) {
	tasks := storageCephDestroyTasks(t)
	readIdx := findAnsibleTask(t, tasks, "Read Bootwright OSD device ownership marker")
	probeIdx := findAnsibleTask(t, tasks, "Probe present Ceph destroy devices for data signatures")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to wipe present Ceph devices without a valid Bootwright OSD record")
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	if !(readIdx < probeIdx && probeIdx < refuseIdx && refuseIdx < wipeIdx) {
		t.Fatalf("ceph destroy must read the OSD marker, probe signatures, and refuse unproven devices before wiping (read=%d probe=%d refuse=%d wipe=%d)", readIdx, probeIdx, refuseIdx, wipeIdx)
	}
	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("OSD device guard must be an assert, got %v", tasks[refuseIdx])
	}
	that := fmt.Sprint(refuse["that"])
	if !strings.Contains(that, "bootwright_ceph_recorded_osd_devices") {
		t.Fatalf("OSD device guard must require declared devices to be in the recorded set, got %v", refuse["that"])
	}
	if !strings.Contains(that, "bootwright_ceph_osd_marker_valid") {
		t.Fatalf("OSD device guard must require a valid marker before exempting a recorded device, got %v", refuse["that"])
	}
	if !strings.Contains(that, "stdout") {
		t.Fatalf("OSD device guard must allow a signature-free (empty) device via the wipefs probe stdout, got %v", refuse["that"])
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); strings.Contains(got, "bootwright_ceph_osd_marker_valid") {
		t.Fatalf("OSD device guard must run unconditionally (fail closed on a missing marker), not be skipped when the marker is absent, got when=%v", tasks[refuseIdx]["when"])
	}
	resolve := tasks[findAnsibleTask(t, tasks, "Resolve recorded Bootwright OSD devices")]
	facts, ok := resolve["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("recorded-device resolution must be a set_fact, got %v", resolve)
	}
	valid := fmt.Sprint(facts["bootwright_ceph_osd_marker_valid"])
	for _, want := range []string{"manager", "'bootwright'", "bootwright_selected_storage_cluster.name", "bootwright_current_storage_host.hostname"} {
		if !strings.Contains(valid, want) {
			t.Fatalf("marker validity must require %s (manager+cluster+node), got %v", want, facts["bootwright_ceph_osd_marker_valid"])
		}
	}
	if got := fmt.Sprint(resolve["when"]); !strings.Contains(got, "content is defined") {
		t.Fatalf("recorded-device resolution must be gated on a present marker, got %v", resolve["when"])
	}
}

func TestStorageCephadmDestroyReprobesMountsBeforeWipe(t *testing.T) {
	tasks := storageCephDestroyTasks(t)
	reprobeIdx := findAnsibleTask(t, tasks, "Re-probe declared Ceph destroy devices for active mounts before wipe")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to wipe devices mounted since the first check")
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	if !(reprobeIdx < refuseIdx && refuseIdx < wipeIdx) {
		t.Fatalf("ceph destroy must re-probe mounts and refuse before wiping (reprobe=%d refuse=%d wipe=%d)", reprobeIdx, refuseIdx, wipeIdx)
	}
	if refuseIdx+1 != wipeIdx {
		t.Fatalf("mount re-probe refusal must be the task immediately before the wipe (refuse=%d wipe=%d)", refuseIdx, wipeIdx)
	}
}

func TestStorageCephadmDestroySkipsAbsentDevices(t *testing.T) {
	tasks := storageCephDestroyTasks(t)
	classifyIdx := findAnsibleTask(t, tasks, "Classify declared Ceph destroy devices by presence")
	unprobeableIdx := findAnsibleTask(t, tasks, "Refuse to wipe Ceph destroy devices that could not be probed")
	mountRefuseIdx := findAnsibleTask(t, tasks, "Refuse to wipe mounted or in-use Ceph destroy devices")
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	if !(classifyIdx < unprobeableIdx && unprobeableIdx < mountRefuseIdx && mountRefuseIdx < wipeIdx) {
		t.Fatalf("destroy must classify devices, fail closed on unprobeable, then refuse mounted, then wipe (classify=%d unprobeable=%d mount=%d wipe=%d)", classifyIdx, unprobeableIdx, mountRefuseIdx, wipeIdx)
	}

	classify, ok := tasks[classifyIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("device classification must be a set_fact, got %v", tasks[classifyIdx])
	}
	if got := fmt.Sprint(classify["bootwright_ceph_absent_devices"]); !strings.Contains(got, "not a block device") {
		t.Fatalf("absent devices must be recognized from the lsblk \"not a block device\" probe, got %v", classify["bootwright_ceph_absent_devices"])
	}
	if got := fmt.Sprint(classify["bootwright_ceph_present_devices"]); !strings.Contains(got, "selectattr('rc', 'equalto', 0)") {
		t.Fatalf("present devices must be the rc==0 probes, got %v", classify["bootwright_ceph_present_devices"])
	}

	unprobeable, ok := tasks[unprobeableIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("unprobeable guard must be an assert, got %v", tasks[unprobeableIdx])
	}
	if got := fmt.Sprint(unprobeable["that"]); !strings.Contains(got, "bootwright_ceph_unprobeable_devices") {
		t.Fatalf("unprobeable guard must fail closed on devices that could not be probed, got %v", unprobeable["that"])
	}

	if got := fmt.Sprint(tasks[mountRefuseIdx]["loop"]); !strings.Contains(got, "bootwright_ceph_present_probe") {
		t.Fatalf("mounted-device refusal must loop over present probes only, got %v", tasks[mountRefuseIdx]["loop"])
	}
	if got := fmt.Sprint(tasks[wipeIdx]["loop"]); !strings.Contains(got, "bootwright_ceph_present_devices") {
		t.Fatalf("device wipe must loop over present devices only, got %v", tasks[wipeIdx]["loop"])
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

func TestStorageCephadmApplyRebuildCleansTopologyBeforeSeedBootstrap(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_apply.yml")
	if len(plays) != 1 {
		t.Fatalf("storage apply plays = %d, want 1", len(plays))
	}
	if got := plays[0]["any_errors_fatal"]; got != true {
		t.Fatalf("storage apply must abort the selected topology when one rebuild guard or cleanup fails, got %v", got)
	}
	if got := plays[0]["strategy"]; got != "linear" {
		t.Fatalf("storage apply must keep rebuild guards and cleanup in lockstep before seed bootstrap, got strategy=%v", got)
	}
	playTasks := nestedAnsibleTasks(t, plays[0], "tasks")
	for _, task := range playTasks {
		if task["name"] == "End selected storage non-seed hosts" {
			t.Fatalf("storage base apply must retain non-seed topology hosts for rebuild cleanup")
		}
	}
	seedContext := playTasks[findAnsibleTask(t, playTasks, "Validate selected storage seed context")]
	if seedContext["run_once"] != true {
		t.Fatalf("seed context validation must run once for the selected topology, got %v", seedContext["run_once"])
	}
	if got := fmt.Sprint(seedContext["ansible.builtin.assert"]); !strings.Contains(got, "ansible_play_hosts_all") || strings.Contains(got, "== inventory_hostname") {
		t.Fatalf("seed context must validate topology membership instead of ending non-seed hosts, got %v", got)
	}

	roleTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/main.yml")
	varsIdx := findAnsibleTask(t, roleTasks, "Resolve Ceph storage role variables")
	rebuildIdx := findAnsibleTask(t, roleTasks, "Guard and clean an authorized Ceph cluster rebuild")
	endNonSeedIdx := findAnsibleTask(t, roleTasks, "End non-seed hosts after the cluster-wide rebuild phase")
	seedPrepareIdx := findAnsibleTask(t, roleTasks, "Prepare Ceph seed convergence context")
	bootstrapIdx := findAnsibleTask(t, roleTasks, "Bootstrap and converge Ceph cluster")
	if !(varsIdx < rebuildIdx && rebuildIdx < endNonSeedIdx && endNonSeedIdx < seedPrepareIdx && seedPrepareIdx < bootstrapIdx) {
		t.Fatalf("role must resolve cheap variables, finish all-host cleanup, end non-seeds, then load seed context and bootstrap")
	}
	if got := fmt.Sprint(roleTasks[endNonSeedIdx]["when"]); !strings.Contains(got, "seedHost") || !strings.Contains(got, "prereqs_only") {
		t.Fatalf("non-seed short circuit must preserve all-host prerequisite tasks and run after rebuild, got when=%v", roleTasks[endNonSeedIdx]["when"])
	}
	if got := fmt.Sprint(roleTasks[bootstrapIdx]["when"]); !strings.Contains(got, "inventory_hostname") || !strings.Contains(got, "seedHost") {
		t.Fatalf("bootstrap phase must remain seed-only, got when=%v", roleTasks[bootstrapIdx]["when"])
	}

	rebuildTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/rebuild.yml")
	classifyIdx := findAnsibleTask(t, rebuildTasks, "Classify Ceph cluster apply mode on the seed host")
	gatherIdx := findAnsibleTask(t, rebuildTasks, "Gather storage node OS facts for authorized rebuild")
	contextIdx := findAnsibleTask(t, rebuildTasks, "Resolve Ceph rebuild host context")
	deviceIdx := findAnsibleTask(t, rebuildTasks, "Guard Ceph rebuild devices on every topology host")
	rmIdx := findAnsibleTask(t, rebuildTasks, "Remove existing cephadm cluster for override rebuild on every topology host")
	clearFSIDIdx := findAnsibleTask(t, rebuildTasks, "Clear authorized Ceph FSID state for override rebuild on every topology host")
	clearSeedIdx := findAnsibleTask(t, rebuildTasks, "Clear exact Ceph seed files for override rebuild")
	clearStagedIdx := findAnsibleTask(t, rebuildTasks, "Clear exact staged Ceph files for override rebuild on the seed host")
	finalizeIdx := findAnsibleTask(t, rebuildTasks, "Finalize Ceph cluster apply mode on the seed host")
	if !(classifyIdx < gatherIdx && gatherIdx < contextIdx && contextIdx < deviceIdx && deviceIdx < rmIdx && rmIdx < clearFSIDIdx && clearFSIDIdx < clearSeedIdx && clearSeedIdx < clearStagedIdx && clearStagedIdx < finalizeIdx) {
		t.Fatalf("rebuild must authorize on seed, gate every host, clean every host, then finalize seed state")
	}
	if got := fmt.Sprint(rebuildTasks[gatherIdx]["when"]); !strings.Contains(got, "bootwright_ceph_rebuild_cleanup_required") {
		t.Fatalf("non-seed OS facts must be gathered only for an authorized cluster-wide rebuild, got when=%v", rebuildTasks[gatherIdx]["when"])
	}
	if got := fmt.Sprint(rebuildTasks[classifyIdx]["when"]); !strings.Contains(got, "inventory_hostname") || !strings.Contains(got, "seedHost") {
		t.Fatalf("cluster ownership classification must remain seed-only, got when=%v", rebuildTasks[classifyIdx]["when"])
	}
	if got := fmt.Sprint(rebuildTasks[contextIdx]["ansible.builtin.include_tasks"]); got != "../destroy_steps/context.yml" {
		t.Fatalf("rebuild must reuse destroy host context gates, got %v", got)
	}
	if got := fmt.Sprint(rebuildTasks[deviceIdx]["ansible.builtin.include_tasks"]); got != "../destroy_steps/device_gates.yml" {
		t.Fatalf("rebuild must reuse destroy device gates, got %v", got)
	}
	for _, idx := range []int{contextIdx, deviceIdx, rmIdx, clearFSIDIdx} {
		when := fmt.Sprint(rebuildTasks[idx]["when"])
		if !strings.Contains(when, "hostvars[bootwright_selected_storage_cluster.seedHost]") || !strings.Contains(when, "bootwright_ceph_rebuild_cleanup_required") {
			t.Fatalf("all-host rebuild task %q must consume the seed authorization, got when=%v", rebuildTasks[idx]["name"], rebuildTasks[idx]["when"])
		}
	}
	rmArgv := fmt.Sprint(rebuildTasks[rmIdx]["ansible.builtin.command"])
	if !strings.Contains(rmArgv, "--zap-osds") || !strings.Contains(rmArgv, "hostvars[bootwright_selected_storage_cluster.seedHost].bootwright_ceph_override_fsid") {
		t.Fatalf("all-host rebuild must remove the seed-authorized fsid and zap local OSDs, got %v", rmArgv)
	}
	clearFSIDLoop := fmt.Sprint(rebuildTasks[clearFSIDIdx]["loop"])
	for _, path := range []string{"/var/lib/ceph/", "/var/log/ceph/"} {
		if !strings.Contains(clearFSIDLoop, path) || !strings.Contains(clearFSIDLoop, "hostvars[bootwright_selected_storage_cluster.seedHost].bootwright_ceph_override_fsid") {
			t.Fatalf("all-host rebuild cleanup must scope %s to the seed-authorized fsid, got %v", path, clearFSIDLoop)
		}
	}
	for _, idx := range []int{clearSeedIdx, clearStagedIdx} {
		when := fmt.Sprint(rebuildTasks[idx]["when"])
		if !strings.Contains(when, "inventory_hostname") || !strings.Contains(when, "seedHost") || !strings.Contains(when, "bootwright_ceph_rebuild_cleanup_required") {
			t.Fatalf("exact-file rebuild task %q must be seed-only and authorized, got when=%v", rebuildTasks[idx]["name"], rebuildTasks[idx]["when"])
		}
	}
	seedLoop := fmt.Sprint(rebuildTasks[clearSeedIdx]["loop"])
	for _, path := range []string{"/etc/ceph/ceph.conf", "/etc/ceph/ceph.client.admin.keyring", "/etc/ceph/ceph.pub", "bootwright_ceph_ownership_marker_path"} {
		if !strings.Contains(seedLoop, path) {
			t.Fatalf("seed rebuild cleanup must remove exact managed file %s, got %v", path, seedLoop)
		}
	}
	stagedLoop := fmt.Sprint(rebuildTasks[clearStagedIdx]["loop"])
	for _, path := range []string{"bootwright_ceph_remote_bootstrap_spec", "bootwright_ceph_remote_private_key", "bootwright_ceph_remote_registry_json", "management-services.yaml", "stretch-crushmap-new.bin"} {
		if !strings.Contains(stagedLoop, path) {
			t.Fatalf("seed rebuild cleanup must remove exact staged file %s, got %v", path, stagedLoop)
		}
	}
}

func TestStorageCephadmRebuildCleanupPreservesSharedCephRoots(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/rebuild.yml")
	for _, task := range tasks {
		file, ok := task["ansible.builtin.file"].(map[string]any)
		if !ok || file["state"] != "absent" {
			continue
		}
		paths := []any{file["path"]}
		if loop, ok := task["loop"].([]any); ok {
			paths = loop
		}
		for _, item := range paths {
			path := fmt.Sprint(item)
			for _, root := range []string{"/etc/ceph", "/var/lib/ceph", "/var/log/ceph", "{{ bootwright_ceph_remote_work_dir }}"} {
				if path == root {
					t.Fatalf("rebuild cleanup task %q must not recursively remove shared or staged root %s", task["name"], root)
				}
			}
			if (strings.HasPrefix(path, "/var/lib/ceph/") || strings.HasPrefix(path, "/var/log/ceph/")) && !strings.Contains(path, "bootwright_ceph_override_fsid") {
				t.Fatalf("rebuild cleanup task %q must scope Ceph state path %s to the authorized fsid", task["name"], path)
			}
		}
	}
}

func TestMachineRegistrationRoleRegistersSafely(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_registration_apply.yml")
	if len(plays) != 1 {
		t.Fatalf("machine registration playbook has %d plays, want 1", len(plays))
	}
	if got := plays[0]["hosts"]; got != "bootwright_storage_hosts" {
		t.Fatalf("machine registration play hosts = %v, want bootwright_storage_hosts", got)
	}
	tasks, ok := plays[0]["tasks"].([]any)
	if !ok {
		t.Fatalf("machine registration play has no tasks")
	}
	decoded := make([]map[string]any, 0, len(tasks))
	for i, task := range tasks {
		item, ok := task.(map[string]any)
		if !ok {
			t.Fatalf("machine registration task %d is not a map", i)
		}
		decoded = append(decoded, item)
	}
	contextAssert := decoded[findAnsibleTask(t, decoded, "Validate selected registration context")]
	if got := fmt.Sprint(contextAssert["ansible.builtin.assert"]); !strings.Contains(got, "rhsmManagement") || !strings.Contains(got, "managed") {
		t.Fatalf("registration play must refuse a non-managed RHSM context, got %v", got)
	}
	assertIncludeRoleName(t, decoded[findAnsibleTask(t, decoded, "Register machine with RHSM")], "bootwright.core.machine_registration_rhsm")

	mainTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_registration_rhsm/tasks/main.yml")
	validateIdx := findAnsibleTask(t, mainTasks, "Validate RHSM registration inputs")
	satelliteIdx := findAnsibleTask(t, mainTasks, "Bind node to corporate Satellite")
	proxyIdx := findAnsibleTask(t, mainTasks, "Configure RHSM proxy and content trust")
	registerIdx := findAnsibleTask(t, mainTasks, "Register node with RHSM")
	if !(validateIdx < satelliteIdx && satelliteIdx < proxyIdx && proxyIdx < registerIdx) {
		t.Fatalf("registration role must validate, bind Satellite, and converge proxy trust before registering (validate=%d satellite=%d proxy=%d register=%d)", validateIdx, satelliteIdx, proxyIdx, registerIdx)
	}

	registerTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_registration_rhsm/tasks/register.yml")
	rhsmRegister := registerTasks[findAnsibleTask(t, registerTasks, "Register node with RHSM")]
	assertRedactsByDefault(t, "RHSM registration", rhsmRegister["no_log"])

	proxyTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/machine_registration_rhsm/tasks/proxy.yml")
	repoCA := proxyTasks[findAnsibleTask(t, proxyTasks, "Point RHSM content CA at the proxy-aware trust store")]
	if got := fmt.Sprint(repoCA["community.general.ini_file"]); !strings.Contains(got, "repo_ca_cert") {
		t.Fatalf("registration proxy tasks must converge rhsm.conf repo_ca_cert, got %v", got)
	}
}

func TestStorageCephadmRoleKeepsSecretsAndArtifactsBounded(t *testing.T) {
	mainTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/main.yml")
	for _, name := range []string{
		"Prepare Ceph repository and subscription",
		"Install Ceph host dependencies",
		"Configure Ceph container registry access",
		"Install cephadm host tooling",
		"Bootstrap and converge Ceph cluster",
	} {
		task := mainTasks[findAnsibleTask(t, mainTasks, name)]
		if _, ok := task["ansible.builtin.include_tasks"]; !ok {
			t.Fatalf("storage role main task %q must include a phase file", name)
		}
	}

	varsTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/context_vars.yml")
	findAnsibleTask(t, varsTasks, "Resolve managed Ceph work paths")
	contextTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/context.yml")
	gatherIdx := findAnsibleTask(t, contextTasks, "Gather storage node OS facts")
	providerIdx := findAnsibleTask(t, contextTasks, "Resolve managed Ceph provider context")
	if gatherIdx >= providerIdx {
		t.Fatalf("host context must gather OS facts before provider templates are materialized")
	}
	providerTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/context_provider.yml")
	findAnsibleTask(t, providerTasks, "Resolve managed Ceph provider context")
	seedContextTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/context_seed.yml")
	seedGatherIdx := findAnsibleTask(t, seedContextTasks, "Gather seed OS facts for provider rendering")
	seedProviderIdx := findAnsibleTask(t, seedContextTasks, "Resolve managed Ceph provider context on the seed host")
	manifestIdx := findAnsibleTask(t, seedContextTasks, "Load the rendered Ceph operation manifest")
	operationsIdx := findAnsibleTask(t, seedContextTasks, "Resolve rendered Ceph operations, execution plan and batch scripts")
	if !(seedGatherIdx < seedProviderIdx && seedProviderIdx < manifestIdx && manifestIdx < operationsIdx) {
		t.Fatalf("seed context must gather OS facts before provider templates, then load bootstrap operations")
	}
	operationFacts, ok := seedContextTasks[operationsIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("rendered operation resolution must be a set_fact, got %v", seedContextTasks[operationsIdx])
	}
	for _, want := range []string{"bootwright_ceph_operations", "bootwright_ceph_plan", "bootwright_ceph_batch_files"} {
		if _, ok := operationFacts[want]; !ok {
			t.Fatalf("seed context must resolve %q from the rendered operation manifest, got %v", want, operationFacts)
		}
	}
	seedCredentials := seedContextTasks[findAnsibleTask(t, seedContextTasks, "Load cephadm registry credentials on the seed host")]
	if got := fmt.Sprint(seedCredentials["when"]); !strings.Contains(got, "is not defined") {
		t.Fatalf("seed context must not reload credentials already loaded by a combined prerequisite run, got when=%v", seedCredentials["when"])
	}
	hostContext := mainTasks[findAnsibleTask(t, mainTasks, "Prepare Ceph storage node context")]
	if got := fmt.Sprint(hostContext["when"]); !strings.Contains(got, "skip_prereqs") {
		t.Fatalf("ordinary seed-only convergence must not gather host facts or load registry credentials on non-seeds, got when=%v", hostContext["when"])
	}

	repositoryTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/repository.yml")
	osValidateIdx := findAnsibleTask(t, repositoryTasks, "Validate Ceph storage node OS family is implemented")
	osValidate, ok := repositoryTasks[osValidateIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("storage node OS validation must be an assert, got %v", repositoryTasks[osValidateIdx])
	}
	if got := fmt.Sprint(osValidate["that"]); !strings.Contains(got, "ansible_os_family == 'RedHat'") {
		t.Fatalf("storage node OS validation must gate on the implemented OS family, got that=%v", osValidate["that"])
	}
	for _, forbidden := range []string{"ansible_distribution_version", "ansible_distribution_major_version", "runtimeOS.exactVersions", "runtimeOS.majorVersions"} {
		if got := fmt.Sprint(osValidate["that"]); strings.Contains(got, forbidden) {
			t.Fatalf("storage node OS validation must not assert a vendor version-support matrix (%s), got that=%v", forbidden, osValidate["that"])
		}
	}
	for _, task := range repositoryTasks {
		if got := fmt.Sprint(task); strings.Contains(got, "runtimeOS.exactVersions") || strings.Contains(got, "runtimeOS.majorVersions") {
			t.Fatalf("the repository phase must carry no Ceph release/RHEL support matrix, got %v", task)
		}
	}
	dispatchIdx := findAnsibleTask(t, repositoryTasks, "Configure Ceph package repository for the rendered distribution")
	if !(osValidateIdx < dispatchIdx) {
		t.Fatalf("repository dispatch must run after the OS validation (validate=%d dispatch=%d)", osValidateIdx, dispatchIdx)
	}
	if got := fmt.Sprint(repositoryTasks[dispatchIdx]["ansible.builtin.include_tasks"]); !strings.Contains(got, "providers/{{ bootwright_ceph_provider_name }}.yml") {
		t.Fatalf("repository preparation must dispatch to the rendered distribution's provider task file, got %v", got)
	}
	for _, rel := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/oss.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/redhat.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/ibm.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/subscription.yml",
	} {
		if tasks := readAnsibleTasks(t, rel); len(tasks) == 0 {
			t.Fatalf("%s has no tasks", rel)
		}
	}
	for _, rel := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/redhat.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/ibm.yml",
	} {
		if body := readRepoFile(t, rel); !strings.Contains(body, "include_tasks: subscription.yml") {
			t.Fatalf("%s must compose the shared subscription task file", rel)
		}
	}

	communityTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/oss.yml")
	downloadIdx := findAnsibleTask(t, communityTasks, "Download cephadm utility to configure community Ceph repository")
	addRepoIdx := findAnsibleTask(t, communityTasks, "Configure community Ceph package repository through cephadm")
	if !(downloadIdx < addRepoIdx) {
		t.Fatalf("community provider must download cephadm before configuring the community repo")
	}
	download, ok := communityTasks[downloadIdx]["ansible.builtin.get_url"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(download["url"]), "bootwright_ceph_community_repo_path") {
		t.Fatalf("community cephadm download must build the release/version-scoped upstream URL, got %v", communityTasks[downloadIdx])
	}
	addRepo, ok := communityTasks[addRepoIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("community repo task must run a command, got %v", communityTasks[addRepoIdx])
	}
	if got := fmt.Sprint(addRepo["argv"]); !strings.Contains(got, "add-repo") || !strings.Contains(got, "--release") || !strings.Contains(got, "--version") {
		t.Fatalf("community repo task must run cephadm add-repo with both --release and --version arms, got %v", addRepo["argv"])
	}
	if got := fmt.Sprint(addRepo["argv"]); !strings.Contains(got, "--gpg-url") || strings.Contains(got, "--mirror") {
		t.Fatalf("community repo task must pass --gpg-url and never the unsupported --mirror flag, got %v", addRepo["argv"])
	}
	if got := fmt.Sprint(addRepo["argv"]); !strings.Contains(got, "--repo-url") {
		t.Fatalf("community repo task must pass custom mirrors through --repo-url, got %v", addRepo["argv"])
	}
	if got := fmt.Sprint(addRepo["creates"]); !strings.Contains(got, "bootwright_ceph_community_repo_file") {
		t.Fatalf("community repo task must be idempotent via creates, got %v", addRepo["creates"])
	}
	releaseKeyIdx := findAnsibleTask(t, communityTasks, "Trust the Ceph community release signing key")
	if !(releaseKeyIdx < addRepoIdx) {
		t.Fatalf("community provider must import the Ceph release key before configuring the community repo (key=%d addRepo=%d)", releaseKeyIdx, addRepoIdx)
	}
	releaseKey, ok := communityTasks[releaseKeyIdx]["ansible.builtin.rpm_key"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(releaseKey["fingerprint"]), "bootwright_ceph_community_release_gpg_fingerprint") {
		t.Fatalf("Ceph release key import must pin the release key fingerprint, got %v", communityTasks[releaseKeyIdx])
	}
	repoKeyHeal, ok := communityTasks[findAnsibleTask(t, communityTasks, "Point existing community Ceph repository at the release signing key")]["ansible.builtin.replace"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(repoKeyHeal["regexp"]), "gpgkey") {
		t.Fatalf("community repo gpgkey heal task must rewrite gpgkey lines in the existing repo file, got %v", repoKeyHeal)
	}
	communitySource := strings.Join([]string{
		readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/oss.yml"),
		readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/vars/os/RedHat.yml"),
	}, "\n")
	for _, forbidden := range []string{"mirror.stream.centos.org", "community_dependency_default_mirror", "ansible.builtin.yum_repository"} {
		if strings.Contains(communitySource, forbidden) {
			t.Fatalf("community Ceph setup must not expose unrestricted cross-distribution package source %q", forbidden)
		}
	}
	inspectRepoIdx := findAnsibleTask(t, communityTasks, "Inspect obsolete Bootwright community dependency repository")
	readRepoIdx := findAnsibleTask(t, communityTasks, "Read obsolete Bootwright community dependency repository")
	refuseRepoIdx := findAnsibleTask(t, communityTasks, "Refuse to remove an unrecognized community dependency repository")
	removeRepoIdx := findAnsibleTask(t, communityTasks, "Remove obsolete Bootwright community dependency repository")
	if !(inspectRepoIdx < readRepoIdx && readRepoIdx < refuseRepoIdx && refuseRepoIdx < removeRepoIdx) {
		t.Fatalf("obsolete dependency repo cleanup must inspect, read, verify, then remove")
	}
	removeRepo := communityTasks[findAnsibleTask(t, communityTasks, "Remove obsolete Bootwright community dependency repository")]
	removeRepoBody, ok := removeRepo["ansible.builtin.file"].(map[string]any)
	if !ok || removeRepoBody["state"] != "absent" || !strings.Contains(fmt.Sprint(removeRepoBody["path"]), "obsolete_dependency_repo_file") {
		t.Fatalf("community Ceph setup must remove the obsolete Bootwright dependency repo file, got %v", removeRepo)
	}
	refuseRepo := communityTasks[refuseRepoIdx]
	refuseRepoBody, ok := refuseRepo["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(refuseRepoBody["that"]), "isreg") || !strings.Contains(fmt.Sprint(refuseRepoBody["that"]), "islnk") || !strings.Contains(fmt.Sprint(refuseRepoBody["that"]), "difference") || !strings.Contains(fmt.Sprint(refuseRepo["vars"]), "bootwright-centos-stream-baseos") || !strings.Contains(fmt.Sprint(refuseRepo["vars"]), "bootwright-centos-stream-crb") {
		t.Fatalf("obsolete dependency repo cleanup must verify Bootwright section ownership, got %v", refuseRepo)
	}

	subscriptionTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/subscription.yml")
	repoEnableIdx := findAnsibleTask(t, subscriptionTasks, "Enable subscription-backed Ceph repositories (and disable the rest)")
	if got := fmt.Sprint(subscriptionTasks[repoEnableIdx]["when"]); !strings.Contains(got, "external") {
		t.Fatalf("subscription repo enablement must skip external rhsm management, got when=%v", got)
	}
	ibmTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/ibm.yml")
	licenseRequireIdx := findAnsibleTask(t, ibmTasks, "Require accepted license for licensed Ceph distribution")
	licenseAcceptIdx := findAnsibleTask(t, ibmTasks, "Accept vendor Ceph license provisions")
	if !(licenseRequireIdx < licenseAcceptIdx) {
		t.Fatalf("IBM provider must require license acceptance before accepting provisions (require=%d accept=%d)", licenseRequireIdx, licenseAcceptIdx)
	}
	if got := fmt.Sprint(ibmTasks[licenseAcceptIdx]["when"]); !strings.Contains(got, "requiresLicense") {
		t.Fatalf("license acceptance must gate on requiresLicense, got when=%v", got)
	}

	registryTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/registry.yml")
	registryLogin := registryTasks[findAnsibleTask(t, registryTasks, "Log storage node into cephadm registry")]
	assertRedactsByDefault(t, "registry login", registryLogin["no_log"])
	if _, ok := registryLogin["containers.podman.podman_login"].(map[string]any); !ok {
		t.Fatalf("registry login must use podman_login, got %v", registryLogin)
	}

	installTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml")

	dependencyTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/dependencies.yml")
	prereqs := dependencyTasks[findAnsibleTask(t, dependencyTasks, "Install Ceph prerequisites on storage node")]
	assertIncludeRoleName(t, prereqs, "bootwright.core.ownership_record")
	if got := fmt.Sprint(prereqs["ansible.builtin.include_role"]); !strings.Contains(got, "package_apply.yml") {
		t.Fatalf("Ceph prerequisite task must use package ownership helper, got %v", prereqs)
	}
	vars, ok := prereqs["vars"].(map[string]any)
	if !ok {
		t.Fatalf("Ceph prerequisite task missing vars: %v", prereqs)
	}
	if got := fmt.Sprint(vars["bootwright_ownership_packages"]); !strings.Contains(got, "bootwright_ceph_provider.prerequisitePackages") {
		t.Fatalf("Ceph prerequisite packages must come from provider projection, got %v", vars["bootwright_ownership_packages"])
	}
	if got := fmt.Sprint(prereqs["delegate_to"]); strings.Contains(got, "item.inventoryHost") || got != "<nil>" {
		t.Fatalf("Ceph prerequisites must run on the current storage host, got delegate_to %v", got)
	}

	firewalldProbeIdx := findAnsibleTask(t, dependencyTasks, "Probe host firewalld availability")
	firewalldProbe := dependencyTasks[firewalldProbeIdx]
	assertIncludeRoleName(t, firewalldProbe, "bootwright.core.machine_base")
	if got := fmt.Sprint(firewalldProbe["ansible.builtin.include_role"]); !strings.Contains(got, "firewalld_probe.yml") {
		t.Fatalf("firewalld probe task must use machine_base firewalld_probe.yml, got %v", firewalldProbe)
	}

	vrrpIdx := findAnsibleTask(t, dependencyTasks, "Allow VRRP so cephadm keepalived ingress daemons can negotiate a virtual IP")
	if firewalldProbeIdx >= vrrpIdx {
		t.Fatalf("VRRP firewalld allowance must run after the firewalld probe (probe=%d vrrp=%d)", firewalldProbeIdx, vrrpIdx)
	}
	if startIdx := findAnsibleTask(t, dependencyTasks, "Start storage node services"); startIdx >= firewalldProbeIdx {
		t.Fatalf("the firewalld probe must run after the node services are started; the probe reports unavailable unless firewalld is already running, so probing first silently drops the VRRP allowance for the whole run on a host where this apply just installed firewalld (start=%d probe=%d)", startIdx, firewalldProbeIdx)
	}
	vrrp := dependencyTasks[vrrpIdx]
	vrrpBody, ok := vrrp["ansible.posix.firewalld"].(map[string]any)
	if !ok {
		t.Fatalf("VRRP firewalld allowance must use ansible.posix.firewalld, got %v", vrrp)
	}
	if vrrpBody["protocol"] != "vrrp" || vrrpBody["state"] != "enabled" || vrrpBody["permanent"] != true || vrrpBody["immediate"] != true {
		t.Fatalf("VRRP firewalld allowance must permanently+immediately enable the vrrp protocol, got %v", vrrpBody)
	}
	if got := fmt.Sprint(vrrp["when"]); !strings.Contains(got, "bootwright_firewalld_available") {
		t.Fatalf("VRRP firewalld allowance must be gated on bootwright_firewalld_available, got when=%v", got)
	}

	startServicesIdx := findAnsibleTask(t, dependencyTasks, "Start storage node services")
	waitSyncIdx := findAnsibleTask(t, dependencyTasks, "Wait for storage node time synchronization")
	if startServicesIdx >= waitSyncIdx {
		t.Fatalf("time-sync wait must run after chronyd is started (start=%d wait=%d)", startServicesIdx, waitSyncIdx)
	}
	waitSync := dependencyTasks[waitSyncIdx]
	if got := waitSync["failed_when"]; got != false {
		t.Fatalf("time-sync wait must be non-fatal (failed_when: false), got %v", got)
	}
	if got := fmt.Sprint(waitSync["ansible.builtin.command"]); !strings.Contains(got, "waitsync") {
		t.Fatalf("time-sync wait must run chronyc waitsync, got %v", waitSync)
	}
	waitSyncBody, ok := waitSync["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("time-sync wait must use an argv command body, got %v", waitSync)
	}
	waitSyncArgv, ok := waitSyncBody["argv"].([]any)
	if !ok {
		t.Fatalf("time-sync wait must use argv, got %v", waitSyncBody)
	}
	wantWaitSyncArgv := []string{"chronyc", "waitsync", "30", "0", "0", "2"}
	if len(waitSyncArgv) != len(wantWaitSyncArgv) {
		t.Fatalf("time-sync wait argv = %v, want %v", waitSyncArgv, wantWaitSyncArgv)
	}
	for i, want := range wantWaitSyncArgv {
		if fmt.Sprint(waitSyncArgv[i]) != want {
			t.Fatalf("time-sync wait argv = %v, want %v", waitSyncArgv, wantWaitSyncArgv)
		}
	}
	if got := fmt.Sprint(waitSync["when"]); !strings.Contains(got, "chronyd") {
		t.Fatalf("time-sync wait must be gated on chronyd being a managed service, got when=%v", got)
	}

	initialProbeIdx := findAnsibleTask(t, installTasks, "Probe cephadm CLI before package fallback")
	cephadmPackageIdx := findAnsibleTask(t, installTasks, "Install cephadm package on storage node when not preinstalled")
	recordCephadmIdx := findAnsibleTask(t, installTasks, "Write cephadm package ownership record")
	verifyCephadmIdx := findAnsibleTask(t, installTasks, "Verify cephadm CLI on storage node")
	failCephadmIdx := findAnsibleTask(t, installTasks, "Fail when cephadm CLI is unavailable")
	if !(initialProbeIdx < cephadmPackageIdx && cephadmPackageIdx < recordCephadmIdx && recordCephadmIdx < verifyCephadmIdx && verifyCephadmIdx < failCephadmIdx) {
		t.Fatalf("Cephadm fallback must run after distro prereqs and before storage services/verification")
	}
	cephadmPackage := installTasks[cephadmPackageIdx]
	if got := cephadmPackage["failed_when"]; got != false {
		t.Fatalf("cephadm package fallback must not fail the package batch, got failed_when=%v", got)
	}
	cephadmPackageBody, ok := cephadmPackage["ansible.builtin.package"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(cephadmPackageBody["name"]), "bootwright_ceph_provider.cephadmPackage") {
		t.Fatalf("cephadm package fallback must install provider-selected cephadm package, got %v", cephadmPackage)
	}
	packageFactsIdx := findAnsibleTask(t, installTasks, "Gather installed package facts before pinning the cephadm build")
	pinnedInstallIdx := findAnsibleTask(t, installTasks, "Install the pinned cephadm build on storage node")
	if !(initialProbeIdx < packageFactsIdx && packageFactsIdx < pinnedInstallIdx && pinnedInstallIdx < recordCephadmIdx) {
		t.Fatalf("package facts must be gathered before the pinned cephadm install so package_records_write can tell a preexisting cephadm from one Bootwright installed; otherwise destroy removes the operator's package")
	}
	pinnedInstall := installTasks[pinnedInstallIdx]
	pinnedBody, ok := pinnedInstall["ansible.builtin.dnf"].(map[string]any)
	if !ok {
		t.Fatalf("the pinned cephadm install must use ansible.builtin.dnf; ansible.builtin.package cannot downgrade, so a pin below the installed build would fail on re-apply, got %v", pinnedInstall)
	}
	if pinnedBody["allow_downgrade"] != true {
		t.Fatalf("the pinned cephadm install must set allow_downgrade so a pin below the installed build converges, got %v", pinnedBody)
	}
	if _, ok := pinnedInstall["failed_when"]; ok {
		t.Fatalf("the pinned cephadm install must fail closed; an unavailable pinned build is an error, not a silent float back to whatever the repository ships, got %v", pinnedInstall)
	}
	if got := fmt.Sprint(pinnedBody["name"]); !strings.Contains(got, "cephadmPackageSpec") || strings.Contains(got, "~") || strings.Contains(got, "join") {
		t.Fatalf("the pinned install name must be the rendered cephadmPackageSpec verbatim, with no Jinja string composition, got %v", got)
	}
	recordName := fmt.Sprint(installTasks[recordCephadmIdx]["vars"])
	if !strings.Contains(recordName, "cephadmPackage") || strings.Contains(recordName, "cephadmPackageSpec") {
		t.Fatalf("the ownership record must key on the bare cephadm package name, never the versioned spec; the static destroy list looks up the bare name and would orphan a versioned record, got %v", recordName)
	}
	if body := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/vars/os/RedHat.yml"); !strings.Contains(body, "- cephadm\n") {
		t.Fatalf("bootwright_ceph_managed_packages must keep the bare cephadm entry so destroy still matches the ownership record")
	}
	assertIncludeRoleName(t, installTasks[recordCephadmIdx], "bootwright.core.ownership_record")
	if got := installTasks[verifyCephadmIdx]["failed_when"]; got != false {
		t.Fatalf("cephadm verify must leave failure handling to the targeted assert, got failed_when=%v", got)
	}
	failCephadm, ok := installTasks[failCephadmIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(failCephadm["fail_msg"]), "MachineInstallProfile") {
		t.Fatalf("cephadm unavailable assert must point to managed OS package ownership, got %v", failCephadm)
	}

	for _, path := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/stage_inputs.yml",
	} {
		if body := readRepoFile(t, path); strings.Contains(body, "ceph-common") || strings.Contains(body, "cephCommonPackage") {
			t.Fatalf("%s must not install host Ceph client tooling", path)
		}
	}

	block := storageCephBootstrapTasks(t)
	for _, name := range []string{
		"Copy cephadm registry JSON",
		"Copy cephadm cluster private SSH key",
		"Copy cephadm cluster public SSH key",
	} {
		task := block[findAnsibleTask(t, block, name)]
		assertRedactsByDefault(t, name, task["no_log"])
	}
	rebuildDecision := block[findAnsibleTask(t, block, "Decide whether this cluster requires an authorized override rebuild")]
	if got := fmt.Sprint(rebuildDecision["ansible.builtin.set_fact"]); !strings.Contains(got, "bootwright_apply_mode") || !strings.Contains(got, "override") {
		t.Fatalf("destructive cephadm rm-cluster must be authorized only in override apply mode, got decision=%v", rebuildDecision)
	}
	rmCluster := block[findAnsibleTask(t, block, "Remove existing cephadm cluster for override rebuild on every topology host")]
	if got := fmt.Sprint(rmCluster["when"]); !strings.Contains(got, "bootwright_ceph_rebuild_cleanup_required") {
		t.Fatalf("destructive cephadm rm-cluster must consume the authorized rebuild decision, got when=%v", rmCluster["when"])
	}
	if got := fmt.Sprint(rmCluster["ansible.builtin.command"]); !strings.Contains(got, "rm-cluster") {
		t.Fatalf("override rebuild must run cephadm rm-cluster, got %v", rmCluster)
	}
	bootstrapStep := block[findAnsibleTask(t, block, "Bootstrap Ceph cluster when absent")]
	if got := fmt.Sprint(bootstrapStep["when"]); !strings.Contains(got, "bootwright_ceph_conf_check.rc") || strings.Contains(got, "status_check") {
		t.Fatalf("bootstrap must be gated solely on ceph.conf presence, got when=%v", bootstrapStep["when"])
	}
	resolveBootstrap := block[findAnsibleTask(t, block, "Resolve cephadm bootstrap command")]
	bootstrapArgv := fmt.Sprint(resolveBootstrap["ansible.builtin.set_fact"])
	if !strings.Contains(bootstrapArgv, "--registry-json") || strings.Contains(bootstrapArgv, "--registry-password") || strings.Contains(bootstrapArgv, "--registry-username") {
		t.Fatalf("bootstrap argv must use registry JSON without username/password arguments, got %v", resolveBootstrap)
	}
	if !strings.Contains(bootstrapArgv, "--image") || !strings.Contains(bootstrapArgv, "bootwright_ceph_bootstrap_image") {
		t.Fatalf("bootstrap argv must conditionally pass --image from the rendered pin, got %v", resolveBootstrap)
	}
	if imageIdx, subcommandIdx := strings.Index(bootstrapArgv, "--image"), strings.Index(bootstrapArgv, "'bootstrap'"); imageIdx < 0 || subcommandIdx < 0 || imageIdx > subcommandIdx {
		t.Fatalf("bootstrap argv must place the global --image before the bootstrap subcommand, got %v", resolveBootstrap)
	}
	if !strings.Contains(bootstrapArgv, "--allow-fqdn-hostname") {
		t.Fatalf("bootstrap argv must pass --allow-fqdn-hostname (IBM recommended), got %v", resolveBootstrap)
	}
	if !strings.Contains(bootstrapArgv, "--dashboard-password-noupdate") {
		t.Fatalf("bootstrap argv must pass --dashboard-password-noupdate, got %v", resolveBootstrap)
	}
	if !strings.Contains(bootstrapArgv, "--single-host-defaults") || !strings.Contains(bootstrapArgv, "singleHostDefaults") {
		t.Fatalf("bootstrap argv must conditionally pass --single-host-defaults, got %v", resolveBootstrap)
	}
	if !strings.Contains(bootstrapArgv, "--automatically-accept-license") || !strings.Contains(bootstrapArgv, "requiresLicense") || !strings.Contains(bootstrapArgv, "license.accepted") {
		t.Fatalf("licensed bootstrap must non-interactively accept the declared license, got %v", resolveBootstrap)
	}
	callHomeTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/ibm_call_home.yml")
	inspectCallHome := callHomeTasks[findAnsibleTask(t, callHomeTasks, "Inspect IBM Call Home manager module state")]
	if got := fmt.Sprint(inspectCallHome["when"]); !strings.Contains(got, "ibm") || !strings.Contains(got, "callHome") {
		t.Fatalf("Call Home inspection must be IBM-only and require explicit intent, got when=%v", inspectCallHome["when"])
	}
	enableCallHome := callHomeTasks[findAnsibleTask(t, callHomeTasks, "Enable IBM Call Home manager module")]
	if got := fmt.Sprint(enableCallHome); !strings.Contains(got, "call_home_agent") || !strings.Contains(got, "enabled") {
		t.Fatalf("Call Home enable path must enable call_home_agent for enabled intent, got %v", enableCallHome)
	}
	acceptCallHome := callHomeTasks[findAnsibleTask(t, callHomeTasks, "Accept enabled IBM Call Home state")]
	if got := fmt.Sprint(acceptCallHome); !strings.Contains(got, "call-home-enabled") || !strings.Contains(got, "accept") {
		t.Fatalf("Call Home enable path must acknowledge the enabled state, got %v", acceptCallHome)
	}
	disableCallHome := callHomeTasks[findAnsibleTask(t, callHomeTasks, "Disable IBM Call Home manager module")]
	if got := fmt.Sprint(disableCallHome); !strings.Contains(got, "call-home-enabled") || !strings.Contains(got, "deny") || !strings.Contains(got, "disabled") {
		t.Fatalf("Call Home disable path must turn off the module for disabled intent, got %v", disableCallHome)
	}
	if got := fmt.Sprint(disableCallHome["when"]); strings.Contains(got, "bootwright_ceph_ibm_call_home_enabled") {
		t.Fatalf("Call Home opt-out must deny consent even when the module is already disabled, got when=%v", disableCallHome["when"])
	}
	coreIdx := findAnsibleTask(t, block, "Apply Ceph OSD service spec")
	topologyIdx := findAnsibleTask(t, block, "Run rendered Ceph topology and storage operations one container per operation")
	lateIdx := findAnsibleTask(t, block, "Apply Ceph late service spec")
	lateOpsIdx := findAnsibleTask(t, block, "Run rendered Ceph late operations one container per operation")
	if !(coreIdx < topologyIdx && topologyIdx < lateIdx && lateIdx < lateOpsIdx) {
		t.Fatalf("storage operations must be ordered core -> topology/storage -> late services -> late operations")
	}
	if got := fmt.Sprint(block[topologyIdx]["when"]); !strings.Contains(got, "topology") || !strings.Contains(got, "storage") {
		t.Fatalf("topology/storage operation loop has unexpected when=%v", block[topologyIdx]["when"])
	}
	if got := fmt.Sprint(block[lateOpsIdx]["when"]); !strings.Contains(got, "object-gateway") || !strings.Contains(got, "late-topology") {
		t.Fatalf("late operation loop has unexpected when=%v", block[lateOpsIdx]["when"])
	}
	if got := fmt.Sprint(block[lateOpsIdx]["when"]); !strings.Contains(got, "stretch-internal-pools") {
		t.Fatalf("late operation loop must admit the argv-less internal-pool reconcile, got when=%v", block[lateOpsIdx]["when"])
	}
	serviceReadyIdx := findAnsibleTask(t, block, "Wait for declared Ceph services to reach their daemon count")
	serviceAssertIdx := findAnsibleTask(t, block, "Assert declared Ceph services reached their daemon count")
	if !(lateIdx < serviceReadyIdx && serviceReadyIdx < serviceAssertIdx && serviceAssertIdx < lateOpsIdx) {
		t.Fatalf("declared services must be proven deployed after the late specs and before the late operations read what their daemons create")
	}
	serviceReady := block[serviceReadyIdx]
	for _, want := range []string{"status.running", "status.size", "map('max')", "rejectattr('service_type', 'equalto', 'osd')", "rejectattr('unmanaged', 'defined')", "bootwright_ceph_service_ls.attempts", "bootwright_ceph_service_readiness_retries"} {
		if got := fmt.Sprint(serviceReady["until"]); !strings.Contains(got, want) {
			t.Fatalf("service readiness wait must contain %q, got until=%v", want, serviceReady["until"])
		}
	}
	serviceAssert, ok := block[serviceAssertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(serviceAssert["that"]), "bootwright_ceph_services_ready") {
		t.Fatalf("service readiness must fail closed on the recorded verdict, got %v", block[serviceAssertIdx])
	}
	for _, want := range []string{"ceph orch ps", "cephadm"} {
		if got := fmt.Sprint(serviceAssert["fail_msg"]); !strings.Contains(got, want) {
			t.Fatalf("service readiness failure must report %q diagnostics, got fail_msg=%v", want, serviceAssert["fail_msg"])
		}
	}
	finalHealthIdx := findAnsibleTask(t, block, "Wait for final managed Ceph health")
	finalHealthAssertIdx := findAnsibleTask(t, block, "Refuse to complete an unreachable or unhealthy Ceph cluster")
	ownershipIdx := findAnsibleTask(t, block, "Record storage cluster ownership")
	if !(lateOpsIdx < finalHealthIdx && finalHealthIdx < finalHealthAssertIdx && finalHealthAssertIdx < ownershipIdx) {
		t.Fatalf("final health must be proven after late operations and before recording successful ownership")
	}
	if got := fmt.Sprint(block[finalHealthIdx]["until"]); !strings.Contains(got, "HEALTH_ERR") || !strings.Contains(got, "bootwright_ceph_final_health.rc") {
		t.Fatalf("final health wait must require a reachable cluster without HEALTH_ERR, got until=%v", block[finalHealthIdx]["until"])
	}

	cleanup := block[findAnsibleTask(t, block, "Remove managed Ceph work directory")]
	fileTask, ok := cleanup["ansible.builtin.file"].(map[string]any)
	if !ok || fileTask["state"] != "absent" {
		t.Fatalf("storage role must clean the remote work directory, got %v", cleanup)
	}

	operationTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/execute.yml")
	run := operationTasks[findAnsibleTask(t, operationTasks, "Run Ceph operation")]
	command, ok := run["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("Run Ceph operation has no command body")
	}
	if got := command["argv"]; got != "{{ ['cephadm', 'shell', '--'] + bootwright_ceph_op_command }}" {
		t.Fatalf("Run Ceph operation must consume rendered argv inside cephadm shell, got %v", got)
	}
	if _, ok := run["changed_when"]; !ok {
		t.Fatalf("Run Ceph operation must declare changed_when")
	}
	if _, ok := run["no_log"]; !ok {
		t.Fatalf("Run Ceph operation must redact captured output")
	}
}

func storageCephBootstrapTasks(t *testing.T) []map[string]any {
	t.Helper()
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/"
	destroyBase := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/"
	return readAnsibleTasksFromFiles(t,
		base+"apply_mode.yml",
		destroyBase+"context.yml",
		destroyBase+"device_gates.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/rebuild.yml",
		base+"apply_mode_finalize.yml",
		base+"stage_inputs.yml",
		base+"bootstrap_cluster.yml",
		base+"ownership_marker.yml",
		base+"ibm_call_home.yml",
		base+"container_image_base.yml",
		base+"network_config.yml",
		base+"registry_login.yml",
		base+"dashboard_secret.yml",
		base+"service_specs.yml",
		base+"batch_support.yml",
		base+"topology_operations.yml",
		base+"late_service_specs.yml",
		base+"management_services.yml",
		base+"service_readiness.yml",
		base+"late_operations.yml",
		base+"result_and_ownership.yml",
		base+"cleanup.yml",
	)
}

func storageCephDestroyTasks(t *testing.T) []map[string]any {
	t.Helper()
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/"
	return readAnsibleTasksFromFiles(t,
		base+"context.yml",
		base+"device_gates.yml",
		base+"cluster_gate.yml",
		base+"wipe_and_cleanup.yml",
		base+"filter_device_reclaim.yml",
	)
}

func TestStorageCephBootstrapSpecApplyIsVerified(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/service_specs.yml"
	specs := readRepoFile(t, path)
	for _, want := range []string{
		"bootwright_ceph_host_spec_apply.stdout",
		"bootwright_ceph_host_spec_apply.stderr",
		"orch\n      - host\n      - ls",
		"bootwright_ceph_declared_mon_hosts",
		"bootwright_ceph_known_orch_hosts",
	} {
		if !strings.Contains(specs, want) {
			t.Errorf("%s must retain %q: `ceph orch apply -i` can reject one document of a multi-document spec and still exit zero, leaving cephadm's default mon placement in force", path, want)
		}
	}
	readiness := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/mon_readiness.yml")
	if !strings.Contains(readiness, "bare\n      `count:`") && !strings.Contains(readiness, "a bare\n      `count:`") {
		t.Error("the monmap diagnosis must tell the operator to read the live mon placement first; a bare count: placement means the declared set was never requested, and none of the three network/image/port causes applies")
	}
}

func TestStorageCephDestroyReclaimsFilterSelectedDisks(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/filter_device_reclaim.yml"
	reclaim := readRepoFile(t, path)
	for _, want := range []string{
		"osdReclaimAll",
		"ceph_bluestore",
		"^ceph-",
		"findmnt",
		"MOUNTPOINT",
	} {
		if !strings.Contains(reclaim, want) {
			t.Errorf("%s must retain destroy-time reclaim bound %q; without it a filter-declared OSD host keeps its bluestore signatures through teardown", path, want)
		}
	}
	chain := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy.yml")
	if !strings.Contains(chain, "destroy_steps/filter_device_reclaim.yml") {
		t.Error("the Ceph destroy chain must import filter_device_reclaim.yml; a filter-declared OSD host names no device path, so the declared-device wipe covers none of its disks")
	}
	wipe := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/wipe_and_cleanup.yml")
	if !strings.Contains(wipe, "bootwright_ceph_zap_tool_needed") {
		t.Error("the declared-device wipe must resolve the zap-tool need from both declared devices and osdReclaimAll, or sgdisk is missing on a filter-declared host")
	}
}

func TestStorageCephadmAllDevicesReclaimSafetyGates(t *testing.T) {
	reclaimPath := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_reclaim.yml"
	reclaim := readRepoFile(t, reclaimPath)
	for _, want := range []string{
		"selectattr('osdReclaimAll', 'defined')",
		"bootwright_ceph_filter_reclaim_authorized",
		"bootwright_ceph_filter_reclaim_ready",
		"rejectattr('osd_ids', 'equalto', [])",
		"ignore_unreachable: true",
	} {
		if !strings.Contains(reclaim, want) {
			t.Errorf("%s must retain reclaim safety gate %q", reclaimPath, want)
		}
	}
	if !strings.Contains(reclaim, "- zap") || !strings.Contains(reclaim, "- --force") {
		t.Errorf("%s must wipe candidates via `ceph orch device zap ... --force`", reclaimPath)
	}
	reclaimTop := readAnsibleTasks(t, reclaimPath)
	reclaimBlockIdx := findAnsibleTask(t, reclaimTop, "Auto-reclaim dirty filter-selected OSD devices before the OSD apply")
	reclaimTasks := nestedAnsibleTasks(t, reclaimTop[reclaimBlockIdx], "block")
	refreshIdx := findAnsibleTask(t, reclaimTasks, "Refresh cephadm device inventory for filter-OSD hosts")
	if until := fmt.Sprint(reclaimTasks[refreshIdx]["until"]); !strings.Contains(until, "bootwright_ceph_filter_device_ls.attempts") {
		t.Errorf("%s must let the inventory poll terminate at its attempt budget: ansible marks an exhausted `until` failed regardless of `failed_when: false` (task_executor sets result['failed']=True after the retry loop), so without the escape an authorized reclaim aborts the play before the OSD spec is applied. got until=%v", reclaimPath, until)
	}
	if findAnsibleTaskIndex(reclaimTasks, "Report filter-OSD hosts whose device inventory never appeared") < 0 {
		t.Errorf("%s must report the hosts whose inventory never arrived, so a partial reclaim is not silent", reclaimPath)
	}
	svc := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/service_specs.yml")
	reclaimIdx := strings.Index(svc, "osd_reclaim.yml")
	coreIdx := strings.Index(svc, "/mnt/core-services.yaml")
	if reclaimIdx < 0 || coreIdx < 0 || reclaimIdx > coreIdx {
		t.Error("service_specs.yml must include osd_reclaim.yml before the core-services (OSD) apply")
	}
}

func TestStorageCephadmAllDevicesCoverageReportIsNonDestructive(t *testing.T) {
	coveragePath := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_coverage_report.yml"
	coverage := readRepoFile(t, coveragePath)
	for _, want := range []string{
		"selectattr('osdReclaimAll', 'defined')",
		"bootwright_ceph_coverage_ready",
		"rejectattr('osd_ids', 'equalto', [])",
		"ignore_unreachable: true",
		"MOUNTPOINT",
		"bootwright_ceph_coverage_residual",
		"ansible.builtin.debug",
	} {
		if !strings.Contains(coverage, want) {
			t.Errorf("%s must retain coverage-report element %q", coveragePath, want)
		}
	}
	for _, forbidden := range []string{"\n          - zap", "\n          - --force", "state: absent"} {
		if strings.Contains(coverage, forbidden) {
			t.Errorf("%s must stay non-destructive (found %q); the coverage report only reads and warns", coveragePath, forbidden)
		}
	}
	coverageTop := readAnsibleTasks(t, coveragePath)
	coverageBlockIdx := findAnsibleTask(t, coverageTop, "Report all-devices OSD disks left unclaimed after the OSD apply")
	coverageTasks := nestedAnsibleTasks(t, coverageTop[coverageBlockIdx], "block")
	coverageRefreshIdx := findAnsibleTask(t, coverageTasks, "Refresh cephadm device inventory for the OSD coverage report")
	if until := fmt.Sprint(coverageTasks[coverageRefreshIdx]["until"]); !strings.Contains(until, "bootwright_ceph_coverage_device_ls.attempts") {
		t.Errorf("%s must let the inventory poll terminate at its attempt budget: `failed_when: false` does not survive retry exhaustion, so a read-only report would otherwise fail an apply whose OSDs are healthy. got until=%v", coveragePath, until)
	}

	boot := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap.yml")
	readinessIdx := strings.Index(boot, "osd_readiness.yml")
	coverageIdx := strings.Index(boot, "osd_coverage_report.yml")
	if readinessIdx < 0 || coverageIdx < 0 || readinessIdx > coverageIdx {
		t.Error("bootstrap.yml must include osd_coverage_report.yml after osd_readiness.yml")
	}
}

func TestStorageOSDReadinessFailureNamesTheReclaimRemedy(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_readiness.yml"
	tasks := readAnsibleTasks(t, path)

	resolve, ok := tasks[findAnsibleTask(t, tasks, "Resolve OSD readiness expectation")]["ansible.builtin.set_fact"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(resolve["bootwright_ceph_osd_reclaim_all_hosts"]), "osdReclaimAll") {
		t.Fatalf("OSD readiness must resolve the all-devices hosts from the rendered osdReclaimAll flag, got %v", resolve)
	}

	remedyIdx := findAnsibleTask(t, tasks, "Compose the OSD readiness remedy for the declared device selection")
	remedyVars, ok := tasks[remedyIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("the readiness remedy must be composed from vars, got %v", tasks[remedyIdx])
	}
	allDevices := fmt.Sprint(remedyVars["bootwright_ceph_osd_remedy_reclaim_all"])
	for _, want := range []string{"data_devices.all=true", "--mode rebuild", "--authorize data-loss", "IRREVERSIBLE", "protectedKinds"} {
		if !strings.Contains(allDevices, want) {
			t.Errorf("the all:true readiness remedy must name %q — it is the only automated reclaim Bootwright implements for a filter-authored host, and the coverage report that also names it never runs when the readiness assert aborts the play; got %v", want, allDevices)
		}
	}
	manual := fmt.Sprint(remedyVars["bootwright_ceph_osd_remedy_manual"])
	for _, want := range []string{"--reclaim-devices", "wipefs --all"} {
		if !strings.Contains(manual, want) {
			t.Errorf("the non-all:true readiness remedy must name %q, got %v", want, manual)
		}
	}
	if strings.Contains(manual, "--mode rebuild") {
		t.Errorf("the non-all:true readiness remedy must not offer --mode rebuild: the auto-reclaim covers all:true only, and narrowing filters are deliberately excluded, got %v", manual)
	}
	if strings.Contains(allDevices, "--reclaim-devices") {
		t.Errorf("the all:true readiness remedy must not offer --reclaim-devices: an all:true selection declares no static path, so the CLI rejects every named device with exit 2, got %v", allDevices)
	}

	summaryIdx := findAnsibleTask(t, tasks, "Summarise declared OSD device availability for the readiness failure")
	summaryVars, ok := tasks[summaryIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("the availability summary must be composed from vars, got %v", tasks[summaryIdx])
	}
	if got := fmt.Sprint(summaryVars["bootwright_ceph_osd_declared_devices"]); !strings.Contains(got, "default('[]', true)") {
		t.Errorf("the availability summary parses command stdout eagerly, so it must guard from_json against an empty (rc!=0) stdout with default('[]', true), got %v", got)
	}
	if findAnsibleTaskIndex(tasks, "Collect machine-readable device availability when OSDs did not become ready") < 0 {
		t.Fatal("the readiness failure must collect `ceph orch device ls --format json`: the --wide table carries no per-device availability the message can count")
	}

	failMsg := fmt.Sprint(tasks[findAnsibleTask(t, tasks, "Assert declared Ceph OSDs were created")]["ansible.builtin.assert"].(map[string]any)["fail_msg"])
	for _, want := range []string{"bootwright_ceph_osd_availability_summary", "bootwright_ceph_osd_readiness_remedy"} {
		if !strings.Contains(failMsg, want) {
			t.Errorf("the OSD readiness failure must render %q, got fail_msg=%v", want, failMsg)
		}
	}
}

func TestStorageCephSeedSSHCheckIsOneFailClosedSweep(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/stage_inputs.yml"
	tasks := readAnsibleTasks(t, path)
	checkIdx := findAnsibleTask(t, tasks, "Verify cephadm seed SSH access to storage nodes")
	requireIdx := findAnsibleTask(t, tasks, "Require cephadm seed SSH access to every selected storage node")
	if checkIdx >= requireIdx {
		t.Fatalf("the seed SSH sweep must run before its gate (check=%d require=%d)", checkIdx, requireIdx)
	}
	check := tasks[checkIdx]
	if _, ok := check["loop"]; ok {
		t.Fatalf("the seed SSH check must be one module execution over every address, got loop=%v", check["loop"])
	}
	shell, ok := check["ansible.builtin.shell"].(map[string]any)
	if !ok {
		t.Fatalf("the seed SSH check must be a shell sweep, got %v", check)
	}
	cmd := fmt.Sprint(shell["cmd"])
	for _, want := range []string{
		"bootwright_ceph_seed_ssh_addresses",
		"ssh -F",
		">/dev/null 2>&1",
		"exit 1",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("the seed SSH sweep is missing %q: %v", want, shell["cmd"])
		}
	}
	if strings.Contains(cmd, "${#") {
		t.Fatalf("the seed SSH sweep uses Bash syntax that Ansible parses as a Jinja comment: %v", shell["cmd"])
	}
	assertRedactsByDefault(t, "Verify cephadm seed SSH access to storage nodes", check["no_log"])
	if got := check["failed_when"]; got != false {
		t.Fatalf("the seed SSH sweep must defer failure to the gate so no_log cannot swallow the failing addresses, got failed_when=%v", got)
	}
	whenClauses := fmt.Sprint(check["when"])
	for _, want := range []string{"privateKeyPath", "knownHostsPath", "bootwright_ceph_seed_ssh_addresses"} {
		if !strings.Contains(whenClauses, want) {
			t.Fatalf("the seed SSH sweep when is missing %q: %v", want, check["when"])
		}
	}
	require, ok := tasks[requireIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the seed SSH gate must be an assert, got %v", tasks[requireIdx])
	}
	if got := fmt.Sprint(require["that"]); !strings.Contains(got, "bootwright_ceph_seed_ssh_check.rc") || !strings.Contains(got, "default(1)") {
		t.Fatalf("the seed SSH gate must fail closed on a missing or non-zero rc, got that=%v", require["that"])
	}
	if got := fmt.Sprint(require["fail_msg"]); !strings.Contains(got, "stdout_lines") {
		t.Fatalf("the seed SSH gate must name the unreachable addresses the sweep collected, got fail_msg=%v", require["fail_msg"])
	}
	if _, ok := tasks[requireIdx]["no_log"]; ok {
		t.Fatal("the seed SSH gate must stay visible; only the sweep that carries ssh output is redacted")
	}
	if got := fmt.Sprint(tasks[requireIdx]["when"]); !strings.Contains(got, "bootwright_ceph_seed_ssh_check.skipped") {
		t.Fatalf("the seed SSH gate must run whenever the sweep ran, got when=%v", tasks[requireIdx]["when"])
	}
}

func TestStorageCephRGWIngressTLSAppliesOneMultiDocumentSpec(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/rgw_ingress_tls.yml"
	tasks := readAnsibleTasks(t, path)
	assembleIdx := findAnsibleTask(t, tasks, "Assemble RGW ingress TLS certificate specs")
	writeIdx := findAnsibleTask(t, tasks, "Write RGW ingress TLS certificate spec")
	applyIdx := findAnsibleTask(t, tasks, "Apply RGW ingress TLS certificate spec")
	if !(assembleIdx < writeIdx && writeIdx < applyIdx) {
		t.Fatalf("RGW ingress TLS must assemble, write, then apply (assemble=%d write=%d apply=%d)", assembleIdx, writeIdx, applyIdx)
	}
	assemble := tasks[assembleIdx]
	assertRedactsByDefault(t, "Assemble RGW ingress TLS certificate specs", assemble["no_log"])
	if got := fmt.Sprint(assemble["ansible.builtin.set_fact"]); !strings.Contains(got, "ssl_cert") || !strings.Contains(got, "item.serviceID") {
		t.Fatalf("RGW ingress TLS assembly must keep the concatenated ssl_cert and the rendered service id, got %v", assemble["ansible.builtin.set_fact"])
	}
	write := tasks[writeIdx]
	assertRedactsByDefault(t, "Write RGW ingress TLS certificate spec", write["no_log"])
	if _, ok := write["loop"]; ok {
		t.Fatalf("RGW ingress TLS must write one multi-document spec file, got loop=%v", write["loop"])
	}
	copyBody, ok := write["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("RGW ingress TLS spec must be written with copy, got %v", write)
	}
	if got := fmt.Sprint(copyBody["dest"]); !strings.HasSuffix(got, "/rgw-ingress-tls.yaml") {
		t.Fatalf("RGW ingress TLS spec file must be a single fixed path, got %v", copyBody["dest"])
	}
	if got := copyBody["mode"]; got != "0600" {
		t.Fatalf("RGW ingress TLS spec file carries a cert and key, so it must stay 0600, got %v", got)
	}
	apply := tasks[applyIdx]
	if _, ok := apply["loop"]; ok {
		t.Fatalf("RGW ingress TLS must run one ceph orch apply, got loop=%v", apply["loop"])
	}
	if got := fmt.Sprint(apply["ansible.builtin.command"]); !strings.Contains(got, "/mnt/rgw-ingress-tls.yaml") {
		t.Fatalf("RGW ingress TLS apply must consume the merged spec file, got %v", apply["ansible.builtin.command"])
	}
}

func flattenNodeAccessTasks(t *testing.T, tasks []map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, task := range tasks {
		out = append(out, task)
		for _, key := range []string{"block", "rescue", "always"} {
			if _, ok := task[key].([]any); ok {
				out = append(out, flattenNodeAccessTasks(t, nestedAnsibleTasks(t, task, key))...)
			}
		}
	}
	return out
}

func TestStorageNodeAccessConnectsWithTheKeyItAuthorizes(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/"

	authorize := readAnsibleTasks(t, base+"authorize.yml")
	idx := findAnsibleTask(t, authorize, "Authorize the machine access key for the storage node orchestration account")
	authorized := fmt.Sprint(authorize[idx]["vars"])
	if !strings.Contains(authorized, "bootwright_node_access_public_key") {
		t.Fatalf("the orchestration account must be authorized with the cluster key Bootwright resolved, got vars=%v", authorize[idx]["vars"])
	}

	context := readAnsibleTasks(t, base+"context.yml")
	argvIdx := findAnsibleTask(t, context, "Resolve storage node access SSH options")
	argv := fmt.Sprint(context[argvIdx]["ansible.builtin.set_fact"])
	if !strings.Contains(argv, "accountPrivateKeyPath") {
		t.Fatalf("the ssh argv must offer the private half of the key authorize.yml authorizes: every proof made as the orchestration account (probe.yml, verify.yml, the re-proof in revoke.yml) otherwise presents only the machine key, and sshd refuses the login with publickey before sudo is ever consulted, got %v", context[argvIdx]["ansible.builtin.set_fact"])
	}
	if !strings.Contains(argv, "installPrivateKeyPath") {
		t.Fatalf("the ssh argv must still offer the machine key for the install-window identity, got %v", context[argvIdx]["ansible.builtin.set_fact"])
	}

	public := fmt.Sprint(context[findAnsibleTask(t, context, "Resolve storage node access endpoints")]["ansible.builtin.set_fact"])
	if !strings.Contains(public, "accountPublicKeyPath") {
		t.Fatalf("the authorized key must come from accountPublicKeyPath, the public half of the same Secret as accountPrivateKeyPath, got %v", public)
	}
}

func TestStorageNodeAccessProvesPasswordlessSudoWithoutATerminal(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/"
	for _, proof := range []struct{ file, task string }{
		{"probe.yml", "Probe the storage node orchestration account"},
		{"verify.yml", "Verify passwordless sudo for the storage node orchestration account"},
		{"revoke.yml", "Verify the storage node orchestration account after revoking root SSH"},
	} {
		tasks := readAnsibleTasks(t, base+proof.file)
		idx := findAnsibleTask(t, tasks, proof.task)
		argv := fmt.Sprint(tasks[idx]["ansible.builtin.command"])
		if !strings.Contains(argv, "bootwright_node_access_ssh_argv") {
			t.Fatalf("%s%s task %q must prove sudo over bootwright_node_access_ssh_argv, got %s", base, proof.file, proof.task, argv)
		}
		if !strings.Contains(argv, "sudo -n true") {
			t.Fatalf("%s%s task %q must prove sudo with the literal sudo -n true, got %s", base, proof.file, proof.task, argv)
		}
		if strings.Contains(argv, "privileged_argv") || strings.Contains(argv, "tty") {
			t.Fatalf("%s%s task %q must never gain a pseudo-terminal: it is the acceptance test for the terminal-free channel cephadm's manager uses, so proving it under -tt would certify a cluster that cannot be orchestrated, got %s", base, proof.file, proof.task, argv)
		}
	}
}

func TestStorageNodeAccessPrivilegedCommandsUseThePrivilegedInvocation(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/"
	exempt := map[string]bool{
		"Probe privileged execution on the storage node without a terminal":                 true,
		"Probe privileged execution on the storage node with a terminal":                    true,
		"Probe privileged execution with a password on the storage node without a terminal": true,
		"Probe privileged execution with a password on the storage node with a terminal":    true,
	}
	for _, file := range []string{"account.yml", "sudoers.yml", "authorize.yml", "verify.yml", "revoke.yml", "restore.yml", "marker.yml", "privilege.yml"} {
		for _, task := range flattenNodeAccessTasks(t, readAnsibleTasks(t, base+file)) {
			cmd, ok := task["ansible.builtin.command"].(map[string]any)
			vars := fmt.Sprint(task["vars"])
			if !ok || !(strings.Contains(vars, "bootwright_node_access_sudo") || strings.Contains(vars, "bootwright_node_access_askpass_sudo")) {
				continue
			}
			name := fmt.Sprint(task["name"])
			if exempt[name] {
				continue
			}
			if !strings.Contains(fmt.Sprint(cmd["argv"]), "bootwright_node_access_privileged_argv") {
				t.Fatalf("%s%s task %q escalates with bootwright_node_access_sudo but not over bootwright_node_access_privileged_argv, so it fails on a node whose sudoers sets requiretty; argv=%v", base, file, name, cmd["argv"])
			}
		}
	}
}

func TestStorageNodeAccessAnswersASudoPasswordWithoutExposingIt(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/"
	privilege := readAnsibleTasks(t, base+"privilege.yml")

	installIdx := findAnsibleTask(t, privilege, "Install the storage node sudo askpass helper for the borrowed identity")
	install := privilege[installIdx]
	cmd, ok := install["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatal("the askpass helper task must run a command")
	}
	stdin := fmt.Sprint(cmd["stdin"])
	if !strings.Contains(stdin, "sudoPasswordEnv") {
		t.Fatalf("the askpass helper must receive the password on stdin from the environment the CLI set, got stdin=%v", cmd["stdin"])
	}
	if strings.Contains(fmt.Sprint(install["vars"]), "sudoPasswordEnv") {
		t.Fatalf("the password must never reach the remote command string: it becomes the argv of the node's shell and any local account can read it from ps, got vars=%v", install["vars"])
	}
	if install["no_log"] != true {
		t.Fatal("the askpass helper task must set no_log, or the operator password is written to the terminal and the run log")
	}
	if !strings.Contains(fmt.Sprint(cmd["argv"]), "bootwright_node_access_ssh_argv") {
		t.Fatalf("the askpass helper must be written over the terminal-free argv: a pseudo-terminal never delivers EOF, so the cat that receives the password would never return, got argv=%v", cmd["argv"])
	}

	for _, name := range []string{
		"Probe privileged execution with a password on the storage node without a terminal",
		"Probe privileged execution with a password on the storage node with a terminal",
	} {
		idx := findAnsibleTask(t, privilege, name)
		if idx < installIdx {
			t.Fatalf("task %q runs before the askpass helper exists (probe=%d install=%d)", name, idx, installIdx)
		}
	}

	command := fmt.Sprint(install["vars"])
	if !strings.Contains(command, "mktemp -d") {
		t.Fatalf("the helper directory must be created by the node with mktemp: a path Bootwright picks may be one the borrowed account cannot write, and a predictable one in a shared temporary directory can be pre-created by another local account that would then read the password out of it, got vars=%v", install["vars"])
	}
	for _, candidate := range []string{`"${XDG_RUNTIME_DIR:-}"`, "/dev/shm"} {
		if !strings.Contains(command, candidate) {
			t.Fatalf("the helper must prefer memory-backed storage (%s) over any disk: a password written to a home directory or /tmp survives in backups, snapshots and forensic images long after the run, got vars=%v", candidate, install["vars"])
		}
	}
	passwordIdx := strings.Index(command, `cat > "$d/pw"`)
	markerIdx := strings.Index(command, "bootwright_node_access_askpass_marker")
	watchdogIdx := strings.Index(command, "bootwright_node_access_askpass_ttl")
	if passwordIdx < 0 || markerIdx < 0 || markerIdx > passwordIdx {
		t.Fatalf("the node must report the directory it chose before the password is written into it, or a chain that fails midway leaves the password somewhere Bootwright never learned the name of, got vars=%v", install["vars"])
	}
	if watchdogIdx < 0 || watchdogIdx > passwordIdx {
		t.Fatalf("the node must arm its own timed removal before the password is written, not after: every guarantee that depends on this run finishing is void if the controller is killed, loses the network, or crashes, got vars=%v", install["vars"])
	}
	if !strings.Contains(command, "nohup") || !strings.Contains(command, "rm -rf") {
		t.Fatalf("the timed removal must outlive the ssh session that armed it, got vars=%v", install["vars"])
	}
	if !strings.Contains(command, `"$c/ask" >/dev/null 2>&1`) {
		t.Fatalf("each candidate directory must be proved executable with an empty helper before the password is written into it, or a noexec mount surfaces much later as a password that looks wrong, got vars=%v", install["vars"])
	}
	if !strings.Contains(command, `: > "$c/pw"`) || strings.Index(command, `: > "$c/pw"`) > passwordIdx {
		t.Fatalf("the password file must be created empty under umask 077 before it holds anything, so it is never briefly readable, got vars=%v", install["vars"])
	}

	resolveIdx := findAnsibleTask(t, privilege, "Resolve the storage node sudo askpass helper the borrowed identity created")
	if resolveIdx < installIdx {
		t.Fatal("the helper directory is whatever the node reported, so it can only be resolved after the install ran")
	}
	resolved := fmt.Sprint(privilege[resolveIdx]["ansible.builtin.set_fact"])
	if !strings.Contains(resolved, "regex_search") {
		t.Fatalf("the directory must be extracted from a delimited marker: a login profile that prints on a non-interactive shell shares that stdout, got %v", privilege[resolveIdx]["ansible.builtin.set_fact"])
	}
	if got := fmt.Sprint(privilege[resolveIdx]["when"]); !strings.Contains(got, "skipped") {
		t.Fatalf("the directory must be resolved whenever the install ran, including when it failed, or the always section cannot remove a password the node already holds, got when=%v", privilege[resolveIdx]["when"])
	}

	main := readAnsibleTasks(t, base+"main.yml")
	always, ok := main[0]["always"].([]any)
	if !ok || len(always) == 0 {
		t.Fatal("the node access block must carry an always section, or a failed run leaves the operator password on the node")
	}
	removal := fmt.Sprint(always)
	if !strings.Contains(removal, "bootwright_node_access_askpass_dir") || !strings.Contains(removal, "rm -rf") {
		t.Fatalf("the always section must remove the askpass helper directory holding the password, got %v", always)
	}
	if !strings.Contains(removal, "bootwright_node_access_askpass_dir | quote") {
		t.Fatalf("the helper path is whatever mktemp returned under a directory Bootwright does not control, so it must be quoted for the node's shell, got %v", always)
	}
	if !strings.Contains(removal, "test ! -e") {
		t.Fatalf("removing the password must be verified, not attempted: rm reports success for a path it could not even look at, got %v", always)
	}
	if !strings.Contains(removal, "Require the storage node sudo askpass helper to be gone") {
		t.Fatalf("a removal that could not be confirmed must fail the run, not pass quietly, got %v", always)
	}
	if !strings.Contains(removal, "ansible_failed_task is not defined") {
		t.Fatalf("the removal assert must stand down when the block already failed, or it replaces the diagnosis the operator needs with a cleanup error, got %v", always)
	}
}

func TestStorageNodeAccessSeparatesNoPasswordFromAnUndeliveredOne(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/"
	privilege := readAnsibleTasks(t, base+"privilege.yml")
	assertIdx := findAnsibleTask(t, privilege, "Require privileged execution on the storage node before provisioning")
	assertion, ok := privilege[assertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatal("the privilege gate must be an assert")
	}
	msg := fmt.Sprint(assertion["fail_msg"])
	if !strings.Contains(msg, "sudoPasswordEnv") {
		t.Fatalf("the refusal must decide whether a password existed for this machine from bootwright_node_access.sudoPasswordEnv, not from whether the helper install succeeded: a helper that failed to install reports as a password never collected and sends the operator to a flag they already passed, got %s", msg)
	}
	if !strings.Contains(msg, "sudoPasswordOffered") {
		t.Fatalf("the refusal must tell an operator who passed --ssh-ask-sudo-password that it does not reach a machine which declares its own login, rather than asking them to pass it again, got %s", msg)
	}
	if !strings.Contains(msg, "bootwright_node_access_askpass_install.rc") {
		t.Fatalf("the refusal must report the return code of the helper install: the task is no_log, so a failure there produces no other output anywhere in the run, got %s", msg)
	}
}

func TestStorageNodeAccessDetectsTheTerminalRequirementBeforeProvisioning(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/"
	main := readAnsibleTasks(t, base+"main.yml")
	block := nestedAnsibleTasks(t, main[0], "block")
	contextIdx := findAnsibleTask(t, block, "Resolve storage node access context")
	probeIdx := findAnsibleTask(t, block, "Probe storage node access identities")
	privilegeIdx := findAnsibleTask(t, block, "Resolve storage node privileged execution")
	accountIdx := findAnsibleTask(t, block, "Ensure the storage node orchestration account")
	if !(contextIdx < probeIdx && probeIdx < privilegeIdx && privilegeIdx < accountIdx) {
		t.Fatalf("node access must resolve privileged execution after selecting an identity and before creating the account, so a node that cannot escalate is refused with nothing mutated (context=%d probe=%d privilege=%d account=%d)", contextIdx, probeIdx, privilegeIdx, accountIdx)
	}

	privilege := readAnsibleTasks(t, base+"privilege.yml")
	ttyIdx := findAnsibleTask(t, privilege, "Probe privileged execution on the storage node with a terminal")
	if got := fmt.Sprint(privilege[ttyIdx]["when"]); !strings.Contains(got, "bootwright_node_access_privilege_probe.rc") {
		t.Fatalf("the terminal probe must run only after the terminal-free probe failed; allocating a pseudo-terminal unconditionally breaks a node whose sshd sets PermitTTY no and answers today, got when=%v", privilege[ttyIdx]["when"])
	}
	requireIdx := findAnsibleTask(t, privilege, "Require privileged execution on the storage node before provisioning")
	if requireIdx < ttyIdx {
		t.Fatalf("node access must refuse only after both probes ran (require=%d tty=%d)", requireIdx, ttyIdx)
	}

	verify := readAnsibleTasks(t, base+"verify.yml")
	switchIdx := findAnsibleTask(t, verify, "Switch to the storage node orchestration identity")
	if got := fmt.Sprint(verify[switchIdx]["ansible.builtin.set_fact"]); !strings.Contains(got, "bootwright_node_access_privileged_argv") {
		t.Fatalf("switching to the orchestration identity must reset the privileged invocation to the terminal-free argv, or a terminal borrowed for the install identity leaks into the account Bootwright hands to cephadm, got %v", verify[switchIdx]["ansible.builtin.set_fact"])
	}
}

func TestStorageNodeAccessSudoersGrantIsComparedOnTheNode(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/"
	tasks := readAnsibleTasks(t, base+"sudoers.yml")
	probeIdx := findAnsibleTask(t, tasks, "Probe the storage node orchestration sudoers grant")
	if got := fmt.Sprint(tasks[probeIdx]["vars"]); !strings.Contains(got, "test \"$(") {
		t.Fatalf("the sudoers grant must be compared on the node: Ansible's Jinja does not unescape string literals, so join('\\n') in a folded scalar joins with a literal backslash-n and never matches the node's real newline, and a pseudo-terminal would inject CR into any stdout compared on the controller; got vars=%v", tasks[probeIdx]["vars"])
	}
	reconcileIdx := findAnsibleTask(t, tasks, "Reconcile the storage node orchestration sudoers grant")
	when := fmt.Sprint(tasks[reconcileIdx]["when"])
	if !strings.Contains(when, "bootwright_node_access_sudoers_probe.rc") {
		t.Fatalf("the sudoers reconcile must key off the on-node comparison's exit status, got when=%v", tasks[reconcileIdx]["when"])
	}
	if strings.Contains(when, "stdout") {
		t.Fatalf("the sudoers reconcile must not compare stdout on the controller, or it re-installs the grant with changed_when true on every apply, got when=%v", tasks[reconcileIdx]["when"])
	}
}
