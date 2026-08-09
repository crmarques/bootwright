package workflow

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
)

func TestAddonStepResourcePoolSerializesOnlyMatchingStorageTargets(t *testing.T) {
	pool := newAddonStepResourcePool()
	releaseFirst, err := pool.acquire(context.Background(), []string{"storage:ceph-a"})
	if err != nil {
		t.Fatalf("acquire first resource: %v", err)
	}
	defer releaseFirst()

	sameEntered := make(chan struct{})
	sameDone := make(chan struct{})
	go func() {
		release, acquireErr := pool.acquire(context.Background(), []string{"storage:ceph-a"})
		if acquireErr == nil {
			close(sameEntered)
			release()
		}
		close(sameDone)
	}()

	otherEntered := make(chan struct{})
	go func() {
		release, acquireErr := pool.acquire(context.Background(), []string{"storage:ceph-b"})
		if acquireErr == nil {
			close(otherEntered)
			release()
		}
	}()

	awaitSignal(t, otherEntered, "an unrelated storage target")
	assertNoSignal(t, sameEntered, "a peer mutation of the held storage target")
	releaseFirst()
	awaitSignal(t, sameEntered, "the serialized peer after release")
	awaitSignal(t, sameDone, "the serialized peer completion")
}

func TestAddonStepResourcePoolCancellationFailsClosed(t *testing.T) {
	pool := newAddonStepResourcePool()
	release, err := pool.acquire(context.Background(), []string{"storage:ceph"})
	if err != nil {
		t.Fatalf("acquire held resource: %v", err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pool.acquire(ctx, []string{"storage:ceph"}); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("cancelled acquire error = %v, want fail-closed context cancellation", err)
	}
}

func TestAddonStepResourcePoolRefusesAnUnknownKey(t *testing.T) {
	pool := newAddonStepResourcePool()
	if _, err := pool.acquire(context.Background(), []string{"storage:ceph", " "}); err == nil || !strings.Contains(err.Error(), "resource key is empty") {
		t.Fatalf("unknown-key acquire error = %v, want an explicit fail-closed refusal", err)
	}
}

func TestRunPreparedTaskGraphSharesStepResourcePoolAcrossConcurrentTasks(t *testing.T) {
	dir := t.TempDir()
	firstHeld := make(chan struct{})
	releaseFirst := make(chan struct{})
	peerEntered := make(chan struct{})
	otherEntered := make(chan struct{})
	readEntered := make(chan struct{})
	executor := func(ctx context.Context, _, _ io.Writer, _, _ string, opts RunOptions, task ApplyTask, _ ApplyTaskRunnerFactory) applyTaskResult {
		if opts.addonStepResources == nil {
			return applyTaskResult{id: task.Entry.ID, err: context.Canceled}
		}
		var keys []string
		switch task.Entry.ID {
		case "export-first":
			keys = []string{"storage:ceph-a"}
		case "export-peer":
			<-firstHeld
			keys = []string{"storage:ceph-a"}
		case "export-other":
			<-firstHeld
			keys = []string{"storage:ceph-b"}
		case "read-only":
			<-firstHeld
		}
		release, err := opts.addonStepResources.acquire(ctx, keys)
		if err != nil {
			return applyTaskResult{id: task.Entry.ID, err: err}
		}
		defer release()
		switch task.Entry.ID {
		case "export-first":
			close(firstHeld)
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return applyTaskResult{id: task.Entry.ID, err: ctx.Err()}
			}
		case "export-peer":
			close(peerEntered)
		case "export-other":
			close(otherEntered)
		case "read-only":
			close(readEntered)
		}
		return applyTaskResult{id: task.Entry.ID}
	}
	tasks := make([]ApplyTask, 0, 4)
	for _, id := range []string{"export-first", "export-peer", "export-other", "read-only"} {
		tasks = append(tasks, ApplyTask{Entry: TaskLedgerEntry{ID: id, Kind: ApplyTaskKindClusterAddon, Label: id, Status: TaskStatusPending}})
	}
	prepared := PreparedApplyTaskGraph{
		RunID:     "step-resource-test",
		StartedAt: time.Now().UTC(),
		Tasks:     tasks,
		Limits:    ConcurrencyLimits{Parallelism: 4},
	}
	done := make(chan error, 1)
	go func() {
		_, err := runPreparedTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), schedulerRunOptions(dir), ApplyTarget{Name: "all"}, "", prepared, nil, func(io.Writer, io.Writer) ansible.Runner { return nil }, executor)
		done <- err
	}()
	awaitSignal(t, firstHeld, "the first external-Ceph export")
	awaitSignal(t, otherEntered, "an export against a different storage cluster")
	awaitSignal(t, readEntered, "a non-mutating step")
	assertNoSignal(t, peerEntered, "a concurrent export against the same storage cluster")
	close(releaseFirst)
	awaitSignal(t, peerEntered, "the peer export after the first released its target")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runPreparedTaskGraph: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runPreparedTaskGraph did not finish")
	}
}

func awaitSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func assertNoSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s ran concurrently", label)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRunStepRefusesStorageMutationWithoutSharedResourcePool(t *testing.T) {
	dir := t.TempDir()
	addon := dfAddonForResourceTest(filepath.Join(dir, "add-on.yaml"))
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "ceph"}}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "odf-export"},
			Spec:     v1alpha1.StorageExportSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"}},
		}},
	}
	executor := &addonStepExecutor{
		runsDir: t.TempDir(),
		runID:   "apply-test",
		taskID:  "addon.ocp.odf",
		opts:    RunOptions{ClustersDir: t.TempDir()},
		state:   state,
		plan:    extensionPlanView{Name: "odf", Cluster: "ocp", Addon: addon},
		inputs:  []v1alpha1.ClusterAddonBindingInput{{Name: "external-storage", Value: "odf-export"}},
	}
	step := addon.Spec.Steps[0]
	if err := osWriteResourceTestPlaybook(dir, step.Playbook); err != nil {
		t.Fatalf("write playbook: %v", err)
	}
	_, err := executor.runStep(context.Background(), step)
	if err == nil {
		t.Fatal("runStep reached a storage mutation without the scheduler's shared resource pool")
	}
	for _, want := range []string{"cannot acquire its mutation lock", "storage:ceph", "resource pool is unavailable"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func dfAddonForResourceTest(sourcePath string) v1alpha1.ClusterAddon {
	return v1alpha1.ClusterAddon{
		Metadata:   v1alpha1.Metadata{Name: "odf"},
		SourcePath: sourcePath,
		Spec: v1alpha1.ClusterAddonSpec{
			Accepts: v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
				Name:        "external-storage",
				ResourceRef: &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport},
			}}},
			Steps: []v1alpha1.ClusterAddonStep{{
				Name:     "attach-external-storage",
				Playbook: "playbooks/export.yaml",
				Target: v1alpha1.ClusterAddonStepTarget{
					FromInput: &v1alpha1.ClusterAddonStepInputTarget{Input: "external-storage"},
				},
			}},
		},
	}
}

func osWriteResourceTestPlaybook(root, rel string) error {
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("- hosts: all\n  tasks: []\n"), 0o600)
}
