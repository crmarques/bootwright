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
	if tasks[0].entry.ID != "provider" {
		t.Fatalf("first task = %s, want provider", tasks[0].entry.ID)
	}
	if tasks[1].entry.ID != "infra.sno-libvirt" {
		t.Fatalf("second task = %s, want infra.sno-libvirt", tasks[1].entry.ID)
	}
	if len(tasks[1].entry.Dependencies) != 1 || tasks[1].entry.Dependencies[0] != "provider" {
		t.Fatalf("infra deps = %v, want provider", tasks[1].entry.Dependencies)
	}
	if tasks[2].entry.ID != "iso.sno-libvirt" {
		t.Fatalf("third task = %s, want iso.sno-libvirt", tasks[2].entry.ID)
	}
	if len(tasks[2].entry.Dependencies) != 1 || tasks[2].entry.Dependencies[0] != "infra.sno-libvirt" {
		t.Fatalf("iso deps = %v, want infra.sno-libvirt", tasks[2].entry.Dependencies)
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
