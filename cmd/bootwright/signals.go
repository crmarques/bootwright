package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalContext returns a child of parent that is cancelled on the first
// SIGINT or SIGTERM. Cancellation is what arms bootwright's in-flight cleanup:
// the ansible runner reaps its process group (ssh/python children) on context
// cancel, and the local-root re-exec waits for that reaping to finish. Without
// this wiring the default signal disposition would terminate bootwright
// immediately, orphaning the ansible tree to PID 1.
//
// After the first signal the default disposition is restored, so a second
// Ctrl-C force-terminates the process if a cleanup step is wedged.
func signalContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			// Restore the default disposition before cancelling so a second
			// signal hard-kills instead of being swallowed by this handler.
			signal.Stop(ch)
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(ch)
		cancel()
	}
}
