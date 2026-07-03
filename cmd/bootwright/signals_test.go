package main

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// TestSignalContextCancelsOnSignal verifies the first SIGTERM cancels the
// returned context — the trigger that arms bootwright's process-group reaping.
// It sends exactly one signal so the restored default disposition never fires.
func TestSignalContextCancelsOnSignal(t *testing.T) {
	ctx, stop := signalContext(context.Background())
	defer stop()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM to self: %v", err)
	}

	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != context.Canceled {
			t.Fatalf("ctx.Err() = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("context was not cancelled after SIGTERM")
	}
}

// TestSignalContextStopReleasesWithoutSignal verifies stop cancels the context
// and unregisters the handler on the normal (no-signal) exit path.
func TestSignalContextStopReleasesWithoutSignal(t *testing.T) {
	ctx, stop := signalContext(context.Background())
	stop()

	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != context.Canceled {
			t.Fatalf("ctx.Err() = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("context was not cancelled after stop")
	}
}
