package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

const (
	machineInfraDestroyPlaybook = "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_destroy.yml"
	machineInfraPrepareDestroy  = "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/machine_infra/prepare_destroy_cluster.yml"
	libvirtSubstrateDestroy     = bootwrightCollectionRoleRoot + "/machine_substrate_libvirt/tasks/destroy.yml"
)

func TestLibvirtNetworkDestroyGateIsClusterScoped(t *testing.T) {
	machineBody := readRepoFile(t, libvirtSubstrateDestroy)
	for _, banned := range []string{
		"net-dumpxml",
		"virt_net",
		"bootwright_libvirt_network_name",
		"bootwright_libvirt_network_owned",
		"bootwright_libvirt_network_present",
		"libvirt-network",
	} {
		if strings.Contains(machineBody, banned) {
			t.Fatalf("machine_substrate_libvirt/tasks/destroy.yml runs once per machine-task host, so a cluster-scoped libvirt network gate there is self-defeating: whichever host removes the network first makes every later host's probe report the network absent, the conclusive-probe assert accepts that, and the ownership refusal is SKIPPED instead of evaluated on the very run that removed the network. Found %q", banned)
		}
	}

	tasks := readAnsibleTasks(t, machineInfraPrepareDestroy)
	gateIdx := findAnsibleTask(t, tasks, "Gate the cluster libvirt network before destroy")
	gateWhen := fmt.Sprint(tasks[gateIdx]["when"])
	for _, want := range []string{
		"bootwright_destroy_machine_scope is not defined",
		"bootwright_libvirt_cluster_machines | length > 0",
	} {
		if !strings.Contains(gateWhen, want) {
			t.Fatalf("cluster libvirt network gate when missing %q: %v", want, tasks[gateIdx]["when"])
		}
	}

	gate := nestedAnsibleTasks(t, tasks[gateIdx], "block")
	probeIdx := findAnsibleTask(t, gate, "Read libvirt network definition")
	conclusiveIdx := findAnsibleTask(t, gate, "Require a conclusive libvirt network probe before destroy")
	decideIdx := findAnsibleTask(t, gate, "Resolve libvirt network ownership for destroy")
	refuseIdx := findAnsibleTask(t, gate, "Refuse to remove a non-Bootwright libvirt network")
	authorizeIdx := findAnsibleTask(t, gate, "Authorize cluster libvirt network removal")
	if !(probeIdx < conclusiveIdx && conclusiveIdx < decideIdx && decideIdx < refuseIdx && refuseIdx < authorizeIdx) {
		t.Fatalf("the cluster libvirt network gate must probe, require a conclusive probe, decide ownership, refuse, and only then authorize removal (probe=%d conclusive=%d decide=%d refuse=%d authorize=%d)", probeIdx, conclusiveIdx, decideIdx, refuseIdx, authorizeIdx)
	}

	probe, ok := gate[probeIdx]["ansible.builtin.command"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(probe["argv"]), "net-dumpxml") {
		t.Fatalf("cluster libvirt network probe must run virsh net-dumpxml, got %v", gate[probeIdx])
	}

	conclusive, ok := gate[conclusiveIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("conclusive network probe check must be an assert, got %v", gate[conclusiveIdx])
	}
	that := fmt.Sprint(conclusive["that"])
	for _, want := range []string{"rc | default(1) == 0", "Network not found", "no network with matching name"} {
		if !strings.Contains(that, want) {
			t.Fatalf("conclusive network probe must accept only rc==0 or a network-absent stderr and refuse anything unprobeable, missing %q in %v", want, conclusive["that"])
		}
	}
	if _, ok := gate[conclusiveIdx]["when"]; ok {
		t.Fatalf("the conclusive network probe assert must not carry its own when: the block already withholds it when there is no probe to judge, and an extra condition is how a fail-closed gate stops firing; got when=%v", gate[conclusiveIdx]["when"])
	}

	refuse, ok := gate[refuseIdx]["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("libvirt network ownership guard must be an assert, got %v", gate[refuseIdx])
	}
	if got := fmt.Sprint(refuse["that"]); !strings.Contains(got, "bootwright_libvirt_network_owned") {
		t.Fatalf("libvirt network guard must require the decided ownership fact, got %v", refuse["that"])
	}
	refuseWhen := fmt.Sprint(gate[refuseIdx]["when"])
	if !strings.Contains(refuseWhen, "bootwright_libvirt_network_present | bool") {
		t.Fatalf("libvirt network guard must be evaluated whenever the probe found the network, got when=%v", gate[refuseIdx]["when"])
	}
	if !strings.Contains(refuseWhen, "not (bootwright_destroy_authorize_unowned_networks") {
		t.Fatalf("libvirt network guard must be skipped only under --authorize unowned-networks, got when=%v", gate[refuseIdx]["when"])
	}
	if strings.Contains(refuseWhen, "bootwright_destroy_authorize_unowned_vms") {
		t.Fatalf("libvirt network guard must not be lifted by the VM authorization: an unowned network may still carry another context's VMs, got when=%v", gate[refuseIdx]["when"])
	}

	authorize, ok := gate[authorizeIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("network removal authorization must be a set_fact, got %v", gate[authorizeIdx])
	}
	if got := fmt.Sprint(authorize["bootwright_libvirt_network_removals"]); !strings.Contains(got, "bootwright_libvirt_network_name") {
		t.Fatalf("network removal authorization must queue the probed network, got %v", authorize["bootwright_libvirt_network_removals"])
	}
	if strings.Count(readRepoFile(t, machineInfraPrepareDestroy), "bootwright_libvirt_network_removals:") != 1 {
		t.Fatal("the post-substrate removal must have exactly one producer, the task that runs after the ownership refusal; a second producer could queue a network the refusal never judged")
	}
}

func TestLibvirtNetworkRemovalRunsAfterMachineSubstrateTeardown(t *testing.T) {
	plays := readAnsiblePlays(t, machineInfraDestroyPlaybook)
	if len(plays) != 3 {
		t.Fatalf("task_machine_infra_destroy plays = %d, want 3", len(plays))
	}
	if got := plays[1]["hosts"]; got != "bootwright_machine_task_hosts" {
		t.Fatalf("play 1 must be the machine substrate teardown, got hosts=%v", got)
	}
	for _, play := range []map[string]any{plays[0], plays[1]} {
		if strings.Contains(fmt.Sprint(play["tasks"]), "virt_net") {
			t.Fatalf("the libvirt network must not be removed before the machine substrate play has undefined every domain attached to it, found a virt_net task in play %v", play["name"])
		}
	}

	tasks := nestedAnsibleTasks(t, plays[2], "tasks")
	removeIdx := findAnsibleTask(t, tasks, "Remove authorized cluster libvirt networks")
	remove, ok := tasks[removeIdx]["community.libvirt.virt_net"].(map[string]any)
	if !ok {
		t.Fatalf("authorized network removal must use community.libvirt.virt_net, got %v", tasks[removeIdx])
	}
	if got := remove["state"]; got != "absent" {
		t.Fatalf("authorized network removal state = %v, want absent", got)
	}
	for _, key := range []string{"name", "uri"} {
		if got := fmt.Sprint(remove[key]); !strings.Contains(got, "bootwright_libvirt_network_removal.") {
			t.Fatalf("authorized network removal %s must come from the gated queue entry rather than being recomputed, got %v", key, remove[key])
		}
	}
	if got := fmt.Sprint(tasks[removeIdx]["loop"]); !strings.Contains(got, "bootwright_libvirt_network_removals") {
		t.Fatalf("authorized network removal must loop only what the cluster-scoped gate authorized, got loop=%v", tasks[removeIdx]["loop"])
	}

	recordIdx := findAnsibleTask(t, tasks, "Remove authorized cluster libvirt network ownership records")
	if recordIdx < removeIdx {
		t.Fatalf("the libvirt-network ownership record must only be dropped after the network removal ran (remove=%d record=%d)", removeIdx, recordIdx)
	}
	if got := fmt.Sprint(tasks[recordIdx]["loop"]); !strings.Contains(got, "bootwright_libvirt_network_removals") {
		t.Fatalf("libvirt-network record removal must loop only what the cluster-scoped gate authorized, got loop=%v", tasks[recordIdx]["loop"])
	}
}

func TestMachineInfraDestroySubstratePlayLoopsDependencyLevels(t *testing.T) {
	plays := readAnsiblePlays(t, machineInfraDestroyPlaybook)
	if got := plays[0]["strategy"]; got != "linear" {
		t.Fatalf("machine infra destroy preparation strategy = %v, want linear", got)
	}
	if got := plays[1]["strategy"]; got != "linear" {
		t.Fatalf("only the linear strategy turns a dependency level into a barrier, got strategy=%v", got)
	}
	if got := plays[2]["strategy"]; got != "linear" {
		t.Fatalf("the record sweep runs over every provider and infra host at once, so it needs linear to print one TASK banner per task, got strategy=%v", got)
	}
	throttle, ok := plays[1]["throttle"].(string)
	if !ok || !strings.Contains(throttle, "default(") {
		t.Fatalf("every machine-task host of one provider shares a single multiplexed SSH connection, so the per-level fan-out must be capped below the provider sshd MaxSessions, got throttle=%#v", plays[1]["throttle"])
	}

	tasks := nestedAnsibleTasks(t, plays[1], "tasks")
	destroy := tasks[findAnsibleTask(t, tasks, "Destroy machine substrate in dependency order")]
	loop := fmt.Sprint(destroy["loop"])
	if !strings.Contains(loop, "bootwright_destroy_cluster_levels.split(';')") {
		t.Fatalf("the substrate play must loop dependency levels so independent clusters tear down in one pass, got loop=%v", destroy["loop"])
	}
	if !strings.Contains(loop, "bootwright_managed_os_install_groups") {
		t.Fatalf("the levels fallback must still cover the managed-OS install groups, got loop=%v", destroy["loop"])
	}
	loopControl, ok := destroy["loop_control"].(map[string]any)
	if !ok || loopControl["loop_var"] != "bootwright_destroy_cluster_level" {
		t.Fatalf("the substrate play loop var must name a level, got %v", destroy["loop_control"])
	}
	when := fmt.Sprint(destroy["when"])
	if !strings.Contains(when, "bootwright_machine_task_cluster_name in bootwright_destroy_cluster_level.split(',')") {
		t.Fatalf("each machine task must run in the level its own cluster belongs to, got when=%v", destroy["when"])
	}
	vars, ok := destroy["vars"].(map[string]any)
	if !ok {
		t.Fatalf("the substrate include must pin the cluster name it dispatches, got %v", destroy)
	}
	if got := fmt.Sprint(vars["bootwright_cluster_name"]); !strings.Contains(got, "bootwright_machine_task_cluster_name") {
		t.Fatalf("tasks/machine_infra/destroy_machine.yml hard-asserts that exactly one destroy group matches bootwright_cluster_name, so the include must receive the host's single cluster name, not the joined level; got %v", vars["bootwright_cluster_name"])
	}
}

func TestMachineInfraDestroySkipsSweptOwnershipRecords(t *testing.T) {
	plays := readAnsiblePlays(t, machineInfraDestroyPlaybook)
	tasks := nestedAnsibleTasks(t, plays[2], "tasks")
	findIdx := findAnsibleTask(t, tasks, "List surviving machine infrastructure ownership record files")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve surviving machine infrastructure ownership record keys")
	destroyIdx := findAnsibleTask(t, tasks, "Destroy recorded machine infrastructure resources")
	if !(findIdx < resolveIdx && resolveIdx < destroyIdx) {
		t.Fatalf("the records play must list surviving record files before including the per-record teardown (find=%d resolve=%d destroy=%d)", findIdx, resolveIdx, destroyIdx)
	}
	if _, ok := tasks[findIdx]["ansible.builtin.find"]; !ok {
		t.Fatalf("surviving-record listing must be a single find rather than one stat per record, got %v", tasks[findIdx])
	}
	resolve, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("surviving-record keys must be a set_fact, got %v", tasks[resolveIdx])
	}
	present := fmt.Sprint(resolve["bootwright_infra_ownership_records_present"])
	if !strings.Contains(present, "'@[^/]*$'") {
		t.Fatalf("a reference record is stored as <name>@<context>.json, so the surviving-record key must strip the context suffix or every reference record would be treated as already swept, got %v", resolve["bootwright_infra_ownership_records_present"])
	}
	if !strings.Contains(present, "'[.]json$'") {
		t.Fatalf("surviving-record keys must drop the .json suffix, got %v", resolve["bootwright_infra_ownership_records_present"])
	}
	readable := fmt.Sprint(resolve["bootwright_infra_ownership_store_readable"])
	if !strings.Contains(readable, "skipped_paths") || !strings.Contains(readable, "files is defined") {
		t.Fatalf("the skip must require positive evidence that the record store was actually scanned on this host, got %v", resolve["bootwright_infra_ownership_store_readable"])
	}
	when := fmt.Sprint(tasks[destroyIdx]["when"])
	if !strings.Contains(when, "in bootwright_infra_ownership_records_present") {
		t.Fatalf("the per-record teardown include must be skipped when the machine substrate play already removed the record, got when=%v", tasks[destroyIdx]["when"])
	}
	if !strings.Contains(when, "not (bootwright_infra_ownership_store_readable | bool)") {
		t.Fatalf("an unreadable or absent record store on this host proves nothing, so the teardown must still run: skipping it there would strand a recorded domain or network. Got when=%v", tasks[destroyIdx]["when"])
	}
}

func TestMachineInfraDestroyPreparesEveryClusterWithMachines(t *testing.T) {
	plays := readAnsiblePlays(t, machineInfraDestroyPlaybook)
	vars, ok := plays[0]["vars"].(map[string]any)
	if !ok {
		t.Fatalf("machine infra destroy preparation play has no vars: %v", plays[0])
	}
	components := fmt.Sprint(vars["bootwright_host_machine_components"])
	if !strings.Contains(components, "bootwright_managed_os_install_groups") {
		t.Fatalf("a libvirt-hosted storage cluster's machines live in a managed-OS install group, so its host must still reach the cluster-scoped libvirt network gate; got %v", vars["bootwright_host_machine_components"])
	}
	tasks := readAnsibleTasks(t, machineInfraPrepareDestroy)
	resolve, ok := tasks[findAnsibleTask(t, tasks, "Resolve current cluster")]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("cluster resolution must be a set_fact: %v", tasks[0])
	}
	if got := fmt.Sprint(resolve["bootwright_current_cluster"]); !strings.Contains(got, "bootwright_managed_os_install_groups") {
		t.Fatalf("cluster resolution must also match a managed-OS install group or the loop's first storage-cluster pass fails on an empty selection, got %v", resolve["bootwright_current_cluster"])
	}
}
