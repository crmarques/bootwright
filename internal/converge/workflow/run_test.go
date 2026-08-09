package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
	"go.yaml.in/yaml/v3"
)

type fakeRunner struct {
	runCalled            bool
	command              []string
	runReturns           error
	lastSpec             ansible.RunSpec
	onRun                func(ansible.RunSpec) error
	skipInstallerVersion bool
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
	if f.onRun != nil {
		if err := f.onRun(spec); err != nil {
			return err
		}
	}
	if f.runReturns != nil {
		return f.runReturns
	}
	if !f.skipInstallerVersion && (strings.HasSuffix(spec.Playbook, "task_container_cluster_create_agent_iso") || strings.HasSuffix(spec.Playbook, "task_container_cluster_create_agent_iso.yml")) {
		if err := writeFakeAgentISOInstallerVersion(spec); err != nil {
			return err
		}
	}
	return nil
}

func writeFakeAgentISOInstallerVersion(spec ansible.RunSpec) error {
	var clustersDir, clusterName string
	for _, pair := range spec.ExtraVarPairs {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		switch key {
		case "bootwright_clusters_dir":
			clustersDir = value
		case "bootwright_task_cluster_name":
			clusterName = value
		}
	}
	data, err := os.ReadFile(spec.ExtraVars)
	if err != nil {
		return err
	}
	var vars struct {
		Clusters []struct {
			Name         string `yaml:"name"`
			Distribution struct {
				Release struct {
					Version string `yaml:"version"`
				} `yaml:"release"`
			} `yaml:"distribution"`
		} `yaml:"bootwright_clusters"`
	}
	if err := yaml.Unmarshal(data, &vars); err != nil {
		return err
	}
	for _, cluster := range vars.Clusters {
		if cluster.Name != clusterName {
			continue
		}
		path := ClusterInstallerVersionPath(clustersDir, clusterName)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(cluster.Distribution.Release.Version+"\n"), 0o600)
	}
	return fmt.Errorf("cluster %q is missing from rendered test vars", clusterName)
}

func (f *fakeRunner) Command(spec ansible.RunSpec) []string {
	executable := spec.Executable
	if executable == "" {
		executable = "ansible-playbook"
	}
	return []string{executable, "-i", spec.Inventory, spec.Playbook}
}

func minimalState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec:     v1alpha1.EnvironmentSpec{Domains: v1alpha1.EnvironmentDomainsSpec{Base: "example.test"}},
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

func twoKubeVirtHostPlanningState() v1alpha1.State {
	state := kubeVirtChildPlanningState(false)
	secondCluster := state.ContainerClusters[0]
	secondCluster.Metadata.Name = "child-ocp-2"
	secondCluster.Spec.Nodes = append([]v1alpha1.OCPNodeSpec(nil), secondCluster.Spec.Nodes...)
	secondCluster.Spec.Nodes[0].Name = "master-1"
	secondCluster.Spec.Nodes[0].MachineRef.Name = "child-master-1"
	state.ContainerClusters = append(state.ContainerClusters, secondCluster)
	secondMachine := state.Machines[0]
	secondMachine.Metadata.Name = "child-master-1"
	secondMachine.Spec.Substrate.ProviderRef.Name = "child-kubevirt-provider-2"
	state.Machines = append(state.Machines, secondMachine)
	secondProvider := state.InfraProviders[0]
	secondProvider.Metadata.Name = "child-kubevirt-provider-2"
	secondKubeVirt := *secondProvider.Spec.KubeVirt
	secondHostRef := *secondKubeVirt.HostClusterRef
	secondHostRef.Name = "metal-ocp-2"
	secondKubeVirt.HostClusterRef = &secondHostRef
	secondKubeVirt.Namespace = "bootwright-child-ocp-2"
	secondProvider.Spec.KubeVirt = &secondKubeVirt
	state.InfraProviders = append(state.InfraProviders, secondProvider)
	return state
}

func TestRunMaterializesKubeVirtHostClusterKubeconfigForAnsible(t *testing.T) {
	for _, tc := range []struct {
		name   string
		runErr error
	}{
		{name: "success"},
		{name: "runner error", runErr: errors.New("runner failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseDir := t.TempDir()
			renderedDir := filepath.Join(baseDir, "rendered")
			clustersDir := filepath.Join(baseDir, "clusters")
			hostKubeconfigDir := filepath.Join(baseDir, "runtime", runtimeHostKubeconfigDirName)
			state := twoKubeVirtHostPlanningState()
			hostContents := map[string]string{
				"metal-ocp":   "apiVersion: v1\ncurrent-context: first\n",
				"metal-ocp-2": "apiVersion: v1\ncurrent-context: second\n",
			}
			for host, content := range hostContents {
				writeEncryptedClusterMaterial(t, clustersDir, host, "kubeconfig", content)
			}
			materializedPaths := map[string]string{}
			runner := &fakeRunner{
				runReturns: tc.runErr,
				onRun: func(spec ansible.RunSpec) error {
					vars, err := os.ReadFile(spec.ExtraVars)
					if err != nil {
						return err
					}
					var rendered struct {
						HostKubeconfigs map[string]string `yaml:"bootwright_kubevirt_host_kubeconfigs"`
					}
					if err := yaml.Unmarshal(vars, &rendered); err != nil {
						return err
					}
					if len(rendered.HostKubeconfigs) != len(hostContents) {
						return fmt.Errorf("materialized host kubeconfig map = %v", rendered.HostKubeconfigs)
					}
					seenPaths := map[string]string{}
					for host, content := range hostContents {
						path := rendered.HostKubeconfigs[host]
						if path == "" || !strings.HasPrefix(path, hostKubeconfigDir+string(os.PathSeparator)) {
							return fmt.Errorf("runtime kubeconfig for %s = %q", host, path)
						}
						if otherHost := seenPaths[path]; otherHost != "" {
							return fmt.Errorf("hosts %s and %s share materialized path %s", otherHost, host, path)
						}
						seenPaths[path] = host
						materializedPaths[host] = path
						data, err := os.ReadFile(path)
						if err != nil {
							return err
						}
						if string(data) != content {
							return fmt.Errorf("materialized kubeconfig for %s = %q", host, data)
						}
						info, err := os.Stat(path)
						if err != nil {
							return err
						}
						if info.Mode().Perm() != 0o600 {
							return fmt.Errorf("materialized kubeconfig mode for %s = %o", host, info.Mode().Perm())
						}
						dirInfo, err := os.Stat(filepath.Dir(path))
						if err != nil {
							return err
						}
						if dirInfo.Mode().Perm() != 0o700 {
							return fmt.Errorf("materialized kubeconfig directory mode for %s = %o", host, dirInfo.Mode().Perm())
						}
						durablePath := filepath.Join(ClusterSecretsDir(clustersDir, host), "kubeconfig")
						if strings.Contains(string(vars), durablePath) {
							return fmt.Errorf("rendered vars contain encrypted durable kubeconfig %s", durablePath)
						}
					}
					return nil
				},
			}
			_, err := Run(context.Background(), RunOptions{
				State:              state,
				RenderedDir:        renderedDir,
				ClustersDir:        clustersDir,
				RunsDir:            filepath.Join(baseDir, "runs"),
				ContextName:        "test",
				SecretsDir:         filepath.Join(baseDir, "secrets"),
				ManagedServicesDir: "/var/lib/bootwright",
				ProviderStateDir:   filepath.Join(baseDir, "provider-state"),
				BundleDir:          t.TempDir(),
				Playbook:           "bootwright.core.workflow_clusters_apply",
				ArtifactsBaseName:  "clusters",
			}, runner, nil)
			if !errors.Is(err, tc.runErr) {
				t.Fatalf("Run error = %v, want %v", err, tc.runErr)
			}
			if len(materializedPaths) != len(hostContents) {
				t.Fatalf("runner observed materialized paths %v", materializedPaths)
			}
			for host, path := range materializedPaths {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("materialized kubeconfig for %s must be removed after Run, stat err=%v", host, err)
				}
				if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
					t.Fatalf("materialized kubeconfig directory for %s must be removed after Run, stat err=%v", host, err)
				}
			}
		})
	}
}

func TestRunMaterializesOnlySelectedKubeVirtHostForVirtctlProvision(t *testing.T) {
	baseDir := t.TempDir()
	clustersDir := filepath.Join(baseDir, "clusters")
	writeEncryptedClusterMaterial(t, clustersDir, "metal-ocp", "kubeconfig", "apiVersion: v1\ncurrent-context: selected\n")
	var materializedPath string
	runner := &fakeRunner{onRun: func(spec ansible.RunSpec) error {
		vars, err := os.ReadFile(spec.ExtraVars)
		if err != nil {
			return err
		}
		var rendered struct {
			HostKubeconfigs map[string]string `yaml:"bootwright_kubevirt_host_kubeconfigs"`
		}
		if err := yaml.Unmarshal(vars, &rendered); err != nil {
			return err
		}
		if len(rendered.HostKubeconfigs) != 1 || rendered.HostKubeconfigs["metal-ocp"] == "" {
			return fmt.Errorf("selected host kubeconfig map = %v", rendered.HostKubeconfigs)
		}
		materializedPath = rendered.HostKubeconfigs["metal-ocp"]
		data, err := os.ReadFile(materializedPath)
		if err != nil {
			return err
		}
		if string(data) != "apiVersion: v1\ncurrent-context: selected\n" {
			return fmt.Errorf("selected host kubeconfig = %q", data)
		}
		return nil
	}}
	_, err := Run(context.Background(), RunOptions{
		State:              twoKubeVirtHostPlanningState(),
		RenderedDir:        filepath.Join(baseDir, "rendered"),
		ClustersDir:        clustersDir,
		RunsDir:            filepath.Join(baseDir, "runs"),
		ContextName:        "test",
		SecretsDir:         filepath.Join(baseDir, "secrets"),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   filepath.Join(baseDir, "provider-state"),
		BundleDir:          t.TempDir(),
		Playbook:           applyHostVirtctlPlaybook,
		ExtraVarPairs:      []string{"bootwright_task_host_cluster_name=metal-ocp"},
		ArtifactsBaseName:  "host-virtctl",
	}, runner, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !runner.runCalled {
		t.Fatal("selected host virtctl task did not reach the runner")
	}
	if _, err := os.Stat(materializedPath); !os.IsNotExist(err) {
		t.Fatalf("selected host kubeconfig must be removed after Run, stat err=%v", err)
	}
}

func TestRunCleansMaterializedKubeVirtHostsWhenLaterHostIsPlaintext(t *testing.T) {
	baseDir := t.TempDir()
	clustersDir := filepath.Join(baseDir, "clusters")
	writeEncryptedClusterMaterial(t, clustersDir, "metal-ocp", "kubeconfig", "apiVersion: v1\ncurrent-context: first\n")
	secondPath := filepath.Join(ClusterSecretsDir(clustersDir, "metal-ocp-2"), "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(secondPath), 0o700); err != nil {
		t.Fatalf("mkdir second host secrets: %v", err)
	}
	if err := os.WriteFile(secondPath, []byte("apiVersion: v1\ncurrent-context: second\n"), 0o600); err != nil {
		t.Fatalf("write second plaintext kubeconfig: %v", err)
	}
	runner := &fakeRunner{}
	_, err := Run(context.Background(), RunOptions{
		State:              twoKubeVirtHostPlanningState(),
		RenderedDir:        filepath.Join(baseDir, "rendered"),
		ClustersDir:        clustersDir,
		RunsDir:            filepath.Join(baseDir, "runs"),
		ContextName:        "test",
		SecretsDir:         filepath.Join(baseDir, "secrets"),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   filepath.Join(baseDir, "provider-state"),
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.workflow_clusters_apply",
		ArtifactsBaseName:  "clusters",
	}, runner, nil)
	if err == nil || !strings.Contains(err.Error(), "not encrypted") {
		t.Fatalf("Run error = %v, want encrypted-envelope refusal", err)
	}
	if runner.runCalled {
		t.Fatal("runner must not run after host kubeconfig materialization fails")
	}
	matches, globErr := filepath.Glob(filepath.Join(baseDir, "runtime", runtimeHostKubeconfigDirName, "bootwright-material-*"))
	if globErr != nil {
		t.Fatalf("glob materialized hosts: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("materialized host scratch paths remain after failure: %v", matches)
	}
	data, readErr := os.ReadFile(secondPath)
	if readErr != nil {
		t.Fatalf("read second plaintext kubeconfig: %v", readErr)
	}
	if string(data) != "apiVersion: v1\ncurrent-context: second\n" {
		t.Fatalf("second plaintext kubeconfig was modified: %q", data)
	}
}

func TestRunRejectsPlaintextKubeVirtHostClusterKubeconfig(t *testing.T) {
	baseDir := t.TempDir()
	clustersDir := filepath.Join(baseDir, "clusters")
	hostSecretsDir := ClusterSecretsDir(clustersDir, "metal-ocp")
	if err := os.MkdirAll(hostSecretsDir, 0o700); err != nil {
		t.Fatalf("mkdir host secrets: %v", err)
	}
	kubeconfigPath := filepath.Join(hostSecretsDir, "kubeconfig")
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write plaintext kubeconfig: %v", err)
	}
	runner := &fakeRunner{}
	_, err := Run(context.Background(), RunOptions{
		State:              kubeVirtChildPlanningState(false),
		RenderedDir:        filepath.Join(baseDir, "rendered"),
		ClustersDir:        clustersDir,
		RunsDir:            filepath.Join(baseDir, "runs"),
		ContextName:        "test",
		SecretsDir:         filepath.Join(baseDir, "secrets"),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   filepath.Join(baseDir, "provider-state"),
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.workflow_clusters_apply",
		ArtifactsBaseName:  "clusters",
	}, runner, nil)
	if err == nil || !strings.Contains(err.Error(), "not encrypted") {
		t.Fatalf("Run error = %v, want encrypted-envelope refusal", err)
	}
	if runner.runCalled {
		t.Fatal("runner must not receive a plaintext host kubeconfig")
	}
	data, readErr := os.ReadFile(kubeconfigPath)
	if readErr != nil {
		t.Fatalf("read plaintext kubeconfig after refusal: %v", readErr)
	}
	if string(data) != "apiVersion: v1\n" {
		t.Fatalf("plaintext kubeconfig was modified: %q", data)
	}
}

func TestRunMachineInfraDestroyToleratesMissingKubeVirtHostKubeconfig(t *testing.T) {
	baseDir := t.TempDir()
	runner := &fakeRunner{}
	result, err := Run(context.Background(), RunOptions{
		State:              kubeVirtChildPlanningState(false),
		RenderedDir:        filepath.Join(baseDir, "rendered"),
		ClustersDir:        filepath.Join(baseDir, "clusters"),
		RunsDir:            filepath.Join(baseDir, "runs"),
		ContextName:        "test",
		SecretsDir:         filepath.Join(baseDir, "secrets"),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   filepath.Join(baseDir, "provider-state"),
		BundleDir:          t.TempDir(),
		Playbook:           roles.PlaybookTaskMachineInfraDestroy,
		ArtifactsBaseName:  "machine-infra",
	}, runner, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !runner.runCalled {
		t.Fatal("machine infrastructure destroy did not reach the runner")
	}
	vars, err := os.ReadFile(result.Render.VarsPath)
	if err != nil {
		t.Fatalf("read rendered vars: %v", err)
	}
	if strings.Contains(string(vars), "bootwright_kubevirt_host_kubeconfigs") {
		t.Fatalf("destroy rendered a host kubeconfig map for an uninstalled host cluster:\n%s", vars)
	}
	if strings.Contains(string(vars), "metal-ocp/secrets/kubeconfig") {
		t.Fatalf("destroy rendered a host kubeconfig path for an uninstalled host cluster:\n%s", vars)
	}
}

func TestRunRejectsMissingKubeVirtHostKubeconfigForApply(t *testing.T) {
	baseDir := t.TempDir()
	runner := &fakeRunner{}
	_, err := Run(context.Background(), RunOptions{
		State:              kubeVirtChildPlanningState(false),
		RenderedDir:        filepath.Join(baseDir, "rendered"),
		ClustersDir:        filepath.Join(baseDir, "clusters"),
		RunsDir:            filepath.Join(baseDir, "runs"),
		ContextName:        "test",
		SecretsDir:         filepath.Join(baseDir, "secrets"),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   filepath.Join(baseDir, "provider-state"),
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.workflow_clusters_apply",
		ArtifactsBaseName:  "clusters",
	}, runner, nil)
	if err == nil || !strings.Contains(err.Error(), "ContainerCluster/metal-ocp") {
		t.Fatalf("Run error = %v, want a named missing-kubeconfig refusal", err)
	}
	if !strings.Contains(err.Error(), "bootwright apply --clusters metal-ocp") {
		t.Fatalf("Run error = %v, want the remedy command", err)
	}
	if runner.runCalled {
		t.Fatal("apply must not run without the KubeVirt host kubeconfig")
	}
}

func TestRunDryRunDoesNotMaterializeKubeVirtHostClusterKubeconfig(t *testing.T) {
	baseDir := t.TempDir()
	renderedDir := filepath.Join(baseDir, "rendered")
	runner := &fakeRunner{}
	result, err := Run(context.Background(), RunOptions{
		State:              kubeVirtChildPlanningState(false),
		RenderedDir:        renderedDir,
		ClustersDir:        filepath.Join(baseDir, "clusters"),
		RunsDir:            filepath.Join(baseDir, "runs"),
		ContextName:        "test",
		SecretsDir:         filepath.Join(baseDir, "secrets"),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   filepath.Join(baseDir, "provider-state"),
		BundleDir:          t.TempDir(),
		Playbook:           "bootwright.core.workflow_clusters_apply",
		ArtifactsBaseName:  "clusters",
		DryRun:             true,
	}, runner, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runner.runCalled {
		t.Fatal("dry-run must not invoke the runner")
	}
	if _, err := os.Stat(filepath.Join(baseDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not create runtime secrets, stat err=%v", err)
	}
	vars, err := os.ReadFile(result.Render.VarsPath)
	if err != nil {
		t.Fatalf("read rendered vars: %v", err)
	}
	if !strings.Contains(string(vars), "{{ bootwright_clusters_dir }}/metal-ocp/secrets/kubeconfig") {
		t.Fatalf("dry-run vars do not retain the logical host kubeconfig path:\n%s", vars)
	}
}

func TestRunUnrelatedPlaybookDoesNotReadKubeVirtHostKubeconfig(t *testing.T) {
	baseDir := t.TempDir()
	runner := &fakeRunner{}
	result, err := Run(context.Background(), RunOptions{
		State:              kubeVirtChildPlanningState(false),
		RenderedDir:        filepath.Join(baseDir, "rendered"),
		ClustersDir:        filepath.Join(baseDir, "clusters"),
		RunsDir:            filepath.Join(baseDir, "runs"),
		ContextName:        "test",
		SecretsDir:         filepath.Join(baseDir, "secrets"),
		ManagedServicesDir: "/var/lib/bootwright",
		ProviderStateDir:   filepath.Join(baseDir, "provider-state"),
		BundleDir:          t.TempDir(),
		Playbook:           applyProviderPlaybook,
		ArtifactsBaseName:  "providers",
	}, runner, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !runner.runCalled {
		t.Fatal("unrelated playbook did not reach the runner")
	}
	vars, err := os.ReadFile(result.Render.VarsPath)
	if err != nil {
		t.Fatalf("read rendered vars: %v", err)
	}
	if strings.Contains(string(vars), "bootwright_kubevirt_host_kubeconfigs") {
		t.Fatalf("unrelated playbook rendered a runtime host kubeconfig map:\n%s", vars)
	}
	if strings.Contains(string(vars), "{{ bootwright_clusters_dir }}/metal-ocp/secrets/kubeconfig") {
		t.Fatalf("unrelated execution rendered the durable encrypted host kubeconfig:\n%s", vars)
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
	}, runner, reporter)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reporter.dryRunLabel != "bootwright.core.check_preflight" {
		t.Fatalf("empty Label should fall back to Playbook, got %q", reporter.dryRunLabel)
	}
}

func TestRunRenderFailureSurfaces(t *testing.T) {
	stateFile := t.TempDir() + "/not-a-dir"
	if err := writeStub(stateFile); err != nil {
		t.Fatalf("setup: %v", err)
	}
	runner := &fakeRunner{}
	_, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        stateFile,
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
						Auth:       v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "ceph-node-ssh"}},
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
							Node: "ceph-0",
						},
					},
					Topology: v1alpha1.StorageCephTopology{
						Nodes: []v1alpha1.StorageCephNode{{
							Name:       "ceph-0",
							MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
						}},
					},
				},
			},
		}},
	}
}

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
			Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{
				Name:       "master-0",
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
