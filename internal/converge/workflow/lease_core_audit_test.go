package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestOwnershipCheckedLeaseOpsLeaveNewHolderIntact proves M2: after run B takes
// over run A's stale lease, run A's resumed heartbeat tick and its deferred
// cleanup must NOT touch run B's lease. The blind SaveRunLease/RemoveRunLease
// pair would have clobbered/deleted B's lease; the IfOwner variants no-op.
func TestOwnershipCheckedLeaseOpsLeaveNewHolderIntact(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()

	// Run A holds a stale lease whose process has died, so run B can take over.
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

	// Run A resumes and tries to refresh its heartbeat: must be a no-op signal and
	// leave B's lease on disk untouched.
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

	// Run A's deferred cleanup must not delete B's lease either.
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

// TestCancelRunLedgerLeavesNewHolderLeaseIntact proves the M2 wiring at a real
// call site: cancelling a stale run's ledger (e.g. during ReconcileCurrentApply
// before a takeover) must not delete the lease a newer run already holds.
func TestCancelRunLedgerLeavesNewHolderLeaseIntact(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()

	ledgerA := NewRunLedger("run-A", "cluster", "", ConcurrencyLimits{}, []TaskLedgerEntry{
		{ID: "t1", Status: TaskStatusRunning},
	}, now)
	// Run B currently owns the on-disk lease.
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

// TestOwnershipCheckedLeaseOpsHonorOwner confirms the IfOwner variants still act
// when the on-disk lease belongs to the caller.
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

// TestAssessRunActivityAliveLocalProcessNotStale proves M2: a same-host lease
// whose PID is verifiably alive is active regardless of heartbeat age. Before the
// fix an aged heartbeat forced the run stale, inviting a concurrent takeover of a
// still-mutating run.
func TestAssessRunActivityAliveLocalProcessNotStale(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := NewRunLedger("run-1", "cluster", "", ConcurrencyLimits{}, nil, now)
	dir := t.TempDir()

	// NewRunLease stamps this host + this (alive) PID; back-date the heartbeat well
	// past the stale window and pin an explicit identity token so the assertion is
	// deterministic on every platform (not just where /proc is readable).
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

// TestAssessRunActivityReusedPIDDoesNotWedgeLease proves the identity guard: after
// a hard crash, an unrelated live process that reuses the lease's PID must NOT keep
// the lease immortal. A mismatched start-time token falls through to the
// heartbeat-age rule so the stale lease still self-heals (and a new run can take
// over) instead of wedging every future apply/destroy.
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
	// The PID is live (reused by an unrelated process) but its identity token
	// differs from the lease's — it is not the lease's process.
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

// TestSweepStaleRuntimeSecretsRemovesNonLiveRunsOnly proves M5: leftover
// materialized runtime-secret dirs of a prior run are reclaimed while the live
// run's dirs and any non-secret artifacts survive.
func TestSweepStaleRuntimeSecretsRemovesNonLiveRunsOnly(t *testing.T) {
	runsDir := t.TempDir()
	const liveRunID = "apply-live"
	const staleRunID = "apply-stale"

	// Two nesting shapes match the two materialization sites: the storage
	// attachment (tasks/<id>/runtime/secrets) and the generic apply task
	// (tasks/<id>/artifacts/runtime/secrets).
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
