package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestKubeVirtDestroyReadsAMissingResourceTypeAsConclusiveAbsence(t *testing.T) {
	topTasks := readAnsibleTasks(t, kubeVirtSubstrateDestroyTasks)
	tasks := nestedAnsibleTasks(t, topTasks[findAnsibleTask(t, topTasks, "Tear down KubeVirt guest on the reachable host cluster")], "block")

	apiIdx := findAnsibleTask(t, tasks, "Resolve whether the host cluster serves the VirtualMachine API at all")
	vmAssertIdx := findAnsibleTask(t, tasks, "Require a conclusive KubeVirt VirtualMachine probe")
	if apiIdx > vmAssertIdx {
		t.Fatalf("the API-absence resolution must precede the conclusive-probe assert that reads it (api=%d assert=%d)", apiIdx, vmAssertIdx)
	}
	apiFact := fmt.Sprint(tasks[apiIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{"doesn't have a resource type", "unable to retrieve the complete list of server APIs"} {
		if !strings.Contains(apiFact, want) {
			t.Errorf("a missing resource type is conclusive only when API discovery completed — under partial discovery the guest's own API group may just have failed to list — missing %q in %v", want, tasks[apiIdx]["ansible.builtin.set_fact"])
		}
	}
	vmThat := fmt.Sprint(tasks[vmAssertIdx]["ansible.builtin.assert"].(map[string]any)["that"])
	if !strings.Contains(vmThat, "bootwright_kubevirt_vm_api_absent") {
		t.Errorf("a host cluster that serves no VirtualMachine resource type cannot hold the guest, so the destroy probe must read that answer as conclusive absence instead of dead-ending with no remedy, got that=%v", vmThat)
	}

	dvAssertIdx := findAnsibleTask(t, tasks, "Require conclusive KubeVirt DataVolume probes")
	dvThat := fmt.Sprint(tasks[dvAssertIdx]["ansible.builtin.assert"].(map[string]any)["that"])
	for _, want := range []string{"doesn't have a resource type", "unable to retrieve the complete list of server APIs"} {
		if !strings.Contains(dvThat, want) {
			t.Errorf("the DataVolume probe must accept a conclusively missing resource type and still refuse one reported under partial discovery, missing %q in that=%v", want, dvThat)
		}
	}

	applyTasks := readAnsibleTasks(t, kubeVirtSubstrateTasks)
	applyAssert := fmt.Sprint(applyTasks[findAnsibleTask(t, applyTasks, "Require a conclusive KubeVirt VirtualMachine probe")]["ansible.builtin.assert"])
	if strings.Contains(applyAssert, "doesn't have a resource type") {
		t.Errorf("apply must keep failing loudly on a host cluster with no VirtualMachine API: no guest can be created there, so for apply that answer is an error, not a pass, got %v", applyAssert)
	}
}

func TestStorageCephadmDestroyResumesFromSurvivingHostsWhenTheSeedIsAlreadyClean(t *testing.T) {
	tasks := storageCephDestroyTasks(t)
	resolveIdx := findAnsibleTask(t, tasks, "Resolve the Ceph clusters a non-seed storage host carries under no resolved fsid")
	confIdx := findAnsibleTask(t, tasks, "Read the Ceph configuration a surviving storage host still carries")
	markerIdx := findAnsibleTask(t, tasks, "Read the Bootwright ownership marker a surviving storage host still carries")
	recordIdx := findAnsibleTask(t, tasks, "Read the storage cluster ownership record fsid on the controller")
	evidenceIdx := findAnsibleTask(t, tasks, "Resolve the cluster evidence the surviving storage hosts still carry")
	decideIdx := findAnsibleTask(t, tasks, "Decide whether the surviving storage hosts prove the fsid this teardown resumes")
	adoptIdx := findAnsibleTask(t, tasks, "Adopt the fsid the surviving storage hosts prove for a resumed teardown")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse a non-seed storage host still carrying a cluster the seed resolved no fsid for")
	nonSeedRmIdx := findAnsibleTask(t, tasks, "Remove cephadm cluster on non-seed hosts")
	if !(resolveIdx < confIdx && confIdx < markerIdx && markerIdx < recordIdx && recordIdx < evidenceIdx && evidenceIdx < decideIdx && decideIdx < adoptIdx && adoptIdx < refuseIdx && refuseIdx < nonSeedRmIdx) {
		t.Fatalf("the resume evidence must be read and judged between the carrier scan and the refusal, so an interrupted teardown continues while anything unproven still stops every host (resolve=%d conf=%d marker=%d record=%d evidence=%d decide=%d adopt=%d refuse=%d rm=%d)", resolveIdx, confIdx, markerIdx, recordIdx, evidenceIdx, decideIdx, adoptIdx, refuseIdx, nonSeedRmIdx)
	}

	adoptWhen := fmt.Sprint(tasks[adoptIdx]["when"])
	for _, want := range []string{
		"bootwright_ceph_destroy_fsid | default('') | length == 0",
		"bootwright_ceph_destroy_surviving_fsids | default([]) | length == 1",
		"bootwright_ceph_destroy_adoption_blockers | default([])) | length == 0",
	} {
		if !strings.Contains(adoptWhen, want) {
			t.Errorf("adoption must require exactly one carried fsid and zero contradictions, missing %q in when=%v", want, tasks[adoptIdx]["when"])
		}
	}
	if strings.Contains(adoptWhen, "authorize") {
		t.Errorf("no --authorize token may substitute for evidence in the resume path, got when=%v", tasks[adoptIdx]["when"])
	}
	decide := fmt.Sprint(tasks[decideIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{
		"bootwright_ceph_destroy_record.stat.exists",
		"bootwright_ceph_destroy_record_fsid",
		"bootwright_ceph_destroy_surviving_conf_fsids",
		"bootwright_ceph_destroy_surviving_marker_fsids",
		"bootwright_ceph_destroy_surviving_marker_clusters",
	} {
		if !strings.Contains(decide, want) {
			t.Errorf("the resume decision must weigh the controller record and every on-host contradiction, missing %q in the decide set_fact", want)
		}
	}

	fail := fmt.Sprint(tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)["fail_msg"])
	if !strings.Contains(fail, "bootwright_ceph_destroy_adoption_blockers") {
		t.Errorf("a refusal after a failed resume must name what blocked it, or the operator cannot tell a foreign cluster from a missing record, got %v", fail)
	}

	probeIdx := findAnsibleTask(t, tasks, "Probe the resumed cluster for a live manager from a surviving host")
	disableIdx := findAnsibleTask(t, tasks, "Stop the Ceph orchestrator of a resumed teardown before any host removes the cluster")
	orchRefuseIdx := findAnsibleTask(t, tasks, "Refuse a resumed removal whose orchestrator can still redeploy the cluster")
	reportIdx := findAnsibleTask(t, tasks, "Report an orchestrator a resumed teardown could not disable on an unanswering cluster")
	if !(refuseIdx < probeIdx && probeIdx < disableIdx && disableIdx < orchRefuseIdx && orchRefuseIdx < reportIdx && reportIdx < nonSeedRmIdx) {
		t.Fatalf("a resumed removal must stop the orchestrator from a surviving host before any host removes the cluster (refuse=%d probe=%d disable=%d orchRefuse=%d report=%d rm=%d)", refuseIdx, probeIdx, disableIdx, orchRefuseIdx, reportIdx, nonSeedRmIdx)
	}
	assertCephMutationTimeoutOnlyFailure(t, tasks[disableIdx], "bootwright_ceph_resumed_orch_disable", "the resumed orchestrator stop")
	probeCmd := fmt.Sprint(tasks[probeIdx]["ansible.builtin.command"])
	for _, want := range []string{"status", "--connect-timeout"} {
		if !strings.Contains(probeCmd, want) {
			t.Errorf("the resumed liveness probe must be a bounded `ceph status`: `ceph fsid` answers from the surviving host's local config with no quorum at all, which reads a dead cluster as a live manager and dead-ends the resume, missing %q in %v", want, tasks[probeIdx]["ansible.builtin.command"])
		}
	}
	if got := fmt.Sprint(tasks[orchRefuseIdx]["when"]); !strings.Contains(got, "bootwright_ceph_resumed_probe.rc") {
		t.Errorf("the resumed refusal must stay gated on a cluster that answered a quorum-requiring status read, so an already-dead cluster is never blocked by a manager that cannot reply, got when=%v", tasks[orchRefuseIdx]["when"])
	}
	if got := fmt.Sprint(tasks[orchRefuseIdx]["when"]); strings.Contains(got, "authorize") {
		t.Errorf("no --authorize token may relax the resumed orchestrator stop, got when=%v", tasks[orchRefuseIdx]["when"])
	}
	if got := fmt.Sprint(tasks[reportIdx]["ansible.builtin.debug"]); !strings.Contains(got, "settle gate") {
		t.Errorf("an unconfirmed disable must name what proves the outcome instead — the settle gate — got %v", tasks[reportIdx]["ansible.builtin.debug"])
	}
}

func TestStorageCephadmDestroyRecoversAuthorityFromExactMonsWhenTheSeedIsAbsent(t *testing.T) {
	tasks := storageCephDestroyTasks(t)
	recordIdx := findAnsibleTask(t, tasks, "Stat the controller ownership record for dead-seed recovery")
	confIdx := findAnsibleTask(t, tasks, "Stat Ceph configuration on a dead-seed recovery mon")
	markerIdx := findAnsibleTask(t, tasks, "Stat the Bootwright marker on a dead-seed recovery mon")
	classifyIdx := findAnsibleTask(t, tasks, "Classify dead-seed recovery evidence on each reachable mon")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve exact dead-seed recovery matches across reachable mons")
	refuseIdx := findAnsibleTask(t, tasks, "Refuse dead-seed recovery without one exact ownership identity")
	electIdx := findAnsibleTask(t, tasks, "Elect the first exact mon as dead-seed ownership authority")
	projectIdx := findAnsibleTask(t, tasks, "Project dead-seed ownership proof onto the elected mon")
	removeIdx := findAnsibleTask(t, tasks, "Remove cephadm cluster on the ownership authority host")
	wipeIdx := findAnsibleTask(t, tasks, "Wipe declared Ceph device signatures")
	if !(recordIdx < confIdx && confIdx < markerIdx && markerIdx < classifyIdx && classifyIdx < resolveIdx && resolveIdx < refuseIdx && refuseIdx < electIdx && electIdx < projectIdx && projectIdx < removeIdx && removeIdx < wipeIdx) {
		t.Fatalf("dead-seed evidence must be read, classified and refused before authority projection or teardown (record=%d conf=%d marker=%d classify=%d resolve=%d refuse=%d elect=%d project=%d remove=%d wipe=%d)", recordIdx, confIdx, markerIdx, classifyIdx, resolveIdx, refuseIdx, electIdx, projectIdx, removeIdx, wipeIdx)
	}

	record := tasks[recordIdx]
	if record["delegate_to"] != "localhost" || record["become"] != false || record["changed_when"] != false || record["failed_when"] != false {
		t.Fatalf("dead-seed controller evidence must be a tolerated read-only localhost probe, got %v", record)
	}
	classify := fmt.Sprint(tasks[classifyIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{
		"bootwright.io/ownership/v1alpha1",
		"storage-cluster",
		"bootwright_ceph_fallback_record.context",
		"bootwright_ceph_fallback_record.cluster",
		"bootwright_ceph_fallback_record.host",
		"attributes",
		"seedHost",
		"bootwright_ceph_fallback_conf_fsid",
		"bootwright_ceph_fallback_marker.manager",
		"bootwright_ceph_fallback_marker.cluster",
		"bootwright_ceph_fallback_marker_fsid",
	} {
		if !strings.Contains(classify, want) {
			t.Errorf("dead-seed ownership classification must bind controller, config and marker evidence exactly; missing %q in set_fact", want)
		}
	}

	resolve := fmt.Sprint(tasks[resolveIdx])
	for _, want := range []string{
		"bootwright_ceph_destroy_reachable_mon_hosts",
		"map('extract', hostvars)",
		"bootwright_ceph_fallback_evidence_blockers",
		"equalto",
		"flatten",
		"unique",
	} {
		if !strings.Contains(resolve, want) {
			t.Errorf("dead-seed authority selection must aggregate every reachable declared mon and reject ambiguous evidence; missing %q in set_fact", want)
		}
	}

	guard, ok := tasks[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("dead-seed ownership decision must be a hard assert, got %v", tasks[refuseIdx])
	}
	that := fmt.Sprint(guard["that"])
	for _, want := range []string{"fallback_matching_hosts", "length > 0", "fallback_blockers", "length == 0"} {
		if !strings.Contains(that, want) {
			t.Errorf("dead-seed refusal must require a complete unambiguous match, missing %q in that=%v", want, guard["that"])
		}
	}
	fail := fmt.Sprint(guard["fail_msg"])
	for _, want := range []string{
		"No teardown mutation has started",
		"Missing, unreadable, contradictory or ambiguous evidence fails closed",
		"re-run the same destroy invocation",
		"peer evidence never reconstructs a controller record",
		"No --authorize token",
		"no record-only path",
	} {
		if !strings.Contains(fail, want) {
			t.Errorf("dead-seed refusal must state the evidence failure and scoped recovery, missing %q in %v", want, guard["fail_msg"])
		}
	}

	for _, idx := range []int{electIdx, projectIdx} {
		when := fmt.Sprint(tasks[idx]["when"])
		for _, want := range []string{"seed_reachable", "fallback_matching_hosts", "fallback_blockers"} {
			if !strings.Contains(when, want) {
				t.Errorf("task %q must repeat the fail-closed fallback predicates, missing %q in when=%v", tasks[idx]["name"], want, tasks[idx]["when"])
			}
		}
		if strings.Contains(when, "authorize") {
			t.Errorf("no authorization token may substitute for dead-seed evidence, got when=%v", tasks[idx]["when"])
		}
	}
	removeWhen := fmt.Sprint(tasks[removeIdx]["when"])
	if !strings.Contains(removeWhen, "bootwright_ceph_destroy_authority_host") || !strings.Contains(removeWhen, "bootwright_ceph_destroy_owned") || !strings.Contains(removeWhen, "bootwright_ceph_destroy_fsid") {
		t.Errorf("the first removal must run only on the host whose exact proof projected owned fsid state, got when=%v", tasks[removeIdx]["when"])
	}
}

func TestStorageCephDestroyPlayKeepsTheSeedFirstAndUsesOnlyDeclaredMonsAsFallback(t *testing.T) {
	plays := readAnsiblePlays(t, "ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_cluster_destroy.yml")
	tasks := nestedAnsibleTasks(t, plays[0], "tasks")
	endIdx := findAnsibleTask(t, tasks, "Stop tearing down unreachable storage hosts")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve reachable Ceph ownership evidence hosts")
	requireIdx := findAnsibleTask(t, tasks, "Require a reachable declared mon when the Ceph seed is absent")
	destroyIdx := findAnsibleTask(t, tasks, "Destroy Ceph storage cluster")
	if !(endIdx < resolveIdx && resolveIdx < requireIdx && requireIdx < destroyIdx) {
		t.Fatalf("the play must end proven-absent hosts, elect a reachable declared ownership source, and refuse before entering the destructive role (end=%d resolve=%d require=%d destroy=%d)", endIdx, resolveIdx, requireIdx, destroyIdx)
	}
	resolve := fmt.Sprint(tasks[resolveIdx]["ansible.builtin.set_fact"])
	for _, want := range []string{"bootwright_storage_seed_host_name in ansible_play_hosts", "monInventoryHosts", "select('in', ansible_play_hosts)", "if bootwright_storage_seed_host_name in ansible_play_hosts else ''"} {
		if !strings.Contains(resolve, want) {
			t.Errorf("destroy must retain the declared seed as first authority and restrict fallback to reachable declared mons, missing %q in %v", want, tasks[resolveIdx]["ansible.builtin.set_fact"])
		}
	}
	guard := fmt.Sprint(tasks[requireIdx]["ansible.builtin.assert"])
	for _, want := range []string{"controller record alone never authorizes", "same destroy invocation", "no record-only destructive escape"} {
		if !strings.Contains(guard, want) {
			t.Errorf("no-mon refusal must explain the safe scoped exit and reject record-only teardown, missing %q in %v", want, tasks[requireIdx]["ansible.builtin.assert"])
		}
	}
}

func TestStorageCephadmOwnershipRecordCarriesTheClusterFsid(t *testing.T) {
	base := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/"
	for _, spec := range []struct {
		rel  string
		task string
	}{
		{rel: base + "phases/bootstrap_steps/ownership_marker.yml", task: "Record storage cluster ownership early for recoverability"},
		{rel: base + "phases/bootstrap_steps/result_and_ownership.yml", task: "Record storage cluster ownership"},
		{rel: base + "phases/bootstrap_steps/apply_mode_finalize.yml", task: "Pre-record storage cluster ownership before bootstrap"},
		{rel: base + "destroy_steps/cluster_gate.yml", task: "Recover Bootwright storage cluster ownership record on controller"},
	} {
		tasks := readAnsibleTasks(t, spec.rel)
		vars, ok := tasks[findAnsibleTask(t, tasks, spec.task)]["vars"].(map[string]any)
		if !ok {
			t.Fatalf("%s %q must carry its record fields as vars", spec.rel, spec.task)
		}
		fields, ok := vars["bootwright_ownership_fields"].(map[string]any)
		if !ok {
			t.Fatalf("%s %q must declare bootwright_ownership_fields", spec.rel, spec.task)
		}
		attrs, ok := fields["attributes"].(map[string]any)
		if !ok {
			t.Fatalf("%s %q must declare record attributes", spec.rel, spec.task)
		}
		if got := fmt.Sprint(attrs["fsid"]); !strings.Contains(got, "fsid") {
			t.Errorf("%s %q must record the cluster fsid: it is the evidence a destroy resumes from when the seed no longer carries the cluster, got attributes=%v", spec.rel, spec.task, attrs)
		}
	}
}

func TestStorageCephadmStampsTheOwnershipMarkerOnEveryClusterHost(t *testing.T) {
	rel := "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/phases/bootstrap_steps/ownership_marker.yml"
	tasks := readAnsibleTasks(t, rel)
	seedIdx := findAnsibleTask(t, tasks, "Stamp Bootwright Ceph ownership marker")
	dirIdx := findAnsibleTask(t, tasks, "Ensure the Ceph configuration directory exists on every other declared cluster host")
	peerIdx := findAnsibleTask(t, tasks, "Stamp Bootwright Ceph ownership marker on every other declared cluster host")
	recordIdx := findAnsibleTask(t, tasks, "Record storage cluster ownership early for recoverability")
	if !(seedIdx < dirIdx && dirIdx < peerIdx && peerIdx < recordIdx) {
		t.Fatalf("every declared cluster host must carry the ownership marker, not just the seed: the seed's copy is exactly the evidence an interrupted teardown deletes first (seed=%d dir=%d peer=%d record=%d)", seedIdx, dirIdx, peerIdx, recordIdx)
	}
	for _, idx := range []int{dirIdx, peerIdx} {
		if got := fmt.Sprint(tasks[idx]["delegate_to"]); !strings.Contains(got, "item.inventoryHost") {
			t.Errorf("%q must reach each declared host by delegation — non-seed hosts left the play before bootstrap, got delegate_to=%v", tasks[idx]["name"], tasks[idx]["delegate_to"])
		}
		if got := fmt.Sprint(tasks[idx]["loop"]); !strings.Contains(got, "rejectattr('inventoryHost', 'equalto', inventory_hostname)") {
			t.Errorf("%q must cover every declared host except the seed itself, got loop=%v", tasks[idx]["name"], tasks[idx]["loop"])
		}
		if got := fmt.Sprint(tasks[idx]["when"]); !strings.Contains(got, "bootwright_ceph_ownership_fsid") {
			t.Errorf("%q must stamp only once a real fsid is known — an empty marker proves nothing and contradicts nothing, got when=%v", tasks[idx]["name"], tasks[idx]["when"])
		}
	}
	peerContent := fmt.Sprint(tasks[peerIdx]["ansible.builtin.copy"])
	for _, want := range []string{"manager", "cluster", "fsid"} {
		if !strings.Contains(peerContent, want) {
			t.Errorf("the peer marker must carry the same evidence as the seed's, missing %q in %v", want, tasks[peerIdx]["ansible.builtin.copy"])
		}
	}
}
