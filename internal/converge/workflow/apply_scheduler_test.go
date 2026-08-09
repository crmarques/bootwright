package workflow

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/ansible"
)

type gatedApplyRunner struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *gatedApplyRunner) Run(ctx context.Context, _ ansible.RunSpec) error {
	r.once.Do(func() { close(r.entered) })
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *gatedApplyRunner) Command(ansible.RunSpec) []string {
	return []string{"ansible-playbook"}
}

type gatedPlaybookRunner struct {
	mu      sync.Mutex
	started []string
	entered map[string]chan struct{}
	release map[string]chan struct{}
}

func newGatedPlaybookRunner(playbooks ...string) *gatedPlaybookRunner {
	runner := &gatedPlaybookRunner{entered: map[string]chan struct{}{}, release: map[string]chan struct{}{}}
	for _, playbook := range playbooks {
		runner.entered[playbook] = make(chan struct{})
		runner.release[playbook] = make(chan struct{})
	}
	return runner
}

func (r *gatedPlaybookRunner) Run(ctx context.Context, spec ansible.RunSpec) error {
	r.mu.Lock()
	r.started = append(r.started, spec.Playbook)
	r.mu.Unlock()
	if entered, ok := r.entered[spec.Playbook]; ok {
		close(entered)
	}
	release, ok := r.release[spec.Playbook]
	if !ok {
		return nil
	}
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *gatedPlaybookRunner) Command(ansible.RunSpec) []string {
	return []string{"ansible-playbook"}
}

func (r *gatedPlaybookRunner) startedPlaybooks() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.started...)
}

func (r *gatedPlaybookRunner) awaitStart(t *testing.T, playbook string) {
	t.Helper()
	select {
	case <-r.entered[playbook]:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never started; started playbooks = %v", playbook, r.startedPlaybooks())
	}
}

func (r *gatedPlaybookRunner) releasePlaybook(playbook string) {
	if release, ok := r.release[playbook]; ok {
		close(release)
	}
}

func schedulerRunOptions(dir string) RunOptions {
	return RunOptions{
		State:              minimalState(),
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}
}

func TestRunApplyTaskGraphPersistsRunningWhileTaskIsInFlight(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	state := minimalState()
	runner := &gatedApplyRunner{entered: make(chan struct{}), release: make(chan struct{})}
	task := ApplyTask{
		Entry:    TaskLedgerEntry{ID: "boot.demo.node-0", Kind: ApplyTaskKindNodeBoot, Label: "boot node-0", Status: TaskStatusPending},
		Playbook: applyBootMachinePlaybook,
		State:    state,
	}
	done := make(chan error, 1)
	go func() {
		_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, schedulerRunOptions(dir),
			ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseBase}}, "", []ApplyTask{task},
			ConcurrencyLimits{Parallelism: 1}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
		done <- err
	}()
	select {
	case <-runner.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("task never started")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		ledger, found, err := LoadRunLedger(runsDir)
		if err != nil {
			t.Fatalf("LoadRunLedger: %v", err)
		}
		if found {
			if entry, ok := ledger.Task(task.Entry.ID); ok && entry.Status == TaskStatusRunning {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("a task that already forked ansible against real hardware was still not persisted as running; after a crash it would read as pending and the machine would look untouched")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(runner.release)
	if err := <-done; err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
}

func TestRunApplyTaskGraphArchivesTheSameBytesItSaves(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	state := minimalState()
	runner := &recordingApplyRunner{}
	task := ApplyTask{
		Entry:    TaskLedgerEntry{ID: "provider.service-host", Kind: ApplyTaskKindProvider, Label: "provider services", Status: TaskStatusPending},
		Playbook: applyProviderPlaybook,
		State:    state,
	}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, schedulerRunOptions(dir),
		ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseFabric}}, "", []ApplyTask{task},
		ConcurrencyLimits{Parallelism: 1}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	current, err := os.ReadFile(LedgerPath(runsDir))
	if err != nil {
		t.Fatalf("read current ledger: %v", err)
	}
	archived, err := os.ReadFile(ArchivedRunLedgerPath(runsDir, ledger.RunID))
	if err != nil {
		t.Fatalf("read archived ledger: %v", err)
	}
	if !bytes.Equal(current, archived) {
		t.Fatalf("the archived run ledger must be the same document the finish step persisted:\ncurrent=%s\narchived=%s", current, archived)
	}
}

func TestRunApplyTaskGraphRecordsReadyAtAndBlockedOnForHostSlotContention(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &recordingApplyRunner{delay: 25 * time.Millisecond}
	tasks := []ApplyTask{}
	for _, id := range []string{"machine-a", "machine-b"} {
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
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseMachines}}, "", tasks, ConcurrencyLimits{Parallelism: 2, ParallelismPerHost: 1}, nil, func(io.Writer, io.Writer) ansible.Runner {
		return runner
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}

	blocked := ""
	for _, task := range ledger.Tasks {
		if task.ReadyAt == nil {
			t.Fatalf("task %s recorded no ReadyAt; dependency-ready time must be attributed", task.ID)
		}
		if task.StartedAt == nil || task.StartedAt.Before(*task.ReadyAt) {
			t.Fatalf("task %s StartedAt=%v must not precede ReadyAt=%v", task.ID, task.StartedAt, task.ReadyAt)
		}
		if len(task.BlockedOn) > 0 {
			blocked = task.ID
			if task.BlockedOn[0] != "host slot host:bastion:machine" {
				t.Fatalf("task %s BlockedOn = %v, want the host slot key that withheld dispatch", task.ID, task.BlockedOn)
			}
			if taskBlockedWait(task) <= 0 {
				t.Fatalf("task %s waited on a host slot but recorded no blocked time", task.ID)
			}
		}
	}
	if blocked == "" {
		t.Fatal("a single host slot serialized two tasks, so one of them must record what blocked it")
	}
}

func TestRunApplyTaskGraphAttributesTheGlobalTaskBudget(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &recordingApplyRunner{delay: 10 * time.Millisecond}
	tasks := []ApplyTask{}
	for _, id := range []string{"task-a", "task-b", "task-c"} {
		tasks = append(tasks, ApplyTask{
			Entry: TaskLedgerEntry{
				ID:     id,
				Kind:   ApplyTaskKindClusterInstall,
				Label:  id,
				Status: TaskStatusPending,
			},
			Playbook: id,
			State:    state,
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
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseMachines}}, "", tasks, ConcurrencyLimits{Parallelism: 1}, nil, func(io.Writer, io.Writer) ansible.Runner {
		return runner
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if runner.maxActive > 1 {
		t.Fatalf("a global limit of 1 ran %d tasks at once", runner.maxActive)
	}
	budgeted := 0
	for _, task := range ledger.Tasks {
		for _, blocker := range task.BlockedOn {
			if blocker == applyTaskBudgetBlocker {
				budgeted++
			}
		}
	}
	if budgeted == 0 {
		t.Fatal("a binding global task budget must be attributed on the tasks it held back, not left invisible")
	}
}

func TestTaskDispatchBlockerReportsTheFirstUnavailableBudget(t *testing.T) {
	task := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:            "t",
			ResourceKeys:  []string{"libvirt:host-a"},
			HostSlotKey:   "host:bastion:machine",
			HostSlotCount: 1,
		},
		RedfishSlots: 2,
	}
	if got := taskDispatchBlocker(task, 2, 2, map[string]int{}, map[string]int{}, 1, map[string]int{}, 1, nil); got != "redfish budget" {
		t.Fatalf("blocker with an exhausted Redfish budget = %q, want %q", got, "redfish budget")
	}
	if got := taskDispatchBlocker(task, 1, 2, map[string]int{}, map[string]int{}, 1, map[string]int{}, 1, nil); got != "" {
		t.Fatalf("a task whose full Redfish demand does not fit must still dispatch on the free slots, got %q", got)
	}
	if got := taskDispatchBlocker(task, 0, 2, map[string]int{"libvirt:host-a": 1}, map[string]int{}, 1, map[string]int{}, 1, nil); got != "resource libvirt:host-a" {
		t.Fatalf("blocker with a held resource key = %q", got)
	}
	if got := taskDispatchBlocker(task, 0, 2, map[string]int{}, map[string]int{"host:bastion:machine": 1}, 1, map[string]int{}, 1, nil); got != "host slot host:bastion:machine" {
		t.Fatalf("blocker with a full host slot = %q", got)
	}
	if got := taskDispatchBlocker(task, 0, 2, map[string]int{}, map[string]int{}, 1, map[string]int{}, 1, nil); got != "" {
		t.Fatalf("blocker for a dispatchable task = %q, want empty", got)
	}
}

func TestTaskClusterInstallAdmissionCountsClustersNotTasks(t *testing.T) {
	install := ApplyTask{Entry: TaskLedgerEntry{ID: "boot.ocp-b.node-0", Kind: ApplyTaskKindNodeBoot, Cluster: "ocp-b"}}
	other := ApplyTask{Entry: TaskLedgerEntry{ID: "addon.ocp-b.logging", Kind: ApplyTaskKindClusterAddon, Cluster: "ocp-b"}}
	if got := taskDispatchBlocker(install, 0, 1, map[string]int{}, map[string]int{}, 1, map[string]int{"ocp-a": 1}, 1, nil); got != ApplyClusterInstallBlocker {
		t.Fatalf("blocker for a second installing cluster = %q, want %q", got, ApplyClusterInstallBlocker)
	}
	if got := taskDispatchBlocker(other, 0, 1, map[string]int{}, map[string]int{}, 1, map[string]int{"ocp-a": 1}, 1, nil); got != "" {
		t.Fatalf("a non-install task must not wait on the cluster install budget, got %q", got)
	}
	if got := taskDispatchBlocker(install, 0, 1, map[string]int{}, map[string]int{}, 1, map[string]int{"ocp-b": 1}, 1, nil); got != "" {
		t.Fatalf("another install task of the already-admitted cluster must dispatch, got %q", got)
	}
	if got := taskDispatchBlocker(install, 0, 1, map[string]int{}, map[string]int{}, 1, map[string]int{"ocp-a": 1}, 2, nil); got != "" {
		t.Fatalf("a raised cluster install limit must admit a second cluster, got %q", got)
	}
	if got := taskDispatchBlocker(install, 0, 1, map[string]int{}, map[string]int{}, 1, map[string]int{}, 1, map[string]bool{"ocp-c": true}); got != ApplyClusterInstallBlocker {
		t.Fatalf("a free slot must not go to a cluster outside the admission set, got %q", got)
	}
	if got := taskDispatchBlocker(install, 0, 1, map[string]int{}, map[string]int{}, 1, map[string]int{"ocp-b": 1}, 1, map[string]bool{"ocp-c": true}); got != "" {
		t.Fatalf("a cluster already holding the slot must keep dispatching regardless of admission, got %q", got)
	}
}

func TestRunApplyTaskGraphInstallsOneClusterAtATime(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &recordingApplyRunner{delay: 25 * time.Millisecond}
	tasks := []ApplyTask{}
	for _, cluster := range []string{"ocp-a", "ocp-b"} {
		tasks = append(tasks, ApplyTask{
			Entry: TaskLedgerEntry{
				ID:          "iso." + cluster,
				Kind:        ApplyTaskKindClusterISO,
				Label:       "iso " + cluster,
				Cluster:     cluster,
				ClusterKind: ApplyClusterKindContainer,
				Status:      TaskStatusPending,
			},
			Playbook: "iso." + cluster,
			State:    state,
		})
	}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), schedulerRunOptions(dir),
		ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseBase}}, "", tasks,
		ConcurrencyLimits{Parallelism: 2}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	calls, maxActive := runner.snapshot()
	if len(calls) != 2 {
		t.Fatalf("runner calls = %v, want both cluster install tasks", calls)
	}
	if maxActive != 1 {
		t.Fatalf("max concurrent cluster installs = %d; two clusters pulling one release payload at once is what starved the proxy", maxActive)
	}
	blocked := ""
	for _, task := range ledger.Tasks {
		for _, blocker := range task.BlockedOn {
			if blocker == ApplyClusterInstallBlocker {
				blocked = task.ID
			}
		}
	}
	if blocked == "" {
		t.Fatal("the cluster install budget held a task back, so it must be attributed on that task")
	}
}

func TestRunApplyTaskGraphHoldsTheClusterInstallSlotForTheWholeChain(t *testing.T) {
	t.Setenv(ParallelismClustersEnvVar, "")
	dir := t.TempDir()
	state := minimalState()
	runner := newGatedPlaybookRunner("iso.ocp-a", "boot.ocp-a", "iso.ocp-b")
	tasks := []ApplyTask{
		{
			Entry:    TaskLedgerEntry{ID: "iso.ocp-a", Kind: ApplyTaskKindClusterISO, Label: "iso ocp-a", Cluster: "ocp-a", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.ocp-a",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "iso.ocp-b", Kind: ApplyTaskKindClusterISO, Label: "iso ocp-b", Cluster: "ocp-b", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.ocp-b",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "boot.ocp-a", Kind: ApplyTaskKindNodeBoot, Label: "boot ocp-a nodes", Cluster: "ocp-a", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"iso.ocp-a"}, Status: TaskStatusPending},
			Playbook: "boot.ocp-a",
			State:    state,
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), schedulerRunOptions(dir),
			ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseBase}}, "", tasks,
			ConcurrencyLimits{}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
		done <- err
	}()
	runner.awaitStart(t, "iso.ocp-a")
	runner.releasePlaybook("iso.ocp-a")
	runner.awaitStart(t, "boot.ocp-a")
	time.Sleep(100 * time.Millisecond)
	for _, playbook := range runner.startedPlaybooks() {
		if playbook == "iso.ocp-b" {
			t.Fatal("the second cluster started its install chain in the gap between the first cluster's ISO and node boot; both clusters then pull the release payload at once, which is what the cluster install limit exists to prevent")
		}
	}
	runner.releasePlaybook("boot.ocp-a")
	runner.awaitStart(t, "iso.ocp-b")
	runner.releasePlaybook("iso.ocp-b")
	if err := <-done; err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	got := runner.startedPlaybooks()
	want := []string{"iso.ocp-a", "boot.ocp-a", "iso.ocp-b"}
	if len(got) != len(want) {
		t.Fatalf("started playbooks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("started playbooks = %v, want the second cluster to wait for the first cluster's last install task: %v", got, want)
		}
	}
}

func TestRunApplyTaskGraphHoldsTheClusterInstallSlotWhileMachinesProvision(t *testing.T) {
	t.Setenv(ParallelismClustersEnvVar, "")
	dir := t.TempDir()
	state := minimalState()
	runner := newGatedPlaybookRunner("iso.ocp-a", "infra.ocp-a", "boot.ocp-a", "iso.ocp-b")
	tasks := []ApplyTask{
		{
			Entry:    TaskLedgerEntry{ID: "iso.ocp-a", Kind: ApplyTaskKindClusterISO, Label: "iso ocp-a", Cluster: "ocp-a", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.ocp-a",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "infra.ocp-a.machine-0", Kind: ApplyTaskKindClusterInstall, Label: "provision machine machine-0", Cluster: "ocp-a", ClusterKind: ApplyClusterKindContainer, Node: "machine-0", Host: "hub-bm", Status: TaskStatusPending},
			Playbook: "infra.ocp-a",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "iso.ocp-b", Kind: ApplyTaskKindClusterISO, Label: "iso ocp-b", Cluster: "ocp-b", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.ocp-b",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "boot.ocp-a", Kind: ApplyTaskKindNodeBoot, Label: "boot ocp-a nodes", Cluster: "ocp-a", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"iso.ocp-a", "infra.ocp-a.machine-0"}, Status: TaskStatusPending},
			Playbook: "boot.ocp-a",
			State:    state,
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), schedulerRunOptions(dir),
			ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseBase}}, "", tasks,
			ConcurrencyLimits{}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
		done <- err
	}()
	runner.awaitStart(t, "iso.ocp-a")
	runner.awaitStart(t, "infra.ocp-a")
	runner.releasePlaybook("iso.ocp-a")
	time.Sleep(100 * time.Millisecond)
	for _, playbook := range runner.startedPlaybooks() {
		if playbook == "iso.ocp-b" {
			t.Fatal("the second cluster took the install slot while the first cluster was still provisioning its machines on a substrate provider; boot.ocp-a is then queued behind the whole second chain, bootstrap and install waits included")
		}
	}
	runner.releasePlaybook("infra.ocp-a")
	runner.awaitStart(t, "boot.ocp-a")
	runner.releasePlaybook("boot.ocp-a")
	runner.awaitStart(t, "iso.ocp-b")
	runner.releasePlaybook("iso.ocp-b")
	if err := <-done; err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	got := runner.startedPlaybooks()
	if len(got) != 4 {
		t.Fatalf("started playbooks = %v, want all four tasks", got)
	}
	if got[2] != "boot.ocp-a" || got[3] != "iso.ocp-b" {
		t.Fatalf("started playbooks = %v, want the second cluster's ISO to follow the first cluster's node boot", got)
	}
}

func TestRunApplyTaskGraphYieldsTheClusterInstallSlotToTheClusterItWaitsOn(t *testing.T) {
	t.Setenv(ParallelismClustersEnvVar, "")
	dir := t.TempDir()
	state := minimalState()
	runner := &recordingApplyRunner{}
	tasks := []ApplyTask{
		{
			Entry:    TaskLedgerEntry{ID: "iso.child", Kind: ApplyTaskKindClusterISO, Label: "iso child", Cluster: "child", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.child",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "infra.child.machine-0", Kind: ApplyTaskKindClusterInstall, Label: "provision machine machine-0", Cluster: "child", ClusterKind: ApplyClusterKindContainer, Node: "machine-0", Host: "parent-bm", Dependencies: []string{"wait.parent"}, Status: TaskStatusPending},
			Playbook: "infra.child",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "iso.parent", Kind: ApplyTaskKindClusterISO, Label: "iso parent", Cluster: "parent", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.parent",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "wait.parent", Kind: ApplyTaskKindInstallWait, Label: "wait install parent", Cluster: "parent", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"iso.parent"}, Status: TaskStatusPending},
			Playbook: "wait.parent",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "boot.child", Kind: ApplyTaskKindNodeBoot, Label: "boot child nodes", Cluster: "child", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"iso.child", "infra.child.machine-0", "wait.parent"}, Status: TaskStatusPending},
			Playbook: "boot.child",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "wait-bootstrap.child", Kind: ApplyTaskKindBootstrapWait, Label: "wait bootstrap child", Cluster: "child", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"boot.child"}, Status: TaskStatusPending},
			Playbook: "wait-bootstrap.child",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "wait.child", Kind: ApplyTaskKindInstallWait, Label: "wait install child", Cluster: "child", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"wait-bootstrap.child"}, Status: TaskStatusPending},
			Playbook: "wait.child",
			State:    state,
		},
	}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), schedulerRunOptions(dir),
		ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseBase}}, "", tasks,
		ConcurrencyLimits{}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if err != nil {
		t.Fatalf("a KubeVirt-hosted cluster whose boot waits on its host cluster's install must not hold the only cluster install slot against that host cluster: %v", err)
	}
	for _, task := range ledger.Tasks {
		if task.Status != TaskStatusOK {
			t.Fatalf("task %s ended %s (%s); every task of both clusters must run", task.ID, task.Status, task.SkippedReason)
		}
	}
}

func TestRedfishGrantUsesTheFreeSlotsRatherThanStarvingTheTask(t *testing.T) {
	storageOS := ApplyTask{Entry: TaskLedgerEntry{ID: "osinstall.ceph", Kind: ApplyTaskKindManagedMachineOS}, RedfishSlots: 6}
	nodeBoot := ApplyTask{Entry: TaskLedgerEntry{ID: "boot.ocp", Kind: ApplyTaskKindNodeBoot}, RedfishSlots: 3}
	if got := taskRedfishGrant(storageOS, 0, DefaultParallelismRedfish); got != 6 {
		t.Fatalf("grant for the first task = %d, want its full demand of 6", got)
	}
	if got := taskRedfishGrant(nodeBoot, 6, DefaultParallelismRedfish); got != 2 {
		t.Fatalf("grant for a 3-slot node boot against 2 free slots = %d, want 2: an all-or-nothing hold parks the whole bare-metal cluster behind an unrelated storage OS install for its full duration", got)
	}
	if got := taskRedfishGrant(nodeBoot, DefaultParallelismRedfish, DefaultParallelismRedfish); got != 0 {
		t.Fatalf("grant against a fully committed budget = %d, want 0", got)
	}
	if got := taskRedfishGrant(ApplyTask{Entry: TaskLedgerEntry{ID: "iso.ocp", Kind: ApplyTaskKindClusterISO}}, 0, DefaultParallelismRedfish); got != 0 {
		t.Fatalf("a task charging no Redfish slots must take none, got %d", got)
	}
}

func TestRunApplyTaskGraphBootsWhileAStorageOSInstallHoldsMostRedfishSlots(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := newGatedPlaybookRunner("osinstall.ceph", "boot.ocp")
	tasks := []ApplyTask{
		{
			Entry:        TaskLedgerEntry{ID: "osinstall.ceph", Kind: ApplyTaskKindManagedMachineOS, Label: "managed OS ceph machines", Cluster: "ceph", ClusterKind: ApplyClusterKindStorage, Status: TaskStatusPending},
			Playbook:     "osinstall.ceph",
			RedfishSlots: 6,
			State:        state,
		},
		{
			Entry:        TaskLedgerEntry{ID: "boot.ocp", Kind: ApplyTaskKindNodeBoot, Label: "boot ocp nodes", Cluster: "ocp", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook:     "boot.ocp",
			RedfishSlots: 3,
			State:        state,
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), schedulerRunOptions(dir),
			ApplyTarget{Name: "all", PhaseNames: []string{ApplyPhaseMachines, ApplyPhaseBase}}, "", tasks,
			ConcurrencyLimits{ParallelismRedfish: DefaultParallelismRedfish}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
		done <- err
	}()
	runner.awaitStart(t, "osinstall.ceph")
	runner.awaitStart(t, "boot.ocp")
	runner.releasePlaybook("osinstall.ceph")
	runner.releasePlaybook("boot.ocp")
	if err := <-done; err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
}

func TestRunApplyTaskGraphGivesTheInstallSlotToAnUnparkedChainFirst(t *testing.T) {
	t.Setenv(ParallelismClustersEnvVar, "")
	dir := t.TempDir()
	state := minimalState()
	runner := newGatedPlaybookRunner("iso.child", "boot.child", "iso.metal", "boot.metal", "wait.metal")
	tasks := []ApplyTask{
		{
			Entry:    TaskLedgerEntry{ID: "iso.child", Kind: ApplyTaskKindClusterISO, Label: "iso child", Cluster: "child", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.child",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "boot.child", Kind: ApplyTaskKindNodeBoot, Label: "boot child nodes", Cluster: "child", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"iso.child", "wait.metal"}, Status: TaskStatusPending},
			Playbook: "boot.child",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "iso.metal", Kind: ApplyTaskKindClusterISO, Label: "iso metal", Cluster: "metal", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.metal",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "boot.metal", Kind: ApplyTaskKindNodeBoot, Label: "boot metal nodes", Cluster: "metal", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"iso.metal"}, Status: TaskStatusPending},
			Playbook: "boot.metal",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "wait.metal", Kind: ApplyTaskKindInstallWait, Label: "wait install metal", Cluster: "metal", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"boot.metal"}, Status: TaskStatusPending},
			Playbook: "wait.metal",
			State:    state,
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), schedulerRunOptions(dir),
			ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseBase}}, "", tasks,
			ConcurrencyLimits{}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
		done <- err
	}()
	runner.awaitStart(t, "iso.metal")
	for _, playbook := range runner.startedPlaybooks() {
		if playbook == "iso.child" {
			t.Fatal("a hosted cluster parked on another cluster took the only install slot ahead of a bare-metal cluster whose chain could run to completion; the bare-metal install then queues behind ISO builds it does not depend on")
		}
	}
	runner.releasePlaybook("iso.metal")
	runner.awaitStart(t, "boot.metal")
	time.Sleep(100 * time.Millisecond)
	for _, playbook := range runner.startedPlaybooks() {
		if playbook == "iso.child" {
			t.Fatal("the parked hosted cluster took the install slot in a gap inside the bare-metal cluster's install chain")
		}
	}
	runner.releasePlaybook("boot.metal")
	runner.awaitStart(t, "wait.metal")
	runner.releasePlaybook("wait.metal")
	runner.awaitStart(t, "iso.child")
	runner.releasePlaybook("iso.child")
	runner.awaitStart(t, "boot.child")
	runner.releasePlaybook("boot.child")
	if err := <-done; err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	got := runner.startedPlaybooks()
	want := []string{"iso.metal", "boot.metal", "wait.metal", "iso.child", "boot.child"}
	if len(got) != len(want) {
		t.Fatalf("started playbooks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("started playbooks = %v, want the parked hosted cluster to run only after the unparked cluster's chain: %v", got, want)
		}
	}
}

func TestRunApplyTaskGraphAdmitsAParkedChainWhenNoUnparkedChainRemains(t *testing.T) {
	t.Setenv(ParallelismClustersEnvVar, "")
	dir := t.TempDir()
	state := minimalState()
	runner := &recordingApplyRunner{}
	tasks := []ApplyTask{
		{
			Entry:    TaskLedgerEntry{ID: "infra.metal.machine-0", Kind: ApplyTaskKindClusterInstall, Label: "provision machine machine-0", Cluster: "metal", ClusterKind: ApplyClusterKindContainer, Node: "machine-0", Host: "metal-bm", Status: TaskStatusPending},
			Playbook: "infra.metal",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "iso.child", Kind: ApplyTaskKindClusterISO, Label: "iso child", Cluster: "child", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.child",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "boot.child", Kind: ApplyTaskKindNodeBoot, Label: "boot child nodes", Cluster: "child", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"iso.child", "infra.metal.machine-0"}, Status: TaskStatusPending},
			Playbook: "boot.child",
			State:    state,
		},
	}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), schedulerRunOptions(dir),
		ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseBase}}, "", tasks,
		ConcurrencyLimits{}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	for _, task := range ledger.Tasks {
		if task.Status != TaskStatusOK {
			t.Fatalf("task %s ended %s; a parked chain must still get the slot when it is the only chain wanting one", task.ID, task.Status)
		}
	}
}

func TestRunApplyTaskGraphYieldsTheClusterInstallSlotWhenTheChainFails(t *testing.T) {
	t.Setenv(ParallelismClustersEnvVar, "")
	dir := t.TempDir()
	state := minimalState()
	runner := &recordingApplyRunner{failures: map[string]error{"iso.ocp-a": errors.New("boom")}}
	tasks := []ApplyTask{
		{
			Entry:    TaskLedgerEntry{ID: "iso.ocp-a", Kind: ApplyTaskKindClusterISO, Label: "iso ocp-a", Cluster: "ocp-a", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.ocp-a",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "boot.ocp-a", Kind: ApplyTaskKindNodeBoot, Label: "boot ocp-a nodes", Cluster: "ocp-a", ClusterKind: ApplyClusterKindContainer, Dependencies: []string{"iso.ocp-a"}, Status: TaskStatusPending},
			Playbook: "boot.ocp-a",
			State:    state,
		},
		{
			Entry:    TaskLedgerEntry{ID: "iso.ocp-b", Kind: ApplyTaskKindClusterISO, Label: "iso ocp-b", Cluster: "ocp-b", ClusterKind: ApplyClusterKindContainer, Status: TaskStatusPending},
			Playbook: "iso.ocp-b",
			State:    state,
		},
	}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), schedulerRunOptions(dir),
		ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseBase}}, "", tasks,
		ConcurrencyLimits{}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if err == nil {
		t.Fatal("RunApplyTaskGraph must report the failed cluster install")
	}
	if task, _ := ledger.Task("iso.ocp-b"); task.Status != TaskStatusOK {
		t.Fatalf("iso.ocp-b ended %s; a cluster whose install chain died must not keep the install slot from the other clusters", task.Status)
	}
}

func TestRunApplyTaskGraphKeepsOneClusterInstallTasksParallel(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &recordingApplyRunner{delay: 25 * time.Millisecond}
	tasks := []ApplyTask{}
	for _, node := range []string{"node-0", "node-1"} {
		tasks = append(tasks, ApplyTask{
			Entry: TaskLedgerEntry{
				ID:          "boot.ocp-a." + node,
				Kind:        ApplyTaskKindNodeBoot,
				Label:       "boot " + node,
				Cluster:     "ocp-a",
				ClusterKind: ApplyClusterKindContainer,
				Status:      TaskStatusPending,
			},
			Playbook: "boot." + node,
			State:    state,
		})
	}
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), schedulerRunOptions(dir),
		ApplyTarget{Name: "clusters", PhaseNames: []string{ApplyPhaseBase}}, "", tasks,
		ConcurrencyLimits{Parallelism: 2}, nil, func(io.Writer, io.Writer) ansible.Runner { return runner })
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if _, maxActive := runner.snapshot(); maxActive != 2 {
		t.Fatalf("max concurrent tasks inside one cluster = %d, want 2; the cluster limit must not serialize a cluster's own nodes", maxActive)
	}
}

func TestResolveApplyConcurrencyLimitsRecordsTrivialAutoValues(t *testing.T) {
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{ID: "a", HostSlotKey: "host:bastion:machine", HostSlotCount: 1}, RedfishSlots: 2},
		{Entry: TaskLedgerEntry{ID: "b", HostSlotKey: "host:bastion:machine", HostSlotCount: 1}},
		{Entry: TaskLedgerEntry{ID: "c"}},
	}
	limits := ResolveApplyConcurrencyLimits(ConcurrencyLimits{}, tasks)
	if limits.AutoParallelism != 3 || limits.AutoParallelismPerHost != 2 || limits.AutoParallelismRedfish != 2 {
		t.Fatalf("auto limits = %+v, want 3/2/2", limits)
	}
	if !limits.ParallelismUnbounded() || !limits.ParallelismPerHostUnbounded() || !limits.ParallelismRedfishUnbounded() {
		t.Fatalf("an empty ConcurrencyLimits{} caps nothing, so every limit must report unbounded: %+v", limits)
	}
	capped := ResolveApplyConcurrencyLimits(ConcurrencyLimits{Parallelism: 2, ParallelismPerHost: 1, ParallelismRedfish: 1}, tasks)
	if capped.ParallelismUnbounded() || capped.ParallelismPerHostUnbounded() || capped.ParallelismRedfishUnbounded() {
		t.Fatalf("explicit caps below the auto values must not report unbounded: %+v", capped)
	}
	if got := ResolveApplyConcurrencyLimits(capped, tasks); got != capped {
		t.Fatalf("resolving already-resolved limits changed them: %+v -> %+v", capped, got)
	}
}
