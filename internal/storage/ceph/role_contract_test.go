package ceph

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

// A valid frozen request must reach the shipped adapter instead of failing a
// stale version assertion before its native admission is evaluated.
func TestCurrentRequestPassesShippedRoleVersionAdmission(t *testing.T) {
	root := filepath.Join("..", "..", "..", "ansible", "collections", "ansible_collections", "bootwright", "core", "roles", "storage_ceph_core")
	read := func(t *testing.T, name string, value any) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("cannot read shipped role contract %s: %v", name, err)
		}
		if err := yaml.Unmarshal(data, value); err != nil {
			t.Fatalf("cannot read shipped role contract %s: %v", name, err)
		}
	}
	var metadata struct {
		Specs map[string]struct {
			Options map[string]struct {
				Options map[string]struct {
					Choices []string `yaml:"choices"`
				} `yaml:"options"`
			} `yaml:"options"`
		} `yaml:"argument_specs"`
	}
	read(t, filepath.Join("meta", "argument_specs.yml"), &metadata)
	for _, phase := range []string{"apply", "observe", "destroy"} {
		t.Run(phase, func(t *testing.T) {
			choices := metadata.Specs[phase].Options["bootwright_ceph_request"].Options["version"].Choices
			if !slices.Equal(choices, []string{Version}) {
				t.Fatalf("role %s accepts %v instead of current request %s", phase, choices, Version)
			}
			var tasks []struct {
				Assert struct {
					That []string `yaml:"that"`
				} `yaml:"ansible.builtin.assert"`
			}
			read(t, filepath.Join("tasks", phase+".yml"), &tasks)
			if len(tasks) == 0 || !slices.Contains(tasks[0].Assert.That, "bootwright_ceph_request.version == '"+Version+"'") {
				t.Fatalf("role %s does not admit the current frozen version before its handoff", phase)
			}
		})
	}
}
