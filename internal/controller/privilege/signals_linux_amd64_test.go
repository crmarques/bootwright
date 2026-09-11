//go:build linux && amd64

package privilege

import (
	"context"
	"syscall"
	"testing"
)

func TestCancellationPreservesOriginalSignal(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(signalCause{signal: signal})
		if got := cancellationSignal(ctx); got != signal {
			t.Fatalf("signal %v became %v", signal, got)
		}
		if got := ExitCode(ctx, 1); got != 128+int(signal) {
			t.Fatalf("signal %v exit %d", signal, got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if cancellationSignal(ctx) != syscall.SIGTERM || ExitCode(ctx, 1) != 1 {
		t.Fatal("ordinary cancellation changed")
	}
}

func TestSignalSubscriptionStopsAndJoins(t *testing.T) {
	ctx, finish := Begin(context.Background())
	finish()
	if ctx.Err() == nil {
		t.Fatal("invocation context remains active")
	}
}
