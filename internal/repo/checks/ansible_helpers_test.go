package repocheck

import (
	"fmt"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const bootwrightCollectionRoleRoot = "ansible/collections/ansible_collections/bootwright/core/roles"

func assertRedactsByDefault(t *testing.T, name string, noLog any) {
	t.Helper()
	if noLog == true {
		return
	}
	s := fmt.Sprint(noLog)
	if !strings.Contains(s, "bootwright_no_log | default(true) | bool") {
		t.Fatalf("%s must redact by default, got no_log=%v", name, noLog)
	}
}

func ansibleCfgValue(body, section, key string) (string, bool) {
	current := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			continue
		}
		if current != section {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == key {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

func readAnsibleTasks(t *testing.T, rel string) []map[string]any {
	t.Helper()
	var tasks []map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, rel)), &tasks); err != nil {
		t.Fatalf("%s: decode YAML: %v", rel, err)
	}
	return tasks
}

func readAnsibleTasksFromFiles(t *testing.T, rels ...string) []map[string]any {
	t.Helper()
	var tasks []map[string]any
	for _, rel := range rels {
		tasks = append(tasks, readAnsibleTasks(t, rel)...)
	}
	return tasks
}

func readAnsiblePlays(t *testing.T, rel string) []map[string]any {
	t.Helper()
	var plays []map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, rel)), &plays); err != nil {
		t.Fatalf("%s: decode YAML: %v", rel, err)
	}
	return plays
}

func nestedAnsibleTasks(t *testing.T, task map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := task[key].([]any)
	if !ok {
		t.Fatalf("%s has no %s task list", task["name"], key)
	}
	tasks := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		child, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s %s[%d] is not a task map", task["name"], key, i)
		}
		tasks = append(tasks, child)
	}
	return tasks
}

func checkShellTaskGuards(t *testing.T, rel string, tasks []map[string]any) {
	t.Helper()
	for _, task := range tasks {
		if _, ok := task["ansible.builtin.shell"]; ok {
			if _, ok := task["changed_when"]; !ok {
				t.Errorf("%s task %q uses ansible.builtin.shell without changed_when", rel, task["name"])
			}
			if _, ok := task["failed_when"]; !ok {
				t.Errorf("%s task %q uses ansible.builtin.shell without failed_when", rel, task["name"])
			}
		}
		for _, key := range []string{"block", "rescue", "always"} {
			raw, ok := task[key].([]any)
			if !ok {
				continue
			}
			children := make([]map[string]any, 0, len(raw))
			for i, item := range raw {
				child, ok := item.(map[string]any)
				if !ok {
					t.Fatalf("%s task %q %s[%d] is not a task map", rel, task["name"], key, i)
				}
				children = append(children, child)
			}
			checkShellTaskGuards(t, rel, children)
		}
	}
}

func collectAnsibleMessages(tasks []map[string]any, out *[]string) {
	for _, task := range tasks {
		for _, module := range []string{"ansible.builtin.assert", "ansible.builtin.debug", "ansible.builtin.fail"} {
			switch body := task[module].(type) {
			case map[string]any:
				for _, key := range []string{"fail_msg", "success_msg", "msg"} {
					if value, ok := body[key]; ok {
						*out = append(*out, fmt.Sprint(value))
					}
				}
			case string:
				*out = append(*out, body)
			}
		}
		for _, key := range []string{"block", "rescue", "always"} {
			raw, ok := task[key].([]any)
			if !ok {
				continue
			}
			children := make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				child, ok := item.(map[string]any)
				if ok {
					children = append(children, child)
				}
			}
			collectAnsibleMessages(children, out)
		}
	}
}

func hasHostProxyFactsImport(tasks []any) bool {
	for _, raw := range tasks {
		task, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if task["name"] != "Resolve proxy environment" {
			continue
		}
		importRole, ok := task["ansible.builtin.import_role"].(map[string]any)
		if !ok {
			continue
		}
		if importRole["name"] == "bootwright.core.machine_proxy" && importRole["tasks_from"] == "facts" {
			return true
		}
	}
	return false
}

func findAnsibleTask(t *testing.T, tasks []map[string]any, name string) int {
	t.Helper()
	if idx := findAnsibleTaskIndex(tasks, name); idx >= 0 {
		return idx
	}
	t.Fatalf("missing Ansible task %q", name)
	return -1
}

func findAnsibleTaskIndex(tasks []map[string]any, name string) int {
	for i, task := range tasks {
		if got, _ := task["name"].(string); got == name {
			return i
		}
	}
	return -1
}

func findAnsibleTaskByPrefix(t *testing.T, tasks []map[string]any, prefix string) int {
	t.Helper()
	for i, task := range tasks {
		if got, _ := task["name"].(string); strings.HasPrefix(got, prefix) {
			return i
		}
	}
	t.Fatalf("missing Ansible task with prefix %q", prefix)
	return -1
}

func assertIncludeTasksFile(t *testing.T, task map[string]any, want string) {
	t.Helper()
	include := task["ansible.builtin.include_tasks"]
	if include == nil {
		include = task["ansible.builtin.import_tasks"]
	}
	if include == nil {
		assertIncludeRoleTasksFrom(t, task, want)
		return
	}
	switch got := include.(type) {
	case string:
		if strings.TrimSpace(got) != want {
			t.Fatalf("%s tasks file got %q, want %q", task["name"], got, want)
		}
	case map[string]any:
		file, ok := got["file"].(string)
		if !ok {
			t.Fatalf("%s tasks include/import has no file", task["name"])
		}
		if strings.TrimSpace(file) != want {
			t.Fatalf("%s tasks file got %q, want %q", task["name"], file, want)
		}
	default:
		t.Fatalf("%s is not an include_tasks or import_tasks task", task["name"])
	}
}

func assertIncludeRoleTasksFrom(t *testing.T, task map[string]any, want string) {
	t.Helper()
	include, ok := task["ansible.builtin.include_role"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an include_tasks, import_tasks or include_role task", task["name"])
	}
	if name, _ := include["name"].(string); name != "bootwright.core.machine_os_identity" {
		t.Fatalf("%s includes role %q, want the shared identity role bootwright.core.machine_os_identity", task["name"], name)
	}
	from, _ := include["tasks_from"].(string)
	if strings.TrimSpace(from) != want {
		t.Fatalf("%s tasks_from got %q, want %q", task["name"], from, want)
	}
}

func assertIncludeRoleName(t *testing.T, task map[string]any, want string) {
	t.Helper()
	include, ok := task["ansible.builtin.include_role"].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an include_role task", task["name"])
	}
	if got := strings.TrimSpace(include["name"].(string)); got != want {
		t.Fatalf("%s include_role got %q", task["name"], got)
	}
}

func stringListContains(v any, want string) bool {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if item == want {
				return true
			}
		}
	case []string:
		for _, item := range x {
			if item == want {
				return true
			}
		}
	case string:
		return x == want
	}
	return false
}

func stringListItemContains(v any, want string) bool {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if text, ok := item.(string); ok && strings.Contains(text, want) {
				return true
			}
		}
	case []string:
		for _, item := range x {
			if strings.Contains(item, want) {
				return true
			}
		}
	case string:
		return strings.Contains(x, want)
	}
	return false
}

func intListEqual(v any, want []int) bool {
	var got []int
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			n, ok := item.(int)
			if !ok {
				return false
			}
			got = append(got, n)
		}
	case []int:
		got = x
	default:
		return false
	}
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
