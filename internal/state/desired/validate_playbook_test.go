package desiredstate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func provisioningState(playbooks ...v1alpha1.Playbook) v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
		Machines:          []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "m1"}}},
		Playbooks:         playbooks,
	}
}

func basePlaybook(name string) v1alpha1.Playbook {
	return v1alpha1.Playbook{
		Metadata:   v1alpha1.Metadata{Name: name},
		SourcePath: "input/playbooks/" + name + ".yaml",
		Spec: v1alpha1.PlaybookSpec{
			Follows:  v1alpha1.PlaybookAnchorBase,
			Playbook: "playbooks/" + name + ".yml",
			Target:   v1alpha1.PlaybookTarget{Clusters: []string{"demo"}},
		},
	}
}

func TestValidatePlaybook(t *testing.T) {
	cases := []struct {
		name string
		mut  func(p *v1alpha1.Playbook)
		want string
	}{
		{"bad-anchor", func(p *v1alpha1.Playbook) { p.Spec.Follows = "boot" }, "follows \"boot\" must be one of"},
		{"both-anchors", func(p *v1alpha1.Playbook) { p.Spec.Gates = v1alpha1.PlaybookAnchorDeps }, "exactly one of gates or follows"},
		{"no-anchor", func(p *v1alpha1.Playbook) { p.Spec.Follows = "" }, "must set one of gates or follows"},
		{"gates-with-continue", func(p *v1alpha1.Playbook) {
			p.Spec.Follows = ""
			p.Spec.Gates = v1alpha1.PlaybookAnchorDeps
			p.Spec.OnFailure = v1alpha1.PlaybookFailureContinue
		}, "gates cannot be combined with onFailure continue"},
		{"bad-run", func(p *v1alpha1.Playbook) { p.Spec.Run = "sometimes" }, "run \"sometimes\" must be"},
		{"bad-failure", func(p *v1alpha1.Playbook) { p.Spec.OnFailure = "retry" }, "onFailure \"retry\" must be"},
		{"empty-target", func(p *v1alpha1.Playbook) { p.Spec.Target = v1alpha1.PlaybookTarget{} }, "target must select at least one"},
		{"unknown-cluster", func(p *v1alpha1.Playbook) {
			p.Spec.Target = v1alpha1.PlaybookTarget{Clusters: []string{"ghost"}}
		}, "does not match any ContainerCluster or StorageCluster"},
		{"unknown-machine", func(p *v1alpha1.Playbook) {
			p.Spec.Target = v1alpha1.PlaybookTarget{Machines: []string{"ghost"}}
		}, "does not match any Machine"},
		{"localhost-ban", func(p *v1alpha1.Playbook) {
			p.Spec.Target = v1alpha1.PlaybookTarget{HostGroups: []string{"bootwright_ocp_hosts"}}
		}, "targets the bootwright controller/localhost"},
		{"absolute-playbook", func(p *v1alpha1.Playbook) { p.Spec.Playbook = "/etc/passwd" }, "must be a relative path"},
		{"escaping-playbook", func(p *v1alpha1.Playbook) { p.Spec.Playbook = "../evil.yml" }, "must stay within"},
		{"vendor-dir", func(p *v1alpha1.Playbook) { p.Spec.RolesPath = "vendor" }, "must not be named vendor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := basePlaybook("hook")
			tc.mut(&p)
			errs := validatePlaybooks(provisioningState(p))
			if !containsSubstring(errs, tc.want) {
				t.Fatalf("findings %v do not contain %q", errs, tc.want)
			}
		})
	}
}

func TestValidatePlaybookOrdering(t *testing.T) {
	dangling := basePlaybook("needs")
	dangling.Spec.Requires = []string{"prep"}
	if errs := validatePlaybooks(provisioningState(dangling)); !containsSubstring(errs, "is not provided by any playbook at the same anchor") {
		t.Fatalf("dangling requires not reported: %v", errs)
	}

	dupA := basePlaybook("a")
	dupA.Spec.Provides = []string{"x"}
	dupB := basePlaybook("b")
	dupB.Spec.Provides = []string{"x"}
	if errs := validatePlaybooks(provisioningState(dupA, dupB)); !containsSubstring(errs, "is already provided by") {
		t.Fatalf("duplicate provides not reported: %v", errs)
	}

	cycleA := basePlaybook("ca")
	cycleA.Spec.Provides = []string{"p"}
	cycleA.Spec.Requires = []string{"q"}
	cycleB := basePlaybook("cb")
	cycleB.Spec.Provides = []string{"q"}
	cycleB.Spec.Requires = []string{"p"}
	if errs := validatePlaybooks(provisioningState(cycleA, cycleB)); !containsSubstring(errs, "cycle") {
		t.Fatalf("provides/requires cycle not reported: %v", errs)
	}
}

func TestValidatePlaybookValidPassesOnDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "playbooks"), 0o700); err != nil {
		t.Fatalf("mkdir playbooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "playbooks", "harden.yml"), []byte("---\n- hosts: all\n  tasks: []\n"), 0o600); err != nil {
		t.Fatalf("write playbook: %v", err)
	}
	p := basePlaybook("harden")
	p.SourcePath = filepath.Join(dir, "harden.yaml")
	p.Spec.Playbook = "playbooks/harden.yml"
	if errs := validatePlaybooks(provisioningState(p)); len(errs) != 0 {
		t.Fatalf("valid playbook reported errors: %v", errs)
	}
}

func TestValidatePlaybookRequiresPlaybooksDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harden.yml"), []byte("---\n- hosts: all\n  tasks: []\n"), 0o600); err != nil {
		t.Fatalf("write playbook: %v", err)
	}
	p := basePlaybook("harden")
	p.SourcePath = filepath.Join(dir, "harden.yaml")
	p.Spec.Playbook = "harden.yml"
	if errs := validatePlaybooks(provisioningState(p)); !containsSubstring(errs, "playbooks/ directory") {
		t.Fatalf("playbook outside playbooks/ not reported: %v", errs)
	}
}
