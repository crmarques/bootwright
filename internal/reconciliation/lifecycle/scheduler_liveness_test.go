package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// refusedWrite is what the operation area answers for a record it cannot
// write. It names a private path, so a test can prove none of it reaches a
// diagnostic.
const refusedWrite = "open /var/lib/bootwright/contexts/lab/state/operations: no space left on device"

// firstOperation is the identity a fresh harness gives its first operation.
var firstOperation = "op-" + strings.Repeat("01", 16)

// startCounter counts the attempts one invocation starts, per block, and ends
// that invocation once a block is started twice, so an engine that re-admits
// a block it could not start fails this suite instead of spinning forever
// while it holds the root lock. refused is closed once the store has refused
// a start, so a test can keep a sibling in flight until that refusal happened.
type startCounter struct {
	mutex   sync.Mutex
	starts  map[string]int
	repeat  context.CancelFunc
	refused chan struct{}
	once    sync.Once
}

func (s *startCounter) count(block string) int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.starts[block]
}

// countedStore is the real operation store with every attempt start counted.
type countedStore struct {
	OperationStore
	counter *startCounter
}

func (s countedStore) StartAttempt(ctx context.Context, id, block string) (int, error) {
	s.counter.mutex.Lock()
	s.counter.starts[block]++
	repeated := s.counter.starts[block] > 1
	s.counter.mutex.Unlock()
	if repeated {
		s.counter.repeat()
	}
	number, err := s.OperationStore.StartAttempt(ctx, id, block)
	if err != nil {
		s.counter.once.Do(func() { close(s.counter.refused) })
	}
	return number, err
}

// counted routes the harness's next invocation through a start counter under
// a deadline, which is the bound on how long a regression can hold the test.
func counted(t *testing.T, h *harness) (context.Context, *startCounter) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	counter := &startCounter{starts: map[string]int{}, repeat: cancel, refused: make(chan struct{})}
	base := h.service.options.Operations
	h.service.options.Operations = func(area operationstore.Area) OperationStore {
		return countedStore{OperationStore: base(area), counter: counter}
	}
	return ctx, counter
}

// refuseStart makes every write of one block's numbered attempt record fail,
// which is how StartAttempt fails when the store will not take its record.
func refuseStart(h *harness, block string, number int) {
	name, _ := reconciliation.FormatNumber(number)
	h.workspace.area.mutex.Lock()
	defer h.workspace.area.mutex.Unlock()
	h.workspace.area.fail["write "+path.Join(firstOperation, "blocks", block, "attempt-"+name+".json")] = errors.New(refusedWrite)
}

func allowStart(h *harness) {
	h.workspace.area.mutex.Lock()
	defer h.workspace.area.mutex.Unlock()
	clear(h.workspace.area.fail)
}

// attemptRecords lists the attempt records one block holds.
func attemptRecords(h *harness, block string) []string {
	h.workspace.area.mutex.Lock()
	defer h.workspace.area.mutex.Unlock()
	var found []string
	for name := range h.workspace.area.files {
		if strings.HasPrefix(name, path.Join(firstOperation, "blocks", block, "attempt-")) {
			found = append(found, path.Base(name))
		}
	}
	slices.Sort(found)
	return found
}

// messages lists what a failure says, code by code, and refuses one that
// carries the store's own words onto the wire.
func messages(t *testing.T, err error) []string {
	t.Helper()
	var said []string
	for _, reported := range diagnostics.Of(err) {
		if strings.Contains(reported.Message+reported.Remediation, "/var/lib") || strings.Contains(reported.Message, "no space") {
			t.Fatalf("a diagnostic carries the store's own failure: %+v", reported)
		}
		said = append(said, reported.Code+": "+reported.Message)
	}
	return said
}

const alphaNeverStarted = "runtime.internal: the attempt of the block alpha could not record its start, so it performed no effect"

// An attempt whose start the store keeps refusing performed nothing, so the
// invocation admits nothing further, never admits that block again, waits for
// what it already started and ends: the operation stays running rather than
// paused, the block stays pending with no attempt recorded, and the refusal
// names the start that failed without the store's own words. Repeating the
// operation once the store takes the record continues it.
func TestAnAttemptTheStoreWillNotStartEndsTheInvocation(t *testing.T) {
	type returned struct {
		result *OperationResult
		err    error
	}
	for name, tc := range map[string]struct {
		bound int
		bravo reconciliation.BlockState
	}{
		"nothing else in flight":      {bound: 1, bravo: reconciliation.BlockPending},
		"a sibling already in flight": {bound: 2, bravo: reconciliation.BlockDone},
	} {
		t.Run(name, func(t *testing.T) {
			h := scheduled(t, tc.bound, definition("alpha"), definition("bravo"))
			refuseStart(h, "alpha", 1)
			ctx, counter := counted(t, h)
			// With room for two, bravo starts beside alpha and is held in
			// flight until alpha's start has been refused, so the invocation
			// can end only by waiting for it. One that returned while bravo
			// still ran would release the root lock over an executing effect
			// and record the operation from a state bravo had not proved.
			inFlight := tc.bound > 1
			entered, release := make(chan struct{}), make(chan struct{})
			let := sync.OnceFunc(func() { close(release) })
			t.Cleanup(let)
			if inFlight {
				h.capability.hold = func(block string) {
					if block == "bravo" {
						<-counter.refused
						close(entered)
						<-release
					}
				}
			}
			finished := make(chan returned, 1)
			go func() {
				result, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
				finished <- returned{result: result, err: err}
			}()
			if inFlight {
				select {
				case <-entered:
				case <-finished:
					t.Fatal("the invocation returned before bravo started")
				}
				// Nothing can end a correct invocation while bravo is held, so
				// this wait never fails one; it only gives a wrong one time to
				// return.
				select {
				case <-finished:
					t.Fatal("the invocation returned while bravo was still in flight")
				case <-time.After(200 * time.Millisecond):
				}
			}
			let()
			outcome := <-finished
			result, err := outcome.result, outcome.err
			if starts := counter.count("alpha"); starts != 1 {
				t.Fatalf("alpha was started %d times by one invocation", starts)
			}
			if ctx.Err() != nil {
				t.Fatalf("the invocation ended only when it was stopped: %v", ctx.Err())
			}
			if err == nil || result == nil {
				t.Fatalf("an apply whose attempt never started succeeded: %+v", result)
			}
			said := messages(t, err)
			if len(said) == 0 || said[0] != alphaNeverStarted {
				t.Fatalf("diagnostics = %q", said)
			}
			if result.Receipt.State != string(reconciliation.OperationRunning) || result.Receipt.Next != "continue-apply" {
				t.Fatalf("receipt = %+v", result.Receipt)
			}
			// The result is the invocation's own view, which holds what bravo
			// proved only if the invocation waited for it to settle.
			reported := map[string]string{}
			for _, block := range result.Blocks {
				reported[block.ID] = block.State
			}
			if reported["alpha"] != string(reconciliation.BlockPending) || reported["bravo"] != string(tc.bravo) {
				t.Fatalf("result blocks = %+v", result.Blocks)
			}
			if slices.Contains(h.capability.applies, "alpha") {
				t.Fatalf("applies = %v", h.capability.applies)
			}
			if tc.bravo == reconciliation.BlockPending && len(h.capability.applies) != 0 {
				t.Fatalf("a block was admitted after an attempt could not start: %v", h.capability.applies)
			}
			record, states := durableOperation(t, h)
			if record.State != reconciliation.OperationRunning || record.LogFault ||
				states["alpha"] != reconciliation.BlockPending || states["bravo"] != tc.bravo {
				t.Fatalf("durable operation = %s (fault %t) with %v", record.State, record.LogFault, states)
			}
			if records := attemptRecords(h, "alpha"); len(records) != 0 {
				t.Fatalf("an attempt that never started is recorded: %v", records)
			}
			applied, _ := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationRunning)
			if want, _ := applied.Bytes(); string(h.workspace.evidence) != string(want) {
				t.Fatalf("evidence = %q", h.workspace.evidence)
			}

			allowStart(h)
			h.capability.hold = nil
			continued, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			if err != nil || continued.Receipt.Operation != firstOperation || continued.Receipt.State != string(reconciliation.OperationDone) {
				t.Fatalf("continuation = %+v (%v)", continued, err)
			}
			if applies := slices.Sorted(slices.Values(h.capability.applies)); !slices.Equal(applies, []string{"alpha", "bravo"}) {
				t.Fatalf("applies = %v", h.capability.applies)
			}
		})
	}
}

// A retry is an attempt too. One that cannot start leaves its block failed,
// as its record still says, and so leaves the operation failed rather than
// running or paused over a block the scheduler believed pending.
func TestARetryTheStoreWillNotStartLeavesItsBlockFailed(t *testing.T) {
	h := newHarness(t, "alpha")
	h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed block reported success")
	}
	h.capability.outcomeFor = nil
	refuseStart(h, "alpha", 2)
	ctx, counter := counted(t, h)
	result, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if starts := counter.count("alpha"); starts != 1 || ctx.Err() != nil {
		t.Fatalf("alpha was started %d times by one invocation (%v)", starts, ctx.Err())
	}
	if err == nil || result == nil || result.Receipt.State != string(reconciliation.OperationFailed) {
		t.Fatalf("retry = %+v (%v)", result, err)
	}
	if said := messages(t, err); len(said) == 0 || said[0] != alphaNeverStarted {
		t.Fatalf("diagnostics = %q", said)
	}
	if len(h.capability.applies) != 1 {
		t.Fatalf("applies = %v", h.capability.applies)
	}
	record, states := durableOperation(t, h)
	if record.State != reconciliation.OperationFailed || states["alpha"] != reconciliation.BlockFailed {
		t.Fatalf("durable operation = %s with %v", record.State, states)
	}
	if records := attemptRecords(h, "alpha"); !slices.Equal(records, []string{"attempt-000001.json"}) {
		t.Fatalf("attempt records = %v", records)
	}
}

// An attempt whose capability this executable lacks cannot start either, and
// it moves nothing: the block keeps its pending record and the operation is
// not recorded failed over a block that never failed.
func TestAnAttemptWithoutItsCapabilityMovesNothing(t *testing.T) {
	h := newHarness(t, "alpha")
	refuseStart(h, "alpha", 1)
	ctx, counter := counted(t, h)
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil || counter.count("alpha") != 1 {
		t.Fatalf("an apply whose attempt never started = %v after %d starts", err, counter.count("alpha"))
	}
	allowStart(h)
	h.service.capabilities = testResolver{missing: true}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.state" || result == nil || result.Receipt.State != string(reconciliation.OperationRunning) {
		t.Fatalf("continuation = %+v (%q, %v)", result, code, err)
	}
	record, states := durableOperation(t, h)
	if record.State != reconciliation.OperationRunning || states["alpha"] != reconciliation.BlockPending {
		t.Fatalf("durable operation = %s with %v", record.State, states)
	}
	if records := attemptRecords(h, "alpha"); len(records) != 0 {
		t.Fatalf("an attempt that never started is recorded: %v", records)
	}
}

// A block this invocation already worked is never admitted again, even when
// it is still pending: that is an attempt that never started.
func TestAWorkedPendingBlockIsNotAdmittedAgain(t *testing.T) {
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{definition("alpha"), definition("bravo")})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]reconciliation.BlockState{"alpha": reconciliation.BlockPending, "bravo": reconciliation.BlockPending}
	run := Service{options: Options{Concurrency: 4}}.newScheduler(nil, nil, bundle{}, nil, operationstore.Operation{}, plan, states, nil, nil)
	run.worked["alpha"] = true
	if next, _, ok := run.next(); !ok || next.ID != "bravo" {
		t.Fatalf("admitted %q (%t) beside a worked pending block", next.ID, ok)
	}
	run.worked["bravo"] = true
	if next, _, ok := run.next(); ok {
		t.Fatalf("admitted %q again", next.ID)
	}
}

// cause keeps a failure that carries no diagnostic, under runtime.internal and
// without its own words, beside the diagnostics other blocks reported, in plan
// order; a cancellation is left for the boundary that recognizes it.
func TestTheCauseKeepsAFailureThatCarriesNoDiagnostic(t *testing.T) {
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{
		definition("alpha"), definition("bravo"), definition("charlie"), definition("delta"),
	})
	if err != nil {
		t.Fatal(err)
	}
	run := Service{}.newScheduler(nil, nil, bundle{}, nil, operationstore.Operation{}, plan, map[string]reconciliation.BlockState{}, nil, nil)
	run.settle(step{block: "delta", state: reconciliation.BlockPending, err: errors.New(refusedWrite), unstarted: true})
	run.settle(step{block: "charlie", state: reconciliation.BlockUnknown, err: fmt.Errorf("adapter: %w", context.Canceled)})
	run.settle(step{block: "bravo", state: reconciliation.BlockFailed, err: diagnostics.NewFailure("lifecycle.state", "bravo could not be served", "")})
	run.settle(step{block: "alpha", state: reconciliation.BlockUnknown, err: errors.New(refusedWrite)})
	said := messages(t, run.cause())
	want := []string{
		"runtime.internal: the block alpha stopped on a failure that escaped a typed boundary",
		"lifecycle.state: bravo could not be served",
		"runtime.internal: the attempt of the block delta could not record its start, so it performed no effect",
	}
	if !slices.Equal(said, want) {
		t.Fatalf("cause = %q, want %q", said, want)
	}

	canceled := Service{}.newScheduler(nil, nil, bundle{}, nil, operationstore.Operation{}, plan, map[string]reconciliation.BlockState{}, nil, nil)
	canceled.settle(step{block: "alpha", state: reconciliation.BlockPending, err: context.DeadlineExceeded, unstarted: true})
	if err := canceled.cause(); err != nil {
		t.Fatalf("a cancellation became a cause: %v", diagnostics.Of(err))
	}
}
