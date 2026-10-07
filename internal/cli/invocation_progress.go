package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// InvocationProgress is what an invocation reports while its work runs: the
// lifecycle progress rows and the log location a power verb also names, and
// the finisher the runner calls before any result is written.
type InvocationProgress interface {
	lifecycle.ProgressReporter
	Finish()
}

// NewInvocationProgress selects the reporter the invocation's output mode
// admits. A JSON invocation writes exactly one document, so its reporter
// writes nothing; any other streams to out.
func NewInvocationProgress(out io.Writer, columns func() int, jsonMode bool) InvocationProgress {
	if jsonMode {
		return silentProgress{}
	}
	return NewLifecycleProgressPresenter(out, columns)
}

type silentProgress struct{}

func (silentProgress) ReportProgress(context.Context, lifecycle.ProgressEvent) {}
func (silentProgress) ReportLogLocation(context.Context, string)               {}
func (silentProgress) Finish()                                                 {}

// NewMediaProgress reports a media command's progress through the invocation's
// progress reporter, so it takes the same output-mode selection: a check
// settles under Checks and every other step under Progress. A nil reporter
// reports nothing.
func NewMediaProgress(reporter lifecycle.ProgressReporter) media.Reporter {
	return mediaProgress{reporter: reporter}
}

type mediaProgress struct{ reporter lifecycle.ProgressReporter }

func (p mediaProgress) ReportProgress(ctx context.Context, event media.ProgressEvent) {
	if p.reporter == nil {
		return
	}
	phase := lifecycle.EffectPhase
	if event.Check {
		phase = lifecycle.CheckPhase
	}
	p.reporter.ReportProgress(ctx, lifecycle.ProgressEvent{
		Phase: phase, Block: event.Step, Description: event.Label, Detail: event.Detail,
		Status: event.Status, Position: event.Position, Total: event.Total,
	})
}
