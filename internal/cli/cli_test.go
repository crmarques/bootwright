package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/contextstore"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/operator"
	"github.com/crmarques/bootwright/internal/provisioning/render"
	"github.com/crmarques/bootwright/internal/scaffold"
	"github.com/crmarques/bootwright/internal/workflow"
)

func TestMain(m *testing.M) {
	localRootGate.enabled = false
	os.Exit(m.Run())
}

func TestHubCommandsNotAdvertised(t *testing.T) {
	for _, args := range [][]string{
		{"check", "--help"},
		{"apply", "--help"},
	} {
		stdout, stderr, code := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("bootwright %s exited %d, stderr=%q", strings.Join(args, " "), code, stderr)
		}
		if strings.Contains(stdout, "hub") {
			t.Fatalf("help for %s still advertises hub:\n%s", strings.Join(args, " "), stdout)
		}
	}

	_, stderr, code := runCLI(t, "check", "hub")
	if code == 0 {
		t.Fatal("bootwright check hub unexpectedly succeeded")
	}
	if !strings.Contains(stderr, `invalid argument "hub"`) {
		t.Fatalf("check hub stderr %q does not reject hub as an invalid target", stderr)
	}

	_, stderr, code = runCLI(t, "apply", "hub")
	if code == 0 {
		t.Fatal("bootwright apply hub unexpectedly succeeded")
	}
	if !strings.Contains(stderr, `invalid argument "hub"`) {
		t.Fatalf("apply hub stderr %q does not reject hub as an invalid target", stderr)
	}
}

func TestClusterTargetIsSingular(t *testing.T) {
	for _, args := range [][]string{
		{"check", "cluster", "--help"},
		{"apply", "cluster", "--help"},
		{"destroy", "cluster", "--help"},
	} {
		_, stderr, code := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("bootwright %s exited %d, stderr=%q", strings.Join(args, " "), code, stderr)
		}
	}

	for _, args := range [][]string{
		{"check", "clusters"},
		{"apply", "clusters"},
		{"destroy", "clusters"},
	} {
		_, stderr, code := runCLI(t, args...)
		if code == 0 {
			t.Fatalf("bootwright %s unexpectedly succeeded", strings.Join(args, " "))
		}
		if !strings.Contains(stderr, `invalid argument "clusters"`) {
			t.Fatalf("%s stderr %q does not reject plural target", strings.Join(args, " "), stderr)
		}
	}
}

func TestCheckSyntaxJSON(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "check", "syntax", "--output", "json")
	if code != 0 {
		t.Fatalf("check syntax exited %d, stderr=%q", code, stderr)
	}
	var report syntaxCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if !report.OK {
		t.Fatalf("expected ok report, got %+v", report)
	}
	if report.ContainerClusters != 1 || report.ClusterInfras != 1 || report.InfraProviders != 1 {
		t.Fatalf("unexpected object counts: %+v", report)
	}
	for _, reject := range []string{"Bootwright:", "[OK]", "Summary"} {
		if strings.Contains(stdout, reject) {
			t.Fatalf("json output contains human decoration %q:\n%s", reject, stdout)
		}
	}
}

func TestHumanOutputStructuredText(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "check syntax",
			args: []string{"check", "syntax"},
			want: []string{"Bootwright: syntax check", "Objects", "Desired state", "[OK] syntax check"},
		},
		{
			name: "status",
			args: []string{"status"},
			want: []string{"Bootwright: status", "Context", "Desired state", "Clusters", "Next steps"},
		},
		{
			name: "render installer",
			args: []string{"render", "installer", "--scope", "sno-libvirt"},
			want: []string{"Bootwright: installer render", "Rendered artifacts", "Installer placeholders", "install-config.yaml", "agent-config.yaml"},
		},
		{
			name: "apply infra dry-run",
			args: []string{"apply", "infra", "--dry-run", "--ask-become-pass=false"},
			want: []string{"Bootwright: infra apply", "Apply plan", "Bootwright prerequisites", "planned task(s)", "Provider services", "Rendered artifacts", "Bundle"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			initTestContext(t, "001-sno-libvirt")
			stdout, stderr, code := runCLI(t, tc.args...)
			if code != 0 {
				t.Fatalf("%s exited %d, stderr=%q\nstdout:\n%s", strings.Join(tc.args, " "), code, stderr, stdout)
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Fatalf("stdout missing %q:\n%s", want, stdout)
				}
			}
			for _, reject := range []string{"info:", ">>>", "<<<"} {
				if strings.Contains(stdout, reject) {
					t.Fatalf("stdout contains old output marker %q:\n%s", reject, stdout)
				}
			}
		})
	}
}

func TestFailedCheckOutputIsActionable(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "check", "infra", "--dry-run")
	if code == 0 {
		t.Fatalf("check infra should fail with missing local secrets in test context:\n%s", stdout)
	}
	for _, want := range []string{"Secret material", "[FAIL]", "impact:", "fix:"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	for _, heading := range []string{"Prepare", "Bastion tools", "Secret material", "Summary"} {
		want := "\n\n" + heading + "\n"
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout does not separate %q with an empty line:\n%s", heading, stdout)
		}
	}
	if !strings.Contains(stderr, "host check failed") {
		t.Fatalf("stderr missing host check failure:\n%s", stderr)
	}
}

func TestScopedApplyDryRunJSON(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"apply", "infra",
		"--scope", "sno-libvirt",
		"--dry-run",
		"--output", "json",
	)
	if code != 0 {
		t.Fatalf("apply infra dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Target != "infra" || report.Action != "apply" || !report.DryRun {
		t.Fatalf("unexpected dry-run report header: %+v", report)
	}
	if len(report.Command) == 0 {
		t.Fatalf("dry-run report did not include planned command: %+v", report)
	}
	if !strings.HasPrefix(report.Render.EffectiveStatePath, ctx.StateDir) {
		t.Fatalf("effective state path %q is outside state dir %q", report.Render.EffectiveStatePath, ctx.StateDir)
	}
}

func TestDestroyInfraHTTPServerScopeDryRunJSON(t *testing.T) {
	initTestContext(t, "002-sno-emul-baremetal")
	stdout, stderr, code := runCLI(t,
		"destroy", "infra",
		"--scope", "http-server",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy infra http-server dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Target != "infra" || report.Action != "destroy" || !report.DryRun {
		t.Fatalf("unexpected dry-run report header: %+v", report)
	}
	if !reflect.DeepEqual(report.Phases, []string{"http-server"}) {
		t.Fatalf("phases = %#v, want http-server", report.Phases)
	}
	if report.Playbook != infraDestroyHTTPServerPlaybook {
		t.Fatalf("playbook = %q, want %q", report.Playbook, infraDestroyHTTPServerPlaybook)
	}
	if report.Limit != render.GroupProviderHosts {
		t.Fatalf("limit = %q, want %q", report.Limit, render.GroupProviderHosts)
	}
	if !slices.Contains(report.ExtraVars, providerServiceScopeExtraVarName+"=http-server") {
		t.Fatalf("extra vars missing http-server scope: %#v", report.ExtraVars)
	}
}

func TestScopedCheckDryRunJSONDoesNotPromptForBecome(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"check", "infra",
		"--dry-run",
		"--output", "json",
	)
	if code != 0 {
		t.Fatalf("check infra dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if commandContains(report.Command, "--ask-become-pass") {
		t.Fatalf("check infra preflight should not ask for become password, got %v", report.Command)
	}
}

func TestScopedApplyDryRunJSONIncludesBecomePromptForProviderHosts(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"apply", "infra",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=true",
	)
	if code != 0 {
		t.Fatalf("apply infra dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if !commandContains(report.Command, "--ask-become-pass") {
		t.Fatalf("expected apply infra command to ask for become password, got %v", report.Command)
	}
}

func TestRenderInstallerScopedFixtureJSON(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"render", "installer",
		"--scope", "sno-libvirt",
		"--output", "json",
	)
	if code != 0 {
		t.Fatalf("render installer exited %d, stderr=%q", code, stderr)
	}
	var report renderInstallerReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if len(report.Clusters) != 1 {
		t.Fatalf("expected one rendered cluster, got %+v", report.Clusters)
	}
	cluster := report.Clusters[0]
	if cluster.Name != "sno-libvirt" {
		t.Fatalf("rendered cluster name = %q, want sno-libvirt", cluster.Name)
	}
	if !strings.HasPrefix(cluster.InstallConfigPath, filepath.Join(ctx.StateDir, render.InstallerRelativeDir)) {
		t.Fatalf("install config path %q is outside installer state dir %q", cluster.InstallConfigPath, ctx.StateDir)
	}
}

func TestApplyRejectsSchemaOnlyDispatchBeforeAnsible(t *testing.T) {
	dir := t.TempDir()
	files, err := scaffold.Workspace("kubevirt-lab", scaffold.ProviderKubeVirt)
	if err != nil {
		t.Fatalf("scaffold kubevirt workspace: %v", err)
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, file.Name), []byte(file.Body), 0o600); err != nil {
			t.Fatalf("write %s: %v", file.Name, err)
		}
	}

	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "kubevirt-lab", "-f", dir)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, stderr, code = runCLI(t, "apply", "infra", "--dry-run")
	if code == 0 {
		t.Fatal("apply infra unexpectedly accepted a schema-only KubeVirt dispatch")
	}
	if !strings.Contains(stderr, "unsupported apply dispatch substrate=kubevirt bmc=none boot=kubevirt") {
		t.Fatalf("stderr does not describe unsupported dispatch: %q", stderr)
	}
}

func TestContextInitPreparesAnsibleBundle(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "internal", "embedded", "bundle", "ansible.cfg")); err != nil {
		t.Skip("embedded bundle has not been synced")
	}
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "test", "-f", fixturePath("001-sno-libvirt"))
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := contextstore.NewContext("test")
	if err != nil {
		t.Fatal(err)
	}
	bundleDir := filepath.Join(ctx.StateDir, ansibleBundleDirName)
	for _, rel := range []string{
		"ansible.cfg",
		"playbooks/checks/preflight.yml",
		".bootwright-bundle.version",
		".bootwright-bundle.sha256",
	} {
		if _, err := os.Stat(filepath.Join(bundleDir, rel)); err != nil {
			t.Fatalf("context init did not prepare %s: %v\nstdout:\n%s", rel, err, stdout)
		}
	}
	if !strings.Contains(stdout, "Ansible bundle") {
		t.Fatalf("stdout missing bundle preparation:\n%s", stdout)
	}
}

func TestContextInitHelpUsesYesForReplacement(t *testing.T) {
	stdout, stderr, code := runCLI(t, "context", "init", "--help")
	if code != 0 {
		t.Fatalf("context init --help exited %d, stderr=%q", code, stderr)
	}
	oldFlag := "--" + "force"
	if !strings.Contains(stdout, "--yes") {
		t.Fatalf("context init help missing --yes:\n%s", stdout)
	}
	if strings.Contains(stdout, oldFlag) {
		t.Fatalf("context init help still advertises %s:\n%s", oldFlag, stdout)
	}
}

func TestContextInitRejectsOldConsentFlag(t *testing.T) {
	setTestHomeAndRoot(t)
	oldFlag := "--" + "force"
	_, stderr, code := runCLI(t, "context", "init", "test", "-f", fixturePath("001-sno-libvirt"), oldFlag)
	if code == 0 {
		t.Fatalf("context init %s unexpectedly succeeded", oldFlag)
	}
	if !strings.Contains(stderr, "unknown flag: "+oldFlag) {
		t.Fatalf("stderr does not reject %s: %q", oldFlag, stderr)
	}

	_, stderr, code = runCLI(t, "context", "init", "test", "-f", fixturePath("001-sno-libvirt"), "--base-dir", t.TempDir())
	if code == 0 {
		t.Fatal("context init --base-dir unexpectedly succeeded")
	}
	if !strings.Contains(stderr, "unknown flag: --base-dir") {
		t.Fatalf("stderr does not reject --base-dir: %q", stderr)
	}
}

func TestContextInitRequiresYesForExistingContext(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, stderr, code = runCLI(t, "context", "init", "test", "-f", source)
	if code == 0 {
		t.Fatal("second context init without --yes unexpectedly succeeded")
	}
	if !strings.Contains(stderr, `context "test" already exists; rerun with --yes to replace it`) {
		t.Fatalf("stderr missing --yes remediation: %q", stderr)
	}
	oldFlag := "--" + "force"
	if strings.Contains(stderr, oldFlag) {
		t.Fatalf("stderr still mentions %s: %q", oldFlag, stderr)
	}
}

func TestContextInitYesReplacesImportedInputs(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := contextstore.NewContext("test")
	if err != nil {
		t.Fatal(err)
	}
	staleSecret := filepath.Join(ctx.SecretsDir, "stale-secret")
	if err := os.WriteFile(staleSecret, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceEnvironment := filepath.Join(source, "environment.yaml")
	body, err := os.ReadFile(sourceEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "# replacement marker\n"
	if err := os.WriteFile(sourceEnvironment, append(body, []byte(marker)...), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, "context", "init", "test", "-f", source, "--yes")
	if code != 0 {
		t.Fatalf("context init --yes exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	imported, err := os.ReadFile(filepath.Join(ctx.InputDir, "environment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(imported), marker) {
		t.Fatalf("imported environment.yaml was not replaced:\n%s", imported)
	}
	if _, err := os.Stat(staleSecret); !os.IsNotExist(err) {
		t.Fatalf("context init --yes did not replace whole context directory: %v", err)
	}
}

func TestContextInitYesPreservesImportedInputsWhenReplacementInvalid(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := contextstore.NewContext("test")
	if err != nil {
		t.Fatal(err)
	}
	importedPath := filepath.Join(ctx.InputDir, "environment.yaml")
	before, err := os.ReadFile(importedPath)
	if err != nil {
		t.Fatal(err)
	}

	replacement := copyFixtureYAML(t, "001-sno-libvirt")
	replaceInFile(t, filepath.Join(replacement, "environment.yaml"), "  secrets:\n", "  retiredField: true\n\n  secrets:\n")
	stdout, stderr, code = runCLI(t, "context", "init", "test", "-f", replacement, "--yes")
	if code == 0 {
		t.Fatalf("context init --yes unexpectedly accepted invalid replacement:\n%s", stdout)
	}
	if !strings.Contains(stderr, "field retiredField not found") {
		t.Fatalf("stderr missing strict decode error: %q", stderr)
	}
	after, err := os.ReadFile(importedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("invalid replacement changed existing environment.yaml\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestContextInitYesPreservesImportedInputsWhenUnselectedReplacementInvalid(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := contextstore.NewContext("test")
	if err != nil {
		t.Fatal(err)
	}
	importedPath := filepath.Join(ctx.InputDir, "environment.yaml")
	before, err := os.ReadFile(importedPath)
	if err != nil {
		t.Fatal(err)
	}

	replacement := copyFixtureYAML(t, "001-sno-libvirt")
	addFixtureResourceSelection(t, replacement)
	if err := os.WriteFile(filepath.Join(replacement, "unselected.yaml"), []byte(`apiVersion: bootwright.io/v1alpha1
kind: Host
metadata:
  name: spare-host
spec:
  retiredField: true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, "context", "init", "test", "-f", replacement, "--yes")
	if code == 0 {
		t.Fatalf("context init --yes unexpectedly accepted invalid unselected replacement:\n%s", stdout)
	}
	if !strings.Contains(stderr, "field retiredField not found") {
		t.Fatalf("stderr missing strict decode error: %q", stderr)
	}
	after, err := os.ReadFile(importedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("invalid replacement changed existing environment.yaml\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(ctx.InputDir, "unselected.yaml")); !os.IsNotExist(err) {
		t.Fatalf("invalid replacement imported unselected.yaml: %v", err)
	}
}

func TestContextInitYesRejectsSelfImportFromInputDir(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := contextstore.NewContext("test")
	if err != nil {
		t.Fatal(err)
	}
	inputDir := ctx.InputDir
	_, stderr, code = runCLI(t, "context", "init", "test", "-f", inputDir, "--yes")
	if code == 0 {
		t.Fatal("context init --yes self-import unexpectedly succeeded")
	}
	if !strings.Contains(stderr, "must not be inside target input directory") {
		t.Fatalf("stderr does not reject self-import: %q", stderr)
	}
}

func TestContextUpdateReplacesOnlyInputFiles(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := contextstore.NewContext("test")
	if err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(ctx.SecretsDir, "manual-secret")
	statePath := filepath.Join(ctx.StateDir, "manual-state")
	runtimePath := filepath.Join(ctx.RuntimeDir, "manual-runtime")
	for path, body := range map[string]string{
		secretPath:  "secret\n",
		statePath:   "state\n",
		runtimePath: "runtime\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	sourceEnvironment := filepath.Join(source, "environment.yaml")
	body, err := os.ReadFile(sourceEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "# update marker\n"
	if err := os.WriteFile(sourceEnvironment, append(body, []byte(marker)...), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code = runCLI(t, "context", "update", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context update exited %d, stderr=%q", code, stderr)
	}
	updated, err := os.ReadFile(filepath.Join(ctx.InputDir, "environment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), marker) {
		t.Fatalf("context update did not replace input files:\n%s", updated)
	}
	for path, want := range map[string]string{
		secretPath:  "secret\n",
		statePath:   "state\n",
		runtimePath: "runtime\n",
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read preserved %s: %v", path, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", path, got, want)
		}
	}
}

func TestContextUpdatePreservesImportedInputsWhenUnselectedReplacementInvalid(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := contextstore.NewContext("test")
	if err != nil {
		t.Fatal(err)
	}
	importedPath := filepath.Join(ctx.InputDir, "environment.yaml")
	before, err := os.ReadFile(importedPath)
	if err != nil {
		t.Fatal(err)
	}

	replacement := copyFixtureYAML(t, "001-sno-libvirt")
	addFixtureResourceSelection(t, replacement)
	if err := os.WriteFile(filepath.Join(replacement, "unselected.yaml"), []byte(`apiVersion: bootwright.io/v1alpha1
kind: Host
metadata:
  name: spare-host
spec:
  addresses:
    - name: ssh
      address: 192.168.132.50
  ssh:
    addressName: ssh
    keyRef:
      name: missing-secret
  capabilities:
    - container-runtime
`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, "context", "update", "test", "-f", replacement)
	if code == 0 {
		t.Fatalf("context update unexpectedly accepted invalid unselected replacement:\n%s", stdout)
	}
	if !strings.Contains(stderr, `Host/spare-host spec.ssh.keyRef "missing-secret" is not declared`) {
		t.Fatalf("stderr missing broken reference error: %q", stderr)
	}
	after, err := os.ReadFile(importedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("invalid update changed existing environment.yaml\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(ctx.InputDir, "unselected.yaml")); !os.IsNotExist(err) {
		t.Fatalf("invalid update imported unselected.yaml: %v", err)
	}
}

func TestContextUpdateRequiresExistingContext(t *testing.T) {
	setTestHomeAndRoot(t)
	_, stderr, code := runCLI(t, "context", "update", "missing", "-f", fixturePath("001-sno-libvirt"))
	if code == 0 {
		t.Fatal("context update unexpectedly succeeded for missing context")
	}
	if !strings.Contains(stderr, `context "missing" not found`) {
		t.Fatalf("stderr missing missing-context error: %q", stderr)
	}
}

func TestSecretListJSONReportsDeclaredStatus(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	_, stderr, code := runCLI(t, "secret", "generate")
	if code != 0 {
		t.Fatalf("secret generate exited %d, stderr=%q", code, stderr)
	}
	stdout, stderr, code := runCLI(t, "secret", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("secret list exited %d, stderr=%q", code, stderr)
	}
	var report secretListReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	byName := map[string]secretListEntry{}
	for _, entry := range report.Secrets {
		byName[entry.Name] = entry
	}
	if got := byName["bmc-credentials"]; got.Type != "generated:credentials" || !got.Present {
		t.Fatalf("bmc-credentials status = %+v", got)
	}
	if got := byName["openshift-pull-secret"]; got.Type != "context" || got.Present {
		t.Fatalf("openshift-pull-secret status = %+v", got)
	}
}

func TestSecretShowPrintsRawContextSecret(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	if err := os.WriteFile(filepath.Join(ctx.SecretsDir, "manual-secret"), []byte("secret\nwith-newline\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, "secret", "show", "--name", "manual-secret")
	if code != 0 {
		t.Fatalf("secret show exited %d, stderr=%q", code, stderr)
	}
	if stdout != "secret\nwith-newline\n" {
		t.Fatalf("secret show stdout = %q", stdout)
	}
}

func TestSecretShowRejectsInvalidName(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	_, stderr, code := runCLI(t, "secret", "show", "--name", "../manual-secret")
	if code == 0 {
		t.Fatal("secret show accepted invalid name")
	}
	if !strings.Contains(stderr, "--name must be a lowercase DNS label") {
		t.Fatalf("stderr missing DNS label error: %q", stderr)
	}
}

func TestSecretDeleteDoesNotRequireValidDesiredState(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	if err := os.WriteFile(filepath.Join(ctx.SecretsDir, "manual-secret"), []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctx.InputDir, "environment.yaml"), []byte("not: [valid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "secret", "delete", "manual-secret", "--yes")
	if code != 0 {
		t.Fatalf("secret delete exited %d, stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(ctx.SecretsDir, "manual-secret")); !os.IsNotExist(err) {
		t.Fatalf("manual-secret still exists or stat failed unexpectedly: %v", err)
	}
}

func TestContextValidateReportsReadyAndMissingChecks(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "context", "validate")
	if code != 0 {
		t.Fatalf("context validate exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "[OK] desired state") {
		t.Fatalf("stdout missing desired-state OK:\n%s", stdout)
	}
	if err := os.RemoveAll(ctx.InputDir); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = runCLI(t, "context", "validade")
	if code == 0 {
		t.Fatal("context validade unexpectedly passed with missing input dir")
	}
	if !strings.Contains(stdout, "[MISSING] input-dir") {
		t.Fatalf("stdout missing input-dir MISSING:\n%s", stdout)
	}
}

func TestContextBackedCommandRequiresReadyContext(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	if err := os.RemoveAll(ctx.InputDir); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "check", "syntax")
	if code == 0 {
		t.Fatal("check syntax unexpectedly ran with missing context input dir")
	}
	if !strings.Contains(stderr, "bootwright context validate") {
		t.Fatalf("stderr missing context validate hint: %q", stderr)
	}
}

func TestLocalRootGateArgs(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"context", "list"}, want: false},
		{args: []string{"context", "current"}, want: false},
		{args: []string{"context", "init", "lab", "-f", "."}, want: false},
		{args: []string{"context", "update", "lab", "-f", "."}, want: false},
		{args: []string{"context", "validate"}, want: true},
		{args: []string{"secret", "show", "--name", "pull-secret"}, want: true},
		{args: []string{"check", "syntax"}, want: true},
		{args: []string{"check", "--help"}, want: false},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if got := argsNeedLocalRoot(tc.args); got != tc.want {
				t.Fatalf("argsNeedLocalRoot(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestEnsureLocalRootForArgsReexecsThroughSudo(t *testing.T) {
	setTestHomeAndRoot(t)
	previous := localRootGate
	defer func() { localRootGate = previous }()

	var gotName string
	var gotArgs []string
	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 1000 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			gotName = name
			gotArgs = append([]string(nil), args...)
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestLocalRootGateHelperProcess", "--")
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_ROOT_GATE_HELPER=1")
			return cmd
		},
	}

	code, handled, err := ensureLocalRootForArgs(context.Background(), []string{"check", "syntax"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ensureLocalRootForArgs: %v", err)
	}
	if !handled || code != 0 {
		t.Fatalf("ensureLocalRootForArgs handled=%v code=%d, want handled success", handled, code)
	}
	if gotName != "sudo" {
		t.Fatalf("command name = %q, want sudo", gotName)
	}
	if !reflect.DeepEqual(gotArgs, []string{"/usr/local/bin/bootwright", "check", "syntax"}) {
		t.Fatalf("sudo args = %v", gotArgs)
	}
}

func TestEnsureLocalRootForArgsSkipsWhenAlreadyRoot(t *testing.T) {
	previous := localRootGate
	defer func() { localRootGate = previous }()

	called := false
	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 0 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			called = true
			return exec.CommandContext(ctx, os.Args[0], "-test.run=TestLocalRootGateHelperProcess", "--")
		},
	}
	_, handled, err := ensureLocalRootForArgs(context.Background(), []string{"check", "syntax"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ensureLocalRootForArgs: %v", err)
	}
	if handled || called {
		t.Fatalf("ensureLocalRootForArgs should skip sudo when euid is root, handled=%v called=%v", handled, called)
	}
}

func TestContextInitStagesInputAndSyncsRegistryAroundSudo(t *testing.T) {
	setTestHomeAndRoot(t)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "environment.yaml"), []byte("apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: lab\nspec:\n  baseDomain: example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := localRootGate
	defer func() { localRootGate = previous }()

	var gotName string
	var gotArgs []string
	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 1000 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			gotName = name
			gotArgs = append([]string(nil), args...)
			helperArgs := append([]string{"-test.run=TestContextInitRootHelperProcess", "--"}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_CONTEXT_INIT_HELPER=1")
			return cmd
		},
	}

	stdout, stderr, code := runCLI(t, "context", "init", "lab", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if gotName != "sudo" {
		t.Fatalf("command name = %q, want sudo", gotName)
	}
	if slices.Contains(gotArgs, source) {
		t.Fatalf("sudo args used original input source instead of staged input: %v", gotArgs)
	}
	registry, err := contextstore.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := contextstore.Load(registry)
	if err != nil {
		t.Fatal(err)
	}
	if store.Current != "lab" || !contextstore.Contains(store, "lab") {
		t.Fatalf("registry was not synced after sudo child: %+v", store)
	}
}

func TestContextInitRootHelperProcess(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_CONTEXT_INIT_HELPER") != "1" {
		return
	}
	args := os.Args
	sep := slices.Index(args, "--")
	if sep < 0 || len(args) < sep+7 {
		os.Exit(2)
	}
	rootArgs := args[sep+1:]
	if rootArgs[1] != "context" || rootArgs[2] != "init" || rootArgs[3] != "lab" || rootArgs[4] != "-f" {
		os.Exit(2)
	}
	if _, err := os.Stat(filepath.Join(rootArgs[5], "environment.yaml")); err != nil {
		os.Exit(2)
	}
	registry := os.Getenv(contextstore.InternalRegistryEnv)
	if registry == "" {
		os.Exit(2)
	}
	if err := contextstore.Save(registry, contextstore.Store{Current: "lab", Contexts: []string{"lab"}}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestLocalRootGateHelperProcess(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_ROOT_GATE_HELPER") != "1" {
		return
	}
	os.Exit(0)
}

func TestContextPrintEnvRequiresSensitiveForProxyCredentials(t *testing.T) {
	initTestContext(t, "002-sno-emul-baremetal")
	_, stderr, code := runCLI(t, "print-env")
	if code == 0 {
		t.Fatal("print-env unexpectedly printed proxy credentials without --sensitive")
	}
	if !strings.Contains(stderr, "--sensitive") {
		t.Fatalf("stderr = %q, want --sensitive hint", stderr)
	}
	_, stderr, code = runCLI(t, "secret", "set", "proxy-credentials", "--username", "proxy", "--password", "secret")
	if code != 0 {
		t.Fatalf("secret set exited %d, stderr=%q", code, stderr)
	}
	stdout, stderr, code := runCLI(t, "print-env", "--sensitive")
	if code != 0 {
		t.Fatalf("print-env --sensitive exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"export BOOTWRIGHT_CONTEXT=test\n",
		"export HTTP_PROXY=http://proxy:secret@192.168.132.1:3128\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestContextPrintEnvProxyWithoutAuth(t *testing.T) {
	initTestContext(t, "002-sno-emul-baremetal")
	ctx, err := currentContext()
	if err != nil {
		t.Fatal(err)
	}
	replaceInFile(t, filepath.Join(ctx.InputDir, "environment.yaml"), `          auth:
            proxyAuthRef:
              name: proxy-credentials
`, "")

	stdout, stderr, code := runCLI(t, "print-env")
	if code != 0 {
		t.Fatalf("print-env exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"export HTTP_PROXY=http://192.168.132.1:3128\n",
		"export HTTPS_PROXY=http://192.168.132.1:3128\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestContextPrintEnvSuppressesManagedProxy(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "print-env")
	if code != 0 {
		t.Fatalf("print-env exited %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "HTTP_PROXY") || strings.Contains(stdout, "HTTPS_PROXY") {
		t.Fatalf("managed proxy should not be exported:\n%s", stdout)
	}
}

func TestContextPrintEnvNoProxy(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "print-env")
	if code != 0 {
		t.Fatalf("print-env exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "export BOOTWRIGHT_CONTEXT="+ctx.Name+"\n") {
		t.Fatalf("stdout missing context export:\n%s", stdout)
	}
	for _, reject := range []string{
		"BOOTWRIGHT_BASE_DIR",
		"BOOTWRIGHT_INPUT_DIR",
		"BOOTWRIGHT_STATE_DIR",
		"BOOTWRIGHT_RUNTIME_DIR",
		"BOOTWRIGHT_SECRETS_DIR",
	} {
		if strings.Contains(stdout, reject) {
			t.Fatalf("stdout unexpectedly contains %s export:\n%s", reject, stdout)
		}
	}
	if strings.Contains(stdout, "HTTP_PROXY") {
		t.Fatalf("stdout unexpectedly contains proxy export:\n%s", stdout)
	}
}

func replaceInFile(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(data), old, new, 1)
	if body == string(data) {
		t.Fatalf("%s did not contain replacement target %q", path, old)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func addFixtureResourceSelection(t *testing.T, dir string) {
	t.Helper()
	replaceInFile(t, filepath.Join(dir, "environment.yaml"), "  baseDomain: bootwright.test\n\n", `  baseDomain: bootwright.test

  resources:
    - hosts.yaml
    - networks.yaml
    - provider.yaml
    - infra-component.yaml
    - cluster-infra.yaml
    - container-cluster.yaml

`)
}

func TestRenderOutputDirRequiresSensitive(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	outputDir := filepath.Join(t.TempDir(), "rendered")
	stdout, stderr, code := runCLI(t, "render", "--output-dir", outputDir, "--scope", "sno-libvirt")
	if code == 0 {
		t.Fatalf("render --output-dir unexpectedly succeeded:\n%s", stdout)
	}
	for _, want := range []string{"--sensitive", "secret material", "local, unversioned", "protect those files"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("render --output-dir without --sensitive wrote %s: %v", outputDir, err)
	}

	_, _, code = runCLI(t, "render", "--output-dir", outputDir, "--scope", "sno-libvirt", "--resolve-secrets")
	if code == 0 {
		t.Fatal("render --output-dir accepted removed --resolve-secrets flag")
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("render --output-dir with removed --resolve-secrets wrote %s: %v", outputDir, err)
	}

	_, stderr, code = runCLI(t, "render", "--output-dir", outputDir, "--scope", "sno-libvirt", "--sensitive", "--resolve-secrets")
	if code == 0 {
		t.Fatal("render --output-dir accepted removed --resolve-secrets flag together with --sensitive")
	}
	if !strings.Contains(stderr, "unknown flag: --resolve-secrets") {
		t.Fatalf("stderr missing unknown flag for removed --resolve-secrets:\n%s", stderr)
	}

	_, stderr, code = runCLI(t, "render", "installer", "--scope", "sno-libvirt", "--resolve-secrets")
	if code == 0 {
		t.Fatal("render installer accepted removed --resolve-secrets flag")
	}
	if !strings.Contains(stderr, "unknown flag: --resolve-secrets") {
		t.Fatalf("stderr missing unknown flag for removed --resolve-secrets:\n%s", stderr)
	}
}

func TestRenderOutputDirWritesExternalToolInputs(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	home := os.Getenv("HOME")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "bootwright-ssh-key.pub"), []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForTests\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pullSecret := filepath.Join(t.TempDir(), "pull-secret.json")
	if err := os.WriteFile(pullSecret, []byte(`{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "secret", "set", "openshift-pull-secret", "--pull-secret", pullSecret)
	if code != 0 {
		t.Fatalf("secret set exited %d, stderr=%q", code, stderr)
	}
	_, stderr, code = runCLI(t, "secret", "generate")
	if code != 0 {
		t.Fatalf("secret generate exited %d, stderr=%q", code, stderr)
	}
	outputDir := filepath.Join(t.TempDir(), "rendered")
	stdout, stderr, code := runCLI(t, "render", "--output-dir", outputDir, "--scope", "sno-libvirt", "--sensitive")
	if code != 0 {
		t.Fatalf("render --output-dir exited %d, stderr=%q", code, stderr)
	}
	installConfig := filepath.Join(outputDir, "openshift-install", "sno-libvirt", "install-config.yaml")
	for _, path := range []string{
		filepath.Join(outputDir, "effective-state.yaml"),
		filepath.Join(outputDir, "bootwright.lock.yaml"),
		filepath.Join(outputDir, "ansible", "inventory.yaml"),
		filepath.Join(outputDir, "ansible", "vars.yaml"),
		installConfig,
		filepath.Join(outputDir, "openshift-install", "sno-libvirt", "agent-config.yaml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected rendered file %s: %v\nstdout:\n%s", path, err, stdout)
		}
	}
	data, err := os.ReadFile(installConfig)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "bootwright-secret-ref:") || !strings.Contains(string(data), "quay.io") {
		t.Fatalf("install-config was not rendered as effective openshift-install input:\n%s", data)
	}
	for _, want := range []string{
		"OpenShift install commands",
		"openshift-install agent create image --dir " + filepath.Join(outputDir, "openshift-install", "sno-libvirt"),
		"openshift-install agent wait-for install-complete --dir " + filepath.Join(outputDir, "openshift-install", "sno-libvirt") + " --log-level info",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestRenderOutputDirRejectsNonEmptyUnmarkedDirectory(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	outputDir := filepath.Join(t.TempDir(), "rendered")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "existing.txt"), []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, "render", "--output-dir", outputDir, "--scope", "sno-libvirt", "--sensitive")
	if code == 0 {
		t.Fatal("render --output-dir accepted a non-empty unmarked output directory")
	}
	if !strings.Contains(stderr, "not marked as Bootwright-owned") {
		t.Fatalf("stderr does not explain managed root rejection: %q", stderr)
	}
}

func TestControllerCLIBastionInventoryUsesLocalhost(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")

	body, err := controllerCLIInventoryBody(state, "/context/secrets")
	if err != nil {
		t.Fatalf("controllerCLIInventoryBody: %v", err)
	}
	for _, reject := range []string{
		"ansible_become_",
		"ansible_pipelining",
		"ansible_ssh_private_key_file",
	} {
		if strings.Contains(body, reject) {
			t.Fatalf("controller CLI inventory should not contain %q: %q", reject, body)
		}
	}

	var inv map[string]any
	if err := yaml.Unmarshal([]byte(body), &inv); err != nil {
		t.Fatalf("decode inventory: %v\n%s", err, body)
	}
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	controller := hosts["localhost"].(map[string]any)
	if got := controller["ansible_connection"]; got != "local" {
		t.Fatalf("ansible_connection = %v, want local", got)
	}
	if got := controller["ansible_host"]; got != "localhost" {
		t.Fatalf("ansible_host = %v, want localhost", got)
	}

	children := all["children"].(map[string]any)
	groupHosts := children[render.GroupControllerHosts].(map[string]any)["hosts"].(map[string]any)
	if _, ok := groupHosts["localhost"]; !ok {
		t.Fatalf("%s hosts = %v, want localhost", render.GroupControllerHosts, groupHosts)
	}
}

func TestControllerCLIAnsibleEnvForcesSystemTemps(t *testing.T) {
	env := controllerCLIAnsibleEnv("/bundle")
	for _, key := range []string{
		"ANSIBLE_LOCAL_TEMP",
		"ANSIBLE_REMOTE_TEMP",
		"ANSIBLE_REMOTE_TMP",
	} {
		if got := env[key]; got != "/tmp" {
			t.Fatalf("%s = %q, want /tmp", key, got)
		}
	}
}

func TestControllerCLIInstallCommandPromptsForBecomeWhenRequested(t *testing.T) {
	got := controllerCLIInstallCommand([]string{"/venv/bin/ansible-playbook", "play.yml"}, true, "")
	want := []string{
		"/venv/bin/ansible-playbook",
		"play.yml",
		"--ask-become-pass",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("controller CLI command mismatch\n got %v\nwant %v", got, want)
	}
}

func TestControllerCLIInstallCommandUsesBecomePasswordFile(t *testing.T) {
	got := controllerCLIInstallCommand([]string{"/venv/bin/ansible-playbook", "play.yml"}, true, "/tmp/bootwright-become")
	want := []string{
		"/venv/bin/ansible-playbook",
		"play.yml",
		"--become-password-file",
		"/tmp/bootwright-become",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("controller CLI command mismatch\n got %v\nwant %v", got, want)
	}
}

func TestControllerCLIInstallCommandLeavesArgsUnwrappedWhenPromptDisabled(t *testing.T) {
	got := controllerCLIInstallCommand([]string{"/venv/bin/ansible-playbook", "play.yml"}, false, "")
	want := []string{
		"/venv/bin/ansible-playbook",
		"play.yml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("controller CLI command mismatch\n got %v\nwant %v", got, want)
	}
}

func TestControllerCLIInstallCommandCopiesArgs(t *testing.T) {
	args := []string{"/venv/bin/ansible-playbook", "play.yml"}
	got := controllerCLIInstallCommand(args, false, "")
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("controller CLI command got %v, want %v", got, args)
	}
	got[0] = "changed"
	if args[0] == "changed" {
		t.Fatal("controller CLI command aliases input args")
	}
}

func TestRunBootstrapPlanRefreshesSudoWithOneBecomePassword(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	passwordLog := filepath.Join(dir, "passwords")
	fakeSudo := filepath.Join(fakeBin, "sudo")
	if err := os.WriteFile(fakeSudo, []byte(`#!/bin/sh
validate=0
noninteractive=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    -S)
      shift
      ;;
    -p)
      shift 2
      ;;
    -v)
      validate=1
      shift
      ;;
    -n)
      noninteractive=1
      shift
      ;;
    --preserve-env=*)
      shift
      ;;
    *)
      break
      ;;
  esac
done
if [ "$validate" -eq 1 ]; then
  IFS= read -r password
  printf '%s\n' "$password" >> "$BOOTWRIGHT_TEST_PASSWORD_LOG"
  test "$password" = secret
  exit $?
fi
if [ "$noninteractive" -ne 1 ]; then
  printf '%s\n' 'interactive sudo was not disabled' >&2
  exit 23
fi
exec "$@"
`), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	plan := []operator.BootstrapStep{
		{Label: "first root step", Cmd: []string{"sudo", "sh", "-c", "printf first"}},
		{Label: "second root step", Cmd: []string{"sudo", "sh", "-c", "printf second"}},
	}
	err := runBootstrapPlan(
		context.Background(),
		strings.NewReader("unused\n"),
		&stdout,
		&stderr,
		plan,
		map[string]string{"BOOTWRIGHT_TEST_PASSWORD_LOG": passwordLog},
		"secret",
		true,
	)
	if err != nil {
		t.Fatalf("runBootstrapPlan: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want no sudo prompt", got)
	}
	if got := stdout.String(); !strings.Contains(got, "$ sudo -n sh -c 'printf first'") || !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("stdout missing noninteractive sudo command or step output:\n%s", got)
	}
	body, err := os.ReadFile(passwordLog)
	if err != nil {
		t.Fatalf("read password log: %v", err)
	}
	if got := string(body); got != "secret\nsecret\n" {
		t.Fatalf("password refreshes = %q, want two refreshes with one collected password", got)
	}
}

func TestRunBootstrapPlanDisablesInteractiveSudoWhenBecomePromptDisabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	fakeSudo := filepath.Join(fakeBin, "sudo")
	if err := os.WriteFile(fakeSudo, []byte(`#!/bin/sh
if [ "$1" != "-n" ]; then
  printf '%s\n' 'sudo was allowed to prompt' >&2
  exit 24
fi
shift
exec "$@"
`), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runBootstrapPlan(
		context.Background(),
		strings.NewReader("unused\n"),
		&stdout,
		&stderr,
		[]operator.BootstrapStep{{Label: "root step", Cmd: []string{"sudo", "sh", "-c", "printf ok"}}},
		nil,
		"",
		false,
	)
	if err != nil {
		t.Fatalf("runBootstrapPlan: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "$ sudo -n sh -c 'printf ok'") || !strings.Contains(got, "ok") {
		t.Fatalf("stdout missing noninteractive sudo command or output:\n%s", got)
	}
}

func TestRunControllerCLIInstallWithBundleUsesPreparedBecomePasswordFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	fakeAnsible := filepath.Join(dir, "ansible-playbook")
	argsPath := filepath.Join(dir, "args")
	passwordPath := filepath.Join(dir, "password-path")
	passwordBodyPath := filepath.Join(dir, "password-body")
	if err := os.WriteFile(fakeAnsible, []byte(`#!/bin/sh
printf '%s\n' "$@" > "$BOOTWRIGHT_TEST_ARGS"
found=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --ask-become-pass)
      exit 21
      ;;
    --become-password-file)
      shift
      found=1
      printf '%s\n' "$1" > "$BOOTWRIGHT_TEST_PASSWORD_PATH"
      cat "$1" > "$BOOTWRIGHT_TEST_PASSWORD_BODY"
      ;;
  esac
  shift
done
test "$found" -eq 1
`), 0o755); err != nil {
		t.Fatalf("write fake ansible: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	bundleDir := t.TempDir()
	spec := operator.CLIInstallSpec{
		OCPReleaseVersion: "4.21.12",
		InstallDir:        "/usr/local/bin",
		Executable:        fakeAnsible,
	}
	err := runControllerCLIInstallWithBundle(
		context.Background(),
		strings.NewReader("secret\n"),
		&stdout,
		&stderr,
		loadFixtureState(t, "001-sno-libvirt"),
		t.TempDir(),
		spec,
		map[string]string{
			"BOOTWRIGHT_TEST_ARGS":          argsPath,
			"BOOTWRIGHT_TEST_PASSWORD_PATH": passwordPath,
			"BOOTWRIGHT_TEST_PASSWORD_BODY": passwordBodyPath,
		},
		true,
		bundleDir,
	)
	if err != nil {
		t.Fatalf("runControllerCLIInstallWithBundle: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if got := stderr.String(); got != "\nBECOME password: " {
		t.Fatalf("stderr prompt = %q, want BECOME password prompt only", got)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	if strings.Contains(string(args), "--ask-become-pass") {
		t.Fatalf("actual ansible command should not ask interactively:\n%s", args)
	}
	if !strings.Contains(string(args), "--become-password-file\n") {
		t.Fatalf("actual ansible command missing --become-password-file:\n%s", args)
	}
	passwordBody, err := os.ReadFile(passwordBodyPath)
	if err != nil {
		t.Fatalf("read recorded password body: %v", err)
	}
	if string(passwordBody) != "secret\n" {
		t.Fatalf("password body = %q, want secret newline", passwordBody)
	}
	passwordFile, err := os.ReadFile(passwordPath)
	if err != nil {
		t.Fatalf("read recorded password path: %v", err)
	}
	if _, err := os.Stat(strings.TrimSpace(string(passwordFile))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prepared password file was not cleaned up: %v", err)
	}
}

func TestStatusInstallerFreshness(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")

	t.Run("missing", func(t *testing.T) {
		report, err := buildStatusReport(testCommonFlags(t, t.TempDir(), "001-sno-libvirt"))
		if err != nil {
			t.Fatal(err)
		}
		requireSingleClusterFreshness(t, report, installerFreshnessMissing)
	})

	t.Run("unknown", func(t *testing.T) {
		baseDir := t.TempDir()
		cf := testCommonFlags(t, baseDir, "001-sno-libvirt")
		installer := installerInstallConfigPath(cf.ctx.StateDir, "sno-libvirt")
		if err := os.MkdirAll(filepath.Dir(installer), 0o755); err != nil {
			t.Fatalf("mkdir installer dir: %v", err)
		}
		if err := os.WriteFile(installer, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write installer: %v", err)
		}
		report, err := buildStatusReport(cf)
		if err != nil {
			t.Fatal(err)
		}
		requireSingleClusterFreshness(t, report, installerFreshnessUnknown)
	})

	t.Run("installer stat error", func(t *testing.T) {
		baseDir := t.TempDir()
		cf := testCommonFlags(t, baseDir, "001-sno-libvirt")
		installer := installerInstallConfigPath(cf.ctx.StateDir, "sno-libvirt")
		if err := os.MkdirAll(installer, 0o755); err != nil {
			t.Fatalf("mkdir installer path: %v", err)
		}
		report, err := buildStatusReport(cf)
		if err != nil {
			t.Fatal(err)
		}
		requireSingleClusterFreshness(t, report, installerFreshnessUnknown)
		if detail := report.Clusters[0].FreshnessDetail; !strings.Contains(detail, "is a directory") {
			t.Fatalf("freshness detail = %q, want directory stat error", detail)
		}
	})

	t.Run("fresh", func(t *testing.T) {
		baseDir := t.TempDir()
		cf := testCommonFlags(t, baseDir, "001-sno-libvirt")
		if _, err := render.All(cf.ctx.StateDir, t.TempDir(), t.TempDir(), state); err != nil {
			t.Fatalf("render: %v", err)
		}
		report, err := buildStatusReport(cf)
		if err != nil {
			t.Fatal(err)
		}
		requireSingleClusterFreshness(t, report, installerFreshnessFresh)
	})

	t.Run("stale", func(t *testing.T) {
		baseDir := t.TempDir()
		cf := testCommonFlags(t, baseDir, "001-sno-libvirt")
		if _, err := render.All(cf.ctx.StateDir, t.TempDir(), t.TempDir(), state); err != nil {
			t.Fatalf("render: %v", err)
		}
		stale := state
		stale.ContainerClusters[0].Spec.Distribution.Release.Version = "4.99.0"
		data, err := yaml.Marshal(stale)
		if err != nil {
			t.Fatalf("marshal stale state: %v", err)
		}
		if err := os.WriteFile(filepath.Join(cf.ctx.StateDir, "effective-state.yaml"), data, 0o644); err != nil {
			t.Fatalf("write stale effective state: %v", err)
		}
		report, err := buildStatusReport(cf)
		if err != nil {
			t.Fatal(err)
		}
		requireSingleClusterFreshness(t, report, installerFreshnessStale)
	})
}

func TestStatusReportsApplyLedger(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{Parallelism: 2}, []workflow.TaskLedgerEntry{
		{ID: "provider", Kind: "providerServices", Label: "provider services", Status: workflow.TaskStatusOK},
		{ID: "iso.sno-libvirt", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso sno-libvirt", Cluster: "sno-libvirt", Dependencies: []string{"provider"}},
		{ID: "boot.sno-libvirt", Kind: workflow.ApplyTaskKindNodeBoot, Label: "boot sno-libvirt nodes", Cluster: "sno-libvirt", Dependencies: []string{"iso.sno-libvirt"}},
		{ID: "wait.sno-libvirt", Kind: workflow.ApplyTaskKindInstallWait, Label: "wait install sno-libvirt", Cluster: "sno-libvirt", Dependencies: []string{"boot.sno-libvirt"}},
	}, now)
	ledger.MarkOK("provider", now.Add(time.Second))
	ledger.MarkOK("iso.sno-libvirt", now.Add(2*time.Second))
	ledger.MarkRunning("boot.sno-libvirt", "/tmp/boot.log", now.Add(3*time.Second))
	if err := workflow.SaveRunLedger(ctx.StateDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}
	if err := workflow.SaveRunLease(ctx.StateDir, workflow.NewRunLease("apply-test", time.Now().UTC())); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}

	stdout, stderr, code := runCLI(t, "status")
	if code != 0 {
		t.Fatalf("status exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"Current apply", "apply-test", "Progress", "Boot sno-libvirt nodes", "0/1 boot stages done", "bootwright status --watch"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status output missing %q:\n%s", want, stdout)
		}
	}
}

func TestStatusReportsStaleApplyLedger(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-stale", "cluster", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "iso.sno-libvirt", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso sno-libvirt", Cluster: "sno-libvirt"},
	}, now)
	if err := workflow.SaveRunLedger(ctx.StateDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}

	stdout, stderr, code := runCLI(t, "status")
	if code != 0 {
		t.Fatalf("status exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"apply-stale", "Lease", "apply lease is missing", "next apply or destroy will mark it cancelled"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status output missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "bootwright status --watch") {
		t.Fatalf("stale apply status should not recommend watch:\n%s", stdout)
	}
}

func TestStatusJSONIncludesApplyLedger(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	ledger := workflow.NewRunLedger("apply-json", "infra", "", workflow.ConcurrencyLimits{Parallelism: 1}, []workflow.TaskLedgerEntry{
		{ID: "provider", Kind: "providerServices", Label: "provider services"},
	}, time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC))
	if err := workflow.SaveRunLedger(ctx.StateDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}

	stdout, stderr, code := runCLI(t, "status", "--output", "json")
	if code != 0 {
		t.Fatalf("status json exited %d, stderr=%q", code, stderr)
	}
	var report statusReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode status json: %v\n%s", err, stdout)
	}
	if report.ApplyRun == nil || report.ApplyRun.RunID != "apply-json" {
		t.Fatalf("apply run missing from report: %+v", report.ApplyRun)
	}
	if report.ApplyRunActivity == nil || report.ApplyRunActivity.State == "" {
		t.Fatalf("apply run activity missing from report: %+v", report.ApplyRunActivity)
	}
}

func TestReconcileCurrentApplyCancelsStaleLedger(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-stale", "cluster", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "iso.sno-libvirt", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso sno-libvirt"},
	}, now)
	if err := workflow.SaveRunLedger(stateDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}

	var stdout bytes.Buffer
	if err := reconcileCurrentApplyBeforeMutation(&stdout, stateDir); err != nil {
		t.Fatalf("reconcileCurrentApplyBeforeMutation: %v", err)
	}
	loaded, ok, err := workflow.LoadRunLedger(stateDir)
	if err != nil {
		t.Fatalf("LoadRunLedger: %v", err)
	}
	if !ok || loaded.Status != workflow.RunStatusCancelled || loaded.Tasks[0].Status != workflow.TaskStatusCancelled {
		t.Fatalf("ledger was not cancelled: ok=%v ledger=%+v", ok, loaded)
	}
	if !strings.Contains(stdout.String(), "stale apply") || !strings.Contains(stdout.String(), "marked apply-stale cancelled") {
		t.Fatalf("stdout missing stale apply warning:\n%s", stdout.String())
	}
}

func TestReconcileCurrentApplyBlocksFreshLedger(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Now().UTC()
	ledger := workflow.NewRunLedger("apply-active", "cluster", "", workflow.ConcurrencyLimits{}, nil, now)
	if err := workflow.SaveRunLedger(stateDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}
	if err := workflow.SaveRunLease(stateDir, workflow.NewRunLease("apply-active", now)); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}

	var stdout bytes.Buffer
	err := reconcileCurrentApplyBeforeMutation(&stdout, stateDir)
	if err == nil || !strings.Contains(err.Error(), "apply run apply-active is still running") {
		t.Fatalf("expected active apply error, got %v", err)
	}
	loaded, _, err := workflow.LoadRunLedger(stateDir)
	if err != nil {
		t.Fatalf("LoadRunLedger: %v", err)
	}
	if loaded.Status != workflow.RunStatusRunning {
		t.Fatalf("active ledger status changed to %s", loaded.Status)
	}
	if stdout.Len() != 0 {
		t.Fatalf("active ledger should not print stale warning:\n%s", stdout.String())
	}
}

func TestApplyDryRunJSONIncludesParallelNodeBootTasks(t *testing.T) {
	initTestContext(t, "005-3nodes-baremetal")
	stdout, stderr, code := runCLI(t, "apply", "cluster", "--dry-run", "--output", "json", "--ask-become-pass=true")
	if code != 0 {
		t.Fatalf("apply dry-run json exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode apply dry-run json: %v\n%s", err, stdout)
	}
	if report.ApplyPlan == nil {
		t.Fatalf("apply plan missing from report: %+v", report)
	}
	if report.ApplyPlan.Limits.Parallelism != 3 {
		t.Fatalf("parallelism = %d, want 3 safe-auto tasks", report.ApplyPlan.Limits.Parallelism)
	}
	if report.ApplyPlan.Limits.ParallelismPerHost != 1 {
		t.Fatalf("per-host parallelism = %d, want 1 safety lock", report.ApplyPlan.Limits.ParallelismPerHost)
	}
	if report.ApplyPlan.Limits.ParallelismRedfish != 3 {
		t.Fatalf("redfish parallelism = %d, want 3 node boot tasks", report.ApplyPlan.Limits.ParallelismRedfish)
	}
	tasks := report.ApplyPlan.Tasks
	if len(tasks) != 3 {
		t.Fatalf("planned %d tasks, want 3: %+v", len(tasks), tasks)
	}
	var bootTask *workflow.TaskLedgerEntry
	for _, task := range tasks {
		if task.Kind == workflow.ApplyTaskKindNodeBoot {
			task := task
			bootTask = &task
		}
	}
	if bootTask == nil {
		t.Fatalf("missing boot task in %+v", tasks)
	}
	if bootTask.ID != "boot.3-nodes-ocp-baremetal" {
		t.Fatalf("boot task = %s, want boot.3-nodes-ocp-baremetal", bootTask.ID)
	}
	if len(bootTask.Dependencies) != 1 || bootTask.Dependencies[0] != "iso.3-nodes-ocp-baremetal" {
		t.Fatalf("boot deps = %v, want iso.3-nodes-ocp-baremetal", bootTask.Dependencies)
	}
	if bootTask.Node != "" {
		t.Fatalf("boot node field = %q, want empty stage-level task", bootTask.Node)
	}
	if len(bootTask.ResourceKeys) != 3 {
		t.Fatalf("boot resource keys = %v, want three Redfish keys", bootTask.ResourceKeys)
	}
	if bootTask.ClusterLogPath == "" || !strings.Contains(bootTask.ClusterLogPath, filepath.Join("clusters", "3-nodes-ocp-baremetal", "install.log")) {
		t.Fatalf("boot cluster log path = %q", bootTask.ClusterLogPath)
	}
	wait := tasks[len(tasks)-1]
	if wait.ID != "wait.3-nodes-ocp-baremetal" {
		t.Fatalf("last task = %s, want wait.3-nodes-ocp-baremetal", wait.ID)
	}
	if len(wait.Dependencies) != 1 || wait.Dependencies[0] != "boot.3-nodes-ocp-baremetal" {
		t.Fatalf("wait deps = %v, want boot.3-nodes-ocp-baremetal", wait.Dependencies)
	}
}

func TestApplyClusterOverrideDryRunPassesInstallOverride(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "apply", "cluster", "--dry-run", "--output", "json", "--override")
	if code != 0 {
		t.Fatalf("apply cluster override dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode apply dry-run json: %v\n%s", err, stdout)
	}
	if !slices.Contains(report.ExtraVars, "bootwright_install_override=true") {
		t.Fatalf("extra vars missing install override: %+v", report.ExtraVars)
	}
	if !slices.Contains(report.Command, "bootwright_install_override=true") {
		t.Fatalf("command missing install override: %+v", report.Command)
	}
}

func TestStatusWatchStopsWhenNoRunLedgerExists(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "status", "--watch", "--watch-interval", "1ms")
	if code != 0 {
		t.Fatalf("status --watch exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "Current apply") {
		t.Fatalf("status --watch output missing Current apply:\n%s", stdout)
	}
}

func TestStatusWatchStopsWhenApplyLedgerIsStale(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	ledger := workflow.NewRunLedger("apply-stale-watch", "cluster", "", workflow.ConcurrencyLimits{}, nil, time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC))
	if err := workflow.SaveRunLedger(ctx.StateDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}

	stdout, stderr, code := runCLI(t, "status", "--watch", "--watch-interval", "1ms")
	if code != 0 {
		t.Fatalf("status --watch exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "apply-stale-watch") || !strings.Contains(stdout, "apply lease is missing") {
		t.Fatalf("status --watch output missing stale apply:\n%s", stdout)
	}
}

func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func setTestHomeAndRoot(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Cleanup(contextstore.SetRootDirForTest(filepath.Join(home, "bootwright-root")))
	return home
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "test", "e2e", name)
}

func copyFixtureYAML(t *testing.T, name string) string {
	t.Helper()
	srcRoot := fixturePath(name)
	dstRoot := t.TempDir()
	if err := filepath.WalkDir(srcRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		return os.WriteFile(dst, body, 0o600)
	}); err != nil {
		t.Fatal(err)
	}
	return dstRoot
}

func commandContains(command []string, arg string) bool {
	for _, got := range command {
		if got == arg {
			return true
		}
	}
	return false
}

func initTestContext(t *testing.T, fixtureName string) contextstore.Context {
	t.Helper()
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "test", "-f", fixturePath(fixtureName))
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := contextstore.NewContext("test")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func testCommonFlags(t *testing.T, rootDir, fixtureName string) *commonFlags {
	t.Helper()
	t.Cleanup(contextstore.SetRootDirForTest(rootDir))
	ctx, err := contextstore.NewContext("test")
	if err != nil {
		panic(err)
	}
	ctx.InputPaths = []string{fixturePath(fixtureName)}
	return &commonFlags{
		ctx: ctx,
	}
}

func loadFixtureState(t *testing.T, name string) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{fixturePath(name)})
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return state
}

func requireSingleClusterFreshness(t *testing.T, report statusReport, want string) {
	t.Helper()
	if len(report.Clusters) != 1 {
		t.Fatalf("expected one cluster, got %+v", report.Clusters)
	}
	if got := report.Clusters[0].InstallerFreshness; got != want {
		t.Fatalf("installer freshness = %q, want %q; report=%+v", got, want, report.Clusters[0])
	}
}
