package workflow

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/remedy"
)

func TestFailedApplyTaskRetainsTypedRemedy(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	opts := schedulerRunOptions(dir)
	request := remedy.Request{
		Action:  remedy.ActionReconcileSharedServiceThenRetrySameSelection,
		Targets: []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "cluster-a"}},
	}
	task := ApplyTask{
		Entry:         TaskLedgerEntry{ID: "controller-name-resolution.demo", Kind: ApplyTaskKindControllerNameResolution, Label: "controller name resolution"},
		Playbook:      "controller-proof",
		State:         opts.State,
		FailureRemedy: request,
	}
	runner := &recordingApplyRunner{failures: map[string]error{task.Playbook: errors.New("resolver proof failed")}}
	result := runOneApplyTask(context.Background(), io.Discard, io.Discard, runsDir, "apply-test", opts, task, func(io.Writer, io.Writer) ansible.Runner {
		return runner
	})
	if result.err == nil {
		t.Fatal("failed apply task returned no error")
	}
	var remedial remedy.Error
	if !errors.As(result.err, &remedial) {
		t.Fatalf("failed task lost its typed remedy: %v", result.err)
	}
	got := remedial.Remedy()
	if got.Action != request.Action || len(got.Targets) != 1 || got.Targets[0] != request.Targets[0] {
		t.Fatalf("failed task remedy = %#v, want %#v", got, request)
	}
	got.Targets[0].Name = "changed"
	if remedial.Remedy().Targets[0].Name != "cluster-a" {
		t.Fatal("failed task remedy exposed mutable target storage")
	}
}

func TestFailedGenericApplyPreservesPriorConvergenceRecord(t *testing.T) {
	cases := []struct {
		name    string
		seed    func(*testing.T, string, ApplyTask)
		failure error
		want    ConvergeSafetyClassification
	}{
		{
			name:    "first failure stays missing",
			failure: errors.New("apply failed"),
			want:    ConvergeSafetyMissing,
		},
		{
			name: "prior match survives failure",
			seed: func(t *testing.T, runsDir string, task ApplyTask) {
				hash, err := ApplyTaskDesiredHash(task)
				if err != nil {
					t.Fatalf("desired hash: %v", err)
				}
				saveStateCheckRecord(t, runsDir, task, hash, ConvergeSafetyOwner)
			},
			failure: errors.New("apply failed"),
			want:    ConvergeSafetyMatch,
		},
		{
			name: "prior drift survives interruption",
			seed: func(t *testing.T, runsDir string, task ApplyTask) {
				saveStateCheckRecord(t, runsDir, task, "sha256:prior-desired", ConvergeSafetyOwner)
			},
			failure: context.Canceled,
			want:    ConvergeSafetyDrift,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			runsDir := filepath.Join(dir, "runs")
			opts := schedulerRunOptions(dir)
			task := ApplyTask{
				Entry:    TaskLedgerEntry{ID: "provider.bastion", Kind: ApplyTaskKindProvider, Label: "provider bastion"},
				Playbook: "provider-failure",
				State:    opts.State,
			}
			if tc.seed != nil {
				tc.seed(t, runsDir, task)
			}
			runner := &recordingApplyRunner{failures: map[string]error{task.Playbook: tc.failure}}
			result := runOneApplyTask(context.Background(), io.Discard, io.Discard, runsDir, "apply-test", opts, task, func(io.Writer, io.Writer) ansible.Runner {
				return runner
			})
			if result.err == nil {
				t.Fatal("failed apply task returned no error")
			}
			objects, err := ClassifyApplyObjects([]ApplyTask{task}, runsDir)
			if err != nil {
				t.Fatalf("classify after failure: %v", err)
			}
			if got := objects[0].Class; got != tc.want {
				t.Fatalf("classification after failure = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFailedStorageApplyPreservesPriorSubObjectRecords(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	opts := schedulerRunOptions(dir)
	state := opts.State
	state.StorageClusters = []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}}
	state.StoragePools = []v1alpha1.StoragePool{storageSubObjectTestPool("data", 3)}
	task := ApplyTask{
		Entry:              TaskLedgerEntry{ID: "storage.demo", Kind: ApplyTaskKindStorageCluster, Label: "storage demo", Cluster: "demo"},
		Playbook:           "storage-failure",
		State:              state,
		DesiredHashVars:    storageClusterDesiredHashVars(state, "demo"),
		StructuralHashVars: storageClusterStructuralHashVars(state, "demo"),
	}
	if err := MarkStorageSubObjectsConvergeSafety(runsDir, "test", "prior-run", state, "demo", ConvergeSafetyStatusReconciled, time.Unix(1700000000, 0)); err != nil {
		t.Fatalf("mark storage subobjects: %v", err)
	}
	resourceID := (storageSubObject{Kind: storageSubObjectKindPool, Cluster: "demo", Name: "data"}).resourceID()
	path := ConvergeSafetyRecordPath(runsDir, resourceID)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prior storage subobject record: %v", err)
	}
	runner := &recordingApplyRunner{failures: map[string]error{task.Playbook: errors.New("storage failed")}}
	result := runOneApplyTask(context.Background(), io.Discard, io.Discard, runsDir, "apply-test", opts, task, func(io.Writer, io.Writer) ansible.Runner {
		return runner
	})
	if result.err == nil {
		t.Fatal("failed storage apply returned no error")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read storage subobject record after failure: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("failed storage apply changed its prior successful subobject record\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestFailedApplyGraphPreservesPriorSuccessfulTaskEvidence(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	opts := schedulerRunOptions(dir)
	opts.ContextName = "test"
	first := ApplyTask{
		Entry:             TaskLedgerEntry{ID: "provider.first", Kind: ApplyTaskKindProvider, Label: "provider first"},
		Playbook:          "provider-first",
		SkipWhenConverged: true,
		State:             opts.State,
	}
	if err := MarkApplyTaskConvergeSafety(runsDir, opts.ContextName, "prior-run", first, ConvergeSafetyStatusReconciled, time.Unix(1700000000, 0)); err != nil {
		t.Fatalf("mark prior task evidence: %v", err)
	}
	archiveSuccessfulTaskEvidence(t, runsDir, "prior-run", first.Entry, TaskStatusOK)
	resourceID := applyTaskSafetyResourceID(first)
	path := ConvergeSafetyRecordPath(runsDir, resourceID)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prior task evidence: %v", err)
	}
	second := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:           "provider.second",
			Kind:         ApplyTaskKindProvider,
			Label:        "provider second",
			Dependencies: []string{first.Entry.ID},
		},
		Playbook: "provider-second",
		State:    opts.State,
	}
	runner := &recordingApplyRunner{failures: map[string]error{second.Playbook: errors.New("second task failed")}}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, opts,
		ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseFabric}}, "", []ApplyTask{first, second},
		ConcurrencyLimits{Parallelism: 1}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if err == nil {
		t.Fatal("apply graph with a failed second task returned no error")
	}
	assertTaskStatus(t, ledger, first.Entry.ID, TaskStatusSkipped)
	assertTaskStatus(t, ledger, second.Entry.ID, TaskStatusFailed)
	if ledger.Status != RunStatusFailed {
		t.Fatalf("run status = %q, want %q", ledger.Status, RunStatusFailed)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read task evidence after graph failure: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("a later graph failure replaced prior successful task evidence\nbefore:\n%s\nafter:\n%s", before, after)
	}
	assertCurrentTaskEvidenceIsProvable(t, runsDir, "prior-run", first, TaskStatusOK)
}

func TestFailedApplyGraphPreservesPriorSuccessfulStorageSubObjectEvidence(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	opts := schedulerRunOptions(dir)
	opts.ContextName = "test"
	state := opts.State
	state.StorageClusters = []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}}
	state.StoragePools = []v1alpha1.StoragePool{storageSubObjectTestPool("data", 3)}
	opts.State = state
	first := ApplyTask{
		Entry:              TaskLedgerEntry{ID: "storage.demo", Kind: ApplyTaskKindStorageCluster, Label: "storage demo", Cluster: "demo"},
		Playbook:           "storage-first",
		State:              state,
		DesiredHashVars:    storageClusterDesiredHashVars(state, "demo"),
		StructuralHashVars: storageClusterStructuralHashVars(state, "demo"),
	}
	if err := MarkStorageSubObjectsConvergeSafety(runsDir, opts.ContextName, "prior-run", state, "demo", ConvergeSafetyStatusReconciled, time.Unix(1700000000, 0)); err != nil {
		t.Fatalf("mark prior storage subobject evidence: %v", err)
	}
	archiveSuccessfulTaskEvidence(t, runsDir, "prior-run", first.Entry, TaskStatusOK)
	sub := storageSubObject{Kind: storageSubObjectKindPool, Cluster: "demo", Name: "data"}
	path := ConvergeSafetyRecordPath(runsDir, sub.resourceID())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prior storage subobject evidence: %v", err)
	}
	second := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:           "provider.after-storage",
			Kind:         ApplyTaskKindProvider,
			Label:        "provider after storage",
			Dependencies: []string{first.Entry.ID},
		},
		Playbook: "provider-after-storage",
		State:    state,
	}
	runner := &recordingApplyRunner{failures: map[string]error{second.Playbook: errors.New("post-storage task failed")}}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, opts,
		ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseBase}}, "", []ApplyTask{first, second},
		ConcurrencyLimits{Parallelism: 1}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if err == nil {
		t.Fatal("apply graph with a failed task after storage returned no error")
	}
	assertTaskStatus(t, ledger, first.Entry.ID, TaskStatusOK)
	assertTaskStatus(t, ledger, second.Entry.ID, TaskStatusFailed)
	if ledger.Status != RunStatusFailed {
		t.Fatalf("run status = %q, want %q", ledger.Status, RunStatusFailed)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read storage subobject evidence after graph failure: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("a later graph failure replaced prior successful storage subobject evidence\nbefore:\n%s\nafter:\n%s", before, after)
	}
	input, err := storageSubObjectDesiredHashInput(state, sub)
	if err != nil {
		t.Fatalf("storage subobject hash input: %v", err)
	}
	matched, err := successfulInputSnapshotMatchesRecordedSchema(runsDir, "prior-run", sub.resourceID(), first.Entry.ID, first.Entry.Kind, TaskStatusOK, ConvergeHashSchema, input)
	if err != nil {
		t.Fatalf("prove retained storage subobject evidence: %v", err)
	}
	if !matched {
		t.Fatal("retained storage subobject evidence did not match its immutable successful input")
	}
}

func TestApplyTaskConvergeSafetyWritesFreshEvidenceWithoutReusableProof(t *testing.T) {
	cases := []struct {
		name       string
		priorRun   RunStatus
		changeTask func(ApplyTask) ApplyTask
	}{
		{
			name:     "changed desired input",
			priorRun: RunStatusOK,
			changeTask: func(task ApplyTask) ApplyTask {
				task.DesiredHashVars = map[string]string{"revision": "changed"}
				return task
			},
		},
		{
			name:       "failed prior run",
			priorRun:   RunStatusFailed,
			changeTask: func(task ApplyTask) ApplyTask { return task },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runsDir := t.TempDir()
			task := ApplyTask{
				Entry:           TaskLedgerEntry{ID: "provider.demo", Kind: ApplyTaskKindProvider},
				Playbook:        "provider-demo",
				DesiredHashVars: map[string]string{"revision": "prior"},
			}
			if err := MarkApplyTaskConvergeSafety(runsDir, "test", "prior-run", task, ConvergeSafetyStatusReconciled, time.Unix(1700000000, 0)); err != nil {
				t.Fatalf("mark prior evidence: %v", err)
			}
			if err := ArchiveRunLedger(runsDir, RunLedger{
				RunID:  "prior-run",
				Status: tc.priorRun,
				Tasks:  []TaskLedgerEntry{{ID: task.Entry.ID, Kind: task.Entry.Kind, Status: TaskStatusOK}},
			}); err != nil {
				t.Fatalf("archive prior run: %v", err)
			}
			current := tc.changeTask(task)
			if err := MarkApplyTaskConvergeSafety(runsDir, "test", "current-run", current, ConvergeSafetyStatusReconciled, time.Unix(1700000100, 0)); err != nil {
				t.Fatalf("mark current evidence: %v", err)
			}
			record, found, err := LoadConvergeSafetyRecord(runsDir, applyTaskSafetyResourceID(current))
			if err != nil {
				t.Fatalf("load current evidence: %v", err)
			}
			if !found {
				t.Fatal("current evidence was not written")
			}
			if record.RunID != "current-run" {
				t.Fatalf("evidence run = %q, want current-run", record.RunID)
			}
			if _, err := os.Stat(successfulInputSnapshotPath(runsDir, "current-run", record.ResourceID)); err != nil {
				t.Fatalf("stat fresh successful input snapshot: %v", err)
			}
		})
	}
}

func archiveSuccessfulTaskEvidence(t *testing.T, runsDir, runID string, entry TaskLedgerEntry, status TaskStatus) {
	t.Helper()
	if err := ArchiveRunLedger(runsDir, RunLedger{
		RunID:  runID,
		Status: RunStatusOK,
		Tasks:  []TaskLedgerEntry{{ID: entry.ID, Kind: entry.Kind, Status: status}},
	}); err != nil {
		t.Fatalf("archive successful task evidence: %v", err)
	}
}

func assertTaskStatus(t *testing.T, ledger RunLedger, taskID string, want TaskStatus) {
	t.Helper()
	task, found := ledger.Task(taskID)
	if !found {
		t.Fatalf("task %s is missing from run ledger", taskID)
	}
	if task.Status != want {
		t.Fatalf("task %s status = %q, want %q", taskID, task.Status, want)
	}
}

func assertCurrentTaskEvidenceIsProvable(t *testing.T, runsDir, runID string, task ApplyTask, status TaskStatus) {
	t.Helper()
	input, err := applyTaskDesiredHashInput(task)
	if err != nil {
		t.Fatalf("task hash input: %v", err)
	}
	matched, err := successfulInputSnapshotMatchesRecordedSchema(runsDir, runID, applyTaskSafetyResourceID(task), task.Entry.ID, task.Entry.Kind, status, ConvergeHashSchema, input)
	if err != nil {
		t.Fatalf("prove retained task evidence: %v", err)
	}
	if !matched {
		t.Fatal("retained task evidence did not match its immutable successful input")
	}
}
