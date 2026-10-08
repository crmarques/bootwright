package main

import (
	"context"
	"os"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/privilege"
)

// Registration starts only after the runner selects an implemented operation.
// privilege.Begin is the one signal subscription: a hangup cancels exactly as
// SIGTERM does, since by default it would end the process without the cleanup
// that stops and reaps the operation's adapters, and one ignored at start, as
// nohup leaves it, stays ignored. Its interrupt reaches the CLI as
// ErrInterrupted; the invocation joins every waiter on exit.
//
// A second SIGINT or SIGTERM ends the process at once with status 130 only
// where escalatesOnItsOwn proves, at that signal, that it reached this process
// once; elsewhere sudo can hand an elevated child one interrupt twice, so the
// cancellation runs on and the supervisor's own second signal kills sudo.
var (
	escalatesOnItsOwn = privilege.ForegroundOfOwnTerminal
	exitProcess       = os.Exit
)

func beginSignalOperation(parent context.Context) (context.Context, func()) {
	signaled, finish := privilege.Begin(parent)
	ctx, cancel := context.WithCancelCause(parent)
	done, escalationDone, stop := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		<-signaled.Done()
		if privilege.ExitCode(signaled, 0) != 0 {
			cancel(cli.ErrInterrupted)
		}
	}()
	go func() {
		defer close(escalationDone)
		select {
		case <-privilege.Escalated(signaled):
			if escalatesOnItsOwn() {
				exitProcess(130)
			}
		case <-stop:
		}
	}()
	return ctx, func() { close(stop); finish(); cancel(context.Canceled); <-done; <-escalationDone }
}
