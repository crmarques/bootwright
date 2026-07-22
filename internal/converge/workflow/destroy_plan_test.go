package workflow

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPlanDestroyTasksInfraChain(t *testing.T) {
	limit := "bootwright_provider_hosts:bootwright_infra_component_hosts:bootwright_infra_hosts"
	extra := []string{"bootwright_infra_destroy_context_sweep=true"}
	tasks, err := PlanDestroyTasks("infra", v1alpha1.State{}, limit, extra, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"destroy.machine-registration", "destroy.machine-infra", "destroy.infra-components", "destroy.provider-services"}
	if len(tasks) != len(wantIDs) {
		t.Fatalf("planned %d tasks, want %d: %+v", len(tasks), len(wantIDs), tasks)
	}
	for i, task := range tasks {
		if task.Entry.ID != wantIDs[i] {
			t.Fatalf("task[%d] = %s, want %s", i, task.Entry.ID, wantIDs[i])
		}
		wantLimit := limit
		if task.Entry.ID == "destroy.machine-registration" {
			wantLimit = "bootwright_storage_hosts"
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
		if i == 0 {
			if len(task.Entry.OrderingDependencies) != 0 {
				t.Fatalf("first task ordering deps = %v, want none", task.Entry.OrderingDependencies)
			}
		} else if len(task.Entry.OrderingDependencies) != 1 || task.Entry.OrderingDependencies[0] != wantIDs[i-1] {
			t.Fatalf("task[%d] ordering deps = %v, want [%s]", i, task.Entry.OrderingDependencies, wantIDs[i-1])
		}
	}
	if tasks[0].Playbook == "" || tasks[0].Playbook == tasks[2].Playbook {
		t.Fatalf("tasks must carry distinct destroy playbooks: %q / %q", tasks[0].Playbook, tasks[2].Playbook)
	}
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
		"destroy.container-clusters",
		"destroy.machine-registration",
		"destroy.machine-infra",
		"destroy.infra-components",
		"destroy.provider-services",
		"destroy.storage-node-access",
	}
	if len(tasks) != len(wantIDs) {
		t.Fatalf("planned %d tasks, want %d: %+v", len(tasks), len(wantIDs), tasks)
	}
	for i, task := range tasks {
		if task.Entry.ID != wantIDs[i] {
			t.Fatalf("task[%d] = %s, want %s", i, task.Entry.ID, wantIDs[i])
		}
		if len(task.ExtraVarPairs) != 1 || task.ExtraVarPairs[0] != extra[0] {
			t.Fatalf("task[%d] extra-vars = %v, want %v", i, task.ExtraVarPairs, extra)
		}
		if len(task.Entry.Dependencies) != 0 {
			t.Fatalf("task[%d] hard deps = %v, want none (ordering only)", i, task.Entry.Dependencies)
		}
		if i == 0 {
			if len(task.Entry.OrderingDependencies) != 0 {
				t.Fatalf("first task ordering deps = %v, want none", task.Entry.OrderingDependencies)
			}
		} else if len(task.Entry.OrderingDependencies) != 1 || task.Entry.OrderingDependencies[0] != wantIDs[i-1] {
			t.Fatalf("task[%d] ordering deps = %v, want [%s]", i, task.Entry.OrderingDependencies, wantIDs[i-1])
		}
	}
	if last := tasks[len(tasks)-1]; last.Entry.Kind != DestroyTaskKindStorageNodeAccess {
		t.Fatalf("storage node access revoke must run last in the full destroy chain, after Machine registration and every other step that still needs the storage hosts' rendered identity; got last kind %q", last.Entry.Kind)
	}
}

func TestPlanDestroyTasksRejectsUnknownScope(t *testing.T) {
	if _, err := PlanDestroyTasks("bogus", v1alpha1.State{}, "", nil, nil); err == nil {
		t.Fatal("expected an error for an unsupported destroy scope")
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
