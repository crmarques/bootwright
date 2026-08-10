package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
)

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
	tasks, err := PlanDestroyTasks("infra", state, "", []string{DestroySkipOrphanSweepExtraVar + "=true"}, nil)
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
		if task.Status == TaskStatusSkipped {
			continue
		}
		if task.Status != TaskStatusOK {
			t.Fatalf("task %s status = %s, want ok", task.ID, task.Status)
		}
		logData, err := os.ReadFile(TaskLogPath(runsDir, ledger.RunID, task))
		if err != nil {
			t.Fatalf("read task log for %s: %v", task.ID, err)
		}
		if !strings.Contains(string(logData), "destroy-stdout-line") {
			t.Fatalf("task %s log missing ansible output:\n%s", task.ID, logData)
		}
	}
	runLog, err := os.ReadFile(ApplyRunLogPath(runsDir, ledger.RunID))
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	if strings.Contains(string(runLog), "apply initiated") || strings.Contains(string(runLog), "apply finished") {
		t.Fatalf("destroy run log emitted per-cluster apply markers:\n%s", runLog)
	}
}

func TestStorageDestroyTaskDoesNotFinalizeBeforeExactAttestationValidation(t *testing.T) {
	dir := t.TempDir()
	state := destroyStorageFanOutState(map[string][]string{"ceph-a": {"a1"}})
	task := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:           DestroyStorageClustersTaskID,
			Kind:         DestroyTaskKindStorageCluster,
			ResourceKeys: []string{"ceph-a"},
		},
		State: state,
	}
	opts := RunOptions{
		ArtifactsRoot: filepath.Join(dir, "artifacts"),
		OwnershipDir:  filepath.Join(dir, "ownership"),
	}
	if err := ownership.SaveResource(opts.OwnershipDir, ownership.ResourceRecord{
		Kind: string(ownership.KindStorageCluster), Name: "ceph-a", Cluster: "ceph-a", Host: "storage__ceph-a__a1",
		Attributes: map[string]string{"seedHost": "storage__ceph-a__a1"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := validateStorageDestroyTask(opts, task); err == nil || !strings.Contains(err.Error(), "no completion attestation") {
		t.Fatalf("missing report error = %v", err)
	}
	if records, err := ownership.LoadContext(opts.OwnershipDir, ""); err != nil || len(records) != 1 {
		t.Fatalf("a missing proof must retain ownership, records=%v err=%v", records, err)
	}
	if err := os.MkdirAll(opts.ArtifactsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	report := storageDestroyResult("ceph-a", []string{"a1"}, nil)
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.ArtifactsRoot, StorageDestroyResultFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	finalize, err := validateStorageDestroyTask(opts, task)
	if err != nil {
		t.Fatalf("valid report: %v", err)
	}
	if records, err := ownership.LoadContext(opts.OwnershipDir, ""); err != nil || len(records) != 1 {
		t.Fatalf("validation must retain ownership until the scheduler persists task success, records=%v err=%v", records, err)
	}
	if err := finalize(); err != nil {
		t.Fatalf("finalize valid report: %v", err)
	}
	if records, err := ownership.LoadContext(opts.OwnershipDir, ""); err != nil || len(records) != 0 {
		t.Fatalf("a validated complete proof must release ownership, records=%v err=%v", records, err)
	}
}

func TestDestroyTaskFinalizerRunsAfterOKLedgerIsDurable(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	opts := RunOptions{
		State:              v1alpha1.State{},
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		OwnershipDir:       filepath.Join(dir, "ownership"),
	}
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "destroy.storage", Kind: DestroyTaskKindStorageCluster, Label: "storage"}}
	prepared := PreparedApplyTaskGraph{RunID: "destroy-finalizer", StartedAt: time.Now().UTC(), Tasks: []ApplyTask{task}, Limits: ConcurrencyLimits{Parallelism: 1}}
	finalized := false
	executor := func(_ context.Context, _, _ io.Writer, _, _ string, _ RunOptions, task ApplyTask, _ ApplyTaskRunnerFactory) applyTaskResult {
		return applyTaskResult{id: task.Entry.ID, finalize: func() error {
			ledger, found, err := LoadRunLedger(runsDir)
			if err != nil || !found {
				return fmt.Errorf("load durable ledger: found=%t err=%v", found, err)
			}
			if len(ledger.Tasks) != 1 || ledger.Tasks[0].Status != TaskStatusOK {
				return fmt.Errorf("durable task status = %+v, want ok", ledger.Tasks)
			}
			finalized = true
			return nil
		}}
	}
	ledger, err := runPreparedTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, opts, ApplyTarget{Name: "destroy"}, "", prepared, nil, nil, executor)
	if err != nil {
		t.Fatalf("run graph: %v", err)
	}
	if !finalized || ledger.Status != RunStatusOK || ledger.Tasks[0].Status != TaskStatusOK {
		t.Fatalf("finalized=%t ledger=%+v", finalized, ledger)
	}
}

func TestDestroyTaskFinalizerFailureBlocksOnlySuccessDependents(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	opts := RunOptions{
		State:              v1alpha1.State{},
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		OwnershipDir:       filepath.Join(dir, "ownership"),
	}
	prepared := PreparedApplyTaskGraph{
		RunID:     "destroy-finalizer-failure",
		StartedAt: time.Now().UTC(),
		Limits:    ConcurrencyLimits{Parallelism: 1},
		Tasks: []ApplyTask{
			{Entry: TaskLedgerEntry{ID: "storage", Kind: DestroyTaskKindStorageCluster, Label: "storage"}},
			{Entry: TaskLedgerEntry{ID: "gated", Label: "gated", SuccessDependencies: []string{"storage"}}},
			{Entry: TaskLedgerEntry{ID: "independent", Label: "independent", OrderingDependencies: []string{"storage"}}},
		},
	}
	runs := map[string]int{}
	executor := func(_ context.Context, _, _ io.Writer, _, _ string, _ RunOptions, task ApplyTask, _ ApplyTaskRunnerFactory) applyTaskResult {
		runs[task.Entry.ID]++
		if task.Entry.ID == "storage" {
			return applyTaskResult{id: task.Entry.ID, finalize: func() error { return errors.New("owner release failed") }}
		}
		return applyTaskResult{id: task.Entry.ID}
	}
	ledger, err := runPreparedTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, opts, ApplyTarget{Name: "destroy"}, "", prepared, nil, nil, executor)
	if err == nil || !strings.Contains(err.Error(), "completion finalizer failed") {
		t.Fatalf("error = %v", err)
	}
	statuses := map[string]TaskStatus{}
	for _, task := range ledger.Tasks {
		statuses[task.ID] = task.Status
	}
	if ledger.Status != RunStatusFailed || statuses["storage"] != TaskStatusFailed || statuses["gated"] != TaskStatusBlocked || statuses["independent"] != TaskStatusOK {
		t.Fatalf("ledger = %+v", ledger)
	}
	if runs["storage"] != 1 || runs["gated"] != 0 || runs["independent"] != 1 {
		t.Fatalf("executor runs = %v", runs)
	}
}
