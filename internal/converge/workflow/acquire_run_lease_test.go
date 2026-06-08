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
	// NewRunLease stamps the current host+pid, so the held lease reads as a live
	// run with a fresh heartbeat.
	if err := SaveRunLease(runsDir, NewRunLease("run-held", now)); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
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
	// A heartbeat older than the stale window marks the prior run dead.
	if err := SaveRunLease(runsDir, NewRunLease("run-stale", now.Add(-10*time.Minute))); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
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

// TestRunApplyTaskGraphFailsClosedWhenLeaseHeld proves the scheduler refuses to
// mutate when another run already holds a fresh lease, instead of overwriting it
// and running a second privileged apply concurrently.
func TestRunApplyTaskGraphFailsClosedWhenLeaseHeld(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	now := time.Now()
	if err := SaveRunLease(runsDir, NewRunLease("run-other", now)); err != nil {
		t.Fatalf("SaveRunLease: %v", err)
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
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseProvider}}, "", []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1}, nil, func(io.Writer, io.Writer) ansible.Runner {
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
