package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func statusTimingsFixtureLedger() workflow.RunLedger {
	base := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	at := func(seconds int) *time.Time {
		t := base.Add(time.Duration(seconds) * time.Second)
		return &t
	}
	end := base.Add(2 * time.Minute)
	return workflow.RunLedger{
		RunID:     "apply-timings",
		Target:    "clusters",
		Status:    workflow.RunStatusOK,
		StartedAt: base,
		EndedAt:   &end,
		Limits: workflow.ConcurrencyLimits{
			Parallelism: 6, AutoParallelism: 6,
			ParallelismPerHost: 1, AutoParallelismPerHost: 2,
			ParallelismRedfish: 2, AutoParallelismRedfish: 2,
		},
		Tasks: []workflow.TaskLedgerEntry{
			{ID: "a", Label: "iso demo", Kind: "iso", Cluster: "demo", Status: workflow.TaskStatusOK, StartedAt: at(0), EndedAt: at(10)},
			{ID: "b", Label: "boot demo nodes", Kind: "nodeBoot", Cluster: "demo", Status: workflow.TaskStatusOK, Dependencies: []string{"a"}, StartedAt: at(10), EndedAt: at(70)},
			{ID: "c", Label: "infra demo", Kind: "infra", Cluster: "demo", Status: workflow.TaskStatusOK, Dependencies: []string{"a"}, ReadyAt: at(11), StartedAt: at(15), EndedAt: at(45), BlockedOn: []string{"host slot host:bastion:machine"}},
			{ID: "d", Label: "wait install demo", Kind: "installWait", Cluster: "demo", Status: workflow.TaskStatusOK, Dependencies: []string{"b"}, OrderingDependencies: []string{"c"}, StartedAt: at(80), EndedAt: at(90)},
			{ID: "e", Label: "provider services", Kind: "providerServices", Status: workflow.TaskStatusOK, StartedAt: at(0), EndedAt: at(45)},
		},
	}
}

func TestStatusTimingsReportsCriticalPathAndQueueWaits(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	if err := workflow.ArchiveRunLedger(ctx.RunsDir, statusTimingsFixtureLedger()); err != nil {
		t.Fatalf("ArchiveRunLedger: %v", err)
	}

	stdout, stderr, code := runCLI(t, "status", "--run", "apply-timings", "--timings")
	if code != 0 {
		t.Fatalf("status --timings exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"Critical path",
		"Create ISO demo: 10s (cumulative 10s)",
		"Boot demo nodes: 1m0s (cumulative 1m10s)",
		"Install demo: 10s (cumulative 1m20s)",
		"Total: 1m20s of 2m0s wall clock (67%)",
		"Task timings",
		"Wall clock: 2m0s",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status --timings output missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Infra demo: 30s  queue 5s  blocked 4s on host slot host:bastion:machine") == false {
		t.Fatalf("status --timings did not attribute the blocked wait to its host slot:\n%s", stdout)
	}
	bootIndex := strings.Index(stdout, "Task timings")
	if bootIndex < 0 || strings.Index(stdout[bootIndex:], "Boot demo nodes") > strings.Index(stdout[bootIndex:], "Provider services") {
		t.Fatalf("task timings must be sorted by descending duration:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Parallelism: tasks unbounded, per host 1, Redfish unbounded") {
		t.Fatalf("status --timings must distinguish a real cap from an unbounded auto limit:\n%s", stdout)
	}
}

func TestStatusTimingsJSONReportsMachineReadableCriticalPath(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	if err := workflow.ArchiveRunLedger(ctx.RunsDir, statusTimingsFixtureLedger()); err != nil {
		t.Fatalf("ArchiveRunLedger: %v", err)
	}

	stdout, stderr, code := runCLI(t, "status", "--run", "apply-timings", "--timings", "--output", "json")
	if code != 0 {
		t.Fatalf("status --timings --output json exited %d, stderr=%q", code, stderr)
	}
	var report runTimingsReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode timings JSON: %v\n%s", err, stdout)
	}
	if report.Run != "apply-timings" || report.WallClockSeconds != 120 {
		t.Fatalf("report head = %+v", report)
	}
	if report.CriticalPath.TotalSeconds != 80 || len(report.CriticalPath.Hops) != 3 {
		t.Fatalf("critical path = %+v, want 80s over 3 hops", report.CriticalPath)
	}
	hops := []string{}
	for _, hop := range report.CriticalPath.Hops {
		hops = append(hops, hop.ID)
	}
	if strings.Join(hops, ",") != "a,b,d" {
		t.Fatalf("critical path hops = %v, want a,b,d", hops)
	}
	if report.CriticalPath.Hops[2].CumulativeSeconds != 80 {
		t.Fatalf("last hop cumulative = %v, want the path total", report.CriticalPath.Hops[2].CumulativeSeconds)
	}
	if report.Tasks[0].ID != "b" || report.Tasks[0].DurationSeconds != 60 {
		t.Fatalf("tasks[0] = %+v, want the longest task first", report.Tasks[0])
	}
	var blocked runTimingsTask
	for _, task := range report.Tasks {
		if task.ID == "c" {
			blocked = task
		}
	}
	if blocked.QueueWaitSeconds != 5 || blocked.BlockedWaitSeconds != 4 {
		t.Fatalf("task c waits = queue %v blocked %v, want 5 and 4", blocked.QueueWaitSeconds, blocked.BlockedWaitSeconds)
	}
	if len(blocked.BlockedOn) != 1 || blocked.BlockedOn[0] != "host slot host:bastion:machine" {
		t.Fatalf("task c blockedOn = %v", blocked.BlockedOn)
	}
	if report.Limits.ParallelismPerHostUnbounded || !report.Limits.ParallelismUnbounded {
		t.Fatalf("limits = %+v, want the per-host cap reported as bounded and the global limit as unbounded", report.Limits)
	}
}

func TestStatusTimingsFallsBackToTheCurrentRunLedger(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	if err := workflow.SaveRunLedger(ctx.RunsDir, statusTimingsFixtureLedger()); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}

	stdout, stderr, code := runCLI(t, "status", "--timings")
	if code != 0 {
		t.Fatalf("status --timings exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "apply-timings") {
		t.Fatalf("status --timings without --run must report the current run:\n%s", stdout)
	}
}

func TestStatusTimingsRejectsUnknownRunAndBareRunFlag(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	if err := workflow.ArchiveRunLedger(ctx.RunsDir, statusTimingsFixtureLedger()); err != nil {
		t.Fatalf("ArchiveRunLedger: %v", err)
	}

	if _, stderr, code := runCLI(t, "status", "--run", "apply-timings"); code != 2 || !strings.Contains(stderr, "--run is only supported with --timings") {
		t.Fatalf("status --run without --timings exited %d, stderr=%q", code, stderr)
	}
	_, stderr, code := runCLI(t, "status", "--run", "apply-nope", "--timings")
	if code == 0 {
		t.Fatal("status --timings for an unknown run must fail")
	}
	if !strings.Contains(stderr, "apply-nope") || !strings.Contains(stderr, "recorded runs: apply-timings") {
		t.Fatalf("unknown-run error must list recorded runs, got %q", stderr)
	}
}

func TestApplyLimitsSummaryNamesUnreachableLimitsUnbounded(t *testing.T) {
	auto := workflow.ConcurrencyLimits{
		Parallelism: 6, AutoParallelism: 6,
		ParallelismPerHost: 2, AutoParallelismPerHost: 2,
		ParallelismRedfish: 3, AutoParallelismRedfish: 3,
	}
	if got := applyLimitsSummary(auto); got != "tasks unbounded, per host unbounded, Redfish unbounded" {
		t.Fatalf("banner for structurally unreachable limits = %q", got)
	}
	capped := workflow.ConcurrencyLimits{
		Parallelism: 2, AutoParallelism: 6,
		ParallelismPerHost: 1, AutoParallelismPerHost: 2,
		ParallelismRedfish: 1, AutoParallelismRedfish: 3,
	}
	if got := applyLimitsSummary(capped); got != "tasks 2, per host 1, Redfish 1" {
		t.Fatalf("banner for real caps = %q", got)
	}
	legacy := workflow.ConcurrencyLimits{Parallelism: 4, ParallelismPerHost: 2, ParallelismRedfish: 1}
	if got := applyLimitsSummary(legacy); got != "tasks 4, per host 2, Redfish 1" {
		t.Fatalf("banner for a ledger without recorded auto values = %q", got)
	}
}
