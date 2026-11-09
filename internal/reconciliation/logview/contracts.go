package logview

import (
	"context"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// Workspace reads a context's logs under its shared root lock, held only while
// the callback runs, and lends no input, evidence or controller state. A file
// it opens stays readable after the callback returns, outside every lock.
type Workspace interface {
	ReadLogs(ctx context.Context, name string, callback func(View) error) error
	OpenRun(ctx context.Context, name, run string) (Stream, bool, error)
	OpenSetup(ctx context.Context, setup string) (Stream, bool, error)
}

// View is valid only inside its ReadLogs callback. Mutating reports whether an
// exclusive context lease holder is alive, by a non-blocking test released at
// once.
type View interface {
	Records() operationstore.Area
	Logs() operationstore.Area
	Open(ctx context.Context, path string) (LogFile, bool, error)
	Mutating(ctx context.Context) (bool, error)
}

// LogFile.ReadRange returns at most maximum bytes from offset, fewer only at
// the end of the file.
type LogFile interface {
	ReadRange(offset int64, maximum int) ([]byte, error)
	Size() (int64, error)
	Close() error
}

// Stream is one bounded run's or setup's output. Held reports whether its
// writer still runs, by a non-blocking test released at once.
type Stream interface {
	LogFile
	Name() string
	Reference() string
	Held(ctx context.Context) (bool, error)
}

type Sink interface {
	Event(Line) error
	Output(Line) error
}

type OperationStore interface {
	Index(ctx context.Context) (operationstore.Index, error)
	ReadOperation(ctx context.Context, id string) (operationstore.Operation, error)
	ReadPlan(ctx context.Context, id string) (reconciliation.Plan, error)
	Block(ctx context.Context, id, block string) (operationstore.BlockRecord, error)
	LatestOutput(ctx context.Context, id, block string, attempt int) (operationstore.OutputFile, bool, error)
}

type OperationStoreFactory func(records, logs operationstore.Area) OperationStore

type Pause func(context.Context, time.Duration) error

type CurrentSelection func(context.Context) (string, error)
