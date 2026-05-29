package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/state/desired"
)

func TestExampleInitWritesValidWorkspace(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "my-sno-lab")
	stdout, stderr, code := runCLI(t, "example", "init", "my-sno-lab", "--output", outputDir)
	if code != 0 {
		t.Fatalf("example init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, name := range []string{
		"environment.yaml",
		"shared/hosts.yaml",
		"shared/provider.yaml",
		"shared/infra-component.yaml",
		"shared/networks.yaml",
		"clusters/my-sno-lab/cluster-infra.yaml",
		"clusters/my-sno-lab/container-cluster.yaml",
	} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("example init did not write %s: %v", name, err)
		}
	}
	if _, err := desiredstate.LoadNormalizeValidate([]string{outputDir}); err != nil {
		t.Fatalf("generated workspace is not valid: %v", err)
	}
	if !strings.Contains(stdout, "apply support: supported") {
		t.Fatalf("stdout missing apply support:\n%s", stdout)
	}
}

func TestExampleInitDoesNotRequireContextRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".bootwright"), 0o700); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(home, ".bootwright", "contexts.yaml")
	if err := os.WriteFile(registry, []byte("current: lab\ncontexts:\n  lab:\n    baseDir: /tmp/lab\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := localRootGate
	localRootGate.enabled = true
	localRootGate.geteuid = func() int { return 1000 }
	t.Cleanup(func() { localRootGate = previous })

	outputDir := filepath.Join(t.TempDir(), "my-sno-lab")
	stdout, stderr, code := runCLI(t, "example", "init", "my-sno-lab", "--output", outputDir)
	if code != 0 {
		t.Fatalf("example init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := desiredstate.LoadNormalizeValidate([]string{outputDir}); err != nil {
		t.Fatalf("generated workspace is not valid: %v", err)
	}
}

func TestExampleInitRejectsNonEmptyOutputWithoutYes(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, "note.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "example", "init", "my-sno-lab", "--output", outputDir)
	if code == 0 {
		t.Fatal("example init unexpectedly wrote into a non-empty directory")
	}
	if !strings.Contains(stderr, "not empty") {
		t.Fatalf("stderr does not describe non-empty output: %q", stderr)
	}
}

func TestRootHelpShowsFirstRunWorkflow(t *testing.T) {
	stdout, stderr, code := runCLI(t, "--help")
	if code != 0 {
		t.Fatalf("bootwright --help exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"bootwright example init lab",
		"bootwright check syntax -f ./lab-input",
		"bootwright context init lab",
		"bootwright context validate",
		"bootwright check bastion",
		"bootwright apply bastion --yes",
		"bootwright check infra",
		"bootwright apply infra --dry-run",
		"bootwright check cluster",
		"bootwright apply cluster --dry-run",
		"bootwright status --watch",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("root help missing %q:\n%s", want, stdout)
		}
	}
}
