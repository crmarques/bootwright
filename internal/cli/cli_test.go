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
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/bastion"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/localroot"
	"github.com/crmarques/bootwright/internal/infra/media"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	"github.com/crmarques/bootwright/internal/state/advice"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/state/scaffold"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestMain(m *testing.M) {
	localRootGate.enabled = false
	os.Exit(m.Run())
}

func TestHubCommandsNotAdvertised(t *testing.T) {
	for _, args := range [][]string{
		{"preflight", "--help"},
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

	_, stderr, code := runCLI(t, "preflight", "hub")
	if code == 0 {
		t.Fatal("bootwright preflight hub unexpectedly succeeded")
	}
	if !strings.Contains(stderr, `invalid argument "hub"`) {
		t.Fatalf("preflight hub stderr %q does not reject hub as an invalid target", stderr)
	}

	_, stderr, code = runCLI(t, "apply", "hub")
	if code == 0 {
		t.Fatal("bootwright apply hub unexpectedly succeeded")
	}
	if !strings.Contains(stderr, `unknown command "hub"`) {
		t.Fatalf("apply hub stderr %q does not reject hub as an invalid target", stderr)
	}
}

func TestClusterTargets(t *testing.T) {
	for _, args := range [][]string{
		{"preflight", "clusters", "--help"},
		{"preflight", "container-cluster", "--help"},
		{"destroy", "--help"},
		{"apply", "--help"},
		{"bastion", "setup", "--help"},
	} {
		_, stderr, code := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("bootwright %s exited %d, stderr=%q", strings.Join(args, " "), code, stderr)
		}
	}

	for _, args := range [][]string{
		{"preflight", "cluster"},
		{"apply", "cluster"},
		{"destroy", "clusters"},
		{"destroy", "infra"},
		{"destroy", "container-cluster"},
	} {
		_, stderr, code := runCLI(t, args...)
		if code == 0 {
			t.Fatalf("bootwright %s unexpectedly succeeded", strings.Join(args, " "))
		}
		if !strings.Contains(stderr, `invalid argument`) && !strings.Contains(stderr, `unknown command`) {
			t.Fatalf("%s stderr %q does not reject unsupported target", strings.Join(args, " "), stderr)
		}
	}
	for _, target := range []string{"bastion", "infra", "clusters", "container-cluster", "storage-cluster", "add-ons", "all"} {
		_, stderr, code := runCLI(t, "apply", target)
		if code == 0 {
			t.Fatalf("bootwright apply %s unexpectedly succeeded", target)
		}
		if !strings.Contains(stderr, `unknown command "`+target+`"`) {
			t.Fatalf("apply %s stderr %q does not reject removed subcommand", target, stderr)
		}
	}

	_, stderr, code := runCLI(t, "destroy", "cluster")
	if code == 0 {
		t.Fatal("bootwright destroy cluster unexpectedly succeeded")
	}
	if !strings.Contains(stderr, `unknown command "cluster"`) {
		t.Fatalf("destroy cluster stderr %q does not reject generic destroy target", stderr)
	}
}

func TestBastionGroupExposesSetup(t *testing.T) {
	stdout, stderr, code := runCLI(t, "bastion", "--help")
	if code != 0 {
		t.Fatalf("bastion --help exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "setup") {
		t.Fatalf("bastion --help missing subcommand %q:\n%s", "setup", stdout)
	}
	if _, _, code = runCLI(t, "bastion", "check"); code == 0 {
		t.Fatalf("bastion check should be removed in favor of preflight bastion, got exit 0:\n%s", stdout)
	}
}

func TestApplyHelpMatchesTargetExecutionModels(t *testing.T) {
	stdout, stderr, code := runCLI(t, "apply", "--help")
	if code != 0 {
		t.Fatalf("apply --help exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"--stage", "--through", "infra|clusters", "--clusters", "ContainerCluster or StorageCluster"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("apply help missing %q:\n%s", want, stdout)
		}
	}
	for _, want := range []string{"--converge-drifted", "reinstall", "wipe-and-rebuild"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("apply --converge-drifted help must name its destructive scope, missing %q:\n%s", want, stdout)
		}
	}
	for _, reject := range []string{"--scope ", "--cluster ", "--scoped-validation", "--check", "--stream-ansible", "--ansible-playbook", "bastion", "container|storage|install|addons", "Subcommand Flags"} {
		if strings.Contains(stdout, reject) {
			t.Fatalf("apply help exposes removed flag or help section %q:\n%s", reject, stdout)
		}
	}

	stdout, stderr, code = runCLI(t, "preflight", "storage-cluster", "--help")
	if code != 0 {
		t.Fatalf("preflight storage-cluster --help exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"comma-separated StorageCluster names to preflight", "bootwright preflight storage-cluster --clusters ceph-storage"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("preflight storage-cluster help missing %q:\n%s", want, stdout)
		}
	}
}

func TestDestroyHelpMatchesTargetExecutionModels(t *testing.T) {
	stdout, stderr, code := runCLI(t, "destroy", "--help")
	if code != 0 {
		t.Fatalf("destroy --help exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"--stage", "infra|clusters", "--clusters", "ContainerCluster or StorageCluster names"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("destroy help missing %q:\n%s", want, stdout)
		}
	}
	for _, reject := range []string{"--scope ", "--cluster ", "Subcommand Flags"} {
		if strings.Contains(stdout, reject) {
			t.Fatalf("destroy help exposes removed flag or help section %q:\n%s", reject, stdout)
		}
	}
}

func TestApplyRejectsRemovedStagesAndFlags(t *testing.T) {
	for _, stage := range []string{"container", "storage", "install", "bogus"} {
		_, stderr, code := runCLI(t, "apply", "--stage", stage, "--dry-run")
		if code == 0 {
			t.Fatalf("apply --stage %s unexpectedly succeeded", stage)
		}
		if !strings.Contains(stderr, "--stage must be one of infra, clusters") {
			t.Fatalf("apply --stage %s stderr = %q", stage, stderr)
		}
	}
	for _, flag := range []string{"--scope", "--cluster"} {
		stdout, stderr, code := runCLI(t, "apply", flag, "sno-libvirt", "--dry-run")
		if code == 0 {
			t.Fatalf("apply %s unexpectedly succeeded", flag)
		}
		if !strings.Contains(stdout+stderr, "unknown flag: "+flag) {
			t.Fatalf("apply %s did not reject old flag, stdout=%q stderr=%q", flag, stdout, stderr)
		}
	}
}

func TestDestroyRejectsRemovedStagesAndFlags(t *testing.T) {
	for _, stage := range []string{"container", "storage", "install", "add-ons"} {
		_, stderr, code := runCLI(t, "destroy", "--stage", stage, "--dry-run")
		if code == 0 {
			t.Fatalf("destroy --stage %s unexpectedly succeeded", stage)
		}
		if !strings.Contains(stderr, "--stage must be one of infra, clusters") {
			t.Fatalf("destroy --stage %s stderr = %q", stage, stderr)
		}
	}
	for _, flag := range []string{"--scope", "--cluster"} {
		stdout, stderr, code := runCLI(t, "destroy", flag, "sno-libvirt", "--dry-run")
		if code == 0 {
			t.Fatalf("destroy %s unexpectedly succeeded", flag)
		}
		if !strings.Contains(stdout+stderr, "unknown flag: "+flag) {
			t.Fatalf("destroy %s did not reject old flag, stdout=%q stderr=%q", flag, stdout, stderr)
		}
	}
}

func TestStageRejectionMessagesListCanonicalVocabulary(t *testing.T) {
	_, applyErr, applyCode := runCLI(t, "apply", "--stage", "bogus", "--dry-run")
	if applyCode != 2 || !strings.Contains(applyErr, "--stage must be one of infra, clusters, fabric, machines, deps, base, add-ons") {
		t.Fatalf("apply --stage bogus code=%d stderr=%q, want full apply vocabulary", applyCode, applyErr)
	}
	_, destroyErr, destroyCode := runCLI(t, "destroy", "--stage", "bogus", "--dry-run")
	if destroyCode != 2 || !strings.Contains(destroyErr, "--stage must be one of infra, clusters (sub-phases fabric, machines, deps, base, add-ons are apply-only)") {
		t.Fatalf("destroy --stage bogus code=%d stderr=%q, want family list + apply-only note", destroyCode, destroyErr)
	}
	_, throughErr, throughCode := runCLI(t, "apply", "--through", "bogus", "--dry-run")
	if throughCode != 2 || !strings.Contains(throughErr, "--through must be one of infra, clusters, fabric, machines, deps, base, add-ons") {
		t.Fatalf("apply --through bogus code=%d stderr=%q, want full through vocabulary", throughCode, throughErr)
	}
}

func TestApplyStageThroughRunsMidGraphRange(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "apply", "--stage", "deps", "--through", "base", "--dry-run", "--ask-become-pass=false")
	if code != 0 {
		t.Fatalf("apply --stage deps --through base --dry-run exited %d, want 0; stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "phases not in this plan: fabric, machines, add-ons") {
		t.Fatalf("mid-graph range deps..base should omit fabric, machines, add-ons:\n%s", stdout)
	}
	if !strings.Contains(stdout, "assumes a prior apply completed: fabric, machines") {
		t.Fatalf("mid-graph range starting at deps should warn about assumed prior phases:\n%s", stdout)
	}
}

func TestApplyThroughEndRunsFullGraph(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "apply", "--through", "end", "--dry-run", "--ask-become-pass=false")
	if code != 0 {
		t.Fatalf("apply --through end --dry-run exited %d, want 0; stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "phases not in this plan") {
		t.Fatalf("apply --through end runs the full graph and should omit no phases:\n%s", stdout)
	}
}

func TestApplyStageAfterThroughRejected(t *testing.T) {
	_, stderr, code := runCLI(t, "apply", "--stage", "clusters", "--through", "infra", "--dry-run")
	if code != 2 {
		t.Fatalf("apply --stage clusters --through infra exited %d, want 2; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "starts after") {
		t.Fatalf("apply --stage clusters --through infra stderr = %q, want start-after-end message", stderr)
	}
}

func TestApplyThroughBaseDryRunReportsTrailingOmissionsWithoutPriorWarning(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "apply", "--through", "base", "--dry-run", "--ask-become-pass=false")
	if code != 0 {
		t.Fatalf("apply --through base --dry-run exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "phases not in this plan: add-ons") {
		t.Fatalf("apply --through base dry-run should report only add-ons omitted:\n%s", stdout)
	}
	if strings.Contains(stdout, "assumes a prior apply completed") {
		t.Fatalf("apply --through base dry-run must not warn about assumed prior phases:\n%s", stdout)
	}
}

func TestDiffHelpDocumentsThrough(t *testing.T) {
	stdout, stderr, code := runCLI(t, "diff", "--help")
	if code != 0 {
		t.Fatalf("diff --help exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"--stage", "--through", "infra|clusters"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("diff help missing %q:\n%s", want, stdout)
		}
	}
}

func TestDestroyHelpUsesArtifactServerScope(t *testing.T) {
	stdout, stderr, code := runCLI(t, "destroy", "--help")
	if code != 0 {
		t.Fatalf("destroy --help exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "artifact-server") {
		t.Fatalf("destroy help missing artifact-server scope on --clusters:\n%s", stdout)
	}
	if strings.Contains(stdout, "http-server") {
		t.Fatalf("destroy help still exposes http-server scope:\n%s", stdout)
	}
}

func TestValidateJSON(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "validate", "--output", "json")
	if code != 0 {
		t.Fatalf("validate exited %d, stderr=%q", code, stderr)
	}
	var report syntaxCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if !report.OK {
		t.Fatalf("expected ok report, got %+v", report)
	}
	if report.ContainerClusters != 1 || report.Machines == 0 || report.InfraProviders != 1 {
		t.Fatalf("unexpected object counts: %+v", report)
	}
	for _, reject := range []string{"Bootwright:", "[OK]", "Summary"} {
		if strings.Contains(stdout, reject) {
			t.Fatalf("json output contains human decoration %q:\n%s", reject, stdout)
		}
	}
}

func TestValidateValidatesInputFilesWithoutContext(t *testing.T) {
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

func TestValidateReportsEnvironmentExcludedClusters(t *testing.T) {
	setTestHomeAndRoot(t)
	inputDir := copyFixtureYAML(t, "001-sno-libvirt")
	environmentPath := filepath.Join(inputDir, "environment.yaml")
	environment, err := os.ReadFile(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	selection := append(environment, []byte("  containerClusters:\n    - sno-libvirt\n")...)
	if err := os.WriteFile(environmentPath, selection, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, "validate", "-f", inputDir)
	if code != 0 {
		t.Fatalf("validate exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.Contains(stdout, "Environment selection") {
		t.Fatalf("selection covering every cluster still warns:\n%s", stdout)
	}

	ghost := "apiVersion: bootwright.io/v1alpha1\nkind: StorageCluster\nmetadata:\n  name: ghost-ceph\nspec:\n  type: ceph\n"
	if err := os.WriteFile(filepath.Join(inputDir, "ghost-storage-cluster.yaml"), []byte(ghost), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, "validate", "-f", inputDir)
	if code != 0 {
		t.Fatalf("validate with excluded cluster exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, want := range []string{
		"Environment selection",
		"[WARN] StorageCluster/ghost-ceph: loaded but excluded by Environment cluster selection",
		`add "ghost-ceph" to Environment spec.storageClusters or remove its YAML`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("validate output missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI(t, "validate", "-f", inputDir, "--output", "json")
	if code != 0 {
		t.Fatalf("validate --output json exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	var report syntaxCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if !report.OK || report.StorageClusters != 0 {
		t.Fatalf("unexpected validate report: %+v", report)
	}
	if !slices.Equal(report.ExcludedStorageClusters, []string{"ghost-ceph"}) || len(report.ExcludedContainerClusters) != 0 {
		t.Fatalf("unexpected excluded clusters in report: %+v", report)
	}
}

func TestValidateNoticesStretchPoolInheritance(t *testing.T) {
	setTestHomeAndRoot(t)

	stdout, stderr, code := runCLI(t, "validate", "-f", fixturePath("001-sno-libvirt"))
	if code != 0 {
		t.Fatalf("validate exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.Contains(stdout, "Stretch pools") {
		t.Fatalf("stretch-less input still notices stretch pools:\n%s", stdout)
	}

	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	stdout, stderr, code = runCLI(t, "validate", "-f", example)
	if code != 0 {
		t.Fatalf("validate exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, want := range []string{
		"Stretch pools",
		"[INFO] StorageCluster/ceph-storage: policy-less pools inherit the stretch rule and size 4/minSize 2: ",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("validate output missing %q:\n%s", want, stdout)
		}
	}
	for _, pool := range []string{"odf-rbd", "odf-cephfs-metadata", "odf-cephfs-data", "odf-rgw"} {
		if !strings.Contains(stdout, pool) {
			t.Fatalf("stretch pool notice missing pool %q:\n%s", pool, stdout)
		}
	}

	stdout, stderr, code = runCLI(t, "validate", "-f", example, "--output", "json")
	if code != 0 {
		t.Fatalf("validate --output json exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	var report syntaxCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	var stretchNotice *advice.StorageAdvisory
	for i, a := range report.Advisories {
		if a.Group == "Stretch pools" {
			stretchNotice = &report.Advisories[i]
			break
		}
	}
	if stretchNotice == nil {
		t.Fatalf("validate --output json missing the stretch-pool advisory:\n%+v", report.Advisories)
	}
	if stretchNotice.Severity != advice.SeverityInfo {
		t.Fatalf("stretch-pool advisory must be INFO in json, got %q", stretchNotice.Severity)
	}
	if !strings.Contains(stretchNotice.Finding, "policy-less pools inherit the stretch rule") {
		t.Fatalf("stretch-pool advisory finding unexpected: %q", stretchNotice.Finding)
	}
}

func TestValidateReportsDeclaredSecretStatus(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")

	stdout, stderr, code := runCLI(t, "validate")
	if code != 0 {
		t.Fatalf("validate exited %d, stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"Declared secrets",
		"[WARN] openshift-pull-secret",
		"bootwright secret set --name openshift-pull-secret",
		"[OK] validate",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("validate output missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI(t, "validate", "-f", fixturePath("001-sno-libvirt"))
	if code != 0 {
		t.Fatalf("validate -f exited %d, stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "Declared secrets") {
		t.Fatalf("validate -f without a context still reports declared secrets:\n%s", stdout)
	}
}

func TestDispatcherCompletionListsSubcommandsOnce(t *testing.T) {
	setTestHomeAndRoot(t)
	for _, parent := range []string{"context", "render", "bastion"} {
		stdout, stderr, code := runCLI(t, cobra.ShellCompRequestCmd, parent, "")
		if code != 0 {
			t.Fatalf("__complete %s exited %d, stderr=%q\nstdout:\n%s", parent, code, stderr, stdout)
		}
		seen := map[string]int{}
		for _, line := range strings.Split(stdout, "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			name := line
			desc := ""
			if i := strings.IndexByte(line, '\t'); i >= 0 {
				name, desc = line[:i], line[i+1:]
			}
			seen[name]++
			if desc == "" {
				t.Fatalf("%s completion %q has no description (bare ValidArgs duplicate?):\n%s", parent, name, stdout)
			}
		}
		if len(seen) == 0 {
			t.Fatalf("%s completion produced no subcommands:\n%s", parent, stdout)
		}
		for name, n := range seen {
			if n != 1 {
				t.Fatalf("%s completion lists %q %d times, want once:\n%s", parent, name, n, stdout)
			}
		}
	}
}

func TestContextInitOutputIsConcise(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"initialized and set as the current context",
		"Ansible bundle",
		ctx.InputDir,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("context init output missing %q:\n%s", want, stdout)
		}
	}
	for _, reject := range []string{"managed-services-dir", "provider-state-dir", "ownership-dir"} {
		if strings.Contains(stdout, reject) {
			t.Fatalf("context init still dumps the %q path layout:\n%s", reject, stdout)
		}
	}
}

func TestValidateJSONFailureIncludesDiagnostics(t *testing.T) {
	setTestHomeAndRoot(t)
	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "environment.yaml"), []byte("apiVersion: bootwright/v1alpha1\nkind: Environment\nmetadata: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, "validate", "-f", inputDir, "--output", "json")
	if code != 1 {
		t.Fatalf("validate -f on invalid input exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("validate json failure wrote stderr: %q", stderr)
	}
	var report syntaxCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.OK || report.Error == "" || len(report.Diagnostics) == 0 {
		t.Fatalf("expected diagnostics in failure report: %+v", report)
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

	stdout, stderr, code := runCLI(t, "validate", "--output", "json")
	if code == 0 {
		t.Fatalf("validate unexpectedly succeeded: %s", stdout)
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
			name: "validate explicit input",
			args: []string{"validate", "-f", fixturePath("001-sno-libvirt")},
			want: []string{"Bootwright: validate", "Objects", "Desired state", "[OK] validate"},
		},
		{
			name: "status",
			args: []string{"status"},
			want: []string{"Bootwright: status", "Context", "Desired state", "Clusters", "Next steps"},
		},
		{
			name: "render installer",
			args: []string{"render", "installer", "--clusters", "sno-libvirt"},
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
			want: []string{"Bootwright: plan", "Apply plan", "planned task(s)", "Rendered artifacts", "Bundle"},
		},
		{
			name: "apply stage infra dry-run",
			args: []string{"apply", "--stage", "infra", "--dry-run", "--ask-become-pass=false"},
			want: []string{"Bootwright: infra apply", "Apply plan", "planned task(s)", "Infrastructure", "Shared services", "Rendered artifacts", "Bundle"},
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

func TestJourneyCommandsRouteToStatus(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "secret", "generate")
	if code != 0 {
		t.Fatalf("secret generate exited %d (want 0; missing context secrets do not fail generate), stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	for _, want := range []string{"request(s) handled", "Needs secret set", "Next steps", "bootwright status"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("secret generate stdout missing %q (journey commands must route back to the status hub):\n%s", want, stdout)
		}
	}
}

func TestFailedCheckOutputIsActionable(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "preflight", "infra", "--dry-run")
	if code == 0 {
		t.Fatalf("preflight infra should fail with missing local secrets in test context:\n%s", stdout)
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
	if !strings.Contains(stderr, "machine check failed") {
		t.Fatalf("stderr missing machine check failure:\n%s", stderr)
	}
}

func TestScopedApplyDryRunJSON(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"apply",
		"--stage", "infra",
		"--clusters", "sno-libvirt",
		"--dry-run",
		"--output", "json",
	)
	if code != 0 {
		t.Fatalf("apply --stage infra dry-run exited %d, stderr=%q", code, stderr)
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

func TestApplyThroughWithClusterScopeDryRunJSON(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"apply",
		"--through", "deps",
		"--clusters", "sno-libvirt",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("apply --through deps --clusters dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Target != "through-deps" || report.Action != "apply" || !report.DryRun {
		t.Fatalf("unexpected dry-run report header: %+v", report)
	}
	for _, omitted := range []string{"base", "add-ons"} {
		if slices.Contains(report.Phases, omitted) {
			t.Fatalf("through-deps plan unexpectedly includes %q: %#v", omitted, report.Phases)
		}
	}
	if !slices.Contains(report.Phases, "deps") {
		t.Fatalf("through-deps plan missing deps phase: %#v", report.Phases)
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

func TestDestroyStageInfraArtifactServerScopeDryRunJSON(t *testing.T) {
	initTestContext(t, "002-sno-emul-baremetal")
	stdout, stderr, code := runCLI(t,
		"destroy",
		"--stage", "infra",
		"--clusters", "artifact-server",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy --stage infra artifact-server dry-run exited %d, stderr=%q", code, stderr)
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

func TestDestroyStageInfraDryRunJSONEnablesContextSweepOnlyWhenUnscoped(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"destroy",
		"--stage", "infra",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy --stage infra dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Target != "infra" || report.Action != "destroy" || !report.DryRun {
		t.Fatalf("unexpected dry-run report header: %+v", report)
	}
	if !slices.Contains(report.ExtraVars, "bootwright_infra_destroy_context_sweep=true") {
		t.Fatalf("unscoped infra destroy must enable context sweep: %#v", report.ExtraVars)
	}

	stdout, stderr, code = runCLI(t,
		"destroy",
		"--stage", "infra",
		"--clusters", "sno-libvirt",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("scoped destroy --stage infra dry-run exited %d, stderr=%q", code, stderr)
	}
	report = scopeDryRunReport{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if slices.Contains(report.ExtraVars, "bootwright_infra_destroy_context_sweep=true") {
		t.Fatalf("scoped infra destroy must not enable context sweep: %#v", report.ExtraVars)
	}
	if !slices.Contains(report.ExtraVars, "bootwright_destroy_cluster_scope=sno-libvirt") {
		t.Fatalf("scoped infra destroy must scope recorded-resource cleanup to selected roots: %#v", report.ExtraVars)
	}
}

func TestDestroyForceUnownedEmitsExtraVar(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")

	help, stderr, code := runCLI(t, "destroy", "--help")
	if code != 0 {
		t.Fatalf("destroy --help exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(help, "--include-unowned") {
		t.Fatalf("destroy help must document --include-unowned:\n%s", help)
	}

	stdout, stderr, code := runCLI(t,
		"destroy",
		"--stage", "infra",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy --stage infra dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if slices.Contains(report.ExtraVars, "bootwright_destroy_force_unowned=true") {
		t.Fatalf("destroy without --include-unowned must not emit the force extra-var: %#v", report.ExtraVars)
	}

	stdout, stderr, code = runCLI(t,
		"destroy",
		"--stage", "infra",
		"--include-unowned",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy --stage infra --include-unowned dry-run exited %d, stderr=%q", code, stderr)
	}
	report = scopeDryRunReport{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if !slices.Contains(report.ExtraVars, "bootwright_destroy_force_unowned=true") {
		t.Fatalf("destroy --include-unowned must emit the force extra-var: %#v", report.ExtraVars)
	}
}

func TestDestroySkipUnreachableRequiresOverrideAndEmitsExtraVar(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")

	help, stderr, code := runCLI(t, "destroy", "--help")
	if code != 0 {
		t.Fatalf("destroy --help exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(help, "--skip-unreachable") {
		t.Fatalf("destroy help must document --skip-unreachable:\n%s", help)
	}

	_, stderr, code = runCLI(t,
		"destroy",
		"--stage", "infra",
		"--skip-unreachable",
		"--dry-run",
		"--ask-become-pass=false",
	)
	if code != 2 {
		t.Fatalf("destroy --skip-unreachable without --force must exit 2, got %d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "--skip-unreachable requires --force") {
		t.Fatalf("destroy --skip-unreachable without --force must explain the requirement, got stderr=%q", stderr)
	}

	stdout, stderr, code := runCLI(t,
		"destroy",
		"--stage", "infra",
		"--force",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy --stage infra --force dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if slices.Contains(report.ExtraVars, "bootwright_destroy_skip_unreachable=true") {
		t.Fatalf("destroy without --skip-unreachable must not emit the skip extra-var: %#v", report.ExtraVars)
	}

	stdout, stderr, code = runCLI(t,
		"destroy",
		"--stage", "infra",
		"--skip-unreachable",
		"--force",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy --skip-unreachable --force dry-run exited %d, stderr=%q", code, stderr)
	}
	report = scopeDryRunReport{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if !slices.Contains(report.ExtraVars, "bootwright_destroy_skip_unreachable=true") {
		t.Fatalf("destroy --skip-unreachable --force must emit the skip extra-var: %#v", report.ExtraVars)
	}
}

func TestDestroyHelpDocumentsPurgeHistory(t *testing.T) {
	help, stderr, code := runCLI(t, "destroy", "--help")
	if code != 0 {
		t.Fatalf("destroy --help exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(help, "--purge-history") {
		t.Fatalf("destroy help must document --purge-history:\n%s", help)
	}
}

func TestDestroyPurgeHistoryRejectsArtifactServerScope(t *testing.T) {
	initTestContext(t, "002-sno-emul-baremetal")
	_, stderr, code := runCLI(t,
		"destroy",
		"--stage", "infra",
		"--clusters", "artifact-server",
		"--purge-history",
		"--dry-run",
		"--ask-become-pass=false",
	)
	if code != 2 {
		t.Fatalf("destroy --purge-history --clusters artifact-server exited %d, want 2; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "--purge-history") {
		t.Fatalf("destroy --purge-history --clusters artifact-server stderr = %q, want a --purge-history explanation", stderr)
	}
}

func TestDestroyPurgeHistoryAcceptedWithClusterScopeDryRun(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	_, stderr, code := runCLI(t,
		"destroy",
		"--purge-history",
		"--dry-run",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy --purge-history --dry-run exited %d, stderr=%q", code, stderr)
	}
}

func TestDestroyFullDryRunJSONPlansDependencySafeLifecycle(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"destroy",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy full dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Target != "all" || report.Action != "destroy" || !report.DryRun {
		t.Fatalf("unexpected dry-run report header: %+v", report)
	}
	if report.Playbook != "" {
		t.Fatalf("full destroy has no single playbook, got %q", report.Playbook)
	}
	if report.DestroyPlan == nil {
		t.Fatalf("full destroy dry-run report missing destroy plan: %+v", report)
	}
	wantIDs := []string{
		"destroy.storage-clusters",
		"destroy.machine-registration",
		"destroy.infra-components",
		"destroy.machine-infra",
		"destroy.container-clusters",
		"destroy.provider-services",
		"destroy.storage-node-access",
	}
	gotIDs := make([]string, 0, len(report.DestroyPlan.Tasks))
	for _, task := range report.DestroyPlan.Tasks {
		gotIDs = append(gotIDs, task.ID)
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("destroy plan task order = %v, want %v", gotIDs, wantIDs)
	}
	if !slices.Contains(report.ExtraVars, "bootwright_infra_destroy_context_sweep=true") {
		t.Fatalf("full destroy must enable the context sweep: %#v", report.ExtraVars)
	}
}

func TestDestroyFullDryRunTextSucceeds(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"destroy",
		"--dry-run",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy full dry-run (text) exited %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stdout+stderr, "--stage must be one of") {
		t.Fatalf("full destroy must not demand a stage:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestDestroyStageClustersDryRunJSON(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"destroy",
		"--stage", "clusters",
		"--clusters", "sno-libvirt",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy --stage clusters dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Target != "clusters" || report.Action != "destroy" || !report.DryRun {
		t.Fatalf("unexpected dry-run report header: %+v", report)
	}
	if !reflect.DeepEqual(report.Phases, []string{"deps", "base", "add-ons"}) {
		t.Fatalf("phases = %#v, want full clusters destroy scope", report.Phases)
	}
	if report.DestroyPlan == nil || len(report.DestroyPlan.Tasks) == 0 {
		t.Fatalf("staged destroy dry-run must carry the executed task plan, got %+v", report.DestroyPlan)
	}
	for _, task := range report.DestroyPlan.Tasks {
		if task.ID == "destroy.machine-infra" {
			t.Fatalf("explicit clusters stage must retain machine substrate: %+v", report.DestroyPlan.Tasks)
		}
	}
}

func TestDestroyClustersDefaultsToSelectedFullLifecycle(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"destroy",
		"--clusters", "sno-libvirt",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("destroy --clusters dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Target != "all" || report.Action != "destroy" || !report.DryRun {
		t.Fatalf("unexpected dry-run report header: %+v", report)
	}
	if report.DestroyPlan == nil || len(report.DestroyPlan.Tasks) == 0 {
		t.Fatalf("staged destroy dry-run must carry the executed task plan, got %+v", report.DestroyPlan)
	}
	var ids []string
	for _, task := range report.DestroyPlan.Tasks {
		ids = append(ids, task.ID)
	}
	for _, want := range []string{"destroy.machine-infra", "destroy.container-clusters"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("selected full lifecycle tasks = %v, missing %s", ids, want)
		}
	}
	if !slices.Contains(report.ExtraVars, "bootwright_destroy_cluster_scope=sno-libvirt") {
		t.Fatalf("selected full lifecycle must scope recorded-resource cleanup: %#v", report.ExtraVars)
	}
	if slices.Contains(report.ExtraVars, "bootwright_infra_destroy_context_sweep=true") {
		t.Fatalf("selected full lifecycle must not enable the context sweep: %#v", report.ExtraVars)
	}
}

func TestProtectedDestroyRequiresOverrideBeyondYes(t *testing.T) {
	initProtectedTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"destroy",
		"--stage", "clusters",
		"--clusters", "sno-libvirt",
		"--yes",
		"--ask-become-pass=false",
	)
	if code == 0 {
		t.Fatalf("protected destroy unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	for _, want := range []string{"destroyProtection=requiredOverride", "requires --force"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("protected destroy stderr missing %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stdout, "Start") || strings.Contains(stdout, "Bundle") {
		t.Fatalf("protected destroy progressed to workflow setup\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestProtectedFullDestroyRequiresOverrideBeyondYes(t *testing.T) {
	initProtectedTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"destroy",
		"--yes",
		"--ask-become-pass=false",
	)
	if code == 0 {
		t.Fatalf("protected full destroy unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	for _, want := range []string{"destroyProtection=requiredOverride", "requires --force"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("protected full destroy stderr missing %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "--stage must be one of") {
		t.Fatalf("full destroy must not demand a stage:\n%s", stderr)
	}
	if strings.Contains(stdout, "Start") || strings.Contains(stdout, "Bundle") {
		t.Fatalf("protected full destroy progressed to workflow setup\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestProtectedDestroyDryRunReportsProtection(t *testing.T) {
	initProtectedTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"destroy",
		"--stage", "clusters",
		"--clusters", "sno-libvirt",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("protected destroy dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.DestroySafety == nil {
		t.Fatalf("dry-run report missing destroy safety: %+v", report)
	}
	if !report.DestroySafety.OverrideRequired || report.DestroySafety.Override {
		t.Fatalf("destroy safety = %+v, want required without override", report.DestroySafety)
	}
	if got := strings.Join(report.DestroySafety.Reasons, "\n"); !strings.Contains(got, "destroyProtection=requiredOverride") {
		t.Fatalf("destroy safety reasons = %q", got)
	}
}

func TestProtectedDestroyOverridePassesSafetyGate(t *testing.T) {
	initProtectedTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"destroy",
		"--stage", "clusters",
		"--clusters", "sno-libvirt",
		"--dry-run",
		"--output", "json",
		"--force",
		"--yes",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("protected destroy override exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.DestroySafety == nil {
		t.Fatalf("dry-run report missing destroy safety: %+v", report)
	}
	if report.DestroySafety.OverrideRequired || !report.DestroySafety.Override {
		t.Fatalf("destroy safety = %+v, want override supplied and no requirement remaining", report.DestroySafety)
	}
	if slices.Contains(report.ExtraVars, "bootwright_destroy_override=true") {
		t.Fatalf("extra vars should not carry an inert destroy override: %+v", report.ExtraVars)
	}
}

func TestProtectedApplyOverrideGreenfieldNotGatedByProtection(t *testing.T) {
	initProtectedTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"apply",
		"--stage", "clusters",
		"--clusters", "sno-libvirt",
		"--converge-drifted",
		"--yes",
		"--ask-become-pass=false",
	)
	if code == 0 {
		t.Fatalf("apply --converge-drifted unexpectedly succeeded (no real infra)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	for _, unwanted := range []string{"destroy-protected", "destructively rebuild"} {
		if strings.Contains(stderr, unwanted) {
			t.Fatalf("greenfield apply --converge-drifted must not be blocked by destroy protection; stderr contained %q:\n%s", unwanted, stderr)
		}
	}
}

func TestProtectedApplyOverrideDryRunPreviews(t *testing.T) {
	initProtectedTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"apply",
		"--stage", "clusters",
		"--clusters", "sno-libvirt",
		"--converge-drifted",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=false",
	)
	if code != 0 {
		t.Fatalf("protected apply --converge-drifted --dry-run should preview, exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if !slices.Contains(report.ExtraVars, "bootwright_apply_mode=override") {
		t.Fatalf("dry-run preview should still carry the override apply mode: %+v", report.ExtraVars)
	}
}

func TestScopedPreflightDryRunJSONDoesNotPromptForBecome(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"preflight", "infra",
		"--dry-run",
		"--output", "json",
	)
	if code != 0 {
		t.Fatalf("preflight infra dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if report.Action != "preflight" {
		t.Fatalf("dry-run action = %q, want preflight", report.Action)
	}
	if commandContains(report.Command, "--ask-become-pass") {
		t.Fatalf("preflight infra should not ask for become password, got %v", report.Command)
	}
}

func TestScopedApplyDryRunJSONIncludesBecomePromptForProviderHosts(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"apply",
		"--stage", "infra",
		"--dry-run",
		"--output", "json",
		"--ask-become-pass=true",
	)
	if code != 0 {
		t.Fatalf("apply --stage infra dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if !commandContains(report.Command, "--ask-become-pass") {
		t.Fatalf("expected apply --stage infra command to ask for become password, got %v", report.Command)
	}
}

func TestRenderInstallerScopedFixtureJSON(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t,
		"render", "installer",
		"--clusters", "sno-libvirt",
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
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "kubevirt-lab", "-f", dir)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, stderr, code = runCLI(t, "apply", "--stage", "infra", "--dry-run")
	if code != 0 {
		t.Fatalf("apply --stage infra dry-run exited %d, stderr=%q", code, stderr)
	}
}

func TestApplyAllScopedKubeVirtChildDryRunReportsHostDependency(t *testing.T) {
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "nested", "-f", example)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, "apply", "--clusters", "dc1-child-ocp", "--dry-run", "--output", "json")
	if code == 0 {
		t.Fatalf("scoped child apply unexpectedly succeeded, stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "include dc1-metal-ocp in --clusters or apply it first") {
		t.Fatalf("scoped child apply error missing host dependency remediation, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestApplyKubeVirtChildOnlySelectionAcceptsReadyParent(t *testing.T) {
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "nested", "-f", example)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.NewContext("nested")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := workflow.SaveClusterInstallRecord(ctx.ClustersDir, workflow.ClusterInstallRecord{
		Cluster:     "dc1-metal-ocp",
		Status:      workflow.ClusterInstallStatusInstalled,
		Phase:       workflow.ClusterInstallPhaseComplete,
		UpdatedAt:   now,
		InstalledAt: &now,
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	kubeconfig := filepath.Join(ctx.ClustersDir, "dc1-metal-ocp", "secrets", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o700); err != nil {
		t.Fatalf("mkdir kubeconfig dir: %v", err)
	}
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	if err := extensionrecords.SaveRecord(ctx.ClustersDir, extensionrecords.Record{
		Cluster:   "dc1-metal-ocp",
		Extension: "openshift-virtualization",
		Status:    extensionrecords.RecordStatusReady,
		Phase:     extensionrecords.RecordPhaseComplete,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	stdout, stderr, code = runCLI(t, "apply", "--stage", "clusters", "--clusters", "dc1-child-ocp", "--dry-run", "--output", "json", "--ask-become-pass=false")
	if code != 0 {
		t.Fatalf("child-only apply with ready parent exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestApplyKubeVirtParentAndChildSelectionAccepted(t *testing.T) {
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "nested", "-f", example)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, "apply", "--stage", "clusters", "--clusters", "dc1-metal-ocp,dc1-child-ocp", "--dry-run", "--output", "json", "--ask-become-pass=false")
	if code != 0 {
		t.Fatalf("parent+child apply exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestScopedRunValidatesWholeInput(t *testing.T) {
	setTestHomeAndRoot(t)
	if stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", fixturePath("001-sno-libvirt")); code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.NewContext("test")
	if err != nil {
		t.Fatal(err)
	}
	orphan := "apiVersion: bootwright.io/v1alpha1\nkind: StorageCluster\nmetadata:\n  name: orphan-ceph\nspec:\n  type: bogus\n"
	if err := os.WriteFile(filepath.Join(ctx.InputDir, "orphan-storage-cluster.yaml"), []byte(orphan), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, "apply", "--clusters", "sno-libvirt", "--dry-run", "--output", "json", "--ask-become-pass=false")
	if code == 0 {
		t.Fatalf("scoped apply ignored the broken out-of-scope StorageCluster, stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "orphan-ceph") {
		t.Fatalf("apply error does not name the out-of-scope StorageCluster, stdout=%q stderr=%q", stdout, stderr)
	}

	if stdout, stderr, code := runCLI(t, "destroy", "--clusters", "sno-libvirt", "--dry-run", "--output", "json", "--ask-become-pass=false"); code == 0 {
		t.Fatalf("scoped destroy ignored the broken out-of-scope StorageCluster, stdout=%q stderr=%q", stdout, stderr)
	}

	if _, stderr, code := runCLI(t, "apply", "--clusters", "sno-libvirt", "--scoped-validation", "--ask-become-pass=false"); code == 0 || !strings.Contains(stderr, "unknown flag") {
		t.Fatalf("apply --scoped-validation should be an unknown flag, code=%d stderr=%q", code, stderr)
	}
}

func TestContextInitPreparesAnsibleBundle(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "internal", "embedded", "bundle", "ansible.cfg")); err != nil {
		t.Skip("embedded bundle has not been synced")
	}
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", fixturePath("001-sno-libvirt"))
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	bundleDir, err := resolveBundleDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"ansible.cfg",
		"bootwright.core.check_preflight",
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
	_, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", fixturePath("001-sno-libvirt"), oldFlag)
	if code == 0 {
		t.Fatalf("context init %s unexpectedly succeeded", oldFlag)
	}
	if !strings.Contains(stderr, "unknown flag: "+oldFlag) {
		t.Fatalf("stderr does not reject %s: %q", oldFlag, stderr)
	}

	_, stderr, code = runCLI(t, "context", "init", "--name", "test", "-f", fixturePath("001-sno-libvirt"), "--base-dir", t.TempDir())
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
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, stderr, code = runCLI(t, "context", "init", "--name", "test", "-f", source)
	if code == 0 {
		t.Fatal("second context init without --yes unexpectedly succeeded")
	}
	if !strings.Contains(stderr, `context "test" already exists; rerun with --yes to drop it and recreate it from`) {
		t.Fatalf("stderr missing --yes remediation: %q", stderr)
	}
	oldFlag := "--" + "force"
	if strings.Contains(stderr, oldFlag) {
		t.Fatalf("stderr still mentions %s: %q", oldFlag, stderr)
	}
}

func TestContextInitCopiesWorkspaceIntoContext(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	wantInput := filepath.Join(ctx.BaseDir, workspace.InputDirName)
	if ctx.InputDir != wantInput || len(ctx.InputPaths) != 1 || ctx.InputPaths[0] != wantInput {
		t.Fatalf("input = %q %v, want %q", ctx.InputDir, ctx.InputPaths, wantInput)
	}
	if _, err := os.Stat(filepath.Join(ctx.InputDir, "environment.yaml")); err != nil {
		t.Fatalf("context init did not copy the source into input/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ctx.BaseDir, "input-source.yaml")); !os.IsNotExist(err) {
		t.Fatalf("context init still wrote a recorded-path file: %v", err)
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	if _, vStderr, vCode := runCLI(t, "validate"); vCode != 0 {
		t.Fatalf("validate after source deletion exited %d, stderr=%q", vCode, vStderr)
	}
	if !strings.Contains(stdout, ctx.InputDir) {
		t.Fatalf("context init stdout missing input dir:\n%s", stdout)
	}
}

func TestContextInitYesDropsAndRecreates(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	droppedSecret := filepath.Join(ctx.SecretsDir, "dropped-secret")
	if err := os.WriteFile(droppedSecret, []byte("dropped\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := copyFixtureYAML(t, "001-sno-libvirt")
	stdout, stderr, code = runCLI(t, "context", "init", "--name", "test", "-f", replacement, "--yes")
	if code != 0 {
		t.Fatalf("context init --yes exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(droppedSecret); !os.IsNotExist(err) {
		t.Fatalf("context init --yes preserved dropped context state: %v", err)
	}
	ctx, err = workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ctx.InputDir, "environment.yaml")); err != nil {
		t.Fatalf("context init --yes did not recreate input from the source: %v", err)
	}
}

func TestContextInitYesKeepsContextWhenReplacementInvalid(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	keptSecret := filepath.Join(ctx.SecretsDir, "kept-secret")
	if err := os.WriteFile(keptSecret, []byte("kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	replacement := copyFixtureYAML(t, "001-sno-libvirt")
	replaceInFile(t, filepath.Join(replacement, "environment.yaml"), "    base: bootwright.test\n", "    base: bootwright.test\n  retiredField: true\n")
	stdout, stderr, code = runCLI(t, "context", "init", "--name", "test", "-f", replacement, "--yes")
	if code == 0 {
		t.Fatalf("context init --yes unexpectedly accepted invalid replacement:\n%s", stdout)
	}
	if !strings.Contains(stderr, "field retiredField not found") {
		t.Fatalf("stderr missing strict decode error: %q", stderr)
	}
	if body, err := os.ReadFile(keptSecret); err != nil || string(body) != "kept\n" {
		t.Fatalf("invalid replacement dropped context state: %q err=%v", body, err)
	}
	if _, err := os.Stat(filepath.Join(ctx.InputDir, "environment.yaml")); err != nil {
		t.Fatalf("invalid replacement dropped the existing input: %v", err)
	}
}

func TestContextInitYesAcceptsUnselectedInvalidFilesWithResourceSelection(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}

	replacement := copyFixtureYAML(t, "001-sno-libvirt")
	addFixtureResourceSelection(t, replacement)
	if err := os.WriteFile(filepath.Join(replacement, "unselected.yaml"), []byte(`apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: spare-machine
spec:
  retiredField: true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, "context", "init", "--name", "test", "-f", replacement, "--yes")
	if code != 0 {
		t.Fatalf("context init --yes exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ctx.InputDir, "unselected.yaml")); err != nil {
		t.Fatalf("context init --yes did not copy the replacement into input/: %v", err)
	}
}

func TestContextInitRejectsWorkspaceInsideBootwrightRoot(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	_, stderr, code = runCLI(t, "context", "init", "--name", "test", "-f", ctx.BaseDir, "--yes")
	if code == 0 {
		t.Fatal("context init unexpectedly recorded a workspace inside the context directory")
	}
	if !strings.Contains(stderr, "must live outside the Bootwright state directory") {
		t.Fatalf("stderr does not reject in-root workspace: %q", stderr)
	}
}

func TestContextInitRequiresSingleWorkspaceDirectory(t *testing.T) {
	setTestHomeAndRoot(t)
	_, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", fixturePath("001-sno-libvirt"), "-f", fixturePath("001-sno-libvirt"))
	if code == 0 {
		t.Fatal("context init accepted multiple -f source paths")
	}
	if !strings.Contains(stderr, "exactly one source directory") {
		t.Fatalf("stderr missing single-source error: %q", stderr)
	}
	file := filepath.Join(t.TempDir(), "environment.yaml")
	if err := os.WriteFile(file, []byte("apiVersion: bootwright.io/v1alpha1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code = runCLI(t, "context", "init", "--name", "test", "-f", file)
	if code == 0 {
		t.Fatal("context init accepted a file as workspace")
	}
	if !strings.Contains(stderr, "is not a directory") {
		t.Fatalf("stderr missing not-a-directory error: %q", stderr)
	}
}

func TestSourceEditsRequireContextUpdate(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	const marker = "# context-update marker\n"
	srcEnv := filepath.Join(source, "environment.yaml")
	data, err := os.ReadFile(srcEnv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcEnv, append(data, []byte(marker)...), 0o600); err != nil {
		t.Fatal(err)
	}
	inputEnv := filepath.Join(ctx.InputDir, "environment.yaml")
	if before, err := os.ReadFile(inputEnv); err != nil || strings.Contains(string(before), marker) {
		t.Fatalf("source edit leaked into the context input before update (err=%v)", err)
	}
	if _, stderr, code := runCLI(t, "context", "update", "--name", "test", "-f", source, "--yes"); code != 0 {
		t.Fatalf("context update exited %d, stderr=%q", code, stderr)
	}
	after, err := os.ReadFile(inputEnv)
	if err != nil || !strings.Contains(string(after), marker) {
		t.Fatalf("context update did not refresh the context input (err=%v)", err)
	}
}

func TestContextUpdateReplacesInputKeepingState(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	keptSecret := filepath.Join(ctx.SecretsDir, "kept-secret")
	if err := os.WriteFile(keptSecret, []byte("kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := copyFixtureYAML(t, "001-sno-libvirt")
	if err := os.WriteFile(filepath.Join(replacement, "extra-note.txt"), []byte("note\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, "context", "update", "--name", "test", "-f", replacement, "--yes")
	if code != 0 {
		t.Fatalf("context update exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(ctx.InputDir, "extra-note.txt")); err != nil {
		t.Fatalf("context update did not copy the new source into input/: %v", err)
	}
	if body, err := os.ReadFile(keptSecret); err != nil || string(body) != "kept\n" {
		t.Fatalf("context update dropped context state: %q err=%v", body, err)
	}
	if out, _, _ := runCLI(t, "context", "current", "--short"); out != "test\n" {
		t.Fatalf("context update changed current context: %q", out)
	}
}

func TestContextUpdateSnapshotsPreviousInput(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	if _, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source); code != 0 {
		t.Fatalf("context init exited %d, stderr=%q", code, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	replacement := copyFixtureYAML(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "context", "update", "--name", "test", "-f", replacement, "--yes")
	if code != 0 {
		t.Fatalf("context update exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "saved to history") {
		t.Fatalf("context update did not report a history snapshot:\n%s", stdout)
	}
	entries, err := os.ReadDir(workspace.InputHistoryDir(ctx))
	if err != nil {
		t.Fatalf("read input history: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one history entry after one update, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "0001-") {
		t.Fatalf("first history entry = %q, want a 0001- prefix", entries[0].Name())
	}
}

func TestContextUpdateRequiresSingleSourceDirectory(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	_, stderr, code := runCLI(t, "context", "update", "--name", "test", "-f", fixturePath("001-sno-libvirt"), "-f", fixturePath("001-sno-libvirt"))
	if code == 0 {
		t.Fatal("context update accepted multiple -f source paths")
	}
	if !strings.Contains(stderr, "exactly one source directory") {
		t.Fatalf("stderr missing single-source error: %q", stderr)
	}
}

func TestContextUpdateAbortsWithoutConfirmation(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	if _, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source); code != 0 {
		t.Fatalf("context init exited %d, stderr=%q", code, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	const marker = "# context-update marker\n"
	srcEnv := filepath.Join(source, "environment.yaml")
	data, err := os.ReadFile(srcEnv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcEnv, append(data, []byte(marker)...), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "context", "update", "--name", "test", "-f", source)
	if code == 0 {
		t.Fatal("context update without --yes proceeded; want abort")
	}
	if !strings.Contains(stderr, "aborted") {
		t.Fatalf("stderr missing abort notice: %q", stderr)
	}
	inputEnv := filepath.Join(ctx.InputDir, "environment.yaml")
	if after, err := os.ReadFile(inputEnv); err != nil || strings.Contains(string(after), marker) {
		t.Fatalf("aborted update still replaced the context input (err=%v)", err)
	}
}

func TestContextUpdateProceedsWithInteractiveYes(t *testing.T) {
	source := copyFixtureYAML(t, "001-sno-libvirt")
	setTestHomeAndRoot(t)
	if _, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", source); code != 0 {
		t.Fatalf("context init exited %d, stderr=%q", code, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	const marker = "# context-update marker\n"
	srcEnv := filepath.Join(source, "environment.yaml")
	data, err := os.ReadFile(srcEnv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcEnv, append(data, []byte(marker)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runCLIWithInput(t, "y\n", "context", "update", "--name", "test", "-f", source); code != 0 {
		t.Fatalf("context update with interactive yes exited %d, stderr=%q", code, stderr)
	}
	inputEnv := filepath.Join(ctx.InputDir, "environment.yaml")
	if after, err := os.ReadFile(inputEnv); err != nil || !strings.Contains(string(after), marker) {
		t.Fatalf("confirmed update did not refresh the context input (err=%v)", err)
	}
}

func TestContextCurrentAndListReadSharedContextStorage(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")

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
	if !strings.Contains(stdout, ctx.BaseDir) {
		t.Fatalf("context current stdout missing %s:\n%s", ctx.BaseDir, stdout)
	}
	stdout, stderr, code = runCLI(t, "context", "list")
	if code != 0 {
		t.Fatalf("context list exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "* test") || !strings.Contains(stdout, ctx.BaseDir) {
		t.Fatalf("context list stdout missing current context:\n%s", stdout)
	}
}

func TestContextListReportsStaleCurrent(t *testing.T) {
	setTestHomeAndRoot(t)
	saveTestContextRegistry(t, "missing")

	stdout, stderr, code := runCLI(t, "context", "list")
	if code != 0 {
		t.Fatalf("context list exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "[WARN] current context: missing not found") {
		t.Fatalf("context list did not report stale current:\n%s", stdout)
	}
}

func TestContextCurrentRejectsStaleCurrent(t *testing.T) {
	setTestHomeAndRoot(t)
	saveTestContextRegistry(t, "missing")

	_, stderr, code := runCLI(t, "context", "current", "--short")
	if code == 0 {
		t.Fatal("context current unexpectedly accepted stale current")
	}
	if !strings.Contains(stderr, `current context "missing" is not available in shared storage`) {
		t.Fatalf("stderr missing stale-current error: %q", stderr)
	}
}

func TestContextDeleteWithoutPurgeFails(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")

	_, stderr, code := runCLI(t, "context", "delete", "--name", "test")
	if code == 0 {
		t.Fatal("context delete without --purge unexpectedly succeeded")
	}
	if !strings.Contains(stderr, "rerun with --purge") {
		t.Fatalf("stderr missing purge remediation: %q", stderr)
	}
}

func TestContextDeleteWithoutPurgeLeavesSharedContextData(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	keepPath := filepath.Join(ctx.RenderedDir, "keep")
	if err := os.WriteFile(keepPath, []byte("state\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, code := runCLI(t, "context", "delete", "--name", "test")
	if code == 0 {
		t.Fatal("context delete without --purge unexpectedly succeeded")
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

	_, stderr, code := runCLI(t, "context", "delete", "--name", "test", "--purge", "--yes")
	if code != 0 {
		t.Fatalf("context delete --purge exited %d, stderr=%q", code, stderr)
	}
	registry, err := workspace.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := workspace.Load(registry)
	if err != nil {
		t.Fatal(err)
	}
	if store.Current != "" {
		t.Fatalf("context delete --purge left current selected: %+v", store)
	}
	if _, err := os.Stat(ctx.BaseDir); !os.IsNotExist(err) {
		t.Fatalf("context delete --purge did not remove context dir: %v", err)
	}
}

func TestContextDeletePurgeRefusesLiveEstate(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	now := time.Now().UTC()
	if err := workflow.SaveClusterInstallRecord(ctx.ClustersDir, workflow.ClusterInstallRecord{
		Cluster:     "sno-libvirt",
		Status:      workflow.ClusterInstallStatusInstalled,
		Phase:       workflow.ClusterInstallPhaseComplete,
		UpdatedAt:   now,
		InstalledAt: &now,
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}

	_, stderr, code := runCLI(t, "context", "delete", "--name", "test", "--purge", "--yes")
	if code == 0 {
		t.Fatal("context delete --purge must refuse while an installed cluster record stands")
	}
	for _, want := range []string{"still owns", "bootwright destroy --context test", "--force"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("live-estate refusal missing %q: %q", want, stderr)
		}
	}
	if _, err := os.Stat(ctx.BaseDir); err != nil {
		t.Fatalf("refused purge must leave the context intact: %v", err)
	}

	_, stderr, code = runCLI(t, "context", "delete", "--name", "test", "--purge", "--yes", "--force")
	if code != 0 {
		t.Fatalf("context delete --purge --force must proceed, exited %d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(ctx.BaseDir); !os.IsNotExist(err) {
		t.Fatalf("context delete --purge --force did not remove context dir: %v", err)
	}
}

func TestContextDeletePurgeClearsDanglingSelectionWhenSharedDirMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))
	homeA := t.TempDir()
	homeB := t.TempDir()

	t.Setenv("HOME", homeA)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "lab", "-f", fixturePath("001-sno-libvirt"))
	if code != 0 {
		t.Fatalf("user A context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	t.Setenv("HOME", homeB)
	if _, stderr, code = runCLI(t, "context", "use", "--name", "lab"); code != 0 {
		t.Fatalf("user B context use exited %d, stderr=%q", code, stderr)
	}
	t.Setenv("HOME", homeA)
	if _, stderr, code = runCLI(t, "context", "delete", "--name", "lab", "--purge", "--yes"); code != 0 {
		t.Fatalf("user A context delete --purge exited %d, stderr=%q", code, stderr)
	}

	t.Setenv("HOME", homeB)
	stdout, stderr, code = runCLI(t, "context", "delete", "--name", "lab", "--purge", "--yes")
	if code != 0 {
		t.Fatalf("user B context delete of dangling selection exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	registryB, err := workspace.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := workspace.Load(registryB)
	if err != nil {
		t.Fatal(err)
	}
	if storeB.Current != "" {
		t.Fatalf("user B current = %q, want cleared", storeB.Current)
	}
}

func TestContextDeletePurgeSucceedsWhenContextNeverExisted(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")

	_, stderr, code := runCLI(t, "context", "delete", "--name", "never-existed", "--purge", "--yes")
	if code != 0 {
		t.Fatalf("context delete of unknown context exited %d, stderr=%q", code, stderr)
	}
}

func TestContextSelectionIsPerHomeWithSharedStorage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))
	homeA := t.TempDir()
	homeB := t.TempDir()

	t.Setenv("HOME", homeA)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "lab", "-f", fixturePath("001-sno-libvirt"))
	if code != 0 {
		t.Fatalf("user A context init lab exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	t.Setenv("HOME", homeB)
	stdout, stderr, code = runCLI(t, "context", "use", "--name", "lab")
	if code != 0 {
		t.Fatalf("user B context use lab exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	t.Setenv("HOME", homeA)
	stdout, stderr, code = runCLI(t, "context", "init", "--name", "other", "-f", fixturePath("001-sno-libvirt"))
	if code != 0 {
		t.Fatalf("user A context init other exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, "context", "current", "--short")
	if code != 0 || stdout != "other\n" {
		t.Fatalf("user A current stdout=%q code=%d stderr=%q", stdout, code, stderr)
	}
	t.Setenv("HOME", homeB)
	stdout, stderr, code = runCLI(t, "context", "current", "--short")
	if code != 0 || stdout != "lab\n" {
		t.Fatalf("user B current stdout=%q code=%d stderr=%q", stdout, code, stderr)
	}
}

func TestContextDeletePurgeClearsOnlyCallerCurrent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))
	homeA := t.TempDir()
	homeB := t.TempDir()

	t.Setenv("HOME", homeA)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "lab", "-f", fixturePath("001-sno-libvirt"))
	if code != 0 {
		t.Fatalf("user A context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	t.Setenv("HOME", homeB)
	stdout, stderr, code = runCLI(t, "context", "use", "--name", "lab")
	if code != 0 {
		t.Fatalf("user B context use exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	t.Setenv("HOME", homeA)
	_, stderr, code = runCLI(t, "context", "delete", "--name", "lab", "--purge", "--yes")
	if code != 0 {
		t.Fatalf("user A context delete --purge exited %d, stderr=%q", code, stderr)
	}
	registryA, err := workspace.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	storeA, err := workspace.Load(registryA)
	if err != nil {
		t.Fatal(err)
	}
	if storeA.Current != "" {
		t.Fatalf("user A current = %q, want cleared", storeA.Current)
	}
	t.Setenv("HOME", homeB)
	registryB, err := workspace.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := workspace.Load(registryB)
	if err != nil {
		t.Fatal(err)
	}
	if storeB.Current != "lab" {
		t.Fatalf("user B current = %q, want stale lab", storeB.Current)
	}
	_, stderr, code = runCLI(t, "context", "current", "--short")
	if code == 0 || !strings.Contains(stderr, `current context "lab" is not available in shared storage`) {
		t.Fatalf("user B stale current code=%d stderr=%q", code, stderr)
	}
}

func TestSecretListJSONReportsDeclaredStatus(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	genOut, stderr, code := runCLI(t, "secret", "generate")
	if code != 0 {
		t.Fatalf("secret generate exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(genOut, "Needs secret set") || !strings.Contains(genOut, "request(s) handled") {
		t.Fatalf("secret generate stdout missing needs-secret-set report:\n%s", genOut)
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
	if got := byName["bmc-credentials"]; got.Type != "generated:usernamePassword" || !got.Present {
		t.Fatalf("bmc-credentials status = %+v", got)
	}
	if got := byName["openshift-pull-secret"]; got.Type != "context:dockerConfigJson" || got.Present {
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
		Secrets: []v1alpha1.Secret{{
			Metadata:   v1alpha1.Metadata{Name: "blocked-secret"},
			SourcePath: filepath.Join(dir, "environment.yaml"),
			Spec: v1alpha1.SecretSpec{
				Type:   v1alpha1.SecretTypeOpaque,
				Source: v1alpha1.SecretSource{File: &v1alpha1.SecretFileSource{Path: path}},
			},
		}},
	}
	entries, err := declaredSecretEntriesForContext("test", filepath.Join(dir, "context-secrets"), state)
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
	writeTestContextSecret(t, ctx.Name, ctx.SecretsDir, "manual-secret", secret.MaterialPrimary, []byte("secret\nwith-newline\n"))
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
	writeTestContextSecret(t, ctx.Name, ctx.SecretsDir, "manual-secret", secret.MaterialPrimary, []byte("secret\n"))
	if err := os.WriteFile(filepath.Join(ctx.InputDir, "environment.yaml"), []byte("not: [valid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "secret", "delete", "--name", "manual-secret", "--yes")
	if code != 0 {
		t.Fatalf("secret delete exited %d, stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(ctx.SecretsDir, "manual-secret")); !os.IsNotExist(err) {
		t.Fatalf("manual-secret still exists or stat failed unexpectedly: %v", err)
	}
}

func TestSecretEncryptionInitStatusMigrateRotate(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "secret", "encryption", "init")
	if code != 0 {
		t.Fatalf("secret encryption init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, "secret", "encryption", "status", "--output", "json")
	if code != 0 {
		t.Fatalf("secret encryption status exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	var status secret.StoreStatus
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, stdout)
	}
	if !status.Initialized || status.KeyProvider != "root-owned-file" || status.ActiveKeyID == "" {
		t.Fatalf("status = %+v, want initialized root-owned-file", status)
	}

	plainPath := filepath.Join(ctx.SecretsDir, "manual-secret")
	if err := os.WriteFile(plainPath, []byte("plain\n"), 0o600); err != nil {
		t.Fatalf("write plaintext fixture: %v", err)
	}
	_, stderr, code = runCLI(t, "secret", "show", "--name", "manual-secret")
	if code == 0 || !strings.Contains(stderr, "not found") {
		t.Fatalf("secret show before migrate code=%d stderr=%q", code, stderr)
	}
	stdout, stderr, code = runCLI(t, "secret", "encryption", "migrate", "--yes")
	if code != 0 {
		t.Fatalf("secret encryption migrate exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	got, err := secret.NewContextStore(ctx.Name, ctx.SecretsDir).Read(secret.MaterialKey{Name: "manual-secret", Role: secret.MaterialPrimary})
	if err != nil {
		t.Fatalf("read migrated secret: %v", err)
	}
	if string(got) != "plain\n" {
		t.Fatalf("migrated secret = %q", got)
	}
	rawEnvelope, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatalf("read migrated envelope: %v", err)
	}
	if bytes.Contains(rawEnvelope, []byte("plain")) {
		t.Fatalf("migrated envelope leaked plaintext")
	}
	before := secret.NewContextStore(ctx.Name, ctx.SecretsDir).Status().ActiveKeyID
	stdout, stderr, code = runCLI(t, "secret", "encryption", "rotate", "--yes")
	if code != 0 {
		t.Fatalf("secret encryption rotate exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	after := secret.NewContextStore(ctx.Name, ctx.SecretsDir).Status().ActiveKeyID
	if before == after {
		t.Fatalf("active key did not rotate: %s", before)
	}
	got, err = secret.NewContextStore(ctx.Name, ctx.SecretsDir).Read(secret.MaterialKey{Name: "manual-secret", Role: secret.MaterialPrimary})
	if err != nil || string(got) != "plain\n" {
		t.Fatalf("read rotated secret = %q err=%v", got, err)
	}
}

func TestStatusReportsReadyAndMissingSetupChecks(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "status")
	if code != 0 {
		t.Fatalf("status exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "[OK] input") {
		t.Fatalf("stdout missing input OK setup check:\n%s", stdout)
	}
	if err := os.RemoveAll(ctx.InputDir); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, "status")
	if code != 1 {
		t.Fatalf("status with missing input dir exited %d, want 1", code)
	}
	if !strings.Contains(stdout, "[MISSING] input") {
		t.Fatalf("stdout missing input MISSING:\n%s", stdout)
	}
	for _, want := range []string{`context "test"`, ctx.InputDir, "is missing", "context update --name test -f", "context init --name test -f <dir> --yes"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr %q missing %q", stderr, want)
		}
	}
	if !strings.Contains(stdout, "Next steps") {
		t.Fatalf("stdout missing next steps for unready context:\n%s", stdout)
	}
}

func TestStatusJSONReportsSetupChecksWithoutBlocking(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "status", "--output", "json")
	if code != 0 {
		t.Fatalf("status json exited %d, stderr=%q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("status json wrote stderr: %q", stderr)
	}
	var report statusReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode status json: %v\n%s", err, stdout)
	}
	if report.Error != "" || report.Context.Name != "test" {
		t.Fatalf("unexpected status report: %+v", report)
	}
	if len(report.SetupChecks) == 0 {
		t.Fatalf("status report missing setup checks: %+v", report)
	}
	foundMissingSecret := false
	for _, entry := range report.Secrets {
		if !entry.Present {
			foundMissingSecret = true
		}
	}
	if !foundMissingSecret {
		t.Fatalf("status report missing declared-secret presence: %+v", report.Secrets)
	}
	if len(report.NextSteps) == 0 {
		t.Fatalf("status report missing next steps: %+v", report)
	}

	if err := os.RemoveAll(ctx.InputDir); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, "status", "--output", "json")
	if code != 1 {
		t.Fatalf("status json with missing input dir exited %d, stderr=%q", code, stderr)
	}
	report = statusReport{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode status json: %v\n%s", err, stdout)
	}
	if report.Error == "" || len(report.NextSteps) == 0 {
		t.Fatalf("unready status report missing error or next steps: %+v", report)
	}
	foundInputMissing := false
	for _, check := range report.SetupChecks {
		if check.Name == "input" && check.Status == string(output.StatusMissing) {
			foundInputMissing = true
		}
	}
	if !foundInputMissing {
		t.Fatalf("unready status report missing input setup check: %+v", report.SetupChecks)
	}
}

func TestContextValidateSubcommandRemoved(t *testing.T) {
	for _, target := range []string{"validate", "validade"} {
		stdout, stderr, code := runCLI(t, "context", target)
		if code == 0 {
			t.Fatalf("context %s unexpectedly succeeded:\n%s", target, stdout)
		}
		if !strings.Contains(stderr, `invalid argument "`+target+`"`) {
			t.Fatalf("stderr does not reject context %s: %q", target, stderr)
		}
	}
}

func TestContextBackedCommandRequiresReadyContext(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	if err := os.RemoveAll(ctx.InputDir); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "validate")
	if code == 0 {
		t.Fatal("validate unexpectedly ran with a missing workspace")
	}
	for _, want := range []string{`context "test"`, ctx.InputDir, "is missing", "context update --name test -f"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr %q missing %q", stderr, want)
		}
	}
}

func TestLocalRootGateArgs(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"context", "list"}, want: true},
		{args: []string{"context", "current"}, want: true},
		{args: []string{"context", "use", "--name", "lab"}, want: true},
		{args: []string{"context", "init", "--name", "lab", "-f", "."}, want: false},
		{args: []string{"context", "update", "--name", "lab", "-f", "."}, want: false},
		{args: []string{"context", "delete", "--name", "lab"}, want: false},
		{args: []string{"context", "delete", "--name", "lab", "--purge"}, want: false},
		{args: []string{"context", "delete", "--name", "lab", "--purge=true"}, want: false},
		{args: []string{"help", "preflight"}, want: false},
		{args: []string{"completion", "bash"}, want: false},
		{args: []string{cobra.ShellCompRequestCmd, ""}, want: false},
		{args: []string{cobra.ShellCompNoDescRequestCmd, ""}, want: false},
		{args: []string{"secret", "set", "--name", "openshift-pull-secret", "--pull-secret", "/home/user/pull-secret.json"}, want: false},
		{args: []string{"secret"}, want: false},
		{args: []string{"secret", "show", "--name", "pull-secret"}, want: true},
		{args: []string{"secret", "show", "pull-secret"}, want: false},
		{args: []string{"secret", "delete", "--name", "manual-secret"}, want: true},
		{args: []string{"secret", "delete", "manual-secret"}, want: false},
		{args: []string{"secret", "delete"}, want: false},
		{args: []string{"media"}, want: false},
		{args: []string{"media", "list"}, want: true},
		{args: []string{"media", "add", "--name", "rhel.iso", "--from-file", "/home/user/rhel.iso"}, want: true},
		{args: []string{"media", "add", "rhel.iso", "--from-file", "/home/user/rhel.iso"}, want: false},
		{args: []string{"media", "add"}, want: false},
		{args: []string{"media", "remove", "--name", "rhel.iso"}, want: true},
		{args: []string{"media", "remove", "rhel.iso"}, want: false},
		{args: []string{"media", "rm", "--name", "rhel.iso"}, want: true},
		{args: []string{"cluster"}, want: false},
		{args: []string{"cluster", "list"}, want: true},
		{args: []string{"cluster", "info"}, want: true},
		{args: []string{"cluster", "kubeconfig", "--name", "managed-01"}, want: true},
		{args: []string{"cluster", "rsh", "--name", "managed-01"}, want: true},
		{args: []string{"cluster", "rsh"}, want: false},
		{args: []string{"cluster", "exec", "--name", "managed-01", "--", "uptime"}, want: true},
		{args: []string{"cluster", "oc", "--name", "managed-01", "get", "nodes"}, want: true},
		{args: []string{"cluster", "oc", "get", "nodes"}, want: false},
		{args: []string{"cluster", "kubectl", "--name", "managed-01", "get", "pods"}, want: true},
		{args: []string{"cluster", "kubectl"}, want: false},
		{args: []string{"bastion"}, want: false},
		{args: []string{"bastion", "setup"}, want: true},
		{args: []string{"machine"}, want: false},
		{args: []string{"machine", "trust"}, want: true},
		{args: []string{"machine", "list"}, want: true},
		{args: []string{"machine", "rsh", "--name", "ceph-0"}, want: true},
		{args: []string{"machine", "rsh"}, want: false},
		{args: []string{"machine", "exec", "--name", "ceph-0", "--", "uptime"}, want: true},
		{args: []string{"example", "init", "--name", "lab", "--output-dir", "./lab-input"}, want: false},
		{args: []string{"validate", "-f", "./lab-input"}, want: false},
		{args: []string{"validate", "--file=./lab-input", "--output", "json"}, want: false},
		{args: []string{"validate"}, want: true},
		{args: []string{"validate", "--output", "json"}, want: true},
		{args: []string{"validate", "--source-dir", "./lab-input"}, want: false},
		{args: []string{"validate", "./lab-input"}, want: false},
		{args: []string{"validate", "--output"}, want: false},
		{args: []string{"preflight"}, want: false},
		{args: []string{"preflight", "infra"}, want: true},
		{args: []string{"preflight", "--help"}, want: false},
		{args: []string{"apply"}, want: true},
		{args: []string{"apply", "--stage", "infra"}, want: true},
		{args: []string{"destroy"}, want: true},
		{args: []string{"destroy", "--yes"}, want: true},
		{args: []string{"destroy", "--stage", "infra"}, want: true},
		{args: []string{"destroy", "--stage", "clusters"}, want: true},
		{args: []string{"destroy", "--stage", "bogus"}, want: false},
		{args: []string{"destroy", "cluster"}, want: true},
		{args: []string{"destroy", "--clusters", "ceph-ibm"}, want: true},
		{args: []string{"destroy", "--clusters=ceph-ibm"}, want: true},
		{args: []string{"destroy", "--clusters", ""}, want: true},
		{args: []string{"destroy", "--stage", "infra", "--clusters", "artifact-server"}, want: true},
		{args: []string{"render"}, want: false},
		{args: []string{"render", "--clusters", "managed-01"}, want: false},
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
		{"preflight"},
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
		{args: []string{"bastion", "setup"}, want: true},
		{args: []string{"preflight", "bastion"}, want: false},
		{args: []string{"apply", "--stage", "infra"}, want: true},
		{args: []string{"apply", "--stage", "clusters"}, want: true},
		{args: []string{"apply"}, want: true},
		{args: []string{"destroy"}, want: true},
		{args: []string{"destroy", "--yes"}, want: true},
		{args: []string{"destroy", "--stage", "infra"}, want: true},
		{args: []string{"destroy", "--stage=clusters"}, want: true},
		{args: []string{"destroy", "--stage", "bogus"}, want: false},
		{args: []string{"preflight", "infra"}, want: false},
		{args: []string{"secret", "set", "--name", "pull-secret"}, want: false},
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

	code, handled, err := ensureLocalRootForArgs(context.Background(), []string{"secret", "list"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ensureLocalRootForArgs: %v", err)
	}
	if !handled || code != 0 {
		t.Fatalf("ensureLocalRootForArgs handled=%v code=%d, want handled success", handled, code)
	}
	if gotName != "sudo" {
		t.Fatalf("command name = %q, want sudo", gotName)
	}
	if len(gotArgs) != 10 || gotArgs[0] != "-n" || gotArgs[1] != "env" || !strings.HasPrefix(gotArgs[2], workspace.InternalRegistryEnv+"=") || gotArgs[3] != localroot.InternalEnv+"=1" || gotArgs[4] != secret.InternalCallerHomeEnv+"="+home || gotArgs[5] != localroot.CallerPathEnv+"="+os.Getenv("PATH") || gotArgs[6] != localRootSudoAuthEnv+"="+localSudoAuthNonInteractive || !reflect.DeepEqual(gotArgs[7:], []string{"/usr/local/bin/bootwright", "secret", "list"}) {
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
	code, handled, err := ensureLocalRootForArgs(context.Background(), []string{"secret", "list"}, strings.NewReader("secret\n"), &bytes.Buffer{}, &stderr)
	if err != nil {
		t.Fatalf("ensureLocalRootForArgs: %v", err)
	}
	if !handled || code != 0 {
		t.Fatalf("ensureLocalRootForArgs handled=%v code=%d, want handled success", handled, code)
	}
	if got := stderr.String(); strings.Contains(got, "sudo required") {
		t.Fatalf("stderr must not explain the sudo prompt = %q", got)
	} else if got != "SUDO password: " {
		t.Fatalf("stderr = %q, want only the SUDO password prompt", got)
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
	if !reflect.DeepEqual(calls[3][commandIndex:], []string{"/usr/local/bin/bootwright", "secret", "list"}) {
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

	code, handled, err := ensureLocalRootForArgs(context.Background(), []string{"apply", "--stage", "infra", "--yes"}, strings.NewReader("secret\n"), &bytes.Buffer{}, &bytes.Buffer{})
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
	if !reflect.DeepEqual(calls[3][commandIndex:], []string{"/usr/local/bin/bootwright", "apply", "--stage", "infra", "--yes"}) {
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
	code, handled, err := ensureLocalRootForArgs(context.Background(), []string{"secret", "list"}, strings.NewReader("wrong\nalso-wrong\nsecret\n"), &bytes.Buffer{}, &stderr)
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
	_, handled, err := ensureLocalRootForArgs(context.Background(), []string{"secret", "list"}, strings.NewReader("wrong\nalso-wrong\nstill-wrong\n"), &bytes.Buffer{}, &stderr)
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
	_, handled, err := ensureLocalRootForArgs(context.Background(), []string{"secret", "list"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
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

	stdout, stderr, code := runCLI(t, "secret", "set", "--name", "openshift-pull-secret", "--pull-secret", source)
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
	rootArgs, rootStdin, cleanup, err := stagedSecretSetRootArgs(strings.NewReader("s3cr3t\n"), "proxy-credentials", "", "", "", "", "", "proxy", true, false, false)
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
	rootArgs, _, cleanup, err := stagedSecretSetRootArgs(strings.NewReader(""), "shared-ceph-external-details", "", "", "", source, "", "", false, false, false)
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
	stdout, stderr, code := runCLI(t, "secret", "set", "--name", "shared-ceph-external-details", "--raw-file", source)
	if code != 0 {
		t.Fatalf("secret set --raw-file exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	target := filepath.Join(ctx.SecretsDir, "shared-ceph-external-details")
	got, err := secret.NewContextStore(ctx.Name, ctx.SecretsDir).Read(secret.MaterialKey{Name: "shared-ceph-external-details", Role: secret.MaterialPrimary})
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

	replacement := filepath.Join(t.TempDir(), "external-details-replacement.json")
	replacementBody := []byte("[{\"name\":\"rook-ceph-mon\",\"kind\":\"Secret\",\"data\":{\"fsid\":\"replacement-fsid\"}}]\n")
	if err := os.WriteFile(replacement, replacementBody, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, "secret", "set", "--name", "shared-ceph-external-details", "--raw-file", replacement)
	if code == 0 {
		t.Fatal("secret set overwrite without --yes unexpectedly succeeded")
	}
	if !strings.Contains(stdout, "Replace secret shared-ceph-external-details") {
		t.Fatalf("stdout missing secret overwrite prompt: %q", stdout)
	}
	if !strings.Contains(stderr, "secret set aborted") {
		t.Fatalf("stderr missing secret set abort: %q", stderr)
	}
	got, err = secret.NewContextStore(ctx.Name, ctx.SecretsDir).Read(secret.MaterialKey{Name: "shared-ceph-external-details", Role: secret.MaterialPrimary})
	if err != nil {
		t.Fatalf("read raw secret after aborted overwrite: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("aborted overwrite changed raw secret = %q, want %q", got, body)
	}

	stdout, stderr, code = runCLIWithInput(t, "y\n", "secret", "set", "--name", "shared-ceph-external-details", "--raw-file", replacement)
	if code != 0 {
		t.Fatalf("secret set overwrite confirmation exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	got, err = secret.NewContextStore(ctx.Name, ctx.SecretsDir).Read(secret.MaterialKey{Name: "shared-ceph-external-details", Role: secret.MaterialPrimary})
	if err != nil {
		t.Fatalf("read raw secret after confirmed overwrite: %v", err)
	}
	if string(got) != string(replacementBody) {
		t.Fatalf("confirmed overwrite raw secret = %q, want %q", got, replacementBody)
	}
}

func TestSecretSetRawFileRejectsConflictingInputModes(t *testing.T) {
	setTestHomeAndRoot(t)
	source := filepath.Join(t.TempDir(), "external-details.json")
	if err := os.WriteFile(source, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "secret", "set", "--name", "shared-ceph-external-details", "--raw-file", source, "--from-file", source)
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
	if len(rootArgs) < 13 || rootArgs[0] != "-n" || rootArgs[1] != "env" || !strings.HasPrefix(rootArgs[2], workspace.InternalRegistryEnv+"=") || rootArgs[3] != localroot.InternalEnv+"=1" || !strings.HasPrefix(rootArgs[4], secret.InternalCallerHomeEnv+"=") || !strings.HasPrefix(rootArgs[5], localroot.CallerPathEnv+"=") || !strings.HasPrefix(rootArgs[6], localRootSudoAuthEnv+"=") {
		os.Exit(2)
	}
	rootArgs = rootArgs[8:]
	if len(rootArgs) != 6 || rootArgs[0] != "secret" || rootArgs[1] != "set" || rootArgs[2] != "--name" || rootArgs[3] != "openshift-pull-secret" || rootArgs[4] != "--pull-secret" {
		os.Exit(2)
	}
	if rootArgs[5] == os.Getenv("BOOTWRIGHT_SECRET_SET_SOURCE") {
		os.Exit(2)
	}
	data, err := os.ReadFile(rootArgs[5])
	if err != nil || string(data) != `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}` {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestContextInitPassesWorkspacePathAndSyncsRegistryAroundSudo(t *testing.T) {
	setTestHomeAndRoot(t)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "environment.yaml"), []byte("apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: lab\nspec:\n  domains:\n    base: example.test\n"), 0o600); err != nil {
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

	stdout, stderr, code := runCLI(t, "context", "init", "--name", "lab", "-f", source)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if gotName != "sudo" {
		t.Fatalf("command name = %q, want sudo", gotName)
	}
	if !slices.Contains(gotArgs, source) {
		t.Fatalf("sudo args missing resolved workspace path %q: %v", source, gotArgs)
	}
	registry, err := workspace.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := workspace.Load(registry)
	if err != nil {
		t.Fatal(err)
	}
	if store.Current != "lab" {
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
	if len(rootArgs) < 13 || rootArgs[0] != "-n" || rootArgs[1] != "env" || !strings.HasPrefix(rootArgs[2], workspace.InternalRegistryEnv+"=") || rootArgs[3] != localroot.InternalEnv+"=1" || !strings.HasPrefix(rootArgs[4], secret.InternalCallerHomeEnv+"=") || !strings.HasPrefix(rootArgs[5], localroot.CallerPathEnv+"=") || !strings.HasPrefix(rootArgs[6], localRootSudoAuthEnv+"=") {
		os.Exit(2)
	}
	registry := strings.TrimPrefix(rootArgs[2], workspace.InternalRegistryEnv+"=")
	rootArgs = rootArgs[8:]
	if rootArgs[0] != "context" || rootArgs[1] != "init" || rootArgs[2] != "--name" || rootArgs[3] != "lab" || rootArgs[4] != "-f" {
		os.Exit(2)
	}
	if _, err := os.Stat(filepath.Join(rootArgs[5], "environment.yaml")); err != nil {
		os.Exit(2)
	}
	if registry == "" {
		os.Exit(2)
	}
	if err := workspace.Save(registry, workspace.Store{Current: "lab"}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestContextUseSyncsRegistryAroundSudo(t *testing.T) {
	setTestHomeAndRoot(t)
	previous := localRootGate
	defer func() { localRootGate = previous }()

	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 1000 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			helperArgs := append([]string{"-test.run=TestContextRegistrySyncRootHelperProcess", "--"}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_CONTEXT_REGISTRY_SYNC_HELPER=1")
			return cmd
		},
	}

	stdout, stderr, code := runCLI(t, "context", "use", "--name", "lab")
	if code != 0 {
		t.Fatalf("context use exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	registry, err := workspace.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := workspace.Load(registry)
	if err != nil {
		t.Fatal(err)
	}
	if store.Current != "lab" {
		t.Fatalf("registry current = %q, want lab", store.Current)
	}
}

func TestContextDeletePurgeSyncsRegistryAroundSudo(t *testing.T) {
	setTestHomeAndRoot(t)
	saveTestContextRegistry(t, "lab")
	previous := localRootGate
	defer func() { localRootGate = previous }()

	localRootGate = localRootGateDeps{
		enabled:    true,
		geteuid:    func() int { return 1000 },
		executable: func() (string, error) { return "/usr/local/bin/bootwright", nil },
		commandContext: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			helperArgs := append([]string{"-test.run=TestContextRegistrySyncRootHelperProcess", "--"}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
			cmd.Env = append(os.Environ(), "BOOTWRIGHT_CONTEXT_REGISTRY_SYNC_HELPER=1")
			return cmd
		},
	}

	stdout, stderr, code := runCLI(t, "context", "delete", "--name", "lab", "--purge", "--yes")
	if code != 0 {
		t.Fatalf("context delete --purge exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	registry, err := workspace.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := workspace.Load(registry)
	if err != nil {
		t.Fatal(err)
	}
	if store.Current != "" {
		t.Fatalf("registry current = %q, want cleared", store.Current)
	}
}

func TestContextDeletePurgeElevatesBeforeReadingContextDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-denied stat check requires non-root test process")
	}
	setTestHomeAndRoot(t)
	ctx, err := workspace.NewContext("prd")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureBaseDir(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ctx.BaseDir, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(ctx.BaseDir, 0o700)

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

	stdout, stderr, code := runCLI(t, "context", "delete", "--name", "prd", "--purge", "--yes")
	if code != 0 {
		t.Fatalf("context delete --purge with an unreadable context dir exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !called {
		t.Fatal("expected sudo elevation to be attempted before reading the context directory")
	}
}

func TestContextRegistrySyncRootHelperProcess(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_CONTEXT_REGISTRY_SYNC_HELPER") != "1" {
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
	if len(rootArgs) < 10 || rootArgs[0] != "-n" || rootArgs[1] != "env" || !strings.HasPrefix(rootArgs[2], workspace.InternalRegistryEnv+"=") {
		os.Exit(2)
	}
	registry := strings.TrimPrefix(rootArgs[2], workspace.InternalRegistryEnv+"=")
	command := rootArgs[8:]
	switch {
	case reflect.DeepEqual(command, []string{"context", "use", "--name", "lab"}):
		if err := workspace.Save(registry, workspace.Store{Current: "lab"}); err != nil {
			os.Exit(2)
		}
	case reflect.DeepEqual(command, []string{"context", "delete", "--name", "lab", "--purge", "--yes"}):
		if err := workspace.Save(registry, workspace.Store{}); err != nil {
			os.Exit(2)
		}
	default:
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
	replaceInFile(t, filepath.Join(dir, "environment.yaml"), "    base: bootwright.test\n\n", `    base: bootwright.test

  resources:
    - secrets.yaml
    - service-machines.yaml
    - networks.yaml
    - provider.yaml
    - infra-component.yaml
    - cluster-machines.yaml
    - container-cluster.yaml

`)
}

func TestRenderOutputDirRequiresSensitive(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	outputDir := filepath.Join(t.TempDir(), "rendered")
	stdout, stderr, code := runCLI(t, "render", "--output-dir", outputDir, "--clusters", "sno-libvirt")
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

	_, _, code = runCLI(t, "render", "--output-dir", outputDir, "--clusters", "sno-libvirt", "--resolve-secrets")
	if code == 0 {
		t.Fatal("render --output-dir accepted removed --resolve-secrets flag")
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("render --output-dir with removed --resolve-secrets wrote %s: %v", outputDir, err)
	}

	_, stderr, code = runCLI(t, "render", "--output-dir", outputDir, "--clusters", "sno-libvirt", "--sensitive", "--resolve-secrets")
	if code == 0 {
		t.Fatal("render --output-dir accepted removed --resolve-secrets flag together with --sensitive")
	}
	if !strings.Contains(stderr, "unknown flag: --resolve-secrets") {
		t.Fatalf("stderr missing unknown flag for removed --resolve-secrets:\n%s", stderr)
	}

	_, stderr, code = runCLI(t, "render", "installer", "--clusters", "sno-libvirt", "--resolve-secrets")
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
	if err := os.WriteFile(filepath.Join(sshDir, "bootwright-ssh-key"), []byte("FAKE PRIVATE KEY FOR TESTS\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "bootwright-ssh-key.pub"), []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForTests\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pullSecret := filepath.Join(t.TempDir(), "pull-secret.json")
	if err := os.WriteFile(pullSecret, []byte(`{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, "secret", "set", "--name", "openshift-pull-secret", "--pull-secret", pullSecret)
	if code != 0 {
		t.Fatalf("secret set exited %d, stderr=%q", code, stderr)
	}
	genOut, stderr, code := runCLI(t, "secret", "generate")
	if code != 0 {
		t.Fatalf("secret generate exited %d, stderr=%q\nstdout:\n%s", code, stderr, genOut)
	}
	if !strings.Contains(genOut, "request(s) handled") || !strings.Contains(genOut, "all declared secrets present") {
		t.Fatalf("secret generate stdout missing success summary:\n%s", genOut)
	}
	outputDir := filepath.Join(t.TempDir(), "rendered")
	stdout, stderr, code := runCLI(t, "render", "--output-dir", outputDir, "--clusters", "sno-libvirt", "--sensitive")
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

	_, stderr, code := runCLI(t, "render", "--output-dir", outputDir, "--clusters", "sno-libvirt", "--sensitive")
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
		if got := env[key]; got != "/var/tmp" {
			t.Fatalf("%s = %q, want /var/tmp", key, got)
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
	if !strings.Contains(msg, "no enabled repositories") {
		t.Fatalf("error missing captured command output:\n%s", msg)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want command output captured in error", got)
	}
}

func TestRunBootstrapPlanSuppressesSuccessfulStepOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	fakePip := filepath.Join(dir, "pip")
	if err := os.WriteFile(fakePip, []byte(`#!/bin/sh
printf '%s\n' 'Collecting ansible-core==2.20.7'
printf '%s\n' 'Installing collected packages: ansible-core'
printf '%s\n' '[notice] A new release of pip is available' >&2
`), 0o755); err != nil {
		t.Fatalf("write fake pip: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runBootstrapPlan(
		context.Background(),
		strings.NewReader("unused\n"),
		&stdout,
		&stderr,
		[]bastion.BootstrapStep{{Label: "install ansible-core==2.20.7 into venv", Cmd: []string{fakePip, "install", "ansible-core==2.20.7"}}},
		nil,
		"",
		false,
	)
	if err != nil {
		t.Fatalf("runBootstrapPlan: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if got := stdout.String(); strings.Contains(got, "Collecting ansible-core") || strings.Contains(got, "Installing collected packages") || !strings.Contains(got, "[OK] Ansible runtime: ready") {
		t.Fatalf("stdout should hide bootstrap step output and show runtime status:\n%s", got)
	}
	if got := stderr.String(); strings.Contains(got, "new release of pip") {
		t.Fatalf("stderr should hide bootstrap step output:\n%s", got)
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
	if got := stdout.String(); strings.Contains(got, "$ sudo") || strings.Contains(got, "first") || strings.Contains(got, "second") || !strings.Contains(got, "[OK] Ansible runtime: ready") {
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
	if got := stdout.String(); strings.Contains(got, "$ sudo") || strings.Contains(got, "ok") || !strings.Contains(got, "[OK] Ansible runtime: ready") {
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
	logPath := filepath.Join(dir, "runs", "bastion", "setup", "ansible-output.log")
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
		logPath,
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
	if _, statErr := os.Stat(logPath); statErr != nil {
		t.Fatalf("controller CLI install log not written to %q: %v", logPath, statErr)
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
		report, _, err := buildStatusReport(testCommonFlags(t, t.TempDir(), "001-sno-libvirt"))
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
		report, _, err := buildStatusReport(cf)
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
		report, _, err := buildStatusReport(cf)
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
		report, _, err := buildStatusReport(cf)
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
		report, _, err := buildStatusReport(cf)
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
	for _, want := range []string{"Current apply", "apply-test", "Progress", "Shared services", "Prerequisites", "[RUNNING] Cluster install", "bootwright status --watch"} {
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
	for _, item := range []struct {
		name string
		role secret.MaterialRole
		body string
	}{
		{name: "openshift-pull-secret", role: secret.MaterialPrimary, body: `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`},
		{name: "3-nodes-ocp-baremetal-cluster-admin-ssh-key", role: secret.MaterialSSHPrivate, body: "fake-private-key\n"},
		{name: "3-nodes-ocp-baremetal-cluster-admin-ssh-key", role: secret.MaterialSSHPublic, body: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForApplyTest\n"},
		{name: "bmc-credentials", role: secret.MaterialPrimary, body: "admin:password\n"},
		{name: "proxy-credentials", role: secret.MaterialPrimary, body: "proxy:password\n"},
	} {
		writeTestContextSecret(t, ctx.Name, ctx.SecretsDir, item.name, item.role, []byte(item.body))
	}
	secrets := map[string]string{
		filepath.Join(sshDir, "bootwright-ssh-key"):     "fake-private-key\n",
		filepath.Join(sshDir, "bootwright-ssh-key.pub"): "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForApplyTest\n",
	}
	for path, body := range secrets {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	hostFingerprint, err := sshtrust.FingerprintSHA256(hostTrustKeyA)
	if err != nil {
		t.Fatalf("host trust fingerprint: %v", err)
	}
	if err := sshtrust.Save(sshtrust.DirForContext(ctx.BaseDir), sshtrust.Store{Hosts: []sshtrust.HostRecord{{
		Name:              "bastion",
		Address:           "bastion.bootwright.test",
		KeyType:           "ssh-ed25519",
		PublicKey:         hostTrustKeyA,
		FingerprintSHA256: hostFingerprint,
		KnownHostsLine:    sshtrust.KnownHostsLine("bastion.bootwright.test", "ssh-ed25519", hostTrustKeyA),
	}}}); err != nil {
		t.Fatalf("seed host trust: %v", err)
	}
	fakeBin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
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
		HashSchema:  workflow.ConvergeHashSchema,
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

	stdout, stderr, code := runCLI(t, "apply", "--stage", "clusters", "--clusters", clusterName, "--yes", "--ask-become-pass=false")
	if code == 0 {
		t.Fatalf("apply --stage clusters unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "different install inputs") {
		t.Fatalf("apply --stage clusters stderr missing install mismatch:\n%s", stderr)
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
	stdout, stderr, code := runCLI(t, "apply", "--stage", "clusters", "--clusters", "3-nodes-ocp-baremetal", "--dry-run", "--output", "json", "--ask-become-pass=true")
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
	if bootTask.ClusterLogPath == "" || !strings.Contains(bootTask.ClusterLogPath, filepath.Join("runs", "history", "dry-run", "bootwright-3-nodes-ocp-baremetal.log")) {
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

func writeTestContextSecret(t *testing.T, contextName, secretsDir, name string, role secret.MaterialRole, data []byte) {
	t.Helper()
	store := secret.NewContextStore(contextName, secretsDir)
	if err := store.Write(secret.MaterialKey{Name: name, Role: role}, data); err != nil {
		t.Fatalf("write encrypted %s/%s: %v", name, role, err)
	}
}

func TestApplyFullGraphDryRunJSONPlansAddonTasks(t *testing.T) {
	initTestContextWithClusterAddon(t)

	stdout, stderr, code := runCLI(t, "apply", "--dry-run", "--output", "json")
	if code != 0 {
		t.Fatalf("apply dry-run json exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode apply dry-run json: %v\n%s", err, stdout)
	}
	if report.Target != "all" {
		t.Fatalf("target = %q, want all", report.Target)
	}
	if report.ApplyPlan == nil {
		t.Fatalf("apply plan missing from report: %+v", report)
	}
	gotIDs := make([]string, 0, len(report.ApplyPlan.Tasks))
	for _, task := range report.ApplyPlan.Tasks {
		gotIDs = append(gotIDs, task.ID)
	}
	wantIDs := []string{
		"provider.bastion",
		"infra-component.bastion",
		"infraprepare.sno-libvirt.bastion",
		"infra.sno-libvirt.master-0",
		"infrafinalize.sno-libvirt.bastion",
		"iso.sno-libvirt",
		"boot.sno-libvirt",
		"wait.sno-libvirt",
		"addon.sno-libvirt.openshift-virtualization",
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

	stdout, stderr, code := runCLI(t, "apply", "--stage", "clusters", "--dry-run", "--output", "json")
	if code != 0 {
		t.Fatalf("apply --stage clusters dry-run json exited %d, stderr=%q", code, stderr)
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
		"iso.sno-libvirt",
		"boot.sno-libvirt",
		"wait.sno-libvirt",
		"addon.sno-libvirt.openshift-virtualization",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("apply --stage clusters task IDs = %v, want %v", gotIDs, wantIDs)
	}
	if got := report.ApplyPlan.Addons; len(got) != 1 || got[0].Cluster != "sno-libvirt" || got[0].Addon != "openshift-virtualization" {
		t.Fatalf("addon plan = %+v, want sno-libvirt openshift-virtualization", got)
	}
	if got := report.ApplyPlan.Addons[0].Resources; len(got) != 3 || got[2].Kind != "HyperConverged" {
		t.Fatalf("addon resources = %+v, want generated OLM resources ending with HyperConverged", got)
	}
	assertTaskDeps(t, report.ApplyPlan.Tasks, "iso.sno-libvirt")
	assertTaskDeps(t, report.ApplyPlan.Tasks, "addon.sno-libvirt.openshift-virtualization", "wait.sno-libvirt")
}

func TestApplyClustersDryRunJSONAcceptsMixedClusterSelection(t *testing.T) {
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "mixed", "-f", example)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, "apply", "--stage", "clusters", "--clusters", "dc1-metal-ocp,ceph-storage", "--dry-run", "--output", "json", "--ask-become-pass=false")
	if code != 0 {
		t.Fatalf("apply mixed clusters dry-run json exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode apply dry-run json: %v\n%s", err, stdout)
	}
	if report.Target != "clusters" {
		t.Fatalf("target = %q, want clusters", report.Target)
	}
	gotIDs := make([]string, 0, len(report.ApplyPlan.Tasks))
	for _, task := range report.ApplyPlan.Tasks {
		gotIDs = append(gotIDs, task.ID)
	}
	for _, want := range []string{"storageinfra.ceph-storage", "storage.ceph-storage", "iso.dc1-metal-ocp", "wait.dc1-metal-ocp"} {
		if !slices.Contains(gotIDs, want) {
			t.Fatalf("mixed apply task IDs missing %s: %v", want, gotIDs)
		}
	}
	for _, reject := range []string{"iso.dc2-metal-ocp"} {
		if slices.Contains(gotIDs, reject) {
			t.Fatalf("mixed apply task IDs unexpectedly include %s: %v", reject, gotIDs)
		}
	}
}

func TestApplyClustersDryRunJSONContainerOnlyDoesNotProvisionAttachedStorage(t *testing.T) {
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "container-only", "-f", example)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, "apply", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--dry-run", "--output", "json", "--ask-become-pass=false")
	if code != 0 {
		t.Fatalf("apply container-only dry-run json exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode apply dry-run json: %v\n%s", err, stdout)
	}
	gotIDs := make([]string, 0, len(report.ApplyPlan.Tasks))
	for _, task := range report.ApplyPlan.Tasks {
		gotIDs = append(gotIDs, task.ID)
	}
	for _, want := range []string{"iso.dc1-metal-ocp", "wait.dc1-metal-ocp"} {
		if !slices.Contains(gotIDs, want) {
			t.Fatalf("container-only apply task IDs missing %s: %v", want, gotIDs)
		}
	}
	for _, reject := range []string{"storage.ceph-storage", "storageinfra.ceph-storage"} {
		if slices.Contains(gotIDs, reject) {
			t.Fatalf("container-only apply must not provision the render-reference Ceph; task IDs include %s: %v", reject, gotIDs)
		}
	}
}

func TestDestroyClustersDryRunJSONAcceptsMixedClusterSelection(t *testing.T) {
	setTestHomeAndRoot(t)
	example := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "mixed-destroy", "-f", example)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, "destroy", "--stage", "clusters", "--clusters", "dc1-metal-ocp,dc2-metal-ocp,dc1-child-ocp,dc2-child-ocp,ceph-storage", "--dry-run", "--output", "json", "--ask-become-pass=false")
	if code != 0 {
		t.Fatalf("destroy mixed clusters dry-run json exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode destroy dry-run json: %v\n%s", err, stdout)
	}
	if report.Target != "clusters" || report.DestroyPlan == nil {
		t.Fatalf("destroy target = %q with plan %+v, want clusters with a task plan", report.Target, report.DestroyPlan)
	}
	if !reflect.DeepEqual(report.Phases, []string{"deps", "base", "add-ons"}) {
		t.Fatalf("phases = %#v, want full clusters destroy scope", report.Phases)
	}
	varsData, err := os.ReadFile(report.Render.VarsPath)
	if err != nil {
		t.Fatalf("read rendered vars: %v", err)
	}
	vars := string(varsData)
	for _, want := range []string{"name: dc1-metal-ocp", "name: ceph-storage"} {
		if !strings.Contains(vars, want) {
			t.Fatalf("destroy rendered vars missing %q:\n%s", want, vars)
		}
	}
}

func TestApplyClustersOverrideDryRunPassesApplyMode(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "apply", "--stage", "clusters", "--dry-run", "--output", "json", "--converge-drifted")
	if code != 0 {
		t.Fatalf("apply --stage clusters override dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode apply dry-run json: %v\n%s", err, stdout)
	}
	if !slices.Contains(report.ExtraVars, "bootwright_apply_mode=override") {
		t.Fatalf("extra vars missing override apply mode: %+v", report.ExtraVars)
	}
	if !slices.Contains(report.Command, "bootwright_apply_mode=override") {
		t.Fatalf("command missing override apply mode: %+v", report.Command)
	}
}

func TestApplyOverrideDoesNotBypassActiveRunLease(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	now := time.Now().UTC()
	ledger := workflow.NewRunLedger("apply-active", "clusters", "", workflow.ConcurrencyLimits{}, nil, now)
	if err := workflow.SaveRunLedger(ctx.RunsDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}
	if err := workflow.SaveRunLease(ctx.RunsDir, workflow.NewRunLease("apply-active", now)); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}

	stdout, stderr, code := runCLI(t,
		"apply", "--stage", "clusters",
		"--clusters", "sno-libvirt",
		"--converge-drifted",
		"--yes",
		"--ask-become-pass=false",
	)
	if code == 0 {
		t.Fatalf("apply --converge-drifted unexpectedly bypassed the active run lease\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "apply run apply-active is still running") {
		t.Fatalf("apply --converge-drifted stderr missing active-run error:\n%s", stderr)
	}
	if strings.Contains(stdout, "Bundle") || strings.Contains(stdout, "Workflow") {
		t.Fatalf("apply --converge-drifted progressed to workflow despite the active lease\nstdout:\n%s", stdout)
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
	return runCLIWithInput(t, "", args...)
}

func runCLIWithInput(t *testing.T, input string, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, strings.NewReader(input), &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func setTestHomeAndRoot(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Cleanup(workspace.SetRootDirForTest(filepath.Join(home, "bootwright-root")))
	return home
}

func saveTestContextRegistry(t *testing.T, current string) {
	t.Helper()
	registry, err := workspace.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.Save(registry, workspace.Store{Current: current}); err != nil {
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
  addonRefs:
    - openshift-virtualization
`), 0o600); err != nil {
		t.Fatalf("write addon profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "cluster-addon-binding.yaml"), []byte(`apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: sno-libvirt-addons
spec:
  clusterRef: sno-libvirt
  profileRefs:
    - virtualization-platform
`), 0o600); err != nil {
		t.Fatalf("write addon binding: %v", err)
	}
	if stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", inputDir); code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func cliTestClusterAddonYAML() string {
	return `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: openshift-virtualization
spec:
  type: olm
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
      - csvSucceeded:
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
	if len(args) < 8 || args[0] != "-n" || args[1] != "env" || !strings.HasPrefix(args[2], workspace.InternalRegistryEnv+"=") || args[3] != localroot.InternalEnv+"=1" || args[4] != secret.InternalCallerHomeEnv+"="+home || args[5] != localroot.CallerPathEnv+"="+os.Getenv("PATH") || args[6] != localRootSudoAuthEnv+"="+auth {
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

func initTestContext(t *testing.T, fixtureName string) workspace.Context {
	t.Helper()
	workspaceDir := copyFixtureYAML(t, fixtureName)
	setTestHomeAndRoot(t)
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", workspaceDir)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func initProtectedTestContext(t *testing.T, fixtureName string) workspace.Context {
	t.Helper()
	setTestHomeAndRoot(t)
	inputDir := copyFixtureYAML(t, fixtureName)
	path := filepath.Join(inputDir, "environment.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read environment fixture: %v", err)
	}
	body := strings.Replace(string(data),
		"  domains:",
		"  safety:\n    destroyProtection: "+v1alpha1.EnvironmentDestroyProtectionRequiredOverride+"\n  domains:", 1)
	if body == string(data) {
		t.Fatal("environment fixture did not contain spec.domains")
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write protected environment fixture: %v", err)
	}
	stdout, stderr, code := runCLI(t, "context", "init", "--name", "test", "-f", inputDir)
	if code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func testCommonFlags(t *testing.T, rootDir, fixtureName string) *commonFlags {
	t.Helper()
	t.Cleanup(workspace.SetRootDirForTest(rootDir))
	ctx, err := workspace.NewContext("test")
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

func TestArgsRequestJSONHonorsFlagTerminator(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"space form", []string{"status", "--output", "json"}, true},
		{"equals form", []string{"status", "--output=json"}, true},
		{"text output", []string{"status", "--output", "text"}, false},
		{"no output flag", []string{"status"}, false},
		{"after terminator", []string{"status", "--", "--output", "json"}, false},
		{"before terminator", []string{"status", "--output", "json", "--", "x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := argsRequestJSON(tc.args); got != tc.want {
				t.Fatalf("argsRequestJSON(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestMediaAddReplaceIsSingleGate(t *testing.T) {
	setTestHomeAndRoot(t)

	help, stderr, code := runCLI(t, "media", "add", "--help")
	if code != 0 {
		t.Fatalf("media add --help exited %d, stderr=%q", code, stderr)
	}
	if strings.Contains(help, "--force") {
		t.Fatalf("media add must not document --force:\n%s", help)
	}
	if !strings.Contains(help, "--yes") {
		t.Fatalf("media add must document --yes:\n%s", help)
	}

	storeDir, err := media.EnsureStoreDir()
	if err != nil {
		t.Fatalf("ensure media store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "rhel.iso"), []byte("old"), 0o600); err != nil {
		t.Fatalf("seed existing media: %v", err)
	}
	src := filepath.Join(t.TempDir(), "src.iso")
	if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
		t.Fatalf("write source iso: %v", err)
	}

	_, stderr, code = runCLIWithInput(t, "n\n", "media", "add", "--name", "rhel.iso", "--from-file", src)
	if code == 0 {
		t.Fatalf("declined media replace must fail; stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "aborted") {
		t.Fatalf("declined media replace must report aborted; stderr=%q", stderr)
	}

	_, stderr, code = runCLI(t, "media", "add", "--name", "rhel.iso", "--from-file", src, "--yes")
	if code != 0 {
		t.Fatalf("media add replace with --yes exited %d, stderr=%q", code, stderr)
	}
}
