package main

import (
	"context"
	"syscall"
	"testing"
	"time"
)

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
