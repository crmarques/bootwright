package desiredstate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func provisioningState(playbooks ...v1alpha1.ProvisioningPlaybook) v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters:     []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
		Machines:              []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "m1"}}},
		ProvisioningPlaybooks: playbooks,
	}
}

func basePlaybook(name string) v1alpha1.ProvisioningPlaybook {
	return v1alpha1.ProvisioningPlaybook{
		Metadata:   v1alpha1.Metadata{Name: name},
		SourcePath: "input/hooks/" + name + ".yaml",
		Spec: v1alpha1.ProvisioningPlaybookSpec{
			Stage:    v1alpha1.ProvisioningStageBase,
			Timing:   v1alpha1.ProvisioningPlaybookTimingAfter,
			Playbook: "playbooks/" + name + ".yml",
			Target:   v1alpha1.ProvisioningPlaybookTarget{Clusters: []string{"demo"}},
		},
	}
}

func TestValidateProvisioningPlaybook(t *testing.T) {
	cases := []struct {
		name string
		mut  func(p *v1alpha1.ProvisioningPlaybook)
		want string
	}{
		{"bad-stage", func(p *v1alpha1.ProvisioningPlaybook) { p.Spec.Stage = "boot" }, "stage \"boot\" must be one of"},
		{"bad-timing", func(p *v1alpha1.ProvisioningPlaybook) { p.Spec.Timing = "during" }, "timing \"during\" must be"},
		{"bad-run", func(p *v1alpha1.ProvisioningPlaybook) { p.Spec.Run = "sometimes" }, "run \"sometimes\" must be"},
		{"bad-failure", func(p *v1alpha1.ProvisioningPlaybook) { p.Spec.FailureMode = "retry" }, "failureMode \"retry\" must be"},
		{"empty-target", func(p *v1alpha1.ProvisioningPlaybook) { p.Spec.Target = v1alpha1.ProvisioningPlaybookTarget{} }, "target must select at least one"},
		{"unknown-cluster", func(p *v1alpha1.ProvisioningPlaybook) {
			p.Spec.Target = v1alpha1.ProvisioningPlaybookTarget{Clusters: []string{"ghost"}}
		}, "does not match any ContainerCluster or StorageCluster"},
		{"unknown-machine", func(p *v1alpha1.ProvisioningPlaybook) {
			p.Spec.Target = v1alpha1.ProvisioningPlaybookTarget{Machines: []string{"ghost"}}
		}, "does not match any Machine"},
		{"localhost-ban", func(p *v1alpha1.ProvisioningPlaybook) {
			p.Spec.Target = v1alpha1.ProvisioningPlaybookTarget{HostGroups: []string{"bootwright_ocp_hosts"}}
		}, "targets the bootwright controller/localhost"},
		{"absolute-playbook", func(p *v1alpha1.ProvisioningPlaybook) { p.Spec.Playbook = "/etc/passwd" }, "must be a relative path"},
		{"escaping-playbook", func(p *v1alpha1.ProvisioningPlaybook) { p.Spec.Playbook = "../evil.yml" }, "must stay within"},
		{"vendor-dir", func(p *v1alpha1.ProvisioningPlaybook) { p.Spec.RolesPath = "vendor" }, "must not be named vendor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := basePlaybook("hook")
			tc.mut(&p)
			errs := validateProvisioningPlaybooks(provisioningState(p))
			if !containsSubstring(errs, tc.want) {
				t.Fatalf("findings %v do not contain %q", errs, tc.want)
			}
		})
	}
}

func TestValidateProvisioningPlaybookOrdering(t *testing.T) {
	dangling := basePlaybook("needs")
	dangling.Spec.Requires = []string{"prep"}
	if errs := validateProvisioningPlaybooks(provisioningState(dangling)); !containsSubstring(errs, "is not provided by any playbook in the same stage/timing") {
		t.Fatalf("dangling requires not reported: %v", errs)
	}

	dupA := basePlaybook("a")
	dupA.Spec.Provides = []string{"x"}
	dupB := basePlaybook("b")
	dupB.Spec.Provides = []string{"x"}
	if errs := validateProvisioningPlaybooks(provisioningState(dupA, dupB)); !containsSubstring(errs, "is already provided by") {
		t.Fatalf("duplicate provides not reported: %v", errs)
	}

	cycleA := basePlaybook("ca")
	cycleA.Spec.Provides = []string{"p"}
	cycleA.Spec.Requires = []string{"q"}
	cycleB := basePlaybook("cb")
	cycleB.Spec.Provides = []string{"q"}
	cycleB.Spec.Requires = []string{"p"}
	if errs := validateProvisioningPlaybooks(provisioningState(cycleA, cycleB)); !containsSubstring(errs, "cycle") {
		t.Fatalf("provides/requires cycle not reported: %v", errs)
	}
}

func TestValidateProvisioningPlaybookValidPassesOnDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harden.yml"), []byte("---\n- hosts: all\n  tasks: []\n"), 0o600); err != nil {
		t.Fatalf("write playbook: %v", err)
	}
	p := basePlaybook("harden")
	p.SourcePath = filepath.Join(dir, "harden.yaml")
	p.Spec.Playbook = "harden.yml"
	if errs := validateProvisioningPlaybooks(provisioningState(p)); len(errs) != 0 {
		t.Fatalf("valid playbook reported errors: %v", errs)
	}
}
