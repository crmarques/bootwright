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
	storageapply "github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
	"go.yaml.in/yaml/v3"
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

type storageResultRunner struct {
	fakeRunner
	t *testing.T
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

type externalDetailsAnsibleRunner struct {
	runCalled  bool
	lastSpec   ansible.RunSpec
	outputJSON string
	runErr     error
}

func (r *storageResultRunner) Run(ctx context.Context, spec ansible.RunSpec) error {
	if err := r.fakeRunner.Run(ctx, spec); err != nil {
		return err
	}
	resultPath := filepath.Join(spec.ArtifactsDir, "storage-result.json")
	result := map[string]any{
		"dataFoundation": map[string]datafoundation.ExternalSecrets{
			"demo": {
				AdminSecret:          "admin-key",
				FSID:                 "fsid-123",
				MonSecret:            "mon-key",
				HealthcheckerKey:     "healthchecker-key",
				RBDNodeKey:           "rbd-node-key",
				RBDProvisionerKey:    "rbd-provisioner-key",
				CephFSNodeKey:        "cephfs-node-key",
				CephFSProvisionerKey: "cephfs-provisioner-key",
			},
		},
	}
	data, err := json.Marshal(result)
	if err != nil {
		r.t.Fatalf("marshal result: %v", err)
	}
	if err := os.MkdirAll(spec.ArtifactsDir, 0o700); err != nil {
		r.t.Fatalf("mkdir storage artifacts: %v", err)
	}
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		r.t.Fatalf("write storage result: %v", err)
	}
	return nil
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

func (r *externalDetailsAnsibleRunner) Run(_ context.Context, spec ansible.RunSpec) error {
	r.runCalled = true
	r.lastSpec = spec
	if r.runErr != nil {
		return r.runErr
	}
	if err := os.MkdirAll(spec.ArtifactsDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(spec.ArtifactsDir, "external-cluster-details.json"), []byte(r.outputJSON), 0o600)
}

func (r *externalDetailsAnsibleRunner) Command(spec ansible.RunSpec) []string {
	executable := spec.Executable
	if executable == "" {
		executable = "ansible-playbook"
	}
	return []string{executable, "-i", spec.Inventory, spec.Playbook}
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
	hash, err := clusterInstallDesiredHash(state, "sno-libvirt", secretsDir)
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
	}, applyContainerClusterTarget(), "", PlanApplyTasks(applyContainerClusterTarget(), state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
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
	}, applyContainerClusterTarget(), "", PlanApplyTasks(applyContainerClusterTarget(), state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
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
			}, applyContainerClusterTarget(), "", PlanApplyTasks(applyContainerClusterTarget(), state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
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
	hash, err := clusterInstallDesiredHash(state, "sno-libvirt", secretsDir)
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
	}, applyContainerClusterTarget(), "", PlanApplyTasks(applyContainerClusterTarget(), state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
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
	}, applyContainerClusterTarget(), "", PlanApplyTasks(applyContainerClusterTarget(), state), ConcurrencyLimits{Parallelism: 1}, nil, func(stdout io.Writer, stderr io.Writer) ansible.Runner {
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
	if len(record.Nodes) != 1 {
		t.Fatalf("node records = %+v, want one node", record.Nodes)
	}
	node := record.Nodes["master-0"]
	if !node.ISOCreated || node.ISOCreatedAt == nil {
		t.Fatalf("node ISO safe point = %+v, want set", node)
	}
	if !node.BootRequested || node.BootRequestedAt == nil {
		t.Fatalf("node boot requested safe point = %+v, want set", node)
	}
	if !node.BootVerified || node.BootVerifiedAt == nil || !node.Booted || node.BootedAt == nil {
		t.Fatalf("node boot verified safe point = %+v, want set", node)
	}
	if !node.InstallWaitStarted || node.InstallWaitStartedAt == nil {
		t.Fatalf("node install wait safe point = %+v, want set", node)
	}
}

func TestClusterInstallDesiredHashChangesWhenProxyCredentialsChange(t *testing.T) {
	dir := t.TempDir()
	state := loadWorkflowFixtureState(t, "004-3nodes-emul-baremetal")
	secretsDir := writeWorkflowInstallerSecrets(t, dir)
	clusterName := "3-nodes-ocp-emul-baremetal"

	first, err := clusterInstallDesiredHash(state, clusterName, secretsDir)
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
	second, err := clusterInstallDesiredHash(state, clusterName, secretsDir)
	if err != nil {
		t.Fatalf("clusterInstallDesiredHash second: %v", err)
	}
	if first == second {
		t.Fatal("cluster install desired hash did not change after proxy credentials changed")
	}
}

func TestPlanApplyAddonsOrdersAddonTasks(t *testing.T) {
	state := extensionPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "addons", PhaseNames: []string{ApplyPhaseAddons}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	gotIDs := applyTaskIDs(tasks)
	wantIDs := []string{
		"addon.demo.a.apply",
		"addon.demo.a.wait",
		"addon.demo.b.apply",
		"addon.demo.b.wait",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("task IDs = %v, want %v", gotIDs, wantIDs)
	}
	assertTaskDeps(t, tasks, "addon.demo.a.apply")
	assertTaskDeps(t, tasks, "addon.demo.a.wait", "addon.demo.a.apply")
	assertTaskDeps(t, tasks, "addon.demo.b.apply", "addon.demo.a.wait")
	assertTaskDeps(t, tasks, "addon.demo.b.wait", "addon.demo.b.apply")
}

func TestPlanApplyAllRunsAddonsAfterInstallWait(t *testing.T) {
	state := extensionPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskDeps(t, tasks, "addon.demo.a.apply", "wait.demo")
	assertTaskDeps(t, tasks, "addon.demo.a.wait", "addon.demo.a.apply")
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
		"addon.demo.a.apply",
		"addon.demo.a.wait",
		"addon.demo.b.apply",
		"addon.demo.b.wait",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("task IDs = %v, want %v", gotIDs, wantIDs)
	}
	assertTaskDeps(t, tasks, "addon.demo.a.apply", "wait.demo")
	assertTaskDeps(t, tasks, "addon.demo.a.wait", "addon.demo.a.apply")
	assertTaskDeps(t, tasks, "addon.demo.b.apply", "addon.demo.a.wait")
	assertTaskDeps(t, tasks, "addon.demo.b.wait", "addon.demo.b.apply")
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
	assertTaskDeps(t, tasks, "addon.demo.odf.apply", "wait.demo")
	assertTaskDeps(t, tasks, "addon.demo.odf.wait", "addon.demo.odf.apply")
	assertTaskDeps(t, tasks, "storageattachment.demo.odf.external-storage.apply", "wait.demo", "storage.ceph", "addon.demo.odf.wait")
}

func TestPlanApplyAllOrdersStorageAttachmentsAfterStorageInstallAndDataFoundation(t *testing.T) {
	state := storageAttachmentPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskDeps(t, tasks, "storageattachment.demo.odf.external-storage.apply", "wait.demo", "storage.ceph", "addon.demo.odf.wait")
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
	assertTaskDeps(t, tasks, "storageattachment.demo.odf.external-storage.apply", "wait.demo", "addon.demo.odf.wait")
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

func TestExternalStorageExamplesPlanDataFoundationAttachmentsWithoutCephTask(t *testing.T) {
	cases := []struct {
		example        string
		bindings       map[string]string
		addon          string
		storageTask    string
		wantStorageJob bool
	}{
		{
			example:     "baremetal-redfish-imported-ceph-odf",
			bindings:    map[string]string{"metal-ocp": "metal-ocp-odf"},
			addon:       "openshift-data-foundation",
			storageTask: "storage.imported-ceph",
		},
		{
			example:        "baremetal-redfish-multidc-virtualized-odf-ceph",
			bindings:       map[string]string{"dc1-metal-ocp": "dc1-metal-ocp-addons", "dc2-metal-ocp": "dc2-metal-ocp-addons", "dc1-child-ocp": "dc1-child-ocp-addons", "dc2-child-ocp": "dc2-child-ocp-addons"},
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
			for cluster := range tc.bindings {
				deps := []string{"wait." + cluster, "addon." + cluster + "." + tc.addon + ".wait"}
				if tc.wantStorageJob {
					deps = []string{"wait." + cluster, tc.storageTask, "addon." + cluster + "." + tc.addon + ".wait"}
				}
				assertTaskDeps(t, tasks, "storageattachment."+cluster+"."+tc.addon+".external-storage.apply", deps...)
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

func TestStorageTaskRunsThroughAnsibleAndPersistsResult(t *testing.T) {
	state := storageAttachmentPlanningState()
	state.StorageExports[0].Spec.DataFoundation = &v1alpha1.StorageExportDataFoundationSpec{
		RBDPoolRef:    v1alpha1.LocalObjectReference{Name: "rbd"},
		FilesystemRef: v1alpha1.LocalObjectReference{Name: "cephfs"},
	}
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
	runner := &storageResultRunner{t: t}
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
	if _, err := os.Stat(filepath.Join(runner.lastSpec.ArtifactsDir, "storage-result.json")); !os.IsNotExist(err) {
		t.Fatalf("storage result was not removed, stat err=%v", err)
	}
	detailsJSON, found, err := storageapply.LoadDataFoundationAttachmentDetails(filepath.Join(dir, "clusters"), "demo", "odf", "external-storage")
	if err != nil || !found {
		t.Fatalf("LoadDataFoundationAttachmentDetails found=%v err=%v", found, err)
	}
	if !strings.Contains(detailsJSON, "rbd-node-key") {
		t.Fatalf("external details missing runtime result: %s", detailsJSON)
	}
}

func TestWriteStorageAttachmentExternalDetailsUsesRuntimeCredentials(t *testing.T) {
	state := storageAttachmentPlanningState()
	state.StorageExports[0].Spec.DataFoundation = &v1alpha1.StorageExportDataFoundationSpec{
		RBDPoolRef:    v1alpha1.LocalObjectReference{Name: "rbd"},
		FilesystemRef: v1alpha1.LocalObjectReference{Name: "cephfs"},
	}
	clustersDir := t.TempDir()
	runtimeDetailsJSON := `[{"name":"rook-csi-rbd-node","kind":"Secret","data":{"userKey":"rbd-node-key"}}]`
	if err := storageapply.SaveDataFoundationAttachmentDetails(clustersDir, "demo", "odf", "external-storage", runtimeDetailsJSON); err != nil {
		t.Fatalf("SaveDataFoundationAttachmentDetails: %v", err)
	}
	path := filepath.Join(t.TempDir(), "rook-ceph-external-cluster-details.yaml")
	err := writeStorageAttachmentExternalDetails(context.Background(), path, state, StorageAttachmentPlan{
		Cluster: "demo",
		Binding: state.ClusterAddonBindings[0],
		Addon:   state.ClusterAddonBindings[0].Spec.Addons[0],
		Input:   state.ClusterAddonBindings[0].Spec.Addons[0].Inputs[0],
	}, storageAttachmentExternalDetailsOptions{ClustersDir: clustersDir, SecretsDir: t.TempDir()})
	if err != nil {
		t.Fatalf("writeStorageAttachmentExternalDetails: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	detailsJSON := manifest["stringData"].(map[string]any)["external_cluster_details"].(string)
	if strings.Contains(detailsJSON, datafoundation.GeneratedAtApplyPlaceholder) {
		t.Fatalf("external_cluster_details still contains placeholder: %s", detailsJSON)
	}
	if !strings.Contains(detailsJSON, "rbd-node-key") {
		t.Fatalf("external_cluster_details missing runtime key: %s", detailsJSON)
	}
}

func TestWriteStorageAttachmentExternalDetailsUsesImportedSecret(t *testing.T) {
	state := externalStorageAttachmentPlanningState()
	secretPath := filepath.Join(t.TempDir(), "external-details.json")
	secretJSON := `[{"name":"rook-ceph-mon","kind":"Secret","data":{"fsid":"external-fsid"}}]`
	if err := os.WriteFile(secretPath, []byte(secretJSON), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	state.Environments = []v1alpha1.Environment{{
		SourcePath: filepath.Join(t.TempDir(), "environment.yaml"),
		Spec: v1alpha1.EnvironmentSpec{Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
			"shared-ceph-external-details": {File: secretPath},
		}},
	}}
	path := filepath.Join(t.TempDir(), "rook-ceph-external-cluster-details.yaml")
	err := writeStorageAttachmentExternalDetails(context.Background(), path, state, StorageAttachmentPlan{
		Cluster: "demo",
		Binding: state.ClusterAddonBindings[0],
		Addon:   state.ClusterAddonBindings[0].Spec.Addons[0],
		Input:   state.ClusterAddonBindings[0].Spec.Addons[0].Inputs[0],
	}, storageAttachmentExternalDetailsOptions{ClustersDir: t.TempDir(), SecretsDir: t.TempDir()})
	if err != nil {
		t.Fatalf("writeStorageAttachmentExternalDetails: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	detailsJSON := manifest["stringData"].(map[string]any)["external_cluster_details"].(string)
	if detailsJSON != secretJSON {
		t.Fatalf("external_cluster_details = %s, want %s", detailsJSON, secretJSON)
	}
}

func TestWriteStorageAttachmentExternalDetailsUsesSSHExecution(t *testing.T) {
	state := storageAttachmentPlanningState()
	state.Environments = []v1alpha1.Environment{{
		Spec: v1alpha1.EnvironmentSpec{Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
			"ceph-known-hosts": {},
			"ceph-node-ssh":    {},
		}},
	}}
	state.StorageExports[0].Spec.Type = v1alpha1.StorageExportTypeDataFoundation
	state.StorageExports[0].Spec.ExternalDetails = &v1alpha1.StorageExportExternalDetailsSpec{
		SSHExecution: &v1alpha1.StorageExportExternalDetailsSSHExecution{
			Timeout: "30s",
			Exporter: v1alpha1.StorageExportExternalDetailsExporter{
				Source: v1alpha1.StorageExportExternalDetailsExporterBoundDataFoundationAddon,
			},
			Config: v1alpha1.StorageExportExternalDetailsExporterConfig{
				RBDDataPoolName:          "rbdpool",
				MonitoringEndpoint:       []string{"10.10.10.11", "10.10.10.12"},
				MonitoringEndpointPort:   9283,
				ClusterName:              "ceph",
				RestrictedAuthPermission: true,
			},
		},
	}
	state.Machines[0].Spec.Access.SSH.User = "operator"

	secretsDir := t.TempDir()
	exportedJSON := `[{"name":"rook-ceph-mon","kind":"Secret","data":{"fsid":"ssh-fsid"}}]`
	ansibleRunner := &externalDetailsAnsibleRunner{outputJSON: exportedJSON}

	path := filepath.Join(t.TempDir(), "rook-ceph-external-cluster-details.yaml")
	err := writeStorageAttachmentExternalDetails(context.Background(), path, state, StorageAttachmentPlan{
		Cluster: "demo",
		Binding: state.ClusterAddonBindings[0],
		Addon:   state.ClusterAddonBindings[0].Spec.Addons[0],
		Input:   state.ClusterAddonBindings[0].Spec.Addons[0].Inputs[0],
	}, storageAttachmentExternalDetailsOptions{
		ClustersDir:        t.TempDir(),
		SecretsDir:         secretsDir,
		TaskRoot:           t.TempDir(),
		BundleDir:          "/bundle",
		AskBecomePass:      true,
		BecomePasswordFile: "/tmp/bootwright-become",
		UseControllingTTY:  true,
		Runner:             ansibleRunner,
	})
	if err != nil {
		t.Fatalf("writeStorageAttachmentExternalDetails: %v", err)
	}

	if !ansibleRunner.runCalled {
		t.Fatal("external details Ansible runner was not invoked")
	}
	spec := ansibleRunner.lastSpec
	if spec.Limit != "external_details_0" {
		t.Fatalf("Ansible limit = %q, want external_details_0", spec.Limit)
	}
	if !spec.AskBecomePass || spec.BecomePasswordFile != "/tmp/bootwright-become" || !spec.UseControllingTTY {
		t.Fatalf("become options were not propagated: %+v", spec)
	}
	inventoryData, err := os.ReadFile(spec.Inventory)
	if err != nil {
		t.Fatalf("read generated inventory: %v", err)
	}
	var inventory map[string]any
	if err := yaml.Unmarshal(inventoryData, &inventory); err != nil {
		t.Fatalf("decode generated inventory: %v", err)
	}
	host := inventory["all"].(map[string]any)["hosts"].(map[string]any)["external_details_0"].(map[string]any)
	if got := host["ansible_host"]; got != "10.10.10.10" {
		t.Fatalf("ansible_host = %v, want 10.10.10.10", got)
	}
	if got := host["ansible_user"]; got != "operator" {
		t.Fatalf("ansible_user = %v, want operator", got)
	}
	if got := host["ansible_ssh_private_key_file"]; got != filepath.Join(secretsDir, "ceph-node-ssh") {
		t.Fatalf("key path = %v", got)
	}
	commonArgs, _ := host["ansible_ssh_common_args"].(string)
	if !strings.Contains(commonArgs, "StrictHostKeyChecking=yes") || !strings.Contains(commonArgs, "UserKnownHostsFile="+filepath.Join(secretsDir, "ceph-known-hosts")) {
		t.Fatalf("ansible_ssh_common_args = %q, want strict host key checking with Machine knownHostsRef", commonArgs)
	}
	if strings.Contains(commonArgs, "/dev/null") {
		t.Fatalf("ansible_ssh_common_args must not discard known hosts: %q", commonArgs)
	}
	playbookData, err := os.ReadFile(spec.Playbook)
	if err != nil {
		t.Fatalf("read generated playbook: %v", err)
	}
	playbookText := string(playbookData)
	for _, want := range []string{
		"become: true",
		"python3",
		"ceph-external-cluster-details-exporter.py",
		"--k8s-cluster-name",
		"demo",
		"--restricted-auth-permission",
	} {
		if !strings.Contains(playbookText, want) {
			t.Fatalf("playbook missing %q:\n%s", want, playbookText)
		}
	}
	if strings.Contains(playbookText, "become_user") || strings.Contains(playbookText, "sudo") {
		t.Fatalf("playbook must rely on Ansible become without explicit sudo/user switching:\n%s", playbookText)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	detailsJSON := manifest["stringData"].(map[string]any)["external_cluster_details"].(string)
	if detailsJSON != exportedJSON {
		t.Fatalf("external_cluster_details = %s, want %s", detailsJSON, exportedJSON)
	}
}

func TestStorageExportSSHExternalDetailsTargetsUseMachineRefs(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
				"ceph-admin-ssh":         {},
				"ceph-admin-known-hosts": {},
			}},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "ceph-admin-01"},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityCephAdmin},
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "ceph-admin.example.test"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}, User: "ceph", KeyRef: v1alpha1.SecretRef{Name: "ceph-admin-ssh"}, KnownHostsRef: v1alpha1.SecretRef{Name: "ceph-admin-known-hosts"}},
				},
			},
		}},
	}
	secretsDir := t.TempDir()
	ssh := &v1alpha1.StorageExportExternalDetailsSSHExecution{
		MachineRefs: []v1alpha1.LocalObjectReference{{Name: "ceph-admin-01"}},
		Config:      v1alpha1.StorageExportExternalDetailsExporterConfig{RBDDataPoolName: "rbdpool"},
	}

	targets, err := storageExportSSHExternalDetailsTargets(state, v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "shared-ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: v1alpha1.StorageClusterManagementExternal,
		},
	}, secretsDir, "", ssh)
	if err != nil {
		t.Fatalf("storageExportSSHExternalDetailsTargets: %v", err)
	}
	want := []externalDetailsSSHTarget{{
		label:          "Machine/ceph-admin-01",
		inventoryName:  "external_details_0",
		address:        "ceph-admin.example.test",
		user:           "ceph",
		keyPath:        filepath.Join(secretsDir, "ceph-admin-ssh"),
		knownHostsPath: filepath.Join(secretsDir, "ceph-admin-known-hosts"),
	}}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %#v, want %#v", targets, want)
	}
	root := t.TempDir()
	if err := writeStorageExportSSHAnsibleFiles(filepath.Join(root, "inventory.yaml"), filepath.Join(root, "vars.yaml"), filepath.Join(root, "playbook.yaml"), targets[0], filepath.Join(root, "details.json"), datafoundation.ExternalDetailsExporterArgs(ssh.Config, "demo")); err != nil {
		t.Fatalf("writeStorageExportSSHAnsibleFiles: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "inventory.yaml"))
	if err != nil {
		t.Fatalf("read generated inventory: %v", err)
	}
	var inventory map[string]any
	if err := yaml.Unmarshal(data, &inventory); err != nil {
		t.Fatalf("decode inventory: %v", err)
	}
	host := inventory["all"].(map[string]any)["hosts"].(map[string]any)["external_details_0"].(map[string]any)
	if got := host["ansible_user"]; got != "ceph" {
		t.Fatalf("machine ref ansible_user = %v, want ceph", got)
	}
	commonArgs, _ := host["ansible_ssh_common_args"].(string)
	if !strings.Contains(commonArgs, "StrictHostKeyChecking=yes") || !strings.Contains(commonArgs, "UserKnownHostsFile="+filepath.Join(secretsDir, "ceph-admin-known-hosts")) {
		t.Fatalf("machine ref ansible_ssh_common_args = %q, want strict host key checking with Machine knownHostsRef", commonArgs)
	}
	if strings.Contains(commonArgs, "/dev/null") {
		t.Fatalf("machine ref ansible_ssh_common_args must not discard known hosts: %q", commonArgs)
	}
}

func TestStorageExportSSHExternalDetailsTargetsUseContextTrustWhenKnownHostsRefOmitted(t *testing.T) {
	state := storageAttachmentPlanningState()
	state.Machines[0].Spec.Access.SSH.KnownHostsRef = v1alpha1.SecretRef{}
	ssh := &v1alpha1.StorageExportExternalDetailsSSHExecution{
		Config: v1alpha1.StorageExportExternalDetailsExporterConfig{RBDDataPoolName: "rbdpool"},
	}
	secretsDir := filepath.Join(t.TempDir(), "runtime", "secrets")
	trustSecretsDir := filepath.Join(t.TempDir(), "context", "secrets")

	targets, err := storageExportSSHExternalDetailsTargets(state, state.StorageClusters[0], secretsDir, trustSecretsDir, ssh)
	if err != nil {
		t.Fatalf("storageExportSSHExternalDetailsTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets got %d, want 1", len(targets))
	}
	if got := targets[0].keyPath; got != filepath.Join(secretsDir, "ceph-node-ssh") {
		t.Fatalf("key path = %s, want runtime secret path", got)
	}
	wantKnownHosts := filepath.Join(filepath.Dir(trustSecretsDir), "trust", "ssh", "known_hosts")
	if got := targets[0].knownHostsPath; got != wantKnownHosts {
		t.Fatalf("known hosts path = %s, want %s", got, wantKnownHosts)
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

	assertTaskDeps(t, tasks, "infra.child-ocp.child-master-0", "wait.metal-ocp", "addon.metal-ocp.openshift-virtualization.wait")
	assertTaskResourceKeys(t, tasks, "infra.child-ocp.child-master-0", "kubevirt:metal-ocp:bootwright-child-ocp")
	assertTaskDeps(t, tasks, "infrafinalize.child-ocp.localhost", "wait.metal-ocp", "addon.metal-ocp.openshift-virtualization.wait", "infra.child-ocp.child-master-0")
	assertTaskResourceKeys(t, tasks, "infrafinalize.child-ocp.localhost", "host:localhost:mutating")
	assertTaskResourceKeys(t, tasks, "boot.child-ocp", "kubevirt:metal-ocp:bootwright-child-ocp")
}

func TestPlanApplyAllOrdersKubeVirtManagedCephAfterHostReadiness(t *testing.T) {
	state := kubeVirtCephPlanningState(true)

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskDeps(t, tasks, "osinstall.ceph-vms", "wait.metal-ocp", "addon.metal-ocp.openshift-virtualization.wait")
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
