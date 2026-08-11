package workflow

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/remedy"
)

const identityHashA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const identityHashB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type convergeSafetyRecordMutation struct {
	name       string
	field      string
	mutate     func(*ConvergeSafetyRecord)
	authority  bool
	foreign    bool
	nonCurrent bool
}

func currentConvergeSafetyRecordMutations() []convergeSafetyRecordMutation {
	return []convergeSafetyRecordMutation{
		{name: "api version", field: "apiVersion", authority: true, mutate: func(record *ConvergeSafetyRecord) { record.APIVersion = "bootwright.io/converge-safety/v2" }},
		{name: "resource id", field: "resourceID", authority: true, mutate: func(record *ConvergeSafetyRecord) { record.ResourceID = "other/resource" }},
		{name: "resource kind", field: "resourceKind", authority: true, mutate: func(record *ConvergeSafetyRecord) { record.ResourceKind = "OtherKind" }},
		{name: "task id", field: "taskID", authority: true, mutate: func(record *ConvergeSafetyRecord) { record.TaskID = "other.task" }},
		{name: "task kind", field: "taskKind", authority: true, mutate: func(record *ConvergeSafetyRecord) { record.TaskKind = "otherTaskKind" }},
		{name: "owner manager", field: "owner.manager", foreign: true, mutate: func(record *ConvergeSafetyRecord) { record.Owner.Manager = "another-manager" }},
		{name: "owner context", field: "owner.context", authority: true, mutate: func(record *ConvergeSafetyRecord) { record.Owner.Context = "another-context" }},
		{name: "status", field: "status", mutate: func(record *ConvergeSafetyRecord) { record.Status = "completed" }},
		{name: "desired hash", field: "desiredHash", mutate: func(record *ConvergeSafetyRecord) { record.DesiredHash = "sha256:short" }},
		{name: "structural hash", field: "structuralHash", mutate: func(record *ConvergeSafetyRecord) { record.StructuralHash = "sha256:short" }},
		{name: "tiebreaker invariant hash", field: "tiebreakerInvariantHash", mutate: func(record *ConvergeSafetyRecord) { record.TiebreakerInvariantHash = "sha256:short" }},
		{name: "hash schema", field: "hashSchema", nonCurrent: true, mutate: func(record *ConvergeSafetyRecord) { record.HashSchema = ConvergeHashSchema + 1 }},
	}
}

func assertUntrustedConvergenceEvidenceError(t *testing.T, class ConvergeSafetyClassification, err error, resourceID, field string) {
	t.Helper()
	if class != ConvergeSafetyUnknown {
		t.Fatalf("classification = %q, want unknown", class)
	}
	var untrusted *UntrustedConvergenceEvidenceError
	if !errors.As(err, &untrusted) {
		t.Fatalf("error %T is not untrusted convergence evidence: %v", err, err)
	}
	if untrusted.ResourceID != resourceID || untrusted.RecordPath == "" || !strings.Contains(err.Error(), field) {
		t.Fatalf("error does not identify resource %q, path, and field %q: %v", resourceID, field, err)
	}
	var remedial remedy.Error
	if !errors.As(err, &remedial) || remedial.Remedy().Action != remedy.ActionRetrySameInvocation {
		t.Fatalf("untrusted evidence does not retain post-repair exact retry: %v", err)
	}
	if strings.Contains(err.Error(), "--mode rebuild") || strings.Contains(err.Error(), "data-loss") {
		t.Fatalf("untrusted evidence suggests a destructive bypass: %v", err)
	}
}

func assertCurrentConvergenceEvidenceError(t *testing.T, class ConvergeSafetyClassification, err error, resourceID, field string) {
	t.Helper()
	if class != ConvergeSafetyUnknown {
		t.Fatalf("classification = %q, want unknown", class)
	}
	var current *CurrentConvergenceEvidenceError
	if !errors.As(err, &current) {
		t.Fatalf("error %T is not current convergence evidence: %v", err, err)
	}
	if current.ResourceID != resourceID || !strings.Contains(err.Error(), field) {
		t.Fatalf("error does not identify resource %q and field %q: %v", resourceID, field, err)
	}
	var remedial remedy.Error
	if !errors.As(err, &remedial) || remedial.Remedy().Action != remedy.ActionRebuildSameSelection {
		t.Fatalf("invalid current evidence does not retain the same-selection rebuild remedy: %v", err)
	}
}

func identityValidationTask(t *testing.T) (ApplyTask, ConvergeSafetyRecord, string) {
	t.Helper()
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "provider.demo", Kind: ApplyTaskKindProvider, Label: "provider demo"}}
	desiredHash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	record := ConvergeSafetyRecord{
		APIVersion:   ConvergeSafetyAPIVersion,
		ResourceID:   applyTaskSafetyResourceID(task),
		ResourceKind: task.Entry.Kind,
		TaskID:       task.Entry.ID,
		TaskKind:     task.Entry.Kind,
		DesiredHash:  desiredHash,
		HashSchema:   ConvergeHashSchema,
		Owner:        ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner, Context: "ctx"},
		Status:       ConvergeSafetyStatusReconciled,
	}
	return task, record, desiredHash
}

func TestCurrentTaskConvergeSafetyIdentityRejectsEveryFieldMutation(t *testing.T) {
	task, base, desiredHash := identityValidationTask(t)
	for _, mutation := range currentConvergeSafetyRecordMutations() {
		t.Run(mutation.name, func(t *testing.T) {
			record := base
			mutation.mutate(&record)
			class, err := classifyApplyTaskWithRecordForContext(task, t.TempDir(), "ctx", record, desiredHash)
			switch {
			case mutation.foreign:
				if err != nil || class != ConvergeSafetyForeign {
					t.Fatalf("foreign owner classification = %q, err=%v", class, err)
				}
			case mutation.authority:
				assertUntrustedConvergenceEvidenceError(t, class, err, base.ResourceID, mutation.field)
			case mutation.nonCurrent:
				if err != nil || class == ConvergeSafetyMatch {
					t.Fatalf("unsupported schema classification = %q, err=%v", class, err)
				}
			default:
				assertCurrentConvergenceEvidenceError(t, class, err, base.ResourceID, mutation.field)
			}
		})
	}
}

func TestCurrentTaskConvergeSafetyAllowsOnlySemanticDriftAndKnownStatuses(t *testing.T) {
	task, base, desiredHash := identityValidationTask(t)
	drift := base
	drift.DesiredHash = identityHashA
	class, err := classifyApplyTaskWithRecordForContext(task, t.TempDir(), "ctx", drift, desiredHash)
	if err != nil || class != ConvergeSafetyDrift {
		t.Fatalf("valid desired-hash inequality = %q, err=%v, want drift", class, err)
	}
	for _, status := range []ConvergeSafetyStatus{ConvergeSafetyStatusCreated, ConvergeSafetyStatusReconciled, ConvergeSafetyStatusSkipped} {
		record := base
		record.Status = status
		record.StructuralHash = identityHashA
		record.TiebreakerInvariantHash = identityHashB
		class, err := classifyApplyTaskWithRecordForContext(task, t.TempDir(), "ctx", record, desiredHash)
		if err != nil || class != ConvergeSafetyMatch {
			t.Fatalf("status %q with syntactically valid optional hashes = %q, err=%v, want match", status, class, err)
		}
	}
}

func identityValidationStorageSubObject(t *testing.T) (v1alpha1.State, storageSubObject, ConvergeSafetyRecord, string) {
	t.Helper()
	sub := storageSubObject{Kind: storageSubObjectKindPool, Cluster: "demo", Name: "rbd"}
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
		StoragePools: []v1alpha1.StoragePool{{
			Metadata: v1alpha1.Metadata{Name: "rbd"},
			Spec: v1alpha1.StoragePoolSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				Ceph:              v1alpha1.StoragePoolCephSpec{Type: v1alpha1.StoragePoolTypeReplicated, Replicated: v1alpha1.StorageCephPoolReplicas{Size: 3, MinSize: 2}},
			},
		}},
	}
	desiredHash, err := storageSubObjectDesiredHash(state, sub)
	if err != nil {
		t.Fatalf("desired hash: %v", err)
	}
	record := ConvergeSafetyRecord{
		APIVersion:   ConvergeSafetyAPIVersion,
		ResourceID:   sub.resourceID(),
		ResourceKind: sub.Kind,
		TaskID:       "storage.demo",
		TaskKind:     ApplyTaskKindStorageCluster,
		DesiredHash:  desiredHash,
		HashSchema:   ConvergeHashSchema,
		Owner:        ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner, Context: "ctx"},
		Status:       ConvergeSafetyStatusReconciled,
	}
	return state, sub, record, desiredHash
}

func TestCurrentStorageSubObjectConvergeSafetyIdentityRejectsEveryFieldMutation(t *testing.T) {
	state, sub, base, desiredHash := identityValidationStorageSubObject(t)
	for _, mutation := range currentConvergeSafetyRecordMutations() {
		t.Run(mutation.name, func(t *testing.T) {
			record := base
			mutation.mutate(&record)
			class, err := classifyStorageSubObjectWithRecordForContext(state, sub, t.TempDir(), "ctx", record, desiredHash)
			switch {
			case mutation.foreign:
				if err != nil || class != ConvergeSafetyForeign {
					t.Fatalf("foreign owner classification = %q, err=%v", class, err)
				}
			case mutation.authority:
				assertUntrustedConvergenceEvidenceError(t, class, err, base.ResourceID, mutation.field)
			case mutation.nonCurrent:
				if err != nil || class == ConvergeSafetyMatch {
					t.Fatalf("unsupported schema classification = %q, err=%v", class, err)
				}
			default:
				assertCurrentConvergenceEvidenceError(t, class, err, base.ResourceID, mutation.field)
			}
		})
	}
}

func TestCurrentStorageSubObjectConvergeSafetyAllowsOnlySemanticDriftAndKnownStatuses(t *testing.T) {
	state, sub, base, desiredHash := identityValidationStorageSubObject(t)
	drift := base
	drift.DesiredHash = identityHashA
	class, err := classifyStorageSubObjectWithRecordForContext(state, sub, t.TempDir(), "ctx", drift, desiredHash)
	if err != nil || class != ConvergeSafetyDrift {
		t.Fatalf("valid desired-hash inequality = %q, err=%v, want drift", class, err)
	}
	for _, status := range []ConvergeSafetyStatus{ConvergeSafetyStatusCreated, ConvergeSafetyStatusReconciled, ConvergeSafetyStatusSkipped} {
		record := base
		record.Status = status
		record.StructuralHash = identityHashA
		record.TiebreakerInvariantHash = identityHashB
		class, err := classifyStorageSubObjectWithRecordForContext(state, sub, t.TempDir(), "ctx", record, desiredHash)
		if err != nil || class != ConvergeSafetyMatch {
			t.Fatalf("status %q with syntactically valid optional hashes = %q, err=%v, want match", status, class, err)
		}
	}
}

func TestContextFreeClassifiersRefuseCurrentSchemaEvidence(t *testing.T) {
	task, taskRecord, taskHash := identityValidationTask(t)
	class, err := classifyApplyTaskWithRecord(task, t.TempDir(), taskRecord, taskHash)
	assertUntrustedConvergenceEvidenceError(t, class, err, taskRecord.ResourceID, "context-free")

	state, sub, subRecord, subHash := identityValidationStorageSubObject(t)
	class, err = classifyStorageSubObjectWithRecord(state, sub, t.TempDir(), subRecord, subHash)
	assertUntrustedConvergenceEvidenceError(t, class, err, subRecord.ResourceID, "context-free")
}

func TestInvalidCurrentTaskEvidenceNeitherSkipsNorRuns(t *testing.T) {
	dir := t.TempDir()
	runsDir := dir + "/runs"
	opts := schedulerRunOptions(dir)
	opts.ContextName = "ctx"
	task := ApplyTask{
		Entry:             TaskLedgerEntry{ID: "provider.demo", Kind: ApplyTaskKindProvider, Label: "provider demo"},
		Playbook:          "provider-demo.yml",
		State:             opts.State,
		SkipWhenConverged: true,
	}
	desiredHash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatal(err)
	}
	record := ConvergeSafetyRecord{
		APIVersion:   ConvergeSafetyAPIVersion,
		ResourceID:   applyTaskSafetyResourceID(task),
		ResourceKind: task.Entry.Kind,
		TaskID:       task.Entry.ID,
		TaskKind:     task.Entry.Kind,
		DesiredHash:  desiredHash,
		HashSchema:   ConvergeHashSchema,
		Owner:        ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner, Context: "copied-context"},
		Status:       ConvergeSafetyStatusReconciled,
	}
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	objects, classifyErr := ClassifyApplyObjects([]ApplyTask{task}, runsDir, "ctx")
	if classifyErr == nil || objects != nil {
		t.Fatalf("invalid task evidence produced objects=%v err=%v", objects, classifyErr)
	}
	var classified *UntrustedConvergenceEvidenceError
	if !errors.As(classifyErr, &classified) {
		t.Fatalf("classification failure lost untrusted-evidence type: %v", classifyErr)
	}
	factoryCalls := 0
	result := runOneApplyTask(context.Background(), io.Discard, io.Discard, runsDir, "run", opts, task, func(io.Writer, io.Writer) ansible.Runner {
		factoryCalls++
		return &recordingApplyRunner{}
	})
	if result.err == nil || result.skipped || factoryCalls != 0 {
		t.Fatalf("invalid evidence result: err=%v skipped=%v runner factories=%d", result.err, result.skipped, factoryCalls)
	}
	var current *UntrustedConvergenceEvidenceError
	if !errors.As(result.err, &current) {
		t.Fatalf("run failure lost untrusted-evidence type: %v", result.err)
	}
	report, err := StateCheck([]ApplyTask{task}, ApplyTarget{}, v1alpha1.State{}, runsDir, "ctx")
	if err != nil {
		t.Fatalf("state check: %v", err)
	}
	if report.InSync || len(report.LoadWarnings) != 1 || !strings.Contains(report.LoadWarnings[0], "owner.context") {
		t.Fatalf("state check accepted or hid copied-context evidence: %+v", report)
	}
}

func TestInvalidCurrentStorageSubObjectEvidenceBlocksClassification(t *testing.T) {
	state, sub, record, _ := identityValidationStorageSubObject(t)
	record.Owner.Context = "copied-context"
	runsDir := t.TempDir()
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	task := ApplyTask{
		Entry: TaskLedgerEntry{ID: "storage.demo", Kind: ApplyTaskKindStorageCluster, Label: "storage demo", Cluster: "demo"},
		State: state,
	}
	objects, err := ClassifyApplyObjects([]ApplyTask{task}, runsDir, "ctx")
	if err == nil || objects != nil {
		t.Fatalf("invalid sub-object evidence produced objects=%v err=%v", objects, err)
	}
	var current *UntrustedConvergenceEvidenceError
	if !errors.As(err, &current) || current.ResourceID != sub.resourceID() {
		t.Fatalf("classification failure lost sub-object evidence identity: %v", err)
	}
}

func TestConvergeSafetyRecordPathMustBeRegularAndNotSymlinked(t *testing.T) {
	task, record, _ := identityValidationTask(t)
	for _, tc := range []struct {
		name string
		seed func(*testing.T, string, string)
	}{
		{
			name: "symlink",
			seed: func(t *testing.T, runsDir, path string) {
				sourceDir := t.TempDir()
				if err := SaveConvergeSafetyRecord(sourceDir, record); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(ConvergeSafetyRecordPath(sourceDir, record.ResourceID), path); err != nil {
					t.Skipf("create symlink: %v", err)
				}
			},
		},
		{
			name: "directory",
			seed: func(t *testing.T, _ string, path string) {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runsDir := t.TempDir()
			path := ConvergeSafetyRecordPath(runsDir, record.ResourceID)
			tc.seed(t, runsDir, path)
			if _, found, err := LoadConvergeSafetyRecord(runsDir, record.ResourceID); err == nil || !found || !strings.Contains(err.Error(), "regular non-symlink") {
				t.Fatalf("strict load found=%v err=%v", found, err)
			}
			if objects, err := ClassifyApplyObjectsForMode([]ApplyTask{task}, runsDir, "ctx", ApplyModeRebuild); err == nil || objects != nil {
				t.Fatalf("rebuild consumed path defect: objects=%v err=%v", objects, err)
			} else {
				var untrusted *UntrustedConvergenceEvidenceError
				if !errors.As(err, &untrusted) || untrusted.RecordPath != path {
					t.Fatalf("path defect = %T %+v, want untrusted record path %s", err, err, path)
				}
			}
			report, err := StateCheck([]ApplyTask{task}, ApplyTarget{}, v1alpha1.State{}, runsDir, "ctx")
			if err != nil || report.InSync || len(report.LoadWarnings) != 1 || !strings.Contains(report.LoadWarnings[0], "regular non-symlink") {
				t.Fatalf("lenient path report=%+v err=%v", report, err)
			}
		})
	}
}

func TestRebuildConsumesOnlyExactAuthorityCurrentTaskPayload(t *testing.T) {
	task, record, _ := identityValidationTask(t)
	runsDir := t.TempDir()
	record.DesiredHash = "sha256:short"
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	if objects, err := ClassifyApplyObjectsForMode([]ApplyTask{task}, runsDir, "ctx", ApplyModeReconcile); err == nil || objects != nil {
		t.Fatalf("reconcile accepted invalid payload: objects=%v err=%v", objects, err)
	}
	objects, err := ClassifyApplyObjectsForMode([]ApplyTask{task}, runsDir, "ctx", ApplyModeRebuild)
	if err != nil || len(objects) != 1 {
		t.Fatalf("rebuild classification = %v, %v", objects, err)
	}
	if got := objects[0]; !got.Recorded() || !got.HasStructuralDrift() || got.HasReconcilableDrift() || got.Class != ConvergeSafetyDrift {
		t.Fatalf("rebuild payload classification = %+v, want recorded structural drift", got)
	}
	if err := EvaluateApplyModePreflight(ApplyModeRebuild, objects); err != nil {
		t.Fatalf("explicit rebuild did not consume exact-authority payload failure: %v", err)
	}

	record.Owner.Context = "copied-context"
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	if objects, err := ClassifyApplyObjectsForMode([]ApplyTask{task}, runsDir, "ctx", ApplyModeRebuild); err == nil || objects != nil {
		t.Fatalf("rebuild adopted copied-context evidence: objects=%v err=%v", objects, err)
	} else {
		var untrusted *UntrustedConvergenceEvidenceError
		if !errors.As(err, &untrusted) {
			t.Fatalf("copied-context error = %T, want untrusted: %v", err, err)
		}
	}

	record.Owner.Context = "ctx"
	record.Owner.Manager = "another-manager"
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	objects, err = ClassifyApplyObjectsForMode([]ApplyTask{task}, runsDir, "ctx", ApplyModeRebuild)
	if err != nil || len(objects) != 1 || !objects[0].HasForeign() {
		t.Fatalf("foreign classification = %v, %v", objects, err)
	}
	if err := EvaluateApplyModePreflight(ApplyModeRebuild, objects); err == nil {
		t.Fatal("rebuild adopted foreign-manager evidence")
	}

	path := ConvergeSafetyRecordPath(runsDir, applyTaskSafetyResourceID(task))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if objects, err := ClassifyApplyObjectsForMode([]ApplyTask{task}, runsDir, "ctx", ApplyModeRebuild); err == nil || objects != nil {
		t.Fatalf("rebuild accepted unreadable evidence: objects=%v err=%v", objects, err)
	} else {
		var untrusted *UntrustedConvergenceEvidenceError
		if !errors.As(err, &untrusted) {
			t.Fatalf("unreadable error = %T, want untrusted: %v", err, err)
		}
	}
}

func TestRebuildConsumesOnlyExactAuthorityCurrentStoragePayload(t *testing.T) {
	state, sub, record, _ := identityValidationStorageSubObject(t)
	runsDir := t.TempDir()
	record.Status = "completed"
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "storage.demo", Kind: ApplyTaskKindStorageCluster, Label: "storage demo", Cluster: "demo"}, State: state}
	objects, err := ClassifyApplyObjectsForMode([]ApplyTask{task}, runsDir, "ctx", ApplyModeRebuild)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range objects {
		if object.ObjectKey != sub.resourceID() {
			continue
		}
		found = object.Recorded() && object.HasStructuralDrift() && !object.HasReconcilableDrift()
	}
	if !found {
		t.Fatalf("storage payload was not conservative structural drift: %+v", objects)
	}

	record.Owner.Context = "copied-context"
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	if objects, err := ClassifyApplyObjectsForMode([]ApplyTask{task}, runsDir, "ctx", ApplyModeRebuild); err == nil || objects != nil {
		t.Fatalf("storage rebuild adopted copied-context evidence: objects=%v err=%v", objects, err)
	}
}

func TestRebuildRunsExactAuthorityInvalidPayloadInsteadOfSkipping(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	opts := schedulerRunOptions(dir)
	opts.ContextName = "ctx"
	task := ApplyTask{
		Entry:             TaskLedgerEntry{ID: "provider.demo", Kind: ApplyTaskKindProvider, Label: "provider demo"},
		Playbook:          "provider-demo.yml",
		State:             opts.State,
		SkipWhenConverged: true,
	}
	_, record, _ := identityValidationTask(t)
	record.DesiredHash = "sha256:short"
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	factoryCalls := 0
	opts.ApplyMode = ApplyModeReconcile
	result := runOneApplyTask(context.Background(), io.Discard, io.Discard, runsDir, "reconcile-run", opts, task, func(io.Writer, io.Writer) ansible.Runner {
		factoryCalls++
		return &recordingApplyRunner{}
	})
	if result.err == nil || factoryCalls != 0 {
		t.Fatalf("reconcile invalid payload: err=%v factoryCalls=%d", result.err, factoryCalls)
	}

	opts.ApplyMode = ApplyModeRebuild
	result = runOneApplyTask(context.Background(), io.Discard, io.Discard, runsDir, "rebuild-run", opts, task, func(io.Writer, io.Writer) ansible.Runner {
		factoryCalls++
		return &recordingApplyRunner{}
	})
	if result.err != nil || result.skipped || factoryCalls != 1 {
		t.Fatalf("rebuild invalid payload: err=%v skipped=%v factoryCalls=%d", result.err, result.skipped, factoryCalls)
	}
	written, found, err := LoadConvergeSafetyRecord(runsDir, applyTaskSafetyResourceID(task))
	if err != nil || !found {
		t.Fatalf("load rebuilt evidence: found=%v err=%v", found, err)
	}
	if written.Owner.Context != "ctx" || written.DesiredHash == "sha256:short" || written.RunID != "rebuild-run" {
		t.Fatalf("rebuilt evidence = %+v", written)
	}
}

func TestForeignManagerCannotUseTiebreakerRebaselinePath(t *testing.T) {
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "demo"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Stretch: &v1alpha1.StorageCephStretch{
				Tiebreaker: v1alpha1.StorageCephTiebreaker{Node: "arbiter"},
			}},
		}},
	}}}
	task := ApplyTask{
		Entry:              TaskLedgerEntry{ID: "storage.demo", Kind: ApplyTaskKindStorageCluster, Cluster: "demo"},
		StructuralHashVars: state,
	}
	invariantHash, err := applyTaskTiebreakerInvariantHash(task)
	if err != nil {
		t.Fatal(err)
	}
	record := ConvergeSafetyRecord{
		HashSchema:              ConvergeHashSchema,
		StructuralHash:          identityHashA,
		TiebreakerInvariantHash: invariantHash,
		TiebreakerNodes:         map[string]string{"demo": "former-arbiter"},
		Owner:                   ConvergeSafetyOwnerIdentity{Manager: "another-manager", Context: "ctx"},
	}
	rebaselinable, err := convergeRecordRebaselinable(t.TempDir(), "ctx", record, nil, task)
	if err != nil || rebaselinable {
		t.Fatalf("foreign tiebreaker evidence rebaselinable=%v err=%v", rebaselinable, err)
	}
}

func TestInstalledClusterStampDoesNotAdoptCopiedContextEvidence(t *testing.T) {
	task := ApplyTask{Entry: TaskLedgerEntry{ID: "wait.demo", Kind: ApplyTaskKindInstallWait, Cluster: "demo"}}
	desiredHash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatal(err)
	}
	record := ConvergeSafetyRecord{
		APIVersion:   ConvergeSafetyAPIVersion,
		ResourceID:   applyTaskSafetyResourceID(task),
		ResourceKind: task.Entry.Kind,
		TaskID:       task.Entry.ID,
		TaskKind:     task.Entry.Kind,
		DesiredHash:  desiredHash,
		HashSchema:   ConvergeHashSchema,
		Owner:        ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner, Context: "copied-context"},
		Status:       ConvergeSafetyStatusReconciled,
	}
	runsDir := t.TempDir()
	if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
		t.Fatal(err)
	}
	err = stampInstalledClusterConvergeRecords(runsDir, "ctx", "new-run", []ApplyTask{task}, []string{"demo"}, record.UpdatedAt)
	var untrusted *UntrustedConvergenceEvidenceError
	if !errors.As(err, &untrusted) || !strings.Contains(err.Error(), "owner.context") {
		t.Fatalf("installed-cluster stamp error = %T %v, want copied-context refusal", err, err)
	}
	written, found, loadErr := LoadConvergeSafetyRecord(runsDir, record.ResourceID)
	if loadErr != nil || !found || written.Owner.Context != "copied-context" || written.RunID != "" {
		t.Fatalf("copied-context record was replaced: record=%+v found=%v err=%v", written, found, loadErr)
	}
}
