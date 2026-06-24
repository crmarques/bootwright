package workflow

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestRunPreparedDestroyTaskGraphRunsStepsToLogs runs a real (empty-limit)
// destroy graph against a fake ansible-playbook and asserts the teardown runs
// every step, routes ansible output to the per-task log (never the terminal),
// and produces an ok ledger.
func TestRunPreparedDestroyTaskGraphRunsStepsToLogs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/bin/sh
echo destroy-stdout-line
echo destroy-stderr-line >&2
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	state := v1alpha1.State{Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}}}
	runsDir := filepath.Join(dir, "runs")
	opts := RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		OwnershipDir:       filepath.Join(dir, "ownership"),
		Executable:         executable,
		BundleDir:          filepath.Join(dir, "bundle"),
	}
	// Empty limit so the fake ansible actually runs (a real limit against an
	// empty inventory would correctly skip as no-hosts).
	tasks, err := PlanDestroyTasks("infra", state, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareDestroyTaskGraph(runsDir, opts, tasks, ConcurrencyLimits{Parallelism: 1})
	if err != nil {
		t.Fatalf("PrepareDestroyTaskGraph: %v", err)
	}
	if !strings.HasPrefix(prepared.RunID, "destroy-") {
		t.Fatalf("destroy run ID = %q, want destroy- prefix", prepared.RunID)
	}

	var stdout, stderr bytes.Buffer
	ledger, err := RunPreparedDestroyTaskGraph(context.Background(), &stdout, &stderr, runsDir, opts, ApplyTarget{Name: "infra destroy"}, "", prepared, nil, nil)
	if err != nil {
		t.Fatalf("RunPreparedDestroyTaskGraph: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if ledger.Status != RunStatusOK {
		t.Fatalf("ledger status = %s, want ok: %+v", ledger.Status, ledger)
	}
	if strings.Contains(stdout.String(), "destroy-stdout-line") || strings.Contains(stderr.String(), "destroy-stderr-line") {
		t.Fatalf("destroy streamed ansible to the terminal\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	for _, task := range ledger.Tasks {
		if task.Status != TaskStatusOK {
			t.Fatalf("task %s status = %s, want ok", task.ID, task.Status)
		}
		logData, err := os.ReadFile(TaskLogPath(runsDir, ledger.RunID, task.ID))
		if err != nil {
			t.Fatalf("read task log for %s: %v", task.ID, err)
		}
		if !strings.Contains(string(logData), "destroy-stdout-line") {
			t.Fatalf("task %s log missing ansible output:\n%s", task.ID, logData)
		}
	}
	// Destroy tasks carry no Entry.Cluster (they use ResourceKeys), so the shared
	// scheduler must not emit the per-cluster apply lifecycle markers for them.
	runLog, err := os.ReadFile(ApplyRunLogPath(runsDir, ledger.RunID))
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	if strings.Contains(string(runLog), "apply initiated") || strings.Contains(string(runLog), "apply finished") {
		t.Fatalf("destroy run log emitted per-cluster apply markers:\n%s", runLog)
	}
}
