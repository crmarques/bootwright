package cli

import (
	"context"
	"io"

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
