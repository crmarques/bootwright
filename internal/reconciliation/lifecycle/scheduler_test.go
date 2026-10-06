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

// gather waits for exactly this many blocks to be in flight together.
func (o *overlap) gather(t *testing.T, count int) []string {
	t.Helper()
	var blocks []string
	for range count {
		blocks = append(blocks, <-o.arrived)
	}
	slices.Sort(blocks)
	return blocks
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

// A failed block is the only retry candidate and runs alone, because what
// follows it depends on it succeeding. A block this invocation failed is not
// retried by the same invocation that failed it.
func TestAFailedBlockIsRetriedAloneAndOnlyOncePerInvocation(t *testing.T) {
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
	if err := <-finished; err != nil {
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

// Two failed blocks are retried one at a time in frozen order, however many
// blocks the bound would admit: a retry waits for everything in flight.
func TestFailedBlocksAreRetriedOneAtATime(t *testing.T) {
	h := scheduled(t, 4, definition("alpha"), definition("bravo"))
	h.capability.errorFor = map[string]error{
		"alpha": errors.New("alpha could not be served"),
		"bravo": errors.New("bravo could not be served"),
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed operation succeeded")
	}
	h.capability.errorFor = nil
	watch := newOverlap(2)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	if first := watch.gather(t, 1); !slices.Equal(first, []string{"alpha"}) {
		t.Fatalf("the first retry was %v, want the first failed block in frozen order", first)
	}
	watch.admit()
	if second := watch.gather(t, 1); !slices.Equal(second, []string{"bravo"}) {
		t.Fatalf("the second retry was %v", second)
	}
	watch.open()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if watch.together("alpha", "bravo") || watch.highest() != 1 {
		t.Fatalf("two failed blocks were retried together: %d in flight at once", watch.highest())
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
// either. No run of the engine leaves both failed, since they never run at the
// same time and a failure admits nothing further, so the states are given to
// admission directly.
func TestFailedBlocksNamingOneResourceAreRetriedOneAtATime(t *testing.T) {
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{
		exclusive("alpha", "path:/srv/tree"), exclusive("bravo", "path:/srv/tree"),
	})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]reconciliation.BlockState{"alpha": reconciliation.BlockFailed, "bravo": reconciliation.BlockFailed}
	run := Service{options: Options{Concurrency: 4}}.newScheduler(nil, nil, bundle{}, nil, operationstore.Operation{}, plan, states, nil, nil)
	retried, _, ok := run.next()
	if !ok || retried.ID != "alpha" {
		t.Fatalf("the first retry = %q (%t)", retried.ID, ok)
	}
	// In flight exactly as start leaves it, without an attempt behind it.
	run.running[retried.ID], run.worked[retried.ID], run.held["path:/srv/tree"] = true, true, retried.ID
	if beside, _, ok := run.next(); ok {
		t.Fatalf("%s was retried beside %s, which holds the resource it needs", beside.ID, retried.ID)
	}
	run.settle(step{block: retried.ID, state: reconciliation.BlockDone})
	if following, _, ok := run.next(); !ok || following.ID != "bravo" {
		t.Fatalf("the second retry = %q (%t)", following.ID, ok)
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
