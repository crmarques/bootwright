//go:build linux && amd64

package privilege

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
// A hangup is relayed like SIGTERM, since by default it would end this
// supervisor without relaying anything to the elevated child; one ignored at
// start, as nohup leaves it, stays ignored (os/signal).
func Begin(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	signals := make(chan os.Signal, 1)
	received := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if !signal.Ignored(syscall.SIGHUP) {
		received = append(received, syscall.SIGHUP)
	}
	signal.Notify(signals, received...)
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
