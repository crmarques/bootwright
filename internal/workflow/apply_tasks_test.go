package workflow

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ansible"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

type fakeClusterAvailabilityChecker struct {
	available bool
	err       error
	paths     []string
}

func (f *fakeClusterAvailabilityChecker) Available(_ context.Context, kubeconfigPath string) (bool, error) {
	f.paths = append(f.paths, kubeconfigPath)
	return f.available, f.err
}

func TestRunApplyTaskGraphUsesRunnerFactory(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &fakeRunner{}
	calls := 0
	factory := func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		calls++
		if stdout == nil || stderr == nil {
			t.Fatal("runner factory received nil task writers")
		}
		return runner
	}
	task := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:     "provider.service-host",
			Kind:   ApplyTaskKindProvider,
			Label:  "provider services service-host",
			Status: TaskStatusPending,
		},
		Playbook: "playbooks/layers/providers/apply.yml",
		State:    state,
	}
	renderedDir := filepath.Join(dir, "rendered")
	runtimeDir := filepath.Join(dir, "runtime")
	runsDir := filepath.Join(dir, "runs")
	managedDir := filepath.Join(dir, "managed")
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
		State:             state,
		RenderedDir:       renderedDir,
		RuntimeDir:        runtimeDir,
		RunsDir:           runsDir,
		SecretsDir:        filepath.Join(dir, "secrets"),
		ManagedDir:        managedDir,
		BundleDir:         filepath.Join(dir, "bundle"),
		ArtifactsBaseName: "provider",
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseProvider}}, "", []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1}, nil, factory)
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner factory calls = %d, want 1", calls)
	}
	if !runner.runCalled {
		t.Fatal("fake runner was not invoked")
	}
	if !strings.HasSuffix(runner.lastSpec.Playbook, "playbooks/layers/providers/apply.yml") {
		t.Fatalf("playbook = %q", runner.lastSpec.Playbook)
	}
}

func TestRunApplyTaskGraphSkipsInstalledClusterBeforeAnsible(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	renderedDir := filepath.Join(dir, "rendered")
	runtimeDir := filepath.Join(dir, "runtime")
	runsDir := filepath.Join(dir, "runs")
	managedDir := filepath.Join(dir, "managed")
	hash, err := clusterInstallDesiredHash(state, "sno-libvirt", secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash: %v", err)
	}
	now := time.Now()
	record := ClusterInstallRecord{
		Cluster:     "sno-libvirt",
		DesiredHash: hash,
		Status:      ClusterInstallStatusInstalled,
		Phase:       ClusterInstallPhaseComplete,
		RunID:       "previous-run",
		StartedAt:   now.UTC(),
		UpdatedAt:   now.UTC(),
		InstalledAt: &now,
	}
	if err := SaveClusterInstallRecord(runtimeDir, record); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	kubeconfig := clusterKubeconfigPath(runtimeDir, state, "sno-libvirt")
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o700); err != nil {
		t.Fatalf("mkdir kubeconfig dir: %v", err)
	}
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	checker := &fakeClusterAvailabilityChecker{available: true}
	calls := 0
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
		State:                      state,
		RenderedDir:                renderedDir,
		RuntimeDir:                 runtimeDir,
		RunsDir:                    runsDir,
		SecretsDir:                 secretsDir,
		ManagedDir:                 managedDir,
		BundleDir:                  filepath.Join(dir, "bundle"),
		ClusterAvailabilityChecker: checker,
	}, ApplyTarget{Name: "cluster", PhaseNames: []string{ApplyPhaseClusters}}, "", PlanApplyTasks(ApplyTarget{Name: "cluster", PhaseNames: []string{ApplyPhaseClusters}}, state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		calls++
		return &fakeRunner{}
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if calls != 0 {
		t.Fatalf("runner factory calls = %d, want 0", calls)
	}
	for _, task := range ledger.Tasks {
		if task.Status != TaskStatusSkipped {
			t.Fatalf("task %s status = %s, want skipped", task.ID, task.Status)
		}
	}
	if len(checker.paths) != 1 || checker.paths[0] != kubeconfig {
		t.Fatalf("availability paths = %v, want %s", checker.paths, kubeconfig)
	}
}

func TestRunApplyTaskGraphBlocksInstalledClusterHashMismatch(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	renderedDir := filepath.Join(dir, "rendered")
	runtimeDir := filepath.Join(dir, "runtime")
	runsDir := filepath.Join(dir, "runs")
	managedDir := filepath.Join(dir, "managed")
	if err := SaveClusterInstallRecord(runtimeDir, ClusterInstallRecord{
		Cluster:     "sno-libvirt",
		DesiredHash: "sha256:old",
		Status:      ClusterInstallStatusInstalled,
		Phase:       ClusterInstallPhaseComplete,
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	calls := 0
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
		State:       state,
		RenderedDir: renderedDir,
		RuntimeDir:  runtimeDir,
		RunsDir:     runsDir,
		SecretsDir:  secretsDir,
		ManagedDir:  managedDir,
		BundleDir:   filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "cluster", PhaseNames: []string{ApplyPhaseClusters}}, "", PlanApplyTasks(ApplyTarget{Name: "cluster", PhaseNames: []string{ApplyPhaseClusters}}, state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		calls++
		return &fakeRunner{}
	})
	if err == nil || !strings.Contains(err.Error(), "different install inputs") {
		t.Fatalf("RunApplyTaskGraph error = %v, want different install inputs", err)
	}
	if calls != 0 {
		t.Fatalf("runner factory calls = %d, want 0", calls)
	}
}

func TestRunApplyTaskGraphBlocksEmptyDesiredHashAfterNodeBoot(t *testing.T) {
	cases := []struct {
		name   string
		status ClusterInstallStatus
		phase  ClusterInstallPhase
	}{
		{
			name:   "nodes booted",
			status: ClusterInstallStatusInstalling,
			phase:  ClusterInstallPhaseNodesBooted,
		},
		{
			name:   "installed",
			status: ClusterInstallStatusInstalled,
			phase:  ClusterInstallPhaseComplete,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			state := loadWorkflowFixtureState(t, "001-sno-libvirt")
			secretsDir := writeWorkflowInstallerSecrets(t, dir)
			renderedDir := filepath.Join(dir, "rendered")
			runtimeDir := filepath.Join(dir, "runtime")
			runsDir := filepath.Join(dir, "runs")
			managedDir := filepath.Join(dir, "managed")
			if err := SaveClusterInstallRecord(runtimeDir, ClusterInstallRecord{
				Cluster:   "sno-libvirt",
				Status:    tc.status,
				Phase:     tc.phase,
				UpdatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatalf("SaveClusterInstallRecord: %v", err)
			}
			calls := 0
			_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
				State:       state,
				RenderedDir: renderedDir,
				RuntimeDir:  runtimeDir,
				RunsDir:     runsDir,
				SecretsDir:  secretsDir,
				ManagedDir:  managedDir,
				BundleDir:   filepath.Join(dir, "bundle"),
			}, ApplyTarget{Name: "cluster", PhaseNames: []string{ApplyPhaseClusters}}, "", PlanApplyTasks(ApplyTarget{Name: "cluster", PhaseNames: []string{ApplyPhaseClusters}}, state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
				calls++
				return &fakeRunner{}
			})
			if err == nil || !strings.Contains(err.Error(), "missing or different install inputs") {
				t.Fatalf("RunApplyTaskGraph error = %v, want missing or different install inputs", err)
			}
			if calls != 0 {
				t.Fatalf("runner factory calls = %d, want 0", calls)
			}
		})
	}
}

func TestRunApplyTaskGraphResumesPostBootInstallAtWait(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	renderedDir := filepath.Join(dir, "rendered")
	runtimeDir := filepath.Join(dir, "runtime")
	runsDir := filepath.Join(dir, "runs")
	managedDir := filepath.Join(dir, "managed")
	hash, err := clusterInstallDesiredHash(state, "sno-libvirt", secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash: %v", err)
	}
	if err := SaveClusterInstallRecord(runtimeDir, ClusterInstallRecord{
		Cluster:     "sno-libvirt",
		DesiredHash: hash,
		Status:      ClusterInstallStatusInstalling,
		Phase:       ClusterInstallPhaseNodesBooted,
		RunID:       "previous-run",
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	runner := &fakeRunner{}
	calls := 0
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
		State:       state,
		RenderedDir: renderedDir,
		RuntimeDir:  runtimeDir,
		RunsDir:     runsDir,
		SecretsDir:  secretsDir,
		ManagedDir:  managedDir,
		BundleDir:   filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "cluster", PhaseNames: []string{ApplyPhaseClusters}}, "", PlanApplyTasks(ApplyTarget{Name: "cluster", PhaseNames: []string{ApplyPhaseClusters}}, state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		calls++
		return runner
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner factory calls = %d, want 1", calls)
	}
	if !strings.HasSuffix(runner.lastSpec.Playbook, "playbooks/layers/openshift/wait-agent-install.yml") {
		t.Fatalf("playbook = %q, want wait-agent-install.yml", runner.lastSpec.Playbook)
	}
	for _, id := range []string{"iso.sno-libvirt", "boot.sno-libvirt"} {
		task, ok := ledger.Task(id)
		if !ok {
			t.Fatalf("missing task %s", id)
		}
		if task.Status != TaskStatusSkipped {
			t.Fatalf("task %s status = %s, want skipped", id, task.Status)
		}
	}
	wait, ok := ledger.Task("wait.sno-libvirt")
	if !ok {
		t.Fatal("missing wait task")
	}
	if wait.Status != TaskStatusOK {
		t.Fatalf("wait status = %s, want ok", wait.Status)
	}
	record, found, err := LoadClusterInstallRecord(runtimeDir, "sno-libvirt")
	if err != nil || !found {
		t.Fatalf("LoadClusterInstallRecord found=%v err=%v", found, err)
	}
	if record.Status != ClusterInstallStatusInstalled || record.Phase != ClusterInstallPhaseComplete {
		t.Fatalf("record = %+v, want installed complete", record)
	}
}

func TestClusterInstallDesiredHashChangesWhenProxyCredentialsChange(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "004-3nodes-emul-baremetal")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	clusterName := "3-nodes-ocp-emul-baremetal"

	first, err := clusterInstallDesiredHash(state, clusterName, secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash first: %v", err)
	}
	proxyCreds := filepath.Join(secretsDir, "proxy-credentials")
	if err := os.WriteFile(proxyCreds, []byte("proxy:changed-secret\n"), 0o600); err != nil {
		t.Fatalf("write proxy credentials: %v", err)
	}
	changedAt := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(proxyCreds, changedAt, changedAt); err != nil {
		t.Fatalf("chtimes proxy credentials: %v", err)
	}
	second, err := clusterInstallDesiredHash(state, clusterName, secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash second: %v", err)
	}
	if first == second {
		t.Fatal("cluster install desired hash did not change after proxy credentials changed")
	}
}

func TestPlanApplyExtensionsOrdersExtensionTasks(t *testing.T) {
	state := extensionPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "extensions", PhaseNames: []string{ApplyPhaseExtensions}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	gotIDs := applyTaskIDs(tasks)
	wantIDs := []string{
		"extension.demo.a.apply",
		"extension.demo.a.wait",
		"extension.demo.b.apply",
		"extension.demo.b.wait",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("task IDs = %v, want %v", gotIDs, wantIDs)
	}
	assertTaskDeps(t, tasks, "extension.demo.a.apply")
	assertTaskDeps(t, tasks, "extension.demo.a.wait", "extension.demo.a.apply")
	assertTaskDeps(t, tasks, "extension.demo.b.apply", "extension.demo.a.wait")
	assertTaskDeps(t, tasks, "extension.demo.b.wait", "extension.demo.b.apply")
}

func TestPlanApplyAllRunsExtensionsAfterInstallWait(t *testing.T) {
	state := extensionPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseClusters, ApplyPhaseExtensions}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskDeps(t, tasks, "extension.demo.a.apply", "wait.demo")
	assertTaskDeps(t, tasks, "extension.demo.a.wait", "extension.demo.a.apply")
}

func loadWorkflowFixtureState(t *testing.T, name string) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "desiredstate", "testdata", "good", name)})
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return state
}

func writeWorkflowInstallerSecrets(t *testing.T, root string) string {
	t.Helper()
	home := filepath.Join(root, "home")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir ssh dir: %v", err)
	}
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(sshDir, "bootwright-ssh-key.pub"), []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForWorkflowTests\n"), 0o600); err != nil {
		t.Fatalf("write ssh public key: %v", err)
	}
	secretsDir := filepath.Join(root, "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir secrets dir: %v", err)
	}
	files := map[string]string{
		"openshift-pull-secret":     `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`,
		"cluster-admin-ssh-key.pub": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForWorkflowTests\n",
		"proxy-credentials":         "proxy:secret\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(secretsDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return secretsDir
}

func extensionPlanningState() v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
		}},
		ClusterExtensions: []v1alpha1.ClusterExtension{
			{Metadata: v1alpha1.Metadata{Name: "a"}, Spec: v1alpha1.ClusterExtensionSpec{Type: v1alpha1.ClusterExtensionTypeManifestSet}},
			{Metadata: v1alpha1.Metadata{Name: "b"}, Spec: v1alpha1.ClusterExtensionSpec{Type: v1alpha1.ClusterExtensionTypeManifestSet}},
		},
		ClusterExtensionSets: []v1alpha1.ClusterExtensionSet{{
			Metadata: v1alpha1.Metadata{Name: "platform"},
			Spec: v1alpha1.ClusterExtensionSetSpec{
				Extensions: []v1alpha1.LocalObjectReference{{Name: "a"}},
			},
		}},
		ClusterExtensionBindings: []v1alpha1.ClusterExtensionBinding{{
			Metadata: v1alpha1.Metadata{Name: "binding"},
			Spec: v1alpha1.ClusterExtensionBindingSpec{
				ClusterSelector: v1alpha1.ClusterExtensionClusterSelector{Names: []string{"demo"}},
				ExtensionSets:   []v1alpha1.LocalObjectReference{{Name: "platform"}},
				Extensions:      []v1alpha1.LocalObjectReference{{Name: "b"}},
			},
		}},
	}
}

func applyTaskIDs(tasks []ApplyTask) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.Entry.ID)
	}
	return out
}

func assertTaskDeps(t *testing.T, tasks []ApplyTask, id string, want ...string) {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID != id {
			continue
		}
		if !reflect.DeepEqual(task.Entry.Dependencies, want) {
			t.Fatalf("%s deps = %v, want %v", id, task.Entry.Dependencies, want)
		}
		return
	}
	t.Fatalf("task %s not found in %+v", id, applyTaskIDs(tasks))
}
