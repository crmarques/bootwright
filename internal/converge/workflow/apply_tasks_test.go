package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/render"
	secretstore "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/state/desired"
)

type fakeClusterAvailabilityChecker struct {
	available bool
	err       error
	paths     []string
}

func (f *fakeClusterAvailabilityChecker) Available(_ context.Context, kubeconfigPath string) (bool, error) {
	f.paths = append(f.paths, kubeconfigPath)
	return f.available, f.err
}

type blockingApplyRunner struct {
	started chan struct{}
}

type recordingApplyRunner struct {
	mu        sync.Mutex
	calls     []string
	failures  map[string]error
	delay     time.Duration
	active    int
	maxActive int
}

func (r *blockingApplyRunner) Run(ctx context.Context, _ ansible.RunSpec) error {
	close(r.started)
	<-ctx.Done()
	return ctx.Err()
}

func (r *blockingApplyRunner) Command(ansible.RunSpec) []string {
	return []string{"ansible-playbook"}
}

func (r *recordingApplyRunner) Run(ctx context.Context, spec ansible.RunSpec) error {
	id := spec.Playbook
	r.mu.Lock()
	r.calls = append(r.calls, id)
	r.active++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.active--
		r.mu.Unlock()
	}()

	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := r.failures[id]; err != nil {
		return err
	}
	return nil
}

func (r *recordingApplyRunner) Command(spec ansible.RunSpec) []string {
	return []string{spec.Playbook}
}

func (r *recordingApplyRunner) snapshot() ([]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...), r.maxActive
}

// cancelOnRunApplyRunner cancels the run context the first time it is invoked,
// recording every playbook it is asked to run. It exercises the scheduler's
// "stop launching after cancellation" path: only the task that triggered the
// cancel must have launched.
type cancelOnRunApplyRunner struct {
	cancel func()
	mu     sync.Mutex
	calls  []string
}

func (r *cancelOnRunApplyRunner) Run(_ context.Context, spec ansible.RunSpec) error {
	r.mu.Lock()
	r.calls = append(r.calls, spec.Playbook)
	r.mu.Unlock()
	r.cancel()
	return nil
}

func (r *cancelOnRunApplyRunner) Command(spec ansible.RunSpec) []string {
	return []string{spec.Playbook}
}

func (r *cancelOnRunApplyRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func TestRunApplyTaskGraphUsesRunnerFactory(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &fakeRunner{}
	calls := 0
	factory := func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		calls++
		if stdout == nil || stderr == nil {
			t.Fatal("runner factory received nil task writers")
		}
		return runner
	}
	task := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:     "provider.service-host",
			Kind:   ApplyTaskKindProvider,
			Label:  "provider services service-host",
			Status: TaskStatusPending,
		},
		Playbook: "bootwright.core.task_provider_services_apply",
		State:    state,
	}
	renderedDir := filepath.Join(dir, "rendered")
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
		State:              state,
		RenderedDir:        renderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: managedServicesDir,
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
		ArtifactsBaseName:  "provider",
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseFabric}}, "", []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1}, nil, factory)
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner factory calls = %d, want 1", calls)
	}
	if !runner.runCalled {
		t.Fatal("fake runner was not invoked")
	}
	if !strings.HasSuffix(runner.lastSpec.Playbook, "bootwright.core.task_provider_services_apply") {
		t.Fatalf("playbook = %q", runner.lastSpec.Playbook)
	}
	record, found, err := LoadConvergeSafetyRecord(runsDir, "providerServices/provider.service-host")
	if err != nil || !found {
		t.Fatalf("LoadConvergeSafetyRecord found=%v err=%v", found, err)
	}
	wantHash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		t.Fatalf("ApplyTaskDesiredHash: %v", err)
	}
	if record.DesiredHash != wantHash || record.Owner.Manager != ConvergeSafetyOwner || record.Status != ConvergeSafetyStatusReconciled {
		t.Fatalf("safety record = %+v, want Bootwright reconciled record with desired hash %s", record, wantHash)
	}
	if record.Observation.Classification != ConvergeSafetyUnknown {
		t.Fatalf("safety observation = %+v, want unknown generic provider observation", record.Observation)
	}
}

func TestApplyTaskDesiredHashFabricVarsIgnoreUnrelatedState(t *testing.T) {
	mustHash := func(task ApplyTask) string {
		t.Helper()
		h, err := ApplyTaskDesiredHash(task)
		if err != nil {
			t.Fatalf("ApplyTaskDesiredHash: %v", err)
		}
		return h
	}
	base := ApplyTask{
		Entry:           TaskLedgerEntry{ID: "provider.host-a", Kind: ApplyTaskKindProvider, Host: "host-a"},
		Playbook:        applyProviderPlaybook,
		Limit:           "host-a",
		State:           v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "cluster-a"}}}},
		DesiredHashVars: []any{map[string]any{"machineRef": "host-a", "kind": "loadBalancer"}},
	}

	// An unrelated cluster appears in State, but the host's rendered fabric vars
	// are unchanged: state-check must NOT report this fabric host as drifted.
	unrelated := base
	unrelated.State = v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{
		{Metadata: v1alpha1.Metadata{Name: "cluster-a"}},
		{Metadata: v1alpha1.Metadata{Name: "cluster-b"}},
	}}

	// The host's own rendered vars change (e.g. a new load-balancer frontend): the
	// fabric host MUST report drift.
	relevant := base
	relevant.DesiredHashVars = []any{map[string]any{"machineRef": "host-a", "kind": "loadBalancer", "frontends": []any{"vip"}}}

	if got, want := mustHash(unrelated), mustHash(base); got != want {
		t.Fatalf("fabric desired hash changed on an unrelated State edit: %s vs %s", got, want)
	}
	if mustHash(relevant) == mustHash(base) {
		t.Fatal("fabric desired hash did not change when the host's rendered vars changed")
	}
}

func TestApplyTaskDesiredHashNonFabricStillHashesState(t *testing.T) {
	base := ApplyTask{
		Entry: TaskLedgerEntry{ID: "clusterInstall/c", Kind: ApplyTaskKindClusterInstall, Cluster: "c"},
		State: v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "c"}}}},
	}
	changed := base
	changed.State = v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "c2"}}}}
	h0, err := ApplyTaskDesiredHash(base)
	if err != nil {
		t.Fatalf("ApplyTaskDesiredHash: %v", err)
	}
	h1, err := ApplyTaskDesiredHash(changed)
	if err != nil {
		t.Fatalf("ApplyTaskDesiredHash: %v", err)
	}
	if h0 == h1 {
		t.Fatal("non-fabric desired hash did not change when State changed")
	}
}

func TestConvergeSafetyClassification(t *testing.T) {
	const desiredHash = "sha256:desired"
	cases := []struct {
		name   string
		record ConvergeSafetyRecord
		want   ConvergeSafetyClassification
	}{
		{
			name: "missing",
			want: ConvergeSafetyMissing,
		},
		{
			name:   "match",
			record: ConvergeSafetyRecord{ResourceID: "resource", DesiredHash: desiredHash, Owner: ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner}},
			want:   ConvergeSafetyMatch,
		},
		{
			name:   "drift",
			record: ConvergeSafetyRecord{ResourceID: "resource", DesiredHash: "sha256:old", Owner: ConvergeSafetyOwnerIdentity{Manager: ConvergeSafetyOwner}},
			want:   ConvergeSafetyDrift,
		},
		{
			name:   "foreign",
			record: ConvergeSafetyRecord{ResourceID: "resource", DesiredHash: desiredHash, Owner: ConvergeSafetyOwnerIdentity{Manager: "other"}},
			want:   ConvergeSafetyForeign,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyConvergeSafety(tc.record, desiredHash, ConvergeSafetyOwner); got != tc.want {
				t.Fatalf("ClassifyConvergeSafety = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRunApplyTaskGraphFailsWhenTasksCannotMakeProgress(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	calls := 0
	task := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:           "provider.service-host",
			Kind:         ApplyTaskKindProvider,
			Label:        "provider services service-host",
			Status:       TaskStatusPending,
			Dependencies: []string{"provider.missing"},
		},
		Playbook: "bootwright.core.task_provider_services_apply",
		State:    state,
	}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseFabric}}, "", []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		calls++
		return &fakeRunner{}
	})
	if err == nil || !strings.Contains(err.Error(), "could not make progress") {
		t.Fatalf("RunApplyTaskGraph error = %v, want progress error", err)
	}
	if calls != 0 {
		t.Fatalf("runner factory calls = %d, want 0", calls)
	}
	if ledger.Status != RunStatusFailed {
		t.Fatalf("ledger status = %s, want failed", ledger.Status)
	}
	got, ok := ledger.Task("provider.service-host")
	if !ok {
		t.Fatal("missing provider task")
	}
	if got.Status != TaskStatusBlocked {
		t.Fatalf("task status = %s, want blocked", got.Status)
	}
	if !strings.Contains(got.SkippedReason, "provider.missing (missing)") {
		t.Fatalf("blocked reason = %q, want missing dependency", got.SkippedReason)
	}
}

func TestRunApplyTaskGraphContinuesIndependentBranchAfterTaskFailure(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &recordingApplyRunner{failures: map[string]error{"fail-a": errors.New("boom")}}
	tasks := []ApplyTask{
		{
			Entry:    TaskLedgerEntry{ID: "fail-a", Kind: ApplyTaskKindProvider, Label: "fail-a", Status: TaskStatusPending},
			Playbook: "fail-a",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "blocked-a", Kind: ApplyTaskKindProvider, Label: "blocked-a", Status: TaskStatusPending, Dependencies: []string{"fail-a"}},
			Playbook: "blocked-a",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "ok-b", Kind: ApplyTaskKindProvider, Label: "ok-b", Status: TaskStatusPending},
			Playbook: "ok-b",
			State:    state,
		},
	}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseFabric}}, "", tasks, ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		return runner
	})
	if err == nil || !strings.Contains(err.Error(), "fail-a failed") {
		t.Fatalf("RunApplyTaskGraph error = %v, want fail-a task error", err)
	}
	calls, _ := runner.snapshot()
	if !reflect.DeepEqual(calls, []string{"fail-a", "ok-b"}) {
		t.Fatalf("runner calls = %v, want failed branch and independent branch only", calls)
	}
	if task, _ := ledger.Task("fail-a"); task.Status != TaskStatusFailed {
		t.Fatalf("fail-a status = %s, want failed", task.Status)
	}
	if task, _ := ledger.Task("ok-b"); task.Status != TaskStatusOK {
		t.Fatalf("ok-b status = %s, want ok", task.Status)
	}
	if task, _ := ledger.Task("blocked-a"); task.Status != TaskStatusBlocked || !strings.Contains(task.SkippedReason, "dependency fail-a failed") {
		t.Fatalf("blocked-a = %s/%q, want blocked by failed dependency", task.Status, task.SkippedReason)
	}
}

func TestObjectProtectedKindStorageSubObjectMapsToStorageCluster(t *testing.T) {
	for _, kind := range []string{storageSubObjectKindPool, storageSubObjectKindFilesystem, storageSubObjectKindGateway, storageSubObjectKindNFSExport, storageSubObjectKindExport} {
		if got := objectProtectedKind(ObjectClassification{Kind: kind}); got != v1alpha1.KindStorageCluster {
			t.Fatalf("%s protected kind = %q, want StorageCluster", kind, got)
		}
	}
}

func TestOverrideDestructiveKindProtectedCoversStoragePool(t *testing.T) {
	pool := ObjectClassification{
		Kind:   storageSubObjectKindPool,
		Label:  "StoragePool/ceph.rbd",
		counts: map[ConvergeSafetyClassification]int{ConvergeSafetyDrift: 1},
	}
	protected := map[string]bool{v1alpha1.KindStorageCluster: true}
	if got := OverrideDestructiveKindProtected([]ObjectClassification{pool}, protected); len(got) != 1 || got[0] != "StoragePool/ceph.rbd" {
		t.Fatalf("a protected StorageCluster must gate a structurally-drifted pool rebuild, got %v", got)
	}
	// Negative: pool not in the protected set -> not gated.
	if got := OverrideDestructiveKindProtected([]ObjectClassification{pool}, map[string]bool{}); len(got) != 0 {
		t.Fatalf("unprotected pool must not be gated, got %v", got)
	}
	// Negative: reconcilable-only drift is not destructive -> not gated.
	reconcilable := pool
	reconcilable.counts = map[ConvergeSafetyClassification]int{ConvergeSafetyDrift: 1}
	reconcilable.reconcilable = 1
	if got := OverrideDestructiveKindProtected([]ObjectClassification{reconcilable}, protected); len(got) != 0 {
		t.Fatalf("reconcilable-only pool drift must not be gated, got %v", got)
	}
}

func TestRunApplyTaskGraphStopsLaunchingAfterContextCancel(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &cancelOnRunApplyRunner{cancel: cancel}
	tasks := []ApplyTask{
		{
			Entry:    TaskLedgerEntry{ID: "first", Kind: ApplyTaskKindProvider, Label: "first", Status: TaskStatusPending},
			Playbook: "first",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "second", Kind: ApplyTaskKindProvider, Label: "second", Status: TaskStatusPending},
			Playbook: "second",
			State:    state,
		},
	}
	ledger, err := RunApplyTaskGraph(ctx, io.Discard, io.Discard, filepath.Join(dir, "runs"), RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseFabric}}, "", tasks, ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		return runner
	})
	// The graph does not complete (second never runs), so an error is expected;
	// the point is that no task launched after the cancellation.
	_ = err
	calls := runner.snapshot()
	if len(calls) != 1 || calls[0] != "first" {
		t.Fatalf("runner calls = %v, want only the cancel-triggering task to have launched", calls)
	}
	if task, _ := ledger.Task("second"); task.Status == TaskStatusOK || task.Status == TaskStatusRunning {
		t.Fatalf("second task status = %s, want it never launched after cancellation", task.Status)
	}
}

func TestRestoreClusterInstallRecordOnSkipRemovesPhantom(t *testing.T) {
	clustersDir := filepath.Join(t.TempDir(), "clusters")
	now := time.Now()
	// A start-mark stamped installing/booting for a cluster with no prior record
	// (a fresh nodeBoot that then no-op skipped because no hosts matched).
	phantom := ClusterInstallRecord{Cluster: "c", Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseBooting, UpdatedAt: now.UTC()}
	if err := SaveClusterInstallRecord(clustersDir, phantom); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	if err := restoreClusterInstallRecordOnSkip(clustersDir, "c", ClusterInstallRecord{}, false); err != nil {
		t.Fatalf("restoreClusterInstallRecordOnSkip: %v", err)
	}
	if _, found, err := LoadClusterInstallRecord(clustersDir, "c"); err != nil || found {
		t.Fatalf("phantom installing record should be removed, found=%v err=%v", found, err)
	}
}

func TestRestoreClusterInstallRecordOnSkipRestoresPrior(t *testing.T) {
	clustersDir := filepath.Join(t.TempDir(), "clusters")
	now := time.Now()
	prior := ClusterInstallRecord{Cluster: "c", Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseISOCreated, RunID: "run-1", UpdatedAt: now.UTC()}
	overwritten := ClusterInstallRecord{Cluster: "c", Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseBooting, RunID: "run-2", UpdatedAt: now.UTC()}
	if err := SaveClusterInstallRecord(clustersDir, overwritten); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	if err := restoreClusterInstallRecordOnSkip(clustersDir, "c", prior, true); err != nil {
		t.Fatalf("restoreClusterInstallRecordOnSkip: %v", err)
	}
	got, found, err := LoadClusterInstallRecord(clustersDir, "c")
	if err != nil || !found {
		t.Fatalf("expected a restored record, found=%v err=%v", found, err)
	}
	if got.Phase != ClusterInstallPhaseISOCreated || got.RunID != "run-1" {
		t.Fatalf("expected prior record restored, got %+v", got)
	}
}

func TestRunApplyTaskGraphHonorsCountedHostSlots(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &recordingApplyRunner{delay: 25 * time.Millisecond}
	tasks := []ApplyTask{}
	for _, id := range []string{"machine-a", "machine-b", "machine-c"} {
		tasks = append(tasks, ApplyTask{
			Entry: TaskLedgerEntry{
				ID:            id,
				Kind:          ApplyTaskKindClusterInstall,
				Label:         id,
				Status:        TaskStatusPending,
				HostSlotKey:   "host:bastion:machine",
				HostSlotCount: 1,
			},
			Playbook:      id,
			State:         state,
			HostSlotKey:   "host:bastion:machine",
			HostSlotCount: 1,
		})
	}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseMachines}}, "", tasks, ConcurrencyLimits{Parallelism: 3, ParallelismPerHost: 2}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		return runner
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	calls, maxActive := runner.snapshot()
	if len(calls) != 3 {
		t.Fatalf("runner calls = %v, want all three machine tasks", calls)
	}
	if maxActive != 2 {
		t.Fatalf("max active host-slot tasks = %d, want 2", maxActive)
	}
	for _, id := range []string{"machine-a", "machine-b", "machine-c"} {
		if task, _ := ledger.Task(id); task.Status != TaskStatusOK {
			t.Fatalf("%s status = %s, want ok", id, task.Status)
		}
	}
}

func TestRunApplyTaskGraphLimitsManagedOSForksToRedfishSlots(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &fakeRunner{}
	task := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:     "osinstall.ceph-libvirt",
			Kind:   ApplyTaskKindManagedMachineOS,
			Label:  "managed OS ceph-libvirt machines",
			Status: TaskStatusPending,
		},
		Playbook:     "bootwright.core.task_managed_machine_os_apply",
		State:        state,
		Forks:        3,
		RedfishSlots: 3,
	}
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseMachines}}, "", []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1, ParallelismRedfish: 2}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		return runner
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if runner.lastSpec.Forks != 2 {
		t.Fatalf("managed OS forks = %d, want Redfish-limited 2", runner.lastSpec.Forks)
	}
}

func TestRunApplyTaskGraphReportsLeaseHeartbeatFailure(t *testing.T) {
	dir := t.TempDir()
	oldSave := saveRunLease
	oldInterval := applyLeaseHeartbeatInterval
	saveRunLease = func(string, RunLease) error {
		return errors.New("lease store unavailable")
	}
	applyLeaseHeartbeatInterval = time.Millisecond
	t.Cleanup(func() {
		saveRunLease = oldSave
		applyLeaseHeartbeatInterval = oldInterval
	})

	state := minimalState()
	runner := &blockingApplyRunner{started: make(chan struct{})}
	task := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:     "provider.service-host",
			Kind:   ApplyTaskKindProvider,
			Label:  "provider services service-host",
			Status: TaskStatusPending,
		},
		Playbook: "bootwright.core.task_provider_services_apply",
		State:    state,
	}
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseFabric}}, "", []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		return runner
	})
	if err == nil || !strings.Contains(err.Error(), "refresh apply lease") {
		t.Fatalf("RunApplyTaskGraph error = %v, want lease heartbeat error", err)
	}
	select {
	case <-runner.started:
	default:
		t.Fatal("runner did not start before heartbeat failure")
	}
}

func TestRunApplyTaskGraphSkipsInstalledClusterBeforeAnsible(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	renderedDir := filepath.Join(dir, "rendered")
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
	hash, err := clusterInstallDesiredHashForContext("test", state, "sno-libvirt", secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash: %v", err)
	}
	now := time.Now()
	record := ClusterInstallRecord{
		Cluster:     "sno-libvirt",
		DesiredHash: hash,
		Status:      ClusterInstallStatusInstalled,
		Phase:       ClusterInstallPhaseComplete,
		RunID:       "previous-run",
		StartedAt:   now.UTC(),
		UpdatedAt:   now.UTC(),
		InstalledAt: &now,
	}
	if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	kubeconfig := clusterKubeconfigPath(clustersDir, "sno-libvirt")
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o700); err != nil {
		t.Fatalf("mkdir kubeconfig dir: %v", err)
	}
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	checker := &fakeClusterAvailabilityChecker{available: true}
	calls := 0
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
		State:                      state,
		RenderedDir:                renderedDir,
		ClustersDir:                clustersDir,
		RunsDir:                    runsDir,
		SecretsDir:                 secretsDir,
		ManagedServicesDir:         managedServicesDir,
		ProviderStateDir:           filepath.Join(dir, "provider-state"),
		BundleDir:                  filepath.Join(dir, "bundle"),
		ClusterAvailabilityChecker: checker,
	}, applyContainerClusterTarget(), "", mustPlanApplyTasks(applyContainerClusterTarget(), state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		calls++
		return &fakeRunner{}
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if calls != 0 {
		t.Fatalf("runner factory calls = %d, want 0", calls)
	}
	for _, task := range ledger.Tasks {
		if task.Status != TaskStatusSkipped {
			t.Fatalf("task %s status = %s, want skipped", task.ID, task.Status)
		}
	}
	if len(checker.paths) != 1 || checker.paths[0] != kubeconfig {
		t.Fatalf("availability paths = %v, want %s", checker.paths, kubeconfig)
	}
}

func TestRunApplyTaskGraphBlocksInstalledClusterHashMismatch(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	renderedDir := filepath.Join(dir, "rendered")
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
		Cluster:     "sno-libvirt",
		DesiredHash: "sha256:old",
		Status:      ClusterInstallStatusInstalled,
		Phase:       ClusterInstallPhaseComplete,
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	calls := 0
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
		State:              state,
		RenderedDir:        renderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         secretsDir,
		ManagedServicesDir: managedServicesDir,
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, applyContainerClusterTarget(), "", mustPlanApplyTasks(applyContainerClusterTarget(), state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		calls++
		return &fakeRunner{}
	})
	if err == nil || !strings.Contains(err.Error(), "different install inputs") {
		t.Fatalf("RunApplyTaskGraph error = %v, want different install inputs", err)
	}
	if calls != 0 {
		t.Fatalf("runner factory calls = %d, want 0", calls)
	}
}

func TestRunApplyTaskGraphBlocksEmptyDesiredHashAfterNodeBoot(t *testing.T) {
	cases := []struct {
		name   string
		status ClusterInstallStatus
		phase  ClusterInstallPhase
	}{
		{
			name:   "nodes booted",
			status: ClusterInstallStatusInstalling,
			phase:  ClusterInstallPhaseNodesBooted,
		},
		{
			name:   "installed",
			status: ClusterInstallStatusInstalled,
			phase:  ClusterInstallPhaseComplete,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			state := loadWorkflowFixtureState(t, "001-sno-libvirt")
			secretsDir := writeWorkflowInstallerSecrets(t, dir)
			renderedDir := filepath.Join(dir, "rendered")
			clustersDir := filepath.Join(dir, "clusters")
			runsDir := filepath.Join(dir, "runs")
			managedServicesDir := filepath.Join(dir, "managed-services")
			if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
				Cluster:   "sno-libvirt",
				Status:    tc.status,
				Phase:     tc.phase,
				UpdatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatalf("SaveClusterInstallRecord: %v", err)
			}
			calls := 0
			_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
				State:              state,
				RenderedDir:        renderedDir,
				ClustersDir:        clustersDir,
				RunsDir:            runsDir,
				SecretsDir:         secretsDir,
				ManagedServicesDir: managedServicesDir,
				ProviderStateDir:   filepath.Join(dir, "provider-state"),
				BundleDir:          filepath.Join(dir, "bundle"),
			}, applyContainerClusterTarget(), "", mustPlanApplyTasks(applyContainerClusterTarget(), state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
				calls++
				return &fakeRunner{}
			})
			if err == nil || !strings.Contains(err.Error(), "missing or different install inputs") {
				t.Fatalf("RunApplyTaskGraph error = %v, want missing or different install inputs", err)
			}
			if calls != 0 {
				t.Fatalf("runner factory calls = %d, want 0", calls)
			}
		})
	}
}

func TestRunApplyTaskGraphResumesPostBootInstallAtWait(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	renderedDir := filepath.Join(dir, "rendered")
	clustersDir := filepath.Join(dir, "clusters")
	runsDir := filepath.Join(dir, "runs")
	managedServicesDir := filepath.Join(dir, "managed-services")
	hash, err := clusterInstallDesiredHashForContext("test", state, "sno-libvirt", secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash: %v", err)
	}
	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{
		Cluster:     "sno-libvirt",
		DesiredHash: hash,
		Status:      ClusterInstallStatusInstalling,
		Phase:       ClusterInstallPhaseNodesBooted,
		RunID:       "previous-run",
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
	runner := &fakeRunner{}
	calls := 0
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
		State:              state,
		RenderedDir:        renderedDir,
		ClustersDir:        clustersDir,
		RunsDir:            runsDir,
		SecretsDir:         secretsDir,
		ManagedServicesDir: managedServicesDir,
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, applyContainerClusterTarget(), "", mustPlanApplyTasks(applyContainerClusterTarget(), state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		calls++
		return runner
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner factory calls = %d, want 1", calls)
	}
	if !strings.HasSuffix(runner.lastSpec.Playbook, "bootwright.core.task_container_cluster_wait_agent_install") {
		t.Fatalf("playbook = %q, want wait-agent-install.yml", runner.lastSpec.Playbook)
	}
	for _, id := range []string{"iso.sno-libvirt", "boot.sno-libvirt"} {
		task, ok := ledger.Task(id)
		if !ok {
			t.Fatalf("missing task %s", id)
		}
		if task.Status != TaskStatusSkipped {
			t.Fatalf("task %s status = %s, want skipped", id, task.Status)
		}
	}
	wait, ok := ledger.Task("wait.sno-libvirt")
	if !ok {
		t.Fatal("missing wait task")
	}
	if wait.Status != TaskStatusOK {
		t.Fatalf("wait status = %s, want ok", wait.Status)
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, "sno-libvirt")
	if err != nil || !found {
		t.Fatalf("LoadClusterInstallRecord found=%v err=%v", found, err)
	}
	if record.Status != ClusterInstallStatusInstalled || record.Phase != ClusterInstallPhaseComplete {
		t.Fatalf("record = %+v, want installed complete", record)
	}
	data, err := os.ReadFile(ClusterConnectionPath(clustersDir, "sno-libvirt"))
	if err != nil {
		t.Fatalf("read cluster connection record: %v", err)
	}
	var connection ClusterConnectionRecord
	if err := json.Unmarshal(data, &connection); err != nil {
		t.Fatalf("decode cluster connection record: %v", err)
	}
	if connection.KubeconfigPath != clusterKubeconfigPath(clustersDir, "sno-libvirt") {
		t.Fatalf("connection kubeconfig path = %q", connection.KubeconfigPath)
	}
	if connection.APIURL != "https://api.sno-libvirt.bootwright.test:6443" {
		t.Fatalf("connection API URL = %q", connection.APIURL)
	}
	if connection.ConsoleURL != "https://console-openshift-console.apps.sno-libvirt.bootwright.test" {
		t.Fatalf("connection console URL = %q", connection.ConsoleURL)
	}
	if connection.IngressBaseDomain != "apps.sno-libvirt.bootwright.test" {
		t.Fatalf("connection ingress base domain = %q", connection.IngressBaseDomain)
	}
}

func TestClusterInstallRecordTracksNodeSafePoints(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	clustersDir := filepath.Join(dir, "clusters")
	runner := &fakeRunner{}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        clustersDir,
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         secretsDir,
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, applyContainerClusterTarget(), "", mustPlanApplyTasks(applyContainerClusterTarget(), state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		return runner
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if ledger.Status != RunStatusOK {
		t.Fatalf("ledger status = %s, want ok", ledger.Status)
	}
	record, found, err := LoadClusterInstallRecord(clustersDir, "sno-libvirt")
	if err != nil || !found {
		t.Fatalf("LoadClusterInstallRecord found=%v err=%v", found, err)
	}
	if record.Status != ClusterInstallStatusInstalled || record.Phase != ClusterInstallPhaseComplete {
		t.Fatalf("record = %+v, want installed complete", record)
	}
}

func TestClusterInstallDesiredHashChangesWhenProxyCredentialsChange(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "004-3nodes-emul-baremetal")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	clusterName := "3-nodes-ocp-emul-baremetal"

	first, err := clusterInstallDesiredHashForContext("test", state, clusterName, secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash first: %v", err)
	}
	proxyCreds := filepath.Join(secretsDir, "proxy-credentials")
	if err := secretstore.NewContextStore("test", secretsDir).Write(secretstore.MaterialKey{Name: "proxy-credentials", Role: secretstore.MaterialPrimary}, []byte("proxy:changed-secret\n")); err != nil {
		t.Fatalf("write proxy credentials: %v", err)
	}
	changedAt := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(proxyCreds, changedAt, changedAt); err != nil {
		t.Fatalf("chtimes proxy credentials: %v", err)
	}
	second, err := clusterInstallDesiredHashForContext("test", state, clusterName, secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash second: %v", err)
	}
	if first == second {
		t.Fatal("cluster install desired hash did not change after proxy credentials changed")
	}
}

func TestPlanApplyAddonsOrdersAddonTasks(t *testing.T) {
	state := extensionPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "add-ons", PhaseNames: []string{ApplyPhaseAddons}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	gotIDs := applyTaskIDs(tasks)
	wantIDs := []string{
		"addon.demo.a",
		"addon.demo.b",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("task IDs = %v, want %v", gotIDs, wantIDs)
	}
	assertTaskDeps(t, tasks, "addon.demo.a")
	assertTaskDeps(t, tasks, "addon.demo.b", "addon.demo.a")
}

func TestPlanApplyAllRunsAddonsAfterInstallWait(t *testing.T) {
	state := extensionPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskDeps(t, tasks, "addon.demo.a", "wait.demo")
}

func TestPlanApplyContainerClusterRunsAddonsAfterInstallWait(t *testing.T) {
	state := extensionPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "container-cluster", PhaseNames: []string{ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	gotIDs := applyTaskIDs(tasks)
	wantIDs := []string{
		"iso.demo",
		"wait.demo",
		"addon.demo.a",
		"addon.demo.b",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("task IDs = %v, want %v", gotIDs, wantIDs)
	}
	assertTaskDeps(t, tasks, "addon.demo.a", "wait.demo")
	assertTaskDeps(t, tasks, "addon.demo.b", "addon.demo.a")
}

func TestPlanApplyBaseOnlyDropsISODependencyForSurgicalRerun(t *testing.T) {
	state := loadWorkflowFixtureState(t, "001-sno-libvirt")

	// base-only (`apply --stage base`): the iso task lives in the deps phase and
	// is not planned, so boot/wait must NOT carry a dependency on it. Otherwise
	// the scheduler blocks on iso.<cluster> "(missing)" and the surgical rerun the
	// flag is meant to support fails. The rerun reuses the ISO a prior deps run
	// published.
	baseOnly, err := PlanApplyTasksChecked(ApplyTarget{Name: "base", PhaseNames: []string{ApplyPhaseBase}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked base-only: %v", err)
	}
	assertTaskMissing(t, baseOnly, "iso.sno-libvirt")
	assertTaskDeps(t, baseOnly, "boot.sno-libvirt")
	assertTaskDeps(t, baseOnly, "wait.sno-libvirt", "boot.sno-libvirt")

	// deps+base together: the iso task is planned, so boot orders behind it.
	depsBase, err := PlanApplyTasksChecked(ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseDeps, ApplyPhaseBase}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked deps+base: %v", err)
	}
	assertTaskPresent(t, depsBase, "iso.sno-libvirt")
	assertTaskDeps(t, depsBase, "boot.sno-libvirt", "iso.sno-libvirt")
}

func TestPlanApplyClustersOrdersClusterLifecycleAndIntegrations(t *testing.T) {
	state := storageAttachmentPlanningState()

	tasks, err := PlanApplyTasksChecked(applyClustersTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskDeps(t, tasks, "storage.ceph", "storageinfra.ceph")
	assertTaskDeps(t, tasks, "iso.demo")
	assertTaskDeps(t, tasks, "wait.demo", "iso.demo")
	// The attachment is owned by the add-on's hooks inside its addon task; no
	// compiled attachment task exists (see
	// TestPlanApplyAllPlansNoCompiledAttachmentTask). This hookless add-on has
	// no fromInput target, so it orders only behind its cluster install wait.
	assertTaskDeps(t, tasks, "addon.demo.odf", "wait.demo")
}

// TestPlanApplyAllPlansNoCompiledAttachmentTask pins the migration: the
// storage-export attachment is applied by the consuming add-on's own hooks
// inside its addon task, so no separate compiled attachment task is planned.
func TestPlanApplyAllPlansNoCompiledAttachmentTask(t *testing.T) {
	state := storageAttachmentPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	for _, id := range applyTaskIDs(tasks) {
		if strings.HasPrefix(id, "storageattachment.") {
			t.Fatalf("compiled attachment task planned: %v", applyTaskIDs(tasks))
		}
	}
	assertTaskPresent(t, tasks, "addon.demo.odf")
}

func TestPlanApplyAllExternalStorageAttachmentSkipsStorageTask(t *testing.T) {
	state := externalStorageAttachmentPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	for _, id := range applyTaskIDs(tasks) {
		if id == "storage.shared-ceph" {
			t.Fatalf("external storage planned storage task: %v", applyTaskIDs(tasks))
		}
	}
	assertTaskDeps(t, tasks, "addon.demo.odf", "wait.demo")
}

func TestExamplesLoadValidateRenderAndPlanApplyAll(t *testing.T) {
	examplesRoot := filepath.Join("..", "..", "..", "examples")
	entries, err := os.ReadDir(examplesRoot)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		// _wip holds intentionally-incomplete scratch examples (gitignored);
		// they are not canonical and are not validated here.
		if name == "_wip" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(examplesRoot, name)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			if _, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state); err != nil {
				t.Fatalf("render.All: %v", err)
			}
			if _, err := PlanApplyTasksChecked(applyAllTarget(), state); err != nil {
				t.Fatalf("PlanApplyTasksChecked full graph: %v", err)
			}
		})
	}
}

// TestDataFoundationExamplesPlanAddonOwnedAttachments pins the migrated shape:
// no compiled attachment tasks; the exporter-hook add-on (managed Ceph) pulls
// a storage.<ceph> dependency onto its own addon task via the hook's fromInput
// target, while the manifest-only add-on (imported Ceph, fromSecretRef) needs
// no Ceph-side dependency at all.
func TestDataFoundationExamplesPlanAddonOwnedAttachments(t *testing.T) {
	cases := []struct {
		example        string
		clusters       []string
		addon          string
		storageTask    string
		wantStorageJob bool
	}{
		{
			example:     "baremetal-redfish-imported-ceph-odf",
			clusters:    []string{"metal-ocp"},
			addon:       "openshift-data-foundation",
			storageTask: "storage.imported-ceph",
		},
		{
			example:        "baremetal-redfish-multidc-virtualized-odf-ceph",
			clusters:       []string{"dc1-metal-ocp", "dc2-metal-ocp", "dc1-child-ocp", "dc2-child-ocp"},
			addon:          "openshift-data-foundation",
			storageTask:    "storage.ceph-storage",
			wantStorageJob: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.example, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", tc.example)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
			if err != nil {
				t.Fatalf("PlanApplyTasksChecked: %v", err)
			}
			if tc.wantStorageJob {
				assertTaskPresent(t, tasks, tc.storageTask)
			} else {
				assertTaskMissing(t, tasks, tc.storageTask)
			}
			for _, id := range applyTaskIDs(tasks) {
				if strings.HasPrefix(id, "storageattachment.") {
					t.Fatalf("compiled attachment task planned: %v", applyTaskIDs(tasks))
				}
			}
			for _, cluster := range tc.clusters {
				addonID := "addon." + cluster + "." + tc.addon
				if tc.wantStorageJob {
					// The exporter hook's fromInput target pulls the managed
					// Ceph apply in as a direct dependency of the addon task.
					assertTaskHasDeps(t, tasks, addonID, tc.storageTask)
				}
				// The add-on orders behind its cluster's install readiness —
				// directly, or through the binding's addon chain (e.g. behind
				// a preceding openshift-virtualization addon task).
				assertTaskDependsTransitively(t, tasks, addonID, "wait."+cluster)
			}
		})
	}
}

func TestPlanApplyStorageTaskStateRendersWithoutConsumerClusterInstall(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "storage", PhaseNames: []string{ApplyPhaseBase}, ClusterKind: ApplyClusterKindStorage}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	var storageTask ApplyTask
	for _, task := range tasks {
		if task.Entry.ID == "storage.ceph-storage" {
			storageTask = task
			break
		}
	}
	if storageTask.Entry.ID == "" {
		t.Fatalf("storage task not found in %v", applyTaskIDs(tasks))
	}
	result, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), storageTask.State)
	if err != nil {
		t.Fatalf("render storage task state: %v", err)
	}
	if len(result.InstallerAssets) != 0 {
		t.Fatalf("storage task rendered installer assets: %#v", result.InstallerAssets)
	}
}

func TestStorageTaskRunsThroughAnsible(t *testing.T) {
	state := storageAttachmentPlanningState()
	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "storage", PhaseNames: []string{ApplyPhaseBase}, ClusterKind: ApplyClusterKindStorage}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	var task ApplyTask
	for _, candidate := range tasks {
		if candidate.Entry.ID == "storage.ceph" {
			task = candidate
			break
		}
	}
	if task.Entry.ID == "" {
		t.Fatalf("storage task not found in %v", applyTaskIDs(tasks))
	}
	if task.Playbook != "bootwright.core.task_storage_cluster_apply" {
		t.Fatalf("storage playbook = %q", task.Playbook)
	}
	if task.Limit != render.StorageNodeHostName("ceph", "ceph-0") {
		t.Fatalf("storage limit = %q", task.Limit)
	}
	if !reflect.DeepEqual(task.ExtraVarPairs, []string{"bootwright_task_storage_cluster_name=ceph", "bootwright_task_storage_skip_prereqs=true"}) {
		t.Fatalf("storage extra vars = %v", task.ExtraVarPairs)
	}

	dir := t.TempDir()
	runner := &fakeRunner{}
	_, err = RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "storage", PhaseNames: []string{ApplyPhaseBase}, ClusterKind: ApplyClusterKindStorage}, "", []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		return runner
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if !runner.runCalled {
		t.Fatal("storage task did not invoke Ansible runner")
	}
	if !strings.HasSuffix(runner.lastSpec.Playbook, "bootwright.core.task_storage_cluster_apply") {
		t.Fatalf("storage playbook = %q", runner.lastSpec.Playbook)
	}
}

func TestManagedStorageOSInstallTaskPrecedesCephInfra(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskDeps(t, tasks, "osprepare.ceph-libvirt.bastion", "provider.bastion", "infra-component.bastion")
	assertTaskPresent(t, tasks, "infra-component.bastion")
	task := assertTaskPresent(t, tasks, "osinstall.ceph-libvirt")
	assertTaskDeps(t, tasks, "osinstall.ceph-libvirt", "provider.bastion", "infra-component.bastion", "osprepare.ceph-libvirt.bastion")
	if task.Limit != render.ManagedOSGroupName("ceph-libvirt") {
		t.Fatalf("managed OS limit = %q, want %q", task.Limit, render.ManagedOSGroupName("ceph-libvirt"))
	}
	if task.Forks != 3 {
		t.Fatalf("managed OS forks = %d, want 3", task.Forks)
	}
	if task.RedfishSlots != 3 {
		t.Fatalf("managed OS RedfishSlots = %d, want 3", task.RedfishSlots)
	}
	if !reflect.DeepEqual(task.ExtraVarPairs, []string{"bootwright_task_managed_os_group_name=ceph-libvirt"}) {
		t.Fatalf("managed OS extra vars = %v", task.ExtraVarPairs)
	}
	assertTaskMissing(t, tasks, "osinstall.ceph-libvirt.ceph-0")
	assertTaskMissing(t, tasks, "osinstall.ceph-libvirt.ceph-1")
	assertTaskMissing(t, tasks, "osinstall.ceph-libvirt.ceph-2")
	assertTaskDeps(t, tasks, "storageinfra.ceph-libvirt", "provider.bastion", "infra-component.bastion", "osinstall.ceph-libvirt")
	assertTaskDeps(t, tasks, "storage.ceph-libvirt", "provider.bastion", "infra-component.bastion", "storageinfra.ceph-libvirt")
}

func TestPlanApplyClustersOrdersKubeVirtChildInfraAfterHostReadiness(t *testing.T) {
	state := kubeVirtChildPlanningState(true)

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskDeps(t, tasks, "infra.child-ocp.child-master-0", "wait.metal-ocp", "addon.metal-ocp.openshift-virtualization")
	assertTaskResourceKeys(t, tasks, "infra.child-ocp.child-master-0", "kubevirt:metal-ocp:bootwright-child-ocp")
	assertTaskDeps(t, tasks, "infrafinalize.child-ocp.localhost", "wait.metal-ocp", "addon.metal-ocp.openshift-virtualization", "infra.child-ocp.child-master-0")
	assertTaskResourceKeys(t, tasks, "infrafinalize.child-ocp.localhost", "host:localhost:mutating")
	assertTaskResourceKeys(t, tasks, "boot.child-ocp", "kubevirt:metal-ocp:bootwright-child-ocp")
}

func TestPlanApplyAllOrdersKubeVirtManagedCephAfterHostReadiness(t *testing.T) {
	state := kubeVirtCephPlanningState(true)

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskDeps(t, tasks, "osinstall.ceph-vms", "wait.metal-ocp", "addon.metal-ocp.openshift-virtualization")
	assertTaskResourceKeys(t, tasks, "osinstall.ceph-vms", "kubevirt:metal-ocp:bootwright-child-ocp")
	assertTaskDeps(t, tasks, "storageinfra.ceph-vms", "osinstall.ceph-vms")
	assertTaskDeps(t, tasks, "storage.ceph-vms", "storageinfra.ceph-vms")
}

func TestPlanApplyAllRejectsKubeVirtManagedCephWithoutHostCapability(t *testing.T) {
	state := kubeVirtCephPlanningState(true)
	state.ClusterAddonBindings = nil

	_, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err == nil {
		t.Fatal("PlanApplyTasksChecked succeeded, want missing kubevirt capability error")
	}
	if !strings.Contains(err.Error(), `machine Machine/ceph-0 uses KubeVirt hostClusterRef "metal-ocp"`) {
		t.Fatalf("error = %v, want managed Ceph KubeVirt hostClusterRef context", err)
	}
}

func TestPlanApplyClustersSkipsUnselectedKubeVirtHostDependencies(t *testing.T) {
	state := kubeVirtChildPlanningState(false)

	tasks, err := PlanApplyTasksChecked(applyClustersTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskDeps(t, tasks, "iso.child-ocp")
	// A pre-existing (unselected) host cluster still gets a deps-stage virtctl
	// provision, but with no prerequisites — the host is assumed ready.
	assertTaskDeps(t, tasks, "virtctl.metal-ocp")
}

func TestPlanApplyProvisionsVirtctlPerHostBeforeBoot(t *testing.T) {
	state := kubeVirtChildPlanningState(true)

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	// One virtctl provision per distinct host cluster, gated behind that host's
	// readiness (install + the KubeVirt-providing addon) — same gate boot waits on.
	assertTaskDeps(t, tasks, "virtctl.metal-ocp", "wait.metal-ocp", "addon.metal-ocp.openshift-virtualization")
	// Every child boot waits for its host's virtctl provision.
	boot := assertTaskPresent(t, tasks, "boot.child-ocp")
	found := false
	for _, dep := range boot.Entry.Dependencies {
		if dep == "virtctl.metal-ocp" {
			found = true
		}
	}
	if !found {
		t.Fatalf("boot.child-ocp deps = %v, want to include virtctl.metal-ocp", boot.Entry.Dependencies)
	}
}

func TestPlanApplyBaseOnlyDoesNotProvisionVirtctl(t *testing.T) {
	// Pre-existing host (unselected): a base-only run has no addon/deps tasks, so
	// boot requires no host capabilities and nothing provisions virtctl.
	state := kubeVirtChildPlanningState(false)

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "base", PhaseNames: []string{ApplyPhaseBase}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	// Without deps in scope nothing provisions virtctl, so no provision task is
	// planned and boot must not wait on a task that does not exist. The preflight
	// keeps the hard virtctl binary check for this base-without-deps case instead.
	assertTaskMissing(t, tasks, "virtctl.metal-ocp")
	boot := assertTaskPresent(t, tasks, "boot.child-ocp")
	for _, dep := range boot.Entry.Dependencies {
		if dep == "virtctl.metal-ocp" {
			t.Fatalf("boot.child-ocp deps = %v, must not require an unplanned virtctl task", boot.Entry.Dependencies)
		}
	}
}

func loadWorkflowFixtureState(t *testing.T, name string) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "state", "desired", "testdata", "good", name)})
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return state
}

func writeWorkflowInstallerSecrets(t *testing.T, root string) string {
	t.Helper()
	home := filepath.Join(root, "home")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir ssh dir: %v", err)
	}
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(sshDir, "bootwright-ssh-key.pub"), []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForWorkflowTests\n"), 0o600); err != nil {
		t.Fatalf("write ssh public key: %v", err)
	}
	secretsDir := filepath.Join(root, "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir secrets dir: %v", err)
	}
	files := []struct {
		name string
		role secretstore.MaterialRole
		body string
	}{
		{name: "openshift-pull-secret", role: secretstore.MaterialPrimary, body: `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`},
		{name: "sno-libvirt-cluster-admin-ssh-key", role: secretstore.MaterialSSHPublic, body: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForWorkflowTests\n"},
		{name: "3-nodes-ocp-emul-baremetal-cluster-admin-ssh-key", role: secretstore.MaterialSSHPublic, body: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForWorkflowTests\n"},
		{name: "demo-cluster-admin-ssh-key", role: secretstore.MaterialSSHPublic, body: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForWorkflowTests\n"},
		{name: "proxy-credentials", role: secretstore.MaterialPrimary, body: "proxy:secret\n"},
	}
	store := secretstore.NewContextStore("test", secretsDir)
	for _, item := range files {
		if err := store.Write(secretstore.MaterialKey{Name: item.name, Role: item.role}, []byte(item.body)); err != nil {
			t.Fatalf("write %s/%s: %v", item.name, item.role, err)
		}
	}
	return secretsDir
}

func extensionPlanningState() v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{
			{Metadata: v1alpha1.Metadata{Name: "a"}, Spec: v1alpha1.ClusterAddonSpec{Type: v1alpha1.ClusterAddonTypeManifestSet}},
			{Metadata: v1alpha1.Metadata{Name: "b"}, Spec: v1alpha1.ClusterAddonSpec{Type: v1alpha1.ClusterAddonTypeManifestSet}},
		},
		ClusterAddonProfiles: []v1alpha1.ClusterAddonProfile{{
			Metadata: v1alpha1.Metadata{Name: "platform"},
			Spec: v1alpha1.ClusterAddonProfileSpec{
				AddonRefs: []v1alpha1.LocalObjectReference{{Name: "a"}},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef:       v1alpha1.LocalObjectReference{Name: "demo"},
				AddonProfileRefs: []v1alpha1.LocalObjectReference{{Name: "platform"}},
				Addons:           []v1alpha1.ClusterAddonBindingAddon{{AddonRef: v1alpha1.LocalObjectReference{Name: "b"}}},
			},
		}},
	}
}

func storageAttachmentPlanningState() v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "ceph-0"},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityCephNode},
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.10.10.10"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{
						AddressRef:    v1alpha1.LocalObjectReference{Name: "ssh"},
						KeyRef:        v1alpha1.SecretRef{Name: "ceph-node-ssh"},
						KnownHostsRef: v1alpha1.SecretRef{Name: "ceph-known-hosts"},
					},
				},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						Bootstrap: v1alpha1.StorageCephadmBootstrap{
							Host: "ceph-0",
						},
					},
					Topology: v1alpha1.StorageCephTopology{
						Hosts: []v1alpha1.StorageCephHost{{
							Hostname:   "ceph-0",
							MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
							Site:       "dc1",
							Roles:      []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleMGR, v1alpha1.StorageCephRoleOSD},
						}},
					},
				},
			},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "export"},
			Spec: v1alpha1.StorageExportSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
				Accepts:  dataFoundationAccepts(),
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "ceph-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				Addons:     []v1alpha1.ClusterAddonBindingAddon{dataFoundationBindingAddon("export")},
			},
		}},
	}
}

func externalStorageAttachmentPlanningState() v1alpha1.State {
	state := storageAttachmentPlanningState()
	state.StorageClusters = []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "shared-ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: v1alpha1.StorageClusterManagementExternal,
		},
	}}
	state.StorageExports = []v1alpha1.StorageExport{{
		Metadata: v1alpha1.Metadata{Name: "shared-ceph-export"},
		Spec: v1alpha1.StorageExportSpec{
			Type:              v1alpha1.StorageExportTypeDataFoundation,
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "shared-ceph"},
			ExternalDetails: &v1alpha1.StorageExportExternalDetailsSpec{
				FromSecretRef: v1alpha1.SecretRef{Name: "shared-ceph-external-details"},
			},
		},
	}}
	state.ClusterAddonBindings[0].Metadata.Name = "shared-ceph-binding"
	state.ClusterAddonBindings[0].Spec.Addons[0].Inputs[0].Values = dataFoundationValues("shared-ceph-export")
	return state
}

func dataFoundationAccepts() v1alpha1.ClusterAddonAccepts {
	return v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
		Name: "external-storage",
		Schema: v1alpha1.ClusterAddonInputSchema{
			Type:     v1alpha1.ClusterAddonInputSchemaTypeObject,
			Required: []string{"exportRef"},
			Properties: map[string]v1alpha1.ClusterAddonInputProperty{
				"exportRef": {RefKind: v1alpha1.KindStorageExport},
			},
		},
		Effects: []v1alpha1.ClusterAddonInputEffect{{
			Type:     v1alpha1.ClusterAddonInputEffectStorageExportAttachment,
			Provider: v1alpha1.ClusterAddonProvidesDataFoundation,
		}},
	}}}
}

func dataFoundationBindingAddon(export string) v1alpha1.ClusterAddonBindingAddon {
	return v1alpha1.ClusterAddonBindingAddon{
		AddonRef: v1alpha1.LocalObjectReference{Name: "odf"},
		Inputs: []v1alpha1.ClusterAddonBindingInput{{
			Name:   "external-storage",
			Values: dataFoundationValues(export),
		}},
	}
}

func dataFoundationValues(export string) map[string]any {
	return map[string]any{
		"exportRef": export,
	}
}

func kubeVirtChildPlanningState(includeParent bool) v1alpha1.State {
	clusters := []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "child-ocp"},
		Spec: v1alpha1.ContainerClusterSpec{Hosts: []v1alpha1.OCPHostSpec{{
			Hostname:   "master-0",
			MachineRef: v1alpha1.LocalObjectReference{Name: "child-master-0"},
		}}},
	}}
	if includeParent {
		clusters = append(clusters, v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: "metal-ocp"}})
	}
	return v1alpha1.State{
		ContainerClusters: clusters,
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "child-master-0"},
			Spec: v1alpha1.MachineSpec{
				Substrate: v1alpha1.MachineSubstrate{
					ProviderRef: v1alpha1.LocalObjectReference{Name: "child-kubevirt-provider"},
					ProfileRef:  v1alpha1.LocalObjectReference{Name: "sno"},
				},
				OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "child-kubevirt-provider"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerKubeVirt,
				KubeVirt: &v1alpha1.InfraProviderKubeVirt{
					HostClusterRef: &v1alpha1.LocalObjectReference{Name: "metal-ocp"},
					Namespace:      "bootwright-child-ocp",
					MachineProfiles: []v1alpha1.MachineProfile{{
						Name: "sno",
					}},
				},
			},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "openshift-virtualization"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesKubeVirt},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "virt"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "metal-ocp"},
				Addons:     []v1alpha1.ClusterAddonBindingAddon{{AddonRef: v1alpha1.LocalObjectReference{Name: "openshift-virtualization"}}},
			},
		}},
	}
}

func kubeVirtCephPlanningState(includeParent bool) v1alpha1.State {
	state := kubeVirtChildPlanningState(includeParent)
	state.ContainerClusters = nil
	if includeParent {
		state.ContainerClusters = []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "metal-ocp"}}}
	}
	state.Machines = []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "ceph-0"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityCephNode},
			Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: "child-kubevirt-provider"},
				ProfileRef:  v1alpha1.LocalObjectReference{Name: "sno"},
			},
			OS: v1alpha1.MachineOSSpec{
				Provided:          v1alpha1.BoolPtr(false),
				InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"},
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.20"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{
					AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
					KeyRef:     v1alpha1.SecretRef{Name: "ceph-ssh"},
				},
			},
		},
	}}
	state.StorageClusters = []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph-vms"},
		Spec: v1alpha1.StorageClusterSpec{
			Type: v1alpha1.StorageClusterTypeCeph,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{
					Hosts: []v1alpha1.StorageCephHost{{
						Hostname:   "ceph-0",
						MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
						Site:       "dc1",
						Roles:      []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleMGR, v1alpha1.StorageCephRoleOSD},
					}},
				},
			},
		},
	}}
	return state
}

func applyTaskIDs(tasks []ApplyTask) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.Entry.ID)
	}
	return out
}

func applyAllTarget() ApplyTarget {
	return ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseFabric, ApplyPhaseMachines, ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}}
}

func applyClustersTarget() ApplyTarget {
	return ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}}
}

func applyContainerClusterTarget() ApplyTarget {
	return ApplyTarget{Name: "container-cluster", PhaseNames: []string{ApplyPhaseDeps, ApplyPhaseBase}}
}

func assertTaskPresent(t *testing.T, tasks []ApplyTask, id string) ApplyTask {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID == id {
			return task
		}
	}
	t.Fatalf("task %s not found in %+v", id, applyTaskIDs(tasks))
	return ApplyTask{}
}

func assertTaskMissing(t *testing.T, tasks []ApplyTask, id string) {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID == id {
			t.Fatalf("task %s unexpectedly found in %+v", id, applyTaskIDs(tasks))
		}
	}
}

func assertTaskDeps(t *testing.T, tasks []ApplyTask, id string, want ...string) {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID != id {
			continue
		}
		if !reflect.DeepEqual(task.Entry.Dependencies, want) {
			t.Fatalf("%s deps = %v, want %v", id, task.Entry.Dependencies, want)
		}
		return
	}
	t.Fatalf("task %s not found in %+v", id, applyTaskIDs(tasks))
}

// assertTaskHasDeps asserts the named task's dependencies CONTAIN each wanted
// entry (subset, order-free) — for real-example graphs whose addon tasks also
// carry capability-ordering edges this test does not pin.
func assertTaskHasDeps(t *testing.T, tasks []ApplyTask, id string, want ...string) {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID != id {
			continue
		}
		have := map[string]bool{}
		for _, dep := range task.Entry.Dependencies {
			have[dep] = true
		}
		for _, dep := range want {
			if !have[dep] {
				t.Fatalf("%s deps = %v, missing %s", id, task.Entry.Dependencies, dep)
			}
		}
		return
	}
	t.Fatalf("task %s not found in %+v", id, applyTaskIDs(tasks))
}

// assertTaskDependsTransitively asserts the named task orders behind target
// through the dependency graph — directly or via intermediate tasks (an addon
// chain, a capability edge).
func assertTaskDependsTransitively(t *testing.T, tasks []ApplyTask, id, target string) {
	t.Helper()
	deps := map[string][]string{}
	for _, task := range tasks {
		deps[task.Entry.ID] = task.Entry.Dependencies
	}
	if _, ok := deps[id]; !ok {
		t.Fatalf("task %s not found in %+v", id, applyTaskIDs(tasks))
	}
	seen := map[string]bool{}
	queue := append([]string(nil), deps[id]...)
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next == target {
			return
		}
		if seen[next] {
			continue
		}
		seen[next] = true
		queue = append(queue, deps[next]...)
	}
	t.Fatalf("%s does not depend (even transitively) on %s; direct deps = %v", id, target, deps[id])
}

func assertTaskResourceKeys(t *testing.T, tasks []ApplyTask, id string, want ...string) {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID != id {
			continue
		}
		if !reflect.DeepEqual(task.Entry.ResourceKeys, want) {
			t.Fatalf("%s resource keys = %v, want %v", id, task.Entry.ResourceKeys, want)
		}
		return
	}
	t.Fatalf("task %s not found in %+v", id, applyTaskIDs(tasks))
}
