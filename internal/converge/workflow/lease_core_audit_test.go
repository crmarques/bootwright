package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnershipCheckedLeaseOpsLeaveNewHolderIntact(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()

	previous := runLeaseProcessAlive
	runLeaseProcessAlive = func(int) bool { return false }
	defer func() { runLeaseProcessAlive = previous }()

	leaseA := NewRunLease("run-A", now.Add(-10*time.Minute))
	if err := SaveRunLease(runsDir, leaseA); err != nil {
		t.Fatalf("SaveRunLease A: %v", err)
	}
	if err := AcquireRunLease(runsDir, NewRunLease("run-B", now), now); err != nil {
		t.Fatalf("AcquireRunLease B over stale A: %v", err)
	}

	leaseA.HeartbeatAt = now
	if err := SaveRunLeaseIfOwner(runsDir, leaseA); !errors.Is(err, ErrLeaseNotOwned) {
		t.Fatalf("SaveRunLeaseIfOwner after takeover = %v, want ErrLeaseNotOwned", err)
	}
	got, found, err := LoadRunLease(runsDir)
	if err != nil || !found {
		t.Fatalf("LoadRunLease: found=%v err=%v", found, err)
	}
	if got.RunID != "run-B" {
		t.Fatalf("heartbeat clobbered new holder: runID=%q, want run-B", got.RunID)
	}

	if err := RemoveRunLeaseIfOwner(runsDir, "run-A"); !errors.Is(err, ErrLeaseNotOwned) {
		t.Fatalf("RemoveRunLeaseIfOwner after takeover = %v, want ErrLeaseNotOwned", err)
	}
	got, found, err = LoadRunLease(runsDir)
	if err != nil || !found {
		t.Fatalf("LoadRunLease after cleanup: found=%v err=%v", found, err)
	}
	if got.RunID != "run-B" {
		t.Fatalf("cleanup deleted new holder: runID=%q, want run-B", got.RunID)
	}
}

func TestCancelRunLedgerLeavesNewHolderLeaseIntact(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()

	ledgerA := NewRunLedger("run-A", "cluster", "", ConcurrencyLimits{}, []TaskLedgerEntry{
		{ID: "t1", Status: TaskStatusRunning},
	}, now)
	if err := SaveRunLease(runsDir, NewRunLease("run-B", now)); err != nil {
		t.Fatalf("SaveRunLease B: %v", err)
	}

	if _, err := CancelRunLedger(runsDir, ledgerA, "superseded", now); err != nil {
		t.Fatalf("CancelRunLedger: %v", err)
	}

	got, found, err := LoadRunLease(runsDir)
	if err != nil || !found {
		t.Fatalf("LoadRunLease: found=%v err=%v", found, err)
	}
	if got.RunID != "run-B" {
		t.Fatalf("CancelRunLedger deleted the new holder's lease: runID=%q, want run-B", got.RunID)
	}
}

func TestOwnershipCheckedLeaseOpsHonorOwner(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()
	lease := NewRunLease("run-1", now)
	if err := SaveRunLease(runsDir, lease); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}
	lease.HeartbeatAt = now.Add(time.Minute)
	if err := SaveRunLeaseIfOwner(runsDir, lease); err != nil {
		t.Fatalf("SaveRunLeaseIfOwner own lease: %v", err)
	}
	got, _, err := LoadRunLease(runsDir)
	if err != nil {
		t.Fatalf("LoadRunLease: %v", err)
	}
	if !got.HeartbeatAt.Equal(lease.HeartbeatAt.UTC()) {
		t.Fatalf("heartbeat not refreshed: got %v, want %v", got.HeartbeatAt, lease.HeartbeatAt.UTC())
	}
	if err := RemoveRunLeaseIfOwner(runsDir, "run-1"); err != nil {
		t.Fatalf("RemoveRunLeaseIfOwner own lease: %v", err)
	}
	if _, found, err := LoadRunLease(runsDir); err != nil || found {
		t.Fatalf("lease not removed: found=%v err=%v", found, err)
	}
}

func TestAssessRunActivityAliveLocalProcessNotStale(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := NewRunLedger("run-1", "cluster", "", ConcurrencyLimits{}, nil, now)
	dir := t.TempDir()

	lease := NewRunLease("run-1", now)
	lease.ProcessStart = "start-token-1"
	lease.HeartbeatAt = now.Add(-ApplyLeaseStaleAfter - time.Hour)
	if err := SaveRunLease(dir, lease); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}
	prevAlive := runLeaseProcessAlive
	prevToken := runLeaseProcessStartToken
	runLeaseProcessAlive = func(int) bool { return true }
	runLeaseProcessStartToken = func(int) (string, bool) { return "start-token-1", true }
	defer func() { runLeaseProcessAlive = prevAlive; runLeaseProcessStartToken = prevToken }()

	activity, err := AssessRunActivity(dir, ledger, now)
	if err != nil {
		t.Fatalf("AssessRunActivity: %v", err)
	}
	if activity.State != RunActivityActive {
		t.Fatalf("alive identity-matched process with stale heartbeat = %+v, want active", activity)
	}
}

func TestAssessRunActivityReusedPIDDoesNotWedgeLease(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := NewRunLedger("run-1", "cluster", "", ConcurrencyLimits{}, nil, now)
	dir := t.TempDir()

	lease := NewRunLease("run-1", now)
	lease.ProcessStart = "start-token-old"
	lease.HeartbeatAt = now.Add(-ApplyLeaseStaleAfter - time.Hour)
	if err := SaveRunLease(dir, lease); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
	}
	prevAlive := runLeaseProcessAlive
	prevToken := runLeaseProcessStartToken
	runLeaseProcessAlive = func(int) bool { return true }
	runLeaseProcessStartToken = func(int) (string, bool) { return "start-token-new", true }
	defer func() { runLeaseProcessAlive = prevAlive; runLeaseProcessStartToken = prevToken }()

	activity, err := AssessRunActivity(dir, ledger, now)
	if err != nil {
		t.Fatalf("AssessRunActivity: %v", err)
	}
	if activity.State != RunActivityStale {
		t.Fatalf("reused-PID lease with stale heartbeat = %+v, want stale (self-heal)", activity)
	}
}

func TestSweepStaleRuntimeSecretsRemovesNonLiveRunsOnly(t *testing.T) {
	runsDir := t.TempDir()
	const liveRunID = "apply-live"
	const staleRunID = "apply-stale"

	staleSecrets := filepath.Join(runsDir, "history", staleRunID, "tasks", "t1", "runtime", "secrets")
	staleSecretsNested := filepath.Join(runsDir, "history", staleRunID, "tasks", "t2", "artifacts", "runtime", "secrets")
	staleKeep := filepath.Join(runsDir, "history", staleRunID, "tasks", "t1", "rendered")
	liveSecrets := filepath.Join(runsDir, "history", liveRunID, "tasks", "t1", "runtime", "secrets")
	for _, dir := range []string{staleSecrets, staleSecretsNested, staleKeep, liveSecrets} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bmc-password"), []byte("plaintext"), 0o600); err != nil {
			t.Fatalf("write %s: %v", dir, err)
		}
	}

	sweepStaleRuntimeSecrets(runsDir, liveRunID)

	for _, gone := range []string{staleSecrets, staleSecretsNested} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("stale runtime secrets survived at %s (err=%v)", gone, err)
		}
	}
	if _, err := os.Stat(staleKeep); err != nil {
		t.Fatalf("non-secret artifact of stale run was removed: %v", err)
	}
	if _, err := os.Stat(liveSecrets); err != nil {
		t.Fatalf("live run runtime secrets were removed: %v", err)
	}
}
