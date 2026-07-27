package workflow

import (
	"encoding/json"
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

func TestRunLedgerTaskLookupUsesIndexAndSurvivesLoadedLedgers(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	entries := []TaskLedgerEntry{
		{ID: "a", Kind: ApplyTaskKindProvider, Label: "a"},
		{ID: "b", Kind: ApplyTaskKindProvider, Label: "b"},
		{ID: "c", Kind: ApplyTaskKindProvider, Label: "c"},
	}
	ledger := NewRunLedger("run-1", "all", "", ConcurrencyLimits{Parallelism: 1}, entries, now)
	if ledger.taskIndex == nil {
		t.Fatal("NewRunLedger must build a task index so dispatch stops linear-scanning")
	}
	for _, id := range []string{"a", "b", "c"} {
		entry, ok := ledger.Task(id)
		if !ok || entry.ID != id {
			t.Fatalf("Task(%q) = %+v ok=%v", id, entry, ok)
		}
	}
	if _, ok := ledger.Task("missing"); ok {
		t.Fatal("Task must not resolve an unknown id")
	}
	ledger.MarkOK("b", now)
	if entry, _ := ledger.Task("b"); entry.Status != TaskStatusOK {
		t.Fatalf("indexed update did not land on b: %+v", entry)
	}

	dir := t.TempDir()
	if err := SaveRunLedger(dir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}
	loaded, _, err := LoadRunLedger(dir)
	if err != nil {
		t.Fatalf("LoadRunLedger: %v", err)
	}
	if loaded.taskIndex != nil {
		t.Fatal("a ledger decoded from disk has no index; lookup must fall back to a scan")
	}
	if entry, ok := loaded.Task("c"); !ok || entry.ID != "c" {
		t.Fatalf("loaded ledger lookup = %+v ok=%v", entry, ok)
	}
	loaded.MarkSkipped("c", "reason", now)
	if entry, _ := loaded.Task("c"); entry.Status != TaskStatusSkipped {
		t.Fatalf("index-less update did not land on c: %+v", entry)
	}

	stale := ledger
	stale.Tasks = []TaskLedgerEntry{{ID: "c", Kind: ApplyTaskKindProvider}, {ID: "a", Kind: ApplyTaskKindProvider}}
	if entry, ok := stale.Task("a"); !ok || entry.ID != "a" {
		t.Fatalf("a stale index must never resolve to the wrong task: got %+v ok=%v", entry, ok)
	}
	stale.MarkFailed("a", "boom", now)
	if entry, _ := stale.Task("a"); entry.Failure != "boom" {
		t.Fatalf("stale-index update landed on the wrong entry: %+v", stale.Tasks)
	}
	if stale.Tasks[0].Failure != "" {
		t.Fatalf("stale-index update corrupted an unrelated task: %+v", stale.Tasks[0])
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

func timingsFixtureLedger() RunLedger {
	base := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	at := func(seconds int) *time.Time {
		t := base.Add(time.Duration(seconds) * time.Second)
		return &t
	}
	end := base.Add(2 * time.Minute)
	return RunLedger{
		RunID:     "apply-timings",
		Target:    "clusters",
		Status:    RunStatusOK,
		StartedAt: base,
		EndedAt:   &end,
		Tasks: []TaskLedgerEntry{
			{ID: "a", Label: "iso demo", Kind: "iso", Status: TaskStatusOK, StartedAt: at(0), EndedAt: at(10)},
			{ID: "b", Label: "boot demo nodes", Kind: "nodeBoot", Status: TaskStatusOK, Dependencies: []string{"a"}, StartedAt: at(10), EndedAt: at(70)},
			{ID: "c", Label: "infra demo", Kind: "infra", Status: TaskStatusOK, Dependencies: []string{"a"}, ReadyAt: at(11), StartedAt: at(15), EndedAt: at(45), BlockedOn: []string{"host slot host:bastion:machine"}},
			{ID: "d", Label: "wait install demo", Kind: "installWait", Status: TaskStatusOK, Dependencies: []string{"b"}, OrderingDependencies: []string{"c"}, StartedAt: at(80), EndedAt: at(90)},
			{ID: "e", Label: "provider services", Kind: "providerServices", Status: TaskStatusOK, StartedAt: at(0), EndedAt: at(45)},
			{ID: "f", Label: "addon demo", Kind: "addon", Status: TaskStatusPending, Dependencies: []string{"d"}},
		},
	}
}

func TestRunLedgerTaskTimingsSortAndWaits(t *testing.T) {
	ledger := timingsFixtureLedger()
	timings := ledger.TaskTimings()

	order := make([]string, 0, len(timings))
	for _, timing := range timings {
		order = append(order, timing.Entry.ID)
	}
	want := []string{"b", "e", "c", "a", "d", "f"}
	if !slices.Equal(order, want) {
		t.Fatalf("task timing order = %v, want descending by duration then id %v", order, want)
	}

	byID := map[string]TaskTiming{}
	for _, timing := range timings {
		byID[timing.Entry.ID] = timing
	}
	if got := byID["b"].Duration; got != time.Minute {
		t.Fatalf("task b duration = %s, want 1m0s", got)
	}
	if got := byID["c"].QueueWait; got != 5*time.Second {
		t.Fatalf("task c queue wait = %s, want 5s after its dependency ended", got)
	}
	if got := byID["c"].BlockedWait; got != 4*time.Second {
		t.Fatalf("task c blocked wait = %s, want 4s between ReadyAt and StartedAt", got)
	}
	if got := byID["d"].QueueWait; got != 10*time.Second {
		t.Fatalf("task d queue wait = %s, want 10s measured from the latest of its hard and ordering deps", got)
	}
	if got := byID["a"].QueueWait; got != 0 {
		t.Fatalf("task a queue wait = %s, want 0 for a task with no dependencies", got)
	}
	if got := byID["f"].Duration; got != 0 {
		t.Fatalf("task f duration = %s, want 0 for a task that never ran", got)
	}
	if got := ledger.WallClock(); got != 2*time.Minute {
		t.Fatalf("wall clock = %s, want 2m0s", got)
	}
}

func TestRunLedgerCriticalPath(t *testing.T) {
	path := timingsFixtureLedger().CriticalPath()

	ids := make([]string, 0, len(path.Hops))
	cumulative := make([]time.Duration, 0, len(path.Hops))
	for _, hop := range path.Hops {
		ids = append(ids, hop.Timing.Entry.ID)
		cumulative = append(cumulative, hop.Cumulative)
	}
	wantIDs := []string{"a", "b", "d"}
	if !slices.Equal(ids, wantIDs) {
		t.Fatalf("critical path = %v, want the longest weighted chain %v", ids, wantIDs)
	}
	wantCumulative := []time.Duration{10 * time.Second, 70 * time.Second, 80 * time.Second}
	if !slices.Equal(cumulative, wantCumulative) {
		t.Fatalf("critical path cumulative = %v, want %v", cumulative, wantCumulative)
	}
	if path.Total != 80*time.Second {
		t.Fatalf("critical path total = %s, want 80s", path.Total)
	}
}

func TestRunLedgerCriticalPathEmptyWithoutTimestamps(t *testing.T) {
	ledger := RunLedger{Tasks: []TaskLedgerEntry{
		{ID: "a", Status: TaskStatusPending},
		{ID: "b", Status: TaskStatusPending, Dependencies: []string{"a"}},
	}}
	if path := ledger.CriticalPath(); len(path.Hops) != 0 || path.Total != 0 {
		t.Fatalf("critical path over an unstarted run = %+v, want empty", path)
	}
}

func TestRunLedgerCriticalPathToleratesDependencyCycle(t *testing.T) {
	base := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	at := func(seconds int) *time.Time {
		t := base.Add(time.Duration(seconds) * time.Second)
		return &t
	}
	ledger := RunLedger{Tasks: []TaskLedgerEntry{
		{ID: "a", Status: TaskStatusOK, Dependencies: []string{"b"}, StartedAt: at(0), EndedAt: at(10)},
		{ID: "b", Status: TaskStatusOK, Dependencies: []string{"a"}, StartedAt: at(0), EndedAt: at(20)},
	}}
	path := ledger.CriticalPath()
	if path.Total <= 0 || len(path.Hops) == 0 {
		t.Fatalf("critical path over a cyclic graph = %+v, want a finite non-empty result", path)
	}
}

func TestTaskLedgerEntryDecodesLedgerWrittenBeforeTimingFields(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
  "runId": "apply-legacy",
  "target": "clusters",
  "status": "ok",
  "startedAt": "2026-05-22T12:00:00Z",
  "limits": {"parallelism": 4, "parallelismPerHost": 2, "parallelismRedfish": 1},
  "tasks": [
    {
      "id": "provider",
      "kind": "providerServices",
      "label": "provider services",
      "status": "ok",
      "startedAt": "2026-05-22T12:00:00Z",
      "endedAt": "2026-05-22T12:00:30Z"
    }
  ]
}
`
	if err := os.WriteFile(LedgerPath(dir), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy ledger: %v", err)
	}
	ledger, ok, err := LoadRunLedger(dir)
	if err != nil {
		t.Fatalf("LoadRunLedger on a ledger written before the timing fields existed: %v", err)
	}
	if !ok {
		t.Fatal("LoadRunLedger did not find the legacy ledger")
	}
	task := ledger.Tasks[0]
	if task.ReadyAt != nil || task.BlockedOn != nil {
		t.Fatalf("legacy task decoded with synthesized timing fields: %+v", task)
	}
	if got := taskDuration(task); got != 30*time.Second {
		t.Fatalf("legacy task duration = %s, want 30s", got)
	}
	if ledger.Limits.ParallelismUnbounded() || ledger.Limits.ParallelismPerHostUnbounded() || ledger.Limits.ParallelismRedfishUnbounded() {
		t.Fatal("a legacy ledger records no auto limits, so no limit may be reported as unbounded")
	}
}

func TestConcurrencyLimitsUnbounded(t *testing.T) {
	limits := ConcurrencyLimits{
		Parallelism: 12, AutoParallelism: 12,
		ParallelismPerHost: 2, AutoParallelismPerHost: 6,
		ParallelismRedfish: 3, AutoParallelismRedfish: 3,
	}
	if !limits.ParallelismUnbounded() {
		t.Fatal("a global limit equal to its trivial auto value is not a cap and must report unbounded")
	}
	if limits.ParallelismPerHostUnbounded() {
		t.Fatal("a per-host limit below its auto value is a real cap and must not report unbounded")
	}
	if !limits.ParallelismRedfishUnbounded() {
		t.Fatal("a Redfish limit equal to its trivial auto value must report unbounded")
	}
}

func TestRunLedgerRecordsReadyAtOnceAndDedupesBlockedOn(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := RunLedger{Tasks: []TaskLedgerEntry{{ID: "t", Status: TaskStatusPending}}}
	ledger.MarkDependencyReady("t", now)
	ledger.MarkDependencyReady("t", now.Add(time.Minute))
	task, _ := ledger.Task("t")
	if task.ReadyAt == nil || !task.ReadyAt.Equal(now) {
		t.Fatalf("ReadyAt = %v, want the first dependency-ready observation %s", task.ReadyAt, now)
	}

	ledger.RecordBlockedOn("t", "resource libvirt:host")
	ledger.RecordBlockedOn("t", "resource libvirt:host")
	ledger.RecordBlockedOn("t", "redfish budget")
	ledger.RecordBlockedOn("t", "  ")
	for i := 0; i < maxBlockedOnReasons+4; i++ {
		ledger.RecordBlockedOn("t", "resource "+string(rune('a'+i)))
	}
	task, _ = ledger.Task("t")
	want := []string{"resource libvirt:host", "redfish budget"}
	if !slices.Equal(task.BlockedOn[:2], want) {
		t.Fatalf("BlockedOn head = %v, want %v", task.BlockedOn[:2], want)
	}
	if len(task.BlockedOn) != maxBlockedOnReasons {
		t.Fatalf("BlockedOn length = %d, want the recorded reasons capped at %d", len(task.BlockedOn), maxBlockedOnReasons)
	}
}

func TestArchivedRunLedgerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ledger := timingsFixtureLedger()
	if err := ArchiveRunLedger(dir, ledger); err != nil {
		t.Fatalf("ArchiveRunLedger: %v", err)
	}
	loaded, ok, err := LoadArchivedRunLedger(dir, ledger.RunID)
	if err != nil || !ok {
		t.Fatalf("LoadArchivedRunLedger: ok=%v err=%v", ok, err)
	}
	if loaded.RunID != ledger.RunID || len(loaded.Tasks) != len(ledger.Tasks) {
		t.Fatalf("archived ledger mismatch: %+v", loaded)
	}
	if got := loaded.Tasks[2].ReadyAt; got == nil {
		t.Fatal("archived ledger lost the per-task ReadyAt timestamp")
	}
	if ids := ArchivedRunIDs(dir); !slices.Equal(ids, []string{ledger.RunID}) {
		t.Fatalf("ArchivedRunIDs = %v, want %v", ids, []string{ledger.RunID})
	}
	if _, ok, err := LoadArchivedRunLedger(dir, "apply-missing"); ok || err != nil {
		t.Fatalf("LoadArchivedRunLedger on a missing run: ok=%v err=%v", ok, err)
	}
	var encoded map[string]any
	data, err := os.ReadFile(ArchivedRunLedgerPath(dir, ledger.RunID))
	if err != nil {
		t.Fatalf("read archived ledger: %v", err)
	}
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatalf("decode archived ledger: %v", err)
	}
	tasks, _ := encoded["tasks"].([]any)
	first, _ := tasks[0].(map[string]any)
	if _, present := first["readyAt"]; present {
		t.Fatal("readyAt must be omitted for tasks that never recorded it")
	}
}
