package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

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
	if got := fmt.Sprint(decide["when"]); !strings.Contains(got, "ceph-pool") || !strings.Contains(got, "'override'") {
		t.Fatalf("pool rebuild decision must be gated on the ceph-pool op under override, got %v", decide["when"])
	}

	rebuild := tasks[findAnsibleTask(t, tasks, "Rebuild structurally drifted Ceph pool for override")]
	if got := fmt.Sprint(rebuild["when"]); !strings.Contains(got, "bootwright_ceph_op_pool_recreate") {
		t.Fatalf("pool rebuild must be gated on the structural-mismatch decision, got %v", rebuild["when"])
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

	fsRebuild := tasks[findAnsibleTask(t, tasks, "Rebuild structurally drifted CephFS for override")]
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
	if got := fmt.Sprint(decide["when"]); !strings.Contains(got, "ec-profile") || !strings.Contains(got, "'override'") {
		t.Fatalf("ec-profile rebuild decision must be gated on the ec-profile op under override, got %v", decide["when"])
	}

	rebuild := tasks[findAnsibleTask(t, tasks, "Rebuild structurally drifted erasure-coded pool for override")]
	if got := fmt.Sprint(rebuild["when"]); !strings.Contains(got, "bootwright_ceph_op_ec_recreate") {
		t.Fatalf("ec-profile rebuild must be gated on the structural-mismatch decision, got %v", rebuild["when"])
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
	if got := fmt.Sprint(profileRm["when"]); !strings.Contains(got, "bootwright_ceph_op_ec_recreate") {
		t.Fatalf("ec-profile delete must be gated on the structural-mismatch decision, got %v", profileRm["when"])
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

	task := tasks[findAnsibleTask(t, tasks, "Assert storage node hostname matches the declared topology")]
	body, ok := task["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("storage hostname verification must be an assert, got %v", task)
	}
	that := fmt.Sprint(body["that"])
	for _, want := range []string{"ansible_facts['nodename'] == item.cephHostname", "ansible_facts['hostname'] == item.cephHostname"} {
		if !strings.Contains(that, want) {
			t.Fatalf("hostname assert must compare gathered hostname facts with the declared topology hostname, got %v", body["that"])
		}
	}
	failMsg := fmt.Sprint(body["fail_msg"])
	for _, want := range []string{"{{ ansible_facts['nodename'] }}", "{{ item.cephHostname }}"} {
		if !strings.Contains(failMsg, want) {
			t.Fatalf("hostname assert fail_msg must name both the real and the declared hostname, got %s", failMsg)
		}
	}
	if got := fmt.Sprint(task["loop"]); !strings.Contains(got, "bootwright_host_storage_nodes") {
		t.Fatalf("hostname assert must loop this host's declared storage nodes, got %v", task["loop"])
	}
	if got := fmt.Sprint(task["when"]); !strings.Contains(got, "bootwright_host_storage_nodes | length > 0") {
		t.Fatalf("hostname assert must be gated on storage nodes, got when=%v", task["when"])
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
	if got := fmt.Sprint(step["ansible.builtin.command"]); !strings.Contains(got, "registry-login") || !strings.Contains(got, "bootwright_ceph_remote_registry_json") {
		t.Fatalf("registry login must run ceph cephadm registry-login -i <json>, got %v", step["ansible.builtin.command"])
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

func TestStorageCephadmOverrideRebuildVerifiesOwnershipMarker(t *testing.T) {
	tasks := storageCephBootstrapTasks(t)

	readIdx := findAnsibleTask(t, tasks, "Read Bootwright Ceph ownership marker for override rebuild")
	decideIdx := findAnsibleTask(t, tasks, "Decide override rebuild ownership")
	gateIdx := findAnsibleTask(t, tasks, "Enforce apply mode for the Ceph cluster")
	zapIdx := findAnsibleTask(t, tasks, "Remove existing cephadm cluster for override rebuild")
	if !(readIdx < decideIdx && decideIdx < gateIdx && gateIdx < zapIdx) {
		t.Fatalf("override rebuild must read marker, decide ownership, and enforce the apply-mode gate before zapping (read=%d decide=%d gate=%d zap=%d)", readIdx, decideIdx, gateIdx, zapIdx)
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
	if got := fmt.Sprint(tasks[zapIdx]["when"]); !strings.Contains(got, "bootwright_ceph_override_owned") {
		t.Fatalf("override rebuild zap must be gated on proven ownership, got %v", tasks[zapIdx]["when"])
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

func TestStorageCephadmOverrideRebuildRmClusterFailsClosed(t *testing.T) {
	tasks := storageCephBootstrapTasks(t)
	rm := tasks[findAnsibleTask(t, tasks, "Remove existing cephadm cluster for override rebuild")]
	if got := fmt.Sprint(rm["failed_when"]); !strings.Contains(got, "!= 0") {
		t.Fatalf("override rebuild rm-cluster must fail closed before clearing config and re-bootstrapping, got failed_when=%v", rm["failed_when"])
	}
}

func TestStorageCephadmDestroyVerifiesOwnershipAndFailsClosed(t *testing.T) {
	tasks := storageCephDestroyTasks(t)

	readIdx := findAnsibleTask(t, tasks, "Read Bootwright Ceph ownership marker on seed host")
	decideIdx := findAnsibleTask(t, tasks, "Decide Ceph destroy ownership on seed host")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to destroy a non-Bootwright Ceph cluster on seed host")
	rmIdx := findAnsibleTask(t, tasks, "Remove cephadm cluster on seed host")
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	if !(readIdx < decideIdx && decideIdx < refuseIdx && refuseIdx < rmIdx && rmIdx < wipeIdx) {
		t.Fatalf("ceph destroy must verify ownership before removing the cluster and wiping (read=%d decide=%d refuse=%d rm=%d wipe=%d)", readIdx, decideIdx, refuseIdx, rmIdx, wipeIdx)
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
	} {
		if !strings.Contains(owned, want) {
			t.Fatalf("ceph destroy ownership decision must require %s, got %v", want, decide["bootwright_ceph_destroy_owned"])
		}
	}

	rm := tasks[rmIdx]
	if got := fmt.Sprint(rm["when"]); !strings.Contains(got, "bootwright_ceph_destroy_owned") {
		t.Fatalf("ceph destroy rm-cluster must be gated on proven ownership, got %v", rm["when"])
	}
	if got := fmt.Sprint(rm["failed_when"]); !strings.Contains(got, "!= 0") {
		t.Fatalf("ceph destroy rm-cluster must fail closed on error, got failed_when=%v", rm["failed_when"])
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
	seedIdx := findAnsibleTask(t, tasks, "Require the Ceph seed host to be reachable before any device wipe")
	wipeIdx := findAnsibleTask(t, tasks, "Destroy Ceph storage cluster")
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
	if got := fmt.Sprint(tasks[checkIdx]["failed_when"]); !strings.Contains(got, "bootwright_ceph_owned_osd_devices") {
		t.Fatalf("OSD device check must exempt only recorded Bootwright devices, got failed_when=%v", got)
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
	refuseIdx := findAnsibleTask(t, tasks, "Refuse to wipe declared devices not recorded as Bootwright OSDs")
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	if !(readIdx < refuseIdx && refuseIdx < wipeIdx) {
		t.Fatalf("ceph destroy must read the OSD marker and refuse unrecorded devices before wiping (read=%d refuse=%d wipe=%d)", readIdx, refuseIdx, wipeIdx)
	}
	refuse, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("OSD device guard must be an assert, got %v", tasks[refuseIdx])
	}
	if got := fmt.Sprint(refuse["that"]); !strings.Contains(got, "bootwright_ceph_recorded_osd_devices") {
		t.Fatalf("OSD device guard must require declared devices to be in the recorded set, got %v", refuse["that"])
	}
	if got := fmt.Sprint(tasks[refuseIdx]["when"]); !strings.Contains(got, "bootwright_ceph_osd_marker_valid") {
		t.Fatalf("OSD device guard must be gated on marker validity so it falls back when no marker exists, got %v", tasks[refuseIdx]["when"])
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

func TestStorageCephadmRoleKeepsSecretsAndArtifactsBounded(t *testing.T) {
	mainTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/main.yml")
	for _, name := range []string{
		"Prepare Ceph repository and subscription",
		"Install Ceph host dependencies",
		"Configure Ceph container registry access",
		"Install cephadm and Ceph host tooling",
		"Bootstrap and converge Ceph cluster",
	} {
		task := mainTasks[findAnsibleTask(t, mainTasks, name)]
		if _, ok := task["ansible.builtin.include_tasks"]; !ok {
			t.Fatalf("storage role main task %q must include a phase file", name)
		}
	}

	contextTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/context.yml")
	gatherIdx := findAnsibleTask(t, contextTasks, "Gather storage node OS facts")
	providerFactIdx := findAnsibleTask(t, contextTasks, "Resolve managed Ceph work paths")
	if !(gatherIdx < providerFactIdx) {
		t.Fatalf("storage context phase must gather OS facts before materializing the provider (gather=%d provider=%d)", gatherIdx, providerFactIdx)
	}

	repositoryTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/repository.yml")
	communityDispatchIdx := findAnsibleTask(t, repositoryTasks, "Configure community Ceph package repository")
	subscriptionDispatchIdx := findAnsibleTask(t, repositoryTasks, "Configure subscription-backed Ceph package repository")
	if got := fmt.Sprint(repositoryTasks[communityDispatchIdx]["when"]); !strings.Contains(got, "community is defined") {
		t.Fatalf("community repository dispatch must gate on the rendered community block, got when=%v", got)
	}
	if got := fmt.Sprint(repositoryTasks[subscriptionDispatchIdx]["when"]); !strings.Contains(got, "requiresRHSM") {
		t.Fatalf("subscription repository dispatch must gate on requiresRHSM, got when=%v", got)
	}
	for _, rel := range []string{
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/community.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/subscription.yml",
	} {
		if tasks := readAnsibleTasks(t, rel); len(tasks) == 0 {
			t.Fatalf("%s has no tasks", rel)
		}
	}

	communityTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/community.yml")
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
	if got := fmt.Sprint(addRepo["creates"]); !strings.Contains(got, "bootwright_ceph_community_repo_file") {
		t.Fatalf("community repo task must be idempotent via creates, got %v", addRepo["creates"])
	}

	subscriptionTasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/subscription.yml")
	rhsmRegister := subscriptionTasks[findAnsibleTask(t, subscriptionTasks, "Register storage node with RHSM")]
	assertRedactsByDefault(t, "RHSM registration", rhsmRegister["no_log"])
	licenseAcceptIdx := findAnsibleTask(t, subscriptionTasks, "Accept vendor Ceph license provisions")
	if got := fmt.Sprint(subscriptionTasks[licenseAcceptIdx]["when"]); !strings.Contains(got, "requiresLicense") {
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
	assertIncludeRoleName(t, installTasks[recordCephadmIdx], "bootwright.core.ownership_record")
	if got := installTasks[verifyCephadmIdx]["failed_when"]; got != false {
		t.Fatalf("cephadm verify must leave failure handling to the targeted assert, got failed_when=%v", got)
	}
	failCephadm, ok := installTasks[failCephadmIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(failCephadm["fail_msg"]), "MachineInstallProfile") {
		t.Fatalf("cephadm unavailable assert must point to managed OS package ownership, got %v", failCephadm)
	}

	probeCephIdx := findAnsibleTask(t, installTasks, "Probe ceph CLI before package fallback")
	cephCommonPackageIdx := findAnsibleTask(t, installTasks, "Install ceph-common package on storage node when not preinstalled")
	recordCephCommonIdx := findAnsibleTask(t, installTasks, "Write ceph-common package ownership record")
	verifyCephIdx := findAnsibleTask(t, installTasks, "Verify ceph CLI on storage node")
	failCephIdx := findAnsibleTask(t, installTasks, "Fail when ceph CLI is unavailable")
	if !(failCephadmIdx < probeCephIdx && probeCephIdx < cephCommonPackageIdx && cephCommonPackageIdx < recordCephCommonIdx && recordCephCommonIdx < verifyCephIdx && verifyCephIdx < failCephIdx) {
		t.Fatalf("ceph-common fallback must run after cephadm tooling and before storage services/verification")
	}
	cephCommonPackage := installTasks[cephCommonPackageIdx]
	if got := cephCommonPackage["failed_when"]; got != false {
		t.Fatalf("ceph-common package fallback must not fail the package batch, got failed_when=%v", got)
	}
	cephCommonPackageBody, ok := cephCommonPackage["ansible.builtin.package"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(cephCommonPackageBody["name"]), "bootwright_ceph_provider.cephCommonPackage") {
		t.Fatalf("ceph-common package fallback must install provider-selected ceph-common package, got %v", cephCommonPackage)
	}
	assertIncludeRoleName(t, installTasks[recordCephCommonIdx], "bootwright.core.ownership_record")
	if got := installTasks[verifyCephIdx]["failed_when"]; got != false {
		t.Fatalf("ceph CLI verify must leave failure handling to the targeted assert, got failed_when=%v", got)
	}
	failCeph, ok := installTasks[failCephIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(failCeph["fail_msg"]), "MachineInstallProfile") {
		t.Fatalf("ceph CLI unavailable assert must point to managed OS package ownership, got %v", failCeph)
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
	ensureClient := block[findAnsibleTask(t, block, "Ensure Ceph client tooling is present for cephadm orchestration")]
	ensureClientBody, ok := ensureClient["ansible.builtin.package"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(ensureClientBody["name"]), "bootwright_ceph_provider.cephCommonPackage") {
		t.Fatalf("bootstrap stage must ensure the provider-selected ceph client package, got %v", ensureClient)
	}
	rmCluster := block[findAnsibleTask(t, block, "Remove existing cephadm cluster for override rebuild")]
	if got := fmt.Sprint(rmCluster["when"]); !strings.Contains(got, "bootwright_apply_mode") || !strings.Contains(got, "override") {
		t.Fatalf("destructive cephadm rm-cluster must be gated on the override apply mode, got when=%v", rmCluster["when"])
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
	if !strings.Contains(bootstrapArgv, "--allow-fqdn-hostname") {
		t.Fatalf("bootstrap argv must pass --allow-fqdn-hostname (IBM recommended), got %v", resolveBootstrap)
	}
	if !strings.Contains(bootstrapArgv, "--dashboard-password-noupdate") {
		t.Fatalf("bootstrap argv must pass --dashboard-password-noupdate, got %v", resolveBootstrap)
	}
	if !strings.Contains(bootstrapArgv, "--single-host-defaults") || !strings.Contains(bootstrapArgv, "singleHostDefaults") {
		t.Fatalf("bootstrap argv must conditionally pass --single-host-defaults, got %v", resolveBootstrap)
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
	if got := fmt.Sprint(block[lateOpsIdx]["when"]); !strings.Contains(got, "object-gateway") {
		t.Fatalf("late operation loop has unexpected when=%v", block[lateOpsIdx]["when"])
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

func storageCephBootstrapTasks(t *testing.T) []map[string]any {
	t.Helper()
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/"
	return readAnsibleTasksFromFiles(t,
		base+"stage_inputs.yml",
		base+"apply_mode.yml",
		base+"bootstrap_cluster.yml",
		base+"ownership_marker.yml",
		base+"container_image_base.yml",
		base+"registry_login.yml",
		base+"dashboard_secret.yml",
		base+"service_specs.yml",
		base+"topology_operations.yml",
		base+"late_service_specs.yml",
		base+"management_services.yml",
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
	)
}
