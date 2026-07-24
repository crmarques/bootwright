package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func signalContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	signal.Ignore(syscall.SIGHUP)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
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
