package installation

import (
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// roleTasks is where the installation role's entry points live in the
// embedded collection.
const roleTasks = "collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/"

// Progress settles only the groups the role reports, against the groups the
// plan declares, so a declared group the role never reports leaves the block's
// count short and its phase shown under another group. Each entry point
// therefore reports every group its verb declares, running and then ok, in
// the declared order, and no group it does not declare.
func TestTheRoleReportsEveryGroupThePlanDeclares(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog())
	for verb, file := range map[reconciliation.Verb]string{reconciliation.Apply: "apply.yml", reconciliation.Destroy: "destroy.yml"} {
		var declared []string
		for _, group := range groups(verb, request) {
			declared = append(declared, group.ID)
		}
		running, settled := reportedGroups(t, roleTasks+file)
		if !slices.Equal(running, declared) {
			t.Errorf("%s reports groups running in the order %q, and the plan declares %q", file, running, declared)
		}
		if !slices.Equal(settled, declared) {
			t.Errorf("%s reports groups ok in the order %q, and the plan declares %q", file, settled, declared)
		}
	}
}

// reportedGroups lists, in task order with blocks opened, the groups a task
// file reports running and the groups it reports ok.
func reportedGroups(t *testing.T, path string) (running, settled []string) {
	t.Helper()
	data, ok := ansible.Assets()[path]
	if !ok {
		t.Fatalf("the embedded collection carries no %s", path)
	}
	var tasks []map[string]any
	if err := yaml.Unmarshal(data, &tasks); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var walk func([]map[string]any)
	walk = func(listed []map[string]any) {
		for _, task := range listed {
			if report, ok := task["bootwright.core.managedos_install_protocol"].(map[string]any); ok && report["phase"] == "group" {
				group, _ := report["group"].(string)
				switch report["status"] {
				case "running":
					running = append(running, group)
				case "ok":
					settled = append(settled, group)
				}
			}
			for _, section := range []string{"block", "rescue", "always"} {
				nested, _ := task[section].([]any)
				var opened []map[string]any
				for _, item := range nested {
					if child, ok := item.(map[string]any); ok {
						opened = append(opened, child)
					}
				}
				walk(opened)
			}
		}
	}
	walk(tasks)
	return running, settled
}
