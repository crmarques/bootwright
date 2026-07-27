package workflow

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func destroyTaskByID(t *testing.T, tasks []ApplyTask, id string) ApplyTask {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID == id {
			return task
		}
	}
	t.Fatalf("destroy plan has no task %q", id)
	return ApplyTask{}
}

func destroyTaskIDs(tasks []ApplyTask) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.Entry.ID)
	}
	return out
}

func assertDestroyOrderingEdges(t *testing.T, tasks []ApplyTask, want map[string][]string) {
	t.Helper()
	for _, task := range tasks {
		wanted, ok := want[task.Entry.ID]
		if !ok {
			t.Fatalf("unexpected destroy task %q in plan %v", task.Entry.ID, destroyTaskIDs(tasks))
		}
		got := task.Entry.OrderingDependencies
		if len(got) == 0 && len(wanted) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, wanted) {
			t.Fatalf("task %q ordering deps = %v, want %v", task.Entry.ID, got, wanted)
		}
	}
	if len(tasks) != len(want) {
		t.Fatalf("planned %v, want edges for %d tasks", destroyTaskIDs(tasks), len(want))
	}
}

func TestPlanDestroyTasksInfraChain(t *testing.T) {
	limit := "bootwright_provider_hosts:bootwright_infra_component_hosts:bootwright_infra_hosts"
	extra := []string{"bootwright_infra_destroy_context_sweep=true"}
	tasks, err := PlanDestroyTasks("infra", v1alpha1.State{}, limit, extra, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"destroy.machine-registration", "destroy.infra-components", "destroy.machine-infra", "destroy.provider-services"}
	if got := destroyTaskIDs(tasks); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("infra destroy plan = %v, want %v", got, wantIDs)
	}
	assertDestroyOrderingEdges(t, tasks, map[string][]string{
		"destroy.machine-registration": nil,
		"destroy.infra-components":     {"destroy.machine-registration"},
		"destroy.machine-infra":        {"destroy.infra-components"},
		"destroy.provider-services":    {"destroy.machine-infra"},
	})
	for i, task := range tasks {
		wantLimit := limit
		if task.Entry.ID == "destroy.machine-registration" {
			wantLimit = "bootwright_storage_hosts"
		} else if task.Entry.ID == "destroy.machine-infra" {
			wantLimit = "bootwright_machine_task_hosts:bootwright_provider_hosts:bootwright_infra_hosts"
		}
		if task.Limit != wantLimit {
			t.Fatalf("task[%d] limit = %q, want %q", i, task.Limit, wantLimit)
		}
		if len(task.ExtraVarPairs) != 1 || task.ExtraVarPairs[0] != extra[0] {
			t.Fatalf("task[%d] extra-vars = %v, want %v", i, task.ExtraVarPairs, extra)
		}
		if len(task.Entry.Dependencies) != 0 {
			t.Fatalf("task[%d] hard deps = %v, want none (ordering only)", i, task.Entry.Dependencies)
		}
	}
	if tasks[0].Playbook == "" || tasks[0].Playbook == tasks[1].Playbook {
		t.Fatalf("tasks must carry distinct destroy playbooks: %q / %q", tasks[0].Playbook, tasks[1].Playbook)
	}
}

func TestPlanDestroyTasksMachineInfraUsesOneForkPerDeclaredHost(t *testing.T) {
	state := loadWorkflowFixtureState(t, "003-3nodes-libvirt")
	tasks, err := PlanDestroyTasks("infra", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Entry.Kind != DestroyTaskKindMachineInfra {
			continue
		}
		if task.Limit != "bootwright_machine_task_hosts:bootwright_provider_hosts:bootwright_infra_hosts" {
			t.Fatalf("machine infra destroy limit = %q", task.Limit)
		}
		if task.Forks != 4 {
			t.Fatalf("machine infra destroy forks = %d, want 4 for three VM task hosts plus their provider host", task.Forks)
		}
		return
	}
	t.Fatal("infra destroy plan has no machine infrastructure task")
}

func TestPlanDestroyTasksClustersChain(t *testing.T) {
	tasks, err := PlanDestroyTasks("clusters", v1alpha1.State{}, "limit", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"destroy.storage-clusters", "destroy.container-clusters", "destroy.storage-node-access"}
	if len(tasks) != len(wantIDs) {
		t.Fatalf("clusters chain = %+v, want %v", tasks, wantIDs)
	}
	for i, id := range wantIDs {
		if tasks[i].Entry.ID != id {
			t.Fatalf("clusters chain = %+v, want %v", tasks, wantIDs)
		}
	}
	if len(tasks[1].Entry.OrderingDependencies) != 1 || tasks[1].Entry.OrderingDependencies[0] != wantIDs[0] {
		t.Fatalf("container destroy must be ordering-sequenced after storage destroy: %v", tasks[1].Entry.OrderingDependencies)
	}
	if len(tasks[2].Entry.OrderingDependencies) != 1 || tasks[2].Entry.OrderingDependencies[0] != wantIDs[1] {
		t.Fatalf("storage node access revoke must be ordering-sequenced last: %v", tasks[2].Entry.OrderingDependencies)
	}
	if tasks[2].Entry.Kind != DestroyTaskKindStorageNodeAccess {
		t.Fatalf("storage node access revoke must carry its own distinct kind, got %q", tasks[2].Entry.Kind)
	}
	if len(tasks[1].Entry.Dependencies) != 0 {
		t.Fatalf("destroy steps must not carry hard deps (ordering only): %v", tasks[1].Entry.Dependencies)
	}
}

func TestPlanDestroyTasksAllChain(t *testing.T) {
	limit := ""
	extra := []string{"bootwright_infra_destroy_context_sweep=true"}
	tasks, err := PlanDestroyTasks("all", v1alpha1.State{}, limit, extra, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"destroy.storage-clusters",
		"destroy.machine-registration",
		"destroy.storage-node-access",
		"destroy.infra-components",
		"destroy.machine-infra",
		"destroy.container-clusters",
		"destroy.provider-services",
	}
	if got := destroyTaskIDs(tasks); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("full destroy plan = %v, want %v", got, wantIDs)
	}
	assertDestroyOrderingEdges(t, tasks, map[string][]string{
		"destroy.storage-clusters":     nil,
		"destroy.machine-registration": {"destroy.storage-clusters"},
		"destroy.storage-node-access":  {"destroy.machine-registration"},
		"destroy.infra-components":     {"destroy.storage-node-access"},
		"destroy.machine-infra":        {"destroy.infra-components"},
		"destroy.container-clusters":   {"destroy.machine-infra"},
		"destroy.provider-services":    {"destroy.machine-infra", "destroy.container-clusters"},
	})
	for i, task := range tasks {
		if len(task.ExtraVarPairs) != 1 || task.ExtraVarPairs[0] != extra[0] {
			t.Fatalf("task[%d] extra-vars = %v, want %v", i, task.ExtraVarPairs, extra)
		}
		wantDependencies := []string(nil)
		if task.Entry.ID == "destroy.container-clusters" {
			wantDependencies = []string{"destroy.machine-infra"}
		}
		if !reflect.DeepEqual(task.Entry.Dependencies, wantDependencies) {
			t.Fatalf("task[%d] hard deps = %v, want %v", i, task.Entry.Dependencies, wantDependencies)
		}
	}
	nodeAccess := slices.Index(wantIDs, "destroy.storage-node-access")
	registration := slices.Index(wantIDs, "destroy.machine-registration")
	storage := slices.Index(wantIDs, "destroy.storage-clusters")
	machineInfra := slices.Index(wantIDs, "destroy.machine-infra")
	if !(storage < registration && registration < nodeAccess) {
		t.Fatalf("every step that connects to bootwright_storage_hosts with the run's statically rendered ansible_user must precede the node-access revoke: %v", wantIDs)
	}
	if nodeAccess > machineInfra {
		t.Fatalf("node-access revoke must precede machine teardown: once the substrate deletes the VMs, task_storage_node_access_destroy.yml ends every host on the unreachable probe and revocation silently no-ops while still reporting success; got %v", wantIDs)
	}
	if got := destroyTaskByID(t, tasks, "destroy.container-clusters").Entry.Dependencies; !reflect.DeepEqual(got, []string{"destroy.machine-infra"}) {
		t.Fatalf("container runtime cleanup must require successful machine teardown so KubeVirt host credentials and retry evidence survive a failed guest deletion, got %v", got)
	}
	if got := destroyTaskByID(t, tasks, "destroy.machine-registration").Entry.OrderingDependencies; slices.Contains(got, "destroy.infra-components") {
		t.Fatalf("RHSM deregistration runs through bootwright_proxy_env, so the proxy InfraComponent must still exist: %v", got)
	}
	if got := destroyTaskByID(t, tasks, "destroy.provider-services").Entry.OrderingDependencies; !slices.Contains(got, "destroy.container-clusters") {
		t.Fatalf("provider services must stay serialised behind container-cluster teardown: container_cluster_agent_install/tasks/destroy.yml restarts systemd-resolved on the controller that every other destroy play resolves its SSH targets through; got %v", got)
	}
}

func TestPlanDestroyTasksRelinksOrderingAcrossSkippedSteps(t *testing.T) {
	tasks, err := PlanDestroyTasks("all", v1alpha1.State{}, "", nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	assertDestroyOrderingEdges(t, tasks, map[string][]string{
		"destroy.machine-registration": nil,
		"destroy.infra-components":     {"destroy.machine-registration"},
		"destroy.machine-infra":        {"destroy.infra-components"},
		"destroy.container-clusters":   {"destroy.machine-infra"},
		"destroy.provider-services":    {"destroy.machine-infra", "destroy.container-clusters"},
	})

	clusters, err := PlanDestroyTasks("clusters", v1alpha1.State{}, "limit", nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	assertDestroyOrderingEdges(t, clusters, map[string][]string{
		"destroy.container-clusters": nil,
	})
}

func TestPlanDestroyTasksRejectsUnknownScope(t *testing.T) {
	if _, err := PlanDestroyTasks("bogus", v1alpha1.State{}, "", nil, nil); err == nil {
		t.Fatal("expected an error for an unsupported destroy scope")
	}
}

func TestPlanDestroyTasksOrdersKubeVirtTenantBeforeHost(t *testing.T) {
	state := destroyKubeVirtDependencyState("child", "host")
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Entry.Kind != DestroyTaskKindMachineInfra {
			continue
		}
		want := DestroyClusterOrderExtraVar + "=child,host"
		if !slices.Contains(task.ExtraVarPairs, want) {
			t.Fatalf("machine teardown extra vars = %v, want %q", task.ExtraVarPairs, want)
		}
		return
	}
	t.Fatal("full destroy plan has no machine infrastructure task")
}

func TestPlanDestroyTasksRejectsKubeVirtHostCycle(t *testing.T) {
	state := destroyKubeVirtDependencyState("a", "b")
	second := destroyKubeVirtDependencyState("b", "a")
	state.Machines = append(state.Machines, second.Machines...)
	state.InfraProviders = append(state.InfraProviders, second.InfraProviders...)
	state.ContainerClusters = []v1alpha1.ContainerCluster{state.ContainerClusters[0], second.ContainerClusters[0]}
	if _, err := PlanDestroyTasks("all", state, "", nil, nil); err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("KubeVirt host cycle error = %v", err)
	}
}

func destroyKubeVirtDependencyState(child, host string) v1alpha1.State {
	machineName := child + "-m0"
	providerName := child + "-kubevirt"
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{
			{
				Metadata: v1alpha1.Metadata{Name: child},
				Spec: v1alpha1.ContainerClusterSpec{
					Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: machineName}}},
				},
			},
			{Metadata: v1alpha1.Metadata{Name: host}},
		},
		Machines: []v1alpha1.Machine{
			{
				Metadata: v1alpha1.Metadata{Name: machineName},
				Spec: v1alpha1.MachineSpec{
					Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: providerName}},
				},
			},
		},
		InfraProviders: []v1alpha1.InfraProvider{
			{
				Metadata: v1alpha1.Metadata{Name: providerName},
				Spec: v1alpha1.InfraProviderSpec{
					Type: v1alpha1.ProvisionerKubeVirt,
					KubeVirt: &v1alpha1.InfraProviderKubeVirt{
						HostClusterRef: &v1alpha1.LocalObjectReference{Name: host},
					},
				},
			},
		},
	}
}

func TestPlanDestroyTasksStorageWorkSetGate(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{
			{Metadata: v1alpha1.Metadata{Name: "ceph-render-ref"}},
			{Metadata: v1alpha1.Metadata{Name: "ceph-selected"}},
		},
	}

	containerOnly, err := PlanDestroyTasks("clusters", state, "limit", nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range containerOnly {
		if task.Entry.Kind == DestroyTaskKindStorageCluster {
			t.Fatalf("container-only selection must plan no storage teardown step; got %+v", task.Entry)
		}
		if task.Entry.Kind == DestroyTaskKindStorageNodeAccess {
			t.Fatalf("container-only selection must plan no storage node access revoke step; got %+v", task.Entry)
		}
	}

	narrowed, err := PlanDestroyTasks("clusters", state, "limit", nil, []string{"ceph-selected"})
	if err != nil {
		t.Fatal(err)
	}
	var storageTask, nodeAccessTask *ApplyTask
	for i := range narrowed {
		switch narrowed[i].Entry.Kind {
		case DestroyTaskKindStorageCluster:
			storageTask = &narrowed[i]
		case DestroyTaskKindStorageNodeAccess:
			nodeAccessTask = &narrowed[i]
		}
	}
	if storageTask == nil {
		t.Fatal("storage-narrowed selection must plan a storage teardown step")
	}
	if len(storageTask.Entry.ResourceKeys) != 1 || storageTask.Entry.ResourceKeys[0] != "ceph-selected" {
		t.Fatalf("storage step must cover only the selected root; got %v", storageTask.Entry.ResourceKeys)
	}
	if nodeAccessTask == nil {
		t.Fatal("storage-narrowed selection must plan a storage node access revoke step")
	}
	if len(nodeAccessTask.Entry.ResourceKeys) != 1 || nodeAccessTask.Entry.ResourceKeys[0] != "ceph-selected" {
		t.Fatalf("storage node access revoke step must cover only the selected root; got %v", nodeAccessTask.Entry.ResourceKeys)
	}
	for _, pair := range storageTask.ExtraVarPairs {
		if strings.HasPrefix(pair, DestroyStorageScopeExtraVar+"=") {
			t.Fatalf("planner must not emit the storage-scope allowlist (composed centrally); got %q", pair)
		}
	}

	unscoped, err := PlanDestroyTasks("clusters", state, "limit", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var unscopedStorage *ApplyTask
	for i := range unscoped {
		if unscoped[i].Entry.Kind == DestroyTaskKindStorageCluster {
			unscopedStorage = &unscoped[i]
		}
	}
	if unscopedStorage == nil || len(unscopedStorage.Entry.ResourceKeys) != 2 {
		t.Fatalf("unscoped destroy must tear down every storage cluster; got %+v", unscopedStorage)
	}
}

func TestDestroyChainSetsForksForEveryStep(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	members := render.HostGroupMembers(state)
	hostCount := func(groups ...string) int {
		set := map[string]bool{}
		for _, group := range groups {
			for _, host := range members[group] {
				set[host] = true
			}
		}
		return len(set)
	}
	bounded := func(count int) int {
		if count < 1 {
			return 1
		}
		if count > destroyMaxForks {
			return destroyMaxForks
		}
		return count
	}
	inventory := hostCount(
		render.GroupStorageHosts, render.GroupInfraComponentHosts, render.GroupProviderHosts,
		render.GroupOCPHosts, render.GroupInfraHosts, render.GroupMachineTaskHosts,
		render.GroupBootHosts, render.GroupControllerHosts,
	)
	storageHosts := hostCount(render.GroupStorageHosts)
	if storageHosts < 2 || storageHosts >= inventory {
		t.Fatalf("fixture must have several storage hosts and a strictly larger inventory to prove the fork limit is per step; storage=%d inventory=%d", storageHosts, inventory)
	}
	want := map[string]int{
		"destroy.storage-clusters":     bounded(storageHosts),
		"destroy.machine-registration": bounded(storageHosts),
		"destroy.storage-node-access":  bounded(storageHosts),
		"destroy.infra-components":     bounded(hostCount(render.GroupInfraComponentHosts)),
		"destroy.container-clusters":   bounded(hostCount(render.GroupOCPHosts)),
		"destroy.provider-services":    bounded(hostCount(render.GroupProviderHosts)),
		"destroy.machine-infra":        bounded(hostCount(render.GroupMachineTaskHosts, render.GroupProviderHosts, render.GroupInfraHosts)),
	}
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != len(want) {
		t.Fatalf("planned %v, want forks for %d steps", destroyTaskIDs(tasks), len(want))
	}
	for _, task := range tasks {
		if task.Forks < 1 {
			t.Fatalf("task %q runs at the Ansible built-in default of 5 forks because the planner left Forks unset", task.Entry.ID)
		}
		if task.Forks > destroyMaxForks {
			t.Fatalf("task %q forks = %d, want at most %d", task.Entry.ID, task.Forks, destroyMaxForks)
		}
		if got := task.Forks; got != want[task.Entry.ID] {
			t.Fatalf("task %q forks = %d, want %d (one worker per host the step's own play targets)", task.Entry.ID, got, want[task.Entry.ID])
		}
	}
	for _, id := range []string{"destroy.storage-clusters", "destroy.machine-registration", "destroy.storage-node-access"} {
		if got := destroyTaskByID(t, tasks, id).Forks; got >= inventory {
			t.Fatalf("task %q forks = %d, want the %d storage hosts rather than the whole %d-host inventory", id, got, storageHosts, inventory)
		}
	}
}

func TestDestroyChainClampsForksOnWideGroups(t *testing.T) {
	hosts := make([]string, 0, destroyMaxForks*2)
	for i := 0; i < destroyMaxForks*2; i++ {
		hosts = append(hosts, "node"+strings.Repeat("x", i%3)+string(rune('a'+i)))
	}
	step := destroyStep{id: "destroy.wide", forksLimit: strings.Join(hosts, ":")}
	if got := destroyStepForks(v1alpha1.State{}, step, ""); got != destroyMaxForks {
		t.Fatalf("wide destroy step forks = %d, want the %d clamp so a large environment cannot fork-bomb the controller", got, destroyMaxForks)
	}
	empty := destroyStep{id: "destroy.empty", forksLimit: render.GroupStorageHosts}
	if got := destroyStepForks(v1alpha1.State{}, empty, ""); got != 1 {
		t.Fatalf("empty group forks = %d, want 1", got)
	}
}
