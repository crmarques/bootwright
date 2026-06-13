package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestPlanApplyTasksBuildsDependencies(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	tasks := workflow.PlanApplyTasks(converge.AllScope.ApplyTarget(), state)
	if len(tasks) != 8 {
		t.Fatalf("planned %d tasks, want 8: %+v", len(tasks), tasks)
	}
	if tasks[0].Entry.ID != "provider.bastion" {
		t.Fatalf("first task = %s, want provider.bastion", tasks[0].Entry.ID)
	}
	if tasks[0].Entry.Host != "bastion" || len(tasks[0].Entry.ResourceKeys) != 1 {
		t.Fatalf("provider host/resources = %q/%v, want bastion with resource key", tasks[0].Entry.Host, tasks[0].Entry.ResourceKeys)
	}
	if tasks[1].Entry.ID != "infra-component.bastion" {
		t.Fatalf("second task = %s, want infra-component.bastion", tasks[1].Entry.ID)
	}
	if tasks[1].Entry.Host != "bastion" || len(tasks[1].Entry.ResourceKeys) != 1 {
		t.Fatalf("infra component host/resources = %q/%v, want bastion with resource key", tasks[1].Entry.Host, tasks[1].Entry.ResourceKeys)
	}
	if tasks[2].Entry.ID != "infraprepare.sno-libvirt.bastion" {
		t.Fatalf("third task = %s, want infraprepare.sno-libvirt.bastion", tasks[2].Entry.ID)
	}
	if len(tasks[2].Entry.Dependencies) != 2 || tasks[2].Entry.Dependencies[0] != "provider.bastion" || tasks[2].Entry.Dependencies[1] != "infra-component.bastion" {
		t.Fatalf("infra prepare deps = %v, want provider and infra-component services", tasks[2].Entry.Dependencies)
	}
	if tasks[3].Entry.ID != "infra.sno-libvirt.master-0" {
		t.Fatalf("fourth task = %s, want infra.sno-libvirt.master-0", tasks[3].Entry.ID)
	}
	if len(tasks[3].Entry.Dependencies) != 3 || tasks[3].Entry.Dependencies[0] != "provider.bastion" || tasks[3].Entry.Dependencies[1] != "infra-component.bastion" || tasks[3].Entry.Dependencies[2] != "infraprepare.sno-libvirt.bastion" {
		t.Fatalf("machine infra deps = %v, want provider, infra-component, and prepare", tasks[3].Entry.Dependencies)
	}
	if tasks[3].Entry.HostSlotKey != "host:bastion:machine" || tasks[3].Entry.HostSlotCount != 1 {
		t.Fatalf("machine infra host slot = %q/%d, want host:bastion:machine/1", tasks[3].Entry.HostSlotKey, tasks[3].Entry.HostSlotCount)
	}
	if tasks[4].Entry.ID != "infrafinalize.sno-libvirt.bastion" {
		t.Fatalf("fifth task = %s, want infrafinalize.sno-libvirt.bastion", tasks[4].Entry.ID)
	}
	if len(tasks[4].Entry.Dependencies) != 3 || tasks[4].Entry.Dependencies[0] != "provider.bastion" || tasks[4].Entry.Dependencies[1] != "infra-component.bastion" || tasks[4].Entry.Dependencies[2] != "infra.sno-libvirt.master-0" {
		t.Fatalf("infra finalize deps = %v, want provider, infra-component, and machine infra", tasks[4].Entry.Dependencies)
	}
	if tasks[5].Entry.ID != "iso.sno-libvirt" {
		t.Fatalf("sixth task = %s, want iso.sno-libvirt", tasks[5].Entry.ID)
	}
	if len(tasks[5].Entry.Dependencies) != 1 || tasks[5].Entry.Dependencies[0] != "infrafinalize.sno-libvirt.bastion" {
		t.Fatalf("iso deps = %v, want infrafinalize.sno-libvirt.bastion", tasks[5].Entry.Dependencies)
	}
	if tasks[6].Entry.ID != "boot.sno-libvirt" {
		t.Fatalf("seventh task = %s, want boot.sno-libvirt", tasks[6].Entry.ID)
	}
	if len(tasks[6].Entry.Dependencies) != 1 || tasks[6].Entry.Dependencies[0] != "iso.sno-libvirt" {
		t.Fatalf("boot deps = %v, want iso.sno-libvirt", tasks[6].Entry.Dependencies)
	}
	if tasks[7].Entry.ID != "wait.sno-libvirt" {
		t.Fatalf("eighth task = %s, want wait.sno-libvirt", tasks[7].Entry.ID)
	}
	if len(tasks[7].Entry.Dependencies) != 1 || tasks[7].Entry.Dependencies[0] != "boot.sno-libvirt" {
		t.Fatalf("wait deps = %v, want boot.sno-libvirt", tasks[7].Entry.Dependencies)
	}
}

func TestPlanApplyTasksContainerClusterScopeHasIndependentInstallTask(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	tasks := workflow.PlanApplyTasks(converge.ContainerClusterScope.ApplyTarget(), state)
	if len(tasks) != 3 {
		t.Fatalf("planned %d tasks, want 3: %+v", len(tasks), tasks)
	}
	if tasks[0].Entry.ID != "iso.sno-libvirt" {
		t.Fatalf("task = %s, want iso.sno-libvirt", tasks[0].Entry.ID)
	}
	if len(tasks[0].Entry.Dependencies) != 0 {
		t.Fatalf("container-cluster-only iso deps = %v, want none", tasks[0].Entry.Dependencies)
	}
	if tasks[1].Entry.ID != "boot.sno-libvirt" {
		t.Fatalf("task = %s, want boot.sno-libvirt", tasks[1].Entry.ID)
	}
	if len(tasks[1].Entry.Dependencies) != 1 || tasks[1].Entry.Dependencies[0] != "iso.sno-libvirt" {
		t.Fatalf("boot deps = %v, want iso.sno-libvirt", tasks[1].Entry.Dependencies)
	}
	if tasks[2].Entry.ID != "wait.sno-libvirt" {
		t.Fatalf("task = %s, want wait.sno-libvirt", tasks[2].Entry.ID)
	}
	if len(tasks[2].Entry.Dependencies) != 1 || tasks[2].Entry.Dependencies[0] != "boot.sno-libvirt" {
		t.Fatalf("wait deps = %v, want boot.sno-libvirt", tasks[2].Entry.Dependencies)
	}
}

func TestPlanApplyTasksBootsAllClusterMachinesBeforeWait(t *testing.T) {
	state := loadFixtureState(t, "005-3nodes-baremetal")
	tasks := workflow.PlanApplyTasks(converge.ContainerClusterScope.ApplyTarget(), state)
	if len(tasks) != 3 {
		t.Fatalf("planned %d tasks, want 3: %+v", len(tasks), tasks)
	}
	boot := tasks[1]
	if boot.Entry.ID != "boot.3-nodes-ocp-baremetal" {
		t.Fatalf("boot task = %s, want boot.3-nodes-ocp-baremetal", boot.Entry.ID)
	}
	if boot.Entry.Kind != workflow.ApplyTaskKindNodeBoot {
		t.Fatalf("boot task kind = %s, want %s", boot.Entry.Kind, workflow.ApplyTaskKindNodeBoot)
	}
	if len(boot.Entry.Dependencies) != 1 || boot.Entry.Dependencies[0] != "iso.3-nodes-ocp-baremetal" {
		t.Fatalf("boot deps = %v, want iso.3-nodes-ocp-baremetal", boot.Entry.Dependencies)
	}
	if len(boot.Entry.ResourceKeys) != 3 {
		t.Fatalf("boot resource keys = %v, want three Redfish keys", boot.Entry.ResourceKeys)
	}
	if boot.RedfishSlots != 3 {
		t.Fatalf("boot RedfishSlots = %d, want 3", boot.RedfishSlots)
	}
	wait := tasks[2]
	if wait.Entry.ID != "wait.3-nodes-ocp-baremetal" {
		t.Fatalf("wait task = %s, want wait.3-nodes-ocp-baremetal", wait.Entry.ID)
	}
	if len(wait.Entry.Dependencies) != 1 || wait.Entry.Dependencies[0] != "boot.3-nodes-ocp-baremetal" {
		t.Fatalf("wait deps = %v, want boot.3-nodes-ocp-baremetal", wait.Entry.Dependencies)
	}
}

func TestResolveApplyConcurrencyLimitsUsesSafeAutoMaximum(t *testing.T) {
	state := loadFixtureState(t, "005-3nodes-baremetal")
	tasks := workflow.PlanApplyTasks(converge.ContainerClusterScope.ApplyTarget(), state)
	limits := workflow.ResolveApplyConcurrencyLimits(workflow.ConcurrencyLimits{}, tasks)
	if limits.Parallelism != len(tasks) {
		t.Fatalf("global parallelism = %d, want %d", limits.Parallelism, len(tasks))
	}
	if limits.ParallelismPerHost != 1 {
		t.Fatalf("per-host parallelism = %d, want 1 safety lock", limits.ParallelismPerHost)
	}
	if limits.ParallelismRedfish != 3 {
		t.Fatalf("redfish parallelism = %d, want 3 node boot tasks", limits.ParallelismRedfish)
	}

	limited := workflow.ResolveApplyConcurrencyLimits(workflow.ConcurrencyLimits{
		Parallelism:        2,
		ParallelismPerHost: 8,
		ParallelismRedfish: 2,
	}, tasks)
	if limited.Parallelism != 2 || limited.ParallelismPerHost != 1 || limited.ParallelismRedfish != 2 {
		t.Fatalf("explicit limits = %+v, want global=2 perHost=1 redfish=2", limited)
	}
}

func TestApplyClusterPhaseLinesAggregateContainerAndStorageStates(t *testing.T) {
	clustersDir := filepath.Join(t.TempDir(), "clusters")
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "infra.cluster-a", Kind: workflow.ApplyTaskKindClusterInstall, Cluster: "cluster-a", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK},
		{ID: "iso.cluster-a", Kind: workflow.ApplyTaskKindClusterISO, Cluster: "cluster-a", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK},
		{ID: "boot.cluster-a", Kind: workflow.ApplyTaskKindNodeBoot, Cluster: "cluster-a", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusRunning},
		{ID: "wait.cluster-a", Kind: workflow.ApplyTaskKindInstallWait, Cluster: "cluster-a", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusPending},
		{ID: "storageinfra.ceph-a", Kind: workflow.ApplyTaskKindStorageInfra, Cluster: "ceph-a", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
		{ID: "storage.ceph-a", Kind: workflow.ApplyTaskKindStorageCluster, Cluster: "ceph-a", ClusterKind: workflow.ApplyClusterKindStorage, Status: workflow.TaskStatusOK},
		{ID: "storageattachment.cluster-a.openshift-data-foundation.external-storage.apply", Kind: workflow.ApplyTaskKindStorageAttachmentApply, Cluster: "cluster-a", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusBlocked, Dependencies: []string{"storage.ceph-a"}},
	}, now)

	lines := applyClusterPhaseLines(clustersDir, ledger)
	byName := map[string]output.ClusterPhaseLine{}
	for _, line := range lines {
		byName[line.Name] = line
	}
	container := byName["cluster-a"]
	if container.Kind != "ContainerCluster" {
		t.Fatalf("cluster-a kind = %q, want ContainerCluster", container.Kind)
	}
	requireApplyPhase(t, container, "Infrastructure", output.StatusOK)
	requireApplyPhase(t, container, "Prepare", output.StatusRunning)
	requireApplyPhase(t, container, "Install", output.StatusPending)
	requireApplyPhase(t, container, "Post-install", output.StatusBlocked)
	storage := byName["ceph-a"]
	if storage.Kind != "StorageCluster" {
		t.Fatalf("ceph-a kind = %q, want StorageCluster", storage.Kind)
	}
	requireApplyPhase(t, storage, "Infrastructure", output.StatusOK)
	requireApplyPhase(t, storage, "Provision", output.StatusOK)
	requireApplyPhase(t, storage, "Publish", output.StatusBlocked)
	if phasePresent(storage, "Prepare") {
		t.Fatalf("storage cluster should not expose a duplicate Prepare phase: %+v", storage.Phases)
	}
}

func TestApplyPhaseStatusTerminalStates(t *testing.T) {
	cases := []struct {
		name   string
		tasks  []workflow.TaskLedgerEntry
		status output.Status
	}{
		{name: "failed", tasks: []workflow.TaskLedgerEntry{{Status: workflow.TaskStatusOK}, {Status: workflow.TaskStatusFailed}}, status: output.StatusFailed},
		{name: "blocked", tasks: []workflow.TaskLedgerEntry{{Status: workflow.TaskStatusBlocked}, {Status: workflow.TaskStatusPending}}, status: output.StatusBlocked},
		{name: "cancelled", tasks: []workflow.TaskLedgerEntry{{Status: workflow.TaskStatusOK}, {Status: workflow.TaskStatusCancelled}}, status: output.StatusCancel},
		{name: "skipped", tasks: []workflow.TaskLedgerEntry{{Status: workflow.TaskStatusSkipped}, {Status: workflow.TaskStatusSkipped}}, status: output.StatusSkipped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyPhaseStatus(tc.tasks, output.StatusPending); got != tc.status {
				t.Fatalf("status = %s, want %s", got, tc.status)
			}
		})
	}
}

func requireApplyPhase(t *testing.T, line output.ClusterPhaseLine, label string, status output.Status) {
	t.Helper()
	for _, phase := range line.Phases {
		if phase.Label == label {
			if phase.Status != status {
				t.Fatalf("%s phase %s = %s, want %s", line.Name, label, phase.Status, status)
			}
			return
		}
	}
	t.Fatalf("%s missing phase %s in %+v", line.Name, label, line.Phases)
}

func phasePresent(line output.ClusterPhaseLine, label string) bool {
	for _, phase := range line.Phases {
		if phase.Label == label {
			return true
		}
	}
	return false
}

func TestApplySummaryPrintsInstallerLogPath(t *testing.T) {
	clustersDir := filepath.Join(t.TempDir(), "clusters")
	ledger := workflow.NewRunLedger("apply-test", "clusters", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{{
		ID:          "wait.sno-libvirt",
		Kind:        workflow.ApplyTaskKindInstallWait,
		Label:       "wait install sno-libvirt",
		Cluster:     "sno-libvirt",
		ClusterKind: workflow.ApplyClusterKindContainer,
		Status:      workflow.TaskStatusPending,
	}}, time.Now())
	ledger.MarkRunning("wait.sno-libvirt", filepath.Join(clustersDir, "ansible-output.log"), time.Now())

	var stdout bytes.Buffer
	printApplyRunSummary(&stdout, clustersDir, nil, ledger)

	want := workflow.OpenShiftInstallerLogPath(clustersDir, "sno-libvirt")
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("summary missing installer log path %q:\n%s", want, stdout.String())
	}
}

func TestApplySummaryPrintsClusterLogPaths(t *testing.T) {
	clustersDir := filepath.Join(t.TempDir(), "clusters")
	clusterLogPath := workflow.ApplyClusterLogPath(clustersDir, "apply-test", "sno-libvirt")
	ledger := workflow.NewRunLedger("apply-test", "clusters", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{{
		ID:             "wait.sno-libvirt",
		Kind:           workflow.ApplyTaskKindInstallWait,
		Label:          "wait install sno-libvirt",
		Cluster:        "sno-libvirt",
		ClusterKind:    workflow.ApplyClusterKindContainer,
		Status:         workflow.TaskStatusPending,
		ClusterLogPath: clusterLogPath,
	}}, time.Now())

	var stdout bytes.Buffer
	printApplyRunSummary(&stdout, clustersDir, nil, ledger)

	for _, want := range []string{
		clusterLogPath,
		workflow.OpenShiftInstallerLogPath(clustersDir, "sno-libvirt"),
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("summary missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunApplyTaskGraphWritesAnsibleOutputToLogs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/bin/sh
echo ansible-stdout-line
echo ansible-stderr-line >&2
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
		}},
	}
	renderedDir := filepath.Join(dir, "rendered")
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
	task := workflow.ApplyTask{
		Entry: workflow.TaskLedgerEntry{
			ID:     "provider",
			Kind:   workflow.ApplyTaskKindProvider,
			Label:  "provider services",
			Status: workflow.TaskStatusPending,
		},
		Playbook: "bootwright.core.task_provider_services_apply",
		State:    state,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ledger, err := workflow.RunApplyTaskGraph(context.Background(), &stdout, &stderr, runsDir, workflow.RunOptions{
		State:              state,
		RenderedDir:        renderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: managedServicesDir,
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		Executable:         executable,
		BundleDir:          filepath.Join(dir, "bundle"),
		ArtifactsBaseName:  "provider",
	}, converge.InfraScope.ApplyTarget(), "", []workflow.ApplyTask{task}, workflow.ConcurrencyLimits{Parallelism: 1}, nil, nil)
	if err != nil {
		t.Fatalf("workflow.RunApplyTaskGraph: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "ansible-stdout-line") || strings.Contains(stderr.String(), "ansible-stderr-line") {
		t.Fatalf("terminal output streamed ansible output\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	runLogData, err := os.ReadFile(workflow.ApplyRunLogPath(runsDir, ledger.RunID))
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	logPath := workflow.TaskLogPath(runsDir, ledger.RunID, "provider")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read ansible output log: %v", err)
	}
	for _, want := range []string{"ansible-stdout-line", "ansible-stderr-line"} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("ansible output log missing %q:\n%s", want, string(logData))
		}
		if !strings.Contains(string(runLogData), want) {
			t.Fatalf("run log missing %q:\n%s", want, string(runLogData))
		}
	}
}

func TestRunApplyTaskGraphFailureSummaryIsConcise(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/bin/sh
echo 'TASK [boot cluster-a nodes]'
echo 'fatal: [node-0]: FAILED! => {"msg":"Redfish boot media action failed"}'
i=1
while [ "$i" -le 10 ]; do
  echo "raw-tail-$i"
  i=$((i + 1))
done
exit 2
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
		}},
	}
	renderedDir := filepath.Join(dir, "rendered")
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	task := workflow.ApplyTask{
		Entry: workflow.TaskLedgerEntry{
			ID:     "provider",
			Kind:   workflow.ApplyTaskKindProvider,
			Label:  "provider services",
			Status: workflow.TaskStatusPending,
		},
		Playbook: "bootwright.core.task_provider_services_apply",
		State:    state,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ledger, err := workflow.RunApplyTaskGraph(context.Background(), &stdout, &stderr, runsDir, workflow.RunOptions{
		State:              state,
		RenderedDir:        renderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		Executable:         executable,
		BundleDir:          filepath.Join(dir, "bundle"),
		ArtifactsBaseName:  "provider",
	}, converge.InfraScope.ApplyTarget(), "", []workflow.ApplyTask{task}, workflow.ConcurrencyLimits{Parallelism: 1}, newApplyReporter(&stdout, &stderr, "test", runsDir, clustersDir, nil, false), nil)
	if err == nil {
		t.Fatalf("workflow.RunApplyTaskGraph succeeded unexpectedly\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	for _, text := range []string{stdout.String(), stderr.String(), err.Error(), ledger.FailedTasks()[0].Failure} {
		if strings.Contains(text, "raw-tail-10") {
			t.Fatalf("concise failure output leaked raw tail:\n%s", text)
		}
	}
	for _, want := range []string{"failed task: Provider services", "reason: fatal: [node-0]: FAILED!", "ansible-output.log"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("failure summary missing %q:\n%s", want, stdout.String())
		}
	}
	runLogData, err := os.ReadFile(workflow.ApplyRunLogPath(runsDir, ledger.RunID))
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	if !strings.Contains(string(runLogData), "raw-tail-10") {
		t.Fatalf("run log missing raw tail:\n%s", string(runLogData))
	}
}

func TestRunApplyTaskGraphMultiClusterWritesClusterLogsWithoutStreamingAnsible(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/bin/sh
cluster="unknown"
for arg in "$@"; do
  case "$arg" in
    bootwright_task_cluster_name=*) cluster="${arg#bootwright_task_cluster_name=}" ;;
  esac
done
echo "ansible stdout ${cluster}"
echo "ansible stderr ${cluster}" >&2
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
		}},
	}
	renderedDir := filepath.Join(dir, "rendered")
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
	tasks := []workflow.ApplyTask{
		{
			Entry: workflow.TaskLedgerEntry{
				ID:          "iso.cluster-a",
				Kind:        workflow.ApplyTaskKindClusterISO,
				Label:       "iso cluster-a",
				Cluster:     "cluster-a",
				ClusterKind: workflow.ApplyClusterKindContainer,
				Status:      workflow.TaskStatusPending,
			},
			Playbook:      "bootwright.core.task_container_cluster_create_agent_iso",
			ExtraVarPairs: []string{"bootwright_task_cluster_name=cluster-a"},
			State:         state,
		},
		{
			Entry: workflow.TaskLedgerEntry{
				ID:          "iso.cluster-b",
				Kind:        workflow.ApplyTaskKindClusterISO,
				Label:       "iso cluster-b",
				Cluster:     "cluster-b",
				ClusterKind: workflow.ApplyClusterKindContainer,
				Status:      workflow.TaskStatusPending,
			},
			Playbook:      "bootwright.core.task_container_cluster_create_agent_iso",
			ExtraVarPairs: []string{"bootwright_task_cluster_name=cluster-b"},
			State:         state,
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ledger, err := workflow.RunApplyTaskGraph(context.Background(), &stdout, &stderr, runsDir, workflow.RunOptions{
		State:              state,
		RenderedDir:        renderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: managedServicesDir,
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		Executable:         executable,
		BundleDir:          filepath.Join(dir, "bundle"),
		ArtifactsBaseName:  "clusters",
	}, converge.ClustersScope.ApplyTarget(), "", tasks, workflow.ConcurrencyLimits{}, newApplyReporter(&stdout, &stderr, "test", runsDir, clustersDir, nil, false), nil)
	if err != nil {
		t.Fatalf("workflow.RunApplyTaskGraph: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "ansible stdout") || strings.Contains(stderr.String(), "ansible stderr") {
		t.Fatalf("multi-cluster output streamed ansible to terminal\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	for _, want := range []string{"Run", "cluster-a (ContainerCluster)", "cluster-b (ContainerCluster)", "cluster-a log", "[DONE]"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	for _, cluster := range []string{"cluster-a", "cluster-b"} {
		logPath := workflow.ApplyClusterLogPath(clustersDir, ledger.RunID, cluster)
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read cluster log %s: %v", logPath, err)
		}
		if !strings.Contains(string(data), "ansible stdout "+cluster) || !strings.Contains(string(data), "ansible stderr "+cluster) {
			t.Fatalf("cluster log %s missing ansible output:\n%s", cluster, data)
		}
		taskLogPath := workflow.TaskLogPath(runsDir, ledger.RunID, "iso."+cluster)
		taskLog, err := os.ReadFile(taskLogPath)
		if err != nil {
			t.Fatalf("read task log %s: %v", taskLogPath, err)
		}
		if !strings.Contains(string(taskLog), "ansible stdout "+cluster) || !strings.Contains(string(taskLog), "ansible stderr "+cluster) {
			t.Fatalf("task log %s missing ansible output:\n%s", cluster, taskLog)
		}
	}
}

func TestRunApplyTaskGraphHonorsResourceLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/bin/sh
lock_dir=""
for arg in "$@"; do
  case "$arg" in
    bootwright_test_lock_dir=*) lock_dir="${arg#bootwright_test_lock_dir=}" ;;
  esac
done
if ! mkdir "$lock_dir"; then
  echo "concurrent execution detected" >&2
  exit 9
fi
sleep 0.1
rmdir "$lock_dir"
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
		}},
	}
	lockDir := filepath.Join(dir, "same-host.lock")
	renderedDir := filepath.Join(dir, "rendered")
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
	tasks := []workflow.ApplyTask{
		{
			Entry: workflow.TaskLedgerEntry{
				ID:           "provider.a",
				Kind:         workflow.ApplyTaskKindProvider,
				Label:        "provider services a",
				ResourceKeys: []string{"host:provider-01:mutating"},
				Status:       workflow.TaskStatusPending,
			},
			Playbook:      "bootwright.core.task_provider_services_apply",
			ExtraVarPairs: []string{"bootwright_test_lock_dir=" + lockDir},
			State:         state,
		},
		{
			Entry: workflow.TaskLedgerEntry{
				ID:           "infra-component.a",
				Kind:         workflow.ApplyTaskKindInfraComponentServices,
				Label:        "infra component services a",
				ResourceKeys: []string{"host:provider-01:mutating"},
				Status:       workflow.TaskStatusPending,
			},
			Playbook:      "bootwright.core.task_infra_component_services_apply",
			ExtraVarPairs: []string{"bootwright_test_lock_dir=" + lockDir},
			State:         state,
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := workflow.RunApplyTaskGraph(context.Background(), &stdout, &stderr, runsDir, workflow.RunOptions{
		State:              state,
		RenderedDir:        renderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: managedServicesDir,
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		Executable:         executable,
		BundleDir:          filepath.Join(dir, "bundle"),
		ArtifactsBaseName:  "provider",
	}, converge.InfraScope.ApplyTarget(), "", tasks, workflow.ConcurrencyLimits{}, nil, nil); err != nil {
		t.Fatalf("workflow.RunApplyTaskGraph should serialize same resource key: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
}
