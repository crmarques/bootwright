package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/privilege"
)

// Registration starts only after the runner selects an implemented operation.
// privilege.Begin is the one signal subscription: a hangup cancels exactly as
// SIGTERM does, since by default it would end the process without the cleanup
// that stops and reaps the operation's adapters, and one ignored at start, as
// nohup leaves it, stays ignored. Its interrupt reaches the CLI as
// ErrInterrupted; the invocation joins both waiters on exit.
func beginSignalOperation(parent context.Context) (context.Context, func()) {
	signaled, finish := privilege.Begin(parent)
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-signaled.Done()
		if privilege.ExitCode(signaled, 0) != 0 {
			cancel(cli.ErrInterrupted)
		}
	}()
	return ctx, func() { finish(); cancel(context.Canceled); <-done }
}
