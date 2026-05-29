package repocheck

import (
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestBecomeCheckPlaybookValidatesBecome(t *testing.T) {
	var plays []map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, "ansible/playbooks/checks/become.yml")), &plays); err != nil {
		t.Fatalf("decode become check playbook: %v", err)
	}
	if len(plays) != 1 {
		t.Fatalf("become check playbook has %d plays, want 1", len(plays))
	}
	play := plays[0]
	if got, want := play["hosts"], "{{ bootwright_become_check_hosts | default('bootwright_provider_hosts:bootwright_infra_hosts:bootwright_boot_hosts') }}"; got != want {
		t.Fatalf("become check hosts = %v, want %q", got, want)
	}
	if got := play["gather_facts"]; got != false {
		t.Fatalf("become check must not gather facts before validating sudo, got %v", got)
	}
	if got := play["become"]; got != true {
		t.Fatalf("become check must run with become=true, got %v", got)
	}
	tasks, ok := play["tasks"].([]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("become check tasks = %#v, want one task", play["tasks"])
	}
	task, ok := tasks[0].(map[string]any)
	if !ok {
		t.Fatalf("become check task has unexpected type: %#v", tasks[0])
	}
	if got := task["changed_when"]; got != false {
		t.Fatalf("become check task must be read-only, changed_when=%v", got)
	}
}

func TestRootTargetPlaybooksRunBecomeCheckFirst(t *testing.T) {
	tests := []struct {
		path  string
		hosts string
	}{
		{"ansible/playbooks/targets/clusters/apply.yml", "bootwright_ocp_hosts:bootwright_boot_hosts"},
		{"ansible/playbooks/targets/infra/apply.yml", "bootwright_provider_hosts:bootwright_infra_hosts"},
		{"ansible/playbooks/targets/infra/destroy.yml", "bootwright_provider_hosts:bootwright_infra_hosts"},
		{"ansible/playbooks/targets/infra/destroy-artifact-server.yml", "bootwright_provider_hosts"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			var imports []map[string]any
			if err := yaml.Unmarshal([]byte(readRepoFile(t, tc.path)), &imports); err != nil {
				t.Fatalf("decode %s: %v", tc.path, err)
			}
			if len(imports) == 0 {
				t.Fatalf("%s has no playbook imports", tc.path)
			}
			first := imports[0]
			if got := first["import_playbook"]; got != "../../checks/become.yml" {
				t.Fatalf("%s first import = %v, want become check", tc.path, got)
			}
			vars, ok := first["vars"].(map[string]any)
			if !ok {
				t.Fatalf("%s become check import has no vars: %#v", tc.path, first["vars"])
			}
			if got := vars["bootwright_become_check_hosts"]; got != tc.hosts {
				t.Fatalf("%s become check hosts = %v, want %q", tc.path, got, tc.hosts)
			}
		})
	}
}
