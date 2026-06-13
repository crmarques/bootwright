package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPlanDestroyTasksInfraChain(t *testing.T) {
	limit := "bootwright_provider_hosts:bootwright_infra_component_hosts:bootwright_infra_hosts"
	extra := []string{"bootwright_infra_destroy_context_sweep=true"}
	tasks, err := PlanDestroyTasks("infra", v1alpha1.State{}, limit, extra)
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
		// Sequential chain: each task depends on the previous (teardown order).
		if i == 0 {
			if len(task.Entry.Dependencies) != 0 {
				t.Fatalf("first task deps = %v, want none", task.Entry.Dependencies)
			}
		} else if len(task.Entry.Dependencies) != 1 || task.Entry.Dependencies[0] != wantIDs[i-1] {
			t.Fatalf("task[%d] deps = %v, want [%s]", i, task.Entry.Dependencies, wantIDs[i-1])
		}
	}
	if tasks[0].Playbook == "" || tasks[0].Playbook == tasks[2].Playbook {
		t.Fatalf("tasks must carry distinct destroy playbooks: %q / %q", tasks[0].Playbook, tasks[2].Playbook)
	}
}

func TestPlanDestroyTasksClustersChain(t *testing.T) {
	tasks, err := PlanDestroyTasks("clusters", v1alpha1.State{}, "limit", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"destroy.storage-clusters", "destroy.container-clusters"}
	if len(tasks) != 2 || tasks[0].Entry.ID != wantIDs[0] || tasks[1].Entry.ID != wantIDs[1] {
		t.Fatalf("clusters chain = %+v, want %v", tasks, wantIDs)
	}
	if len(tasks[1].Entry.Dependencies) != 1 || tasks[1].Entry.Dependencies[0] != wantIDs[0] {
		t.Fatalf("container destroy must follow storage destroy: %v", tasks[1].Entry.Dependencies)
	}
}

func TestPlanDestroyTasksRejectsUnknownScope(t *testing.T) {
	if _, err := PlanDestroyTasks("all", v1alpha1.State{}, "", nil); err == nil {
		t.Fatal("expected an error for an unsupported destroy scope")
	}
}
