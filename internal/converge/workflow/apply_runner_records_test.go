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
)

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
