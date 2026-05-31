package workflow

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/desired"
	storageapply "github.com/crmarques/bootwright/internal/storage"
	"go.yaml.in/yaml/v3"
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
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
		State:              state,
		RenderedDir:        renderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: managedServicesDir,
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
		ArtifactsBaseName:  "provider",
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
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
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
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	kubeconfig := clusterKubeconfigPath(clustersDir, "sno-libvirt")
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
		ClustersDir:                clustersDir,
		RunsDir:                    runsDir,
		SecretsDir:                 secretsDir,
		ManagedServicesDir:         managedServicesDir,
		ProviderStateDir:           filepath.Join(dir, "provider-state"),
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
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
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
		State:              state,
		RenderedDir:        renderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         secretsDir,
		ManagedServicesDir: managedServicesDir,
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
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
			clustersDir := filepath.Join(dir, "clusters")
			runsDir := filepath.Join(dir, "runs")
			managedServicesDir := filepath.Join(dir, "managed-services")
			if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
				Cluster:   "sno-libvirt",
				Status:    tc.status,
				Phase:     tc.phase,
				UpdatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatalf("SaveClusterInstallRecord: %v", err)
			}
			calls := 0
			_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
				State:              state,
				RenderedDir:        renderedDir,
				ClustersDir:        clustersDir,
				RunsDir:            runsDir,
				SecretsDir:         secretsDir,
				ManagedServicesDir: managedServicesDir,
				ProviderStateDir:   filepath.Join(dir, "provider-state"),
				BundleDir:          filepath.Join(dir, "bundle"),
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
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
	hash, err := clusterInstallDesiredHash(state, "sno-libvirt", secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash: %v", err)
	}
	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
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
		State:              state,
		RenderedDir:        renderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         secretsDir,
		ManagedServicesDir: managedServicesDir,
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
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
	record, found, err := LoadClusterInstallRecord(clustersDir, "sno-libvirt")
	if err != nil || !found {
		t.Fatalf("LoadClusterInstallRecord found=%v err=%v", found, err)
	}
	if record.Status != ClusterInstallStatusInstalled || record.Phase != ClusterInstallPhaseComplete {
		t.Fatalf("record = %+v, want installed complete", record)
	}
	data, err := os.ReadFile(ClusterConnectionPath(clustersDir, "sno-libvirt"))
	if err != nil {
		t.Fatalf("read cluster connection record: %v", err)
	}
	var connection ClusterConnectionRecord
	if err := json.Unmarshal(data, &connection); err != nil {
		t.Fatalf("decode cluster connection record: %v", err)
	}
	if connection.KubeconfigPath != clusterKubeconfigPath(clustersDir, "sno-libvirt") {
		t.Fatalf("connection kubeconfig path = %q", connection.KubeconfigPath)
	}
	if connection.APIURL != "https://api.sno-libvirt.bootwright.test:6443" {
		t.Fatalf("connection API URL = %q", connection.APIURL)
	}
	if connection.ConsoleURL != "https://console-openshift-console.apps.sno-libvirt.bootwright.test" {
		t.Fatalf("connection console URL = %q", connection.ConsoleURL)
	}
	if connection.IngressBaseDomain != "apps.sno-libvirt.bootwright.test" {
		t.Fatalf("connection ingress base domain = %q", connection.IngressBaseDomain)
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

func TestPlanApplyClusterRunsExtensionsAfterInstallWait(t *testing.T) {
	state := extensionPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "cluster", PhaseNames: []string{ApplyPhaseClusters, ApplyPhaseExtensions}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	gotIDs := applyTaskIDs(tasks)
	wantIDs := []string{
		"iso.demo",
		"wait.demo",
		"extension.demo.a.apply",
		"extension.demo.a.wait",
		"extension.demo.b.apply",
		"extension.demo.b.wait",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("task IDs = %v, want %v", gotIDs, wantIDs)
	}
	assertTaskDeps(t, tasks, "extension.demo.a.apply", "wait.demo")
	assertTaskDeps(t, tasks, "extension.demo.a.wait", "extension.demo.a.apply")
	assertTaskDeps(t, tasks, "extension.demo.b.apply", "extension.demo.a.wait")
	assertTaskDeps(t, tasks, "extension.demo.b.wait", "extension.demo.b.apply")
}

func TestPlanApplyAllOrdersStorageBindingsAfterStorageInstallAndDataFoundation(t *testing.T) {
	state := storageBindingPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseStorage, ApplyPhaseClusters, ApplyPhaseExtensions}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskDeps(t, tasks, "storagebinding.demo.ceph-binding.apply", "wait.demo", "storage.ceph", "extension.demo.odf.wait")
}

func TestPlanApplyAllExternalStorageBindingSkipsStorageTask(t *testing.T) {
	state := externalStorageBindingPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseStorage, ApplyPhaseClusters, ApplyPhaseExtensions}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	for _, id := range applyTaskIDs(tasks) {
		if id == "storage.shared-ceph" {
			t.Fatalf("external storage planned storage task: %v", applyTaskIDs(tasks))
		}
	}
	assertTaskDeps(t, tasks, "storagebinding.demo.shared-ceph-binding.apply", "wait.demo", "extension.demo.odf.wait")
}

func TestExamplesLoadValidateRenderAndPlanApplyAll(t *testing.T) {
	examplesRoot := filepath.Join("..", "..", "..", "examples")
	entries, err := os.ReadDir(examplesRoot)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(examplesRoot, name)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			if _, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state); err != nil {
				t.Fatalf("render.All: %v", err)
			}
			if _, err := PlanApplyTasksChecked(applyAllTarget(), state); err != nil {
				t.Fatalf("PlanApplyTasksChecked apply all: %v", err)
			}
		})
	}
}

func TestExternalStorageExamplesPlanDataFoundationBindingsWithoutCephTask(t *testing.T) {
	cases := []struct {
		example        string
		clusters       []string
		binding        string
		extension      string
		storageTask    string
		wantStorageJob bool
	}{
		{
			example:     "baremetal-redfish-odf-external-ceph",
			clusters:    []string{"demo-ocp"},
			binding:     "shared-ceph-data-foundation",
			extension:   "openshift-data-foundation-operator",
			storageTask: "storage.shared-ceph",
		},
		{
			example:     "baremetal-redfish-fusion-external-ceph",
			clusters:    []string{"demo-ocp"},
			binding:     "shared-ceph-data-foundation",
			extension:   "ibm-fusion-data-foundation-operator",
			storageTask: "storage.shared-ceph",
		},
		{
			example:     "baremetal-redfish-fleet-imported-ceph-data-foundation",
			clusters:    []string{"dc1-ocp", "dc2-ocp"},
			binding:     "shared-ceph-data-foundation",
			extension:   "openshift-data-foundation-operator",
			storageTask: "storage.shared-ceph",
		},
		{
			example:        "baremetal-redfish-fleet-stretched-ceph-data-foundation",
			clusters:       []string{"dc1-ocp", "dc2-ocp"},
			binding:        "ceph-stretch-data-foundation",
			extension:      "ibm-fusion-data-foundation-operator",
			storageTask:    "storage.ceph-stretch",
			wantStorageJob: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.example, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", tc.example)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
			if err != nil {
				t.Fatalf("PlanApplyTasksChecked: %v", err)
			}
			if tc.wantStorageJob {
				assertTaskPresent(t, tasks, tc.storageTask)
			} else {
				assertTaskMissing(t, tasks, tc.storageTask)
			}
			for _, cluster := range tc.clusters {
				deps := []string{"wait." + cluster, "extension." + cluster + "." + tc.extension + ".wait"}
				if tc.wantStorageJob {
					deps = []string{"wait." + cluster, tc.storageTask, "extension." + cluster + "." + tc.extension + ".wait"}
				}
				assertTaskDeps(t, tasks, "storagebinding."+cluster+"."+tc.binding+".apply", deps...)
			}
		})
	}
}

func TestPlanApplyStorageTaskStateRendersWithoutConsumerClusterInfra(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-fleet-stretched-ceph-data-foundation")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "storage", PhaseNames: []string{ApplyPhaseStorage}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	var storageTask ApplyTask
	for _, task := range tasks {
		if task.Entry.ID == "storage.ceph-stretch" {
			storageTask = task
			break
		}
	}
	if storageTask.Entry.ID == "" {
		t.Fatalf("storage task not found in %v", applyTaskIDs(tasks))
	}
	result, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), storageTask.State)
	if err != nil {
		t.Fatalf("render storage task state: %v", err)
	}
	if len(result.InstallerAssets) != 0 {
		t.Fatalf("storage task rendered installer assets: %#v", result.InstallerAssets)
	}
}

func TestWriteStorageBindingExternalDetailsUsesRuntimeCredentials(t *testing.T) {
	state := storageBindingPlanningState()
	state.StorageExports[0].Spec.DataFoundation = &v1alpha1.StorageExportDataFoundationSpec{
		RBDPoolRef: v1alpha1.LocalObjectReference{Name: "rbd"},
		CephFSRef:  v1alpha1.LocalObjectReference{Name: "cephfs"},
	}
	clustersDir := t.TempDir()
	record := storageapply.DataFoundationBindingRecord{
		StorageCluster: "ceph",
		Binding:        "ceph-binding",
		Cluster:        "demo",
		Secrets: render.DataFoundationExternalSecrets{
			AdminSecret:          "admin-key",
			FSID:                 "fsid-123",
			MonSecret:            "mon-key",
			HealthcheckerKey:     "healthchecker-key",
			RBDNodeKey:           "rbd-node-key",
			RBDProvisionerKey:    "rbd-provisioner-key",
			CephFSNodeKey:        "cephfs-node-key",
			CephFSProvisionerKey: "cephfs-provisioner-key",
		},
	}
	if err := storageapply.SaveDataFoundationBindingRecord(clustersDir, record); err != nil {
		t.Fatalf("SaveDataFoundationBindingRecord: %v", err)
	}
	path := filepath.Join(t.TempDir(), "rook-ceph-external-cluster-details.yaml")
	err := writeStorageBindingExternalDetails(path, state, StorageBindingPlan{
		Cluster: "demo",
		Binding: state.StorageClusterBindings[0],
	}, clustersDir, t.TempDir())
	if err != nil {
		t.Fatalf("writeStorageBindingExternalDetails: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	detailsJSON := manifest["stringData"].(map[string]any)["external_cluster_details"].(string)
	if strings.Contains(detailsJSON, render.DataFoundationGeneratedAtApplyPlaceholder) {
		t.Fatalf("external_cluster_details still contains placeholder: %s", detailsJSON)
	}
	if !strings.Contains(detailsJSON, "rbd-node-key") {
		t.Fatalf("external_cluster_details missing runtime key: %s", detailsJSON)
	}
}

func TestWriteStorageBindingExternalDetailsUsesImportedSecret(t *testing.T) {
	state := externalStorageBindingPlanningState()
	secretPath := filepath.Join(t.TempDir(), "external-details.json")
	secretJSON := `[{"name":"rook-ceph-mon","kind":"Secret","data":{"fsid":"external-fsid"}}]`
	if err := os.WriteFile(secretPath, []byte(secretJSON), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	state.Environments = []v1alpha1.Environment{{
		SourcePath: filepath.Join(t.TempDir(), "environment.yaml"),
		Spec: v1alpha1.EnvironmentSpec{Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
			"shared-ceph-external-details": {File: secretPath},
		}},
	}}
	path := filepath.Join(t.TempDir(), "rook-ceph-external-cluster-details.yaml")
	err := writeStorageBindingExternalDetails(path, state, StorageBindingPlan{
		Cluster: "demo",
		Binding: state.StorageClusterBindings[0],
	}, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("writeStorageBindingExternalDetails: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	detailsJSON := manifest["stringData"].(map[string]any)["external_cluster_details"].(string)
	if detailsJSON != secretJSON {
		t.Fatalf("external_cluster_details = %s, want %s", detailsJSON, secretJSON)
	}
}

func TestPlanApplyAllOrdersKubeVirtChildInfraAfterHostReadiness(t *testing.T) {
	state := kubeVirtChildPlanningState(true)

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseProvider, ApplyPhaseCluster, ApplyPhaseClusters, ApplyPhaseExtensions}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskDeps(t, tasks, "infra.child-ocp.localhost", "wait.metal-ocp", "extension.metal-ocp.openshift-virtualization.wait")
	assertTaskResourceKeys(t, tasks, "infra.child-ocp.localhost", "host:localhost:mutating", "kubevirt:metal-ocp:bootwright-child-ocp")
	assertTaskResourceKeys(t, tasks, "boot.child-ocp", "kubevirt:metal-ocp:bootwright-child-ocp")
}

func TestPlanApplyAllRejectsScopedKubeVirtChildWithoutHostCluster(t *testing.T) {
	state := kubeVirtChildPlanningState(false)

	_, err := PlanApplyTasksChecked(ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseProvider, ApplyPhaseCluster, ApplyPhaseClusters, ApplyPhaseExtensions}}, state)
	if err == nil {
		t.Fatal("expected missing host cluster dependency error, got nil")
	}
	if !strings.Contains(err.Error(), `include metal-ocp in --scope or apply it first`) {
		t.Fatalf("error %q does not include scoped dependency remediation", err)
	}
}

func loadWorkflowFixtureState(t *testing.T, name string) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "state", "desired", "testdata", "good", name)})
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

func storageBindingPlanningState() v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{},
			},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "export"},
			Spec: v1alpha1.StorageExportSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			},
		}},
		StorageClusterBindings: []v1alpha1.StorageClusterBinding{{
			Metadata: v1alpha1.Metadata{Name: "ceph-binding"},
			Spec: v1alpha1.StorageClusterBindingSpec{
				StorageExportRef: v1alpha1.LocalObjectReference{Name: "export"},
				ClusterSelector:  v1alpha1.StorageClusterBindingClusterSelector{Names: []string{"demo"}},
			},
		}},
		ClusterExtensions: []v1alpha1.ClusterExtension{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterExtensionSpec{
				Type:     v1alpha1.ClusterExtensionTypeManifestSet,
				Provides: []string{v1alpha1.ClusterExtensionProvidesDataFoundation},
			},
		}},
		ClusterExtensionBindings: []v1alpha1.ClusterExtensionBinding{{
			Metadata: v1alpha1.Metadata{Name: "odf-binding"},
			Spec: v1alpha1.ClusterExtensionBindingSpec{
				ClusterSelector: v1alpha1.ClusterExtensionClusterSelector{Names: []string{"demo"}},
				Extensions:      []v1alpha1.LocalObjectReference{{Name: "odf"}},
			},
		}},
	}
}

func externalStorageBindingPlanningState() v1alpha1.State {
	state := storageBindingPlanningState()
	state.StorageClusters = []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "shared-ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: v1alpha1.StorageClusterManagementExternal,
		},
	}}
	state.StorageExports = []v1alpha1.StorageExport{{
		Metadata: v1alpha1.Metadata{Name: "shared-ceph-export"},
		Spec: v1alpha1.StorageExportSpec{
			Type:              v1alpha1.StorageExportTypeDataFoundation,
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "shared-ceph"},
			DataFoundation: &v1alpha1.StorageExportDataFoundationSpec{
				ExternalDetailsRef: v1alpha1.SecretRef{Name: "shared-ceph-external-details"},
			},
		},
	}}
	state.StorageClusterBindings = []v1alpha1.StorageClusterBinding{{
		Metadata: v1alpha1.Metadata{Name: "shared-ceph-binding"},
		Spec: v1alpha1.StorageClusterBindingSpec{
			StorageExportRef: v1alpha1.LocalObjectReference{Name: "shared-ceph-export"},
			ClusterSelector:  v1alpha1.StorageClusterBindingClusterSelector{Names: []string{"demo"}},
			DataFoundation: v1alpha1.StorageClusterBindingDataFoundation{
				Namespace:          "openshift-storage",
				StorageClusterName: "ocs-external-storagecluster",
				StorageSystemName:  "ocs-external-storagecluster-storagesystem",
			},
		},
	}}
	return state
}

func kubeVirtChildPlanningState(includeParent bool) v1alpha1.State {
	clusters := []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "child-ocp"},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{
			Hostname: "master-0",
			MachineRef: v1alpha1.NodeMachineRef{
				ClusterInfra: "child-ocp-infra",
				Name:         "master-0",
			},
		}}},
	}}
	if includeParent {
		clusters = append(clusters, v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: "metal-ocp"}})
	}
	return v1alpha1.State{
		ContainerClusters: clusters,
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "child-ocp-infra"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{Machines: []v1alpha1.ClusterMachineComponent{{
					Name: "master-0",
					From: v1alpha1.From{Provider: "child-kubevirt-provider", Profile: "sno"},
				}}},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "child-kubevirt-provider"},
			Spec: v1alpha1.InfraProviderSpec{MachineProfiles: []v1alpha1.MachineProfileCapability{{
				Name: "sno",
				KubeVirt: &v1alpha1.MachineProfileKubeVirtProvisioner{
					HostClusterRef: &v1alpha1.LocalObjectReference{Name: "metal-ocp"},
					Namespace:      "bootwright-child-ocp",
				},
			}}},
		}},
		ClusterExtensions: []v1alpha1.ClusterExtension{{
			Metadata: v1alpha1.Metadata{Name: "openshift-virtualization"},
			Spec: v1alpha1.ClusterExtensionSpec{
				Type:     v1alpha1.ClusterExtensionTypeManifestSet,
				Provides: []string{v1alpha1.ClusterExtensionProvidesKubeVirt},
			},
		}},
		ClusterExtensionBindings: []v1alpha1.ClusterExtensionBinding{{
			Metadata: v1alpha1.Metadata{Name: "virt"},
			Spec: v1alpha1.ClusterExtensionBindingSpec{
				ClusterSelector: v1alpha1.ClusterExtensionClusterSelector{Names: []string{"metal-ocp"}},
				Extensions:      []v1alpha1.LocalObjectReference{{Name: "openshift-virtualization"}},
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

func applyAllTarget() ApplyTarget {
	return ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseProvider, ApplyPhaseCluster, ApplyPhaseStorage, ApplyPhaseClusters, ApplyPhaseExtensions}}
}

func assertTaskPresent(t *testing.T, tasks []ApplyTask, id string) {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID == id {
			return
		}
	}
	t.Fatalf("task %s not found in %+v", id, applyTaskIDs(tasks))
}

func assertTaskMissing(t *testing.T, tasks []ApplyTask, id string) {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID == id {
			t.Fatalf("task %s unexpectedly found in %+v", id, applyTaskIDs(tasks))
		}
	}
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

func assertTaskResourceKeys(t *testing.T, tasks []ApplyTask, id string, want ...string) {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID != id {
			continue
		}
		if !reflect.DeepEqual(task.Entry.ResourceKeys, want) {
			t.Fatalf("%s resource keys = %v, want %v", id, task.Entry.ResourceKeys, want)
		}
		return
	}
	t.Fatalf("task %s not found in %+v", id, applyTaskIDs(tasks))
}
