package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/bastion"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/runtime/context"
	"github.com/crmarques/bootwright/internal/runtime/root/localroot"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/state/scaffold"
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

func TestClusterTargets(t *testing.T) {
	for _, args := range [][]string{
		{"check", "clusters", "--help"},
		{"apply", "clusters", "--help"},
		{"check", "container-cluster", "--help"},
		{"apply", "container-cluster", "--help"},
		{"destroy", "container-cluster", "--help"},
	} {
		_, stderr, code := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("bootwright %s exited %d, stderr=%q", strings.Join(args, " "), code, stderr)
		}
	}

	for _, args := range [][]string{
		{"check", "cluster"},
		{"apply", "cluster"},
		{"destroy", "clusters"},
	} {
		_, stderr, code := runCLI(t, args...)
		if code == 0 {
			t.Fatalf("bootwright %s unexpectedly succeeded", strings.Join(args, " "))
		}
		if !strings.Contains(stderr, `invalid argument`) {
			t.Fatalf("%s stderr %q does not reject unsupported target", strings.Join(args, " "), stderr)
		}
	}

	_, stderr, code := runCLI(t, "destroy", "cluster")
	if code == 0 {
		t.Fatal("bootwright destroy cluster unexpectedly succeeded")
	}
	if !strings.Contains(stderr, `invalid argument "cluster"`) {
		t.Fatalf("destroy cluster stderr %q does not reject generic destroy target", stderr)
	}
}

func TestApplyHelpMatchesTargetExecutionModels(t *testing.T) {
	stdout, stderr, code := runCLI(t, "apply", "all", "--help")
	if code != 0 {
		t.Fatalf("apply all --help exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "Apply infrastructure, storage, OpenShift clusters, and addons") {
		t.Fatalf("apply all help does not mention addons:\n%s", stdout)
	}
	if !strings.Contains(stdout, "comma-separated cluster names to apply") {
		t.Fatalf("apply all help does not describe mixed cluster scope:\n%s", stdout)
	}

	stdout, stderr, code = runCLI(t, "apply", "clusters", "--help")
	if code != 0 {
		t.Fatalf("apply clusters --help exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "Provision cluster infrastructure, storage, OpenShift clusters, addons, and integrations") {
		t.Fatalf("apply clusters help does not mention lifecycle integrations:\n%s", stdout)
	}

	stdout, stderr, code = runCLI(t, "apply", "addons", "--help")
	if code != 0 {
		t.Fatalf("apply addons --help exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"--dry-run", "--yes", "--output"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("apply addons help missing %q:\n%s", want, stdout)
		}
	}
	for _, reject := range []string{"--ansible-playbook", "--ask-become-pass", "--check", "--parallelism-per-host", "--parallelism-redfish"} {
		if strings.Contains(stdout, reject) {
			t.Fatalf("apply addons help exposes provider-host flag %q:\n%s", reject, stdout)
		}
	}

	stdout, stderr, code = runCLI(t, "check", "storage-cluster", "--help")
	if code != 0 {
		t.Fatalf("check storage-cluster --help exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"comma-separated StorageCluster names to check", "bootwright check storage-cluster --scope ceph-storage"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("check storage-cluster help missing %q:\n%s", want, stdout)
		}
	}
}

func TestDestroyInfraHelpUsesArtifactServerScope(t *testing.T) {
	stdout, stderr, code := runCLI(t, "destroy", "infra", "--help")
	if code != 0 {
		t.Fatalf("destroy infra --help exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "artifact-server") {
		t.Fatalf("destroy infra help missing artifact-server scope:\n%s", stdout)
	}
	if strings.Contains(stdout, "http-server") {
		t.Fatalf("destroy infra help still exposes http-server scope:\n%s", stdout)
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

func TestCheckSyntaxValidatesInputFilesWithoutContext(t *testing.T) {
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "check", "syntax", "-f", fixturePath("001-sno-libvirt"), "--output", "json")
	if code != 0 {
		t.Fatalf("check syntax -f exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	var report syntaxCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if !report.OK || report.ContainerClusters != 1 {
		t.Fatalf("unexpected syntax report: %+v", report)
	}
}

func TestValidateCommandMatchesSyntaxCheck(t *testing.T) {
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "validate", "-f", fixturePath("001-sno-libvirt"), "--output", "json")
	if code != 0 {
		t.Fatalf("validate -f exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	var report syntaxCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if !report.OK || report.ContainerClusters != 1 {
		t.Fatalf("unexpected validate report: %+v", report)
	}
}

func TestJSONErrorEnvelopeBeforeRootReexec(t *testing.T) {
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

	stdout, stderr, code := runCLI(t, "check", "syntax", "--output", "json")
	if code == 0 {
		t.Fatalf("check syntax unexpectedly succeeded: %s", stdout)
	}
	if stderr != "" {
		t.Fatalf("json error path wrote stderr: %q", stderr)
	}
	var report commandErrorReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.OK || report.Error == "" || !strings.Contains(report.Error, "legacy context registry map") {
		t.Fatalf("unexpected error report: %+v", report)
	}
	if len(report.Diagnostics) == 0 || !strings.Contains(report.Diagnostics[0].Remediation, "context init") {
		t.Fatalf("error report missing command remediation: %+v", report.Diagnostics)
	}
}

func TestHumanOutputStructuredText(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "validate",
			args: []string{"validate"},
			want: []string{"Bootwright: validate", "Objects", "Desired state", "[OK] validate"},
		},
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
			name: "render effective",
			args: []string{"render", "effective"},
			want: []string{"Bootwright: effective-state render", "Rendered artifacts", "Effective state", "Objects"},
		},
		{
			name: "plan",
			args: []string{"plan", "--ask-become-pass=false"},
			want: []string{"Bootwright: plan", "Apply plan", "Bootwright prerequisites", "planned task(s)", "Rendered artifacts", "Bundle"},
		},
		{
			name: "apply infra dry-run",
			args: []string{"apply", "infra", "--dry-run", "--ask-become-pass=false"},
			want: []string{"Bootwright: infra apply", "Apply plan", "Bootwright prerequisites", "planned task(s)", "Provider services", "Infra component services", "Rendered artifacts", "Bundle"},
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
	if report.ReadinessChecked || !strings.Contains(report.ReadinessChecks, "not run") {
		t.Fatalf("dry-run report did not make readiness status explicit: %+v", report)
	}
	if len(report.Command) == 0 {
		t.Fatalf("dry-run report did not include planned command: %+v", report)
	}
	if !strings.HasPrefix(report.Render.EffectiveStatePath, ctx.RenderedDir) {
		t.Fatalf("effective state path %q is outside state dir %q", report.Render.EffectiveStatePath, ctx.RenderedDir)
	}
}

func TestPlanDryRunJSON(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "plan", "--output", "json", "--ask-become-pass=false")
	if code != 0 {
		t.Fatalf("plan json exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Target != "all" || report.Action != "plan" || !report.DryRun || !report.PlanOnly {
		t.Fatalf("unexpected plan report header: %+v", report)
	}
	if report.ApplyPlan == nil || len(report.ApplyPlan.Tasks) == 0 {
		t.Fatalf("plan report missing apply task graph: %+v", report.ApplyPlan)
	}
}

func TestDestroyInfraArtifactServerScopeDryRunJSON(t *testing.T) {
	initTestContext(t, "002-sno-emul-baremetal")
	stdout, stderr, code := runCLI(t,
		"destroy", "infra",
		"--scope", "artifact-server",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy infra artifact-server dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Target != "infra" || report.Action != "destroy" || !report.DryRun {
		t.Fatalf("unexpected dry-run report header: %+v", report)
	}
	if !reflect.DeepEqual(report.Phases, []string{"artifact-server"}) {
		t.Fatalf("phases = %#v, want artifact-server", report.Phases)
	}
	if report.Playbook != infraDestroyArtifactServerPlaybook {
		t.Fatalf("playbook = %q, want %q", report.Playbook, infraDestroyArtifactServerPlaybook)
	}
	if report.Limit != render.GroupInfraComponentHosts {
		t.Fatalf("limit = %q, want %q", report.Limit, render.GroupInfraComponentHosts)
	}
	if !slices.Contains(report.ExtraVars, infraComponentServiceScopeExtraVarName+"=artifact-server") {
		t.Fatalf("extra vars missing artifact-server scope: %#v", report.ExtraVars)
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
	if !strings.HasPrefix(cluster.InstallConfigPath, filepath.Join(ctx.ClustersDir, "sno-libvirt", "rendered", render.InstallerRelativeDir)) {
		t.Fatalf("install config path %q is outside cluster installer state dir %q", cluster.InstallConfigPath, ctx.ClustersDir)
	}
}

func TestRenderEffectiveWritesNormalizedState(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "render", "effective", "--output", "json")
	if code != 0 {
		t.Fatalf("render effective exited %d, stderr=%q", code, stderr)
	}
	var report renderEffectiveReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	wantPath := filepath.Join(ctx.RenderedDir, "effective-state.yaml")
	if report.EffectiveStatePath != wantPath {
		t.Fatalf("effectiveStatePath = %q, want %q", report.EffectiveStatePath, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read effective state: %v", err)
	}
	var state v1alpha1.State
	if err := yaml.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode effective state: %v\n%s", err, data)
	}
	if len(state.ContainerClusters) != 1 {
		t.Fatalf("expected one container cluster, got %d", len(state.ContainerClusters))
	}
	cluster := state.ContainerClusters[0]
	if cluster.Spec.Distribution.Type != "openshift" {
		t.Fatalf("distribution.type = %q, want openshift", cluster.Spec.Distribution.Type)
	}
	if cluster.Spec.Install.Method != "agent" || cluster.Spec.Install.Mode != "connected" {
		t.Fatalf("install defaults = method %q mode %q, want agent/connected", cluster.Spec.Install.Method, cluster.Spec.Install.Mode)
	}
}

func TestApplyAcceptsKubeVirtDispatchDryRun(t *testing.T) {
	dir := t.TempDir()
	files, err := scaffold.Workspace("kubevirt-lab", scaffold.ProviderKubeVirt)
	if err != nil {
		t.Fatalf("scaffold kubevirt workspace: %v", err)
	}
	for _, file := range files {
		path := filepath.Join(dir, file.Name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", file.Name, err)
		}
		if err := os.WriteFile(path, []byte(file.Body), 0o600); err != nil {
			t.Fatalf("write %s: %v", file.Name, err)
		}
	}

	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "kubevirt-lab", "-f", dir)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, stderr, code = runCLI(t, "apply", "infra", "--dry-run")
	if code != 0 {
		t.Fatalf("apply infra dry-run exited %d, stderr=%q", code, stderr)
	}
}

func TestApplyAllScopedKubeVirtChildDryRunReportsHostDependency(t *testing.T) {
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	stdout, stderr, code := runCLI(t, "context", "init", "nested", "-f", example)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, "apply", "all", "--scope", "dc1-child-ocp", "--dry-run", "--output", "json")
	if code == 0 {
		t.Fatalf("scoped child apply unexpectedly succeeded, stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "include dc1-metal-ocp in --scope or apply it first") {
		t.Fatalf("scoped child apply error missing host dependency remediation, stdout=%q stderr=%q", stdout, stderr)
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
	bundleDir, err := resolveBundleDir()
	if err != nil {
		t.Fatal(err)
	}
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

func TestContextInitYesAcceptsUnselectedInvalidFilesWithResourceSelection(t *testing.T) {
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
	if code != 0 {
		t.Fatalf("context init --yes exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	after, err := os.ReadFile(importedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) == string(before) {
		t.Fatalf("replacement did not change existing environment.yaml\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(ctx.InputDir, "unselected.yaml")); err != nil {
		t.Fatalf("unselected.yaml was not imported for future editing: %v", err)
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
	statePath := filepath.Join(ctx.RenderedDir, "manual-state")
	runtimePath := filepath.Join(ctx.ClustersDir, "manual-runtime")
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

func TestContextUpdateAcceptsUnselectedInvalidFilesWithResourceSelection(t *testing.T) {
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
	if code != 0 {
		t.Fatalf("context update exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	after, err := os.ReadFile(importedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) == string(before) {
		t.Fatalf("update did not change existing environment.yaml\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(ctx.InputDir, "unselected.yaml")); err != nil {
		t.Fatalf("unselected.yaml was not imported for future editing: %v", err)
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

func TestContextCurrentAndListDoNotRequireContextDirAccess(t *testing.T) {
	home := setTestHomeAndRoot(t)
	root := filepath.Join(home, "bootwright-root")
	lockTestContextsDir(t, root)
	saveTestContextRegistry(t, "test", "test")
	wantBase := filepath.Join(root, "contexts", "test")

	stdout, stderr, code := runCLI(t, "context", "current", "--short")
	if code != 0 {
		t.Fatalf("context current --short exited %d, stderr=%q", code, stderr)
	}
	if stdout != "test\n" {
		t.Fatalf("context current --short stdout = %q", stdout)
	}
	stdout, stderr, code = runCLI(t, "context", "current")
	if code != 0 {
		t.Fatalf("context current exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, wantBase) {
		t.Fatalf("context current stdout missing %s:\n%s", wantBase, stdout)
	}
	stdout, stderr, code = runCLI(t, "context", "list")
	if code != 0 {
		t.Fatalf("context list exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, wantBase) {
		t.Fatalf("context list stdout missing %s:\n%s", wantBase, stdout)
	}
}

func TestContextDeleteWithoutPurgeDoesNotRequireContextDirAccess(t *testing.T) {
	home := setTestHomeAndRoot(t)
	root := filepath.Join(home, "bootwright-root")
	lockTestContextsDir(t, root)
	saveTestContextRegistry(t, "test", "test")

	_, stderr, code := runCLI(t, "context", "delete", "test")
	if code != 0 {
		t.Fatalf("context delete exited %d, stderr=%q", code, stderr)
	}
	registry, err := contextstore.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := contextstore.Load(registry)
	if err != nil {
		t.Fatal(err)
	}
	if contextstore.Contains(store, "test") {
		t.Fatalf("context delete left test registered: %+v", store)
	}
}

func TestContextDeleteRemovesOnlyRegistryEntryWithoutPurge(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	keepPath := filepath.Join(ctx.RenderedDir, "keep")
	if err := os.WriteFile(keepPath, []byte("state\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, "context", "delete", "test")
	if code != 0 {
		t.Fatalf("context delete exited %d, stderr=%q", code, stderr)
	}
	registry, err := contextstore.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := contextstore.Load(registry)
	if err != nil {
		t.Fatal(err)
	}
	if contextstore.Contains(store, "test") {
		t.Fatalf("context delete left test registered: %+v", store)
	}
	if store.Current == "test" {
		t.Fatalf("context delete left test current: %+v", store)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("context delete without --purge removed context data: %v", err)
	}
}

func TestContextDeletePurgeRemovesContextDirectory(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	keepPath := filepath.Join(ctx.RenderedDir, "keep")
	if err := os.WriteFile(keepPath, []byte("state\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, "context", "delete", "test", "--purge", "--yes")
	if code != 0 {
		t.Fatalf("context delete --purge exited %d, stderr=%q", code, stderr)
	}
	registry, err := contextstore.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := contextstore.Load(registry)
	if err != nil {
		t.Fatal(err)
	}
	if contextstore.Contains(store, "test") {
		t.Fatalf("context delete --purge left test registered: %+v", store)
	}
	if _, err := os.Stat(ctx.BaseDir); !os.IsNotExist(err) {
		t.Fatalf("context delete --purge did not remove context dir: %v", err)
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

func TestSecretListReportsUnreadableFileAsFailedEntry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-denied stat check requires non-root test process")
	}
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocked, "secret")
	if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0o700)

	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata:   v1alpha1.Metadata{Name: "lab"},
			SourcePath: filepath.Join(dir, "environment.yaml"),
			Spec: v1alpha1.EnvironmentSpec{
				Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
					"blocked-secret": {File: path},
				},
			},
		}},
	}
	entries, err := declaredSecretEntries(filepath.Join(dir, "context-secrets"), state)
	if err != nil {
		t.Fatalf("declaredSecretEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one", entries)
	}
	if entries[0].Present {
		t.Fatalf("blocked secret reported present: %+v", entries[0])
	}
	if !strings.Contains(entries[0].Detail, "permission denied") {
		t.Fatalf("blocked secret detail = %q, want permission denied", entries[0].Detail)
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
	stdout, _, code = runCLI(t, "context", "validate")
	if code == 0 {
		t.Fatal("context validate unexpectedly passed with missing input dir")
	}
	if !strings.Contains(stdout, "[MISSING] input-dir") {
		t.Fatalf("stdout missing input-dir MISSING:\n%s", stdout)
	}
}

func TestContextValidateJSONReportsWarningsWithoutBlocking(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "context", "validate", "--output", "json")
	if code != 0 {
		t.Fatalf("context validate json exited %d, stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("context validate json wrote stderr: %q", stderr)
	}
	var report contextValidateReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode context validate json: %v\n%s", err, stdout)
	}
	if !report.OK || report.Context.Name != "test" {
		t.Fatalf("unexpected context validate report: %+v", report)
	}
	foundSecretWarning := false
	for _, check := range report.Checks {
		if check.Group == "Declared secrets" && check.Status == string(output.StatusWarn) {
			foundSecretWarning = true
			if check.Remediation == "" {
				t.Fatalf("secret warning missing remediation: %+v", check)
			}
		}
	}
	if !foundSecretWarning {
		t.Fatalf("context validate report missing declared-secret warning: %+v", report.Checks)
	}
	if len(report.NextSteps) == 0 {
		t.Fatalf("context validate report missing next steps: %+v", report)
	}
}

func TestContextValidateRejectsAccidentalAlias(t *testing.T) {
	stdout, stderr, code := runCLI(t, "context", "validade")
	if code == 0 {
		t.Fatalf("context validade unexpectedly succeeded:\n%s", stdout)
	}
	if !strings.Contains(stderr, `invalid argument "validade"`) {
		t.Fatalf("stderr does not reject validade alias: %q", stderr)
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
		{args: []string{"context", "use", "lab"}, want: false},
		{args: []string{"context", "init", "lab", "-f", "."}, want: false},
		{args: []string{"context", "update", "lab", "-f", "."}, want: false},
		{args: []string{"context", "delete", "lab"}, want: false},
		{args: []string{"context", "delete", "lab", "--purge"}, want: false},
		{args: []string{"context", "validate"}, want: true},
		{args: []string{"help", "check"}, want: false},
		{args: []string{"completion", "bash"}, want: false},
		{args: []string{cobra.ShellCompRequestCmd, ""}, want: false},
		{args: []string{cobra.ShellCompNoDescRequestCmd, ""}, want: false},
		{args: []string{"secret", "set", "openshift-pull-secret", "--pull-secret", "/home/user/pull-secret.json"}, want: false},
		{args: []string{"secret"}, want: false},
		{args: []string{"secret", "show", "--name", "pull-secret"}, want: true},
		{args: []string{"example", "init", "lab", "--output", "./lab-input"}, want: false},
		{args: []string{"check"}, want: false},
		{args: []string{"check", "syntax", "-f", "./lab-input"}, want: false},
		{args: []string{"check", "syntax", "--file=./lab-input", "--output", "json"}, want: false},
		{args: []string{"check", "syntax"}, want: true},
		{args: []string{"check", "--help"}, want: false},
		{args: []string{"apply"}, want: false},
		{args: []string{"apply", "infra"}, want: true},
		{args: []string{"destroy"}, want: false},
		{args: []string{"destroy", "infra"}, want: true},
		{args: []string{"render"}, want: false},
		{args: []string{"render", "--scope", "managed-01"}, want: false},
		{args: []string{"render", "installer"}, want: true},
		{args: []string{"render", "storage"}, want: true},
		{args: []string{"render", "effective"}, want: true},
		{args: []string{"render", "--output-dir", "./rendered", "--sensitive"}, want: true},
		{args: []string{"render", "--output-dir=./rendered", "--sensitive"}, want: true},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if got := argsNeedLocalRoot(tc.args); got != tc.want {
				t.Fatalf("argsNeedLocalRoot(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestLocalRootGateSkipsRootlessHelpAndCompletion(t *testing.T) {
	setTestHomeAndRoot(t)
	previous := localRootGate
	defer func() { localRootGate = previous }()

	called := false
	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 1000 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			called = true
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestLocalRootGateHelperProcess", "--")
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_ROOT_GATE_HELPER=1")
			return cmd
		},
	}

	cases := [][]string{
		{"check"},
		{"apply"},
		{"destroy"},
		{"secret"},
		{"render"},
		{"completion", "bash"},
		{cobra.ShellCompRequestCmd, ""},
		{cobra.ShellCompNoDescRequestCmd, ""},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			called = false
			code, handled, err := ensureLocalRootForArgs(context.Background(), args, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
			if err != nil {
				t.Fatalf("ensureLocalRootForArgs: %v", err)
			}
			if handled || code != 0 || called {
				t.Fatalf("ensureLocalRootForArgs(%v) handled=%v code=%d called=%t, want no sudo", args, handled, code, called)
			}
		})
	}
}

func TestLocalRootGateBecomeArgs(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"apply", "bastion"}, want: true},
		{args: []string{"apply", "infra"}, want: true},
		{args: []string{"apply", "clusters"}, want: true},
		{args: []string{"apply", "all"}, want: true},
		{args: []string{"destroy", "infra"}, want: true},
		{args: []string{"destroy", "container-cluster"}, want: true},
		{args: []string{"check", "infra"}, want: false},
		{args: []string{"secret", "set", "pull-secret"}, want: false},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if got := argsMayUseBecome(tc.args); got != tc.want {
				t.Fatalf("argsMayUseBecome(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestEnsureLocalRootForArgsReexecsThroughSudo(t *testing.T) {
	home := setTestHomeAndRoot(t)
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
	if len(gotArgs) != 10 || gotArgs[0] != "-n" || gotArgs[1] != "env" || !strings.HasPrefix(gotArgs[2], contextstore.InternalRegistryEnv+"=") || gotArgs[3] != localroot.InternalEnv+"=1" || gotArgs[4] != secret.InternalCallerHomeEnv+"="+home || gotArgs[5] != localroot.CallerPathEnv+"="+os.Getenv("PATH") || gotArgs[6] != localRootSudoAuthEnv+"="+localSudoAuthNonInteractive || !reflect.DeepEqual(gotArgs[7:], []string{"/usr/local/bin/bootwright", "check", "syntax"}) {
		t.Fatalf("sudo args = %v", gotArgs)
	}
}

func TestEnsureLocalRootForArgsPromptsOnceAndUsesNonInteractiveSudo(t *testing.T) {
	home := setTestHomeAndRoot(t)
	previous := localRootGate
	defer func() { localRootGate = previous }()
	previousTTY := openControllingTTY
	openControllingTTY = func() (*os.File, error) { return nil, os.ErrNotExist }
	defer func() { openControllingTTY = previousTTY }()

	var calls [][]string
	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 1000 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name != "sudo" {
				t.Fatalf("command name = %q, want sudo", name)
			}
			calls = append(calls, append([]string(nil), args...))
			helperArgs := append([]string{"-test.run=TestLocalRootGateSudoPromptHelperProcess", "--"}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_ROOT_GATE_SUDO_PROMPT_HELPER=1")
			return cmd
		},
	}

	var stderr bytes.Buffer
	code, handled, err := ensureLocalRootForArgs(context.Background(), []string{"check", "syntax"}, strings.NewReader("secret\n"), &bytes.Buffer{}, &stderr)
	if err != nil {
		t.Fatalf("ensureLocalRootForArgs: %v", err)
	}
	if !handled || code != 0 {
		t.Fatalf("ensureLocalRootForArgs handled=%v code=%d, want handled success", handled, code)
	}
	if got := stderr.String(); got != "SUDO password: " {
		t.Fatalf("stderr prompt = %q, want SUDO password prompt", got)
	}
	if len(calls) != 4 {
		t.Fatalf("sudo calls = %v, want validate, refresh, timeout lookup, command", calls)
	}
	if !reflect.DeepEqual(calls[0], []string{"-n", "-v"}) {
		t.Fatalf("first sudo call = %v, want noninteractive validation", calls[0])
	}
	if !reflect.DeepEqual(calls[1], []string{"-S", "-p", "", "-v"}) {
		t.Fatalf("second sudo call = %v, want password validation", calls[1])
	}
	if !reflect.DeepEqual(calls[2], []string{"-V"}) {
		t.Fatalf("third sudo call = %v, want timeout lookup", calls[2])
	}
	commandIndex := localRootCommandIndex(t, calls[3], home, localSudoAuthPrompted)
	if got := localRootEnvValue(calls[3], localRootBecomePasswordFileEnv); got != "" {
		t.Fatalf("check command should not receive a become password file: %v", calls[3])
	}
	if !reflect.DeepEqual(calls[3][commandIndex:], []string{"/usr/local/bin/bootwright", "check", "syntax"}) {
		t.Fatalf("fourth sudo call = %v", calls[3])
	}
}

func TestEnsureLocalRootForBecomeCommandExportsPasswordFile(t *testing.T) {
	home := setTestHomeAndRoot(t)
	previous := localRootGate
	defer func() { localRootGate = previous }()
	previousTTY := openControllingTTY
	openControllingTTY = func() (*os.File, error) { return nil, os.ErrNotExist }
	defer func() { openControllingTTY = previousTTY }()

	var calls [][]string
	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 1000 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name != "sudo" {
				t.Fatalf("command name = %q, want sudo", name)
			}
			calls = append(calls, append([]string(nil), args...))
			helperArgs := append([]string{"-test.run=TestLocalRootGateSudoPromptHelperProcess", "--"}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_ROOT_GATE_SUDO_PROMPT_HELPER=1")
			return cmd
		},
	}

	code, handled, err := ensureLocalRootForArgs(context.Background(), []string{"apply", "infra", "--yes"}, strings.NewReader("secret\n"), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ensureLocalRootForArgs: %v", err)
	}
	if !handled || code != 0 {
		t.Fatalf("ensureLocalRootForArgs handled=%v code=%d, want handled success", handled, code)
	}
	if len(calls) != 4 {
		t.Fatalf("sudo calls = %v, want validate, refresh, timeout lookup, command", calls)
	}
	if !reflect.DeepEqual(calls[2], []string{"-V"}) {
		t.Fatalf("third sudo call = %v, want timeout lookup", calls[2])
	}
	commandIndex := localRootCommandIndex(t, calls[3], home, localSudoAuthPrompted)
	if got := localRootEnvValue(calls[3], localRootBecomePasswordFileEnv); got == "" {
		t.Fatalf("apply command missing inherited become password file: %v", calls[3])
	} else if _, err := os.Stat(got); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inherited become password file was not cleaned up: %v", err)
	}
	if !reflect.DeepEqual(calls[3][commandIndex:], []string{"/usr/local/bin/bootwright", "apply", "infra", "--yes"}) {
		t.Fatalf("fourth sudo call = %v", calls[3])
	}
}

func TestEnsureLocalRootForArgsRetriesInvalidSudoPassword(t *testing.T) {
	setTestHomeAndRoot(t)
	previous := localRootGate
	defer func() { localRootGate = previous }()
	previousTTY := openControllingTTY
	openControllingTTY = func() (*os.File, error) { return nil, os.ErrNotExist }
	defer func() { openControllingTTY = previousTTY }()

	var calls [][]string
	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 1000 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name != "sudo" {
				t.Fatalf("command name = %q, want sudo", name)
			}
			calls = append(calls, append([]string(nil), args...))
			helperArgs := append([]string{"-test.run=TestLocalRootGateSudoPromptHelperProcess", "--"}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_ROOT_GATE_SUDO_PROMPT_HELPER=1")
			return cmd
		},
	}

	var stderr bytes.Buffer
	code, handled, err := ensureLocalRootForArgs(context.Background(), []string{"check", "syntax"}, strings.NewReader("wrong\nalso-wrong\nsecret\n"), &bytes.Buffer{}, &stderr)
	if err != nil {
		t.Fatalf("ensureLocalRootForArgs: %v", err)
	}
	if !handled || code != 0 {
		t.Fatalf("ensureLocalRootForArgs handled=%v code=%d, want handled success", handled, code)
	}
	if got := strings.Count(stderr.String(), "SUDO password: "); got != 3 {
		t.Fatalf("sudo prompt count = %d, stderr=%q", got, stderr.String())
	}
	if strings.Contains(stderr.String(), "sudo: no password was provided") {
		t.Fatalf("stderr includes sudo EOF diagnostic: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Sorry, try again.") {
		t.Fatalf("stderr missing sudo retry diagnostic: %q", stderr.String())
	}
	if len(calls) != 6 {
		t.Fatalf("sudo calls = %v, want noninteractive validation, three password attempts, timeout lookup, command", calls)
	}
}

func TestEnsureLocalRootForArgsFailsAfterThreeInvalidSudoPasswords(t *testing.T) {
	setTestHomeAndRoot(t)
	previous := localRootGate
	defer func() { localRootGate = previous }()
	previousTTY := openControllingTTY
	openControllingTTY = func() (*os.File, error) { return nil, os.ErrNotExist }
	defer func() { openControllingTTY = previousTTY }()

	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 1000 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			helperArgs := append([]string{"-test.run=TestLocalRootGateSudoPromptHelperProcess", "--"}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_ROOT_GATE_SUDO_PROMPT_HELPER=1")
			return cmd
		},
	}

	var stderr bytes.Buffer
	_, handled, err := ensureLocalRootForArgs(context.Background(), []string{"check", "syntax"}, strings.NewReader("wrong\nalso-wrong\nstill-wrong\n"), &bytes.Buffer{}, &stderr)
	if err == nil {
		t.Fatal("expected sudo authentication failure")
	}
	if handled {
		t.Fatal("invalid sudo credentials should not report handled command")
	}
	if !strings.Contains(err.Error(), "sudo authentication failed after 3 attempts") {
		t.Fatalf("error = %q", err)
	}
	if got := strings.Count(stderr.String(), "SUDO password: "); got != 3 {
		t.Fatalf("sudo prompt count = %d, stderr=%q", got, stderr.String())
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

func TestAskBecomePassDefaultUsesLocalRootSudoAuth(t *testing.T) {
	t.Setenv(localRootSudoAuthEnv, localSudoAuthPrompted)
	if !askBecomePassDefault() {
		t.Fatal("prompted local sudo re-exec should default Ansible become prompting on")
	}
	t.Setenv(localRootSudoAuthEnv, localSudoAuthNonInteractive)
	if askBecomePassDefault() {
		t.Fatal("noninteractive local sudo re-exec should default Ansible become prompting off")
	}
}

func TestSecretSetStagesFileInputBeforeSudo(t *testing.T) {
	setTestHomeAndRoot(t)
	source := filepath.Join(t.TempDir(), "pull-secret.json")
	if err := os.WriteFile(source, []byte(`{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`), 0o600); err != nil {
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
			helperArgs := append([]string{"-test.run=TestSecretSetRootHelperProcess", "--"}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_SECRET_SET_HELPER=1", "BOOTWRIGHT_SECRET_SET_SOURCE="+source)
			return cmd
		},
	}

	stdout, stderr, code := runCLI(t, "secret", "set", "openshift-pull-secret", "--pull-secret", source)
	if code != 0 {
		t.Fatalf("secret set exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if gotName != "sudo" {
		t.Fatalf("command name = %q, want sudo", gotName)
	}
	if slices.Contains(gotArgs, source) {
		t.Fatalf("sudo args used original secret source instead of staged input: %v", gotArgs)
	}
}

func TestSecretSetStagesPasswordInputBeforeSudo(t *testing.T) {
	rootArgs, rootStdin, cleanup, err := stagedSecretSetRootArgs(strings.NewReader("s3cr3t\n"), "proxy-credentials", "", "", "", "", "", "proxy", "", true, false)
	if err != nil {
		t.Fatalf("stagedSecretSetRootArgs: %v", err)
	}
	defer cleanup()
	if commandContains(rootArgs, "--password-stdin") {
		t.Fatalf("root args still read password from stdin: %v", rootArgs)
	}
	if commandContains(rootArgs, "s3cr3t") {
		t.Fatalf("root args exposed password: %v", rootArgs)
	}
	idx := slices.Index(rootArgs, "--from-file")
	if idx < 0 || idx+1 >= len(rootArgs) {
		t.Fatalf("root args missing staged credentials file: %v", rootArgs)
	}
	data, err := os.ReadFile(rootArgs[idx+1])
	if err != nil {
		t.Fatalf("read staged credentials: %v", err)
	}
	if string(data) != "proxy:s3cr3t\n" {
		t.Fatalf("staged credentials = %q", data)
	}
	buf := make([]byte, 1)
	n, err := rootStdin.Read(buf)
	if n != 0 || err == nil {
		t.Fatalf("root stdin should be drained, read n=%d err=%v", n, err)
	}
}

func TestSecretSetStagesRawFileInputBeforeSudo(t *testing.T) {
	source := filepath.Join(t.TempDir(), "external-details.json")
	body := []byte(`[{"name":"rook-ceph-mon","kind":"Secret","data":{"fsid":"external-fsid"}}]`)
	if err := os.WriteFile(source, body, 0o600); err != nil {
		t.Fatal(err)
	}
	rootArgs, _, cleanup, err := stagedSecretSetRootArgs(strings.NewReader(""), "shared-ceph-external-details", "", "", "", source, "", "", "", false, false)
	if err != nil {
		t.Fatalf("stagedSecretSetRootArgs: %v", err)
	}
	defer cleanup()
	if slices.Contains(rootArgs, source) {
		t.Fatalf("root args used original raw secret source instead of staged input: %v", rootArgs)
	}
	if commandContains(rootArgs, string(body)) {
		t.Fatalf("root args exposed raw secret body: %v", rootArgs)
	}
	idx := slices.Index(rootArgs, "--raw-file")
	if idx < 0 || idx+1 >= len(rootArgs) {
		t.Fatalf("root args missing staged raw secret file: %v", rootArgs)
	}
	got, err := os.ReadFile(rootArgs[idx+1])
	if err != nil {
		t.Fatalf("read staged raw secret: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("staged raw secret = %q, want %q", got, body)
	}
}

func TestSecretSetRawFileWritesContextSecret(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	source := filepath.Join(t.TempDir(), "external-details.json")
	body := []byte("[{\"name\":\"rook-ceph-mon\",\"kind\":\"Secret\",\"data\":{\"fsid\":\"external-fsid\"}}]\n")
	if err := os.WriteFile(source, body, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, "secret", "set", "shared-ceph-external-details", "--raw-file", source)
	if code != 0 {
		t.Fatalf("secret set --raw-file exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	target := filepath.Join(ctx.SecretsDir, "shared-ceph-external-details")
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read raw secret: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("raw secret = %q, want %q", got, body)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat raw secret: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("raw secret mode = %o, want 0600", info.Mode().Perm())
	}
	for _, stream := range []string{stdout, stderr} {
		if strings.Contains(stream, "external-fsid") {
			t.Fatalf("secret output leaked raw secret body: stdout=%q stderr=%q", stdout, stderr)
		}
	}
}

func TestSecretSetRawFileRejectsConflictingInputModes(t *testing.T) {
	setTestHomeAndRoot(t)
	source := filepath.Join(t.TempDir(), "external-details.json")
	if err := os.WriteFile(source, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "secret", "set", "shared-ceph-external-details", "--raw-file", source, "--from-file", source)
	if code == 0 {
		t.Fatal("secret set --raw-file unexpectedly accepted conflicting input modes")
	}
	if !strings.Contains(stderr, "mutually exclusive") {
		t.Fatalf("stderr = %q, want mutually exclusive error", stderr)
	}
}

func TestSecretSetRootHelperProcess(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_SECRET_SET_HELPER") != "1" {
		return
	}
	args := os.Args
	sep := slices.Index(args, "--")
	if sep < 0 {
		os.Exit(2)
	}
	rootArgs := args[sep+1:]
	if reflect.DeepEqual(rootArgs, []string{"-n", "-v"}) || reflect.DeepEqual(rootArgs, []string{"-S", "-p", "", "-v"}) {
		os.Exit(0)
	}
	if len(args) < sep+11 {
		os.Exit(2)
	}
	if len(rootArgs) < 13 || rootArgs[0] != "-n" || rootArgs[1] != "env" || !strings.HasPrefix(rootArgs[2], contextstore.InternalRegistryEnv+"=") || rootArgs[3] != localroot.InternalEnv+"=1" || !strings.HasPrefix(rootArgs[4], secret.InternalCallerHomeEnv+"=") || !strings.HasPrefix(rootArgs[5], localroot.CallerPathEnv+"=") || !strings.HasPrefix(rootArgs[6], localRootSudoAuthEnv+"=") {
		os.Exit(2)
	}
	rootArgs = rootArgs[8:]
	if len(rootArgs) != 5 || rootArgs[0] != "secret" || rootArgs[1] != "set" || rootArgs[2] != "openshift-pull-secret" || rootArgs[3] != "--pull-secret" {
		os.Exit(2)
	}
	if rootArgs[4] == os.Getenv("BOOTWRIGHT_SECRET_SET_SOURCE") {
		os.Exit(2)
	}
	data, err := os.ReadFile(rootArgs[4])
	if err != nil || string(data) != `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}` {
		os.Exit(2)
	}
	os.Exit(0)
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
	if sep < 0 {
		os.Exit(2)
	}
	rootArgs := args[sep+1:]
	if reflect.DeepEqual(rootArgs, []string{"-n", "-v"}) || reflect.DeepEqual(rootArgs, []string{"-S", "-p", "", "-v"}) {
		os.Exit(0)
	}
	if len(args) < sep+9 {
		os.Exit(2)
	}
	if len(rootArgs) < 13 || rootArgs[0] != "-n" || rootArgs[1] != "env" || !strings.HasPrefix(rootArgs[2], contextstore.InternalRegistryEnv+"=") || rootArgs[3] != localroot.InternalEnv+"=1" || !strings.HasPrefix(rootArgs[4], secret.InternalCallerHomeEnv+"=") || !strings.HasPrefix(rootArgs[5], localroot.CallerPathEnv+"=") || !strings.HasPrefix(rootArgs[6], localRootSudoAuthEnv+"=") {
		os.Exit(2)
	}
	registry := strings.TrimPrefix(rootArgs[2], contextstore.InternalRegistryEnv+"=")
	rootArgs = rootArgs[8:]
	if rootArgs[0] != "context" || rootArgs[1] != "init" || rootArgs[2] != "lab" || rootArgs[3] != "-f" {
		os.Exit(2)
	}
	if _, err := os.Stat(filepath.Join(rootArgs[4], "environment.yaml")); err != nil {
		os.Exit(2)
	}
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

func TestLocalRootGateSudoPromptHelperProcess(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_ROOT_GATE_SUDO_PROMPT_HELPER") != "1" {
		return
	}
	args := os.Args
	sep := slices.Index(args, "--")
	if sep < 0 {
		os.Exit(2)
	}
	sudoArgs := args[sep+1:]
	switch {
	case reflect.DeepEqual(sudoArgs, []string{"-n", "-v"}):
		os.Exit(1)
	case reflect.DeepEqual(sudoArgs, []string{"-S", "-p", "", "-v"}):
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(2)
		}
		if string(body) != "secret\n" {
			fmt.Fprintln(os.Stderr, "Sorry, try again.")
			fmt.Fprintln(os.Stderr, "sudo: no password was provided")
			fmt.Fprintln(os.Stderr, "sudo: 1 incorrect password attempt")
			os.Exit(1)
		}
		os.Exit(0)
	case reflect.DeepEqual(sudoArgs, []string{"-V"}):
		fmt.Fprintln(os.Stdout, "Authentication timestamp timeout: 4.0 minutes")
		os.Exit(0)
	case len(sudoArgs) >= 2 && sudoArgs[0] == "-n" && sudoArgs[1] == "env":
		path := localRootEnvValue(sudoArgs, localRootBecomePasswordFileEnv)
		if path != "" {
			body, err := os.ReadFile(path)
			if err != nil || string(body) != "secret\n" {
				os.Exit(2)
			}
		}
		os.Exit(0)
	default:
		os.Exit(2)
	}
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

func TestRunBootstrapPlanExplainsPythonInstallFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	fakeDnf := filepath.Join(dir, "dnf")
	if err := os.WriteFile(fakeDnf, []byte("#!/bin/sh\nprintf '%s\\n' 'no enabled repositories' >&2\nexit 42\n"), 0o755); err != nil {
		t.Fatalf("write fake dnf: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runBootstrapPlan(
		context.Background(),
		strings.NewReader("unused\n"),
		&stdout,
		&stderr,
		[]bastion.BootstrapStep{{Label: "install python3.12", Cmd: []string{fakeDnf, "install", "-y", "python3.12"}}},
		nil,
		"",
		true,
	)
	if err == nil {
		t.Fatal("expected bootstrap failure")
	}
	msg := err.Error()
	for _, want := range []string{
		"Python 3.12+ was not found",
		"dnf install failed",
		"enable or repair host package repositories",
		"install Python 3.12+ on PATH manually",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q:\n%s", want, msg)
		}
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
	plan := []bastion.BootstrapStep{
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
	if got := stdout.String(); strings.Contains(got, "$ sudo") || !strings.Contains(got, "first") || !strings.Contains(got, "second") || !strings.Contains(got, "[OK] Ansible runtime: ready") {
		t.Fatalf("stdout should hide commands and show runtime status:\n%s", got)
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
		[]bastion.BootstrapStep{{Label: "root step", Cmd: []string{"sudo", "sh", "-c", "printf ok"}}},
		nil,
		"",
		false,
	)
	if err != nil {
		t.Fatalf("runBootstrapPlan: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if got := stdout.String(); strings.Contains(got, "$ sudo") || !strings.Contains(got, "ok") || !strings.Contains(got, "[OK] Ansible runtime: ready") {
		t.Fatalf("stdout should hide commands and show runtime status:\n%s", got)
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
	spec := bastion.CLIInstallSpec{
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
		installer := installerInstallConfigPath(cf.ctx.ClustersDir, "sno-libvirt")
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
		installer := installerInstallConfigPath(cf.ctx.ClustersDir, "sno-libvirt")
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
		if _, err := render.All(cf.ctx.RenderedDir, cf.ctx.ClustersDir, t.TempDir(), state); err != nil {
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
		if _, err := render.All(cf.ctx.RenderedDir, cf.ctx.ClustersDir, t.TempDir(), state); err != nil {
			t.Fatalf("render: %v", err)
		}
		stale := state
		stale.ContainerClusters[0].Spec.Distribution.Release.Version = "4.99.0"
		data, err := yaml.Marshal(stale)
		if err != nil {
			t.Fatalf("marshal stale state: %v", err)
		}
		if err := os.WriteFile(filepath.Join(cf.ctx.RenderedDir, "effective-state.yaml"), data, 0o644); err != nil {
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
		{ID: "iso.sno-libvirt", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso sno-libvirt", Cluster: "sno-libvirt", ClusterKind: workflow.ApplyClusterKindContainer, Dependencies: []string{"provider"}},
		{ID: "boot.sno-libvirt", Kind: workflow.ApplyTaskKindNodeBoot, Label: "boot sno-libvirt nodes", Cluster: "sno-libvirt", ClusterKind: workflow.ApplyClusterKindContainer, Dependencies: []string{"iso.sno-libvirt"}},
		{ID: "wait.sno-libvirt", Kind: workflow.ApplyTaskKindInstallWait, Label: "wait install sno-libvirt", Cluster: "sno-libvirt", ClusterKind: workflow.ApplyClusterKindContainer, Dependencies: []string{"boot.sno-libvirt"}},
	}, now)
	ledger.MarkOK("provider", now.Add(time.Second))
	ledger.MarkOK("iso.sno-libvirt", now.Add(2*time.Second))
	ledger.MarkRunning("boot.sno-libvirt", "/tmp/boot.log", now.Add(3*time.Second))
	if err := workflow.SaveRunLedger(ctx.RunsDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}
	if err := workflow.SaveRunLease(ctx.RunsDir, workflow.NewRunLease("apply-test", time.Now().UTC())); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}

	stdout, stderr, code := runCLI(t, "status")
	if code != 0 {
		t.Fatalf("status exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"Current apply", "apply-test", "Progress", "Boot sno-libvirt nodes", "[RUNNING] Prepare", "bootwright status --watch"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status output missing %q:\n%s", want, stdout)
		}
	}
}

func TestStatusReportsStaleApplyLedger(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-stale", "clusters", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "iso.sno-libvirt", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso sno-libvirt", Cluster: "sno-libvirt"},
	}, now)
	if err := workflow.SaveRunLedger(ctx.RunsDir, ledger); err != nil {
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
	if err := workflow.SaveRunLedger(ctx.RunsDir, ledger); err != nil {
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
	runsDir := t.TempDir()
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-stale", "clusters", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "iso.sno-libvirt", Kind: workflow.ApplyTaskKindClusterISO, Label: "iso sno-libvirt"},
	}, now)
	if err := workflow.SaveRunLedger(runsDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}

	var stdout bytes.Buffer
	if err := reconcileCurrentApplyBeforeMutation(&stdout, runsDir); err != nil {
		t.Fatalf("reconcileCurrentApplyBeforeMutation: %v", err)
	}
	loaded, ok, err := workflow.LoadRunLedger(runsDir)
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
	runsDir := t.TempDir()
	now := time.Now().UTC()
	ledger := workflow.NewRunLedger("apply-active", "clusters", "", workflow.ConcurrencyLimits{}, nil, now)
	if err := workflow.SaveRunLedger(runsDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}
	if err := workflow.SaveRunLease(runsDir, workflow.NewRunLease("apply-active", now)); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}

	var stdout bytes.Buffer
	err := reconcileCurrentApplyBeforeMutation(&stdout, runsDir)
	if err == nil || !strings.Contains(err.Error(), "apply run apply-active is still running") {
		t.Fatalf("expected active apply error, got %v", err)
	}
	loaded, _, err := workflow.LoadRunLedger(runsDir)
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

func TestApplyContainerClusterBlocksInstallMismatchBeforeRuntimeInstallerRewrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell scripts")
	}
	ctx := initTestContext(t, "005-3nodes-baremetal")
	home := os.Getenv("HOME")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir ssh dir: %v", err)
	}
	secrets := map[string]string{
		filepath.Join(ctx.SecretsDir, "openshift-pull-secret"):     `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`,
		filepath.Join(ctx.SecretsDir, "cluster-admin-ssh-key"):     "fake-private-key\n",
		filepath.Join(ctx.SecretsDir, "cluster-admin-ssh-key.pub"): "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForApplyTest\n",
		filepath.Join(ctx.SecretsDir, "bmc-credentials"):           "admin:password\n",
		filepath.Join(ctx.SecretsDir, "proxy-credentials"):         "proxy:password\n",
		filepath.Join(sshDir, "bootwright-ssh-key"):                "fake-private-key\n",
		filepath.Join(sshDir, "bootwright-ssh-key.pub"):            "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForApplyTest\n",
	}
	for path, body := range secrets {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	fakeBin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	fakeAnsible := filepath.Join(fakeBin, "ansible-playbook")
	for name, body := range map[string]string{
		"python3":          "#!/bin/sh\nexit 0\n",
		"ansible-playbook": "#!/bin/sh\nprintf '%s\\n' 'ansible should not run' >&2\nexit 33\n",
	} {
		path := filepath.Join(fakeBin, name)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	clusterName := "3-nodes-ocp-baremetal"
	if err := workflow.SaveClusterInstallRecord(ctx.ClustersDir, workflow.ClusterInstallRecord{
		Cluster:     clusterName,
		DesiredHash: "sha256:old",
		Status:      workflow.ClusterInstallStatusInstalling,
		Phase:       workflow.ClusterInstallPhaseNodesBooted,
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	installConfig := filepath.Join(ctx.ClustersDir, clusterName, "runtime", render.RuntimeRelativeDir, "install-config.yaml")
	if err := os.MkdirAll(filepath.Dir(installConfig), 0o700); err != nil {
		t.Fatalf("mkdir runtime installer dir: %v", err)
	}
	const sentinel = "sentinel runtime installer input\n"
	if err := os.WriteFile(installConfig, []byte(sentinel), 0o600); err != nil {
		t.Fatalf("write sentinel install-config: %v", err)
	}

	stdout, stderr, code := runCLI(t, "apply", "container-cluster", "--yes", "--ask-become-pass=false", "--ansible-playbook", fakeAnsible)
	if code == 0 {
		t.Fatalf("apply container-cluster unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "different install inputs") {
		t.Fatalf("apply container-cluster stderr missing install mismatch:\n%s", stderr)
	}
	data, err := os.ReadFile(installConfig)
	if err != nil {
		t.Fatalf("read sentinel install-config: %v", err)
	}
	if string(data) != sentinel {
		t.Fatalf("runtime install-config was rewritten before install-state reconciliation:\n%s", data)
	}
	if strings.Contains(stdout, "Resolve installer") || strings.Contains(stderr, "ansible should not run") {
		t.Fatalf("apply progressed past install-state reconciliation\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestApplyDryRunJSONIncludesParallelNodeBootTasks(t *testing.T) {
	initTestContext(t, "005-3nodes-baremetal")
	stdout, stderr, code := runCLI(t, "apply", "container-cluster", "--dry-run", "--output", "json", "--ask-become-pass=true")
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
	if bootTask.ClusterLogPath == "" || !strings.Contains(bootTask.ClusterLogPath, filepath.Join("clusters", "3-nodes-ocp-baremetal", "runs", "dry-run", "bootwright.log")) {
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

func TestApplyAddonsDryRunJSONPlansAddonTasks(t *testing.T) {
	initTestContextWithClusterAddon(t)

	stdout, stderr, code := runCLI(t, "apply", "addons", "--dry-run", "--output", "json")
	if code != 0 {
		t.Fatalf("apply addons dry-run json exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode apply dry-run json: %v\n%s", err, stdout)
	}
	if report.Target != "addons" {
		t.Fatalf("target = %q, want addons", report.Target)
	}
	if report.ApplyPlan == nil {
		t.Fatalf("apply plan missing from report: %+v", report)
	}
	gotIDs := make([]string, 0, len(report.ApplyPlan.Tasks))
	for _, task := range report.ApplyPlan.Tasks {
		gotIDs = append(gotIDs, task.ID)
	}
	wantIDs := []string{
		"addon.sno-libvirt.openshift-virtualization.apply",
		"addon.sno-libvirt.openshift-virtualization.wait",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("addon task IDs = %v, want %v", gotIDs, wantIDs)
	}
	if got := report.ApplyPlan.Addons; len(got) != 1 || got[0].Cluster != "sno-libvirt" || got[0].Addon != "openshift-virtualization" {
		t.Fatalf("addon plan = %+v, want sno-libvirt openshift-virtualization", got)
	}
	if got := report.ApplyPlan.Addons[0].Resources; len(got) != 3 || got[2].Kind != "HyperConverged" {
		t.Fatalf("addon resources = %+v, want generated OLM resources ending with HyperConverged", got)
	}
}

func TestApplyClustersDryRunJSONPlansAddonTasks(t *testing.T) {
	initTestContextWithClusterAddon(t)

	stdout, stderr, code := runCLI(t, "apply", "clusters", "--dry-run", "--output", "json")
	if code != 0 {
		t.Fatalf("apply clusters dry-run json exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode apply dry-run json: %v\n%s", err, stdout)
	}
	if report.Target != "clusters" {
		t.Fatalf("target = %q, want clusters", report.Target)
	}
	if report.ApplyPlan == nil {
		t.Fatalf("apply plan missing from report: %+v", report)
	}
	gotIDs := make([]string, 0, len(report.ApplyPlan.Tasks))
	for _, task := range report.ApplyPlan.Tasks {
		gotIDs = append(gotIDs, task.ID)
	}
	wantIDs := []string{
		"infra.sno-libvirt.lab-host",
		"iso.sno-libvirt",
		"boot.sno-libvirt",
		"wait.sno-libvirt",
		"addon.sno-libvirt.openshift-virtualization.apply",
		"addon.sno-libvirt.openshift-virtualization.wait",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("apply clusters task IDs = %v, want %v", gotIDs, wantIDs)
	}
	if got := report.ApplyPlan.Addons; len(got) != 1 || got[0].Cluster != "sno-libvirt" || got[0].Addon != "openshift-virtualization" {
		t.Fatalf("addon plan = %+v, want sno-libvirt openshift-virtualization", got)
	}
	if got := report.ApplyPlan.Addons[0].Resources; len(got) != 3 || got[2].Kind != "HyperConverged" {
		t.Fatalf("addon resources = %+v, want generated OLM resources ending with HyperConverged", got)
	}
	assertTaskDeps(t, report.ApplyPlan.Tasks, "iso.sno-libvirt", "infra.sno-libvirt.lab-host")
	assertTaskDeps(t, report.ApplyPlan.Tasks, "addon.sno-libvirt.openshift-virtualization.apply", "wait.sno-libvirt")
	assertTaskDeps(t, report.ApplyPlan.Tasks, "addon.sno-libvirt.openshift-virtualization.wait", "addon.sno-libvirt.openshift-virtualization.apply")
}

func TestApplyClustersOverrideDryRunPassesInstallOverride(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "apply", "clusters", "--dry-run", "--output", "json", "--override")
	if code != 0 {
		t.Fatalf("apply clusters override dry-run exited %d, stderr=%q", code, stderr)
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
	ledger := workflow.NewRunLedger("apply-stale-watch", "clusters", "", workflow.ConcurrencyLimits{}, nil, time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC))
	if err := workflow.SaveRunLedger(ctx.RunsDir, ledger); err != nil {
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

func lockTestContextsDir(t *testing.T, root string) {
	t.Helper()
	contextsDir := filepath.Join(root, "contexts")
	if err := os.MkdirAll(contextsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(contextsDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(contextsDir, 0o700) })
}

func saveTestContextRegistry(t *testing.T, current string, names ...string) {
	t.Helper()
	registry, err := contextstore.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := contextstore.Save(registry, contextstore.Store{Current: current, Contexts: names}); err != nil {
		t.Fatal(err)
	}
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "test", "e2e", name)
}

func initTestContextWithClusterAddon(t *testing.T) {
	t.Helper()
	setTestHomeAndRoot(t)
	inputDir := copyFixtureYAML(t, "001-sno-libvirt")
	if err := os.WriteFile(filepath.Join(inputDir, "cluster-addon.yaml"), []byte(cliTestClusterAddonYAML()), 0o600); err != nil {
		t.Fatalf("write addon: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "cluster-addon-profile.yaml"), []byte(`apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonProfile
metadata:
  name: virtualization-platform
spec:
  addons:
    - name: openshift-virtualization
`), 0o600); err != nil {
		t.Fatalf("write addon profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "cluster-addon-binding.yaml"), []byte(`apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: sno-libvirt-addons
spec:
  clusterRef:
    name: sno-libvirt
  addonProfiles:
    - name: virtualization-platform
`), 0o600); err != nil {
		t.Fatalf("write addon binding: %v", err)
	}
	if stdout, stderr, code := runCLI(t, "context", "init", "test", "-f", inputDir); code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func cliTestClusterAddonYAML() string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: openshift-virtualization
spec:
  type: olm-operator
  olm:
    namespace:
      name: openshift-cnv
      create: true
    subscription:
      name: hco-operatorhub
      package: kubevirt-hyperconverged
      channel: stable
      source: redhat-operators
      sourceNamespace: openshift-marketplace
      installPlanApproval: Automatic
    customResources:
      - apiVersion: hco.kubevirt.io/v1beta1
        kind: HyperConverged
        metadata:
          name: kubevirt-hyperconverged
          namespace: openshift-cnv
        spec: {}
  readiness:
    checks:
      - type: csvSucceeded
        namespace: openshift-cnv
        subscription: hco-operatorhub
`
}

func assertTaskDeps(t *testing.T, tasks []workflow.TaskLedgerEntry, id string, want ...string) {
	t.Helper()
	for _, task := range tasks {
		if task.ID != id {
			continue
		}
		if !reflect.DeepEqual(task.Dependencies, want) {
			t.Fatalf("%s deps = %v, want %v", id, task.Dependencies, want)
		}
		return
	}
	t.Fatalf("task %s not found", id)
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

func localRootCommandIndex(t *testing.T, args []string, home, auth string) int {
	t.Helper()
	if len(args) < 8 || args[0] != "-n" || args[1] != "env" || !strings.HasPrefix(args[2], contextstore.InternalRegistryEnv+"=") || args[3] != localroot.InternalEnv+"=1" || args[4] != secret.InternalCallerHomeEnv+"="+home || args[5] != localroot.CallerPathEnv+"="+os.Getenv("PATH") || args[6] != localRootSudoAuthEnv+"="+auth {
		t.Fatalf("local root args prefix = %v", args)
	}
	index := 7
	if index < len(args) && strings.HasPrefix(args[index], localRootBecomePasswordFileEnv+"=") {
		index++
	}
	if index >= len(args) {
		t.Fatalf("local root args missing executable: %v", args)
	}
	return index
}

func localRootEnvValue(args []string, key string) string {
	prefix := key + "="
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
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
