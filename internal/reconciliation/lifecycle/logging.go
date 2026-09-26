package lifecycle

import (
	"context"
	"sync"

	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// logBoundary is one invocation's required logging boundary over the operation
// its logs belong to. The first failure to create, append to or finalize a
// required log latches it: the invocation admits nothing further, requests
// cancellation of what is in flight, and records the operation's durable log
// fault, which stays set until a later invocation restores the boundary.
// Blocks running together share one boundary, so the latch is guarded.
type logBoundary struct {
	store     OperationStore
	operation string
	cancel    context.CancelFunc
	mutex     sync.Mutex
	fault     error
}

func newLogBoundary(store OperationStore, operation string, cancel context.CancelFunc) *logBoundary {
	return &logBoundary{store: store, operation: operation, cancel: cancel}
}

// fail latches a required-log failure and returns the fault the invocation
// reports. The fault is recorded under the boundary cancellation does not
// reach, because the cancellation it requests must not suppress its record.
func (b *logBoundary) fail(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.fault != nil {
		return b.fault
	}
	b.fault = logFault(err)
	if b.cancel != nil {
		b.cancel()
	}
	// A record that cannot be written here is written by whatever settles the
	// operation next, and a later invocation proves the boundary again before
	// it does any work, so the latch never depends on this write alone.
	_ = markLogFault(recordingContext(ctx), b.store, b.operation)
	return b.fault
}

// err reports the latched fault, if any.
func (b *logBoundary) err() error {
	if b == nil {
		return nil
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.fault
}

func (b *logBoundary) faulted() bool { return b.err() != nil }

// open creates a required log and writes its opening record. Logs are
// recorded rather than performed, so an interrupt never reads as a log fault.
func (b *logBoundary) open(ctx context.Context, target string) (*operationstore.Log, error) {
	log, err := b.store.OpenLog(recordingContext(ctx), target)
	if err != nil {
		return nil, b.fail(ctx, err)
	}
	return log, nil
}

func (b *logBoundary) append(ctx context.Context, log *operationstore.Log, record operationstore.LogRecord) error {
	return b.fail(ctx, log.Append(recordingContext(ctx), record))
}

func (b *logBoundary) close(ctx context.Context, log *operationstore.Log) {
	_ = b.fail(ctx, log.Close(recordingContext(ctx)))
}

// markLogFault sets an operation's durable log fault. It is sticky: only a
// restoration clears it.
func markLogFault(ctx context.Context, store OperationStore, id string) error {
	operation, err := store.ReadOperation(ctx, id)
	if err != nil || operation.LogFault {
		return err
	}
	operation.LogFault = true
	return store.UpdateOperation(ctx, operation)
}

// restore proves an operation's private logging boundary before an invocation
// observes, probes, registers or performs anything that depends on it: it
// reopens the operation log and durably writes its opening record, and only
// then clears a log fault an earlier invocation recorded. It opens only the
// operation log, which every invocation reopens, and never an attempt or
// resolution log, so it repairs nothing; a failure sets or preserves the fault
// and leaves everything else as it was.
func restore(ctx context.Context, store OperationStore, operation operationstore.Operation) (operationstore.Operation, *operationstore.Log, error) {
	boundary := newLogBoundary(store, operation.ID, nil)
	log, err := boundary.open(ctx, operationstore.OperationLogPath(operation.ID))
	if err != nil {
		return operation, nil, err
	}
	if !operation.LogFault {
		return operation, log, nil
	}
	operation.LogFault = false
	if err := store.UpdateOperation(ctx, operation); err != nil {
		boundary.close(ctx, log)
		return operation, nil, err
	}
	return operation, log, nil
}
