package workflow

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/ansible"
)

func TestAcquireRunLeaseFailsClosedOnFreshLease(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()
	if err := saveRunLeaseFixture(runsDir, NewRunLease("run-held", now)); err != nil {
		t.Fatalf("saveRunLeaseFixture: %v", err)
	}
	err := AcquireRunLease(runsDir, NewRunLease("run-mine", now), now)
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("AcquireRunLease over a fresh lease = %v, want a still-running error", err)
	}
	got, found, err := LoadRunLease(runsDir)
	if err != nil || !found {
		t.Fatalf("LoadRunLease: found=%v err=%v", found, err)
	}
	if got.RunID != "run-held" {
		t.Fatalf("fresh lease was overwritten: runID=%q, want run-held", got.RunID)
	}
}

func TestAcquireRunLeaseTakesOverStaleLease(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()
	previous := runLeaseProcessAlive
	runLeaseProcessAlive = func(int) bool { return false }
	defer func() { runLeaseProcessAlive = previous }()
	if err := saveRunLeaseFixture(runsDir, NewRunLease("run-stale", now.Add(-10*time.Minute))); err != nil {
		t.Fatalf("saveRunLeaseFixture: %v", err)
	}
	if err := AcquireRunLease(runsDir, NewRunLease("run-mine", now), now); err != nil {
		t.Fatalf("AcquireRunLease over a stale lease: %v", err)
	}
	got, found, err := LoadRunLease(runsDir)
	if err != nil || !found {
		t.Fatalf("LoadRunLease: found=%v err=%v", found, err)
	}
	if got.RunID != "run-mine" {
		t.Fatalf("stale lease was not taken over: runID=%q, want run-mine", got.RunID)
	}
}

func TestAcquireRunLeaseKeepsMatchingLiveProcessDespiteStaleHeartbeat(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()
	held := NewRunLease("run-held", now.Add(-10*time.Minute))
	held.ProcessStart = "held-process"
	if err := saveRunLeaseFixture(runsDir, held); err != nil {
		t.Fatalf("saveRunLeaseFixture: %v", err)
	}
	previousAlive := runLeaseProcessAlive
	previousStart := runLeaseProcessStartToken
	runLeaseProcessAlive = func(int) bool { return true }
	runLeaseProcessStartToken = func(int) (string, bool) { return "held-process", true }
	defer func() {
		runLeaseProcessAlive = previousAlive
		runLeaseProcessStartToken = previousStart
	}()

	err := AcquireRunLease(runsDir, NewRunLease("run-mine", now), now)
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("AcquireRunLease over matching live process = %v, want still-running refusal", err)
	}
	got, found, loadErr := LoadRunLease(runsDir)
	if loadErr != nil || !found || got.RunID != held.RunID {
		t.Fatalf("matching live process lease after refusal: found=%v lease=%+v err=%v", found, got, loadErr)
	}
}

func TestAcquireRunLeaseRefusesStaleLocalLeaseWithoutProcessIdentityProof(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name         string
		storedStart  string
		currentStart string
		currentOK    bool
		want         string
	}{
		{name: "stored identity missing", currentStart: "current", currentOK: true, want: "lease has no process-start identity"},
		{name: "current identity unreadable", storedStart: "stored", currentOK: false, want: "current process-start identity is unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runsDir := t.TempDir()
			stale := NewRunLease("run-stale", now.Add(-10*time.Minute))
			stale.ProcessStart = tc.storedStart
			if err := saveRunLeaseFixture(runsDir, stale); err != nil {
				t.Fatalf("saveRunLeaseFixture: %v", err)
			}
			previousAlive := runLeaseProcessAlive
			previousStart := runLeaseProcessStartToken
			runLeaseProcessAlive = func(int) bool { return true }
			runLeaseProcessStartToken = func(int) (string, bool) { return tc.currentStart, tc.currentOK }
			defer func() {
				runLeaseProcessAlive = previousAlive
				runLeaseProcessStartToken = previousStart
			}()

			err := AcquireRunLease(runsDir, NewRunLease("run-mine", now), now)
			if err == nil {
				t.Fatal("AcquireRunLease over an unverifiable local process succeeded")
			}
			for _, want := range []string{"refusing automatic takeover", tc.want, "cannot prove the original process stopped", "remove the lease only after proving"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("AcquireRunLease error = %q, want %q", err, want)
				}
			}
			got, found, loadErr := LoadRunLease(runsDir)
			if loadErr != nil || !found || got.RunID != stale.RunID {
				t.Fatalf("unverifiable local lease after refusal: found=%v lease=%+v err=%v", found, got, loadErr)
			}
		})
	}
}

func TestAcquireRunLeaseTakesOverStaleLeaseWithReusedPID(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()
	stale := NewRunLease("run-stale", now.Add(-10*time.Minute))
	stale.ProcessStart = "old-process"
	if err := saveRunLeaseFixture(runsDir, stale); err != nil {
		t.Fatalf("saveRunLeaseFixture: %v", err)
	}
	previousAlive := runLeaseProcessAlive
	previousStart := runLeaseProcessStartToken
	runLeaseProcessAlive = func(int) bool { return true }
	runLeaseProcessStartToken = func(int) (string, bool) { return "reused-process", true }
	defer func() {
		runLeaseProcessAlive = previousAlive
		runLeaseProcessStartToken = previousStart
	}()

	if err := AcquireRunLease(runsDir, NewRunLease("run-mine", now), now); err != nil {
		t.Fatalf("AcquireRunLease over reused PID lease: %v", err)
	}
	got, found, err := LoadRunLease(runsDir)
	if err != nil || !found || got.RunID != "run-mine" {
		t.Fatalf("reused PID takeover: found=%v lease=%+v err=%v", found, got, err)
	}
}

func TestAcquireRunLeaseRefusesRemoteOrUnknownStaleLease(t *testing.T) {
	now := time.Now()
	for _, ownerHost := range []string{"remote-controller", ""} {
		name := ownerHost
		if name == "" {
			name = "unknown-controller"
		}
		t.Run(name, func(t *testing.T) {
			runsDir := t.TempDir()
			stale := NewRunLease("run-stale", now.Add(-10*time.Minute))
			stale.Hostname = ownerHost
			if err := saveRunLeaseFixture(runsDir, stale); err != nil {
				t.Fatalf("saveRunLeaseFixture: %v", err)
			}
			err := AcquireRunLease(runsDir, NewRunLease("run-mine", now), now)
			if err == nil {
				t.Fatal("AcquireRunLease over a remote stale lease succeeded")
			}
			for _, want := range []string{"refusing automatic takeover", LeasePath(runsDir), "run-stale", "cannot prove", "remove the lease only after proving"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("AcquireRunLease error = %q, want %q", err, want)
				}
			}
			got, found, loadErr := LoadRunLease(runsDir)
			if loadErr != nil || !found {
				t.Fatalf("LoadRunLease after refusal: found=%v err=%v", found, loadErr)
			}
			if got.RunID != stale.RunID || got.Hostname != stale.Hostname {
				t.Fatalf("remote stale lease changed after refusal: got=%+v want=%+v", got, stale)
			}
		})
	}
}

func TestAcquireRunLeaseCreatesWhenAbsent(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()
	if err := AcquireRunLease(runsDir, NewRunLease("run-mine", now), now); err != nil {
		t.Fatalf("AcquireRunLease on an empty dir: %v", err)
	}
	if _, found, err := LoadRunLease(runsDir); err != nil || !found {
		t.Fatalf("lease was not created: found=%v err=%v", found, err)
	}
}

func TestRunApplyTaskGraphFailsClosedWhenLeaseHeld(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	now := time.Now()
	if err := saveRunLeaseFixture(runsDir, NewRunLease("run-other", now)); err != nil {
		t.Fatalf("saveRunLeaseFixture: %v", err)
	}

	state := minimalState()
	runner := &recordingApplyRunner{}
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
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, runsDir, RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseFabric}}, "", []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1}, nil, func(io.Writer, io.Writer) ansible.Runner {
		return runner
	})
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("RunApplyTaskGraph with a held lease = %v, want a still-running error", err)
	}
	if calls, _ := runner.snapshot(); len(calls) != 0 {
		t.Fatalf("apply ran %d task(s) despite a held lease: %v", len(calls), calls)
	}
	got, _, err := LoadRunLease(runsDir)
	if err != nil {
		t.Fatalf("LoadRunLease: %v", err)
	}
	if got.RunID != "run-other" {
		t.Fatalf("held lease was overwritten: runID=%q, want run-other", got.RunID)
	}
}
