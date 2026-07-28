package desiredstate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func provisioningState(playbooks ...v1alpha1.CustomPlaybook) v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
		Machines:          []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "m1"}}},
		CustomPlaybooks:   playbooks,
	}
}

func basePlaybook(name string) v1alpha1.CustomPlaybook {
	return v1alpha1.CustomPlaybook{
		Metadata:   v1alpha1.Metadata{Name: name},
		SourcePath: "input/playbooks/" + name + ".yaml",
		Spec: v1alpha1.CustomPlaybookSpec{
			Follows:  v1alpha1.CustomPlaybookAnchorBase,
			Playbook: "playbooks/" + name + ".yml",
			Target:   v1alpha1.CustomPlaybookTarget{Clusters: []string{"demo"}},
		},
	}
}

func TestValidatePlaybook(t *testing.T) {
	cases := []struct {
		name string
		mut  func(p *v1alpha1.CustomPlaybook)
		want string
	}{
		{"bad-anchor", func(p *v1alpha1.CustomPlaybook) { p.Spec.Follows = "boot" }, "follows \"boot\" must be one of"},
		{"both-anchors", func(p *v1alpha1.CustomPlaybook) { p.Spec.Gates = v1alpha1.CustomPlaybookAnchorDeps }, "exactly one of gates or follows"},
		{"no-anchor", func(p *v1alpha1.CustomPlaybook) { p.Spec.Follows = "" }, "must set one of gates or follows"},
		{"gates-with-continue", func(p *v1alpha1.CustomPlaybook) {
			p.Spec.Follows = ""
			p.Spec.Gates = v1alpha1.CustomPlaybookAnchorDeps
			p.Spec.OnFailure = v1alpha1.PlaybookFailureContinue
		}, "gates cannot be combined with onFailure continue"},
		{"bad-run", func(p *v1alpha1.CustomPlaybook) { p.Spec.Run = "sometimes" }, "run \"sometimes\" must be"},
		{"bad-failure", func(p *v1alpha1.CustomPlaybook) { p.Spec.OnFailure = "retry" }, "onFailure \"retry\" must be"},
		{"empty-target", func(p *v1alpha1.CustomPlaybook) { p.Spec.Target = v1alpha1.CustomPlaybookTarget{} }, "target must select at least one"},
		{"unknown-cluster", func(p *v1alpha1.CustomPlaybook) {
			p.Spec.Target = v1alpha1.CustomPlaybookTarget{Clusters: []string{"ghost"}}
		}, "does not match any ContainerCluster or StorageCluster"},
		{"unknown-machine", func(p *v1alpha1.CustomPlaybook) {
			p.Spec.Target = v1alpha1.CustomPlaybookTarget{Machines: []string{"ghost"}}
		}, "does not match any Machine"},
		{"localhost-ban", func(p *v1alpha1.CustomPlaybook) {
			p.Spec.Target = v1alpha1.CustomPlaybookTarget{HostGroups: []string{"bootwright_ocp_hosts"}}
		}, "targets the bootwright controller/localhost"},
		{"absolute-playbook", func(p *v1alpha1.CustomPlaybook) { p.Spec.Playbook = "/etc/passwd" }, "must be a relative path"},
		{"escaping-playbook", func(p *v1alpha1.CustomPlaybook) { p.Spec.Playbook = "../evil.yml" }, "must stay within"},
		{"vendor-dir", func(p *v1alpha1.CustomPlaybook) { p.Spec.RolesPath = "vendor" }, "must not be named vendor"},
		{"empty-tag", func(p *v1alpha1.CustomPlaybook) { p.Spec.Tags = []string{"base", ""} }, "tags[1] is empty"},
		{"comma-tag", func(p *v1alpha1.CustomPlaybook) { p.Spec.SkipTags = []string{"bla,ble"} }, "is not a valid Ansible tag"},
		{"padded-tag", func(p *v1alpha1.CustomPlaybook) { p.Spec.Tags = []string{" base"} }, "leading or trailing whitespace"},
		{"repeated-tag", func(p *v1alpha1.CustomPlaybook) { p.Spec.Tags = []string{"base", "base"} }, "tags[1] \"base\" is listed twice"},
		{"contradictory-tag", func(p *v1alpha1.CustomPlaybook) {
			p.Spec.Tags = []string{"base"}
			p.Spec.SkipTags = []string{"base"}
		}, "in both tags and skipTags"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := basePlaybook("hook")
			tc.mut(&p)
			errs := validateCustomPlaybooks(provisioningState(p))
			if !containsSubstring(errs, tc.want) {
				t.Fatalf("findings %v do not contain %q", errs, tc.want)
			}
		})
	}
}

func TestValidatePlaybookOrdering(t *testing.T) {
	dangling := basePlaybook("needs")
	dangling.Spec.Requires = []string{"prep"}
	if errs := validateCustomPlaybooks(provisioningState(dangling)); !containsSubstring(errs, "is not provided by any playbook at the same anchor") {
		t.Fatalf("dangling requires not reported: %v", errs)
	}

	dupA := basePlaybook("a")
	dupA.Spec.Provides = []string{"x"}
	dupB := basePlaybook("b")
	dupB.Spec.Provides = []string{"x"}
	if errs := validateCustomPlaybooks(provisioningState(dupA, dupB)); !containsSubstring(errs, "is already provided by") {
		t.Fatalf("duplicate provides not reported: %v", errs)
	}

	cycleA := basePlaybook("ca")
	cycleA.Spec.Provides = []string{"p"}
	cycleA.Spec.Requires = []string{"q"}
	cycleB := basePlaybook("cb")
	cycleB.Spec.Provides = []string{"q"}
	cycleB.Spec.Requires = []string{"p"}
	if errs := validateCustomPlaybooks(provisioningState(cycleA, cycleB)); !containsSubstring(errs, "cycle") {
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
	if errs := validateCustomPlaybooks(provisioningState(p)); len(errs) != 0 {
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
	if errs := validateCustomPlaybooks(provisioningState(p)); !containsSubstring(errs, "playbooks/ directory") {
		t.Fatalf("playbook outside playbooks/ not reported: %v", errs)
	}
}
