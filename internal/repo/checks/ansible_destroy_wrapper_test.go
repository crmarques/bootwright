package repocheck

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"go.yaml.in/yaml/v3"
)

func destroyWrapperImportPositions(t *testing.T, path string) map[string]int {
	t.Helper()
	var imports []map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, path)), &imports); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	out := map[string]int{}
	for i, entry := range imports {
		name, _ := entry["import_playbook"].(string)
		if name == "" {
			t.Fatalf("%s import %d has no import_playbook", path, i)
		}
		out[name] = i
	}
	return out
}

func destroyPlaybookFile(playbook string) string {
	return strings.TrimPrefix(playbook, "bootwright.core.") + ".yml"
}

func TestDestroyWrappersAreATopologicalOrderOfTheGeneratedGraph(t *testing.T) {
	for _, tc := range []struct{ scope, path string }{
		{"infra", "ansible/collections/ansible_collections/bootwright/core/playbooks/workflow_infra_destroy.yml"},
		{"clusters", "ansible/collections/ansible_collections/bootwright/core/playbooks/workflow_clusters_destroy.yml"},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			tasks, err := workflow.PlanDestroyTasks(tc.scope, v1alpha1.State{}, "", nil, nil)
			if err != nil {
				t.Fatalf("plan %s destroy: %v", tc.scope, err)
			}
			position := destroyWrapperImportPositions(t, tc.path)
			playbookOf := map[string]string{}
			for _, task := range tasks {
				file := destroyPlaybookFile(task.Playbook)
				if _, ok := position[file]; !ok {
					t.Fatalf("%s runs %s in the split path but the monolith wrapper never imports it; the no-remote-work path would silently skip that teardown step", tc.scope, file)
				}
				playbookOf[task.Entry.ID] = file
			}
			for _, task := range tasks {
				self := playbookOf[task.Entry.ID]
				edges := append(append([]string(nil), task.Entry.Dependencies...), task.Entry.OrderingDependencies...)
				for _, dep := range edges {
					other, ok := playbookOf[dep]
					if !ok || other == self {
						continue
					}
					if position[other] >= position[self] {
						t.Fatalf("%s: %s depends on %s, but the wrapper imports %s at %d before %s at %d; the monolith must be a topological order of the graph the split path runs",
							tc.path, task.Entry.ID, dep, self, position[self], other, position[other])
					}
				}
			}
		})
	}
}
