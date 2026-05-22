package workflow

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

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
	if got := filepath.Base(LedgerPath(dir)); got != "current-apply.json" {
		t.Fatalf("ledger path base = %q", got)
	}
}

func TestRunLedgerBlocksDependents(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := NewRunLedger("run-1", "all", "", ConcurrencyLimits{}, []TaskLedgerEntry{
		{ID: "provider", Kind: "providerServices", Label: "provider"},
		{ID: "infra.cluster-a", Kind: "clusterInfra", Label: "infra", Dependencies: []string{"provider"}},
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
