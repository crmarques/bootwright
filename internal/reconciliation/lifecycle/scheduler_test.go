package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// overlap watches which blocks are in flight together, so a test proves what
// actually ran at the same time rather than how long anything took.
type overlap struct {
	mutex   sync.Mutex
	live    map[string]bool
	peak    int
	arrived chan string
	release chan struct{}
	opened  bool
	seen    [][]string
}

func newOverlap(capacity int) *overlap {
	return &overlap{live: map[string]bool{}, arrived: make(chan string, capacity), release: make(chan struct{})}
}

// enter records one block starting and holds it until the test releases its
// round, so the blocks a test gathers are provably in flight at one moment.
func (o *overlap) enter(block string) {
	o.mutex.Lock()
	o.live[block] = true
	o.peak = max(o.peak, len(o.live))
	o.seen = append(o.seen, slices.Sorted(maps.Keys(o.live)))
	gate, opened := o.release, o.opened
	o.mutex.Unlock()
	o.arrived <- block
	if !opened {
		<-gate
	}
}

func (o *overlap) leave(block string) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	delete(o.live, block)
}

// gather waits for exactly this many blocks to be in flight together, and
// fails rather than hangs when fewer of them ever start.
func (o *overlap) gather(t *testing.T, count int) []string {
	t.Helper()
	var blocks []string
	deadline := time.After(gatherWithin)
	for range count {
		select {
		case block := <-o.arrived:
			blocks = append(blocks, block)
		case <-deadline:
			slices.Sort(blocks)
			t.Fatalf("%d blocks were to be in flight together, yet only %v started within %s", count, blocks, gatherWithin)
		}
	}
	slices.Sort(blocks)
	return blocks
}

// gatherWithin bounds every wait on the engine, generously beyond any correct
// run, so a test of the wrong scheduler fails rather than hangs.
const gatherWithin = 30 * time.Second

// returnedWithin waits for an invocation's result within the same bound.
func returnedWithin(t *testing.T, finished <-chan error) error {
	t.Helper()
	select {
	case err := <-finished:
		return err
	case <-time.After(gatherWithin):
		t.Fatalf("the invocation did not return within %s", gatherWithin)
		return nil
	}
}

// admit releases the round now in flight and holds the next one, so a test can
// walk a graph wave by wave.
func (o *overlap) admit() {
	o.mutex.Lock()
	gate := o.release
	o.release = make(chan struct{})
	o.mutex.Unlock()
	close(gate)
}

// open releases everything, now and later, so the rest of the operation runs
// without the test standing in its way.
func (o *overlap) open() {
	o.mutex.Lock()
	gate, already := o.release, o.opened
	o.opened = true
	o.mutex.Unlock()
	if !already {
		close(gate)
	}
}

func (o *overlap) highest() int {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.peak
}

// together reports whether these blocks were ever in flight at one moment.
func (o *overlap) together(first, second string) bool {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return slices.ContainsFunc(o.seen, func(live []string) bool {
		return slices.Contains(live, first) && slices.Contains(live, second)
	})
}

// dependent is one block that waits for the blocks it names.
func dependent(id string, dependencies ...string) reconciliation.BlockDefinition {
	value := definition(id)
	slices.Sort(dependencies)
	value.Dependencies = dependencies
	return value
}

// exclusive is one block that will not run beside another naming its resource.
func exclusive(id, resource string) reconciliation.BlockDefinition {
	value := definition(id)
	value.Exclusive = []string{resource}
	return value
}

func scheduled(t *testing.T, bound int, definitions ...reconciliation.BlockDefinition) *harness {
	t.Helper()
	h := newPlannedHarness(t, definitions)
	h.service.options.Concurrency = bound
	return h
}

// Blocks the graph does not order run together, and never more of them than
// the executable's bound allows however many the plan admits.
func TestIndependentBlocksRunTogetherWithinTheBound(t *testing.T) {
	h := scheduled(t, 3, definition("alpha"), definition("bravo"), definition("charlie"),
		definition("delta"), definition("echo"))
	watch := newOverlap(5)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	// Three arrive and stay, which is the bound; a fourth would raise the peak.
	first := watch.gather(t, 3)
	if len(first) != 3 {
		t.Fatalf("first wave = %v", first)
	}
	watch.open()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if watch.highest() != 3 {
		t.Fatalf("blocks in flight reached %d, want the bound of 3", watch.highest())
	}
	if len(h.capability.applies) != 5 {
		t.Fatalf("applied %v", h.capability.applies)
	}
}

// A block never starts before every block it waits for is durably done, and
// the blocks released by one of them start together.
func TestADependentWaitsWhileItsSiblingsRunTogether(t *testing.T) {
	h := scheduled(t, 4, definition("alpha"), dependent("bravo", "alpha"),
		dependent("charlie", "alpha"), dependent("delta", "bravo"))
	watch := newOverlap(4)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	if first := watch.gather(t, 1); !slices.Equal(first, []string{"alpha"}) {
		t.Fatalf("the first wave started %v, want only the block nothing waits for", first)
	}
	watch.admit()
	if second := watch.gather(t, 2); !slices.Equal(second, []string{"bravo", "charlie"}) {
		t.Fatalf("the second wave started %v", second)
	}
	watch.open()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if !watch.together("bravo", "charlie") {
		t.Fatal("the two blocks one dependency released never ran together")
	}
	if watch.together("alpha", "bravo") || watch.together("bravo", "delta") {
		t.Fatal("a block ran beside the block it waits for")
	}
	if position := slices.Index(h.capability.applies, "alpha"); position != 0 {
		t.Fatalf("applied %v", h.capability.applies)
	}
}

// Two blocks that name one host resource never run at the same time, however
// independent the graph says they are, while a block naming none runs beside
// either of them.
func TestBlocksNamingOneResourceNeverRunTogether(t *testing.T) {
	h := scheduled(t, 4, exclusive("alpha", "path:/srv/tree"), exclusive("bravo", "path:/srv/tree"),
		definition("charlie"))
	watch := newOverlap(3)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	// Only one of the pair may start, so the pair plus the free block is two.
	watch.gather(t, 2)
	watch.open()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if watch.together("alpha", "bravo") {
		t.Fatal("two blocks that will not share a resource ran at the same time")
	}
	if watch.highest() != 2 {
		t.Fatalf("blocks in flight reached %d, want 2", watch.highest())
	}
	for _, block := range []string{"alpha", "bravo", "charlie"} {
		if !slices.Contains(h.capability.applies, block) {
			t.Fatalf("applied %v", h.capability.applies)
		}
	}
}

// A failure admits no further work, and the blocks already in flight still
// record what they proved: cancelling them would turn proved outcomes into
// unproved ones. The result names every block that failed, in plan order.
func TestAFailureStopsAdmissionAndKeepsEveryFailedBlock(t *testing.T) {
	h := scheduled(t, 4, definition("alpha"), definition("bravo"), definition("charlie"),
		dependent("delta", "alpha"))
	h.capability.errorFor = map[string]error{
		"alpha": diagnostics.NewFailure("lifecycle.state", "alpha could not be served", ""),
		"bravo": diagnostics.NewFailure("lifecycle.state", "bravo could not be served", ""),
	}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil {
		t.Fatal("a failed operation succeeded")
	}
	if result.Receipt.State != "failed" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	states := map[string]string{}
	for _, block := range result.Blocks {
		states[block.ID] = block.State
	}
	if states["alpha"] != "failed" || states["bravo"] != "failed" {
		t.Fatalf("states = %v", states)
	}
	if states["charlie"] != "done" {
		t.Fatalf("a block in flight beside a failure lost its outcome: %v", states)
	}
	if states["delta"] != "pending" {
		t.Fatalf("a dependent of a failed block started: %v", states)
	}
	reported := diagnostics.Of(err)
	var messages []string
	for _, entry := range reported {
		messages = append(messages, entry.Message)
	}
	joined := strings.Join(messages, "|")
	if !strings.Contains(joined, "alpha could not be served") || !strings.Contains(joined, "bravo could not be served") {
		t.Fatalf("diagnostics = %v", messages)
	}
	if slices.Index(messages, "alpha could not be served") > slices.Index(messages, "bravo could not be served") {
		t.Fatalf("failed blocks are not reported in plan order: %v", messages)
	}
}

// An interrupt admits nothing further and waits for what is in flight, so
// every block that started records the outcome it reached rather than being
// abandoned with no durable answer.
func TestAnInterruptAdmitsNothingAndWaitsForWhatIsRunning(t *testing.T) {
	h := scheduled(t, 2, definition("alpha"), definition("bravo"), definition("charlie"),
		definition("delta"))
	watch := newOverlap(4)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan *OperationResult, 1)
	go func() {
		result, _ := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- result
	}()
	watch.gather(t, 2)
	cancel()
	watch.open()
	result := <-finished
	if result == nil {
		t.Fatal("a cancelled operation reported no state")
	}
	// A cancellation is never a pause: the operation stays resumable rather
	// than reporting that it stopped somewhere it was asked to stop.
	if result.Receipt.State == "paused" {
		t.Fatalf("a cancelled operation paused: %+v", result.Receipt)
	}
	if result.Receipt.Next != "continue-apply" && result.Receipt.Next != "resolve" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if started := len(h.capability.applies); started != 2 {
		t.Fatalf("a cancelled operation started %d blocks, want the two already in flight", started)
	}
	pending := 0
	for _, block := range result.Blocks {
		if block.State == "pending" {
			pending++
		}
	}
	if pending != 2 {
		t.Fatalf("blocks left pending = %d, want 2: %+v", pending, result.Blocks)
	}
}

// An unproved effect is observed before anything else, and nothing starts,
// retries or is removed beside it but another observation.
func TestUnprovedEffectsAreObservedBeforeAnythingElseStarts(t *testing.T) {
	h := scheduled(t, 4, definition("alpha"), definition("bravo"), dependent("charlie", "alpha"))
	h.capability.outcomeFor = map[string]Result{
		"alpha": {Outcome: reconciliation.OutcomeUnknown},
		"bravo": {Outcome: reconciliation.OutcomeUnknown},
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unproved operation succeeded")
	}
	if len(h.capability.observes) != 0 {
		t.Fatalf("an invocation observed the effects it had just left unproved: %v", h.capability.observes)
	}
	// The continuation observes both, and starts nothing while it does.
	h.capability.outcomeFor = nil
	h.capability.observations = []Observation{
		{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)},
		{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)},
	}
	h.capability.calls = nil
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	observed := slices.Clone(h.capability.observes)
	slices.Sort(observed)
	if !slices.Equal(observed, []string{"alpha", "bravo"}) {
		t.Fatalf("observed %v", observed)
	}
	// Every observation precedes every attempt, so nothing started while an
	// effect's outcome was still unproved.
	for index, call := range h.capability.calls {
		if strings.HasPrefix(call, "apply:") && slices.ContainsFunc(h.capability.calls[index:], func(later string) bool {
			return strings.HasPrefix(later, "observe:")
		}) {
			t.Fatalf("a block started while an effect was still unproved: %v", h.capability.calls)
		}
	}
	if started := h.capability.applies[2:]; !slices.Equal(started, []string{"charlie"}) {
		t.Fatalf("the continuation started %v beside its observations", started)
	}
}

// A failed block's retry is what its dependents wait for, as they would for its
// first attempt. A block this invocation failed is not retried by the same
// invocation that failed it.
func TestAFailedBlockIsRetriedBeforeItsDependentsAndOnlyOncePerInvocation(t *testing.T) {
	h := scheduled(t, 4, definition("alpha"), dependent("bravo", "alpha"), dependent("charlie", "alpha"))
	h.capability.errorFor = map[string]error{"alpha": errors.New("alpha could not be served")}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed operation succeeded")
	}
	if !slices.Equal(h.capability.applies, []string{"alpha"}) {
		t.Fatalf("a failed block was retried by the invocation that failed it: %v", h.capability.applies)
	}
	h.capability.errorFor = nil
	watch := newOverlap(3)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	if retried := watch.gather(t, 1); !slices.Equal(retried, []string{"alpha"}) {
		t.Fatalf("the retry ran beside %v", retried)
	}
	watch.admit()
	if released := watch.gather(t, 2); !slices.Equal(released, []string{"bravo", "charlie"}) {
		t.Fatalf("a successful retry released %v", released)
	}
	watch.open()
	if err := returnedWithin(t, finished); err != nil {
		t.Fatal(err)
	}
	if !watch.together("bravo", "charlie") {
		t.Fatal("the blocks a successful retry released never ran together")
	}
	if !slices.Equal(h.capability.applies, []string{"alpha", "alpha", "bravo", "charlie"}) &&
		!slices.Equal(h.capability.applies, []string{"alpha", "alpha", "charlie", "bravo"}) {
		t.Fatalf("applied %v", h.capability.applies)
	}
}

// Failed blocks the graph does not order are retried together, as their first
// attempts ran, and a block still pending that waits on none of them starts
// beside them; a dependent of a failed block waits for that block's retry
// alone (D144).
func TestIndependentFailedBlocksAreRetriedTogether(t *testing.T) {
	h := scheduled(t, 3, definition("alpha"), definition("bravo"), definition("charlie"),
		definition("delta"), dependent("echo", "alpha"))
	h.capability.errorFor = map[string]error{
		"alpha":   errors.New("alpha could not be served"),
		"bravo":   errors.New("bravo could not be served"),
		"charlie": errors.New("charlie could not be served"),
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed operation succeeded")
	}
	if first := slices.Sorted(slices.Values(h.capability.applies)); !slices.Equal(first, []string{"alpha", "bravo", "charlie"}) {
		t.Fatalf("the first apply worked %v", first)
	}
	h.capability.errorFor = nil
	h.service.options.Concurrency = 8
	watch := newOverlap(5)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	if wave := watch.gather(t, 4); !slices.Equal(wave, []string{"alpha", "bravo", "charlie", "delta"}) {
		t.Fatalf("the continuation started %v together", wave)
	}
	watch.admit()
	if released := watch.gather(t, 1); !slices.Equal(released, []string{"echo"}) {
		t.Fatalf("a successful retry released %v", released)
	}
	watch.open()
	if err := returnedWithin(t, finished); err != nil {
		t.Fatal(err)
	}
	if watch.highest() != 4 {
		t.Fatalf("%d blocks were in flight at once, want the three retries beside delta", watch.highest())
	}
}

// A failed block outside the stage selection is never retried, even by an
// invocation that retries a failed block the selection admits. A selection
// that admits no failed block refuses before any effect and names the apply
// whose selection adds the stage of the first one.
func TestAFailedBlockOutsideTheSelectionIsNeverRetried(t *testing.T) {
	first := definition("alpha")
	first.Stage = reconciliation.StageInfraComponents
	second := definition("bravo")
	second.Stage = reconciliation.StageMachines
	h := scheduled(t, 4, first, second)
	h.capability.errorFor = map[string]error{
		"alpha": errors.New("alpha could not be served"),
		"bravo": errors.New("bravo could not be served"),
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed operation succeeded")
	}
	h.capability.errorFor = nil
	result, _ := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", Stages: []string{"machines"}, SkipConfirmation: true,
	})
	if retried := h.capability.applies[2:]; !slices.Equal(retried, []string{"bravo"}) {
		t.Fatalf("a selection of machines retried %v", retried)
	}
	if result == nil {
		t.Fatal("the retry reported no state")
	}
	states := map[string]string{}
	for _, block := range result.Blocks {
		states[block.ID] = block.State
	}
	if states["alpha"] != "failed" || states["bravo"] != "done" {
		t.Fatalf("states = %v", states)
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", Stages: []string{"substrates"}, SkipConfirmation: true,
	})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.stage" || reported[0].Remediation != "repeat bootwright apply --context lab --stage infra-components,substrates" {
		t.Fatalf("retry refusal = %+v", reported)
	}
	if len(h.capability.applies) != 3 {
		t.Fatalf("a refused selection performed an effect: %v", h.capability.applies)
	}
}

// Two failed blocks that will not share a resource are never retried together
// either, while a third that needs none of it is retried beside the first. A
// retry that fails again still lets the other waits for its resource be
// retried, yet admits no further first attempt. No run of the engine leaves two
// failed blocks that share a resource, since they never run at the same time
// and a failure admits no further first attempt, so the states are given to
// admission directly.
func TestFailedBlocksNamingOneResourceAreRetriedOneAtATime(t *testing.T) {
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{
		exclusive("alpha", "path:/srv/tree"), exclusive("bravo", "path:/srv/tree"), definition("charlie"),
		dependent("delta", "charlie"),
	})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]reconciliation.BlockState{
		"alpha": reconciliation.BlockFailed, "bravo": reconciliation.BlockFailed, "charlie": reconciliation.BlockFailed,
	}
	run := Service{options: Options{Concurrency: 4}}.newScheduler(nil, nil, approved{}, nil, operationstore.Operation{}, plan, states, nil, nil)
	retried, _, ok := run.next()
	if !ok || retried.ID != "alpha" {
		t.Fatalf("the first retry = %q (%t)", retried.ID, ok)
	}
	// In flight exactly as start leaves it, without an attempt behind it.
	run.running[retried.ID], run.worked[retried.ID], run.held["path:/srv/tree"] = true, true, retried.ID
	beside, _, ok := run.next()
	if !ok || beside.ID != "charlie" {
		t.Fatalf("the retry beside %s = %q (%t), want charlie", retried.ID, beside.ID, ok)
	}
	run.running[beside.ID], run.worked[beside.ID] = true, true
	if blocked, _, ok := run.next(); ok {
		t.Fatalf("%s was retried beside %s, which holds the resource it needs", blocked.ID, retried.ID)
	}
	run.settle(step{block: retried.ID, state: reconciliation.BlockFailed})
	following, _, ok := run.next()
	if !ok || following.ID != "bravo" {
		t.Fatalf("the second retry = %q (%t)", following.ID, ok)
	}
	run.running[following.ID], run.worked[following.ID] = true, true
	run.settle(step{block: beside.ID, state: reconciliation.BlockDone})
	if started, _, ok := run.next(); ok {
		t.Fatalf("%s started after a retry failed again", started.ID)
	}
}

// An unproved effect is resolved before any failed block is retried, and no
// retry or first attempt starts beside its observation; once it is proved, the
// failed blocks are retried together with the pending block beside them.
func TestAnUnprovedEffectIsResolvedBeforeAnyRetry(t *testing.T) {
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{
		definition("alpha"), definition("bravo"), definition("charlie"), definition("delta"),
	})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]reconciliation.BlockState{
		"alpha": reconciliation.BlockUnknown, "bravo": reconciliation.BlockFailed, "charlie": reconciliation.BlockFailed,
	}
	run := Service{options: Options{Concurrency: 4}}.newScheduler(nil, nil, approved{}, nil, operationstore.Operation{}, plan, states, nil, nil)
	observed, _, ok := run.next()
	if !ok || observed.ID != "alpha" {
		t.Fatalf("the first admission = %q (%t), want the unknown block", observed.ID, ok)
	}
	run.running[observed.ID], run.observed[observed.ID] = true, true
	if beside, _, ok := run.next(); ok {
		t.Fatalf("%s started beside the observation of %s", beside.ID, observed.ID)
	}
	run.settle(step{block: observed.ID, state: reconciliation.BlockDone})
	var wave []string
	for {
		block, _, ok := run.next()
		if !ok {
			break
		}
		run.running[block.ID], run.worked[block.ID] = true, true
		wave = append(wave, block.ID)
	}
	if !slices.Equal(wave, []string{"bravo", "charlie", "delta"}) {
		t.Fatalf("once the effect was proved, admission started %v", wave)
	}
}

// Once a selected failed block is retried, admission follows the ordinary rules
// whatever a failed block outside the selection holds back, so what it starts
// does not depend on which block completes first: the retry's dependent and
// the dependent of a pending block both start in either order.
func TestAdmissionAfterARetryDoesNotDependOnCompletionOrder(t *testing.T) {
	staged := func(definition reconciliation.BlockDefinition, stage reconciliation.Stage) reconciliation.BlockDefinition {
		definition.Stage = stage
		return definition
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{
		staged(definition("first-failed"), reconciliation.StageMachines),
		staged(definition("second-failed"), reconciliation.StageInfraComponents),
		staged(definition("first-pending"), reconciliation.StageMachines),
		staged(dependent("second-pending", "first-pending"), reconciliation.StageMachines),
		staged(dependent("after-retry", "first-failed"), reconciliation.StageMachines),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range [][]string{{"first-failed", "first-pending"}, {"first-pending", "first-failed"}} {
		t.Run(order[0]+" first", func(t *testing.T) {
			states := map[string]reconciliation.BlockState{
				"first-failed": reconciliation.BlockFailed, "second-failed": reconciliation.BlockFailed,
			}
			selection := reconciliation.StageSelection{reconciliation.StageMachines}
			run := Service{options: Options{Concurrency: 4}}.newScheduler(nil, nil, approved{}, nil, operationstore.Operation{}, plan, states, nil, selection)
			started := []string{}
			drain := func() {
				for {
					block, _, ok := run.next()
					if !ok {
						return
					}
					run.running[block.ID], run.worked[block.ID] = true, true
					started = append(started, block.ID)
				}
			}
			drain()
			if !slices.Equal(started, []string{"first-failed", "first-pending"}) {
				t.Fatalf("the first wave was %v", started)
			}
			for _, finished := range order {
				run.settle(step{block: finished, state: reconciliation.BlockDone})
				drain()
			}
			slices.Sort(started)
			if !slices.Equal(started, []string{"after-retry", "first-failed", "first-pending", "second-pending"}) {
				t.Fatalf("completing %v started %v", order, started)
			}
		})
	}
}

// A stage selection still pauses at its boundary when blocks run together: the
// operation stops because nothing more may start, which is resumable.
func TestAStageBoundaryPausesWhileBlocksRunTogether(t *testing.T) {
	first := definition("alpha")
	first.Stage = reconciliation.StageInfraComponents
	second := definition("bravo")
	second.Stage = reconciliation.StageInfraComponents
	later := dependent("charlie", "alpha")
	later.Stage = reconciliation.StageMachines
	h := scheduled(t, 4, first, second, later)
	result, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", Stages: []string{"infra-components"}, SkipConfirmation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.State != "paused" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	applied := slices.Clone(h.capability.applies)
	slices.Sort(applied)
	if !slices.Equal(applied, []string{"alpha", "bravo"}) {
		t.Fatalf("applied %v", applied)
	}
}

// A preview derives what orders the work from the frozen plan itself: each
// step's predecessors by their place in the list, and how many rounds the
// plan's own shape needs, beside how many blocks this build starts at once.
func TestPlanPreviewNamesPredecessorsAndWaves(t *testing.T) {
	h := scheduled(t, 4, definition("alpha"), definition("zulu"),
		dependent("bravo", "alpha", "zulu"), dependent("charlie", "bravo"))
	result, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Waves != 3 || result.Widest != 2 || result.Bound != 4 {
		t.Fatalf("schedule = %d waves, widest %d, bound %d", result.Waves, result.Widest, result.Bound)
	}
	places := map[string]plannedPlace{}
	for index, planned := range result.Steps {
		places[planned.ID] = plannedPlace{position: index + 1, after: planned.After, wave: planned.Wave}
	}
	// The two blocks nothing waits for open the plan and name no predecessor.
	for _, root := range []string{"alpha", "zulu"} {
		if len(places[root].after) != 0 || places[root].wave != 1 {
			t.Fatalf("%s = %+v", root, places[root])
		}
	}
	want := []int{places["alpha"].position, places["zulu"].position}
	slices.Sort(want)
	if !slices.Equal(places["bravo"].after, want) || places["bravo"].wave != 2 {
		t.Fatalf("bravo = %+v, want after %v", places["bravo"], want)
	}
	if !slices.Equal(places["charlie"].after, []int{places["bravo"].position}) || places["charlie"].wave != 3 {
		t.Fatalf("charlie = %+v", places["charlie"])
	}
}

type plannedPlace struct {
	position int
	after    []int
	wave     int
}

// A resolution is not an attempt: a block it proves unfinished is retried by
// the same invocation, exactly as the next one would retry it, and its
// dependents start in that invocation. The resolution's row stays open for the
// retry instead of reporting a failure the invocation is about to repair, and
// the retry's success leaves no diagnostic behind.
func TestABlockItsResolutionLeavesFailedIsRetriedByTheSameApply(t *testing.T) {
	for name, effect := range map[string]reconciliation.EffectState{
		"no effect": reconciliation.EffectNoEffect,
		"partial":   reconciliation.EffectPartial,
	} {
		t.Run(name, func(t *testing.T) {
			h := scheduled(t, 4, definition("alpha"), dependent("bravo", "alpha"))
			progress := &testProgress{}
			h.service.options.Progress = progress
			h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}}
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
				t.Fatal("an unproved operation succeeded")
			}
			h.capability.outcomeFor = nil
			h.capability.observations = []Observation{{Effect: effect}}
			before := len(progress.reported())
			result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			if err != nil || result == nil || result.Receipt.State != string(reconciliation.OperationDone) {
				t.Fatalf("the resolving apply = %+v (%v), want it done and clean", result, err)
			}
			if !slices.Equal(h.capability.observes, []string{"alpha"}) {
				t.Fatalf("observed %v, want alpha once", h.capability.observes)
			}
			if !slices.Equal(h.capability.applies, []string{"alpha", "alpha", "bravo"}) {
				t.Fatalf("attempted %v, want alpha retried before bravo started", h.capability.applies)
			}
			rows := progress.reported()[before:]
			var alpha []string
			for _, row := range rows {
				if strings.HasPrefix(row, "serve alpha:") {
					alpha = append(alpha, row)
				}
			}
			if !slices.Contains(alpha, "serve alpha:the effect is unfinished; retrying it:running:1/2") {
				t.Fatalf("alpha reported %q, want its row kept open for the retry", alpha)
			}
			if slices.ContainsFunc(alpha, func(row string) bool { return strings.HasPrefix(row, "serve alpha::failed:") }) {
				t.Fatalf("alpha reported %q, want no failure the same invocation repairs", alpha)
			}
			if last := alpha[len(alpha)-1]; last != "serve alpha::done:1/2" {
				t.Fatalf("alpha ends on %q, want the retry's terminal state", last)
			}
		})
	}
}

// A retry that fails again is a failed attempt like any other: it admits no
// first attempt, its own error is the diagnostic in place of the resolution's,
// and the next invocation retries the block again.
func TestARetryAfterAResolutionThatFailsAgainStopsFirstAttempts(t *testing.T) {
	h := scheduled(t, 4, definition("alpha"), dependent("bravo", "alpha"))
	h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unproved operation succeeded")
	}
	h.capability.outcomeFor = nil
	h.capability.errorFor = map[string]error{"alpha": failure("lifecycle.state", "alpha could not be served", "")}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectPartial}}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil || result == nil || result.Receipt.State != string(reconciliation.OperationFailed) {
		t.Fatalf("the failed retry = %+v (%v), want the apply failed", result, err)
	}
	if !slices.Equal(h.capability.applies, []string{"alpha", "alpha"}) {
		t.Fatalf("attempted %v, want alpha retried and bravo never started", h.capability.applies)
	}
	reported := diagnostics.Of(err)
	if !slices.ContainsFunc(reported, func(entry diagnostics.Diagnostic) bool { return entry.Message == "alpha could not be served" }) {
		t.Fatalf("the failed retry reported %+v, want its own error", reported)
	}
	if slices.ContainsFunc(reported, func(entry diagnostics.Diagnostic) bool { return strings.Contains(entry.Message, "partly realized") }) {
		t.Fatalf("the failed retry still reported the resolution's diagnostic: %+v", reported)
	}
	h.capability.errorFor = nil
	repeated, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || repeated.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the repeated apply = %+v (%v)", repeated, err)
	}
	if !slices.Equal(h.capability.applies, []string{"alpha", "alpha", "alpha", "bravo"}) || len(h.capability.observes) != 1 {
		t.Fatalf("attempted %v and observed %v, want alpha retried again with no second observation", h.capability.applies, h.capability.observes)
	}
}

// A resolution only retries a block the invocation may attempt: one whose
// stage the selection leaves out stays failed with the remedy that names the
// command taking it on, and its row reports the failure.
func TestABlockAResolutionLeavesFailedOutsideTheSelectionIsNotRetried(t *testing.T) {
	first := definition("alpha")
	first.Stage = reconciliation.StageInfraComponents
	second := definition("bravo")
	second.Stage = reconciliation.StageMachines
	h := scheduled(t, 4, first, second)
	progress := &testProgress{}
	h.service.options.Progress = progress
	h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unproved operation succeeded")
	}
	h.capability.outcomeFor = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectPartial}}
	attempts, before := len(h.capability.applies), len(progress.reported())
	result, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", Stages: []string{"machines"}, SkipConfirmation: true,
	})
	if err == nil || result == nil || result.Receipt.State != string(reconciliation.OperationFailed) {
		t.Fatalf("the resolving apply = %+v (%v), want alpha left failed", result, err)
	}
	if !slices.Equal(h.capability.observes, []string{"alpha"}) || slices.Contains(h.capability.applies[attempts:], "alpha") {
		t.Fatalf("observed %v and then attempted %v, want alpha resolved and not retried", h.capability.observes, h.capability.applies[attempts:])
	}
	rows := progress.reported()[before:]
	if !slices.Contains(rows, "serve alpha::failed:1/2") || slices.Contains(rows, "serve alpha:the effect is unfinished; retrying it:running:1/2") {
		t.Fatalf("progress = %q, want alpha reported failed and no retry announced", rows)
	}
	if !slices.ContainsFunc(diagnostics.Of(err), func(entry diagnostics.Diagnostic) bool {
		return entry.Message == "the frozen effect is partly realized and owned by this context"
	}) {
		t.Fatalf("the resolving apply reported %+v, want the resolution's remedy", diagnostics.Of(err))
	}
}

// A retry the invocation announced and never starts settles the block's row
// with the state it keeps, so no row stays running. One that started has its
// own terminal row, and one whose attempt could not start has none.
func TestAnAnnouncedRetryThatNeverStartsIsSettledFailed(t *testing.T) {
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{definition("alpha")})
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		worked, unstarted bool
		rows              int
	}{
		"the invocation stopped first": {false, false, 1},
		"the retry ran":                {true, false, 0},
		"the retry could not start":    {true, true, 1},
	} {
		t.Run(name, func(t *testing.T) {
			progress := &testProgress{}
			states := map[string]reconciliation.BlockState{"alpha": reconciliation.BlockUnknown}
			run := Service{options: Options{Concurrency: 4, Progress: progress}}.newScheduler(nil, nil, approved{}, nil, operationstore.Operation{}, plan, states, nil, nil)
			run.settle(step{block: "alpha", state: reconciliation.BlockFailed, announced: true, position: 1})
			if tc.worked {
				run.worked["alpha"] = true
			}
			if tc.unstarted {
				run.unstarted["alpha"] = true
			}
			run.closeAnnounced(context.Background())
			rows := progress.reported()
			if len(rows) != tc.rows || tc.rows == 1 && rows[0] != "serve alpha::failed:1/1" {
				t.Fatalf("the settled rows = %q, want %d failed row of alpha", rows, tc.rows)
			}
		})
	}
}

// A failed resolution is not an attempt, so the block it left failed is
// admitted for its retry, beside whatever else may start; a failed attempt is
// one, so that block is not admitted again and nothing else starts. An effect
// the invocation has observed, or whose own attempt left it unproved, is not
// admitted for resolution again.
func TestAFailedResolutionIsRetriedButAFailedAttemptIsNot(t *testing.T) {
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{
		definition("alpha"), definition("bravo"),
	})
	if err != nil {
		t.Fatal(err)
	}
	failed := map[string]reconciliation.BlockState{"alpha": reconciliation.BlockFailed, "bravo": reconciliation.BlockPending}
	if kind, admitted := firstAdmission(plan, failed, nil, map[string]bool{"alpha": true}, nil, nil); kind != StepRetry || !slices.Equal(admitted, []int{0, 1}) {
		t.Fatalf("after a resolution admission = %s %v, want the retry of alpha beside the start of bravo", kind, admitted)
	}
	if kind, admitted := firstAdmission(plan, failed, nil, nil, map[string]bool{"alpha": true}, nil); kind != StepRetry || len(admitted) != 0 {
		t.Fatalf("after an attempt admission = %s %v, want nothing", kind, admitted)
	}
	alone, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{definition("alpha")})
	if err != nil {
		t.Fatal(err)
	}
	if kind, admitted := firstAdmission(alone, map[string]reconciliation.BlockState{"alpha": reconciliation.BlockFailed}, nil, map[string]bool{"alpha": true}, nil, nil); kind != StepRetry || !slices.Equal(admitted, []int{0}) {
		t.Fatalf("after a resolution of a lone block admission = %s %v, want its retry", kind, admitted)
	}
	unknown := map[string]reconciliation.BlockState{"alpha": reconciliation.BlockUnknown, "bravo": reconciliation.BlockPending}
	for name, tc := range map[string]struct{ observed, worked map[string]bool }{
		"observed": {observed: map[string]bool{"alpha": true}},
		"worked":   {worked: map[string]bool{"alpha": true}},
	} {
		if kind, admitted := firstAdmission(plan, unknown, nil, tc.observed, tc.worked, nil); kind != StepResolve || len(admitted) != 0 {
			t.Fatalf("an unproved effect already %s admission = %s %v, want nothing to resolve", name, kind, admitted)
		}
	}
}

// heldInstallationWindow is how long a held installation must stand alone, far
// beyond the moment an unserialized scheduler admits its peer.
const heldInstallationWindow = time.Second

func TestClusterInstallationsSerializeWhileMediaBuildsOverlap(t *testing.T) {
	first := dependent("cluster-install-alpha", "cluster-media-alpha")
	second := dependent("cluster-install-bravo", "cluster-media-bravo")
	first.Exclusive, second.Exclusive = []string{"container-cluster-install"}, []string{"container-cluster-install"}
	h := scheduled(t, 4, definition("cluster-media-alpha"), definition("cluster-media-bravo"), first, second)
	watch := newOverlap(4)
	t.Cleanup(watch.open)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	if first := watch.gather(t, 2); !slices.Equal(first, []string{"cluster-media-alpha", "cluster-media-bravo"}) {
		t.Fatal("media builds did not start together", first)
	}
	watch.admit()
	installing := watch.gather(t, 1)
	select {
	case early := <-watch.arrived:
		t.Fatalf("%s started while %v was still held installing", early, installing)
	case <-time.After(heldInstallationWindow):
	}
	watch.admit()
	following := watch.gather(t, 1)
	if want := []string{"cluster-install-alpha", "cluster-install-bravo"}; !slices.Equal(slices.Sorted(slices.Values(append(installing, following...))), want) {
		t.Fatalf("installations started as %v then %v, want each of %v once", installing, following, want)
	}
	watch.open()
	if err := returnedWithin(t, finished); err != nil {
		t.Fatal(err)
	}
	if !watch.together("cluster-media-alpha", "cluster-media-bravo") || watch.together("cluster-install-alpha", "cluster-install-bravo") {
		t.Fatal("cluster schedule did not preserve independent media builds and exclusive installations")
	}
	if !slices.Contains(h.capability.applies, "cluster-install-alpha") || !slices.Contains(h.capability.applies, "cluster-install-bravo") {
		t.Fatal("an installation never started", h.capability.applies)
	}
}
