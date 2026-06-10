package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/runtime/ownership"
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
// triggering provider-closure validation in the installer renderer.
func minimalState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec:     v1alpha1.EnvironmentSpec{BaseDomain: "example.test"},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "bastion"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "bastion.example.test"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
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

func TestRunUsesContextManagedKnownHostsWithRuntimeSecrets(t *testing.T) {
	renderedDir := t.TempDir()
	contextSecretsDir := filepath.Join(t.TempDir(), "context", "secrets")
	runner := &fakeRunner{}
	if _, err := Run(context.Background(), RunOptions{
		State:              storageSSHState(),
		RenderedDir:        renderedDir,
		ClustersDir:        t.TempDir(),
		RunsDir:            t.TempDir(),
		SecretsDir:         contextSecretsDir,
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   "/var/lib/bootwright/provider-state",
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.task_storage_cluster_apply",
		ArtifactsBaseName:  "storage",
	}, runner, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(runner.lastSpec.Inventory)
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}
	inventory := string(data)
	runtimeSecretsDir := filepath.Join(filepath.Dir(renderedDir), "runtime", "secrets")
	contextKnownHosts := filepath.Join(filepath.Dir(contextSecretsDir), "trust", "ssh", "known_hosts")
	taskKnownHosts := filepath.Join(filepath.Dir(runtimeSecretsDir), "trust", "ssh", "known_hosts")
	if !strings.Contains(inventory, filepath.Join(runtimeSecretsDir, "ceph-node-ssh")) {
		t.Fatalf("inventory missing runtime private key path %s:\n%s", runtimeSecretsDir, inventory)
	}
	if !strings.Contains(inventory, "UserKnownHostsFile="+contextKnownHosts) {
		t.Fatalf("inventory missing context known_hosts %s:\n%s", contextKnownHosts, inventory)
	}
	if strings.Contains(inventory, "UserKnownHostsFile="+taskKnownHosts) {
		t.Fatalf("inventory used task-local known_hosts %s:\n%s", taskKnownHosts, inventory)
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

func storageSSHState() v1alpha1.State {
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "ceph-0"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "192.0.2.10"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						KeyRef:     v1alpha1.SecretRef{Name: "ceph-node-ssh"},
					},
				},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						Bootstrap: v1alpha1.StorageCephadmBootstrap{
							Host: "ceph-0",
						},
					},
					Topology: v1alpha1.StorageCephTopology{
						Hosts: []v1alpha1.StorageCephHost{{
							Hostname:   "ceph-0",
							MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
						}},
					},
				},
			},
		}},
	}
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
	state := minimalState()
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
		{"literal inventory host", "bastion", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testState := state
			if tc.limit == "bastion" {
				testState = stateWithInfraHost("bastion")
			}
			if got := LimitMatchesNoHosts(tc.limit, testState); got != tc.want {
				t.Fatalf("LimitMatchesNoHosts(%q) = %v, want %v", tc.limit, got, tc.want)
			}
		})
	}
}

func TestLimitMatchesNoHostsUsesOwnershipRecords(t *testing.T) {
	records := []ownership.ResourceRecord{{
		Kind: "libvirt-domain",
		Name: "cluster-a-machine-0",
		Host: "provider-0",
	}}
	if got := LimitMatchesNoHostsWithOwnershipRecords(render.GroupInfraHosts, v1alpha1.State{}, records); got {
		t.Fatalf("LimitMatchesNoHostsWithOwnershipRecords(%q) = true, want false", render.GroupInfraHosts)
	}
	if got := LimitMatchesNoHostsWithOwnershipRecords("provider-0", v1alpha1.State{}, records); got {
		t.Fatal("literal recorded host limit matched no hosts")
	}
}

func stateWithInfraHost(hostName string) v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: hostName},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityLibvirt},
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: hostName}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
			},
		}, {
			Metadata: v1alpha1.Metadata{Name: "master-0"},
			Spec: v1alpha1.MachineSpec{
				Substrate: v1alpha1.MachineSubstrate{
					ProviderRef: v1alpha1.LocalObjectReference{Name: "provider"},
					ProfileRef:  v1alpha1.LocalObjectReference{Name: "profile"},
				},
				OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster"},
			Spec: v1alpha1.ContainerClusterSpec{Hosts: []v1alpha1.OCPHostSpec{{
				Hostname:   "master-0",
				MachineRef: v1alpha1.LocalObjectReference{Name: "master-0"},
			}}},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "provider"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerLibvirt,
				Libvirt: &v1alpha1.InfraProviderLibvirt{
					MachineRef: v1alpha1.LocalObjectReference{Name: hostName},
					URI:        "qemu:///system",
					MachineProfiles: []v1alpha1.MachineProfile{{
						Name: "profile",
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
