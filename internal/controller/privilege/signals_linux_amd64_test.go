//go:build linux && amd64

package privilege

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestCancellationPreservesOriginalSignal(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
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

// A hangup delivered to the unprivileged supervisor alone would end it by
// default without relaying anything to the elevated child, so it cancels the
// privileged operation with SIGHUP as its relayed signal.
func TestHangupIsRelayedToThePrivilegedOperation(t *testing.T) {
	if signal.Ignored(syscall.SIGHUP) {
		t.Skip("SIGHUP is ignored by this test process, as nohup leaves it")
	}
	// A second subscriber keeps an unhandled hangup from ending the test
	// binary, so a missing registration fails here instead.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGHUP)
	defer signal.Stop(guard)
	ctx, finish := Begin(context.Background())
	defer finish()
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a hangup did not cancel the privileged operation")
	}
	if got := cancellationSignal(ctx); got != syscall.SIGHUP {
		t.Fatalf("the hangup was relayed as %v", got)
	}
}
