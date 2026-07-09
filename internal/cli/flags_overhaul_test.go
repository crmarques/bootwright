package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/workspace"
)

func TestGlobalContextFlagSelectsContext(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))
	t.Setenv("HOME", t.TempDir())

	if _, stderr, code := runCLI(t, "context", "init", "--name", "lab", "-f", fixturePath("001-sno-libvirt")); code != 0 {
		t.Fatalf("context init lab exited %d, stderr=%q", code, stderr)
	}
	if _, stderr, code := runCLI(t, "context", "init", "--name", "other", "-f", fixturePath("001-sno-libvirt")); code != 0 {
		t.Fatalf("context init other exited %d, stderr=%q", code, stderr)
	}

	stdout, stderr, code := runCLI(t, "cluster", "list", "--output", "json", "--context", "lab")
	if code != 0 {
		t.Fatalf("cluster list --context lab exited %d, stderr=%q", code, stderr)
	}
	if got := decodeReportContext(t, stdout); got != "lab" {
		t.Fatalf("--context lab targeted %q, want lab:\n%s", got, stdout)
	}

	stdout, stderr, code = runCLI(t, "cluster", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("cluster list exited %d, stderr=%q", code, stderr)
	}
	if got := decodeReportContext(t, stdout); got != "other" {
		t.Fatalf("default targeted %q, want current context other:\n%s", got, stdout)
	}

	if _, _, code := runCLI(t, "cluster", "list", "--output", "json", "--context", "nope"); code == 0 {
		t.Fatal("--context nope unexpectedly succeeded")
	}
}

func decodeReportContext(t *testing.T, stdout string) string {
	t.Helper()
	var report struct {
		Context string `json:"context"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode report context: %v\n%s", err, stdout)
	}
	return report.Context
}

func TestRenamedFlagsRejectOldSpellings(t *testing.T) {
	cases := []struct {
		name string
		args []string
		flag string
	}{
		{"cluster kubeconfig --cluster", []string{"cluster", "kubeconfig", "--cluster", "managed-01"}, "--cluster"},
		{"cluster info --cluster", []string{"cluster", "info", "--cluster", "managed-01"}, "--cluster"},
		{"machine trust --hosts", []string{"machine", "trust", "--hosts", "provider-01"}, "--hosts"},
		{"example init --output", []string{"example", "init", "--name", "lab", "--output", t.TempDir()}, "--output"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, tc.args...)
			if code == 0 {
				t.Fatalf("%s unexpectedly succeeded:\n%s", tc.name, stdout)
			}
			if !strings.Contains(stdout+stderr, "unknown flag: "+tc.flag) {
				t.Fatalf("%s did not reject removed flag, stdout=%q stderr=%q", tc.name, stdout, stderr)
			}
		})
	}
}

func TestStripLeadingGlobalFlags(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"--context", "lab", "apply"}, []string{"apply"}},
		{[]string{"--context=lab", "destroy", "--stage", "infra"}, []string{"destroy", "--stage", "infra"}},
		{[]string{"apply", "--context", "lab"}, []string{"apply", "--context", "lab"}},
		{[]string{"--context"}, nil},
		{nil, nil},
	}
	for _, tc := range cases {
		got := stripLeadingGlobalFlags(tc.in)
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Fatalf("stripLeadingGlobalFlags(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}

	if argsNeedLocalRoot(stripLeadingGlobalFlags([]string{"--context", "lab", "render"})) {
		t.Fatal("`--context lab render` should stay rootless (bare render)")
	}
	if !argsNeedLocalRoot(stripLeadingGlobalFlags([]string{"--context", "lab", "apply"})) {
		t.Fatal("`--context lab apply` should be rootful")
	}
}
