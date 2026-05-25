package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/workflow"
)

func TestPlanApplyTasksBuildsDependencies(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	tasks := workflow.PlanApplyTasks(allScope.applyTarget(), state)
	if len(tasks) != 5 {
		t.Fatalf("planned %d tasks, want 5: %+v", len(tasks), tasks)
	}
	if tasks[0].Entry.ID != "provider.lab-host" {
		t.Fatalf("first task = %s, want provider.lab-host", tasks[0].Entry.ID)
	}
	if tasks[0].Entry.Host != "lab-host" || len(tasks[0].Entry.ResourceKeys) != 1 {
		t.Fatalf("provider host/resources = %q/%v, want lab-host with resource key", tasks[0].Entry.Host, tasks[0].Entry.ResourceKeys)
	}
	if tasks[1].Entry.ID != "infra.sno-libvirt.lab-host" {
		t.Fatalf("second task = %s, want infra.sno-libvirt.lab-host", tasks[1].Entry.ID)
	}
	if len(tasks[1].Entry.Dependencies) != 1 || tasks[1].Entry.Dependencies[0] != "provider.lab-host" {
		t.Fatalf("infra deps = %v, want provider.lab-host", tasks[1].Entry.Dependencies)
	}
	if tasks[2].Entry.ID != "iso.sno-libvirt" {
		t.Fatalf("third task = %s, want iso.sno-libvirt", tasks[2].Entry.ID)
	}
	if len(tasks[2].Entry.Dependencies) != 1 || tasks[2].Entry.Dependencies[0] != "infra.sno-libvirt.lab-host" {
		t.Fatalf("iso deps = %v, want infra.sno-libvirt.lab-host", tasks[2].Entry.Dependencies)
	}
	if tasks[3].Entry.ID != "boot.sno-libvirt" {
		t.Fatalf("fourth task = %s, want boot.sno-libvirt", tasks[3].Entry.ID)
	}
	if len(tasks[3].Entry.Dependencies) != 1 || tasks[3].Entry.Dependencies[0] != "iso.sno-libvirt" {
		t.Fatalf("boot deps = %v, want iso.sno-libvirt", tasks[3].Entry.Dependencies)
	}
	if tasks[4].Entry.ID != "wait.sno-libvirt" {
		t.Fatalf("fifth task = %s, want wait.sno-libvirt", tasks[4].Entry.ID)
	}
	if len(tasks[4].Entry.Dependencies) != 1 || tasks[4].Entry.Dependencies[0] != "boot.sno-libvirt" {
		t.Fatalf("wait deps = %v, want boot.sno-libvirt", tasks[4].Entry.Dependencies)
	}
}

func TestPlanApplyTasksClusterScopeHasIndependentInstallTask(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	tasks := workflow.PlanApplyTasks(clusterScope.applyTarget(), state)
	if len(tasks) != 3 {
		t.Fatalf("planned %d tasks, want 3: %+v", len(tasks), tasks)
	}
	if tasks[0].Entry.ID != "iso.sno-libvirt" {
		t.Fatalf("task = %s, want iso.sno-libvirt", tasks[0].Entry.ID)
	}
	if len(tasks[0].Entry.Dependencies) != 0 {
		t.Fatalf("cluster-only iso deps = %v, want none", tasks[0].Entry.Dependencies)
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
	tasks := workflow.PlanApplyTasks(clusterScope.applyTarget(), state)
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
	tasks := workflow.PlanApplyTasks(clusterScope.applyTarget(), state)
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

func TestRunApplyTaskGraphStreamsAnsibleOutput(t *testing.T) {
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
	stateDir := filepath.Join(dir, "state")
	runtimeDir := filepath.Join(dir, "runtime-root")
	task := workflow.ApplyTask{
		Entry: workflow.TaskLedgerEntry{
			ID:     "provider",
			Kind:   workflow.ApplyTaskKindProvider,
			Label:  "provider services",
			Status: workflow.TaskStatusPending,
		},
		Playbook: "playbooks/layers/providers/apply.yml",
		State:    state,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ledger, err := workflow.RunApplyTaskGraph(context.Background(), &stdout, &stderr, stateDir, workflow.RunOptions{
		State:             state,
		StateDir:          stateDir,
		RuntimeDir:        runtimeDir,
		SecretsDir:        filepath.Join(dir, "secrets"),
		HostStateDir:      filepath.Join(dir, "host-state"),
		Executable:        executable,
		BundleDir:         filepath.Join(dir, "bundle"),
		ArtifactsBaseName: "provider",
	}, infraScope.applyTarget(), "", []workflow.ApplyTask{task}, workflow.ConcurrencyLimits{Parallelism: 1}, nil)
	if err != nil {
		t.Fatalf("workflow.RunApplyTaskGraph: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "ansible-stdout-line") {
		t.Fatalf("stdout missing live ansible output:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "ansible-stderr-line") {
		t.Fatalf("stderr missing live ansible output:\n%s", stderr.String())
	}
	if strings.Contains(stdout.String(), "log "+stateDir) {
		t.Fatalf("stdout should not point normal progress at the ansible log:\n%s", stdout.String())
	}
	logPath := workflow.TaskLogPath(runtimeDir, ledger.RunID, "provider")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read ansible output log: %v", err)
	}
	for _, want := range []string{"ansible-stdout-line", "ansible-stderr-line"} {
		if !strings.Contains(string(logData), want) {
			t.Fatalf("ansible output log missing %q:\n%s", want, string(logData))
		}
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
	stateDir := filepath.Join(dir, "state")
	runtimeDir := filepath.Join(dir, "runtime-root")
	tasks := []workflow.ApplyTask{
		{
			Entry: workflow.TaskLedgerEntry{
				ID:      "iso.cluster-a",
				Kind:    workflow.ApplyTaskKindClusterISO,
				Label:   "iso cluster-a",
				Cluster: "cluster-a",
				Status:  workflow.TaskStatusPending,
			},
			Playbook:      "playbooks/layers/openshift/create-agent-iso.yml",
			ExtraVarPairs: []string{"bootwright_task_cluster_name=cluster-a"},
			State:         state,
		},
		{
			Entry: workflow.TaskLedgerEntry{
				ID:      "iso.cluster-b",
				Kind:    workflow.ApplyTaskKindClusterISO,
				Label:   "iso cluster-b",
				Cluster: "cluster-b",
				Status:  workflow.TaskStatusPending,
			},
			Playbook:      "playbooks/layers/openshift/create-agent-iso.yml",
			ExtraVarPairs: []string{"bootwright_task_cluster_name=cluster-b"},
			State:         state,
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ledger, err := workflow.RunApplyTaskGraph(context.Background(), &stdout, &stderr, stateDir, workflow.RunOptions{
		State:             state,
		StateDir:          stateDir,
		RuntimeDir:        runtimeDir,
		SecretsDir:        filepath.Join(dir, "secrets"),
		HostStateDir:      filepath.Join(dir, "host-state"),
		Executable:        executable,
		BundleDir:         filepath.Join(dir, "bundle"),
		ArtifactsBaseName: "cluster",
	}, clusterScope.applyTarget(), "", tasks, workflow.ConcurrencyLimits{}, newApplyReporter(&stdout, &stderr))
	if err != nil {
		t.Fatalf("workflow.RunApplyTaskGraph: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "ansible stdout") || strings.Contains(stderr.String(), "ansible stderr") {
		t.Fatalf("multi-cluster output streamed ansible to terminal\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	for _, want := range []string{"Logs", "cluster-a:", "cluster-b:", "Create agent ISOs"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	for _, cluster := range []string{"cluster-a", "cluster-b"} {
		logPath := workflow.ApplyClusterLogPath(runtimeDir, ledger.RunID, cluster)
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read cluster log %s: %v", logPath, err)
		}
		if !strings.Contains(string(data), "ansible stdout "+cluster) || !strings.Contains(string(data), "ansible stderr "+cluster) {
			t.Fatalf("cluster log %s missing ansible output:\n%s", cluster, data)
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
	stateDir := filepath.Join(dir, "state")
	runtimeDir := filepath.Join(dir, "runtime-root")
	tasks := []workflow.ApplyTask{
		{
			Entry: workflow.TaskLedgerEntry{
				ID:           "provider.a",
				Kind:         workflow.ApplyTaskKindProvider,
				Label:        "provider services a",
				ResourceKeys: []string{"host:provider-01:mutating"},
				Status:       workflow.TaskStatusPending,
			},
			Playbook:      "playbooks/layers/providers/apply.yml",
			ExtraVarPairs: []string{"bootwright_test_lock_dir=" + lockDir},
			State:         state,
		},
		{
			Entry: workflow.TaskLedgerEntry{
				ID:           "provider.b",
				Kind:         workflow.ApplyTaskKindProvider,
				Label:        "provider services b",
				ResourceKeys: []string{"host:provider-01:mutating"},
				Status:       workflow.TaskStatusPending,
			},
			Playbook:      "playbooks/layers/providers/apply.yml",
			ExtraVarPairs: []string{"bootwright_test_lock_dir=" + lockDir},
			State:         state,
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := workflow.RunApplyTaskGraph(context.Background(), &stdout, &stderr, stateDir, workflow.RunOptions{
		State:             state,
		StateDir:          stateDir,
		RuntimeDir:        runtimeDir,
		SecretsDir:        filepath.Join(dir, "secrets"),
		HostStateDir:      filepath.Join(dir, "host-state"),
		Executable:        executable,
		BundleDir:         filepath.Join(dir, "bundle"),
		ArtifactsBaseName: "provider",
	}, infraScope.applyTarget(), "", tasks, workflow.ConcurrencyLimits{}, nil); err != nil {
		t.Fatalf("workflow.RunApplyTaskGraph should serialize same resource key: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
}
