package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestControllerVirtctlUsesScalarVersionFilter(t *testing.T) {
	tasks := readAnsibleTasks(
		t,
		"ansible/collections/ansible_collections/bootwright/core/roles/controller_virtctl/tasks/main.yml",
	)
	for _, name := range []string{
		"Decide whether virtctl needs install (require exact server version match)",
		"Verify installed virtctl matches the host cluster KubeVirt version",
	} {
		task := tasks[findAnsibleTask(t, tasks, name)]
		vars, ok := task["vars"].(map[string]any)
		if !ok {
			t.Fatalf("%s must declare version parsing vars", name)
		}
		expression := fmt.Sprint(vars["bootwright_virtctl_installed_client_version"])
		if !strings.Contains(expression, "| bootwright.core.bootwright_virtctl_version") {
			t.Fatalf("%s must parse the client version with the scalar collection filter, got %q", name, expression)
		}
		if strings.Contains(expression, "regex_search") {
			t.Fatalf("%s must not use grouped regex_search, got %q", name, expression)
		}
	}
}
