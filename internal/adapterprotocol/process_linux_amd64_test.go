//go:build linux && amd64

package adapterprotocol

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// cancelingJudge admits every record and asks for its acknowledgement, but
// cancels the run while it judges, so the run's context is done before the
// acknowledgement could be written.
type cancelingJudge struct {
	cancel context.CancelFunc
	spare  bool
}

func (j cancelingJudge) Judge(context.Context, Record, Moment) Verdict {
	j.cancel()
	return Verdict{Valid: true, Acknowledge: true}
}

func (j cancelingJudge) Spare() bool { return j.spare }

func (cancelingJudge) Acknowledged(Record) {}

func (cancelingJudge) Drain(bool) time.Duration { return 2 * time.Second }

// A run whose context is done never acknowledges, whichever way its runner
// ends a canceled tree: the adapter's reader sees its acknowledgement channel
// close instead of proceed. The reader writes the record from a session of
// its own, so neither the group kill nor the adapter's signal ends it before
// it records what it read.
func TestNoAcknowledgementIsWrittenOnceTheRunIsCanceled(t *testing.T) {
	for _, check := range []struct {
		name      string
		admission Admission
		stop      bool
		spare     bool
	}{
		{"a lifecycle run stops its tree", Lifecycle, true, false},
		{"a controller run stops its tree", Controller, false, false},
		{"a controller run spares its tree", Controller, false, true},
	} {
		t.Run(check.name, func(t *testing.T) {
			reply := filepath.Join(t.TempDir(), "reply")
			script := `setsid sh -c 'printf "{\"phase\":\"loaded\"}\n" >&3; exec 3>&-; ` +
				`if read -r line <&4; then printf "%s" "$line"; else printf EOF; fi > "$0.partial"; mv "$0.partial" "$0"' '` + reply + `' </dev/null >/dev/null 2>&1 & ` +
				`exec 3>&- 4<&-; wait`
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			invocation := Invocation{
				Loader: "/bin/sh", Automation: t.TempDir(), StopOnBreach: check.stop, Admission: check.admission,
				Command: func(string, ...string) *exec.Cmd { return exec.Command("/bin/sh", "-c", script) },
			}
			ending := Run(ctx, invocation, cancelingJudge{cancel: cancel, spare: check.spare})
			if ending.Kind != Canceled {
				t.Fatalf("the canceled run ended %+v, want canceled", ending)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				read, err := os.ReadFile(reply)
				if err == nil {
					if string(read) != "EOF" {
						t.Fatalf("the adapter read %q after the run was canceled, want its channel closed", read)
					}
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("the adapter's reader recorded nothing: %v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

// recordingJudge admits every record, asks for its acknowledgement and records
// each one the session reports delivered.
type recordingJudge struct{ acknowledged []string }

func (*recordingJudge) Judge(context.Context, Record, Moment) Verdict {
	return Verdict{Valid: true, Acknowledge: true}
}

func (j *recordingJudge) Acknowledged(record Record) {
	j.acknowledged = append(j.acknowledged, record.Phase)
}

func (*recordingJudge) Spare() bool { return false }

func (*recordingJudge) Drain(bool) time.Duration { return time.Second }

// failingWriter is an acknowledgement channel whose writes fail with err.
type failingWriter struct {
	err    error
	closed bool
}

func (w *failingWriter) Write([]byte) (int, error) { return 0, w.err }

func (w *failingWriter) Close() error {
	w.closed = true
	return nil
}

// An acknowledgement the runner cannot write because no process holds the
// channel's read end authorizes nothing and is no failure of its own: the
// protocol ends and the adapter's exit decides. Any other failed write may have
// reached the adapter, so its delivery is uncertain.
func TestAnUndeliveredAcknowledgementAuthorizesNothing(t *testing.T) {
	for _, check := range []struct {
		name  string
		err   error
		ended bool
	}{
		{"no reader", &fs.PathError{Op: "write", Path: "|1", Err: syscall.EPIPE}, false},
		{"another failure", &fs.PathError{Op: "write", Path: "|1", Err: syscall.EIO}, true},
	} {
		t.Run(check.name, func(t *testing.T) {
			judge, input := &recordingJudge{}, &failingWriter{err: check.err}
			run := &session{judge: judge, input: input}
			run.receive(context.Background(), Record{Phase: "loaded"})
			if len(judge.acknowledged) != 0 {
				t.Fatalf("an undelivered acknowledgement authorized %q", judge.acknowledged)
			}
			if run.ended != check.ended || check.ended && run.ending.Kind != Uncertain {
				t.Fatalf("the write failing with %v ended the run %v (%+v), want ended %v", check.err, run.ended, run.ending, check.ended)
			}
			if !check.ended {
				if !input.closed {
					t.Fatal("an undelivered acknowledgement left the channel open")
				}
				if ending := run.outcome(); ending.Kind != NoResult {
					t.Fatalf("an undelivered acknowledgement ended %+v, want no result", ending)
				}
			}
		})
	}
}

// A delivered acknowledgement is the only one the judge records.
func TestADeliveredAcknowledgementIsRecorded(t *testing.T) {
	judge := &recordingJudge{}
	run := &session{judge: judge, input: &failingWriter{}}
	run.receive(context.Background(), Record{Phase: "loaded"})
	if len(judge.acknowledged) != 1 || run.ended {
		t.Fatalf("a delivered acknowledgement recorded %q and ended %v", judge.acknowledged, run.ended)
	}
}
