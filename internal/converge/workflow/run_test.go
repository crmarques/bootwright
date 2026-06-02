package workflow

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/render"
)

// fakeRunner satisfies ansible.Runner without actually exec'ing.
type fakeRunner struct {
	runCalled  bool
	command    []string
	runReturns error
	lastSpec   ansible.RunSpec
}

type fakeReporter struct {
	renderStarted          bool
	resolveInstallerCalled bool
	dryRunLabel            string
	skipLabel              string
	skipLimit              string
	ansibleExecutable      string
}

func (f *fakeReporter) RenderStart()                           { f.renderStarted = true }
func (f *fakeReporter) ResolveInstallerStart()                 { f.resolveInstallerCalled = true }
func (f *fakeReporter) DryRunCommand(label string, _ []string) { f.dryRunLabel = label }
func (f *fakeReporter) SkipNoHosts(label string, limit string) {
	f.skipLabel = label
	f.skipLimit = limit
}
func (f *fakeReporter) AnsibleStart(executable string) { f.ansibleExecutable = executable }

func (f *fakeRunner) Run(_ context.Context, spec ansible.RunSpec) error {
	f.runCalled = true
	f.lastSpec = spec
	f.command = f.Command(spec)
	return f.runReturns
}

func (f *fakeRunner) Command(spec ansible.RunSpec) []string {
	executable := spec.Executable
	if executable == "" {
		executable = "ansible-playbook"
	}
	return []string{executable, "-i", spec.Inventory, spec.Playbook}
}

// minimalState is the smallest state that satisfies render.All without
// triggering provider-closure validation in the installer renderer. It has
// no ContainerClusters (so no installer renders) and an empty ClusterInfra list
// (so the per-cluster vars loop is a no-op). Sufficient for exercising the
// dispatch / dry-run / runner-execution paths of workflow.Run.
func minimalState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec:     v1alpha1.EnvironmentSpec{BaseDomain: "example.test"},
		}},
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "bastion"},
			Spec: v1alpha1.HostSpec{
				Addresses: []v1alpha1.HostAddress{{Name: "ssh", Address: "bastion.example.test"}},
				SSH:       &v1alpha1.HostSSHSpec{AddressName: "ssh"},
			},
		}},
	}
}

func TestRunDryRunDoesNotInvokeRunner(t *testing.T) {
	renderedDir := t.TempDir()
	runner := &fakeRunner{}
	reporter := &fakeReporter{}
	result, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        renderedDir,
		ClustersDir:        t.TempDir(),
		RunsDir:            t.TempDir(),
		SecretsDir:         t.TempDir(),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   "/var/lib/bootwright/provider-state",
		Executable:         "ansible-playbook",
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.check_preflight",
		ArtifactsBaseName:  "preflight-test",
		DryRun:             true,
		Label:              "test check",
	}, runner, reporter)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runner.runCalled {
		t.Fatal("dry-run must not invoke runner.Run")
	}
	if result.Render.VarsPath == "" {
		t.Fatal("render should still emit a VarsPath even on dry-run")
	}
	if reporter.dryRunLabel != "test check" {
		t.Fatalf("dry-run label = %q, want test check", reporter.dryRunLabel)
	}
	if len(result.Command) == 0 || result.Command[0] != "ansible-playbook" {
		t.Fatalf("expected command[0]=ansible-playbook, got %v", result.Command)
	}
}

func TestRunExecutesRunnerWhenNotDryRun(t *testing.T) {
	renderedDir := t.TempDir()
	runner := &fakeRunner{}
	reporter := &fakeReporter{}
	if _, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        renderedDir,
		ClustersDir:        t.TempDir(),
		RunsDir:            t.TempDir(),
		SecretsDir:         t.TempDir(),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   "/var/lib/bootwright/provider-state",
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.workflow_infra_apply",
		ArtifactsBaseName:  "infra",
		DryRun:             false,
	}, runner, reporter); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !runner.runCalled {
		t.Fatal("non-dry-run must invoke runner.Run")
	}
	if reporter.dryRunLabel != "" {
		t.Fatalf("non-dry-run must not report dry-run, got %q", reporter.dryRunLabel)
	}
}

func TestRunPassesControllingTTYToRunner(t *testing.T) {
	runner := &fakeRunner{}
	_, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        t.TempDir(),
		ClustersDir:        t.TempDir(),
		RunsDir:            t.TempDir(),
		SecretsDir:         t.TempDir(),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   "/var/lib/bootwright/provider-state",
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.workflow_clusters_apply",
		ArtifactsBaseName:  "clusters",
		UseControllingTTY:  true,
	}, runner, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !runner.lastSpec.UseControllingTTY {
		t.Fatal("Run must pass UseControllingTTY through to ansible.RunSpec")
	}
}

func TestRunPassesForksToRunner(t *testing.T) {
	runner := &fakeRunner{}
	_, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        t.TempDir(),
		ClustersDir:        t.TempDir(),
		RunsDir:            t.TempDir(),
		SecretsDir:         t.TempDir(),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   "/var/lib/bootwright/provider-state",
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.workflow_clusters_apply",
		ArtifactsBaseName:  "clusters",
		Forks:              9,
	}, runner, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runner.lastSpec.Forks != 9 {
		t.Fatalf("RunSpec.Forks = %d, want 9", runner.lastSpec.Forks)
	}
}

func TestRunPassesBecomePasswordFileToRunner(t *testing.T) {
	runner := &fakeRunner{}
	_, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        t.TempDir(),
		ClustersDir:        t.TempDir(),
		RunsDir:            t.TempDir(),
		SecretsDir:         t.TempDir(),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   "/var/lib/bootwright/provider-state",
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.workflow_clusters_apply",
		ArtifactsBaseName:  "clusters",
		BecomePasswordFile: "/tmp/bootwright-become",
	}, runner, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runner.lastSpec.BecomePasswordFile != "/tmp/bootwright-become" {
		t.Fatalf("BecomePasswordFile = %q, want /tmp/bootwright-become", runner.lastSpec.BecomePasswordFile)
	}
}

func TestRunPropagatesRunnerError(t *testing.T) {
	runner := &fakeRunner{runReturns: errors.New("boom")}
	_, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        t.TempDir(),
		ClustersDir:        t.TempDir(),
		RunsDir:            t.TempDir(),
		SecretsDir:         t.TempDir(),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   "/var/lib/bootwright/provider-state",
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.workflow_infra_apply",
		ArtifactsBaseName:  "infra",
	}, runner, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Run must propagate runner error, got %v", err)
	}
}

func TestRunDryRunLabelFallsBackToPlaybook(t *testing.T) {
	runner := &fakeRunner{}
	reporter := &fakeReporter{}
	_, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        t.TempDir(),
		ClustersDir:        t.TempDir(),
		RunsDir:            t.TempDir(),
		SecretsDir:         t.TempDir(),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   "/var/lib/bootwright/provider-state",
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.check_preflight",
		ArtifactsBaseName:  "preflight-test",
		DryRun:             true,
		// Label intentionally empty.
	}, runner, reporter)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reporter.dryRunLabel != "bootwright.core.check_preflight" {
		t.Fatalf("empty Label should fall back to Playbook, got %q", reporter.dryRunLabel)
	}
}

func TestRunRenderFailureSurfaces(t *testing.T) {
	// Pointing RenderedDir at a path that already exists as a *file* causes
	// MkdirAll to fail with a recognizable error.
	stateFile := t.TempDir() + "/not-a-dir"
	if err := writeStub(stateFile); err != nil {
		t.Fatalf("setup: %v", err)
	}
	runner := &fakeRunner{}
	_, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        stateFile, // file, not directory
		ClustersDir:        t.TempDir(),
		RunsDir:            t.TempDir(),
		SecretsDir:         t.TempDir(),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   "/var/lib/bootwright/provider-state",
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.check_preflight",
		ArtifactsBaseName:  "preflight-test",
	}, runner, nil)
	if err == nil {
		t.Fatal("Run should fail when RenderedDir is unwritable")
	}
	if runner.runCalled {
		t.Fatal("runner should not be invoked when render fails")
	}
}

func writeStub(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// TestRunSkipsAnsibleWhenLimitMatchesNoHosts pins the test-002 fix:
// when `--limit` names only renderer-emitted groups that are empty for
// this state, workflow.Run must skip the runner and surface
// Skipped=true instead of letting ansible abort with "no hosts to
// target".
func TestRunSkipsAnsibleWhenLimitMatchesNoHosts(t *testing.T) {
	runner := &fakeRunner{}
	reporter := &fakeReporter{}
	result, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        t.TempDir(),
		ClustersDir:        t.TempDir(),
		RunsDir:            t.TempDir(),
		SecretsDir:         t.TempDir(),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   "/var/lib/bootwright/provider-state",
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.workflow_infra_apply",
		Limit:              render.GroupProviderHosts + ":" + render.GroupInfraComponentHosts + ":" + render.GroupInfraHosts,
		ArtifactsBaseName:  "infra",
		Label:              "infra apply",
	}, runner, reporter)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runner.runCalled {
		t.Fatal("Run must skip the runner when --limit matches no hosts")
	}
	if !result.Skipped {
		t.Fatal("RunResult.Skipped must be true on the skip path")
	}
	if reporter.skipLabel != "infra apply" || reporter.skipLimit == "" {
		t.Fatalf("expected skip report, got label=%q limit=%q", reporter.skipLabel, reporter.skipLimit)
	}
}

// TestLimitMatchesNoHostsTable exercises the parser without touching
// disk. Empty/whitespace limits ⇒ false (no --limit set, full
// inventory is in play). Limits naming any non-empty group ⇒ false.
// Limits naming only empty/unknown groups ⇒ true.
func TestLimitMatchesNoHostsTable(t *testing.T) {
	state := minimalState() // no Hosts, no ClusterInfras
	tests := []struct {
		name  string
		limit string
		want  bool
	}{
		{"empty limit", "", false},
		{"whitespace limit", "   ", false},
		{"ocp group has the localhost controller", render.GroupOCPHosts, false},
		{"only empty groups", render.GroupProviderHosts + ":" + render.GroupInfraComponentHosts + ":" + render.GroupInfraHosts, true},
		{"unknown group", "no_such_group", true},
		{"mix of empty and non-empty", render.GroupProviderHosts + ":" + render.GroupOCPHosts, false},
		{"literal inventory host", "lab-host", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testState := state
			if tc.limit == "lab-host" {
				testState = stateWithInfraHost("lab-host")
			}
			if got := LimitMatchesNoHosts(tc.limit, testState); got != tc.want {
				t.Fatalf("LimitMatchesNoHosts(%q) = %v, want %v", tc.limit, got, tc.want)
			}
		})
	}
}

func stateWithInfraHost(hostName string) v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "provider"},
			Spec: v1alpha1.InfraProviderSpec{
				MachineProfiles: []v1alpha1.MachineProfileCapability{{
					Name: "profile",
					Libvirt: &v1alpha1.MachineProfileLibvirtProvisioner{
						HostRef: v1alpha1.LocalObjectReference{Name: hostName},
					},
				}},
			},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "infra"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					Machines: []v1alpha1.ClusterMachineComponent{{
						Name: "master-0",
						From: v1alpha1.From{Provider: "provider", Profile: "profile"},
					}},
				},
			},
		}},
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"empty arg", []string{"", "x"}, "'' x"},
		{"plain", []string{"ansible-playbook", "-i", "inv"}, "ansible-playbook -i inv"},
		{"space-containing", []string{"echo", "hello world"}, "echo 'hello world'"},
		{"with-quotes", []string{"foo", "a'b"}, "foo 'a'\\''b'"},
		{"with-dollar", []string{"sh", "-c", "$X"}, "sh -c '$X'"},
		{"with-tab", []string{"x\ty"}, "'x\ty'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShellQuote(tc.in); got != tc.want {
				t.Fatalf("ShellQuote(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
