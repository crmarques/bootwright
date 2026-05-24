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
	tasks := planApplyTasks(allScope, state)
	if len(tasks) != 5 {
		t.Fatalf("planned %d tasks, want 5: %+v", len(tasks), tasks)
	}
	if tasks[0].entry.ID != "provider.lab-host" {
		t.Fatalf("first task = %s, want provider.lab-host", tasks[0].entry.ID)
	}
	if tasks[0].entry.Host != "lab-host" || len(tasks[0].entry.ResourceKeys) != 1 {
		t.Fatalf("provider host/resources = %q/%v, want lab-host with resource key", tasks[0].entry.Host, tasks[0].entry.ResourceKeys)
	}
	if tasks[1].entry.ID != "infra.sno-libvirt.lab-host" {
		t.Fatalf("second task = %s, want infra.sno-libvirt.lab-host", tasks[1].entry.ID)
	}
	if len(tasks[1].entry.Dependencies) != 1 || tasks[1].entry.Dependencies[0] != "provider.lab-host" {
		t.Fatalf("infra deps = %v, want provider.lab-host", tasks[1].entry.Dependencies)
	}
	if tasks[2].entry.ID != "iso.sno-libvirt" {
		t.Fatalf("third task = %s, want iso.sno-libvirt", tasks[2].entry.ID)
	}
	if len(tasks[2].entry.Dependencies) != 1 || tasks[2].entry.Dependencies[0] != "infra.sno-libvirt.lab-host" {
		t.Fatalf("iso deps = %v, want infra.sno-libvirt.lab-host", tasks[2].entry.Dependencies)
	}
	if tasks[3].entry.ID != "boot.sno-libvirt.master-0" {
		t.Fatalf("fourth task = %s, want boot.sno-libvirt.master-0", tasks[3].entry.ID)
	}
	if len(tasks[3].entry.Dependencies) != 1 || tasks[3].entry.Dependencies[0] != "iso.sno-libvirt" {
		t.Fatalf("boot deps = %v, want iso.sno-libvirt", tasks[3].entry.Dependencies)
	}
	if tasks[4].entry.ID != "wait.sno-libvirt" {
		t.Fatalf("fifth task = %s, want wait.sno-libvirt", tasks[4].entry.ID)
	}
	if len(tasks[4].entry.Dependencies) != 1 || tasks[4].entry.Dependencies[0] != "boot.sno-libvirt.master-0" {
		t.Fatalf("wait deps = %v, want boot.sno-libvirt.master-0", tasks[4].entry.Dependencies)
	}
}

func TestPlanApplyTasksClusterScopeHasIndependentInstallTask(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	tasks := planApplyTasks(clusterScope, state)
	if len(tasks) != 3 {
		t.Fatalf("planned %d tasks, want 3: %+v", len(tasks), tasks)
	}
	if tasks[0].entry.ID != "iso.sno-libvirt" {
		t.Fatalf("task = %s, want iso.sno-libvirt", tasks[0].entry.ID)
	}
	if len(tasks[0].entry.Dependencies) != 0 {
		t.Fatalf("cluster-only iso deps = %v, want none", tasks[0].entry.Dependencies)
	}
	if tasks[1].entry.ID != "boot.sno-libvirt.master-0" {
		t.Fatalf("task = %s, want boot.sno-libvirt.master-0", tasks[1].entry.ID)
	}
	if len(tasks[1].entry.Dependencies) != 1 || tasks[1].entry.Dependencies[0] != "iso.sno-libvirt" {
		t.Fatalf("boot deps = %v, want iso.sno-libvirt", tasks[1].entry.Dependencies)
	}
	if tasks[2].entry.ID != "wait.sno-libvirt" {
		t.Fatalf("task = %s, want wait.sno-libvirt", tasks[2].entry.ID)
	}
	if len(tasks[2].entry.Dependencies) != 1 || tasks[2].entry.Dependencies[0] != "boot.sno-libvirt.master-0" {
		t.Fatalf("wait deps = %v, want boot.sno-libvirt.master-0", tasks[2].entry.Dependencies)
	}
}

func TestPlanApplyTasksBootsAllClusterMachinesBeforeWait(t *testing.T) {
	state := loadFixtureState(t, "005-3nodes-baremetal")
	tasks := planApplyTasks(clusterScope, state)
	if len(tasks) != 5 {
		t.Fatalf("planned %d tasks, want 5: %+v", len(tasks), tasks)
	}
	wantBootIDs := []string{
		"boot.3-nodes-ocp-baremetal.master-0",
		"boot.3-nodes-ocp-baremetal.master-1",
		"boot.3-nodes-ocp-baremetal.master-2",
	}
	for i, want := range wantBootIDs {
		task := tasks[i+1]
		if task.entry.ID != want {
			t.Fatalf("boot task %d = %s, want %s", i, task.entry.ID, want)
		}
		if task.entry.Kind != applyTaskKindNodeBoot {
			t.Fatalf("boot task kind = %s, want %s", task.entry.Kind, applyTaskKindNodeBoot)
		}
		if len(task.entry.Dependencies) != 1 || task.entry.Dependencies[0] != "iso.3-nodes-ocp-baremetal" {
			t.Fatalf("boot deps = %v, want iso.3-nodes-ocp-baremetal", task.entry.Dependencies)
		}
		if len(task.entry.ResourceKeys) != 1 {
			t.Fatalf("boot resource keys = %v, want one Redfish key", task.entry.ResourceKeys)
		}
	}
	wait := tasks[4]
	if wait.entry.ID != "wait.3-nodes-ocp-baremetal" {
		t.Fatalf("wait task = %s, want wait.3-nodes-ocp-baremetal", wait.entry.ID)
	}
	if len(wait.entry.Dependencies) != len(wantBootIDs) {
		t.Fatalf("wait deps = %v, want %v", wait.entry.Dependencies, wantBootIDs)
	}
	for i, want := range wantBootIDs {
		if wait.entry.Dependencies[i] != want {
			t.Fatalf("wait dep %d = %s, want %s", i, wait.entry.Dependencies[i], want)
		}
	}
}

func TestResolveApplyConcurrencyLimitsUsesSafeAutoMaximum(t *testing.T) {
	state := loadFixtureState(t, "005-3nodes-baremetal")
	tasks := planApplyTasks(clusterScope, state)
	limits := resolveApplyConcurrencyLimits(workflow.ConcurrencyLimits{}, tasks)
	if limits.Parallelism != len(tasks) {
		t.Fatalf("global parallelism = %d, want %d", limits.Parallelism, len(tasks))
	}
	if limits.ParallelismPerHost != 1 {
		t.Fatalf("per-host parallelism = %d, want 1 safety lock", limits.ParallelismPerHost)
	}
	if limits.ParallelismRedfish != 3 {
		t.Fatalf("redfish parallelism = %d, want 3 node boot tasks", limits.ParallelismRedfish)
	}

	limited := resolveApplyConcurrencyLimits(workflow.ConcurrencyLimits{
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
	task := applyTask{
		entry: workflow.TaskLedgerEntry{
			ID:     "provider",
			Kind:   applyTaskKindProvider,
			Label:  "provider services",
			Status: workflow.TaskStatusPending,
		},
		playbook: "playbooks/layers/providers/apply.yml",
		state:    state,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ledger, err := runApplyTaskGraph(context.Background(), &stdout, &stderr, stateDir, workflow.RunOptions{
		State:             state,
		StateDir:          stateDir,
		SecretsDir:        filepath.Join(dir, "secrets"),
		HostStateDir:      filepath.Join(dir, "host-state"),
		Executable:        executable,
		BundleDir:         filepath.Join(dir, "bundle"),
		ArtifactsBaseName: "provider",
	}, infraScope, "", []applyTask{task}, workflow.ConcurrencyLimits{Parallelism: 1})
	if err != nil {
		t.Fatalf("runApplyTaskGraph: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
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
	logPath := taskLogPath(stateDir, ledger.RunID, "provider")
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
	tasks := []applyTask{
		{
			entry: workflow.TaskLedgerEntry{
				ID:      "iso.cluster-a",
				Kind:    applyTaskKindClusterISO,
				Label:   "iso cluster-a",
				Cluster: "cluster-a",
				Status:  workflow.TaskStatusPending,
			},
			playbook:      "playbooks/layers/openshift/create-agent-iso.yml",
			extraVarPairs: []string{"bootwright_task_cluster_name=cluster-a"},
			state:         state,
		},
		{
			entry: workflow.TaskLedgerEntry{
				ID:      "iso.cluster-b",
				Kind:    applyTaskKindClusterISO,
				Label:   "iso cluster-b",
				Cluster: "cluster-b",
				Status:  workflow.TaskStatusPending,
			},
			playbook:      "playbooks/layers/openshift/create-agent-iso.yml",
			extraVarPairs: []string{"bootwright_task_cluster_name=cluster-b"},
			state:         state,
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ledger, err := runApplyTaskGraph(context.Background(), &stdout, &stderr, stateDir, workflow.RunOptions{
		State:             state,
		StateDir:          stateDir,
		SecretsDir:        filepath.Join(dir, "secrets"),
		HostStateDir:      filepath.Join(dir, "host-state"),
		Executable:        executable,
		BundleDir:         filepath.Join(dir, "bundle"),
		ArtifactsBaseName: "cluster",
	}, clusterScope, "", tasks, workflow.ConcurrencyLimits{})
	if err != nil {
		t.Fatalf("runApplyTaskGraph: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
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
		logPath := applyClusterLogPath(stateDir, ledger.RunID, cluster)
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
	tasks := []applyTask{
		{
			entry: workflow.TaskLedgerEntry{
				ID:           "provider.a",
				Kind:         applyTaskKindProvider,
				Label:        "provider services a",
				ResourceKeys: []string{"host:provider-01:mutating"},
				Status:       workflow.TaskStatusPending,
			},
			playbook:      "playbooks/layers/providers/apply.yml",
			extraVarPairs: []string{"bootwright_test_lock_dir=" + lockDir},
			state:         state,
		},
		{
			entry: workflow.TaskLedgerEntry{
				ID:           "provider.b",
				Kind:         applyTaskKindProvider,
				Label:        "provider services b",
				ResourceKeys: []string{"host:provider-01:mutating"},
				Status:       workflow.TaskStatusPending,
			},
			playbook:      "playbooks/layers/providers/apply.yml",
			extraVarPairs: []string{"bootwright_test_lock_dir=" + lockDir},
			state:         state,
		},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := runApplyTaskGraph(context.Background(), &stdout, &stderr, stateDir, workflow.RunOptions{
		State:             state,
		StateDir:          stateDir,
		SecretsDir:        filepath.Join(dir, "secrets"),
		HostStateDir:      filepath.Join(dir, "host-state"),
		Executable:        executable,
		BundleDir:         filepath.Join(dir, "bundle"),
		ArtifactsBaseName: "provider",
	}, infraScope, "", tasks, workflow.ConcurrencyLimits{}); err != nil {
		t.Fatalf("runApplyTaskGraph should serialize same resource key: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
}
