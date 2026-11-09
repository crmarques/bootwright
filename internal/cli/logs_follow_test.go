package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/logview"
)

type countingFailingWriter struct{ writes int }

func (w *countingFailingWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("the reader of stdout went away")
}

func TestAnInterruptedFollowExits130AfterWhatItStreamed(t *testing.T) {
	var cancel context.CancelCauseFunc
	record := &dispatchRecord{
		err:      context.Canceled,
		followed: []followedLine{{Line: logview.Line{Source: "dns", Text: "printed before the interrupt"}}},
	}
	record.afterCall = func() { cancel(ErrInterrupted) }
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
		ctx, cancel = context.WithCancelCause(ctx)
		return ctx, func() { cancel(nil) }
	}}).Run(context.Background(), []string{"logs", "--context", "lab", "--follow"})
	if code != 130 || record.calls != 1 || out.String() != "[dns] printed before the interrupt\n" || !strings.Contains(errOut.String(), "runtime.interrupted") {
		t.Fatalf("an interrupted follow exited %d after %d calls, stdout %q, stderr %q", code, record.calls, out.String(), errOut.String())
	}
}

func TestAFollowWhoseStdoutFailsExits1Silently(t *testing.T) {
	record := &dispatchRecord{
		followed: []followedLine{{Line: logview.Line{Text: "09:00:00  -  opened"}, event: true}},
		result:   commandResult{logsFollow: &logview.Summary{Context: "lab", State: "done"}},
	}
	out := &countingFailingWriter{}
	var errOut bytes.Buffer
	code := New(Config{Out: out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"logs", "--context", "lab", "--follow"})
	if code != 1 || out.writes != 1 || errOut.Len() != 0 {
		t.Fatalf("a follow whose stdout failed exited %d after %d writes with stderr %q", code, out.writes, errOut.String())
	}
}
