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
	if err := saveRunLeaseFixture(runsDir, leaseA); err != nil {
		t.Fatalf("saveRunLeaseFixture A: %v", err)
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

func TestRunLeaseHeartbeatSerializesOwnerCheckWithTakeover(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()
	leaseA := NewRunLease("run-A", now.Add(-10*time.Minute))
	if err := saveRunLeaseFixture(runsDir, leaseA); err != nil {
		t.Fatalf("saveRunLeaseFixture A: %v", err)
	}

	previousAlive := runLeaseProcessAlive
	previousChecked := runLeaseOwnerChecked
	previousAttempted := runLeaseLockAttempted
	runLeaseProcessAlive = func(int) bool { return false }
	defer func() {
		runLeaseProcessAlive = previousAlive
		runLeaseOwnerChecked = previousChecked
		runLeaseLockAttempted = previousAttempted
	}()

	ownerChecked := make(chan struct{})
	releaseOwner := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(releaseOwner)
		}
	}()
	runLeaseOwnerChecked = func(operation string) {
		if operation != "save" {
			return
		}
		close(ownerChecked)
		<-releaseOwner
	}
	leaseA.HeartbeatAt = now
	heartbeatDone := make(chan error, 1)
	go func() {
		heartbeatDone <- SaveRunLeaseIfOwner(runsDir, leaseA)
	}()
	waitRunLeaseSignal(t, ownerChecked, "heartbeat owner check")

	lockAttempted := make(chan struct{}, 1)
	runLeaseLockAttempted = func() {
		lockAttempted <- struct{}{}
	}
	takeoverDone := make(chan error, 1)
	go func() {
		takeoverDone <- AcquireRunLease(runsDir, NewRunLease("run-B", now), now)
	}()
	waitRunLeaseSignal(t, lockAttempted, "takeover lock attempt")
	select {
	case err := <-takeoverDone:
		t.Fatalf("takeover completed while heartbeat transaction held the lock: %v", err)
	default:
	}

	close(releaseOwner)
	released = true
	if err := waitRunLeaseResult(t, heartbeatDone, "heartbeat"); err != nil {
		t.Fatalf("SaveRunLeaseIfOwner: %v", err)
	}
	if err := waitRunLeaseResult(t, takeoverDone, "takeover"); err != nil {
		t.Fatalf("AcquireRunLease B: %v", err)
	}
	assertRunLeaseHolder(t, runsDir, "run-B")
}

func TestRunLeaseCleanupSerializesOwnerCheckWithTakeover(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()
	leaseA := NewRunLease("run-A", now.Add(-10*time.Minute))
	if err := saveRunLeaseFixture(runsDir, leaseA); err != nil {
		t.Fatalf("saveRunLeaseFixture A: %v", err)
	}

	previousAlive := runLeaseProcessAlive
	previousChecked := runLeaseOwnerChecked
	previousAttempted := runLeaseLockAttempted
	runLeaseProcessAlive = func(int) bool { return false }
	defer func() {
		runLeaseProcessAlive = previousAlive
		runLeaseOwnerChecked = previousChecked
		runLeaseLockAttempted = previousAttempted
	}()

	ownerChecked := make(chan struct{})
	releaseOwner := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(releaseOwner)
		}
	}()
	runLeaseOwnerChecked = func(operation string) {
		if operation != "remove" {
			return
		}
		close(ownerChecked)
		<-releaseOwner
	}
	cleanupDone := make(chan error, 1)
	go func() {
		cleanupDone <- RemoveRunLeaseIfOwner(runsDir, "run-A")
	}()
	waitRunLeaseSignal(t, ownerChecked, "cleanup owner check")

	lockAttempted := make(chan struct{}, 1)
	runLeaseLockAttempted = func() {
		lockAttempted <- struct{}{}
	}
	takeoverDone := make(chan error, 1)
	go func() {
		takeoverDone <- AcquireRunLease(runsDir, NewRunLease("run-B", now), now)
	}()
	waitRunLeaseSignal(t, lockAttempted, "takeover lock attempt")
	select {
	case err := <-takeoverDone:
		t.Fatalf("takeover completed while cleanup transaction held the lock: %v", err)
	default:
	}

	close(releaseOwner)
	released = true
	if err := waitRunLeaseResult(t, cleanupDone, "cleanup"); err != nil {
		t.Fatalf("RemoveRunLeaseIfOwner: %v", err)
	}
	if err := waitRunLeaseResult(t, takeoverDone, "takeover"); err != nil {
		t.Fatalf("AcquireRunLease B: %v", err)
	}
	assertRunLeaseHolder(t, runsDir, "run-B")
}

func waitRunLeaseSignal(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
	}
}

func waitRunLeaseResult(t *testing.T, result <-chan error, operation string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
		return nil
	}
}

func assertRunLeaseHolder(t *testing.T, runsDir, runID string) {
	t.Helper()
	got, found, err := LoadRunLease(runsDir)
	if err != nil || !found {
		t.Fatalf("LoadRunLease: found=%v err=%v", found, err)
	}
	if got.RunID != runID {
		t.Fatalf("lease holder = %q, want %q", got.RunID, runID)
	}
}

func TestCancelRunLedgerLeavesNewHolderLeaseIntact(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()

	ledgerA := NewRunLedger("run-A", "cluster", "", ConcurrencyLimits{}, []TaskLedgerEntry{
		{ID: "t1", Status: TaskStatusRunning},
	}, now)
	if err := saveRunLeaseFixture(runsDir, NewRunLease("run-B", now)); err != nil {
		t.Fatalf("saveRunLeaseFixture B: %v", err)
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
	if err := saveRunLeaseFixture(runsDir, lease); err != nil {
		t.Fatalf("saveRunLeaseFixture: %v", err)
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
	if err := saveRunLeaseFixture(dir, lease); err != nil {
		t.Fatalf("saveRunLeaseFixture: %v", err)
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
	if err := saveRunLeaseFixture(dir, lease); err != nil {
		t.Fatalf("saveRunLeaseFixture: %v", err)
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
