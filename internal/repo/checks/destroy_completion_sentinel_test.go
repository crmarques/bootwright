package repocheck

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const destroyCompletionTasksPath = "ansible/collections/ansible_collections/bootwright/core/playbooks/tasks/destroy_completion.yml"

func TestDestroyCompletionSentinelIsExactAndFinal(t *testing.T) {
	tasks := readAnsibleTasks(t, destroyCompletionTasksPath)
	if len(tasks) != 1 {
		t.Fatalf("destroy completion task count = %d, want 1", len(tasks))
	}
	task := tasks[0]
	if task["name"] != "Record Bootwright destroy completion" || task["changed_when"] != false || len(task) != 3 {
		t.Fatalf("destroy completion sentinel must remain one exact non-mutating task, got %v", task)
	}
	facts, ok := task["ansible.builtin.set_fact"].(map[string]any)
	if !ok || !reflect.DeepEqual(facts, map[string]any{"bootwright_destroy_completion_recorded": true}) {
		t.Fatalf("destroy completion sentinel facts = %v", task["ansible.builtin.set_fact"])
	}
}

func TestEveryNonStorageDestroyPlayEndsWithExactCompletionSentinel(t *testing.T) {
	cases := []struct {
		path  string
		hosts string
	}{
		{"ansible/collections/ansible_collections/bootwright/core/playbooks/task_container_cluster_agent_destroy.yml", "bootwright_ocp_hosts"},
		{"ansible/collections/ansible_collections/bootwright/core/playbooks/task_container_cluster_runtime_destroy.yml", "bootwright_ocp_hosts"},
		{"ansible/collections/ansible_collections/bootwright/core/playbooks/task_controller_name_resolution_destroy_cleanup.yml", "bootwright_controller_hosts"},
		{"ansible/collections/ansible_collections/bootwright/core/playbooks/task_controller_name_resolution_destroy_preflight.yml", "bootwright_controller_hosts"},
		{"ansible/collections/ansible_collections/bootwright/core/playbooks/task_infra_component_services_destroy.yml", "bootwright_infra_component_hosts"},
		{"ansible/collections/ansible_collections/bootwright/core/playbooks/task_provider_services_destroy.yml", "bootwright_provider_hosts"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			plays := readAnsiblePlays(t, tc.path)
			if len(plays) != 1 || fmt.Sprint(plays[0]["hosts"]) != tc.hosts {
				t.Fatalf("completion play shape = %v, want one play on %s", plays, tc.hosts)
			}
			tasks := destroyCompletionTaskList(t, plays[0], "tasks")
			if len(tasks) == 0 || !isDestroyCompletionInclude(tasks[len(tasks)-1]) {
				t.Fatalf("last task must emit exact destroy completion after every mutator, got %v", tasks)
			}
			if got := countDestroyCompletionIncludes(tasks); got != 1 {
				t.Fatalf("destroy completion include count = %d, want 1", got)
			}
		})
	}
}

func TestMachineInfraHasOneFinalUnionCompletionPlay(t *testing.T) {
	const path = "ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_infra_destroy.yml"
	plays := readAnsiblePlays(t, path)
	count := 0
	for _, play := range plays {
		tasks := destroyCompletionTaskList(t, play, "tasks")
		includes := countDestroyCompletionIncludes(tasks)
		count += includes
		if includes == 0 {
			continue
		}
		if fmt.Sprint(play["hosts"]) != "bootwright_machine_task_hosts:bootwright_provider_hosts:bootwright_infra_hosts" || includes != 1 || !isDestroyCompletionInclude(tasks[len(tasks)-1]) {
			t.Fatalf("machine infra completion must be one final union play, got hosts=%v tasks=%v", play["hosts"], tasks)
		}
	}
	if count != 1 {
		t.Fatalf("machine infra completion include count = %d, want 1", count)
	}
}

func TestReachabilityTolerantDestroyBranchesProveCompletionBeforePositiveEndHost(t *testing.T) {
	cases := []string{
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_machine_registration_deregister.yml",
		"ansible/collections/ansible_collections/bootwright/core/playbooks/task_storage_node_access_destroy.yml",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			plays := readAnsiblePlays(t, path)
			if len(plays) != 1 || fmt.Sprint(plays[0]["hosts"]) != "bootwright_storage_hosts" {
				t.Fatalf("reachability-tolerant destroy play shape = %v", plays)
			}
			play := plays[0]
			for _, section := range []string{"pre_tasks", "tasks"} {
				tasks := destroyCompletionTaskList(t, play, section)
				for i, task := range tasks {
					if fmt.Sprint(task["ansible.builtin.meta"]) != "end_host" {
						continue
					}
					name := fmt.Sprint(task["name"])
					when := fmt.Sprint(task["when"])
					if name == "End nodes Bootwright cannot reach or escalate on" {
						if !strings.Contains(when, "unreachable") || (i > 0 && isDestroyCompletionInclude(tasks[i-1])) {
							t.Fatalf("unproven unreachable branch must end without completion: %v", task)
						}
						continue
					}
					if i == 0 || !isDestroyCompletionInclude(tasks[i-1]) || !reflect.DeepEqual(tasks[i-1]["when"], task["when"]) {
						t.Fatalf("positive no-work end_host %q lacks an immediately preceding completion with the same predicate", name)
					}
				}
			}
			tasks := destroyCompletionTaskList(t, play, "tasks")
			if len(tasks) == 0 || !isDestroyCompletionInclude(tasks[len(tasks)-1]) {
				t.Fatalf("reachable mutation path does not end in exact completion: %v", tasks)
			}
		})
	}
}

func destroyCompletionTaskList(t *testing.T, play map[string]any, section string) []map[string]any {
	t.Helper()
	raw, ok := play[section].([]any)
	if !ok {
		return nil
	}
	tasks := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		task, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s[%d] is not an Ansible task: %v", section, i, item)
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func isDestroyCompletionInclude(task map[string]any) bool {
	return task["ansible.builtin.include_tasks"] == "tasks/destroy_completion.yml"
}

func countDestroyCompletionIncludes(tasks []map[string]any) int {
	count := 0
	for _, task := range tasks {
		if isDestroyCompletionInclude(task) {
			count++
		}
	}
	return count
}
