//go:build linux && amd64

package privilege

import (
	"context"
	"syscall"
	"testing"
)

// An interrupt ends in runtime.interrupted with status 130 whatever the
// signal was, and a child that already reported its own interrupt, or wrote
// anything but a held line, or ran on the operator's terminal, gets no second
// report and ends with the child's own status, whichever signal sudo relayed
// to it, so a JSON document's exitCode is the process's status.
func TestElevationOutcomesAfterAnInterrupt(t *testing.T) {
	const interrupted = "[FAIL] runtime.interrupted: operation interrupted\n"
	const document = `{"exitCode":130}` + "\n"
	for _, test := range []struct {
		elevationCase
		signal syscall.Signal
		before bool
	}{
		{elevationCase: elevationCase{name: "a JSON SIGTERM before the start", json: true, child: scriptedChild{code: 128 + int(syscall.SIGTERM)}, exit: 130, code: "runtime.interrupted", message: "operation interrupted"}, signal: syscall.SIGTERM},
		{elevationCase: elevationCase{name: "a SIGTERM before sudo runs", json: true, exit: 130, code: "runtime.interrupted", message: "operation interrupted"}, signal: syscall.SIGTERM, before: true},
		{elevationCase: elevationCase{name: "a human interrupt before the start", child: scriptedChild{stderr: []string{hostWarning}, code: 128 + int(syscall.SIGINT)}, exit: 130, code: "runtime.interrupted", message: "operation interrupted"}, signal: syscall.SIGINT},
		{elevationCase: elevationCase{name: "a child that reported its own interrupt", child: scriptedChild{stderr: []string{startAnnouncement, interrupted}, code: 130}, exit: 130, stderr: interrupted}, signal: syscall.SIGINT},
		{elevationCase: elevationCase{name: "an unannounced child that reported its own interrupt after a warning", child: scriptedChild{stderr: []string{hostWarning, interrupted}, code: 130}, exit: 130, stderr: hostWarning + interrupted}, signal: syscall.SIGINT},
		{elevationCase: elevationCase{name: "an interactive child on the operator's terminal", terminal: true, errorTerminal: true, child: scriptedChild{stderr: []string{interrupted}, code: 130}, exit: 130, stderr: interrupted}, signal: syscall.SIGINT},
		{elevationCase: elevationCase{name: "a JSON child that reported its own SIGTERM", json: true, child: scriptedChild{stdout: document, stderr: []string{startAnnouncement}, code: 130}, exit: 130}, signal: syscall.SIGTERM},
		{elevationCase: elevationCase{name: "a JSON child that reported its own hangup", json: true, child: scriptedChild{stdout: document, stderr: []string{startAnnouncement}, code: 130}, exit: 130}, signal: syscall.SIGHUP},
		{elevationCase: elevationCase{name: "a human child that reported its own SIGTERM", child: scriptedChild{stderr: []string{startAnnouncement, interrupted}, code: 130}, exit: 130, stderr: interrupted}, signal: syscall.SIGTERM},
		{elevationCase: elevationCase{name: "an interactive child that reported its own hangup", terminal: true, errorTerminal: true, child: scriptedChild{stderr: []string{interrupted}, code: 130}, exit: 130, stderr: interrupted}, signal: syscall.SIGHUP},
		{elevationCase: elevationCase{name: "a started human child killed at the deadline", child: scriptedChild{stderr: []string{startAnnouncement}, code: 128 + int(syscall.SIGKILL)}, exit: 130, code: "runtime.interrupted", message: "operation interrupted"}, signal: syscall.SIGINT},
		{elevationCase: elevationCase{name: "an interactive child killed at the deadline", terminal: true, errorTerminal: true, child: scriptedChild{code: 128 + int(syscall.SIGKILL)}, exit: 130, code: "runtime.interrupted", message: "operation interrupted"}, signal: syscall.SIGINT},
		{elevationCase: elevationCase{name: "a JSON child killed at the deadline after its document", json: true, child: scriptedChild{stdout: document, stderr: []string{startAnnouncement}, code: 128 + int(syscall.SIGKILL)}, exit: 130}, signal: syscall.SIGTERM},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			interrupt := func() { cancel(signalCause{signal: test.signal}) }
			if test.before {
				interrupt()
			} else {
				test.child.during = interrupt
			}
			test.check(t, ctx)
		})
	}
}
