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

type escalationKey struct{}

// Begin is the process's one signal subscription. The unprivileged supervisor
// of a sudo invocation derives from it the cancellation it relays to the
// elevated child, and each implemented operation the CLI runs in this process
// derives its own. Its owner must finish, stopping subscriptions and joining
// the waiter. A hangup cancels like SIGTERM, since by default it would end the
// process without relaying anything to the elevated child or reaping an
// operation's adapters; one ignored at start, as nohup leaves it, stays
// ignored (os/signal).
//
// Once the context is done, the next SIGINT or SIGTERM closes its escalation
// channel, which only the supervisor reads, to kill sudo. A hangup never
// escalates: one terminal hangup reaches the foreground job twice, from the
// shell that resends it to its jobs and from the kernel when that shell exits.
func Begin(parent context.Context) (context.Context, func()) {
	escalated := make(chan struct{})
	ctx, cancel := context.WithCancelCause(withEscalation(parent, escalated))
	signals := make(chan os.Signal, 1)
	received := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if !signal.Ignored(syscall.SIGHUP) {
		received = append(received, syscall.SIGHUP)
	}
	signal.Notify(signals, received...)
	stop, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case received := <-signals:
			cancel(signalCause{signal: received.(syscall.Signal)})
		case <-ctx.Done():
		case <-stop:
			return
		}
		for {
			select {
			case received := <-signals:
				if received != syscall.SIGHUP {
					close(escalated)
					return
				}
			case <-stop:
				return
			}
		}
	}()
	return ctx, func() { signal.Stop(signals); cancel(context.Canceled); close(stop); <-finished }
}

func withEscalation(parent context.Context, escalated <-chan struct{}) context.Context {
	return context.WithValue(parent, escalationKey{}, escalated)
}

// escalation is closed by a second operator signal; it is nil, and never
// closes, for a context Begin did not derive.
func escalation(ctx context.Context) <-chan struct{} {
	escalated, _ := ctx.Value(escalationKey{}).(<-chan struct{})
	return escalated
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
