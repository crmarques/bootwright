package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

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
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_coverage_report.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/osd_readiness.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/registry_login.yml",
		"ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/service_specs.yml",
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
	for _, want := range []string{"type', 'equalto', 'osd", "reweight', 'gt', 0", "type', 'equalto', 'host", "name', 'in', bootwright_ceph_osd_dynamic_hosts", "map('intersect'", "map('length')", "select('gt', 0)", "bootwright_ceph_osd_dynamic_hosts | length", "bootwright_ceph_osd_host_tree.attempts", "bootwright_ceph_osd_readiness_retries"} {
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
	for _, want := range []string{"bootwright_ceph_osd_host_tree.rc", "name', 'in', bootwright_ceph_osd_dynamic_hosts", "map('intersect'", "bootwright_ceph_osd_dynamic_hosts | length"} {
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
	for _, want := range []string{"bootwright_ceph_osd_host_tree.rc", "bootwright_ceph_osd_host_tree.stdout", "bootwright_ceph_osd_dynamic_host", "type', 'equalto', 'osd", "type', 'equalto', 'host", "intersect"} {
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
	if got := fmt.Sprint(refuse["fail_msg"]); !strings.Contains(got, "--confirm-data-loss") {
		t.Fatalf("pool destroy refusal must name the --confirm-data-loss remedy, got %v", refuse["fail_msg"])
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
	if got := fmt.Sprint(fsRefuse["fail_msg"]); !strings.Contains(got, "--confirm-data-loss") {
		t.Fatalf("CephFS destroy refusal must name the --confirm-data-loss remedy, got %v", fsRefuse["fail_msg"])
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
	if got := fmt.Sprint(decide["when"]); !strings.Contains(got, "ec-profile") || !strings.Contains(got, "'override'") {
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
	if got := fmt.Sprint(refuse["fail_msg"]); !strings.Contains(got, "--confirm-data-loss") {
		t.Fatalf("ec-profile destroy refusal must name the --confirm-data-loss remedy, got %v", refuse["fail_msg"])
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
	if got := fmt.Sprint(check["fail_msg"]); !strings.Contains(got, "--reclaim-devices") {
		t.Fatalf("OSD device refusal must name the reclaim remedy, got fail_msg=%v", got)
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
	operationsIdx := findAnsibleTask(t, seedContextTasks, "Load rendered Ceph operations")
	if !(seedGatherIdx < seedProviderIdx && seedProviderIdx < operationsIdx) {
		t.Fatalf("seed context must gather OS facts before provider templates, then load bootstrap operations")
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
	osValidateIdx := findAnsibleTask(t, repositoryTasks, "Validate Ceph storage node OS for the rendered distribution")
	osValidate, ok := repositoryTasks[osValidateIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("storage node OS validation must be an assert, got %v", repositoryTasks[osValidateIdx])
	}
	if got := fmt.Sprint(osValidate["that"]); !strings.Contains(got, "runtimeOS.exactVersions") || !strings.Contains(got, "ansible_os_family == 'RedHat'") {
		t.Fatalf("storage node OS validation must enforce the rendered runtimeOS matrix on RHEL-family nodes, got that=%v", osValidate["that"])
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
	boot := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap.yml")
	readinessIdx := strings.Index(boot, "osd_readiness.yml")
	coverageIdx := strings.Index(boot, "osd_coverage_report.yml")
	if readinessIdx < 0 || coverageIdx < 0 || readinessIdx > coverageIdx {
		t.Error("bootstrap.yml must include osd_coverage_report.yml after osd_readiness.yml")
	}
}
