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

// The supervisor relays the first operator signal and waits for the elevated
// command, which bounds its own cancellation; a second SIGINT or SIGTERM is
// the operator's choice to kill it. One terminal hangup reaches the
// foreground job twice, from the shell that resends it to its jobs and from
// the kernel when that shell exits, so a hangup never escalates.
func TestASecondSignalEscalates(t *testing.T) {
	hangupIgnored := signal.Ignored(syscall.SIGHUP)
	guarded := []os.Signal{syscall.SIGTERM}
	if !hangupIgnored {
		guarded = append(guarded, syscall.SIGHUP)
	}
	guard := make(chan os.Signal, 4)
	signal.Notify(guard, guarded...)
	defer signal.Stop(guard)
	signalSelf := func(t *testing.T, signal syscall.Signal) {
		t.Helper()
		if err := syscall.Kill(os.Getpid(), signal); err != nil {
			t.Fatal(err)
		}
	}
	cancelled := func(t *testing.T, ctx context.Context) {
		t.Helper()
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("the first signal did not cancel")
		}
	}
	stillOpen := func(t *testing.T, ctx context.Context, wait time.Duration) {
		t.Helper()
		select {
		case <-escalation(ctx):
			t.Fatal("the escalation closed without a second operator signal")
		case <-time.After(wait):
		}
	}
	t.Run("a second SIGTERM", func(t *testing.T) {
		ctx, finish := Begin(context.Background())
		defer finish()
		signalSelf(t, syscall.SIGTERM)
		cancelled(t, ctx)
		if got := cancellationSignal(ctx); got != syscall.SIGTERM {
			t.Fatalf("the first signal was relayed as %v", got)
		}
		stillOpen(t, ctx, 200*time.Millisecond)
		signalSelf(t, syscall.SIGTERM)
		select {
		case <-escalation(ctx):
		case <-time.After(5 * time.Second):
			t.Fatal("a second signal did not escalate")
		}
	})
	t.Run("one signal", func(t *testing.T) {
		ctx, finish := Begin(context.Background())
		signalSelf(t, syscall.SIGTERM)
		cancelled(t, ctx)
		finish()
		stillOpen(t, ctx, 200*time.Millisecond)
	})
	t.Run("a doubled hangup", func(t *testing.T) {
		if hangupIgnored {
			t.Skip("SIGHUP is ignored by this test process, as nohup leaves it")
		}
		ctx, finish := Begin(context.Background())
		defer finish()
		signalSelf(t, syscall.SIGHUP)
		cancelled(t, ctx)
		signalSelf(t, syscall.SIGHUP)
		stillOpen(t, ctx, 300*time.Millisecond)
		signalSelf(t, syscall.SIGTERM)
		select {
		case <-escalation(ctx):
		case <-time.After(5 * time.Second):
			t.Fatal("a SIGTERM after a hangup did not escalate")
		}
	})
	if escalation(context.Background()) != nil {
		t.Fatal("a context Begin did not derive escalates")
	}
}
