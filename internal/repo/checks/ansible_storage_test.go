package repocheck

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/storage/cephprovider"
)

func TestStorageNodeAccessDestroySelectsAReachableIdentity(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_node_access_destroy.yml"
	plays := readAnsiblePlays(t, path)
	if len(plays) != 1 {
		t.Fatalf("%s has %d plays, want 1", path, len(plays))
	}
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	selectIdx := findAnsibleTask(t, tasks, "Select storage node teardown connection")
	pingIdx := findAnsibleTask(t, tasks, "Probe node reachability and escalation before revoking node access")
	recordIdx := findAnsibleTask(t, tasks, "Record an unreachable node when no storage identity answers")
	revokeIdx := findAnsibleTask(t, tasks, "Revoke storage node orchestration access")
	if !(selectIdx < pingIdx && pingIdx < recordIdx && recordIdx < revokeIdx) {
		t.Fatalf("node-access destroy must select a reachable identity, verify escalation, then revoke (select=%d ping=%d record=%d revoke=%d)", selectIdx, pingIdx, recordIdx, revokeIdx)
	}
	include, ok := tasks[selectIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok || include["name"] != "bootwright.core.storage_node_access" || include["tasks_from"] != "select_connection.yml" {
		t.Fatalf("node-access destroy must use the shared teardown connection selector, got %v", tasks[selectIdx])
	}
	if got := fmt.Sprint(tasks[pingIdx]["when"]); !strings.Contains(got, "bootwright_node_access_connection_available") {
		t.Fatalf("node-access destroy must not run a second SSH probe when neither identity answers, got when=%v", tasks[pingIdx]["when"])
	}
	if got := fmt.Sprint(tasks[recordIdx]["when"]); !strings.Contains(got, "bootwright_node_access_connection_available") {
		t.Fatalf("node-access destroy must record an unavailable managed identity as unreachable, got when=%v", tasks[recordIdx]["when"])
	}
}

func TestStorageNodeAccessDestroyEndsOnlyANodeItProvesAbsent(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_node_access_destroy.yml"
	plays := readAnsiblePlays(t, path)
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	classifyIdx := findAnsibleTask(t, tasks, "Classify how a node refused its node access teardown connection")
	absentIdx := findAnsibleTask(t, tasks, "End nodes this node access teardown proved absent")
	warnIdx := findAnsibleTask(t, tasks, "Warn that a node Bootwright could not reach keeps its authorized access")
	endIdx := findAnsibleTask(t, tasks, "End nodes Bootwright cannot reach or escalate on")
	revokeIdx := findAnsibleTask(t, tasks, "Revoke storage node orchestration access")
	if !(classifyIdx < absentIdx && absentIdx < warnIdx && warnIdx < endIdx && endIdx < revokeIdx) {
		t.Fatalf("node-access destroy must classify absence, end the nodes it proved absent, then warn before it ends the rest (classify=%d absent=%d warn=%d end=%d revoke=%d)", classifyIdx, absentIdx, warnIdx, endIdx, revokeIdx)
	}
	include, ok := tasks[classifyIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok || include["name"] != "bootwright.core.storage_node_access" || include["tasks_from"] != "classify_absence.yml" {
		t.Fatalf("node-access destroy must classify through the shared absence classifier, not a private copy, got %v", tasks[classifyIdx])
	}
	when := fmt.Sprint(tasks[absentIdx]["when"])
	if !strings.Contains(when, "bootwright_node_access_node_absent") {
		t.Errorf("the silent end_host must read proven absence, got when=%v", tasks[absentIdx]["when"])
	}
	if strings.Contains(when, "bootwright_node_access_connection_available") {
		t.Errorf("an identity refusal is not absence: ending the host on the availability flag revokes nothing and leaves the cephadm cluster key authorized while the run reports success, got when=%v", tasks[absentIdx]["when"])
	}
	if _, isEnd := tasks[endIdx]["ansible.builtin.meta"]; !isEnd {
		t.Fatalf("the unproven-absence path must still end the host, got %v", tasks[endIdx])
	}
	warn := fmt.Sprint(tasks[warnIdx]["ansible.builtin.debug"])
	for _, want := range []string{"could not prove the node absent", "cephadm cluster key", "stay authorized"} {
		if !strings.Contains(warn, want) {
			t.Errorf("the skip warning must name the residue it leaves; missing %q in %v", want, tasks[warnIdx]["ansible.builtin.debug"])
		}
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

func TestStorageNodeAccessDestroyToleratesMissingInstallAccount(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/revoke_node_access.yml"
	tasks := readAnsibleTasks(t, path)
	probeIdx := findAnsibleTask(t, tasks, "Probe the storage node install account before reauthorizing its key")
	recordIdx := findAnsibleTask(t, tasks, "Record whether the storage node install identity can be restored")
	restoreIdx := findAnsibleTask(t, tasks, "Reauthorize the machine access key for the storage node install account")
	noteIdx := findAnsibleTask(t, tasks, "Note the storage node install identity Bootwright could not restore")
	if !(probeIdx < recordIdx && recordIdx < restoreIdx && restoreIdx < noteIdx) {
		t.Fatalf("node-access destroy must probe the install account and record the verdict before restoring its key (probe=%d record=%d restore=%d note=%d)", probeIdx, recordIdx, restoreIdx, noteIdx)
	}
	probe, ok := tasks[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok || fmt.Sprint(probe["argv"]) != "[getent passwd {{ bootwright_node_access_install_account }}]" {
		t.Fatalf("node-access destroy must probe the resolved install account, got %v", tasks[probeIdx])
	}
	if tasks[probeIdx]["changed_when"] != false || tasks[probeIdx]["failed_when"] != false {
		t.Fatalf("missing install account probe must be a read-only tolerated absence, got %v", tasks[probeIdx])
	}
	restore, ok := tasks[restoreIdx]["ansible.posix.authorized_key"].(map[string]any)
	if !ok || restore["user"] != "{{ bootwright_node_access_install_account }}" {
		t.Fatalf("node-access destroy must restore the key on the account apply took it from, got %v", tasks[restoreIdx])
	}
	narrowing := []string{
		"Reauthorize the machine access key for the storage node install account",
		"Deauthorize the machine access key for the storage node orchestration account",
		"Deauthorize the cephadm cluster key for the storage node orchestration account",
		"Remove the storage node access marker",
		"Remove the storage node orchestration sudoers grant",
	}
	for _, name := range narrowing {
		idx := findAnsibleTask(t, tasks, name)
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "bootwright_node_access_destroy_install_present") {
			t.Fatalf("%q must be gated on a restorable install identity: with no account to hand the node back to, removing the orchestration account's keys and sudo grant leaves the node reachable only from its console; got when=%v", name, tasks[idx]["when"])
		}
	}
	if got := fmt.Sprint(tasks[noteIdx]["when"]); !strings.Contains(got, "not (bootwright_node_access_destroy_install_present") {
		t.Fatalf("node-access destroy must report the access it deliberately left behind, got when=%v", tasks[noteIdx]["when"])
	}
	machineKeyIdx := findAnsibleTask(t, tasks, "Deauthorize the machine access key for the storage node orchestration account")
	if got := fmt.Sprint(tasks[machineKeyIdx]["when"]); !strings.Contains(got, "not (bootwright_node_access.installIdentity") {
		t.Fatalf("the machine access key must survive on an orchestration account that IS the install identity (ADR 0019): apply removed it there, this task restores it two steps earlier, and removing it again with the cluster key empties the only account's authorized_keys; got when=%v", tasks[machineKeyIdx]["when"])
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
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/container_runtime.yml",
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
	var scanned int
	for _, path := range paths {
		for _, block := range ansibleArgvBlocks(path, readRepoFile(t, path)) {
			argv := ansibleBoundedCommand(block.argv)
			if len(argv) == 0 {
				continue
			}
			scanned++
			if argv[0] == "ceph" || argv[0] == "radosgw-admin" {
				t.Fatalf("%s:%d invokes host-installed %s instead of cephadm shell; a `timeout --kill-after=<n> <secs>` wrapper in front of it does not make it a cephadm shell invocation", path, block.line+1, argv[0])
			}
		}
	}
	if scanned == 0 {
		t.Fatalf("no argv block was inspected across %d managed Ceph task files: the scanner no longer matches the tasks it guards", len(paths))
	}
	execute := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/operations/execute.yml")
	for _, want := range []string{"'timeout'", "bootwright_ceph_timeout_kill_after_seconds", "bootwright_ceph_orchestration_timeout_seconds", "'cephadm', 'shell', '--'", "+ bootwright_ceph_op_command"} {
		if !strings.Contains(execute, want) {
			t.Fatalf("rendered Ceph operations must run inside a bounded cephadm shell, missing %q", want)
		}
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
	if probe["changed_when"] != false {
		t.Fatalf("the interpreter preflight must be a read-only probe, got changed_when=%v", probe["changed_when"])
	}
	assertCephProbeTimeoutOnlyFailure(t, probe, "bootwright_ceph_batch_probe", "the interpreter preflight")
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
	assertCephMutationTimeoutOnlyFailure(t, run, "bootwright_ceph_batch_run", "the rendered operation batch")
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

func TestStoragePreflightRefusesPackageModeCephDaemonRPMs(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/check_storage_preflight/tasks/package_mode_daemons.yml"
	tasks := readAnsibleTasks(t, path)

	inspect := tasks[findAnsibleTask(t, tasks, "Inspect installed package facts for package-mode Ceph daemons")]
	block := nestedAnsibleTasks(t, inspect, "block")
	probe := block[findAnsibleTask(t, block, "Gather installed storage node package facts")]
	packageFacts, ok := probe["ansible.builtin.package_facts"].(map[string]any)
	if !ok || packageFacts["manager"] != "auto" {
		t.Fatalf("package-mode daemon residue must be discovered from live package facts, got %v", probe)
	}
	if probe["changed_when"] != false {
		t.Fatalf("the package fact probe must be read-only, got changed_when=%v", probe["changed_when"])
	}
	for _, forbidden := range []string{"failed_when", "ignore_errors", "ignore_unreachable", "run_once", "delegate_to"} {
		if value, found := probe[forbidden]; found {
			t.Fatalf("the package fact probe must fail closed on each selected host, but sets %s=%v", forbidden, value)
		}
	}
	conclusive := block[findAnsibleTask(t, block, "Require a conclusive storage node package inventory")]
	assert, ok := conclusive["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("a successful package-facts call must still prove it returned a package mapping, got %v", conclusive)
	}
	that := fmt.Sprint(assert["that"])
	for _, want := range []string{"ansible_facts.packages is defined", "ansible_facts.packages is mapping"} {
		if !strings.Contains(that, want) {
			t.Fatalf("the package inventory proof is missing %q, got %v", want, assert["that"])
		}
	}
	rescue := nestedAnsibleTasks(t, inspect, "rescue")
	probeFailure := rescue[findAnsibleTask(t, rescue, "Refuse storage mutation when package facts cannot be read")]
	if _, ok := probeFailure["ansible.builtin.fail"].(map[string]any); !ok {
		t.Fatalf("an inconclusive package probe must end the host instead of continuing, got %v", probeFailure)
	}
	probeFailureMessage := fmt.Sprint(probeFailure["ansible.builtin.fail"])
	for _, want := range []string{"ansible_failed_result.msg", "before any Ceph mutation", "bootwright_mutating_invocation", "same preflight"} {
		if !strings.Contains(probeFailureMessage, want) {
			t.Fatalf("the package probe refusal is missing %q, got %v", want, probeFailure["ansible.builtin.fail"])
		}
	}

	resolve := tasks[findAnsibleTask(t, tasks, "Resolve installed package-mode Ceph daemon RPMs")]
	facts, ok := resolve["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("installed package-mode daemon names must be resolved from package facts, got %v", resolve)
	}
	expr := fmt.Sprint(facts["bootwright_preflight_package_mode_ceph_daemons"])
	for _, name := range []string{"ceph-mds", "ceph-mgr", "ceph-mon", "ceph-osd", "ceph-radosgw", "rbd-mirror"} {
		if strings.Count(expr, "'"+name+"'") != 1 {
			t.Fatalf("package-mode daemon classifier must contain %s exactly once, got %s", name, expr)
		}
	}
	for _, want := range []string{"select('in', ansible_facts.packages)", "| list"} {
		if !strings.Contains(expr, want) {
			t.Fatalf("package-mode daemon classifier must derive the installed subset at runtime (missing %q), got %s", want, expr)
		}
	}

	refuse := tasks[findAnsibleTask(t, tasks, "Refuse pre-existing package-mode Ceph daemon RPMs")]
	assert, ok = refuse["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("installed package-mode daemons must trip an assertion, got %v", refuse)
	}
	if got := fmt.Sprint(assert["that"]); !strings.Contains(got, "bootwright_preflight_package_mode_ceph_daemons | length == 0") {
		t.Fatalf("the package-mode daemon gate must refuse every non-empty discovered set, got %v", assert["that"])
	}
	message := fmt.Sprint(assert["fail_msg"])
	for _, want := range []string{
		"bootwright_preflight_package_mode_ceph_daemons | join(', ')",
		"dnf remove {{ bootwright_preflight_package_mode_ceph_daemons | join(' ') }}",
		"will not uninstall",
		"bootwright_mutating_invocation",
		"same preflight",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("the package-mode daemon refusal is missing %q, got %s", want, message)
		}
	}
	for _, task := range append(append(block, rescue...), tasks[1:]...) {
		for _, mutator := range []string{"ansible.builtin.package", "ansible.builtin.dnf", "ansible.builtin.yum", "ansible.builtin.command", "ansible.builtin.shell"} {
			if _, found := task[mutator]; found {
				t.Fatalf("package-mode residue detection must never mutate or uninstall packages, but task %q uses %s", task["name"], mutator)
			}
		}
	}
}

func TestStoragePackageModeDaemonGatePrecedesMutationOnEverySelectedHost(t *testing.T) {
	preflightPath := "ansible/collections/ansible_collections/bootwright/core/roles/check_storage_preflight/tasks/main.yml"
	preflightTasks := readAnsibleTasks(t, preflightPath)
	resolveIdx := findAnsibleTask(t, preflightTasks, "Resolve storage nodes on this host")
	gateIdx := findAnsibleTask(t, preflightTasks, "Check for package-mode Ceph daemon residue")
	systemdIdx := findAnsibleTask(t, preflightTasks, "Check storage node systemd")
	if !(resolveIdx < gateIdx && gateIdx < systemdIdx) {
		t.Fatalf("storage preflight must select this host's nodes, run the package gate, then continue with later checks (resolve=%d gate=%d systemd=%d)", resolveIdx, gateIdx, systemdIdx)
	}
	assertIncludeTasksFile(t, preflightTasks[gateIdx], "package_mode_daemons.yml")
	if got := fmt.Sprint(preflightTasks[gateIdx]["when"]); !strings.Contains(got, "bootwright_host_storage_nodes | length > 0") {
		t.Fatalf("standalone preflight must run the package gate only on selected storage hosts, got when=%v", preflightTasks[gateIdx]["when"])
	}

	rolePath := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/main.yml"
	roleTasks := readAnsibleTasks(t, rolePath)
	applyGateIdx := findAnsibleTask(t, roleTasks, "Check package-mode Ceph daemon residue before storage mutation")
	nodeIdentityIdx := findAnsibleTask(t, roleTasks, "Resolve and gate Ceph storage node identity")
	repositoryIdx := findAnsibleTask(t, roleTasks, "Prepare Ceph repository and subscription")
	rebuildIdx := findAnsibleTask(t, roleTasks, "Guard and clean an authorized Ceph cluster rebuild")
	bootstrapIdx := findAnsibleTask(t, roleTasks, "Bootstrap and converge Ceph cluster")
	if !(applyGateIdx < nodeIdentityIdx && applyGateIdx < repositoryIdx && applyGateIdx < rebuildIdx && applyGateIdx < bootstrapIdx) {
		t.Fatalf("the package-mode daemon gate must precede every Ceph host or cluster mutation (gate=%d identity=%d repository=%d rebuild=%d bootstrap=%d)", applyGateIdx, nodeIdentityIdx, repositoryIdx, rebuildIdx, bootstrapIdx)
	}
	include, ok := roleTasks[applyGateIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok || include["name"] != "bootwright.core.check_storage_preflight" || include["tasks_from"] != "package_mode_daemons.yml" {
		t.Fatalf("apply must reuse the exact storage-preflight package gate, got %v", roleTasks[applyGateIdx])
	}
	for _, flag := range []string{"bootwright_task_storage_skip_prereqs", "bootwright_task_storage_prereqs_only"} {
		if strings.Contains(fmt.Sprint(roleTasks[applyGateIdx]["when"]), flag) {
			t.Fatalf("the package gate must run in both storage apply halves, but is conditional on %s", flag)
		}
	}

	nodeAccessPath := "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_node_access_apply.yml"
	nodeAccessPlays := readAnsiblePlays(t, nodeAccessPath)
	if len(nodeAccessPlays) != 1 {
		t.Fatalf("%s has %d plays, want 1", nodeAccessPath, len(nodeAccessPlays))
	}
	nodeAccessPlay := nodeAccessPlays[0]
	if nodeAccessPlay["hosts"] != "bootwright_storage_hosts" || nodeAccessPlay["strategy"] != "linear" || nodeAccessPlay["any_errors_fatal"] != true {
		t.Fatalf("the node-access gate requires the selected storage group under linear any-errors-fatal execution, got hosts=%v strategy=%v any_errors_fatal=%v", nodeAccessPlay["hosts"], nodeAccessPlay["strategy"], nodeAccessPlay["any_errors_fatal"])
	}
	nodeAccessTasks := nestedAnsibleTasks(t, nodeAccessPlay, "tasks")
	nodeAccessSelectIdx := findAnsibleTask(t, nodeAccessTasks, "End hosts outside selected storage cluster")
	nodeAccessGateIdx := findAnsibleTask(t, nodeAccessTasks, "Check package-mode Ceph daemon residue before node access mutation")
	nodeAccessApplyIdx := findAnsibleTask(t, nodeAccessTasks, "Apply Ceph storage node access")
	if !(nodeAccessSelectIdx < nodeAccessGateIdx && nodeAccessGateIdx < nodeAccessApplyIdx) {
		t.Fatalf("storage node selection and the package gate must precede orchestration-account mutation (select=%d gate=%d apply=%d)", nodeAccessSelectIdx, nodeAccessGateIdx, nodeAccessApplyIdx)
	}
	nodeAccessInclude, ok := nodeAccessTasks[nodeAccessGateIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok || nodeAccessInclude["name"] != "bootwright.core.check_storage_preflight" || nodeAccessInclude["tasks_from"] != "package_mode_daemons.yml" {
		t.Fatalf("node-access apply must reuse the exact storage-preflight package gate, got %v", nodeAccessTasks[nodeAccessGateIdx])
	}

	playbookPath := "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_apply.yml"
	plays := readAnsiblePlays(t, playbookPath)
	if len(plays) != 1 {
		t.Fatalf("%s has %d plays, want 1", playbookPath, len(plays))
	}
	play := plays[0]
	if play["hosts"] != "bootwright_storage_hosts" || play["strategy"] != "linear" || play["any_errors_fatal"] != true {
		t.Fatalf("the cross-host gate requires the selected storage group under linear any-errors-fatal execution, got hosts=%v strategy=%v any_errors_fatal=%v", play["hosts"], play["strategy"], play["any_errors_fatal"])
	}
	playTasks := nestedAnsibleTasks(t, play, "tasks")
	selectIdx := findAnsibleTask(t, playTasks, "End hosts outside selected storage cluster")
	applyIdx := findAnsibleTask(t, playTasks, "Apply Ceph storage cluster")
	if selectIdx >= applyIdx {
		t.Fatalf("hosts outside the selected storage cluster must end before the role containing the package gate runs (select=%d apply=%d)", selectIdx, applyIdx)
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
	for _, want := range []string{"num_in_osds", "bootwright_ceph_osd_readiness_mode == 'exact'", "bootwright_ceph_osd_expected_count", "bootwright_ceph_osd_stat.attempts", "bootwright_ceph_osd_readiness_attempts", "bootwright_ceph_osd_readiness_deadline"} {
		if !strings.Contains(globalUntil, want) {
			t.Fatalf("global OSD readiness must preserve exact static-path behavior %q, got %v", want, globalUntil)
		}
	}
	if strings.Count(globalUntil, "bootwright_ceph_osd_expected_count") < 2 {
		t.Fatalf("global OSD readiness must enforce expectedCount for exact and dynamic selections, got %v", globalUntil)
	}
	for _, want := range []string{"bootwright_ceph_osd_stat.rc", "124", "137"} {
		if !strings.Contains(globalUntil, want) {
			t.Fatalf("global OSD readiness must stop retrying on a finite command timeout so rc 124/137 fails closed before the sampled stdout is trusted, missing %q in %v", want, globalUntil)
		}
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
	if strings.Contains(ready, "bootwright_ceph_osd_stat.rc") {
		t.Fatalf("the OSD readiness verdict must not require the final sample's exit status, got %v", ready)
	}
	globalAssert, ok := tasks[globalAssertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(globalAssert["that"]), "bootwright_ceph_osd_ready") {
		t.Fatalf("global OSD readiness must fail through the evaluated readiness fact after diagnostics, got %v", tasks[globalAssertIdx])
	}
	if msg := fmt.Sprint(globalAssert["fail_msg"]); strings.Contains(msg, "from_json).num_in_osds") {
		t.Fatalf("the readiness failure message must render on partial stdout; from_json on the raw sample explodes the assert instead of reporting, got %v", msg)
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
	for _, want := range []string{"type', 'equalto', 'osd", "reweight', 'gt', 0", "type', 'equalto', 'host", "name', 'in', bootwright_ceph_osd_dynamic_crush_names", "map('intersect'", "map('length')", "select('gt', 0)", "bootwright_ceph_osd_dynamic_hosts | length", "bootwright_ceph_osd_host_tree.attempts", "bootwright_ceph_osd_readiness_attempts", "bootwright_ceph_osd_readiness_deadline"} {
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
	if perHost["changed_when"] != false {
		t.Fatalf("dynamic-host OSD readiness must be a read-only retry probe, got changed_when=%v", perHost["changed_when"])
	}
	assertCephProbeTimeoutOnlyFailure(t, perHost, "bootwright_ceph_osd_host_tree", "dynamic-host OSD readiness")

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
	treeDoc := fmt.Sprint(dynamicVars["bootwright_ceph_osd_tree_doc"])
	if !strings.Contains(treeDoc, "from_json") || !strings.Contains(treeDoc, "regex_search") {
		t.Fatalf("dynamic-host OSD assertion fail_msg parses the CRUSH tree eagerly, so from_json must be reached only for a snapshot that terminates: an empty (rc!=0) OR half-written stdout must yield a default instead of exploding the assert, got %v", treeDoc)
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
	if got := fmt.Sprint(refuse["fail_msg"]); !strings.Contains(got, "bootwright_apply_rebuild_invocation") {
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
	if got := fmt.Sprint(fsRefuse["fail_msg"]); !strings.Contains(got, "bootwright_apply_rebuild_invocation") {
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
	if got := fmt.Sprint(refuse["fail_msg"]); !strings.Contains(got, "bootwright_apply_rebuild_invocation") {
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
	assertCephMutationTimeoutOnlyFailure(t, apply, "bootwright_ceph_host_spec_apply", "the bootstrap spec apply")

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
	outer := tasks[idx]
	step := cephCommandTask(t, outer, "registry login")
	if got := fmt.Sprint(step["ansible.builtin.command"]); !strings.Contains(got, "registry-login") || !strings.Contains(got, "bootwright_ceph_remote_work_dir") || !strings.Contains(got, "/mnt/registry-login.json") {
		t.Fatalf("registry login must mount its staged JSON into cephadm shell, got %v", step["ansible.builtin.command"])
	}
	if got := fmt.Sprint(outer["when"]); !strings.Contains(got, "bootwright_ceph_registry_credentials is defined") {
		t.Fatalf("registry login must be gated on resolved credentials, got when=%v", outer["when"])
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

func TestStorageCephContainerRuntimeIsProvenBeforeAnyClusterWork(t *testing.T) {
	main := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/main.yml")
	context := strings.Index(main, "phases/context_vars.yml")
	image := strings.Index(main, "Require a Ceph image for the container-runtime safety proof")
	gate := strings.Index(main, "phases/container_runtime.yml")
	rebuild := strings.Index(main, "phases/rebuild.yml")
	bootstrap := strings.Index(main, "phases/bootstrap.yml")
	endHost := strings.Index(main, "end_host")
	if context < 0 || image < 0 || gate < 0 || rebuild < 0 || bootstrap < 0 || endHost < 0 {
		t.Fatalf("the role is missing a step this ordering depends on (context=%d image=%d gate=%d rebuild=%d bootstrap=%d endHost=%d)", context, image, gate, rebuild, bootstrap, endHost)
	}
	if !(context < image && image < gate && gate < rebuild && gate < bootstrap) {
		t.Fatalf("the provider image must be resolved and required before the runtime proof, and the proof must precede rebuild and bootstrap (context=%d image=%d gate=%d rebuild=%d bootstrap=%d)", context, image, gate, rebuild, bootstrap)
	}
	if gate > endHost {
		t.Fatalf("the container runtime gate must run before non-seed hosts leave the play, or it only ever proves the seed (gate=%d endHost=%d)", gate, endHost)
	}
	if strings.Contains(main, "phases/context_provider.yml") {
		t.Fatalf("the role must not materialize the full provider before distribution facts exist; only its image scalar is needed by the early runtime gate")
	}

	mainTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/main.yml")
	for _, name := range []string{
		"Resolve Ceph storage role variables",
		"Require a Ceph image for the container-runtime safety proof",
		"Prove the Ceph storage node container runtime",
	} {
		task := mainTasks[findAnsibleTask(t, mainTasks, name)]
		if when := fmt.Sprint(task["when"]); strings.Contains(when, "bootwright_task_storage_skip_prereqs") {
			t.Fatalf("%q is gated by skip_prereqs, so apply --stage base can bypass it: when=%v", name, task["when"])
		}
	}
	contextTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/context_vars.yml")
	contextTask := contextTasks[findAnsibleTask(t, contextTasks, "Resolve managed Ceph image for pre-mutation checks")]
	contextFacts, ok := contextTask["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the early Ceph context must be resolved with set_fact, got %v", contextTask)
	}
	imageExpr := fmt.Sprint(contextFacts["bootwright_ceph_bootstrap_image"])
	if !strings.Contains(imageExpr, "bootwright_selected_storage_cluster.provider.image") {
		t.Fatalf("the early Ceph context must extract only the provider image needed by the runtime gate, got %v", contextFacts)
	}
	if _, ok := contextFacts["bootwright_ceph_provider"]; ok {
		t.Fatalf("the early Ceph context must not materialize the full provider before distribution facts exist, got %v", contextFacts)
	}
	imageTask := mainTasks[findAnsibleTask(t, mainTasks, "Require a Ceph image for the container-runtime safety proof")]
	imageAssert, ok := imageTask["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(imageAssert["that"]), "bootwright_ceph_bootstrap_image") {
		t.Fatalf("the pre-mutation image gate must fail closed on an unresolved provider image, got %v", imageTask)
	}

	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/container_runtime.yml")
	probeIdx := findAnsibleTask(t, tasks, "Prove the storage node container runtime can start a Ceph container")
	fallbackIdx := findAnsibleTask(t, tasks, "Prove the storage node container runtime starts under the cgroupfs cgroup manager")
	writeIdx := findAnsibleTask(t, tasks, "Select the cgroupfs cgroup manager where systemd cannot install the device filter")
	removeIdx := findAnsibleTask(t, tasks, "Remove the Bootwright cgroup manager drop-in the node no longer needs")
	assertIdx := findAnsibleTask(t, tasks, "Require a storage node container runtime that can start a Ceph container")
	if !(probeIdx < fallbackIdx && fallbackIdx < writeIdx && writeIdx < assertIdx) {
		t.Fatalf("the gate must probe, fall back, remediate, then assert (probe=%d fallback=%d write=%d assert=%d)", probeIdx, fallbackIdx, writeIdx, assertIdx)
	}

	probe := fmt.Sprint(tasks[probeIdx]["ansible.builtin.command"])
	for _, want := range []string{"podman", "run", "--entrypoint", "stat", "/var/lib/ceph"} {
		if !strings.Contains(probe, want) {
			t.Fatalf("the runtime probe must start a real container the way cephadm does before every deployment; missing %q in %v", want, tasks[probeIdx]["ansible.builtin.command"])
		}
	}

	writeWhen := fmt.Sprint(tasks[writeIdx]["when"])
	if !strings.Contains(writeWhen, "bootwright_ceph_runtime_probe.rc") || !strings.Contains(writeWhen, "bootwright_ceph_runtime_cgroupfs_probe.rc") {
		t.Fatalf("the cgroup manager drop-in must be written only when the default manager provably fails AND cgroupfs provably works — writing it unconditionally would drop cgroup BPF device isolation on every Ceph node. Got when=%v", tasks[writeIdx]["when"])
	}
	systemdIdx := findAnsibleTask(t, tasks, "Prove the storage node still needs the cgroupfs cgroup manager Bootwright selected")
	if !(writeIdx < systemdIdx && systemdIdx < removeIdx) {
		t.Fatalf("the systemd cgroup manager must be re-proven between writing and removing the drop-in (write=%d systemd=%d remove=%d)", writeIdx, systemdIdx, removeIdx)
	}
	systemdProbe := fmt.Sprint(tasks[systemdIdx]["ansible.builtin.command"])
	if !strings.Contains(systemdProbe, "--cgroup-manager=systemd") {
		t.Fatalf("the probe that decides whether the drop-in is still needed must pin --cgroup-manager=systemd, or the drop-in answers for the node it is being tested against. Got %v", tasks[systemdIdx]["ansible.builtin.command"])
	}

	removeWhen := fmt.Sprint(tasks[removeIdx]["when"])
	if !strings.Contains(removeWhen, "bootwright_ceph_runtime_systemd_probe.rc") {
		t.Fatalf("the drop-in must be removed only once a probe that pins --cgroup-manager=systemd provably succeeds. Keying the removal on the ordinary probe is self-defeating: the drop-in makes that probe pass, so every later apply deletes the remediation and then hands cephadm a node that can start no container at all. Got when=%v", tasks[removeIdx]["when"])
	}
	if strings.Contains(removeWhen, "bootwright_ceph_runtime_probe.rc") {
		t.Fatalf("the drop-in removal must not depend on the ordinary probe, which runs under whatever cgroup manager the drop-in already selected. Got when=%v", tasks[removeIdx]["when"])
	}
	if fmt.Sprint(tasks[removeIdx]["register"]) != "bootwright_ceph_cgroup_manager_dropin_removed" {
		t.Fatalf("the drop-in removal must register its result so the runtime can be re-proven after it, got register=%v", tasks[removeIdx]["register"])
	}
	reproveIdx := findAnsibleTask(t, tasks, "Re-prove the storage node container runtime after selecting the cgroup manager")
	if !strings.Contains(fmt.Sprint(tasks[reproveIdx]["when"]), "bootwright_ceph_runtime_config_changed") {
		t.Fatalf("the runtime must be re-proven whenever this apply changed the cgroup manager selection in either direction, not only when the drop-in was written. Got when=%v", tasks[reproveIdx]["when"])
	}

	assertion, ok := tasks[assertIdx]["ansible.builtin.assert"]
	if !ok {
		t.Fatalf("the runtime gate must fail closed with an assert, got %v", tasks[assertIdx])
	}
	if !strings.Contains(fmt.Sprint(assertion), "bootwright_ceph_runtime_ready") {
		t.Fatalf("the gate must assert on the runtime as this apply leaves the node, not on a probe taken before it changed the cgroup manager selection: accepting the earlier probe passes a node whose remediation this same task removed moments later. Got %v", assertion)
	}
}

func TestStorageCephMonReadinessNamesAContainerRuntimeFault(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/mon_readiness.yml"
	readiness := readRepoFile(t, path)
	for _, want := range []string{
		"bootwright_ceph_mon_runtime_uid_gid_failures",
		"bootwright_ceph_mon_runtime_ebpf_failures",
		"Failed to extract uid/gid",
		"eBPF device filter",
		"20-bootwright-ceph-cgroup-manager.conf",
	} {
		if !strings.Contains(readiness, want) {
			t.Errorf("%s must name a host container-runtime fault out of the evidence it already collects, missing %q: cephadm resolves the ceph uid/gid by starting a container before every deployment, so a host that cannot start one places no daemon of any kind and its mon never reaches the monmap — without this the operator is sent to the network, image-pull and mon-port causes, none of which applies", path, want)
		}
	}
	if !strings.Contains(readiness, "bootwright_ceph_mon_service_ls.stdout | default('')") {
		t.Errorf("%s must scan the mon service events for the runtime fault, not only the cephadm event log: cephadm records the podman failure as a service event on `ceph orch ls --service_type mon`, and `ceph log last 100 cephadm` can hold nothing but reconfigure lines", path)
	}
	runtimeBlock := strings.Index(readiness, "cephadm could not start a container on a host at all")
	usualCauses := strings.Index(readiness, "the three usual causes")
	if runtimeBlock < 0 || usualCauses < 0 || runtimeBlock > usualCauses {
		t.Errorf("the container-runtime block must precede the three usual causes and rule them out (runtime=%d causes=%d)", runtimeBlock, usualCauses)
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

	statIdx := findAnsibleTask(t, tasks, "Stat Bootwright storage cluster ownership record for override rebuild")
	readIdx := findAnsibleTask(t, tasks, "Read Bootwright storage cluster ownership record for override rebuild")
	decodeIdx := findAnsibleTask(t, tasks, "Decode Bootwright storage cluster ownership record for override rebuild")
	validateIdx := findAnsibleTask(t, tasks, "Validate incomplete-bootstrap controller ownership evidence")
	detectIdx := findAnsibleTask(t, tasks, "Detect an incomplete Bootwright bootstrap eligible for override rebuild")
	authorizeIdx := findAnsibleTask(t, tasks, "Decide whether the controller authorizes this exact incomplete bootstrap cleanup")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to touch a Bootwright Ceph cluster whose ownership marker is missing")
	gateIdx := findAnsibleTask(t, tasks, "Enforce apply mode for the Ceph cluster")
	rebuildIdx := findAnsibleTask(t, tasks, "Decide whether this cluster requires an authorized override rebuild")
	zapIdx := findAnsibleTask(t, tasks, "Remove existing cephadm cluster for override rebuild on every topology host")
	stampIdx := findAnsibleTask(t, tasks, "Stamp Bootwright Ceph ownership marker")
	if !(statIdx < readIdx && readIdx < decodeIdx && decodeIdx < validateIdx && validateIdx < detectIdx && detectIdx < authorizeIdx && authorizeIdx < refuseIdx && refuseIdx < gateIdx && gateIdx < rebuildIdx && rebuildIdx < zapIdx && zapIdx < stampIdx) {
		t.Fatalf("exact controller evidence and positive authorization must precede the refusal, gate, cleanup, and marker stamp (stat=%d read=%d decode=%d validate=%d detect=%d authorize=%d refuse=%d gate=%d rebuild=%d zap=%d stamp=%d)", statIdx, readIdx, decodeIdx, validateIdx, detectIdx, authorizeIdx, refuseIdx, gateIdx, rebuildIdx, zapIdx, stampIdx)
	}
	if tasks[readIdx]["delegate_to"] != "localhost" || tasks[readIdx]["become"] != false || tasks[readIdx]["failed_when"] != false {
		t.Fatalf("incomplete-bootstrap record read must be a non-mutating controller-local probe whose ambiguity reaches the explicit refusal, got %v", tasks[readIdx])
	}
	decode := fmt.Sprint(tasks[decodeIdx])
	for _, want := range []string{"from_json", "rescue", "bootwright_ceph_override_record_decode_failed"} {
		if !strings.Contains(decode, want) {
			t.Fatalf("incomplete-bootstrap record decode must fail closed into explicit evidence classification; missing %q in %v", want, tasks[decodeIdx])
		}
	}
	validate, ok := tasks[validateIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("incomplete-bootstrap record validation must be a set_fact, got %v", tasks[validateIdx])
	}
	validateExpr := fmt.Sprint(validate["bootwright_ceph_incomplete_bootstrap_record_valid"])
	for _, want := range []string{"bootwright.io/ownership/v1alpha1", "storage-cluster", ".name", ".owner", ".role", ".context", ".cluster", ".host", ".seedHost", "bootwright_selected_storage_cluster.name", "bootwright_selected_storage_cluster.seedHost"} {
		if !strings.Contains(validateExpr, want) {
			t.Fatalf("incomplete-bootstrap record validation must prove exact owner identity and desired seed; missing %q in %v", want, validateExpr)
		}
	}

	detect, ok := tasks[detectIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("incomplete-bootstrap detection must be a set_fact, got %v", tasks[detectIdx])
	}
	expr := fmt.Sprint(detect["bootwright_ceph_incomplete_bootstrap"])
	for _, want := range []string{
		"bootwright_ceph_incomplete_bootstrap_record_valid",
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
	authorize, ok := tasks[authorizeIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("incomplete-bootstrap authorization must be a set_fact, got %v", tasks[authorizeIdx])
	}
	authorizeExpr := fmt.Sprint(authorize["bootwright_ceph_incomplete_bootstrap_cleanup_authorized"])
	for _, want := range []string{"bootwright_apply_mode", "rebuild", "bootwright_ceph_incomplete_bootstrap", "bootwright_selected_storage_cluster.name", "bootwright_ceph_incomplete_bootstrap_authorized_clusters"} {
		if !strings.Contains(authorizeExpr, want) {
			t.Fatalf("the shared incomplete-bootstrap consequence predicate must consume mode, host proof, and the exact controller list; missing %q in %v", want, authorizeExpr)
		}
	}
	applyMode := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/apply_mode.yml")
	if got := strings.Count(applyMode, "bootwright_ceph_incomplete_bootstrap_authorized_clusters"); got != 1 {
		t.Fatalf("the dedicated positive list must be consumed solely by the shared incomplete-bootstrap consequence predicate, got %d consumers", got)
	}

	rebuild, ok := tasks[rebuildIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(rebuild["bootwright_ceph_rebuild_cleanup_required"]), "bootwright_ceph_incomplete_bootstrap_cleanup_authorized") {
		t.Fatalf("override rebuild decision must honor the incomplete-bootstrap decision, got %v", tasks[rebuildIdx])
	}
	gateVars, ok := tasks[gateIdx]["vars"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(gateVars["bootwright_gate_owned"]), "bootwright_ceph_incomplete_bootstrap_cleanup_authorized") {
		t.Fatalf("the ownership gate must consume the same incomplete-bootstrap consequence predicate as cleanup, got %v", tasks[gateIdx]["vars"])
	}
	if got := fmt.Sprint(tasks[zapIdx]["when"]); !strings.Contains(got, "bootwright_ceph_rebuild_cleanup_required") {
		t.Fatalf("override rebuild zap must consume the authorized rebuild decision, got %v", tasks[zapIdx]["when"])
	}

	refuse, ok := tasks[refuseIdx]["ansible.builtin.fail"].(map[string]any)
	if !ok {
		t.Fatalf("missing-marker refusal must be a fail, got %v", tasks[refuseIdx])
	}
	when := fmt.Sprint(tasks[refuseIdx]["when"])
	for _, want := range []string{"bootwright_selected_storage_cluster.seedHost", "bootwright_ceph_override_owned_fsid | default('') | length == 0", "not (bootwright_ceph_incomplete_bootstrap_cleanup_authorized"} {
		if !strings.Contains(when, want) {
			t.Fatalf("missing-marker refusal must stay closed until the shared consequence predicate is true; missing %q in %v", want, tasks[refuseIdx]["when"])
		}
	}
	for _, want := range []string{"cleanup, bootstrap, and ownership-marker stamping", "missing, unreadable, or does not exactly identify", "selected desired seed", "bootwright_apply_rebuild_invocation", "data-loss authorization", "retains this context and selection"} {
		if got := fmt.Sprint(refuse["msg"]); !strings.Contains(got, want) {
			t.Fatalf("missing-marker refusal must use the exact controller-built recovery invocation; missing %q in %v", want, refuse["msg"])
		}
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
	rmIdx := findAnsibleTask(t, tasks, "Remove cephadm cluster on the ownership authority host")
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

	if strings.Contains(readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/release_node.yml"), "bootwright_ownership_kind: storage-cluster") {
		t.Fatal("the remote role must leave the controller storage ownership record for exact-set attestation validation")
	}

	zap := tasks[findAnsibleTask(t, tasks, "Zap declared Ceph device partition tables")]
	if got := fmt.Sprint(zap["failed_when"]); !strings.Contains(got, "!= 0") {
		t.Fatalf("ceph destroy sgdisk zap must fail closed on error, got failed_when=%v", zap["failed_when"])
	}
}

func TestStorageCephadmDestroyLiveFsidProbeIsBounded(t *testing.T) {
	tasks := storageCephDestroyTasks(t)

	confIdx := findAnsibleTask(t, tasks, "Check existing Ceph configuration on seed host")
	scanIdx := findAnsibleTask(t, tasks, "Scan the seed host for Ceph cluster directories")
	stateIdx := findAnsibleTask(t, tasks, "Resolve whether the seed host still carries Ceph cluster state")
	probeIdx := findAnsibleTask(t, tasks, "Check Ceph fsid on seed host")
	if !(confIdx < stateIdx && scanIdx < stateIdx && stateIdx < probeIdx) {
		t.Fatalf("ceph destroy must resolve the seed's local cluster state before probing it live (conf=%d scan=%d state=%d probe=%d)", confIdx, scanIdx, stateIdx, probeIdx)
	}

	state, ok := tasks[stateIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("seed local Ceph state must be a set_fact, got %v", tasks[stateIdx])
	}
	local := fmt.Sprint(state["bootwright_ceph_destroy_local_state"])
	for _, want := range []string{"bootwright_ceph_destroy_conf_check.rc", "bootwright_ceph_destroy_state_dirs.files"} {
		if !strings.Contains(local, want) {
			t.Fatalf("seed local Ceph state must read %s: a seed carrying /var/lib/ceph/<fsid> but no ceph.conf still answers `ceph fsid`, and skipping the probe there would fail the ownership gate open, got %v", want, state["bootwright_ceph_destroy_local_state"])
		}
	}

	probe, ok := tasks[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("ceph destroy live fsid probe must be a command, got %v", tasks[probeIdx])
	}
	argv := fmt.Sprint(probe["argv"])
	if !strings.Contains(argv, "timeout") {
		t.Fatalf("ceph destroy live fsid probe must be wrapped in `timeout`: `cephadm shell` pulls a container image and `ceph fsid` retries a dead mon forever, so an unbounded probe parks the whole teardown with no output, got argv=%v", probe["argv"])
	}
	when := fmt.Sprint(tasks[probeIdx]["when"])
	if !strings.Contains(when, "bootwright_ceph_destroy_local_state") {
		t.Fatalf("ceph destroy live fsid probe must be gated on the seed still carrying Ceph cluster state, got when=%v", tasks[probeIdx]["when"])
	}
}

func TestStorageCephadmDestroyOwnershipRefusalNamesItsEvidence(t *testing.T) {
	tasks := storageCephDestroyTasks(t)

	evidenceIdx := findAnsibleTask(t, tasks, "Resolve the unproven Ceph destroy ownership evidence on seed host")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to destroy a non-Bootwright Ceph cluster on seed host")
	if evidenceIdx >= refuseIdx {
		t.Fatalf("ceph destroy must resolve its ownership evidence before refusing on it (evidence=%d refuse=%d)", evidenceIdx, refuseIdx)
	}

	evidence, ok := tasks[evidenceIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("ceph destroy ownership evidence must be a set_fact, got %v", tasks[evidenceIdx])
	}
	unproven := fmt.Sprint(evidence["bootwright_ceph_destroy_unproven"])
	for _, want := range []string{
		"bootwright_ceph_destroy_record.stat.exists",
		"bootwright_ceph_destroy_owned_fsid",
		"bootwright_ceph_destroy_conf_fsid",
		"bootwright_ceph_destroy_live_fsid",
	} {
		if !strings.Contains(unproven, want) {
			t.Fatalf("ceph destroy refusal must be able to name %s as the unproven factor, got %v", want, evidence["bootwright_ceph_destroy_unproven"])
		}
	}

	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("ceph destroy ownership guard must be an assert, got %v", tasks[refuseIdx])
	}
	msg := fmt.Sprint(refuse["fail_msg"])
	for _, want := range []string{"bootwright_ceph_destroy_unproven", "bootwright_ceph_destroy_evidence", "--recover-ceph-ownership"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("ceph destroy ownership refusal must report %s: an operator cannot act on a refusal that lists three possible causes and names none, got %v", want, refuse["fail_msg"])
		}
	}
	if strings.Contains(msg, "either no ownership record exists") {
		t.Fatalf("ceph destroy ownership refusal must not fall back to the undiagnosable either/or prose, got %v", refuse["fail_msg"])
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
	authorityIdx := findAnsibleTask(t, tasks, "Require a reachable declared mon when the Ceph seed is absent")
	wipeIdx := findAnsibleTask(t, tasks, "Destroy Ceph storage cluster")
	if !(selectIdx < probeIdx && probeIdx < recordIdx && recordIdx < reachableIdx && reachableIdx < authorityIdx) {
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
	if authorityIdx >= wipeIdx {
		t.Fatalf("ownership-authority reachability assert (idx %d) must run before the destroy include_role wipe (idx %d)", authorityIdx, wipeIdx)
	}
	if _, ok := tasks[authorityIdx]["ansible.builtin.assert"]; !ok {
		t.Fatalf("ownership-authority reachability guard must be a hard assert so any_errors_fatal aborts all hosts, got %v", tasks[authorityIdx])
	}

	preliminaryIdx := findAnsibleTask(t, tasks, "Prepare the storage destroy attestation before teardown")
	terminalIdx := findAnsibleTask(t, tasks, "Record the terminal storage destroy attestation")
	if !(preliminaryIdx < wipeIdx && wipeIdx < terminalIdx) {
		t.Fatalf("the preliminary report must retain skipped-node evidence before ended hosts disappear, and the terminal report must overwrite it after every surviving node's witness (preliminary=%d wipe=%d terminal=%d)", preliminaryIdx, wipeIdx, terminalIdx)
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

func TestMachineRegistrationDeregisterEndsOnlyANodeItProvesAbsent(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_registration_deregister.yml"
	plays := readAnsiblePlays(t, path)
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	classifyIdx := findAnsibleTask(t, tasks, "Classify how a node refused its deregistration connection")
	absentIdx := findAnsibleTask(t, tasks, "End nodes this deregistration proved absent")
	warnIdx := findAnsibleTask(t, tasks, "Warn that a node Bootwright could not reach keeps its RHSM registration")
	endIdx := findAnsibleTask(t, tasks, "End nodes Bootwright cannot reach or escalate on")
	deregisterIdx := findAnsibleTask(t, tasks, "Deregister machine from RHSM")
	if !(classifyIdx < absentIdx && absentIdx < warnIdx && warnIdx < endIdx && endIdx < deregisterIdx) {
		t.Fatalf("deregistration must classify absence, end the nodes it proved absent, then warn before it ends the rest (classify=%d absent=%d warn=%d end=%d deregister=%d)", classifyIdx, absentIdx, warnIdx, endIdx, deregisterIdx)
	}
	include, ok := tasks[classifyIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok || include["name"] != "bootwright.core.storage_node_access" || include["tasks_from"] != "classify_absence.yml" {
		t.Fatalf("deregistration must classify through the shared absence classifier, not a private copy, got %v", tasks[classifyIdx])
	}
	when := fmt.Sprint(tasks[absentIdx]["when"])
	if !strings.Contains(when, "bootwright_node_access_node_absent") {
		t.Errorf("the silent end_host must read proven absence, got when=%v", tasks[absentIdx]["when"])
	}
	if strings.Contains(when, "bootwright_node_access_connection_available") {
		t.Errorf("an identity refusal is not absence: ending the host on the availability flag leaves the subscription consumed against the hardware DMI UUID and it collides with the next install, got when=%v", tasks[absentIdx]["when"])
	}
	if _, isEnd := tasks[endIdx]["ansible.builtin.meta"]; !isEnd {
		t.Fatalf("the unproven-absence path must still end the host, got %v", tasks[endIdx])
	}
	warn := fmt.Sprint(tasks[warnIdx]["ansible.builtin.debug"])
	for _, want := range []string{"could not prove the node absent", "RHSM registration was NOT released", "DMI system UUID"} {
		if !strings.Contains(warn, want) {
			t.Errorf("the skip warning must name the residue it leaves; missing %q in %v", want, tasks[warnIdx]["ansible.builtin.debug"])
		}
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
	lvmIdx := findAnsibleTask(t, tasks, "Take the LVM stack down on the declared Ceph devices before the wipe")
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	if !(reprobeIdx < refuseIdx && refuseIdx < lvmIdx && lvmIdx < wipeIdx) {
		t.Fatalf("ceph destroy must re-probe mounts and refuse before it takes the LVM stack down and wipes (reprobe=%d refuse=%d lvm=%d wipe=%d)", reprobeIdx, refuseIdx, lvmIdx, wipeIdx)
	}
	if refuseIdx+1 != lvmIdx {
		t.Fatalf("mount re-probe refusal must be the task immediately before the first destructive step, which is the LVM teardown (refuse=%d lvm=%d)", refuseIdx, lvmIdx)
	}
}

func TestStorageCephadmDestroyTearsDownLVMBeforeTheWipe(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/lvm_teardown.yml"
	tasks := readAnsibleTasks(t, path)
	vgchangeIdx := findAnsibleTask(t, tasks, "Deactivate the volume groups on the Ceph devices this teardown wipes")
	openIdx := findAnsibleTask(t, tasks, "Refuse to wipe a Ceph device whose volume group is still open")
	vgremoveIdx := findAnsibleTask(t, tasks, "Remove the volume groups on the Ceph devices this teardown wipes")
	pvremoveIdx := findAnsibleTask(t, tasks, "Remove the physical volume labels from the Ceph devices this teardown wipes")
	if !(vgchangeIdx < openIdx && openIdx < vgremoveIdx && vgremoveIdx < pvremoveIdx) {
		t.Fatalf("destroy must deactivate, refuse an open volume group, then remove the volume group and the physical volume label: wipefs cannot clear a device whose volume group is active and reports \"Device or resource busy\" instead (vgchange=%d refuse=%d vgremove=%d pvremove=%d)", vgchangeIdx, openIdx, vgremoveIdx, pvremoveIdx)
	}
	if got := fmt.Sprint(tasks[vgchangeIdx]["failed_when"]); got != "false" {
		t.Errorf("the deactivation must not fail the run itself; its result is what the open-volume-group refusal reads to name the live OSD holding the device, got failed_when=%v", tasks[vgchangeIdx]["failed_when"])
	}
	if got := fmt.Sprint(tasks[openIdx]["when"]); strings.Contains(got, "authorize") {
		t.Errorf("no --authorize token may relax the open-volume-group refusal: wiping under a live OSD corrupts it, got when=%v", tasks[openIdx]["when"])
	}
	release := fmt.Sprint(tasks[findAnsibleTask(t, tasks, "Release the Ceph clusters still running on the storage node")]["ansible.builtin.command"])
	if !strings.Contains(release, "rm-cluster") || !strings.Contains(release, "--fsid") {
		t.Fatalf("the leftover-cluster release must be fsid-scoped `cephadm rm-cluster --force --fsid <fsid>`, got %v", release)
	}
	if strings.Contains(release, "zap-osds") {
		t.Errorf("the leftover-cluster release must not pass --zap-osds: this teardown takes the LVM stack down and wipes the devices itself, and --zap-osds needs a pullable container image the node may no longer reach, got %v", release)
	}
	tagsIdx := findAnsibleTask(t, tasks, "Read the Ceph OSD tags of the volume groups this teardown owns")
	if got := fmt.Sprint(tasks[tagsIdx]["loop"]); !strings.Contains(got, "bootwright_ceph_teardown_owned_vgs") {
		t.Errorf("the released cluster identity must come from the OSD tags of the volume groups standing on devices this node records as its own, never from every volume group present, got loop=%v", tasks[tagsIdx]["loop"])
	}
	if tagsIdx > vgchangeIdx {
		t.Errorf("the leftover cluster must be released before the deactivation; its daemons are what hold the logical volumes open (tags=%d vgchange=%d)", tagsIdx, vgchangeIdx)
	}
}

func TestStorageCephadmDestroyWipePathsTakeTheLVMStackDown(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/"
	declared := readAnsibleTasks(t, base+"wipe_and_cleanup.yml")
	declaredLVM := findAnsibleTask(t, declared, "Take the LVM stack down on the declared Ceph devices before the wipe")
	declaredWipe := findAnsibleTask(t, declared, "Wipe declared Ceph device signatures")
	if declaredLVM > declaredWipe {
		t.Fatalf("the declared-device wipe must take the LVM stack down first; a ceph-* volume group left standing makes wipefs fail with \"Device or resource busy\" (lvm=%d wipe=%d)", declaredLVM, declaredWipe)
	}
	if got := fmt.Sprint(declared[declaredLVM]["vars"]); !strings.Contains(got, "bootwright_ceph_recorded_osd_devices") {
		t.Errorf("the declared-device path must vouch only for devices its OSD ownership marker records, so a device wiped under --authorize unowned-devices never triggers an automatic cluster release, got vars=%v", declared[declaredLVM]["vars"])
	}
	filter := readAnsibleTasks(t, base+"filter_device_reclaim.yml")
	filterTasks := nestedAnsibleTasks(t, filter[findAnsibleTask(t, filter, "Reclaim Ceph-signed disks on filter-selected OSD hosts")], "block")
	filterLVM := findAnsibleTask(t, filterTasks, "Take the LVM stack down on the filter-selected OSD disks before the wipe")
	filterWipe := findAnsibleTask(t, filterTasks, "Remove filesystem signatures from filter-selected OSD disks")
	if filterLVM > filterWipe {
		t.Fatalf("the filter reclaim selects disks by their ceph-* volume group, so it must take that stack down before wipefs (lvm=%d wipe=%d)", filterLVM, filterWipe)
	}
}

func TestStorageCephadmDestroySkipsOnlyANodeItProvesAbsent(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_destroy.yml")
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	classifyIdx := findAnsibleTask(t, tasks, "Classify how a storage host refused its teardown connection")
	recordIdx := findAnsibleTask(t, tasks, "Record how a storage host refused its teardown connection")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse a storage host this teardown cannot prove absent")
	reachableIdx := findAnsibleTask(t, tasks, "Require storage hosts reachable unless --authorize unreachable-nodes")
	skipIdx := findAnsibleTask(t, tasks, "Stop tearing down unreachable storage hosts")
	if !(classifyIdx < recordIdx && recordIdx < refuseIdx && refuseIdx < reachableIdx && reachableIdx < skipIdx) {
		t.Fatalf("destroy must classify the refusal and fail closed on a node it cannot prove absent before it may skip anything (classify=%d record=%d refuse=%d reachable=%d skip=%d)", classifyIdx, recordIdx, refuseIdx, reachableIdx, skipIdx)
	}
	if _, ok := tasks[refuseIdx]["ansible.builtin.assert"]; !ok {
		t.Fatalf("the unproven-absence refusal must be a hard assert so any_errors_fatal aborts the teardown, got %v", tasks[refuseIdx])
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); strings.Contains(got, "bootwright_destroy_skip_unreachable") {
		t.Errorf("--authorize unreachable-nodes must not relax the unproven-absence refusal: skipping a node that is in fact running leaves its Ceph daemons up and its OSD devices holding data while the run reports the cluster destroyed, got when=%v", tasks[refuseIdx]["when"])
	}
	assertion := fmt.Sprint(tasks[refuseIdx]["ansible.builtin.assert"])
	if !strings.Contains(assertion, "bootwright_storage_node_absent") {
		t.Errorf("the refusal must gate on proven absence, not on a negated answered flag: an unreadable refusal must fail closed rather than skip, got %v", tasks[refuseIdx]["ansible.builtin.assert"])
	}
	if !strings.Contains(assertion, "bootwright_storage_node_refusal") {
		t.Errorf("the refusal must print what the probes reported, or the operator is told a node was skipped with no evidence, got %v", tasks[refuseIdx]["ansible.builtin.assert"])
	}
	include, ok := tasks[classifyIdx]["ansible.builtin.include_role"].(map[string]any)
	if !ok || include["name"] != "bootwright.core.storage_node_access" || include["tasks_from"] != "classify_absence.yml" {
		t.Fatalf("storage destroy must classify through the shared absence classifier, not a private copy, got %v", tasks[classifyIdx])
	}
	if got := fmt.Sprint(tasks[classifyIdx]["vars"]); !strings.Contains(got, "bootwright_storage_reachable_probe") {
		t.Errorf("the unmanaged-access path must feed the ping diagnostic to the classifier, got vars=%v", tasks[classifyIdx]["vars"])
	}
	record := fmt.Sprint(tasks[recordIdx]["vars"]) + fmt.Sprint(tasks[recordIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{"bootwright_node_access_node_absent", "bootwright_node_access_node_refusal", "bootwright_storage_reachable_probe"} {
		if !strings.Contains(record, want) {
			t.Errorf("the classification must read %s so the teardown reports what actually refused instead of the power-off reading, got %v", want, tasks[recordIdx])
		}
	}
	if strings.Contains(record, "No route to host") {
		t.Errorf("the destroy play must consume the hoisted absence verdict, not re-derive one from its own copy of the pattern, got %v", tasks[recordIdx])
	}
}

func TestStorageNodeAbsenceIsClassifiedOnceForEveryTeardown(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_node_access/tasks/"
	classifyTasks := readAnsibleTasks(t, base+"classify_absence.yml")
	if len(classifyTasks) != 1 {
		t.Fatalf("the shared absence classifier must be one task, got %d", len(classifyTasks))
	}
	pattern := fmt.Sprint(classifyTasks[0]["vars"])
	for _, want := range []string{"No route to host", "Network is unreachable", "Host is down", "Connection timed out", "Operation timed out", "Connection refused", "port 22: Connection", "Destination Host Unreachable"} {
		if !strings.Contains(pattern, want) {
			t.Errorf("absence must be matched positively; %q is one of the connection-level failures that prove a node could not be contacted, got %s", want, pattern)
		}
	}
	for _, reject := range []string{"Permission denied", "Host key verification", "sudo:"} {
		if strings.Contains(pattern, reject) {
			t.Errorf("the classification must not enumerate identity refusals like %q: anything that is not proven absence is not absence, and listing them invites the inverse default, got %s", reject, pattern)
		}
	}
	setFact, ok := classifyTasks[0]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the shared absence classifier must be a set_fact, got %v", classifyTasks[0])
	}
	verdict := fmt.Sprint(setFact["bootwright_node_access_node_absent"])
	for _, want := range []string{"bootwright_node_access_absence_diagnostic", "bootwright_node_access_absence_timed_out"} {
		if !strings.Contains(verdict, want) {
			t.Errorf("the shared verdict must read %q so every teardown gets the same answer, got %v", want, setFact)
		}
	}

	selector := readAnsibleTasks(t, base+"select_connection.yml")
	availableIdx := findAnsibleTask(t, selector, "Record whether a storage node identity is reachable for teardown")
	refusalIdx := findAnsibleTask(t, selector, "Record how the storage node answered its teardown identities")
	classifyIdx := findAnsibleTask(t, selector, "Classify whether the storage node teardown refusal proves absence")
	if !(availableIdx < refusalIdx && refusalIdx < classifyIdx) {
		t.Fatalf("the selector must record the refusal before it classifies it (available=%d refusal=%d classify=%d)", availableIdx, refusalIdx, classifyIdx)
	}
	refusal := fmt.Sprint(selector[refusalIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{"bootwright_node_access_target_probe", "bootwright_node_access_install_probe", "bootwright_node_access_node_absent"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the recorded refusal must name what each identity reported and default the verdict, missing %q in %v", want, selector[refusalIdx])
		}
	}
	if got := fmt.Sprint(selector[classifyIdx]["when"]); !strings.Contains(got, "bootwright_node_access_connection_available") {
		t.Errorf("a node that answered is never absent, so the selector must classify only a refusal, got when=%v", selector[classifyIdx]["when"])
	}
	if got := fmt.Sprint(selector[classifyIdx]["vars"]); !strings.Contains(got, "124") || !strings.Contains(got, "137") || !strings.Contains(got, "143") {
		t.Errorf("a probe the timeout wrapper killed is proof the node never answered, got vars=%v", selector[classifyIdx]["vars"])
	}
}

func TestStorageCephadmDestroyReportsWhyEachSkippedNodeWasSkipped(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_destroy.yml")
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	summaryIdx := findAnsibleTask(t, tasks, "Prepare the storage destroy attestation before teardown")
	block := fmt.Sprint(tasks[summaryIdx])
	if !strings.Contains(block, "bootwright_storage_node_refusal") {
		t.Fatalf("the skip summary must carry each node's refusal, not only its name: a skipped node with no recorded cause is what let a running node pass as absent, got %v", tasks[summaryIdx])
	}
	for _, want := range []string{"bootwright_storage_destroy_attestation", "storage-destroy-result.json"} {
		if !strings.Contains(block, want) {
			t.Errorf("the preliminary controller result must retain %q so an all-unreachable topology has an exact report and any reachable node remains incomplete until the terminal proof, got %v", want, tasks[summaryIdx])
		}
	}
}

func TestStorageCephadmDestroyConfirmsCompletionWithARemoteWitness(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy.yml")
	idx := findAnsibleTask(t, tasks, "Confirm the storage node completed its teardown")
	if idx != len(tasks)-1 {
		t.Fatalf("the completion witness must be the LAST task of the destroy chain so reaching it proves every teardown step ran on the node (idx=%d len=%d)", idx, len(tasks))
	}
	if _, ok := tasks[idx]["ansible.builtin.command"]; !ok {
		t.Fatalf("the witness must be a remote command: under ignore_unreachable a dropped host still runs local actions like set_fact and debug, so only a task that needs the SSH connection proves the node stayed reachable through its wipe, got %v", tasks[idx])
	}
	if got := fmt.Sprint(tasks[idx]["register"]); got != "bootwright_ceph_destroy_completion" {
		t.Fatalf("the witness must register bootwright_ceph_destroy_completion for the mid-teardown audit, got %v", tasks[idx]["register"])
	}
}

func TestStorageCephadmDestroyAuditsNodesLostMidTeardown(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_destroy.yml")
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	roleIdx := findAnsibleTask(t, tasks, "Destroy Ceph storage cluster")
	auditIdx := findAnsibleTask(t, tasks, "Record the terminal storage destroy attestation")
	if auditIdx < roleIdx {
		t.Fatalf("the mid-teardown audit must run after the destroy role, or the completion witness it reads has not run yet (role=%d audit=%d)", roleIdx, auditIdx)
	}
	audit := fmt.Sprint(tasks[auditIdx])
	for _, want := range []string{
		"bootwright_storage_destroy_attestation",
		"outcome",
		"nodes",
		"storage-destroy-result.json",
		"bootwright_destroy_skip_unreachable",
	} {
		if !strings.Contains(audit, want) {
			t.Errorf("the audit must retain %q: with --authorize unreachable-nodes the play runs under ignore_unreachable, so a node that drops after the reachability probe has its device wipe silently skipped and only this audit records the partial destroy, got %v", want, tasks[auditIdx])
		}
	}
	filter := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/plugins/filter/storage_destroy.py")
	for _, want := range []string{"bootwright_ceph_destroy_completion", "bootwright_ceph_sweep_verify", "connection lost during teardown"} {
		if !strings.Contains(filter, want) {
			t.Errorf("the terminal attestation filter must retain %q, got:\n%s", want, filter)
		}
	}
}

func TestStorageCephadmDestroyDefersControllerOwnershipReleaseUntilValidatedAttestation(t *testing.T) {
	release := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/release_node.yml")
	if strings.Contains(release, "Remove storage cluster ownership record") || strings.Contains(release, "bootwright_ownership_kind: storage-cluster") {
		t.Fatalf("the remote role must not erase the controller's recovery anchor before the task runner validates exact topology coverage, got:\n%s", release)
	}
	runner := readRepoFile(t, "internal/converge/workflow/destroy_runner.go")
	callPos := strings.Index(runner, "validateStorageDestroyTask(taskOpts, task)")
	successPos := strings.Index(runner, "return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped")
	validatePos := strings.Index(runner, "ValidateStorageDestroyResults")
	reconcilePos := strings.Index(runner, "ReconcileStorageDestroyOwnership")
	if callPos < 0 || validatePos < 0 || reconcilePos < validatePos {
		t.Fatalf("a storage task must validate its terminal report and reconcile ownership before it returns success to the ledger (call=%d success=%d validate=%d reconcile=%d)", callPos, successPos, validatePos, reconcilePos)
	}
	scheduler := readRepoFile(t, "internal/converge/workflow/apply_scheduler.go")
	durableOKPos := strings.Index(scheduler, "ledger.MarkOK(event.id")
	if durableOKPos < 0 {
		t.Fatal("the scheduler no longer records an OK task status")
	}
	durableTail := scheduler[durableOKPos:]
	durableSavePos := strings.Index(durableTail, "saveLedger()")
	finalizePos := strings.Index(durableTail, "event.finalize()")
	if durableSavePos < 0 || finalizePos < 0 || durableSavePos > finalizePos {
		t.Fatalf("completed ownership may be released only after the task's OK status is durably saved (mark=%d save=%d finalize=%d)", durableOKPos, durableSavePos, finalizePos)
	}
}

func TestStorageCephadmDestroyReleasesHostEvidenceAfterLocalData(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/"
	dataTasks := readAnsibleTasks(t, base+"release_node.yml")
	managedDataIdx := findAnsibleTask(t, dataTasks, "Remove managed Ceph local data")
	sharedDataIdx := findAnsibleTask(t, dataTasks, "Remove Bootwright-specific Ceph local data alongside a co-resident cluster")
	ownedDirsIdx := findAnsibleTask(t, dataTasks, "Remove owned Ceph fsid directories alongside a co-resident cluster")
	packagesIdx := findAnsibleTask(t, dataTasks, "Remove Ceph package ownership records")
	if !(managedDataIdx < packagesIdx && sharedDataIdx < ownedDirsIdx && ownedDirsIdx < packagesIdx) {
		t.Fatalf("local data must be removed before package cleanup and the later evidence phase (managedData=%d sharedData=%d ownedDirs=%d packages=%d)", managedDataIdx, sharedDataIdx, ownedDirsIdx, packagesIdx)
	}
	for _, idx := range []int{managedDataIdx, sharedDataIdx, ownedDirsIdx} {
		body := fmt.Sprint(dataTasks[idx])
		for _, evidence := range []string{"/etc/ceph", "bootwright_ceph_ownership_marker_path", "bootwright_ceph_osd_marker_path"} {
			if strings.Contains(body, evidence) {
				t.Errorf("data-removal task %q must not erase host ownership evidence %q in the same loop: an item failure could otherwise leave data behind after the retry proof is gone, got %v", dataTasks[idx]["name"], evidence, dataTasks[idx])
			}
		}
	}
	evidenceTasks := readAnsibleTasks(t, base+"release_evidence.yml")
	managedEvidenceIdx := findAnsibleTask(t, evidenceTasks, "Release managed Ceph host ownership evidence")
	adminEvidenceIdx := findAnsibleTask(t, evidenceTasks, "Release owned Ceph admin configuration alongside a co-resident cluster")
	markerEvidenceIdx := findAnsibleTask(t, evidenceTasks, "Release Bootwright Ceph ownership markers alongside a co-resident cluster")
	for _, idx := range []int{managedEvidenceIdx, adminEvidenceIdx, markerEvidenceIdx} {
		if strings.Contains(fmt.Sprint(evidenceTasks[idx]), "/var/lib/ceph") {
			t.Errorf("host-evidence task %q must not mix data removal into its loop, got %v", evidenceTasks[idx]["name"], evidenceTasks[idx])
		}
	}
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_destroy.yml")
	playTasks := nestedAnsibleTasks(t, plays[0], "tasks")
	attestationIdx := findAnsibleTask(t, playTasks, "Record the terminal storage destroy attestation")
	releaseIdx := findAnsibleTask(t, playTasks, "Release host ownership evidence after recording the terminal attestation")
	if attestationIdx >= releaseIdx {
		t.Fatalf("the exact terminal artifact must be written before host evidence is released (attestation=%d release=%d)", attestationIdx, releaseIdx)
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

func TestStorageCephadmDestroyFailsClosedWhenADeviceProbeDidNotRun(t *testing.T) {
	tasks := storageCephDestroyTasks(t)
	for _, probe := range []struct {
		guard    string
		consumer string
		register string
		expected string
	}{
		{
			guard:    "Refuse to destroy a storage node whose declared device probe did not run",
			consumer: "Classify declared Ceph destroy devices by presence",
			register: "bootwright_ceph_device_mounts",
			expected: "bootwright_current_storage_host.devices",
		},
		{
			guard:    "Refuse to wipe present Ceph devices whose signature probe did not run",
			consumer: "Refuse to wipe present Ceph devices without a valid Bootwright OSD record",
			register: "bootwright_ceph_destroy_signatures",
			expected: "bootwright_ceph_present_devices",
		},
		{
			guard:    "Refuse to wipe devices whose mount re-probe did not run",
			consumer: "Refuse to wipe devices mounted since the first check",
			register: "bootwright_ceph_device_mounts_final",
			expected: "bootwright_ceph_present_devices",
		},
	} {
		guardIdx := findAnsibleTask(t, tasks, probe.guard)
		consumerIdx := findAnsibleTask(t, tasks, probe.consumer)
		if guardIdx > consumerIdx {
			t.Fatalf("%q must precede %q, or the gate it protects has already read the incomplete probe (guard=%d consumer=%d)", probe.guard, probe.consumer, guardIdx, consumerIdx)
		}
		assertProbeCompletenessGuard(t, tasks[guardIdx], probe.guard, probe.register, probe.expected)
	}

	lvm := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/lvm_teardown.yml")
	guardIdx := findAnsibleTask(t, lvm, "Refuse to wipe when the volume group probe did not run for every device")
	resolveIdx := findAnsibleTask(t, lvm, "Resolve the Ceph volume groups this teardown must take down before the wipe")
	if guardIdx > resolveIdx {
		t.Fatalf("the volume group probe guard must precede the resolution that reads it (guard=%d resolve=%d)", guardIdx, resolveIdx)
	}
	assertProbeCompletenessGuard(t, lvm[guardIdx], "Refuse to wipe when the volume group probe did not run for every device", "bootwright_ceph_teardown_pvs", "bootwright_ceph_lvm_teardown_devices")
}

func assertProbeCompletenessGuard(t *testing.T, task map[string]any, name, register, expected string) {
	t.Helper()
	guard, ok := task["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%q must be an assert, got %v", name, task)
	}
	that := fmt.Sprint(guard["that"])
	for _, want := range []string{register + ".results", "selectattr('rc', 'defined')", expected, "length"} {
		if !strings.Contains(that, want) {
			t.Errorf("%q must count only %s results that carry an exit status and compare that against %s: a looped probe on a host whose connection dropped still registers one result per item, each carrying `unreachable` and no `rc`, so a count of results alone cannot tell a probe that ran from one that never reached the node. Missing %q in %v", name, register, expected, want, guard["that"])
		}
	}
}

func TestStorageCephadmDestroyProvesASeedHasNothingLeftBeforeSkippingItsRemoval(t *testing.T) {
	tasks := storageCephDestroyTasks(t)
	stateIdx := findAnsibleTask(t, tasks, "Resolve whether the seed host still carries Ceph cluster state")
	decideIdx := findAnsibleTask(t, tasks, "Decide Ceph destroy ownership on seed host")
	unprovenIdx := findAnsibleTask(t, tasks, "Resolve the unproven Ceph destroy ownership evidence on seed host")
	removeIdx := findAnsibleTask(t, tasks, "Remove cephadm cluster on the ownership authority host")
	if !(stateIdx < decideIdx && decideIdx < unprovenIdx && unprovenIdx < removeIdx) {
		t.Fatalf("the seed must resolve its local Ceph state before it decides ownership, and name its evidence before the removal that gate protects (state=%d decide=%d unproven=%d remove=%d)", stateIdx, decideIdx, unprovenIdx, removeIdx)
	}

	decide, ok := tasks[decideIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the ownership decision must be a set_fact, got %v", tasks[decideIdx])
	}
	owned := fmt.Sprint(decide["bootwright_ceph_destroy_owned"])
	for _, want := range []string{
		"bootwright_ceph_destroy_conf_check.rc is defined",
		"bootwright_ceph_destroy_local_state",
	} {
		if !strings.Contains(owned, want) {
			t.Errorf("the already-destroyed branch must require %q: a probe that never answered carries no rc and reads as \"no configuration\", which is exactly what a seed with nothing left reports, so the seed's cluster removal skips — and every other node's with it, because those are scoped to the fsid this seed resolves. Got %v", want, decide["bootwright_ceph_destroy_owned"])
		}
	}
	if strings.Contains(owned, "bootwright_ceph_fsid.stdout | default('') | trim | length == 0)\n          and") {
		t.Errorf("the already-destroyed branch must not rest on an empty `ceph fsid` answer: cephadm that cannot run answers nothing on a seed whose cluster is alive. Got %v", decide["bootwright_ceph_destroy_owned"])
	}

	unproven, ok := tasks[unprovenIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the unproven-evidence resolution must be a set_fact, got %v", tasks[unprovenIdx])
	}
	reasons := fmt.Sprint(unproven["bootwright_ceph_destroy_unproven"])
	for _, want := range []string{
		"bootwright_ceph_destroy_conf_check.rc is not defined",
		"bootwright_ceph_fsid.rc is not defined",
		"bootwright_ceph_destroy_local_state",
	} {
		if !strings.Contains(reasons, want) {
			t.Errorf("the refusal must name %q among its causes, or a seed refused for state it never proved reports an empty reason list. Got %v", want, unproven["bootwright_ceph_destroy_unproven"])
		}
	}
}

func TestStorageCephadmDestroyRunsACephadmItProvedTheHostHas(t *testing.T) {
	tasks := storageCephDestroyTasks(t)

	for _, name := range []string{
		"Remove cephadm cluster on the ownership authority host",
		"Remove cephadm cluster on non-seed hosts",
	} {
		cmd, ok := tasks[findAnsibleTask(t, tasks, name)]["ansible.builtin.command"].(map[string]any)
		if !ok {
			t.Fatalf("%q must be a command, got %v", name, tasks[findAnsibleTask(t, tasks, name)])
		}
		argv, ok := cmd["argv"].([]any)
		if !ok || len(argv) == 0 {
			t.Fatalf("%q must invoke cephadm through argv, got %v", name, cmd)
		}
		tokens := make([]string, 0, len(argv))
		for _, arg := range argv {
			tokens = append(tokens, fmt.Sprint(arg))
		}
		removal := ansibleBoundedCommand(tokens)
		if len(removal) == 0 {
			t.Fatalf("%q must bound a cephadm invocation, got %v", name, tokens)
		}
		if !strings.Contains(removal[0], "bootwright_ceph_destroy_cephadm") {
			t.Errorf("%q must run the cephadm this teardown resolved for the host, not a bare `cephadm` off PATH: cephadm needs its package only on the host it bootstrapped, so a node enrolled over SSH holds a whole cluster and still answers the binary with ENOENT, which fails the module before the removal runs. Got the bounded command %v", name, removal[0])
		}
	}

	removal := tasks[findAnsibleTask(t, tasks, "Remove cephadm cluster on non-seed hosts")]
	if got := fmt.Sprint(removal["when"]); !strings.Contains(got, "bootwright_ceph_destroy_cephadm | default('') | length > 0") {
		t.Errorf("the non-seed cluster removal must be gated on a cephadm the host can run: a gate that proves only that the host carries cluster state runs the binary regardless and fails the whole teardown on ENOENT. Got when=%v", removal["when"])
	}

	report := tasks[findAnsibleTask(t, tasks, "Report non-seed hosts that hold no Ceph cluster state to remove")]
	if got := fmt.Sprint(report["when"]); !strings.Contains(got, "bootwright_ceph_destroy_cephadm | default('') | length == 0") {
		t.Errorf("the already-removed report must be the exact complement of the removal gate, or a host falls through both and its cluster is left standing without a word. Got when=%v", report["when"])
	}
}

func TestStorageCephadmDestroyRefusesAClusterNoCephadmCanRemove(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/cephadm_command.yml"
	tasks := readAnsibleTasks(t, path)

	probeIdx := findAnsibleTask(t, tasks, "Probe this storage host for the cephadm command")
	findIdx := findAnsibleTask(t, tasks, "Find the cephadm binary this cluster deployed on a storage host without the package")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve the cephadm command that removes this cluster from a storage host")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to leave a Ceph cluster standing on a host with no cephadm to remove it")
	if !(probeIdx < findIdx && findIdx < resolveIdx && resolveIdx < refuseIdx) {
		t.Fatalf("the host must be probed for cephadm, then for the binary the cluster deployed, before the resolution the refusal reads (probe=%d find=%d resolve=%d refuse=%d)", probeIdx, findIdx, resolveIdx, refuseIdx)
	}

	find, ok := tasks[findIdx]["ansible.builtin.find"].(map[string]any)
	if !ok {
		t.Fatalf("the deployed-binary lookup must be a find, got %v", tasks[findIdx])
	}
	if got := fmt.Sprint(find["paths"]); !strings.Contains(got, "/var/lib/ceph/") || !strings.Contains(got, "bootwright_ceph_destroy_host_fsid") {
		t.Errorf("the deployed cephadm lives under the fsid directory this teardown owns, so the lookup must be scoped to it and never to another cluster's state. Got paths=%v", find["paths"])
	}

	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the no-cephadm guard must be an assert, got %v", tasks[refuseIdx])
	}
	if got := fmt.Sprint(refuse["that"]); !strings.Contains(got, "bootwright_ceph_destroy_cephadm") {
		t.Errorf("the guard must require a resolved cephadm, got that=%v", refuse["that"])
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); !strings.Contains(got, "bootwright_ceph_destroy_host_state") {
		t.Errorf("a host that carries no cluster state needs no cephadm, so the refusal must be scoped to hosts that do, got when=%v", tasks[refuseIdx]["when"])
	}
	for _, want := range []string{"rm-cluster", "--zap-osds", "systemd units"} {
		if got := fmt.Sprint(refuse["fail_msg"]); !strings.Contains(got, want) {
			t.Errorf("the refusal must say what it will not do without cephadm and what the operator can run instead, missing %q in %v", want, refuse["fail_msg"])
		}
	}
}

func TestStorageCephadmDestroyRefusesPeersWhenTheSeedResolvesNoFsid(t *testing.T) {
	tasks := storageCephDestroyTasks(t)
	scanIdx := findAnsibleTask(t, tasks, "Scan a non-seed storage host for Ceph state the seed named no cluster for")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve the Ceph clusters a non-seed storage host carries under no resolved fsid")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse a non-seed storage host still carrying a cluster the seed resolved no fsid for")
	nonSeedRmIdx := findAnsibleTask(t, tasks, "Remove cephadm cluster on non-seed hosts")
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	if !(scanIdx < resolveIdx && resolveIdx < refuseIdx && refuseIdx < nonSeedRmIdx && nonSeedRmIdx < wipeIdx) {
		t.Fatalf("a peer still carrying cluster state must be refused before the teardown removes anything (scan=%d resolve=%d refuse=%d rm=%d wipe=%d)", scanIdx, resolveIdx, refuseIdx, nonSeedRmIdx, wipeIdx)
	}
	if _, ok := tasks[refuseIdx]["ansible.builtin.assert"]; !ok {
		t.Fatalf("the refusal must be a hard assert so any_errors_fatal stops every host, got %v", tasks[refuseIdx])
	}
	when := fmt.Sprint(tasks[refuseIdx]["when"])
	for _, want := range []string{
		"inventory_hostname != bootwright_ceph_destroy_authority_host",
		"bootwright_ceph_destroy_fsid | default('') | length == 0",
	} {
		if !strings.Contains(when, want) {
			t.Errorf("the refusal must fire exactly where the seed resolved no fsid and this host is not the seed, missing %q in when=%v", want, tasks[refuseIdx]["when"])
		}
	}
	if strings.Contains(when, "authorize") {
		t.Errorf("no --authorize token may relax this: skipping it leaves a live cluster on every peer while the run reports it destroyed, got when=%v", tasks[refuseIdx]["when"])
	}
	fail := fmt.Sprint(tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)["fail_msg"])
	for _, want := range []string{"FOREIGN", "recover-ceph-ownership", "rm-cluster"} {
		if !strings.Contains(fail, want) {
			t.Errorf("the refusal must name the misclassification it prevents and both exits, missing %q in %v", want, fail)
		}
	}

	for _, name := range []string{
		"Resolve the cephadm command for the non-seed host cluster removal",
		"Remove cephadm cluster on non-seed hosts",
	} {
		got := fmt.Sprint(tasks[findAnsibleTask(t, tasks, name)]["when"])
		if !strings.Contains(got, "bootwright_ceph_destroy_fsid | default('') | length > 0") {
			t.Errorf("%q stays scoped to the seed's fsid — that is precisely why an unresolved fsid must be refused above rather than silently skipping every peer removal, got when=%v", name, got)
		}
	}
}

func TestStorageCephadmDestroyStopsTheOrchestratorBeforeAnyRemoval(t *testing.T) {
	tasks := storageCephDestroyTasks(t)
	disableIdx := findAnsibleTask(t, tasks, "Stop the Ceph orchestrator before any host removes the cluster")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to remove a cluster whose orchestrator can still redeploy it")
	seedRmIdx := findAnsibleTask(t, tasks, "Remove cephadm cluster on the ownership authority host")
	nonSeedRmIdx := findAnsibleTask(t, tasks, "Remove cephadm cluster on non-seed hosts")
	if !(disableIdx < refuseIdx && refuseIdx < seedRmIdx && seedRmIdx < nonSeedRmIdx) {
		t.Fatalf("destroy must disable the cephadm manager module and refuse on failure before the first host removes the cluster: every host purged while the module is enabled is reconciled back by a manager still running on a host the teardown has not reached, which redeploys its daemons and runs ceph-volume over the OSD devices the purge just freed (disable=%d refuse=%d seed=%d non-seed=%d)", disableIdx, refuseIdx, seedRmIdx, nonSeedRmIdx)
	}

	cmd := fmt.Sprint(tasks[disableIdx]["ansible.builtin.command"])
	for _, want := range []string{"cephadm", "shell", "mgr", "module", "disable", "timeout"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("the orchestrator stop must be a bounded `cephadm shell -- ceph mgr module disable cephadm`, missing %q in %v", want, tasks[disableIdx]["ansible.builtin.command"])
		}
	}
	assertCephMutationTimeoutOnlyFailure(t, tasks[disableIdx], "bootwright_ceph_destroy_orch_disable", "the orchestrator stop")
	for _, idx := range []int{disableIdx, refuseIdx} {
		when := fmt.Sprint(tasks[idx]["when"])
		if !strings.Contains(when, "bootwright_ceph_destroy_fsid") {
			t.Errorf("task %q must be gated on the fsid this teardown is about to remove — the same condition that gates rm-cluster — so the orchestrator stop covers every removal, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
		if !strings.Contains(when, "bootwright_ceph_destroy_owned") {
			t.Errorf("task %q must run only once ownership is proven: this teardown never touches a cluster it has not claimed, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}
	if got := fmt.Sprint(tasks[disableIdx]["when"]); strings.Contains(got, "bootwright_ceph_fsid.stdout") || strings.Contains(got, "bootwright_ceph_status_probe") {
		t.Errorf("the orchestrator stop must NOT be gated on any probe answering: a probe that misses a live cluster would let the removal race the manager that reconciles every purged host straight back, got when=%v", tasks[disableIdx]["when"])
	}
	statusProbeIdx := findAnsibleTask(t, tasks, "Probe whether the cluster still answers commands before the orchestrator stop")
	if statusProbeIdx > disableIdx {
		t.Errorf("the liveness probe must run before the disable whose refusal reads it (probe=%d disable=%d)", statusProbeIdx, disableIdx)
	}
	probeCmd := fmt.Sprint(tasks[statusProbeIdx]["ansible.builtin.command"])
	for _, want := range []string{"status", "--connect-timeout", "timeout"} {
		if !strings.Contains(probeCmd, want) {
			t.Errorf("the liveness probe must be a bounded `ceph status`: `ceph fsid` answers from the local config with no quorum at all, which reads a dead cluster as a live manager and dead-ends the teardown, missing %q in %v", want, tasks[statusProbeIdx]["ansible.builtin.command"])
		}
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); !strings.Contains(got, "bootwright_ceph_status_probe.rc") {
		t.Errorf("the refusal must stay gated on a cluster that answered a quorum-requiring status read, so a teardown of an already-dead cluster is never blocked by a manager that cannot reply, got when=%v", tasks[refuseIdx]["when"])
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); strings.Contains(got, "bootwright_ceph_fsid.stdout") {
		t.Errorf("the refusal must not read `ceph fsid` as liveness: it answers from the local config while the quorum the disable needs is gone, got when=%v", tasks[refuseIdx]["when"])
	}
	reportIdx := findAnsibleTask(t, tasks, "Report an orchestrator this teardown could not disable on an unanswering cluster")
	if reportIdx < refuseIdx {
		t.Errorf("the unanswering-cluster report must follow the refusal it complements (report=%d refuse=%d)", reportIdx, refuseIdx)
	}
	if got := fmt.Sprint(tasks[reportIdx]["ansible.builtin.debug"]); !strings.Contains(got, "settle gate") {
		t.Errorf("a disable this teardown could not confirm must name what proves the outcome instead — the settle gate — or the run reads as if nothing was left unproven, got %v", tasks[reportIdx]["ansible.builtin.debug"])
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); strings.Contains(got, "authorize") {
		t.Errorf("no --authorize token may relax the orchestrator stop: the alternative is a teardown racing the orchestrator it is removing, got when=%v", tasks[refuseIdx]["when"])
	}

	rebuild := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/rebuild.yml")
	rebuildDisableIdx := findAnsibleTask(t, rebuild, "Stop the Ceph orchestrator before the override rebuild removes the cluster")
	rebuildRefuseIdx := findAnsibleTask(t, rebuild, "Refuse an override rebuild whose orchestrator can still redeploy the cluster")
	rebuildRmIdx := findAnsibleTask(t, rebuild, "Remove existing cephadm cluster for override rebuild on every topology host")
	if !(rebuildDisableIdx < rebuildRefuseIdx && rebuildRefuseIdx < rebuildRmIdx) {
		t.Fatalf("the override rebuild removes the old cluster from every topology host and must stop the orchestrator first, or the cluster it is replacing reprovisions the disks the rebuild bootstraps onto (disable=%d refuse=%d rm=%d)", rebuildDisableIdx, rebuildRefuseIdx, rebuildRmIdx)
	}
	if got := fmt.Sprint(rebuild[rebuildDisableIdx]["when"]); !strings.Contains(got, "bootwright_ceph_override_reachable") {
		t.Errorf("the rebuild stop must run only where the old cluster answered, got when=%v", rebuild[rebuildDisableIdx]["when"])
	}
}

func TestStorageCephadmDestroyVerifiesTheWipeBeforeReleasingTheNode(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/"
	tasks := storageCephDestroyTasks(t)
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	zapIdx := findAnsibleTask(t, tasks, "Zap declared Ceph device partition tables")
	verifyIdx := findAnsibleTask(t, tasks, "Re-read the declared Ceph device signatures after the wipe")
	ranIdx := findAnsibleTask(t, tasks, "Refuse to release a storage node whose wipe verification did not run")
	signedIdx := findAnsibleTask(t, tasks, "Refuse to release a storage node whose declared device is still signed")
	stateIdx := findAnsibleTask(t, tasks, "Remove managed Ceph local data")
	if !(wipeIdx < zapIdx && zapIdx < verifyIdx && verifyIdx < ranIdx && ranIdx < signedIdx && signedIdx < stateIdx) {
		t.Fatalf("destroy must re-read every wiped device and refuse a surviving signature BEFORE it removes the local OSD ownership marker; the controller record remains until the terminal report is validated (wipe=%d zap=%d verify=%d ran=%d signed=%d state=%d)", wipeIdx, zapIdx, verifyIdx, ranIdx, signedIdx, stateIdx)
	}
	if got := fmt.Sprint(tasks[verifyIdx]["ansible.builtin.command"]); !strings.Contains(got, "--no-act") {
		t.Errorf("the verification must read signatures without touching the device, got %v", tasks[verifyIdx]["ansible.builtin.command"])
	}
	if got := fmt.Sprint(tasks[verifyIdx]["loop"]); !strings.Contains(got, "bootwright_ceph_present_devices") {
		t.Errorf("the verification must cover exactly the devices the wipe ran over, got loop=%v", tasks[verifyIdx]["loop"])
	}
	if got := fmt.Sprint(tasks[ranIdx]["ansible.builtin.assert"]); !strings.Contains(got, "selectattr('rc', 'defined')") {
		t.Errorf("a verification result that carries no exit status is not a device this teardown read, so the completeness gate must count exit statuses, got %v", tasks[ranIdx]["ansible.builtin.assert"])
	}
	for _, idx := range []int{ranIdx, signedIdx} {
		if got := fmt.Sprint(tasks[idx]["when"]); strings.Contains(got, "authorize") {
			t.Errorf("no --authorize token may relax the post-wipe verification: it reports what the disk holds now, not who owned it, got when=%v", tasks[idx]["when"])
		}
	}

	filter := readAnsibleTasks(t, base+"filter_device_reclaim.yml")
	filterTasks := nestedAnsibleTasks(t, filter[findAnsibleTask(t, filter, "Reclaim Ceph-signed disks on filter-selected OSD hosts")], "block")
	filterZapIdx := findAnsibleTask(t, filterTasks, "Zap partition tables on filter-selected OSD disks")
	filterVerifyIdx := findAnsibleTask(t, filterTasks, "Re-read the filter-selected OSD disk signatures after the wipe")
	filterSignedIdx := findAnsibleTask(t, filterTasks, "Refuse a filter-selected OSD disk that is still signed after the wipe")
	if !(filterZapIdx < filterVerifyIdx && filterVerifyIdx < filterSignedIdx) {
		t.Fatalf("the filter reclaim names no device paths, so its post-wipe re-read is the only proof its disks are clean (zap=%d verify=%d signed=%d)", filterZapIdx, filterVerifyIdx, filterSignedIdx)
	}
}

func TestStorageCephadmDestroySettlesBeforeReleasingTheNode(t *testing.T) {
	chain := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy.yml")
	settlePos := strings.Index(chain, "destroy_steps/settle_gate.yml")
	releasePos := strings.Index(chain, "destroy_steps/release_node.yml")
	filterPos := strings.Index(chain, "destroy_steps/filter_device_reclaim.yml")
	wipePos := strings.Index(chain, "destroy_steps/wipe_and_cleanup.yml")
	if settlePos < 0 || releasePos < 0 {
		t.Fatalf("the Ceph destroy chain must import settle_gate.yml and release_node.yml (settle=%d release=%d)", settlePos, releasePos)
	}
	if !(wipePos < filterPos && filterPos < settlePos && settlePos < releasePos) {
		t.Fatalf("both wipe paths must finish, then the settle gate must prove the outcome, and only then may the node be released: releasing ownership evidence between the two wipes leaves a filter-declared host no re-run can claim (wipe=%d filter=%d settle=%d release=%d)", wipePos, filterPos, settlePos, releasePos)
	}

	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/"
	settle := readAnsibleTasks(t, base+"settle_gate.yml")
	devicesIdx := findAnsibleTask(t, settle, "Resolve every device this teardown wiped on the storage node")
	unitsIdx := findAnsibleTask(t, settle, "List the Ceph daemons still running on the storage node after every wipe")
	probeIdx := findAnsibleTask(t, settle, "Refuse to release a storage node whose surviving-daemon probe did not run")
	unclassifiedIdx := findAnsibleTask(t, settle, "Resolve Ceph units whose cluster identity cannot be classified")
	daemonIdx := findAnsibleTask(t, settle, "Refuse to release a storage node a Ceph daemon outlived")
	verifyIdx := findAnsibleTask(t, settle, "Re-read every wiped Ceph device once the cluster teardown settled")
	ranIdx := findAnsibleTask(t, settle, "Refuse to release a storage node whose settled re-read did not run")
	signedIdx := findAnsibleTask(t, settle, "Refuse to release a storage node whose wiped device was signed again")
	if !(devicesIdx < unitsIdx && unitsIdx < probeIdx && probeIdx < unclassifiedIdx && unclassifiedIdx < daemonIdx && daemonIdx < verifyIdx && verifyIdx < ranIdx && ranIdx < signedIdx) {
		t.Fatalf("the settle gate must resolve the wiped set, prove its daemon probe ran, classify every surviving unit, refuse a survivor, then re-read every device and refuse a signature (devices=%d units=%d probe=%d unclassified=%d daemon=%d verify=%d ran=%d signed=%d)", devicesIdx, unitsIdx, probeIdx, unclassifiedIdx, daemonIdx, verifyIdx, ranIdx, signedIdx)
	}
	if got := fmt.Sprint(settle[unitsIdx]); !strings.Contains(got, "active,activating,deactivating") {
		t.Errorf("the surviving-unit probe must include transitions that can still launch ceph-volume, got %v", settle[unitsIdx])
	}

	if got := fmt.Sprint(settle[devicesIdx]["ansible.builtin.set_fact"]); !strings.Contains(got, "bootwright_ceph_present_devices") || !strings.Contains(got, "bootwright_ceph_filter_wiped_disks") {
		t.Errorf("the settled re-read must cover BOTH wipe paths: a filter-declared host names no device path, so the declared set alone leaves its disks unproven, got %v", settle[devicesIdx]["ansible.builtin.set_fact"])
	}
	reclaim := readRepoFile(t, base+"filter_device_reclaim.yml")
	if !strings.Contains(reclaim, "bootwright_ceph_filter_wiped_disks") {
		t.Error("the filter reclaim must record the disks it wiped, or the settle gate cannot re-read them")
	}
	if got := fmt.Sprint(settle[verifyIdx]["ansible.builtin.command"]); !strings.Contains(got, "--no-act") {
		t.Errorf("the settled verification must read signatures without touching the device, got %v", settle[verifyIdx]["ansible.builtin.command"])
	}
	if got := fmt.Sprint(settle[ranIdx]["ansible.builtin.assert"]); !strings.Contains(got, "selectattr('rc', 'defined')") {
		t.Errorf("a re-read result that carries no exit status is not a device this teardown read, so the completeness gate must count exit statuses, got %v", settle[ranIdx]["ansible.builtin.assert"])
	}
	if got := fmt.Sprint(settle[daemonIdx]["ansible.builtin.assert"]); !strings.Contains(got, "bootwright_ceph_settle_stray_fsids") || !strings.Contains(got, "bootwright_ceph_settle_unclassified_units") {
		t.Errorf("the daemon refusal must key on non-preserved fsids and unclassified units such as ceph-volume activations, got %v", settle[daemonIdx]["ansible.builtin.assert"])
	}
	ownedIdx := findAnsibleTask(t, settle, "Resolve the Ceph cluster this teardown owns on the storage node")
	preservedIdx := findAnsibleTask(t, settle, "Resolve the co-resident Ceph clusters this teardown preserves")
	strayIdx := findAnsibleTask(t, settle, "Resolve the Ceph daemons this teardown refuses to leave running")
	if !(ownedIdx < preservedIdx && preservedIdx < strayIdx && strayIdx < daemonIdx) {
		t.Fatalf("the tolerated set must be resolved before the refusal reads it (owned=%d preserved=%d stray=%d refuse=%d)", ownedIdx, preservedIdx, strayIdx, daemonIdx)
	}
	if got := fmt.Sprint(settle[ownedIdx]["ansible.builtin.set_fact"]); !strings.Contains(got, "bootwright_ceph_destroy_fsid") || !strings.Contains(got, "bootwright_ceph_destroy_authority_host") {
		t.Errorf("a non-authority host must learn the fsid only from the host whose live evidence proved this teardown's ownership, got %v", settle[ownedIdx]["ansible.builtin.set_fact"])
	}
	stateIdx := findAnsibleTask(t, settle, "Scan the storage node for the Ceph clusters this teardown preserves")
	if got := fmt.Sprint(settle[stateIdx]["ansible.builtin.find"]); !strings.Contains(got, "/var/lib/ceph") {
		t.Errorf("a surviving daemon is tolerable only when it belongs to a co-resident cluster this host still holds state for, got %v", settle[stateIdx]["ansible.builtin.find"])
	}
	if stateIdx > preservedIdx {
		t.Errorf("the preserved set must be read from the state scan that precedes it (scan=%d preserved=%d)", stateIdx, preservedIdx)
	}
	for _, idx := range []int{probeIdx, daemonIdx, ranIdx, signedIdx} {
		if got := fmt.Sprint(settle[idx]["when"]); strings.Contains(got, "authorize") {
			t.Errorf("no --authorize token may relax the settle gate: it reports what the node holds now, not who owned it, got when=%v", settle[idx]["when"])
		}
		if _, ok := settle[idx]["ansible.builtin.assert"]; !ok {
			t.Errorf("settle-gate refusal %v must be a hard assert so any_errors_fatal stops every host before any of them releases its ownership evidence", settle[idx]["name"])
		}
	}
}

func TestStorageCephadmDestroySweepsTheCephLVMTheDeviceWipeDidNotCover(t *testing.T) {
	chain := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy.yml")
	settlePos := strings.Index(chain, "destroy_steps/settle_gate.yml")
	sweepPos := strings.Index(chain, "destroy_steps/lvm_sweep.yml")
	releasePos := strings.Index(chain, "destroy_steps/release_node.yml")
	if sweepPos < 0 {
		t.Fatalf("the Ceph destroy chain must import lvm_sweep.yml: every gate before it reads the device list the run resolved for itself, so a declared device that probed absent, an OSD on a path no declaration names, and a disk re-signed after its wipe was verified all leave a green teardown with this cluster's volume groups still on the node")
	}
	if !(settlePos < sweepPos && sweepPos < releasePos) {
		t.Fatalf("the sweep must run after the settle gate proved no Ceph daemon outlived the teardown (taking LVM down under a live OSD is what the teardown refuses) and before the node is released (a refusal must keep the ownership evidence a re-run needs): settle=%d sweep=%d release=%d", settlePos, sweepPos, releasePos)
	}

	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/lvm_sweep.yml"
	sweep := readAnsibleTasks(t, path)
	scanIdx := findAnsibleTask(t, sweep, "Scan the storage node for Ceph LVM the device wipe did not cover")
	scanRanIdx := findAnsibleTask(t, sweep, "Refuse to release a storage node whose Ceph LVM scan did not run")
	rowsIdx := findAnsibleTask(t, sweep, "Resolve the Ceph volume groups the storage node still carries")
	classifyIdx := findAnsibleTask(t, sweep, "Resolve which Ceph LVM this teardown may take down on the storage node")
	teardownIdx := findAnsibleTask(t, sweep, "Take the LVM stack down on the Ceph volume groups that outlived the wipe")
	wipeIdx := findAnsibleTask(t, sweep, "Wipe the signatures of the Ceph devices that outlived the wipe")
	zapIdx := findAnsibleTask(t, sweep, "Zap the partition tables of the Ceph devices that outlived the wipe")
	verifyIdx := findAnsibleTask(t, sweep, "Re-scan the storage node for Ceph LVM once the sweep finished")
	verifyRanIdx := findAnsibleTask(t, sweep, "Refuse to release a storage node whose Ceph LVM re-scan did not run")
	proofIdx := findAnsibleTask(t, sweep, "Resolve the final whole-node Ceph LVM proof")
	survivorIdx := findAnsibleTask(t, sweep, "Refuse to release a storage node still carrying this cluster's Ceph LVM")
	if !(scanIdx < scanRanIdx && scanRanIdx < rowsIdx && rowsIdx < classifyIdx && classifyIdx < teardownIdx && teardownIdx < wipeIdx && wipeIdx < zapIdx && zapIdx < verifyIdx && verifyIdx < verifyRanIdx && verifyRanIdx < proofIdx && proofIdx < survivorIdx) {
		t.Fatalf("the sweep must scan the node, prove the scan ran, classify what it may take down, remove the LVM stack, wipe and zap the devices, then re-scan and refuse a survivor (scan=%d ran=%d rows=%d classify=%d teardown=%d wipe=%d zap=%d verify=%d verifyRan=%d proof=%d survivor=%d)", scanIdx, scanRanIdx, rowsIdx, classifyIdx, teardownIdx, wipeIdx, zapIdx, verifyIdx, verifyRanIdx, proofIdx, survivorIdx)
	}

	body := readRepoFile(t, path)
	if strings.Contains(body, "bootwright_ceph_present_devices") {
		t.Error("the sweep must read the node, not the device list every earlier gate already read: a host whose declared devices probed absent resolves an empty present set, and a sweep scoped to it proves exactly nothing")
	}
	for _, want := range []string{
		"bootwright_ceph_settle_owned_fsid",
		"bootwright_ceph_settle_preserved_fsids",
		"bootwright_current_storage_host.devices",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s must classify the volume groups it found by %s: it may take down only LVM this teardown's own cluster fsid claims or a device this node declares, and must preserve the co-resident cluster the local-state removal preserves", path, want)
		}
	}
	if got := fmt.Sprint(sweep[teardownIdx]["ansible.builtin.include_tasks"]); got != "lvm_teardown.yml" {
		t.Errorf("the sweep must take the LVM stack down through the same lvm_teardown.yml both wipe paths use, so one sequence owns vgchange/vgremove/pvremove and the fsid-scoped cluster release, got %v", sweep[teardownIdx]["ansible.builtin.include_tasks"])
	}
	if got := fmt.Sprint(sweep[zapIdx]["when"]); !strings.Contains(got, "bootwright_ceph_zap_tool_present") {
		t.Errorf("the sweep zap must degrade to the wipefs-only wipe on a host with no sgdisk, got when=%v", sweep[zapIdx]["when"])
	}
	if !strings.Contains(body, "ensure_zap_tool.yml") {
		t.Errorf("%s must reach the zap tool through ensure_zap_tool.yml: a node whose declared devices all probed absent never needed it earlier in the run, and the sweep is exactly the path that then finds disks to zap", path)
	}
	if got := fmt.Sprint(sweep[scanIdx]["ansible.builtin.script"]); !strings.Contains(got, "ceph_lvm_quiet_scan.sh 1 31 1 30 0") {
		t.Errorf("the initial scan must wait only to the first writer-free sample before mutation, got %v", sweep[scanIdx])
	}
	if got := fmt.Sprint(sweep[verifyIdx]["ansible.builtin.script"]); !strings.Contains(got, "ceph_lvm_quiet_scan.sh 3 31 1 30 2") {
		t.Errorf("the final proof must require three stable whole-node samples, got %v", sweep[verifyIdx])
	}
	if got := fmt.Sprint(sweep[proofIdx]); !strings.Contains(got, "bootwright_ceph_sweep_final_classification") || strings.Contains(got, "selectattr('pv', 'in', bootwright_ceph_sweep_devices") {
		t.Errorf("the survivor gate must classify its own fresh rows instead of filtering through the initial device set, got %v", sweep[proofIdx])
	}
	quietScan := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/files/ceph_lvm_quiet_scan.sh")
	for _, want := range []string{"required_samples", "quiet_seconds", "ceph_volume_writers", "pvs --noheadings", "lvs --noheadings", "first_rows", "stable_samples"} {
		if !strings.Contains(quietScan, want) {
			t.Errorf("the bounded quiet scanner must retain %q, got:\n%s", want, quietScan)
		}
	}
	for _, idx := range []int{scanRanIdx, verifyRanIdx, survivorIdx} {
		if _, ok := sweep[idx]["ansible.builtin.assert"]; !ok {
			t.Errorf("sweep refusal %v must be a hard assert so any_errors_fatal stops every host before any of them releases its ownership evidence", sweep[idx]["name"])
		}
		if got := fmt.Sprint(sweep[idx]["when"]); strings.Contains(got, "authorize") {
			t.Errorf("no --authorize token may relax the sweep proof: it reports what the node holds now, not who owned it, got when=%v", sweep[idx]["when"])
		}
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
	providerTask := providerTasks[findAnsibleTask(t, providerTasks, "Resolve managed Ceph provider context")]
	providerFacts, ok := providerTask["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the post-fact provider context must be resolved with set_fact, got %v", providerTask)
	}
	if _, ok := providerFacts["bootwright_ceph_bootstrap_image"]; ok {
		t.Fatalf("the provider image must have one scalar owner before full provider materialization, got %v", providerFacts)
	}
	seedContextTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/context_seed.yml")
	seedGatherIdx := findAnsibleTask(t, seedContextTasks, "Gather seed OS facts for provider rendering")
	seedProviderIdx := findAnsibleTask(t, seedContextTasks, "Resolve managed Ceph provider context on the seed host")
	manifestIdx := findAnsibleTask(t, seedContextTasks, "Load the rendered Ceph operation manifest")
	operationsIdx := findAnsibleTask(t, seedContextTasks, "Resolve rendered Ceph operations, execution plan and batch scripts")
	if !(seedGatherIdx < seedProviderIdx && seedProviderIdx < manifestIdx && manifestIdx < operationsIdx) {
		t.Fatalf("seed context must gather OS facts before provider templates, then load bootstrap operations")
	}
	seedGatherWhen := fmt.Sprint(seedContextTasks[seedGatherIdx]["when"])
	if !strings.Contains(seedGatherWhen, "ansible_distribution_major_version") {
		t.Fatalf("the conditional seed gather must test the distribution-major fact provider templates actually consume, got when=%v", seedContextTasks[seedGatherIdx]["when"])
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
	run := cephCommandTask(t, operationTasks[findAnsibleTask(t, operationTasks, "Run Ceph operation")], "Run Ceph operation")
	command, ok := run["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("Run Ceph operation has no command body")
	}
	argv := fmt.Sprint(command["argv"])
	for _, want := range []string{"'timeout'", "bootwright_ceph_timeout_kill_after_seconds", "bootwright_ceph_orchestration_timeout_seconds", "'cephadm', 'shell', '--'", "bootwright_ceph_op_command"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("Run Ceph operation must consume rendered argv inside a bounded cephadm shell, missing %q in %v", want, command["argv"])
		}
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
		base+"ownership_authority.yml",
		base+"device_gates.yml",
		base+"cluster_gate.yml",
		base+"wipe_and_cleanup.yml",
		base+"filter_device_reclaim.yml",
		base+"settle_gate.yml",
		base+"lvm_sweep.yml",
		base+"release_node.yml",
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

func TestStorageCephZapToolIsOptionalAndProbedWithoutAShellBuiltin(t *testing.T) {
	ensurePath := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/ensure_zap_tool.yml"
	ensure := readRepoFile(t, ensurePath)
	if strings.Contains(ensure, "- command\n") {
		t.Errorf("%s must not probe for sgdisk through the `command` shell builtin: ansible.builtin.command runs no shell, and most RHEL hosts ship no /usr/bin/command, so the probe reports every host as missing sgdisk and forces a package transaction", ensurePath)
	}
	for _, want := range []string{
		"- sgdisk\n      - --version",
		"rescue:",
		"bootwright_ceph_zap_tool_present",
	} {
		if !strings.Contains(ensure, want) {
			t.Errorf("%s must retain %q: the zap tool is a convenience over wipefs, so a host that cannot install gdisk must degrade to the wipefs-only wipe instead of failing the run", ensurePath, want)
		}
	}
	ensureTasks := readAnsibleTasks(t, ensurePath)
	installIdx := findAnsibleTask(t, ensureTasks, "Install the Ceph device zap tool when this host lacks it")
	if _, ok := ensureTasks[installIdx]["rescue"]; !ok {
		t.Error("the gdisk install must sit in a block with a rescue; at teardown the repositories that served the apply are routinely gone, and an uninstallable convenience package must not strand the hardware")
	}
	for _, gated := range []struct {
		path string
		name string
	}{
		{"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/wipe_and_cleanup.yml", "Zap declared Ceph device partition tables"},
		{"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/filter_device_reclaim.yml", "Zap partition tables on filter-selected OSD disks"},
		{"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml", "Zap partition tables of reclaimed OSD devices"},
	} {
		tasks := readAnsibleTasks(t, gated.path)
		idx := findAnsibleTaskIndex(tasks, gated.name)
		if idx < 0 {
			tasks = nestedAnsibleTasks(t, tasks[findAnsibleTask(t, tasks, "Reclaim Ceph-signed disks on filter-selected OSD hosts")], "block")
			idx = findAnsibleTask(t, tasks, gated.name)
		}
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "bootwright_ceph_zap_tool_present") {
			t.Errorf("%q must be gated on bootwright_ceph_zap_tool_present; without the gate a host with no sgdisk fails the wipe that wipefs already completed, got when=%v", gated.name, tasks[idx]["when"])
		}
	}
	for _, caller := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/destroy_steps/wipe_and_cleanup.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/install.yml",
	} {
		if !strings.Contains(readRepoFile(t, caller), "ensure_zap_tool.yml") {
			t.Errorf("%s must reach the zap tool through ensure_zap_tool.yml so apply and destroy share one probe, one best-effort install, and one availability fact", caller)
		}
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
	if _, ok := reclaimTop[reclaimBlockIdx]["rescue"]; ok {
		t.Fatalf("%s reclaim block must not rescue a zap-result refusal; a rescue would let the persistent OSD service run after a failed wipe", reclaimPath)
	}
	reclaimTasks := nestedAnsibleTasks(t, reclaimTop[reclaimBlockIdx], "block")
	refreshIdx := findAnsibleTask(t, reclaimTasks, "Refresh cephadm device inventory for filter-OSD hosts")
	if until := fmt.Sprint(reclaimTasks[refreshIdx]["until"]); !strings.Contains(until, "bootwright_ceph_filter_device_ls.attempts") {
		t.Errorf("%s must let the inventory poll terminate at its attempt budget: ansible marks an exhausted `until` failed regardless of `failed_when: false` (task_executor sets result['failed']=True after the retry loop), so without the escape an authorized reclaim aborts the play before the OSD spec is applied. got until=%v", reclaimPath, until)
	}
	if findAnsibleTaskIndex(reclaimTasks, "Refuse all-devices reclaim without complete refreshed inventory") < 0 {
		t.Errorf("%s must refuse when refreshed inventory is incomplete, so an authorized reclaim cannot become partial or fail open", reclaimPath)
	}
	if findAnsibleTaskIndex(reclaimTasks, "Refuse an automatic OSD reclaim whose zap failed") < 0 {
		t.Errorf("%s must refuse a failed zap before applying the persistent OSD service", reclaimPath)
	}
	for _, want := range []string{"bootwright_json_list_probe", "Refuse all-devices reclaim without readable live OSD metadata", "bootwright_apply_rebuild_invocation", "data-loss"} {
		if !strings.Contains(reclaim, want) {
			t.Errorf("%s must retain fail-closed reclaim input/remedy element %q", reclaimPath, want)
		}
	}
	zapIdx := findAnsibleTask(t, reclaimTasks, "Zap dirty filter-selected OSD devices")
	refuseZapIdx := findAnsibleTask(t, reclaimTasks, "Refuse an automatic OSD reclaim whose zap failed")
	if zapIdx >= refuseZapIdx {
		t.Fatalf("%s must inspect every zap result before the reclaim block can return to the OSD spec apply (zap=%d refusal=%d)", reclaimPath, zapIdx, refuseZapIdx)
	}
	assertCephMutationTimeoutOnlyFailure(t, reclaimTasks[zapIdx], "bootwright_ceph_filter_zap", "the per-device zap loop")
	if got := fmt.Sprint(reclaimTasks[zapIdx]["loop_control"]); !strings.Contains(got, "break_when") || !strings.Contains(got, "124") || !strings.Contains(got, "137") {
		t.Fatalf("%s must stop the zap loop on an unknown timeout outcome before changing another device, got loop_control=%v", reclaimPath, reclaimTasks[zapIdx]["loop_control"])
	}
	refuseZap, ok := reclaimTasks[refuseZapIdx]["ansible.builtin.fail"].(map[string]any)
	if !ok {
		t.Fatalf("%s zap-result gate must be a hard fail, got %v", reclaimPath, reclaimTasks[refuseZapIdx])
	}
	for _, suppressor := range []string{"failed_when", "ignore_errors", "ignore_unreachable"} {
		if _, ok := reclaimTasks[refuseZapIdx][suppressor]; ok {
			t.Fatalf("%s zap-result gate must propagate its hard failure; found %s=%v", reclaimPath, suppressor, reclaimTasks[refuseZapIdx][suppressor])
		}
	}
	for _, want := range []string{"bootwright_ceph_filter_zap.results", "rejectattr('rc', 'equalto', 0)"} {
		if got := fmt.Sprint(reclaimTasks[refuseZapIdx]["loop"]); !strings.Contains(got, want) {
			t.Fatalf("%s zap-result gate loop missing %q: %v", reclaimPath, want, reclaimTasks[refuseZapIdx]["loop"])
		}
	}
	for _, want := range []string{"host", "path", "stderr", "bootwright_apply_rebuild_invocation", "data-loss authorization"} {
		if got := fmt.Sprint(refuseZap["msg"]); !strings.Contains(got, want) {
			t.Fatalf("%s zap failure must name the device, failure, and exact intentional retry; missing %q in %v", reclaimPath, want, refuseZap["msg"])
		}
	}
	svc := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/service_specs.yml")
	reclaimIdx := strings.Index(svc, "osd_reclaim.yml")
	coreIdx := strings.Index(svc, "/mnt/core-services.yaml")
	if reclaimIdx < 0 || coreIdx < 0 || reclaimIdx > coreIdx {
		t.Error("service_specs.yml must include osd_reclaim.yml before the core-services (OSD) apply")
	}
}

func TestStorageCephadmDynamicFilterDeviceGateFailsClosed(t *testing.T) {
	gatePath := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_filter_gate.yml"
	gate := readRepoFile(t, gatePath)
	for _, want := range []string{
		"osdDeviceFilters",
		"bootwright_ceph_osd_filter_candidates",
		"ceph osd metadata",
		"ignore_unreachable: true",
		"wipefs",
		"--no-act",
		"Neither `--mode rebuild` nor `--authorize data-loss` bypasses this gate",
		"bootwright_apply_reclaim_invocation",
		"bootwright_apply_reclaim_devices",
		"bootwright_reclaim_device_operand_probe",
		"__BOOTWRIGHT_RUNTIME_RECLAIM_DEVICES_7EF51C56__",
	} {
		if !strings.Contains(gate, want) {
			t.Errorf("%s must retain dynamic filter safety element %q", gatePath, want)
		}
	}
	for _, forbidden := range []string{"\n          - zap", "\n          - --all", "\n          - --force"} {
		if strings.Contains(gate, forbidden) {
			t.Errorf("%s must stay read-only and fail closed (found %q)", gatePath, forbidden)
		}
	}

	tasks := readAnsibleTasks(t, gatePath)
	gateBlockIdx := findAnsibleTask(t, tasks, "Gate dynamically selected OSD devices before applying their service specs")
	tasks = nestedAnsibleTasks(t, tasks[gateBlockIdx], "block")
	inventoryIdx := findAnsibleTask(t, tasks, "Refuse dynamic OSD selection without complete device inventory")
	metadataIdx := findAnsibleTask(t, tasks, "Read live OSD metadata for the dynamic device gate")
	classifyIdx := findAnsibleTask(t, tasks, "Classify unavailable devices against the authored OSD filters")
	mountIdx := findAnsibleTask(t, tasks, "Probe dynamically selected unavailable devices for active mounts")
	signatureIdx := findAnsibleTask(t, tasks, "Probe unmounted dynamic OSD candidates for existing signatures")
	operandIdx := findAnsibleTask(t, tasks, "Refuse an invalid runtime OSD reclaim operand")
	templateIdx := findAnsibleTask(t, tasks, "Refuse a malformed runtime OSD reclaim invocation template")
	refuseIdx := findAnsibleTask(t, tasks, "Enforce dynamic OSD device emptiness before any reclaim")
	if !(inventoryIdx < metadataIdx && metadataIdx < classifyIdx && classifyIdx < mountIdx && mountIdx < signatureIdx && signatureIdx < operandIdx && operandIdx < templateIdx && templateIdx < refuseIdx) {
		t.Fatalf("dynamic filter gate order must be inventory -> live OSD exclusion -> classify -> mount -> signature -> validated retry operand/template -> refusal, got %d %d %d %d %d %d %d %d", inventoryIdx, metadataIdx, classifyIdx, mountIdx, signatureIdx, operandIdx, templateIdx, refuseIdx)
	}

	servicePath := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/service_specs.yml"
	service := readRepoFile(t, servicePath)
	filterIdx := strings.Index(service, "osd_filter_gate.yml")
	reclaimIdx := strings.Index(service, "osd_reclaim.yml")
	applyIdx := strings.Index(service, "/mnt/core-services.yaml")
	if filterIdx < 0 || reclaimIdx < 0 || applyIdx < 0 || !(filterIdx < reclaimIdx && reclaimIdx < applyIdx) {
		t.Fatalf("%s must execute the read-only dynamic gate before auto-reclaim and the persistent OSD service apply", servicePath)
	}
	if !strings.Contains(service, "selectattr('osdDeviceFilters', 'defined')") {
		t.Fatalf("%s must require device inventory for every dynamic-filter host", servicePath)
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
	monIdx := strings.Index(boot, "bootstrap_steps/mon_readiness.yml")
	if monIdx < 0 || monIdx > readinessIdx {
		t.Errorf("bootstrap.yml must run the monmap gate before the OSD gate: mon_readiness.yml is the only step that collects `ceph log last 100 cephadm` and carries the `Failed to extract uid/gid` and `Filtered out host` detectors, and an OSD gate firing first aborts the play before that evidence is ever gathered (mon=%d osd=%d)", monIdx, readinessIdx)
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
	for _, want := range []string{"data_devices.all=true", "bootwright_apply_rebuild_invocation", "data-loss authorization", "IRREVERSIBLE", "protectedKinds"} {
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

	orchestrator := fmt.Sprint(remedyVars["bootwright_ceph_osd_remedy_orchestrator"])
	for _, want := range []string{"ceph health detail", "ceph log last 200 cephadm", "ceph orch host ls --detail", "ceph mgr fail"} {
		if !strings.Contains(orchestrator, want) {
			t.Errorf("the zero-unavailable readiness remedy must name %q: osd_reclaim.yml selects only devices cephadm reports as available=false, so with none unavailable every reclaim path has an empty candidate set and only the orchestrator can be at fault; got %v", want, orchestrator)
		}
	}
	for _, forbidden := range []string{"--authorize data-loss", "wipefs --all", "sgdisk --zap-all"} {
		if strings.Contains(orchestrator, forbidden) {
			t.Errorf("the zero-unavailable readiness remedy must not offer %q; it is what told the operator to wipe clean disks, got %v", forbidden, orchestrator)
		}
	}
	if !strings.Contains(orchestrator, "Do NOT wipe") {
		t.Errorf("the zero-unavailable readiness remedy must say plainly that no disk is to be wiped on this evidence, got %v", orchestrator)
	}
	selection := fmt.Sprint(tasks[remedyIdx]["ansible.builtin.set_fact"].(map[string]any)["bootwright_ceph_osd_readiness_remedy"])
	if !strings.Contains(selection, "bootwright_ceph_osd_remedy_orchestrator") ||
		!strings.Contains(selection, "bootwright_ceph_osd_declared_unavailable_total") ||
		!strings.Contains(selection, "bootwright_ceph_osd_reclaim_all_unavailable_total") {
		t.Errorf("the readiness remedy must be chosen from the observed availability counts, not from osdReclaimAll alone, got %v", selection)
	}
	if strings.Index(selection, "bootwright_ceph_osd_remedy_orchestrator") > strings.Index(selection, "bootwright_ceph_osd_remedy_reclaim_all") {
		t.Errorf("the orchestrator remedy must be selected first, so a clean-disk cluster never reaches a reclaim text, got %v", selection)
	}
}

func TestStorageOSDReadinessBudgetScalesWithTheDeclaredOSDCount(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_readiness.yml"
	tasks := readAnsibleTasks(t, path)

	resolveIdx := findAnsibleTask(t, tasks, "Resolve OSD readiness expectation")
	budgetIdx := findAnsibleTask(t, tasks, "Resolve the OSD readiness attempt budget")
	deadlineIdx := findAnsibleTask(t, tasks, "Set the OSD readiness wall-clock deadline")
	waitIdx := findAnsibleTask(t, tasks, "Wait for declared Ceph OSDs to be created and in")
	if !(resolveIdx < budgetIdx && budgetIdx < deadlineIdx && deadlineIdx < waitIdx) {
		t.Fatalf("the attempt budget and its wall-clock deadline must be resolved from the expectation before the poll uses them (resolve=%d budget=%d deadline=%d wait=%d)", resolveIdx, budgetIdx, deadlineIdx, waitIdx)
	}
	deadline, ok := tasks[deadlineIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the OSD readiness deadline must be a set_fact, got %v", tasks[deadlineIdx])
	}
	if got := fmt.Sprint(deadline["bootwright_ceph_osd_readiness_deadline"]); !strings.Contains(got, "now(utc=true).timestamp()") {
		t.Errorf("the OSD readiness deadline must be an absolute wall-clock stamp taken once, so the `until` clauses compare against a fixed instant, got %v", got)
	}
	deadlineVars, ok := tasks[deadlineIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("the OSD readiness deadline must derive its budget from a vars block, got %v", tasks[deadlineIdx])
	}
	wallBudget := fmt.Sprint(deadlineVars["bootwright_ceph_osd_readiness_wall_budget"])
	for _, want := range []string{"bootwright_ceph_osd_readiness_wall_seconds", "bootwright_ceph_osd_readiness_attempts", "bootwright_ceph_osd_readiness_delay"} {
		if !strings.Contains(wallBudget, want) {
			t.Errorf("the OSD readiness wall-clock budget must scale with the attempt budget and stay overridable, missing %q: each attempt is capped at 120s by `timeout`, so 180 attempts against a cephadm that answers slowly rather than hanging costs hours where the attempt count alone promises minutes. got %v", want, wallBudget)
		}
	}
	budget := fmt.Sprint(tasks[budgetIdx]["ansible.builtin.set_fact"].(map[string]any)["bootwright_ceph_osd_readiness_attempts"])
	if !strings.Contains(budget, "bootwright_ceph_osd_readiness_retries") {
		t.Errorf("the attempt budget must stay overridable through bootwright_ceph_osd_readiness_retries, got %v", budget)
	}
	if !strings.Contains(budget, "bootwright_ceph_osd_expected_count") {
		t.Errorf("the attempt budget must scale with the declared OSD count: the OSD gate waits on a device scan across every OSD host and then one ceph-volume run per OSD, so a 30-OSD cluster needs longer than a 3-OSD one, got %v", budget)
	}
	if !strings.Contains(budget, "90") || !strings.Contains(budget, "max") {
		t.Errorf("the attempt budget must keep a floor of at least 90 attempts: at 30 attempts x 10s the gate gave up ~3.5 minutes before cephadm deployed the OSDs, and it must also outlast a cephadm mgr restart, got %v", budget)
	}
	if got := fmt.Sprint(tasks[waitIdx]["retries"]); !strings.Contains(got, "bootwright_ceph_osd_readiness_attempts") {
		t.Errorf("the OSD poll must take its retry count from the resolved budget, got retries=%v", got)
	}

	for _, name := range []string{
		"Wait for declared Ceph OSDs to be created and in",
		"Wait for an in OSD on every dynamic-selection host",
		"Collect Ceph device inventory when OSDs did not become ready",
		"Collect machine-readable device availability when OSDs did not become ready",
		"Collect declared OSD service status when OSDs did not become ready",
		"Collect Ceph orchestrator host status when OSDs did not become ready",
		"Collect Ceph daemon placement when OSDs did not become ready",
	} {
		command, ok := tasks[findAnsibleTask(t, tasks, name)]["ansible.builtin.command"].(map[string]any)
		if !ok {
			t.Fatalf("%q must be a command probe", name)
		}
		argv := fmt.Sprint(command["argv"])
		if !strings.HasPrefix(argv, "[timeout ") || !strings.Contains(argv, "--kill-after") {
			t.Errorf("%q must bound each attempt with `timeout --kill-after=<n>`: ansible.cfg's `timeout` is the SSH connection timeout, not a task timeout, so a wedged `cephadm shell` blocks the attempt forever and the retry budget never advances. got argv=%v", name, argv)
		}
	}

	body := readRepoFile(t, path)
	if strings.Contains(body, "default('{}')") || strings.Contains(body, "default('[]')") {
		t.Error("every from_json guard in the OSD gate must be default('<json>', true): the one-argument default does not substitute for an empty string, so an rc==0-with-empty-stdout attempt raises inside the `until` conditional instead of retrying")
	}
}

func TestStorageOSDReadinessCountsDeclaredDevicesNotReturnedRows(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_readiness.yml"
	tasks := readAnsibleTasks(t, path)

	summaryIdx := findAnsibleTask(t, tasks, "Summarise declared OSD device availability for the readiness failure")
	summaryVars, ok := tasks[summaryIdx]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("the availability summary must be composed from vars, got %v", tasks[summaryIdx])
	}
	declaredHosts := fmt.Sprint(summaryVars["bootwright_ceph_osd_declared_osd_host_entries"])
	if !strings.Contains(declaredHosts, "bootwright_selected_storage_cluster.hosts") {
		t.Errorf("the declared-device denominator must come from the rendered declaration, not from the rows `ceph orch device ls` happened to return — counting the returned rows is what printed \"0 of 5 device(s) are UNAVAILABLE\" for a 30-device declaration; got %v", declaredHosts)
	}
	for _, want := range []string{"rejectattr('devices', 'equalto', none)", "rejectattr('devices', 'equalto', [])"} {
		if !strings.Contains(declaredHosts, want) {
			t.Errorf("the declared OSD host set must keep only hosts carrying a non-empty devices list, which is also what excludes the mon-only arbiter node, missing %q: %v", want, declaredHosts)
		}
	}
	summary := fmt.Sprint(tasks[summaryIdx]["ansible.builtin.set_fact"].(map[string]any))
	for _, want := range []string{
		"bootwright_ceph_osd_uninventoried_hosts",
		"bootwright_ceph_osd_declared_device_total",
		"bootwright_ceph_osd_declared_inventoried_total",
		"bootwright_ceph_osd_declared_unavailable_total",
		"bootwright_ceph_osd_declared_available_total",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("the availability summary must publish %q: an uninventoried declared device is not a rejected declared device and the message must never conflate them, got %v", want, summary)
		}
	}
	counted := fmt.Sprint(summaryVars["bootwright_ceph_osd_availability_counted"])
	for _, want := range []string{"no device inventory for", "did inventory", "never inventoried at all"} {
		if !strings.Contains(counted, want) {
			t.Errorf("the availability line must separate the never-inventoried, unavailable and available counts, missing %q: %v", want, counted)
		}
	}

	staleIdx := findAnsibleTask(t, tasks, "Resolve the age of the cephadm orchestrator cache when OSDs did not become ready")
	if staleIdx > summaryIdx {
		t.Fatalf("the orchestrator cache age must be resolved before the summary renders it (stale=%d summary=%d)", staleIdx, summaryIdx)
	}
	stale := fmt.Sprint(tasks[staleIdx]["vars"].(map[string]any)["bootwright_ceph_osd_refresh_stamps"])
	if !strings.Contains(stale, "last_refresh") {
		t.Errorf("`ceph orch ps` and `ceph orch device ls` are served from the cephadm mgr cache, so the verdict must read the last_refresh stamps it carries and report their age, got %v", stale)
	}
	dated := fmt.Sprint(summaryVars["bootwright_ceph_osd_staleness_dated"])
	if !strings.Contains(dated, "STALE") || !strings.Contains(dated, "ceph mgr fail") {
		t.Errorf("a cache that did not advance across the whole wait must be called out plainly, so a restarting cephadm mgr is never again read as a dead cluster, got %v", dated)
	}

	failMsg := fmt.Sprint(tasks[findAnsibleTask(t, tasks, "Assert declared Ceph OSDs were created")]["ansible.builtin.assert"].(map[string]any)["fail_msg"])
	if !strings.Contains(failMsg, "bootwright_ceph_osd_orch_staleness") {
		t.Errorf("the readiness failure must render the orchestrator cache age, got fail_msg=%v", failMsg)
	}
	if !strings.Contains(failMsg, "bootwright_ceph_osd_service_breakdown") {
		t.Errorf("the readiness failure must render the per-service running counts already collected by `ceph orch ls --service_type osd`, got fail_msg=%v", failMsg)
	}
	if !strings.Contains(failMsg, "{% if bootwright_ceph_osd_uninventoried_hosts") && !strings.Contains(failMsg, "{% if (bootwright_ceph_osd_uninventoried_hosts") {
		t.Errorf("the `the orchestrator has not scanned the hosts ... ceph mgr fail` hint must be gated on the declared OSD hosts cephadm holds no inventory for, not on the whole device table being empty — one seed row suppressed it. got fail_msg=%v", failMsg)
	}
	if !strings.Contains(failMsg, "ceph-volume was never invoked") {
		t.Errorf("the readiness failure must stop asserting that ceph-volume rejected the devices when no declared device is unavailable, got fail_msg=%v", failMsg)
	}
}

func TestStorageCephGatesTheDeviceScanBeforeTheOSDSpecApply(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/service_specs.yml"
	tasks := readAnsibleTasks(t, path)

	hostApplyIdx := findAnsibleTask(t, tasks, "Apply Ceph host, mon, and mgr specs")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve the declared OSD hosts the device scan must reach")
	deadlineIdx := findAnsibleTask(t, tasks, "Set the device-scan wall-clock deadline")
	waitIdx := findAnsibleTask(t, tasks, "Wait for cephadm to inventory every declared OSD host before the OSD apply")
	missingIdx := findAnsibleTask(t, tasks, "Resolve the declared OSD hosts cephadm has not inventoried")
	reportIdx := findAnsibleTask(t, tasks, "Report declared OSD hosts whose device inventory never appeared")
	osdApplyIdx := findAnsibleTask(t, tasks, "Apply Ceph OSD service spec")
	if !(hostApplyIdx < resolveIdx && resolveIdx < deadlineIdx && deadlineIdx < waitIdx &&
		waitIdx < missingIdx && missingIdx < reportIdx && reportIdx < osdApplyIdx) {
		t.Fatalf("the device-scan gate must sit between the host document apply and the OSD spec apply (host=%d resolve=%d deadline=%d wait=%d missing=%d report=%d osd=%d)", hostApplyIdx, resolveIdx, deadlineIdx, waitIdx, missingIdx, reportIdx, osdApplyIdx)
	}

	resolve, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the device-scan gate must resolve its inputs with set_fact, got %v", tasks[resolveIdx])
	}
	scanAttempts := fmt.Sprint(resolve["bootwright_ceph_osd_scan_attempts"])
	for _, want := range []string{"bootwright_ceph_osd_scan_retries", "expectedCount", "90", "max"} {
		if !strings.Contains(scanAttempts, want) {
			t.Errorf("the device-scan budget must be derived the same way as the OSD readiness budget, missing %q: a fixed budget shorter than the readiness gate's aborts a rebuild whose device scan is merely slow, before the gate that would have waited it out. got %v", want, scanAttempts)
		}
	}
	if got := fmt.Sprint(resolve["bootwright_ceph_osd_scan_mode"]); !strings.Contains(got, "osdReadiness") || !strings.Contains(got, "mode") {
		t.Errorf("the device-scan gate must resolve the OSD readiness mode from the declaration so it can stand down on a cluster that opted out of every OSD gate, got %v", got)
	}

	deadline, ok := tasks[deadlineIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the device-scan wall-clock deadline must be a set_fact, got %v", tasks[deadlineIdx])
	}
	if got := fmt.Sprint(deadline["bootwright_ceph_osd_scan_deadline"]); !strings.Contains(got, "now(utc=true).timestamp()") {
		t.Errorf("the device-scan deadline must be an absolute wall-clock stamp, got %v", got)
	}

	wait := tasks[waitIdx]
	command, ok := wait["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("the device-scan gate must poll with a command probe, got %v", wait)
	}
	argv := fmt.Sprint(command["argv"])
	for _, want := range []string{"timeout", "--kill-after", "device", "ls", "--format", "json", "--refresh"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("the device-scan gate argv missing %q: %v", want, argv)
		}
	}
	if wait["changed_when"] != false {
		t.Fatalf("the device-scan gate must be a read-only retry probe, got changed_when=%v", wait["changed_when"])
	}
	assertCephProbeTimeoutOnlyFailure(t, wait, "bootwright_ceph_osd_scan_device_ls", "the device-scan gate")
	if got := fmt.Sprint(wait["retries"]); !strings.Contains(got, "bootwright_ceph_osd_scan_attempts") {
		t.Errorf("the device-scan poll must take its retry count from the derived budget, got retries=%v", got)
	}
	for _, task := range []map[string]any{wait, tasks[missingIdx]} {
		if got := fmt.Sprint(task["when"]); !strings.Contains(got, "bootwright_ceph_osd_scan_mode != 'skip'") {
			t.Errorf("the device-scan gate must stand down on a cluster whose OSD readiness mode is skip: an unmanaged OSD node still renders a devices list, so the declared-host set is non-empty and the scan would otherwise poll for an inventory nobody waits on. got when=%v", got)
		}
	}
	until := fmt.Sprint(wait["until"])
	for _, want := range []string{"bootwright_ceph_declared_osd_hosts", "rejectattr('devices', 'equalto', [])", "bootwright_ceph_osd_scan_device_ls.attempts", "bootwright_ceph_osd_scan_attempts", "bootwright_ceph_osd_scan_deadline"} {
		if !strings.Contains(until, want) {
			t.Fatalf("the device-scan gate must wait for a non-empty inventory on every declared OSD host and escape at its own attempt budget (ansible marks an exhausted `until` failed regardless of failed_when: false) and at a wall-clock deadline, missing %q: %v", want, until)
		}
	}
	if !strings.Contains(until, "now(utc=true).timestamp()") {
		t.Fatalf("the device-scan `until` must escape on wall-clock time: every attempt is capped at 300s by `timeout`, so an attempt budget alone bounds a slow cephadm at hours. got %v", until)
	}

	if _, asserted := tasks[reportIdx]["ansible.builtin.assert"]; asserted {
		t.Fatalf("the device-scan verdict must fail open: the OSD readiness gate is the single fail-closed point for a cluster's OSDs, it waits on a budget of its own, and it already reports an unscanned host as unexamined rather than dirty. A second fail-closed gate here aborts the run before that evidence is ever collected. got %v", tasks[reportIdx])
	}
	report, ok := tasks[reportIdx]["ansible.builtin.debug"].(map[string]any)
	if !ok {
		t.Fatalf("the device-scan verdict must be a debug that lets the run proceed, got %v", tasks[reportIdx])
	}
	msg := fmt.Sprint(report["msg"])
	if !strings.Contains(msg, "bootwright_ceph_osd_scan_missing_hosts") {
		t.Fatalf("the device-scan verdict must name the hosts cephadm has not inventoried; `ceph orch host ls` lists a host the instant cephadm accepts the document, before it has ever connected to it, so the host read-back alone proves nothing. got msg=%v", msg)
	}
	if !strings.Contains(msg, "readiness gate") {
		t.Errorf("the device-scan verdict must say which gate does fail closed, so failing open here is not read as the condition being ignored, got msg=%v", msg)
	}
	if got := fmt.Sprint(tasks[reportIdx]["when"]); !strings.Contains(got, "bootwright_ceph_osd_scan_missing_hosts") {
		t.Errorf("the device-scan verdict must render only when a declared OSD host was never inventoried, got when=%v", got)
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
	assembled := fmt.Sprint(assemble["ansible.builtin.set_fact"])
	for _, want := range []string{"'ssl': true", "'ssl_cert'", "'ssl_key'", "item.serviceID"} {
		if !strings.Contains(assembled, want) {
			t.Fatalf("certmgr-era cephadm treats inline ingress TLS as the ssl_cert/ssl_key pair with ssl enabled and never writes haproxy.pem from a lone combined bundle, so the assembly must carry %s, got %v", want, assemble["ansible.builtin.set_fact"])
		}
	}
	pem := fmt.Sprint(assemble["vars"])
	if !strings.Contains(pem, "certificatePath") || !strings.Contains(pem, "keyPath") {
		t.Fatalf("the certificate and key must be read from their own secret paths, got vars=%v", assemble["vars"])
	}
	if strings.Contains(pem, "join(") {
		t.Fatalf("the certificate and key must stay separate fields; a joined bundle renders a haproxy.cfg whose ssl directive references a haproxy.pem cephadm never writes, got vars=%v", assemble["vars"])
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
	assertCephSpecFileIsMultiDocument(t, "RGW ingress TLS", "bootwright_ceph_rgw_ingress_tls_specs", copyBody["content"])
	apply := cephCommandTask(t, tasks[applyIdx], "RGW ingress TLS apply")
	if _, ok := apply["loop"]; ok {
		t.Fatalf("RGW ingress TLS must run one ceph orch apply, got loop=%v", apply["loop"])
	}
	if got := fmt.Sprint(apply["ansible.builtin.command"]); !strings.Contains(got, "/mnt/rgw-ingress-tls.yaml") {
		t.Fatalf("RGW ingress TLS apply must consume the merged spec file, got %v", apply["ansible.builtin.command"])
	}
	assertCephSpecApplyIsVerified(t, tasks, "Refuse an RGW ingress TLS spec cephadm reported an error for", applyIdx, "bootwright_ceph_rgw_ingress_tls_apply")
}

func assertCephSpecFileIsMultiDocument(t *testing.T, label, fact string, content any) {
	t.Helper()
	got := fmt.Sprint(content)
	if !strings.Contains(got, "map('to_nice_yaml')") || !strings.Contains(got, "join('---\n')") {
		t.Fatalf("%s must be written as one YAML document per service; `ceph orch apply -i` reads the file with safe_load_all and refuses a document that is a sequence rather than a mapping (\"Service Spec is not an (JSON or YAML) object\"), got content=%v", label, content)
	}
	if strings.Contains(got, fact+" | to_nice_yaml") {
		t.Fatalf("%s must not serialize the whole list into one document, got content=%v", label, content)
	}
}

func cephCommandTask(t *testing.T, task map[string]any, label string) map[string]any {
	t.Helper()
	if _, ok := task["ansible.builtin.command"].(map[string]any); ok {
		return task
	}
	if _, ok := task["block"].([]any); !ok {
		t.Fatalf("%s has neither a command nor a protected command block: %v", label, task)
	}
	var found map[string]any
	for _, candidate := range nestedAnsibleTasks(t, task, "block") {
		if _, ok := candidate["ansible.builtin.command"].(map[string]any); !ok {
			continue
		}
		if found != nil {
			t.Fatalf("%s has more than one command in its protected block", label)
		}
		found = candidate
	}
	if found == nil {
		t.Fatalf("%s protected block has no command", label)
	}
	return found
}

func assertCephSpecApplyIsVerified(t *testing.T, tasks []map[string]any, refusal string, applyIdx int, register string) {
	t.Helper()
	apply := cephCommandTask(t, tasks[applyIdx], refusal)
	assertCephMutationTimeoutOnlyFailure(t, apply, register, refusal)
	if fmt.Sprint(apply["register"]) != register {
		t.Fatalf("%q reads %s, so the apply must register it, got register=%v", refusal, register, apply["register"])
	}
	assertRedactsByDefault(t, refusal+" (the apply it reads)", apply["no_log"])
	refuseIdx := findAnsibleTask(t, tasks, refusal)
	if refuseIdx < applyIdx {
		t.Fatalf("%q must follow the apply it reads (refuse=%d apply=%d)", refusal, refuseIdx, applyIdx)
	}
	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("%q must be an assert, got %v", refusal, tasks[refuseIdx])
	}
	that := fmt.Sprint(refuse["that"])
	for _, want := range []string{register + ".rc | default(1) | int == 0", register + ".stdout", register + ".stderr"} {
		if !strings.Contains(that, want) {
			t.Fatalf("%q must refuse a rejected document cephadm exited zero for (missing %q), got that=%v", refusal, want, refuse["that"])
		}
	}
	msg := fmt.Sprint(refuse["fail_msg"])
	if !strings.Contains(msg, "cephadm said:") {
		t.Fatalf("%q must quote what cephadm printed, got fail_msg=%v", refusal, refuse["fail_msg"])
	}
	for _, raw := range []string{register + ".stdout", register + ".stderr"} {
		if strings.Contains(msg, raw) {
			t.Fatalf("cephadm echoes the document it rejected, certificate and client secret included, so %q must quote a redacted copy rather than %s", refusal, raw)
		}
	}
	redaction := fmt.Sprint(tasks[refuseIdx]["vars"])
	for _, want := range []string{register + ".stdout", register + ".stderr", `regex_replace('got "[\s\S]*'`, "regex_replace('-----BEGIN[\\s\\S]*'"} {
		if !strings.Contains(redaction, want) {
			t.Fatalf("%q must strip the rejected document and any PEM block out of cephadm's output (missing %q), got vars=%v", refusal, want, tasks[refuseIdx]["vars"])
		}
	}
	if _, ok := tasks[refuseIdx]["no_log"]; ok {
		t.Fatalf("%q must stay visible; the output it quotes is redacted at the source", refusal)
	}
}

func assertCephMutationTimeoutOnlyFailure(t *testing.T, task map[string]any, register, label string) {
	t.Helper()
	want := register + ".rc | default(0) | int in [124, 137]"
	if got := fmt.Sprint(task["failed_when"]); got != want {
		t.Fatalf("%s must defer ordinary command errors to its tailored refusal but propagate timeout rc 124/137 to the runner, got failed_when=%v, want %q", label, task["failed_when"], want)
	}
}

func assertCephProbeTimeoutOnlyFailure(t *testing.T, task map[string]any, register, label string) {
	t.Helper()
	want := register + ".rc | default(0) | int in [124, 137]"
	if got := fmt.Sprint(task["failed_when"]); got != want {
		t.Fatalf("%s must tolerate ordinary probe absence but propagate timeout rc 124/137 to the runner, got failed_when=%v, want %q", label, task["failed_when"], want)
	}
}

func TestStorageManagementSpecAppliesOneMultiDocumentSpec(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/management_services.yml"
	tasks := readAnsibleTasks(t, path)
	assemble := fmt.Sprint(tasks[findAnsibleTask(t, tasks, "Assemble management service specs")]["ansible.builtin.set_fact"])
	if strings.Contains(assemble, "ssl_certificate") {
		t.Fatalf("cephadm's MgmtGatewaySpec and OAuth2ProxySpec take the certificate as ssl_cert/ssl_key; ssl_certificate/ssl_certificate_key is refused as an unexpected keyword argument, got %v", assemble)
	}
	for _, want := range []string{"'ssl_cert'", "'ssl_key'"} {
		if !strings.Contains(assemble, want) {
			t.Fatalf("the management service spec must supply the authored certificate as %s, got %v", want, assemble)
		}
	}
	write := tasks[findAnsibleTask(t, tasks, "Write management service spec")]
	copyBody, ok := write["ansible.builtin.copy"].(map[string]any)
	if !ok {
		t.Fatalf("the management service spec must be written with copy, got %v", write)
	}
	assertCephSpecFileIsMultiDocument(t, "the management service spec", "bootwright_ceph_management_specs", copyBody["content"])
	applyIdx := findAnsibleTask(t, tasks, "Apply management service spec")
	apply := cephCommandTask(t, tasks[applyIdx], "management service spec apply")
	if got := fmt.Sprint(apply["until"]); !strings.Contains(got, "attempts") {
		t.Fatalf("a retried apply that no longer fails on its own must let its attempts run out, or the refusal that reads it never runs, got until=%v", apply["until"])
	}
	assertCephSpecApplyIsVerified(t, tasks, "Refuse a management service spec cephadm reported an error for", applyIdx, "bootwright_ceph_management_apply")
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

func TestStorageManagementSpecGatesTheCephRelease(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/management_services.yml"
	tasks := readAnsibleTasks(t, path)
	probeIdx := findAnsibleTask(t, tasks, "Read the Ceph release the daemons run")
	majorIdx := findAnsibleTask(t, tasks, "Resolve the lowest Ceph major release running a manager daemon")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse a management gateway on a Ceph release that has no such service")
	assembleIdx := findAnsibleTask(t, tasks, "Assemble management service specs")
	if !(probeIdx < majorIdx && majorIdx < refuseIdx && refuseIdx < assembleIdx) {
		t.Fatalf("the release gate must refuse before the spec is assembled and applied (probe=%d major=%d refuse=%d assemble=%d)", probeIdx, majorIdx, refuseIdx, assembleIdx)
	}
	if got := fmt.Sprint(tasks[probeIdx]["ansible.builtin.command"]); !strings.Contains(got, "versions") {
		t.Fatalf("the release gate must read the daemon versions the mgr actually runs, not the client in the shell image, got %v", tasks[probeIdx]["ansible.builtin.command"])
	}
	if tasks[probeIdx]["changed_when"] != false {
		t.Fatalf("the version probe must be read-only, got %v", tasks[probeIdx])
	}
	assertCephProbeTimeoutOnlyFailure(t, tasks[probeIdx], "bootwright_ceph_daemon_versions", "the daemon version probe")
	if got := fmt.Sprint(tasks[majorIdx]["ansible.builtin.set_fact"]); !strings.Contains(got, ".mgr") || !strings.Contains(got, "min") {
		t.Fatalf("the gate must take the LOWEST major among the mgr daemons; a half-upgraded cluster still has a mgr that refuses the document, got %v", tasks[majorIdx]["ansible.builtin.set_fact"])
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); !strings.Contains(got, "> 0") {
		t.Fatalf("an unreadable version must fall through to the apply-time refusal rather than block the run, got when=%v", tasks[refuseIdx]["when"])
	}
	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the release gate must be an assert, got %v", tasks[refuseIdx])
	}
	floor := fmt.Sprintf("default(%d)", cephprovider.MgmtGatewayMinimumCephMajor)
	if got := fmt.Sprint(refuse["that"]); !strings.Contains(got, floor) {
		t.Fatalf("the apply-time floor must match cephprovider.MgmtGatewayMinimumCephMajor (%s), or validate and apply disagree about which releases carry the service, got that=%v", floor, refuse["that"])
	}
	if got := fmt.Sprint(refuse["fail_msg"]); !strings.Contains(got, "spec.ceph.release") {
		t.Fatalf("the refusal must name the field the operator has to change, got fail_msg=%v", refuse["fail_msg"])
	}
	apply := cephCommandTask(t, tasks[findAnsibleTask(t, tasks, "Apply management service spec")], "management service spec apply")
	until := fmt.Sprint(apply["until"])
	for _, want := range []string{"unexpected keyword argument", "not an \\(JSON or YAML\\) object", "SpecValidationError"} {
		if !strings.Contains(until, want) {
			t.Fatalf("a spec cephadm refuses on shape never becomes valid, so the retry loop must stop on %q instead of burning every attempt, got until=%v", want, apply["until"])
		}
	}
}

func TestStorageManagementSpecRepairsThePersistedSSLSwitch(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/management_services.yml"
	tasks := readAnsibleTasks(t, path)
	applyIdx := findAnsibleTask(t, tasks, "Apply management service spec")
	readIdx := findAnsibleTask(t, tasks, "Read the persisted management gateway spec once cephadm settles it")
	repairIdx := findAnsibleTask(t, tasks, "Re-persist the ssl switch the cephadm spec store serialized away")
	verifyIdx := findAnsibleTask(t, tasks, "Read back the persisted management gateway spec after the repair")
	assertIdx := findAnsibleTask(t, tasks, "Assert the persisted management gateway spec keeps ssl disabled")
	if !(applyIdx < readIdx && readIdx < repairIdx && repairIdx < verifyIdx && verifyIdx < assertIdx) {
		t.Fatalf("the persisted-spec repair must run after the apply and prove the stored copy afterwards (apply=%d read=%d repair=%d verify=%d assert=%d)", applyIdx, readIdx, repairIdx, verifyIdx, assertIdx)
	}
	readUntil := fmt.Sprint(tasks[readIdx]["until"])
	if !strings.Contains(readUntil, "needs_configuration") || !strings.Contains(readUntil, "attempts") || tasks[readIdx]["retries"] == nil {
		t.Fatalf("the store read must poll until the envelope reports needs_configuration false: the orch apply itself schedules one more store rewrite — the serve pass ends with mark_configured, which re-serializes the in-memory spec through the falsy-dropping serializer — so a repair written before the store settles is silently clobbered minutes after the apply goes green, got until=%v retries=%v", tasks[readIdx]["until"], tasks[readIdx]["retries"])
	}
	repairWhen := fmt.Sprint(tasks[repairIdx]["when"])
	if !strings.Contains(repairWhen, "sslDisabled") || !strings.Contains(repairWhen, "'ssl' not in") || !strings.Contains(repairWhen, "needs_configuration") {
		t.Fatalf("the repair must run only for a vendor gateway whose SETTLED stored spec lost the ssl key; the vendor spec serializer drops falsy fields on persistence while the in-memory spec stays correct, and a mgr failover reloads the stored copy with ssl back at its class default true, got when=%v", tasks[repairIdx]["when"])
	}
	repairCmd := fmt.Sprint(tasks[repairIdx]["ansible.builtin.command"])
	if !strings.Contains(repairCmd, "config-key") || !strings.Contains(repairCmd, "'ssl': false") {
		t.Fatalf("the repair must write the stored spec back through config-key with ssl false injected into the inner spec block, got %v", tasks[repairIdx]["ansible.builtin.command"])
	}
	for _, idx := range []int{verifyIdx, assertIdx} {
		if got := fmt.Sprint(tasks[idx]["when"]); strings.Contains(got, "persisted.rc") {
			t.Fatalf("the verify read and the closing assert must not gate on the first read's rc — a transient read failure would then skip the proof and the apply would go green with the store still poisoned, the exact fail-open the assert exists to prevent, got when=%v", tasks[idx]["when"])
		}
	}
	verify, ok := tasks[assertIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the persisted-spec proof must be an assert, got %v", tasks[assertIdx])
	}
	if got := fmt.Sprint(verify["that"]); !strings.Contains(got, "ssl is defined") || !strings.Contains(got, "needs_configuration") {
		t.Fatalf("the proof must check the stored copy carries the ssl key AND that the store had settled — an unsettled store still has a clobbering rewrite pending, got that=%v", verify["that"])
	}
	if got := fmt.Sprint(verify["fail_msg"]); !strings.Contains(got, "failover") {
		t.Fatalf("the refusal must explain the relapse a manager failover triggers, got fail_msg=%v", verify["fail_msg"])
	}
}

func TestStorageGrafanaCredentialSeedsAnAdminAndRedeploys(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/grafana_credential.yml"
	tasks := readAnsibleTasks(t, path)
	assembleIdx := findAnsibleTask(t, tasks, "Assemble the Grafana service spec carrying its administrator credential")
	applyIdx := findAnsibleTask(t, tasks, "Apply the Grafana service spec")
	redeployIdx := findAnsibleTask(t, tasks, "Recreate the Grafana daemons so the credential reaches grafana.ini")
	if !(assembleIdx < applyIdx && applyIdx < redeployIdx) {
		t.Fatalf("the credential must be assembled, applied, then made live (assemble=%d apply=%d redeploy=%d)", assembleIdx, applyIdx, redeployIdx)
	}
	if got := fmt.Sprint(tasks[assembleIdx]["ansible.builtin.set_fact"]); !strings.Contains(got, "initial_admin_password") {
		t.Fatalf("cephadm renders disable_initial_admin_creation into grafana.ini whenever the spec omits initial_admin_password, and Grafana then creates NO administrator — its login page refuses every credential; got %v", tasks[assembleIdx]["ansible.builtin.set_fact"])
	}
	for _, idx := range []int{assembleIdx, applyIdx} {
		if _, ok := tasks[idx]["no_log"]; !ok && idx == assembleIdx {
			t.Fatalf("the assembly holds the plaintext administrator password and must not reach the run log, got %v", tasks[idx])
		}
	}
	if got := fmt.Sprint(tasks[redeployIdx]["ansible.builtin.command"]); !strings.Contains(got, "redeploy") {
		t.Fatalf("initial_admin_password reaches grafana.ini only when the daemon is recreated — applying the spec alone leaves the running Grafana with no administrator, got %v", tasks[redeployIdx]["ansible.builtin.command"])
	}
	if got := fmt.Sprint(tasks[redeployIdx]["when"]); !strings.Contains(got, "settled") {
		t.Fatalf("the redeploy must be gated on the stored credential differing, or every apply bounces Grafana, got when=%v", tasks[redeployIdx]["when"])
	}
}

func TestStorageManagementGatewayHealthRewritesStaleDaemons(t *testing.T) {
	bootstrap := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap.yml")
	block, ok := bootstrap[0]["block"].([]any)
	if !ok {
		t.Fatalf("bootstrap.yml must open with the guarded block, got %v", bootstrap[0])
	}
	readiness, health := -1, -1
	for i, entry := range block {
		task, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		switch fmt.Sprint(task["ansible.builtin.include_tasks"]) {
		case "bootstrap_steps/service_readiness.yml":
			readiness = i
		case "bootstrap_steps/management_gateway_health.yml":
			health = i
		}
	}
	if health < 0 || readiness < 0 || health < readiness {
		t.Fatalf("the gateway health step must run after service readiness — before it, a fresh cluster's daemons are not deployed yet and every probe reads as a fault (readiness=%d health=%d)", readiness, health)
	}

	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/management_gateway_health.yml"
	tasks := readAnsibleTasks(t, path)
	probeIdx := findAnsibleTask(t, tasks, "Probe every management gateway for a live dashboard upstream")
	staleIdx := findAnsibleTask(t, tasks, "Record the gateways whose configuration outlived the dashboard it proxies")
	reconfigIdx := findAnsibleTask(t, tasks, "Rewrite the management gateway daemons from the settled spec")
	assertIdx := findAnsibleTask(t, tasks, "Assert every management gateway proxies the dashboard it fronts")
	if !(probeIdx < staleIdx && staleIdx < reconfigIdx && reconfigIdx < assertIdx) {
		t.Fatalf("the health step must probe, then rewrite, then prove (probe=%d stale=%d reconfig=%d assert=%d)", probeIdx, staleIdx, reconfigIdx, assertIdx)
	}
	if got := fmt.Sprint(tasks[staleIdx]["ansible.builtin.set_fact"]); !strings.Contains(got, "502") {
		t.Fatalf("cephadm computes a gateway daemon's dependencies from the manager daemon NAMES alone, never the dashboard port or scheme, so a corrected spec never rewrites a running daemon and the only symptom of the stale nginx is the upstream fault it answers with, got %v", tasks[staleIdx]["ansible.builtin.set_fact"])
	}
	reconfig := fmt.Sprint(tasks[reconfigIdx]["ansible.builtin.command"])
	if !strings.Contains(reconfig, "reconfig") || !strings.Contains(reconfig, "mgmt-gateway") {
		t.Fatalf("`ceph orch reconfig mgmt-gateway` is the only command that rewrites a running gateway daemon from the current spec, got %v", tasks[reconfigIdx]["ansible.builtin.command"])
	}
	if got := fmt.Sprint(tasks[reconfigIdx]["when"]); !strings.Contains(got, "stale_hosts") {
		t.Fatalf("the rewrite must fire only on observed staleness — an unconditional reconfigure bounces every gateway on every apply, got when=%v", tasks[reconfigIdx]["when"])
	}
	if got := readRepoFile(t, path); strings.Contains(got, "validate_certs") {
		t.Fatalf("the probe covers only gateways serving plain HTTP, where no certificate is in play; an https gateway needs the cluster's cephadm root CA as a trust anchor, not a disabled verification, got a validate_certs site in %s", path)
	}
}

func TestStorageDependenciesOpenTheGatewayInternalPort(t *testing.T) {
	path := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/dependencies.yml"
	tasks := readAnsibleTasks(t, path)
	idx := findAnsibleTask(t, tasks, "Open the management gateway internal port cephadm never registers")
	fw, ok := tasks[idx]["ansible.posix.firewalld"].(map[string]any)
	if !ok {
		t.Fatalf("the internal-port opening must use the firewalld module like the VRRP allowance above it, got %v", tasks[idx])
	}
	if got := fmt.Sprint(fw["port"]); !strings.Contains(got, "29443") || !strings.Contains(got, "/tcp") {
		t.Fatalf("cephadm registers only the gateway's public port with firewalld, so the dashboard's monitoring calls to https://<vip>:29443/internal/... die with no-route-to-host unless this task opens the internal port, got port=%v", fw["port"])
	}
	if fw["permanent"] != true || fw["immediate"] != true {
		t.Fatalf("the internal-port opening must be permanent and immediate, got %v", fw)
	}
	when := fmt.Sprint(tasks[idx]["when"])
	if !strings.Contains(when, "bootwright_firewalld_available") || !strings.Contains(when, "management.hosts") {
		t.Fatalf("the internal-port opening must follow the firewalld probe and apply only when the cluster declares a management gateway, got when=%v", tasks[idx]["when"])
	}
}

func TestStorageRefusesNodesRunningAForeignCephadmCluster(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/"
	main := readRepoFile(t, base+"main.yml")
	rebuild := strings.Index(main, "phases/rebuild.yml")
	foreign := strings.Index(main, "phases/foreign_cluster.yml")
	endHost := strings.Index(main, "end_host")
	if rebuild < 0 || foreign < 0 || endHost < 0 || !(rebuild < foreign && foreign < endHost) {
		t.Fatalf("the foreign-cluster gate must run on every topology host after an authorized rebuild removed the override fsid and before non-seed hosts end, got rebuild=%d foreign=%d end_host=%d", rebuild, foreign, endHost)
	}

	tasks := readAnsibleTasks(t, base+"phases/foreign_cluster.yml")
	listIdx := findAnsibleTask(t, tasks, "List the cephadm systemd units the storage node carries")
	unitIdx := findAnsibleTask(t, tasks, "Resolve the cephadm cluster identities that own systemd units on the storage node")
	knownIdx := findAnsibleTask(t, tasks, "Resolve the Ceph cluster identities this apply may legitimately find on the storage node")
	foreignIdx := findAnsibleTask(t, tasks, "Resolve the cephadm clusters on the storage node this apply does not own")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse a storage node that still runs a cephadm cluster this apply does not own")
	if !(listIdx < unitIdx && unitIdx < knownIdx && knownIdx < foreignIdx && foreignIdx < refuseIdx) {
		t.Fatalf("the foreign-cluster gate must list the units, resolve their identities, resolve what this apply owns, subtract, then refuse")
	}

	command, ok := tasks[listIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("the foreign-cluster gate must enumerate units with a command probe, got %v", tasks[listIdx])
	}
	argv := fmt.Sprint(command["argv"])
	for _, want := range []string{"systemctl", "list-units", "--all", "ceph-*@*.service"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("the unit probe must enumerate every loaded cephadm unit on the node, missing %q: %v", want, argv)
		}
	}

	known, ok := tasks[knownIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("the foreign-cluster gate must resolve the owned identities in a set_fact, got %v", tasks[knownIdx])
	}
	confIdx := findAnsibleTask(t, tasks, "Read the Ceph configuration the storage node already carries")
	if confIdx > knownIdx {
		t.Fatalf("the node's own cluster identity must be read before the owned set is resolved, got conf=%d known=%d", confIdx, knownIdx)
	}
	if got := fmt.Sprint(tasks[confIdx]["ansible.builtin.slurp"]); !strings.Contains(got, "/etc/ceph/ceph.conf") {
		t.Fatalf("the gate must read this node's own cluster identity from its ceph.conf, got %v", got)
	}
	knownExpr := fmt.Sprint(known["bootwright_ceph_host_known_fsids"])
	for _, want := range []string{"bootwright_ceph_host_conf", "bootwright_ceph_override_fsid"} {
		if !strings.Contains(knownExpr, want) {
			t.Fatalf("the owned set must cover both the cluster this node already belongs to and the fsid an authorized rebuild is replacing, missing %q, got %v", want, knownExpr)
		}
	}

	subtract := fmt.Sprint(tasks[foreignIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{"bootwright_ceph_host_unit_fsids", "difference", "bootwright_ceph_host_known_fsids"} {
		if !strings.Contains(subtract, want) {
			t.Fatalf("the foreign set must be the unit identities minus the owned ones, missing %q, got %v", want, subtract)
		}
	}

	assertion, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("the foreign-cluster gate must fail through an assert, got %v", tasks[refuseIdx])
	}
	if got := fmt.Sprint(assertion["that"]); !strings.Contains(got, "bootwright_ceph_host_foreign_fsids") {
		t.Fatalf("the refusal must gate on the resolved foreign set, got that=%v", got)
	}
	failMsg := fmt.Sprint(assertion["fail_msg"])
	for _, want := range []string{
		"bootwright_ceph_host_foreign_units",
		"Address already in use",
		"cephadm rm-cluster --force --fsid",
		"--authorize foreign-daemons",
		"unreachable-nodes",
		"seed",
	} {
		if !strings.Contains(failMsg, want) {
			t.Fatalf("the refusal must name the leftover units, the port collision they cause, why the seed-side ownership gates miss them, the fsid-scoped remedy and the token that runs it in-product, missing %q, got %v", want, failMsg)
		}
	}
}

func TestStorageReclaimsAForeignCephadmClusterOnlyUnderItsToken(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/"
	tasks := readAnsibleTasks(t, base+"phases/foreign_cluster.yml")
	listIdx := findAnsibleTask(t, tasks, "List the cephadm systemd units the storage node carries")
	foreignIdx := findAnsibleTask(t, tasks, "Resolve the cephadm clusters on the storage node this apply does not own")
	rmIdx := findAnsibleTask(t, tasks, "Remove the cephadm clusters on the storage node this apply is authorized to reclaim")
	reprobeIdx := findAnsibleTask(t, tasks, "List the cephadm systemd units the storage node carries after the authorized removal")
	leftIdx := findAnsibleTask(t, tasks, "Resolve the cephadm clusters the authorized removal left on the storage node")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse a storage node that still runs a cephadm cluster this apply does not own")
	if !(foreignIdx < rmIdx && rmIdx < reprobeIdx && reprobeIdx < leftIdx && leftIdx < refuseIdx) {
		t.Fatalf("an authorized reclaim must act on the resolved foreign set, then re-probe the node and re-resolve what is left, and the refusal must still have the last word, got foreign=%d rm=%d reprobe=%d left=%d refuse=%d", foreignIdx, rmIdx, reprobeIdx, leftIdx, refuseIdx)
	}

	command, ok := tasks[rmIdx]["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("the reclaim must run cephadm through a command task, got %v", tasks[rmIdx])
	}
	argv := fmt.Sprint(command["argv"])
	for _, want := range []string{"cephadm", "rm-cluster", "--force", "--fsid"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("the reclaim must run the fsid-scoped removal the refusal names, missing %q: %v", want, argv)
		}
	}
	if strings.Contains(argv, "--zap-osds") {
		t.Fatalf("the reclaim must never zap the foreign cluster's OSD disks: the token authorizes removing that cluster's presence on this node, not destroying the data of a cluster Bootwright does not own, got %v", argv)
	}
	if got := fmt.Sprint(tasks[rmIdx]["loop"]); !strings.Contains(got, "bootwright_ceph_host_foreign_fsids") {
		t.Fatalf("the reclaim must remove exactly the identities the gate resolved as foreign, got loop=%v", got)
	}
	when := fmt.Sprint(tasks[rmIdx]["when"])
	for _, want := range []string{"bootwright_ceph_authorize_foreign_daemons", "bootwright_ceph_host_foreign_fsids"} {
		if !strings.Contains(when, want) {
			t.Fatalf("removing another cluster's daemons must ride the operator's token and never a default, missing %q, got when=%v", want, when)
		}
	}
	if got := fmt.Sprint(tasks[rmIdx]["failed_when"]); !strings.Contains(got, "rc != 0") {
		t.Fatalf("a removal that failed must fail the run rather than fall through to a refusal that no longer explains it, got failed_when=%v", got)
	}

	if _, present := tasks[reprobeIdx]["failed_when"]; present {
		t.Fatalf("the post-removal probe must fail closed: a node whose units cannot be read is not a node proven clean, got %v", tasks[reprobeIdx])
	}
	if fmt.Sprint(tasks[reprobeIdx]["register"]) == fmt.Sprint(tasks[listIdx]["register"]) {
		t.Fatalf("the post-removal probe must register its own variable: Ansible overwrites a register when its task is skipped, so reusing the first probe's name would erase the listing every unauthorized run refuses on, got %v", tasks[reprobeIdx]["register"])
	}
	left := fmt.Sprint(tasks[leftIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{fmt.Sprint(tasks[reprobeIdx]["register"]), "difference", "bootwright_ceph_host_known_fsids", "bootwright_ceph_host_foreign_fsids"} {
		if !strings.Contains(left, want) {
			t.Fatalf("what the removal left must be resolved from the post-removal listing, not from the pre-removal one, missing %q, got %v", want, left)
		}
	}
}
