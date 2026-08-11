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
	stdout, stderr, code := runCLI(t, "example", "init", "--name", "my-sno-lab", "--openshift-version", "42.7.3", "--output-dir", outputDir)
	if code != 0 {
		t.Fatalf("example init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, name := range []string{
		"environment.yaml",
		"infra/providers/provider.yaml",
		"infra/machines/bastion.yaml",
		"infra/networkconfigs/networks.yaml",
		"infra/components/load-balancer.yaml",
		"infra/components/name-resolution.yaml",
		"infra/components/ntp-server.yaml",
		"clusters/container/my-sno-lab/cluster.yaml",
		"clusters/container/my-sno-lab/cluster-machines.yaml",
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

func TestExampleInitWritesValidStorageWorkspace(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "my-ceph-lab")
	stdout, stderr, code := runCLI(t, "example", "init", "--name", "my-ceph-lab", "--kind", "storage-cluster", "--ceph-release", "37.4.2", "--output-dir", outputDir)
	if code != 0 {
		t.Fatalf("example init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, name := range []string{
		"environment.yaml",
		"secrets.yaml",
		"clusters/storage/my-ceph-lab/cluster.yaml",
		"clusters/storage/my-ceph-lab/nodes/my-ceph-lab-node-1.yaml",
		"clusters/storage/my-ceph-lab/nodes/my-ceph-lab-node-2.yaml",
		"clusters/storage/my-ceph-lab/nodes/my-ceph-lab-node-3.yaml",
	} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("example init did not write %s: %v", name, err)
		}
	}
	if _, err := desiredstate.LoadNormalizeValidate([]string{outputDir}); err != nil {
		t.Fatalf("generated storage workspace is not valid: %v", err)
	}
	for _, want := range []string{"kind: storage-cluster", "apply support: supported"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "provider:") {
		t.Fatalf("storage scaffold reported a substrate provider:\n%s", stdout)
	}
}

func TestExampleInitStorageWorkspaceUsesBlockStyle(t *testing.T) {
	files, err := storageExampleWorkspace("block-style-check", "37.4.2")
	if err != nil {
		t.Fatalf("storage workspace: %v", err)
	}
	for _, file := range files {
		for _, line := range strings.Split(file.Body, "\n") {
			value := line
			if idx := strings.Index(line, "#"); idx >= 0 {
				value = line[:idx]
			}
			value = strings.TrimSpace(value)
			for _, marker := range []string{": {", ": [", "- {", "- ["} {
				if strings.Contains(value, marker) {
					t.Fatalf("%s uses a flow-style collection: %q", file.Name, line)
				}
			}
		}
	}
}

func TestExampleInitRejectsProviderForStorageKind(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "my-ceph-lab")
	_, stderr, code := runCLI(t, "example", "init", "--name", "my-ceph-lab", "--kind", "storage-cluster", "--provider", "bare-metal", "--ceph-release", "37.4.2", "--output-dir", outputDir)
	if code != 2 {
		t.Fatalf("example init exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "--provider does not apply") {
		t.Fatalf("stderr does not explain the refusal: %q", stderr)
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("refused example init still created %s: %v", outputDir, err)
	}
}

func TestExampleInitRequiresAuthoredProductVersion(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "container",
			args: []string{"example", "init", "--name", "my-ocp-lab", "--output-dir", filepath.Join(t.TempDir(), "my-ocp-lab")},
			want: "--openshift-version is required",
		},
		{
			name: "storage",
			args: []string{"example", "init", "--name", "my-ceph-lab", "--kind", "storage-cluster", "--output-dir", filepath.Join(t.TempDir(), "my-ceph-lab")},
			want: "--ceph-release is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, stderr, code := runCLI(t, test.args...)
			if code != 2 || !strings.Contains(stderr, test.want) {
				t.Fatalf("example init exited %d, stderr=%q, want %q", code, stderr, test.want)
			}
		})
	}
}

func TestExampleInitRejectsProductVersionFlagForOtherKind(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "ceph release on container",
			args: []string{"example", "init", "--name", "my-ocp-lab", "--openshift-version", "42.7.3", "--ceph-release", "37.4.2", "--output-dir", filepath.Join(t.TempDir(), "my-ocp-lab")},
			want: "--ceph-release applies only",
		},
		{
			name: "OpenShift version on storage",
			args: []string{"example", "init", "--name", "my-ceph-lab", "--kind", "storage-cluster", "--openshift-version", "42.7.3", "--ceph-release", "37.4.2", "--output-dir", filepath.Join(t.TempDir(), "my-ceph-lab")},
			want: "--openshift-version applies only",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, stderr, code := runCLI(t, test.args...)
			if code != 2 || !strings.Contains(stderr, test.want) {
				t.Fatalf("example init exited %d, stderr=%q, want %q", code, stderr, test.want)
			}
		})
	}
}

func TestExampleInitRejectsUnknownKind(t *testing.T) {
	_, stderr, code := runCLI(t, "example", "init", "--name", "my-lab", "--kind", "database-cluster", "--output-dir", filepath.Join(t.TempDir(), "my-lab"))
	if code != 2 {
		t.Fatalf("example init exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "unknown kind") {
		t.Fatalf("stderr does not name the unknown kind: %q", stderr)
	}
}

func TestExampleInitStorageRejectsNonEmptyOutputWithoutYes(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, "note.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "example", "init", "--name", "my-ceph-lab", "--kind", "storage-cluster", "--ceph-release", "37.4.2", "--output-dir", outputDir)
	if code == 0 {
		t.Fatal("example init unexpectedly wrote into a non-empty directory")
	}
	if !strings.Contains(stderr, "not empty") {
		t.Fatalf("stderr does not describe non-empty output: %q", stderr)
	}
	stdout, stderr, code := runCLI(t, "example", "init", "--name", "my-ceph-lab", "--kind", "storage-cluster", "--ceph-release", "37.4.2", "--output-dir", outputDir, "--yes")
	if code != 0 {
		t.Fatalf("example init --yes exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := desiredstate.LoadNormalizeValidate([]string{outputDir}); err != nil {
		t.Fatalf("overwritten storage workspace is not valid: %v", err)
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
	stdout, stderr, code := runCLI(t, "example", "init", "--name", "my-sno-lab", "--openshift-version", "42.7.3", "--output-dir", outputDir)
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
	_, stderr, code := runCLI(t, "example", "init", "--name", "my-sno-lab", "--openshift-version", "42.7.3", "--output-dir", outputDir)
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
		"bootwright example init --name lab --openshift-version <version>",
		"bootwright validate -f ./lab-input",
		"bootwright context init --name lab",
		"bootwright bastion setup --yes",
		"bootwright preflight all",
		"bootwright render effective",
		"bootwright plan",
		"bootwright apply --yes",
		"bootwright status --watch",
		"bootwright cluster info",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("root help missing %q:\n%s", want, stdout)
		}
	}
}
