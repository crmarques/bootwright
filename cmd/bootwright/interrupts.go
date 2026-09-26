package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/crmarques/bootwright/internal/cli"
)

// Registration starts only after the runner selects an implemented operation.
// The invocation owns the signal channel and joins its single waiter on exit.
// A hangup cancels exactly as SIGTERM does: by default it would end the
// process without the cleanup that stops and reaps the operation's adapters.
// A process started with hangups ignored, as nohup starts it, was asked to
// outlive them, and subscribing would stop ignoring them (os/signal), so it is
// left alone.
func beginSignalOperation(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	interrupts := make(chan os.Signal, 1)
	received := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if !signal.Ignored(syscall.SIGHUP) {
		received = append(received, syscall.SIGHUP)
	}
	signal.Notify(interrupts, received...)
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-interrupts:
			cancel(cli.ErrInterrupted)
		case <-ctx.Done():
		}
	}()
	return ctx, func() { signal.Stop(interrupts); cancel(context.Canceled); <-done }
}
