package workflow

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCommandRunLeaseSpansPreparedGraphAndPostRunCleanup(t *testing.T) {
	runsDir := t.TempDir()
	guard, err := AcquireCommandRunLease(context.Background(), runsDir, "apply")
	if err != nil {
		t.Fatalf("AcquireCommandRunLease: %v", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = guard.Close()
		}
	}()
	opts := RunOptions{
		ClustersDir:        t.TempDir(),
		RenderedDir:        t.TempDir(),
		ManagedServicesDir: t.TempDir(),
		ProviderStateDir:   t.TempDir(),
		RunLease:           guard,
	}
	prepared, err := PrepareApplyTaskGraph(guard.Context(), runsDir, opts, nil, ConcurrencyLimits{})
	if err != nil {
		t.Fatalf("PrepareApplyTaskGraph: %v", err)
	}
	if prepared.RunID != guard.RunID {
		t.Fatalf("prepared run ID = %q, want held lease %q", prepared.RunID, guard.RunID)
	}
	if _, err := RunPreparedApplyTaskGraph(guard.Context(), nil, nil, runsDir, opts, ApplyTarget{Name: "apply"}, "", prepared, nil, nil); err != nil {
		t.Fatalf("RunPreparedApplyTaskGraph: %v", err)
	}
	lease, found, err := LoadRunLease(runsDir)
	if err != nil || !found || lease.RunID != guard.RunID {
		t.Fatalf("lease after graph: found=%v lease=%+v err=%v", found, lease, err)
	}
	if _, err := AcquireCommandRunLease(context.Background(), runsDir, "destroy"); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("competing destroy after graph = %v, want active-run refusal", err)
	}
	if err := guard.RequireOwned(); err != nil {
		t.Fatalf("post-run cleanup no longer owns lease: %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatalf("close command lease: %v", err)
	}
	closed = true
	replacement, err := AcquireCommandRunLease(context.Background(), runsDir, "destroy")
	if err != nil {
		t.Fatalf("acquire after command cleanup: %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatalf("close replacement lease: %v", err)
	}
}

func TestCommandRunLeaseFailsWhenOwnershipChanges(t *testing.T) {
	runsDir := t.TempDir()
	guard, err := AcquireCommandRunLease(context.Background(), runsDir, "destroy")
	if err != nil {
		t.Fatalf("AcquireCommandRunLease: %v", err)
	}
	replacement := NewRunLease("apply-replacement", time.Now().UTC())
	if err := SaveRunLease(runsDir, replacement); err != nil {
		t.Fatalf("replace lease: %v", err)
	}
	if err := guard.RequireOwned(); err == nil || !strings.Contains(err.Error(), "taken over") {
		t.Fatalf("RequireOwned after takeover = %v", err)
	}
	if err := guard.Close(); err == nil || !strings.Contains(err.Error(), "held by another run") {
		t.Fatalf("Close after takeover = %v", err)
	}
	got, found, err := LoadRunLease(runsDir)
	if err != nil || !found || got.RunID != replacement.RunID {
		t.Fatalf("replacement lease after stale close: found=%v lease=%+v err=%v", found, got, err)
	}
}

func TestCommandRunLeaseRegistersEveryDesiredStateMutator(t *testing.T) {
	for _, command := range []string{"apply", "destroy", "context-update", "diff-adopt", "replace-arbiter"} {
		t.Run(command, func(t *testing.T) {
			guard, err := AcquireCommandRunLease(context.Background(), t.TempDir(), command)
			if err != nil {
				t.Fatalf("AcquireCommandRunLease(%q): %v", command, err)
			}
			if !strings.HasPrefix(guard.RunID, command+"-") {
				t.Fatalf("run ID = %q, want %q prefix", guard.RunID, command+"-")
			}
			if err := guard.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
		})
	}
	if _, err := AcquireCommandRunLease(context.Background(), t.TempDir(), "future-unclassified-mutator"); err == nil || !strings.Contains(err.Error(), "unsupported mutating command") {
		t.Fatalf("unregistered mutator = %v, want fail-closed classification error", err)
	}
}

func TestSharedServiceMutationLeaseSerializesContextsAndVerbs(t *testing.T) {
	runsDir := t.TempDir()
	first, err := AcquireSharedServiceMutationLease(context.Background(), runsDir, "hub", "apply")
	if err != nil {
		t.Fatalf("AcquireSharedServiceMutationLease first: %v", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = first.Close()
		}
	}()
	if _, err := AcquireSharedServiceMutationLease(context.Background(), runsDir, "spoke", "destroy"); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("competing cross-context destroy = %v, want active shared-service lease refusal", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first shared-service lease: %v", err)
	}
	closed = true
	second, err := AcquireSharedServiceMutationLease(context.Background(), runsDir, "spoke", "destroy")
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if !strings.HasPrefix(second.RunID, "shared-services-spoke-destroy-") {
		t.Fatalf("run ID = %q", second.RunID)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second shared-service lease: %v", err)
	}
}

func TestSharedServiceMutationLeaseFailsClosedOnUnknownIntent(t *testing.T) {
	if _, err := AcquireSharedServiceMutationLease(context.Background(), t.TempDir(), "", "apply"); err == nil || !strings.Contains(err.Error(), "context name") {
		t.Fatalf("empty context = %v, want classification refusal", err)
	}
	if _, err := AcquireSharedServiceMutationLease(context.Background(), t.TempDir(), "hub", "future-mutator"); err == nil || !strings.Contains(err.Error(), "unsupported shared-service mutating command") {
		t.Fatalf("unknown intent = %v, want fail-closed classification refusal", err)
	}
}

func TestCommandRunLeaseBindContextPreservesParentAndLeaseCancellation(t *testing.T) {
	guard, err := AcquireCommandRunLease(context.Background(), t.TempDir(), "apply")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	parent, cancelParent := context.WithCancel(context.Background())
	bound, release, err := guard.BindContext(parent)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	cancelParent()
	select {
	case <-bound.Done():
	case <-time.After(time.Second):
		t.Fatal("bound context ignored caller cancellation")
	}
	release()
	if err := guard.RequireOwned(); err != nil {
		t.Fatalf("caller cancellation must not release the lease: %v", err)
	}

	bound, release, err = guard.BindContext(context.Background())
	if err != nil {
		t.Fatalf("bind second: %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	defer release()
	select {
	case <-bound.Done():
	case <-time.After(time.Second):
		t.Fatal("bound context ignored lease cancellation")
	}
}
