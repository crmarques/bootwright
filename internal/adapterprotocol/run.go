package adapterprotocol

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"time"
)

// Invocation is one adapter run: the private interpreter's launch, the
// automation it runs and the job files it reads, and how the run ends.
type Invocation struct {
	Loader      string
	Arguments   []string
	Environment []string
	// Automation is the approved bundle's automation directory; Playbook is
	// a path under its collection's playbooks.
	Automation string
	Playbook   string
	Inventory  string
	Variables  string
	LocalTemp  string
	RemoteTemp string
	Scratch    string
	// Lifecycle passes the supervisor its lifecycle marker and arms the
	// parent-death signal, so the adapter dies with this invocation.
	Lifecycle bool
	// Lock, when set, is inherited as descriptor 5 by every adapter process.
	Lock   *os.File
	Output io.Writer
	// StopOnBreach stops the adapter's whole tree on a breach and on
	// cancellation; without it a breach only closes the acknowledgement
	// channel.
	StopOnBreach bool
	Admission    Admission
	Shape        func(Record) error
	Command      func(string, ...string) *exec.Cmd
	// Starting runs once the channels are open, just before the adapter
	// starts; its error ends the run before anything starts.
	Starting func() error
}

// Moment is when a record is judged. Open is false once the run has ended
// for any reason but the adapter's failed exit, or was canceled; Exited marks
// a record read after that failed exit, or after an acknowledgement no adapter
// process could receive, judged as if it had been read first.
type Moment struct {
	Open   bool
	Exited bool
}

// Verdict is a judge's ruling on one record: whether it is valid, whether it
// waits for an acknowledgement, and a failure it names, which ends the run.
type Verdict struct {
	Valid       bool
	Acknowledge bool
	Failure     error
}

// Judge is a runner adapter's own reading of the protocol.
type Judge interface {
	Judge(ctx context.Context, record Record, moment Moment) Verdict
	// Acknowledged runs once proceed for a record is delivered, and only
	// then: what an acknowledgement authorizes is recorded here.
	Acknowledged(record Record)
	// Spare reports that cancellation must spare the adapter's tree.
	Spare() bool
	// Drain is how long a descendant may hold the result channel once the
	// adapter exits or the run is canceled.
	Drain(canceled bool) time.Duration
}

// Kind is how a run ended.
type Kind uint8

const (
	Completed Kind = iota + 1
	Canceled
	// Invalid is a record the judge refused.
	Invalid
	// Incomplete is a record the decoder refused or a channel it could not
	// read.
	Incomplete
	// Named is the failure the judge named.
	Named
	FailedExit
	// Retained is a result channel a descendant held past the drain.
	Retained
	// RetainedOutput is the adapter's own output a descendant held past the
	// drain after the adapter exited zero.
	RetainedOutput
	// Uncertain is an acknowledgement whose delivery failed.
	Uncertain
	NoResult
	ResultChannel
	AuthorizationChannel
	NotStarted
	// Aborted is the error Starting returned.
	Aborted
)

// Ending is how a run ended, with the failure a Named or Aborted ending
// carries and a Completed ending's outcome and evidence.
type Ending struct {
	Kind     Kind
	Failure  error
	Outcome  string
	Evidence json.RawMessage
}
