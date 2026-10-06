//go:build linux && amd64

package privilege

import (
	"context"
	"errors"
	"syscall"
	"testing"
)

// Once a session's child ran, its status is the session's even after an
// interrupt: a remote command that exited on the relayed signal, the client's
// own 255 and a command that finished first all stand, with nothing added. An
// interrupt that stopped sudo before the session, and an invocation that is no
// session, still report it.
func TestAnInterruptedSessionKeepsTheChildsStatus(t *testing.T) {
	for _, test := range []elevationCase{
		{name: "a remote command ended by the relayed signal", session: true, terminal: true, child: scriptedChild{code: 128 + int(syscall.SIGTERM)}, exit: 143},
		{name: "the client's own failure", session: true, child: scriptedChild{stderr: []string{startAnnouncement}, code: 255}, exit: 255},
		{name: "a remote command that finished first", session: true, child: scriptedChild{stderr: []string{startAnnouncement}}, exit: 0},
		{name: "an interrupt that stopped sudo before the session", session: true, child: scriptedChild{err: errors.New("sudo was interrupted")},
			exit: 130, code: "runtime.interrupted", message: "operation interrupted"},
		{name: "no session still reports the interrupt", child: scriptedChild{stderr: []string{startAnnouncement}, code: 128 + int(syscall.SIGTERM)},
			exit: 130, code: "runtime.interrupted", message: "operation interrupted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			test.child.during = func() { cancel(signalCause{signal: syscall.SIGINT}) }
			test.check(t, ctx)
		})
	}
}
