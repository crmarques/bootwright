package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLeaseLiveness(t *testing.T) {
	host, _ := os.Hostname()
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	oldAlive := runLeaseProcessAlive
	oldToken := runLeaseProcessStartToken
	defer func() { runLeaseProcessAlive = oldAlive; runLeaseProcessStartToken = oldToken }()

	cases := []struct {
		name       string
		alive      bool
		tokenOK    bool
		token      string
		lease      RunLease
		now        time.Time
		wantState  RunActivityState
		wantDetail string
	}{
		{
			name:  "live local process is active regardless of heartbeat age",
			alive: true, tokenOK: true, token: "tok",
			lease:     RunLease{Hostname: host, PID: 42, ProcessStart: "tok", HeartbeatAt: now.Add(-time.Hour)},
			now:       now,
			wantState: RunActivityActive, wantDetail: "apply lease process is running",
		},
		{
			name:      "no heartbeat is stale",
			alive:     false,
			lease:     RunLease{Hostname: host, PID: 42},
			now:       now,
			wantState: RunActivityStale, wantDetail: "apply lease has no heartbeat",
		},
		{
			name:      "gone local process is stale",
			alive:     false,
			lease:     RunLease{Hostname: host, PID: 42, HeartbeatAt: now},
			now:       now.Add(time.Second),
			wantState: RunActivityStale, wantDetail: "apply lease process is not running",
		},
		{
			name:      "remote lease with aged heartbeat is stale",
			alive:     false,
			lease:     RunLease{Hostname: "other-host", PID: 42, HeartbeatAt: now},
			now:       now.Add(ApplyLeaseStaleAfter + time.Second),
			wantState: RunActivityStale, wantDetail: "apply lease heartbeat is stale",
		},
		{
			name:      "remote lease with fresh heartbeat is active",
			alive:     false,
			lease:     RunLease{Hostname: "other-host", PID: 42, HeartbeatAt: now},
			now:       now.Add(ApplyLeaseStaleAfter - time.Second),
			wantState: RunActivityActive, wantDetail: "apply lease heartbeat is fresh",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runLeaseProcessAlive = func(int) bool { return tc.alive }
			runLeaseProcessStartToken = func(int) (string, bool) { return tc.token, tc.tokenOK }
			gotState, gotDetail := leaseLiveness(tc.lease, tc.now)
			if gotState != tc.wantState || gotDetail != tc.wantDetail {
				t.Fatalf("leaseLiveness = %s/%q, want %s/%q", gotState, gotDetail, tc.wantState, tc.wantDetail)
			}
			if got := leaseFresh(tc.lease, tc.now); got != (tc.wantState == RunActivityActive) {
				t.Fatalf("leaseFresh = %v, want %v (must agree with leaseLiveness)", got, tc.wantState == RunActivityActive)
			}
		})
	}
}

func TestRunLedgerRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := NewRunLedger("run-1", "all", "cluster-a", ConcurrencyLimits{
		Parallelism:        4,
		ParallelismPerHost: 2,
		ParallelismRedfish: 8,
	}, []TaskLedgerEntry{{
		ID:    "provider",
		Kind:  "providerServices",
		Label: "provider services",
	}}, now)

	dir := t.TempDir()
	if err := SaveRunLedger(dir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}
	loaded, ok, err := LoadRunLedger(dir)
	if err != nil {
		t.Fatalf("LoadRunLedger: %v", err)
	}
	if !ok {
		t.Fatal("LoadRunLedger did not find saved ledger")
	}
	if loaded.RunID != "run-1" || loaded.Tasks[0].Status != TaskStatusPending {
		t.Fatalf("loaded ledger mismatch: %+v", loaded)
	}
	if got := filepath.Base(LedgerPath(dir)); got != "current.json" {
		t.Fatalf("ledger path base = %q", got)
	}
}

func TestRunLeaseRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	lease := NewRunLease("run-1", now)

	dir := t.TempDir()
	if err := SaveRunLease(dir, lease); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}
	loaded, ok, err := LoadRunLease(dir)
	if err != nil {
		t.Fatalf("LoadRunLease: %v", err)
	}
	if !ok {
		t.Fatal("LoadRunLease did not find saved lease")
	}
	if loaded.RunID != "run-1" || loaded.PID == 0 || !loaded.HeartbeatAt.Equal(now) {
		t.Fatalf("loaded lease mismatch: %+v", loaded)
	}
	if got := filepath.Base(LeasePath(dir)); got != "current.lease.json" {
		t.Fatalf("lease path base = %q", got)
	}
}

func TestAssessRunActivity(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := NewRunLedger("run-1", "cluster", "", ConcurrencyLimits{}, nil, now)
	dir := t.TempDir()

	recent, err := AssessRunActivity(dir, ledger, now)
	if err != nil {
		t.Fatalf("AssessRunActivity recent missing lease: %v", err)
	}
	if recent.State != RunActivityActive {
		t.Fatalf("recent missing lease activity = %+v, want active", recent)
	}

	missing, err := AssessRunActivity(dir, ledger, now.Add(ApplyLeaseStaleAfter+time.Second))
	if err != nil {
		t.Fatalf("AssessRunActivity missing lease: %v", err)
	}
	if missing.State != RunActivityStale || missing.Detail == "" {
		t.Fatalf("missing lease activity = %+v, want stale with detail", missing)
	}

	lease := NewRunLease("run-1", now)
	if err := SaveRunLease(dir, lease); err != nil {
		t.Fatalf("SaveRunLease fresh: %v", err)
	}
	active, err := AssessRunActivity(dir, ledger, now.Add(ApplyLeaseStaleAfter-time.Second))
	if err != nil {
		t.Fatalf("AssessRunActivity fresh lease: %v", err)
	}
	if active.State != RunActivityActive || active.Lease == nil {
		t.Fatalf("fresh lease activity = %+v, want active with lease", active)
	}

	lease.Hostname = "other-host"
	lease.HeartbeatAt = now.Add(-ApplyLeaseStaleAfter - time.Second)
	if err := SaveRunLease(dir, lease); err != nil {
		t.Fatalf("SaveRunLease stale: %v", err)
	}
	stale, err := AssessRunActivity(dir, ledger, now)
	if err != nil {
		t.Fatalf("AssessRunActivity stale lease: %v", err)
	}
	if stale.State != RunActivityStale || stale.Lease == nil {
		t.Fatalf("stale lease activity = %+v, want stale with lease", stale)
	}
}

func TestAssessRunActivityTreatsFreshLocalLeaseWithMissingProcessAsStale(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := NewRunLedger("run-1", "cluster", "", ConcurrencyLimits{}, nil, now)
	lease := NewRunLease("run-1", now)
	dir := t.TempDir()
	if err := SaveRunLease(dir, lease); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}
	previous := runLeaseProcessAlive
	runLeaseProcessAlive = func(int) bool { return false }
	defer func() { runLeaseProcessAlive = previous }()

	activity, err := AssessRunActivity(dir, ledger, now.Add(time.Second))
	if err != nil {
		t.Fatalf("AssessRunActivity: %v", err)
	}
	if activity.State != RunActivityStale || activity.Detail != "apply lease process is not running" {
		t.Fatalf("activity = %+v, want stale missing process", activity)
	}
}

func TestAssessRunActivityDoesNotProbeRemoteHostLeaseProcess(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := NewRunLedger("run-1", "cluster", "", ConcurrencyLimits{}, nil, now)
	lease := NewRunLease("run-1", now)
	lease.Hostname = "other-host"
	dir := t.TempDir()
	if err := SaveRunLease(dir, lease); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}
	previous := runLeaseProcessAlive
	runLeaseProcessAlive = func(int) bool {
		t.Fatal("remote host lease must not probe the local process table")
		return false
	}
	defer func() { runLeaseProcessAlive = previous }()

	activity, err := AssessRunActivity(dir, ledger, now.Add(time.Second))
	if err != nil {
		t.Fatalf("AssessRunActivity: %v", err)
	}
	if activity.State != RunActivityActive {
		t.Fatalf("activity = %+v, want active", activity)
	}
}

func TestCancelRunLedgerMarksNonTerminalTasksCancelled(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := NewRunLedger("run-1", "cluster", "", ConcurrencyLimits{}, []TaskLedgerEntry{
		{ID: "done", Status: TaskStatusOK},
		{ID: "running", Status: TaskStatusRunning},
		{ID: "pending", Status: TaskStatusPending},
	}, now)
	dir := t.TempDir()
	if err := SaveRunLease(dir, NewRunLease("run-1", now)); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}

	cancelled, err := CancelRunLedger(dir, ledger, "test cancellation", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("CancelRunLedger: %v", err)
	}
	if cancelled.Status != RunStatusCancelled || cancelled.EndedAt == nil {
		t.Fatalf("cancelled ledger status = %+v", cancelled)
	}
	if cancelled.Tasks[0].Status != TaskStatusOK {
		t.Fatalf("terminal task status changed to %s", cancelled.Tasks[0].Status)
	}
	for _, task := range cancelled.Tasks[1:] {
		if task.Status != TaskStatusCancelled || task.SkippedReason != "test cancellation" || task.EndedAt == nil {
			t.Fatalf("task was not cancelled with reason: %+v", task)
		}
	}
	if _, found, err := LoadRunLease(dir); err != nil || found {
		t.Fatalf("lease after cancellation found=%v err=%v", found, err)
	}
}

func TestRunLedgerBlocksDependents(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := NewRunLedger("run-1", "all", "", ConcurrencyLimits{}, []TaskLedgerEntry{
		{ID: "provider", Kind: "providerServices", Label: "provider"},
		{ID: "infra.cluster-a", Kind: "clusterInstall", Label: "infra", Dependencies: []string{"provider"}},
		{ID: "install.cluster-a", Kind: "clusterInstall", Label: "install", Dependencies: []string{"infra.cluster-a"}},
	}, now)

	ledger.MarkFailed("provider", "boom", now.Add(time.Second))

	if got, _ := ledger.Task("infra.cluster-a"); got.Status != TaskStatusBlocked {
		t.Fatalf("infra status = %s, want blocked", got.Status)
	}
	if got, _ := ledger.Task("install.cluster-a"); got.Status != TaskStatusBlocked {
		t.Fatalf("install status = %s, want blocked", got.Status)
	}
	reasons := []string{ledger.Tasks[1].SkippedReason, ledger.Tasks[2].SkippedReason}
	if !slices.ContainsFunc(reasons, func(reason string) bool {
		return strings.Contains(reason, "dependency provider failed")
	}) {
		t.Fatalf("blocked reasons = %v, want provider dependency", reasons)
	}
}

func TestRunLedgerProgressCounts(t *testing.T) {
	ledger := RunLedger{Tasks: []TaskLedgerEntry{
		{Status: TaskStatusOK},
		{Status: TaskStatusOK},
		{Status: TaskStatusRunning},
		{Status: TaskStatusPending},
	}}
	counts := ledger.ProgressCounts()
	got := map[TaskStatus]int{}
	for _, count := range counts {
		got[count.Status] = count.Count
	}
	if got[TaskStatusOK] != 2 || got[TaskStatusRunning] != 1 || got[TaskStatusPending] != 1 {
		t.Fatalf("counts = %#v", got)
	}
}

func TestRunLedgerTasksForClusterPreservesLedgerOrder(t *testing.T) {
	ledger := RunLedger{Tasks: []TaskLedgerEntry{
		{ID: "storage.ceph", Cluster: "ceph"},
		{ID: "provider"},
		{ID: "storageinfra.ceph", Cluster: "ceph"},
	}}

	tasks := ledger.TasksForCluster("ceph")
	got := []string{tasks[0].ID, tasks[1].ID}
	want := []string{"storage.ceph", "storageinfra.ceph"}
	if !slices.Equal(got, want) {
		t.Fatalf("cluster task order = %v, want %v", got, want)
	}
}
