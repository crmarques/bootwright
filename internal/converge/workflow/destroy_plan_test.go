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
	wantIDs := []string{"destroy.machine-infra", "destroy.infra-components", "destroy.provider-services"}
	if len(tasks) != len(wantIDs) {
		t.Fatalf("planned %d tasks, want %d: %+v", len(tasks), len(wantIDs), tasks)
	}
	for i, task := range tasks {
		if task.Entry.ID != wantIDs[i] {
			t.Fatalf("task[%d] = %s, want %s", i, task.Entry.ID, wantIDs[i])
		}
		// Every task reuses the run's limit and extra-vars unchanged — the
		// safety property that keeps the split equivalent to the monolith.
		if task.Limit != limit {
			t.Fatalf("task[%d] limit = %q, want %q", i, task.Limit, limit)
		}
		if len(task.ExtraVarPairs) != 1 || task.ExtraVarPairs[0] != extra[0] {
			t.Fatalf("task[%d] extra-vars = %v, want %v", i, task.ExtraVarPairs, extra)
		}
		// Sequential chain: each task is ORDERING-sequenced after the previous
		// (teardown order preserved) without a hard dependency, so a failed stage
		// does not block the rest. Hard Dependencies stay empty.
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
	wantIDs := []string{"destroy.storage-clusters", "destroy.container-clusters"}
	if len(tasks) != 2 || tasks[0].Entry.ID != wantIDs[0] || tasks[1].Entry.ID != wantIDs[1] {
		t.Fatalf("clusters chain = %+v, want %v", tasks, wantIDs)
	}
	if len(tasks[1].Entry.OrderingDependencies) != 1 || tasks[1].Entry.OrderingDependencies[0] != wantIDs[0] {
		t.Fatalf("container destroy must be ordering-sequenced after storage destroy: %v", tasks[1].Entry.OrderingDependencies)
	}
	if len(tasks[1].Entry.Dependencies) != 0 {
		t.Fatalf("destroy steps must not carry hard deps (ordering only): %v", tasks[1].Entry.Dependencies)
	}
}

// TestPlanDestroyTasksAllChain locks in the whole-context (stage omitted)
// teardown order: the clusters chain first, then the infra they ran on, as one
// sequential ORDERING chain (no hard deps) — the reverse of the apply order.
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
		"destroy.machine-infra",
		"destroy.infra-components",
		"destroy.provider-services",
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
}

func TestPlanDestroyTasksRejectsUnknownScope(t *testing.T) {
	if _, err := PlanDestroyTasks("bogus", v1alpha1.State{}, "", nil, nil); err == nil {
		t.Fatal("expected an error for an unsupported destroy scope")
	}
}

// TestPlanDestroyTasksStorageWorkSetGate is the regression guard for the scoped
// destroy bug: a --clusters selection that names container clusters but no
// storage cluster (storageWorkNames is a non-nil empty slice) must NOT plan a
// storage teardown step, even though the render-inclusive state still carries
// the managed StorageCluster pulled in by the container cluster's
// data-foundation attachment. A non-empty work set keeps the step and labels it
// with the named roots. The allowlist extra-var that gates the wipe is composed
// centrally (converge.ApplyDestroyScopeExtraVars), not by the planner, so this
// also locks that the planner does NOT re-emit it.
func TestPlanDestroyTasksStorageWorkSetGate(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{
			{Metadata: v1alpha1.Metadata{Name: "ceph-render-ref"}},
			{Metadata: v1alpha1.Metadata{Name: "ceph-selected"}},
		},
	}

	// Container-only selection: empty (non-nil) storage work set drops the step.
	containerOnly, err := PlanDestroyTasks("clusters", state, "limit", nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range containerOnly {
		if task.Entry.Kind == DestroyTaskKindStorageCluster {
			t.Fatalf("container-only selection must plan no storage teardown step; got %+v", task.Entry)
		}
	}

	// Storage-narrowed selection: the step runs and is labelled with the named
	// root only; the render reference is excluded. The planner must NOT emit the
	// allowlist extra-var itself — that is the central composer's job, so the gate
	// is single-sourced across the task-graph and single-playbook paths.
	narrowed, err := PlanDestroyTasks("clusters", state, "limit", nil, []string{"ceph-selected"})
	if err != nil {
		t.Fatal(err)
	}
	var storageTask *ApplyTask
	for i := range narrowed {
		if narrowed[i].Entry.Kind == DestroyTaskKindStorageCluster {
			storageTask = &narrowed[i]
		}
	}
	if storageTask == nil {
		t.Fatal("storage-narrowed selection must plan a storage teardown step")
	}
	if len(storageTask.Entry.ResourceKeys) != 1 || storageTask.Entry.ResourceKeys[0] != "ceph-selected" {
		t.Fatalf("storage step must cover only the selected root; got %v", storageTask.Entry.ResourceKeys)
	}
	for _, pair := range storageTask.ExtraVarPairs {
		if strings.HasPrefix(pair, DestroyStorageScopeExtraVar+"=") {
			t.Fatalf("planner must not emit the storage-scope allowlist (composed centrally); got %q", pair)
		}
	}

	// No selection (nil): teardown covers every rendered storage cluster.
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
