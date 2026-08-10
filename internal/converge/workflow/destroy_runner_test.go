package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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

func TestStorageDestroyTaskStagesProofBeforeReleasingOwnership(t *testing.T) {
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
		Attributes: map[string]string{"seedHost": "storage__ceph-a__a1", "fsid": "11111111-1111-1111-1111-111111111111"},
	}); err != nil {
		t.Fatal(err)
	}
	expected := StorageDestroyExpectedNodes(state, task.Entry.ResourceKeys)
	seeds := StorageDestroyExpectedSeedHosts(state, task.Entry.ResourceKeys)
	if _, err := validateStorageDestroyTaskReport(opts, expected); err == nil || !strings.Contains(err.Error(), "no completion attestation") {
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
	results, err := validateStorageDestroyTaskReport(opts, expected)
	if err != nil {
		t.Fatalf("valid report: %v", err)
	}
	manifest, err := PrepareStorageDestroyOwnershipRelease(opts.OwnershipDir, "", results, seeds)
	if err != nil {
		t.Fatalf("stage valid report: %v", err)
	}
	if len(manifest.Clusters) != 1 {
		t.Fatalf("release manifest = %+v", manifest)
	}
	records, err := ownership.LoadContext(opts.OwnershipDir, "")
	if err != nil || len(records) != 1 || records[0].Attributes[storageDestroyStatusAttr] != storageDestroyStatusProofValidated {
		t.Fatalf("validated proof must retain a release-pending owner, records=%v err=%v", records, err)
	}
	if err := ReconcileStorageDestroyOwnership(opts.OwnershipDir, "", results, seeds); err == nil || !strings.Contains(err.Error(), "not fully released") {
		t.Fatalf("proof-only reconciliation error = %v", err)
	}
	if err := MarkStorageDestroyOwnershipReleased(opts.OwnershipDir, "", results, seeds, manifest); err != nil {
		t.Fatalf("mark evidence released: %v", err)
	}
	if err := ReconcileStorageDestroyOwnership(opts.OwnershipDir, "", results, seeds); err != nil {
		t.Fatalf("reconcile released owner: %v", err)
	}
	if records, err := ownership.LoadContext(opts.OwnershipDir, ""); err != nil || len(records) != 0 {
		t.Fatalf("a released complete proof must remove ownership, records=%v err=%v", records, err)
	}
}
