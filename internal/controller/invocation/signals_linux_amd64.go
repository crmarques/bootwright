//go:build linux && amd64

package invocation

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

type signalCause struct{ signal syscall.Signal }

func (s signalCause) Error() string { return "invocation interrupted" }

// Begin derives cancellation only for the already-classified privileged
// operation. Its owner must finish, stopping subscriptions and joining the waiter.
func Begin(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case received := <-signals:
			cancel(signalCause{signal: received.(syscall.Signal)})
		case <-ctx.Done():
		}
	}()
	return ctx, func() { signal.Stop(signals); cancel(context.Canceled); <-finished }
}

func cancellationSignal(ctx context.Context) syscall.Signal {
	var cause signalCause
	if errors.As(context.Cause(ctx), &cause) {
		return cause.signal
	}
	return syscall.SIGTERM
}

func ExitCode(ctx context.Context, fallback int) int {
	var cause signalCause
	if errors.As(context.Cause(ctx), &cause) {
		return 128 + int(cause.signal)
	}
	return fallback
}
