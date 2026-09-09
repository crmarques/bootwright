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
func beginSignalOperation(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
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
