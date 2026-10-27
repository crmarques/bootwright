package lifecycle

import (
	"context"
	"errors"
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// MaxRunningBlocks bounds how many of an operation's blocks run at once. The
// graph decides what may run together; this decides how much of that one host
// is asked to do at the same time, so it is a property of the executable
// rather than of the plan and is never frozen with it.
const MaxRunningBlocks = 8

// step is one block this invocation put in flight, and what it proved.
type step struct {
	block string
	state reconciliation.BlockState
	err   error
	// unstarted is an attempt that could not start. It performed no effect
	// and recorded nothing, so state is the one the block already had.
	unstarted bool
	announced bool
	position  int
}

// scheduler starts every block the frozen plan admits, up to the bound, and
// re-evaluates as each one settles. It owns no durable state: the block
// records remain the truth, and this decides only what runs next.
type scheduler struct {
	service   Service
	tx        Transaction
	store     OperationStore
	approved  approved
	operation operationstore.Operation
	plan      reconciliation.Plan
	material  map[string]secrets.Material
	selection reconciliation.Selection
	bound     int
	// logging is the invocation's required logging boundary. Once it latches
	// a fault, nothing further is admitted.
	logging *logBoundary

	states map[string]reconciliation.BlockState
	// running are the blocks this invocation has in flight, unlike the durable running
	// state: an executor that died mid-attempt, an unproved effect rather than work in progress.
	running map[string]bool
	// observed and worked keep this invocation from resolving, or attempting, any block twice (D146).
	observed  map[string]bool
	worked    map[string]bool
	announced map[string]int
	// An unstarted attempt blocks further admission; its refusal may affect others.
	unstarted map[string]bool
	// held maps each exclusive resource in use to the block using it.
	held    map[string]string
	causes  map[string]error
	results chan step
	// observeOnly admits resolutions and nothing else. A removal uses it to
	// prove the outcome of the effects it is about to take back, which is
	// read-only and may start no attempt of the operation it observes.
	observeOnly bool
	// removal marks the observation a fresh removal makes of the apply it
	// supersedes: each unproved block is read by the check of the block of
	// removalPlan that takes it back (D119), unless the apply's own observation
	// proves it completed (D123).
	removal       bool
	removalPlan   reconciliation.Plan
	online        onlineRemoval
	onlineProved  bool
	onlineBlocked bool
	preStopProved bool
}

// converge runs the operation's blocks until nothing more may start, and
// reports whether it stopped at a stage or operator-stop boundary and what any
// block that did not complete reported.
func (s Service) converge(ctx context.Context, tx Transaction, store OperationStore, approved approved, logging *logBoundary, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, material map[string]secrets.Material, selection reconciliation.Selection) (bool, error) {
	return s.newScheduler(tx, store, approved, logging, operation, plan, states, material, selection).converge(ctx)
}

// observe resolves every unproved effect of an operation and starts nothing
// else, leaving the block states it was given holding what each one proved.
// removal says a fresh removal resolves an apply's blocks, by the check of the
// block of removalPlan that takes each one back.
func (s Service) observe(ctx context.Context, tx Transaction, store OperationStore, approved approved, logging *logBoundary, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, material map[string]secrets.Material, removal bool, removalPlan reconciliation.Plan) error {
	run := s.newScheduler(tx, store, approved, logging, operation, plan, states, material, reconciliation.Selection{})
	run.observeOnly = true
	run.removal, run.removalPlan = removal, removalPlan
	_, err := run.converge(ctx)
	return err
}

// Concurrency reports how many of an operation's blocks this service runs at
// once: the bound composition gave it, or MaxRunningBlocks when it gave none.
// It is what every scheduler the service creates is bounded by.
func (s Service) Concurrency() int {
	if s.options.Concurrency <= 0 {
		return MaxRunningBlocks
	}
	return s.options.Concurrency
}

func (s Service) newScheduler(tx Transaction, store OperationStore, approved approved, logging *logBoundary, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, material map[string]secrets.Material, selection reconciliation.Selection) *scheduler {
	return &scheduler{
		service: s, tx: tx, store: store, approved: approved, operation: operation,
		plan: plan, material: material, selection: selection, bound: s.Concurrency(), logging: logging,
		states: states, running: map[string]bool{}, observed: map[string]bool{}, worked: map[string]bool{},
		unstarted: map[string]bool{}, announced: map[string]int{},
		held: map[string]string{}, causes: map[string]error{},
		results: make(chan step, len(plan.Blocks)),
		online:  s.onlineRemoval(plan),
	}
}

func (c *scheduler) converge(ctx context.Context) (bool, error) {
	for {
		// Cancellation admits nothing further, and the blocks already in
		// flight are waited for rather than abandoned: each one records its
		// own outcome, and an abandoned attempt is an unproved effect.
		if ctx.Err() == nil {
			c.admit(ctx)
		}
		if len(c.running) == 0 {
			break
		}
		c.settle(<-c.results)
	}
	c.closeAnnounced(ctx)
	// A log fault requests cancellation, so a stop it caused is never a pause
	// whether or not that request has reached this context yet. Nor is one an
	// attempt that could not start caused: its block was startable, and only
	// the failure stopped it.
	if ctx.Err() != nil || c.logging.faulted() || len(c.unstarted) != 0 {
		return false, c.cause()
	}
	// A stop that leaves an effect unproved is not a pause: a block still
	// durably running is one whose resolution could not start.
	return pendingRemains(c.plan, c.states) && !c.anyState(unproved), c.cause()
}

// admit starts every block the durable state and the bound allow, so a wave of
// independent work goes out together rather than one block at a time. A
// latched log fault admits nothing, whatever the cancellation it requested has
// reached so far, and neither does an attempt that could not start; what is
// already in flight is waited for either way.
func (c *scheduler) admit(ctx context.Context) {
	for len(c.running) < c.bound && !c.logging.faulted() && len(c.unstarted) == 0 && !c.onlineBlocked {
		block, position, ok := c.next()
		if !ok {
			return
		}
		if !c.provePreStopBoundary(ctx, block) || !c.proveOnlineBoundary(ctx, block) {
			return
		}
		c.start(ctx, block, position)
	}
}

// next chooses the one block admission may start now. An unproved effect is observed before anything
// else and admits nothing beside another observation, because no retry, dependent block or removal may
// start while an effect's outcome is unproved. Then pre-stop inverses alone run until each is done.
// Otherwise the first block admission takes in frozen order runs, a retry exactly as a first attempt,
// provided nothing running holds a resource it needs to itself and this invocation has not worked it:
// a worked block still pending is one whose attempt never ran, and admitting it again meets the same refusal.
func (c *scheduler) next() (reconciliation.Block, int, bool) {
	kind, admitted := firstAdmission(c.plan, c.states, c.selection, c.observed, c.worked, c.running)
	switch {
	case kind == StepResolve:
		for _, index := range admitted {
			if !c.running[c.plan.Blocks[index].ID] {
				return c.plan.Blocks[index], index, true
			}
		}
		return reconciliation.Block{}, 0, false
	case c.observeOnly:
		return reconciliation.Block{}, 0, false
	case !c.online.preStopDone(c.states):
		return c.nextPreStop()
	}
	for _, index := range admitted {
		block := c.plan.Blocks[index]
		if c.online.machines[block.ID] && !c.online.complete(c.states) {
			continue
		}
		if !c.running[block.ID] && !c.worked[block.ID] && c.free(block) {
			return block, index, true
		}
	}
	return reconciliation.Block{}, 0, false
}

func (c *scheduler) nextPreStop() (reconciliation.Block, int, bool) {
	for index, block := range c.plan.Blocks {
		state := c.states[block.ID]
		if c.online.preStop[block.ID] && (state == reconciliation.BlockPending || state == reconciliation.BlockFailed) && !c.running[block.ID] && !c.worked[block.ID] && c.free(block) {
			return block, index, true
		}
	}
	return reconciliation.Block{}, 0, false
}

// firstAdmission is what admission takes first, as plan positions in frozen
// order: while any effect is unproved, StepResolve and every unproved block
// neither observed nor worked; otherwise StepRetry while any block is failed,
// else StepStart, with every selected failed block not yet worked and every
// startable block, but no startable block once a worked block has failed, and
// nothing before this invocation observes or works a block when no failed block
// is selected. The scheduler, the refusal of a selection that admits nothing and
// the stage preview all derive from it, so the preview marks exactly what the
// next apply admits.
func firstAdmission(plan reconciliation.Plan, states map[string]reconciliation.BlockState, selection reconciliation.Selection, observed, worked, running map[string]bool) (string, []int) {
	admitted := []int{}
	if slices.ContainsFunc(plan.Blocks, func(block reconciliation.Block) bool { return unproved(states[block.ID]) }) {
		for index, block := range plan.Blocks {
			if unproved(states[block.ID]) && !observed[block.ID] && !worked[block.ID] {
				admitted = append(admitted, index)
			}
		}
		return StepResolve, admitted
	}
	kind := StepStart
	if slices.ContainsFunc(plan.Blocks, func(block reconciliation.Block) bool { return states[block.ID] == reconciliation.BlockFailed }) {
		kind = StepRetry
		failedAgain := false
		for index, block := range plan.Blocks {
			switch {
			case states[block.ID] != reconciliation.BlockFailed, running[block.ID]:
			case worked[block.ID]:
				failedAgain = true
			case selection.Admits(block):
				admitted = append(admitted, index)
			}
		}
		if failedAgain || len(admitted) == 0 && len(observed) == 0 && len(worked) == 0 {
			return kind, admitted
		}
	}
	startable := reconciliation.Startable(plan, states, selection)
	for index, block := range plan.Blocks {
		if slices.ContainsFunc(startable, func(ready reconciliation.Block) bool { return ready.ID == block.ID }) {
			admitted = append(admitted, index)
		}
	}
	slices.Sort(admitted)
	return kind, admitted
}

// anyState reports whether any block of the plan is in a state, including one
// this invocation is working on right now.
func (c *scheduler) anyState(is func(reconciliation.BlockState) bool) bool {
	return slices.ContainsFunc(c.plan.Blocks, func(block reconciliation.Block) bool {
		return is(c.states[block.ID])
	})
}

// free reports whether every exclusive resource this block claims is idle.
func (c *scheduler) free(block reconciliation.Block) bool {
	for _, key := range block.Exclusive {
		if _, busy := c.held[key]; busy {
			return false
		}
	}
	return true
}

// start puts one block in flight under its own goroutine. Its position is the
// place it holds in the frozen plan, so a row an operator reads names the
// same step the plan they confirmed listed.
func (c *scheduler) start(ctx context.Context, block reconciliation.Block, position int) {
	c.running[block.ID] = true
	prior := c.states[block.ID]
	observing := unproved(prior)
	if observing {
		c.observed[block.ID] = true
	} else {
		c.worked[block.ID] = true
	}
	for _, key := range block.Exclusive {
		c.held[key] = block.ID
	}
	command := c.observingCommand()
	retry := observing && !c.observeOnly && c.selection.Admits(block)
	go func() {
		var state reconciliation.BlockState
		var err error
		var unstarted, announced bool
		if observing {
			// The observation's durable transition is authoritative even when it
			// reports a refusal, so the operation state matches the record; one
			// that never started recorded nothing, and the block keeps its state.
			state, err = c.service.resolveUnknown(ctx, c.tx, c.store, c.approved, c.logging, c.operation, c.plan, block, c.material, position+1, len(c.plan.Blocks), c.removal, c.removalPlan, command, retry)
			if state == "" {
				state = c.durable(ctx, block.ID, prior)
			}
			announced = retry && state == reconciliation.BlockFailed
		} else {
			// An attempt that never started performed no effect; its block
			// holds what its record reads, running when the start landed.
			state, err = c.service.attempt(ctx, c.tx, c.store, c.approved, c.logging, c.operation, c.plan, block, c.material, position+1, len(c.plan.Blocks))
			if state == "" {
				state, unstarted = c.durable(ctx, block.ID, prior), true
			}
		}
		c.results <- step{block: block.ID, state: state, err: err, unstarted: unstarted, announced: announced, position: position + 1}
	}()
}

// durable re-reads a block whose start or resolution reported a failure, so
// the operation is recorded from its block records alone (F09-06).
func (c *scheduler) durable(ctx context.Context, block string, prior reconciliation.BlockState) reconciliation.BlockState {
	record, err := c.store.Block(recordingContext(ctx), c.operation.ID, block)
	if err != nil {
		return reconciliation.BlockUnknown
	}
	if record.State == "" {
		return prior
	}
	return record.State
}

// observingCommand is the exact command that observes again a block a
// resolution leaves unknown, read from the states the scheduler holds when the
// block starts: the removal that resolves an apply's blocks, with its own
// plan's tokens, or the continuation of this operation.
func (c *scheduler) observingCommand() string {
	name := c.tx.Identity().Name
	if c.removal {
		return contextCommand(name, string(reconciliation.Destroy), authorizingUnfixed(c.removalPlan)...)
	}
	if command := continuationCommand(name, c.operation, c.plan, c.states); command != "" {
		return command
	}
	return contextCommand(name, string(c.operation.Verb))
}

// settle records what one block proved and frees what it held.
func (c *scheduler) settle(finished step) {
	delete(c.running, finished.block)
	for key, holder := range c.held {
		if holder == finished.block {
			delete(c.held, key)
		}
	}
	c.states[finished.block] = finished.state
	if finished.unstarted {
		c.unstarted[finished.block] = true
	}
	if finished.announced {
		c.announced[finished.block] = finished.position
	}
	delete(c.causes, finished.block)
	if finished.err != nil {
		c.causes[finished.block] = finished.err
	}
}

// closeAnnounced settles the row of every announced retry that never started.
func (c *scheduler) closeAnnounced(ctx context.Context) {
	for _, block := range c.plan.Blocks {
		position, ok := c.announced[block.ID]
		if ok && (!c.worked[block.ID] || c.unstarted[block.ID]) {
			c.service.report(ctx, ProgressEvent{Block: block.ID, Description: block.Description, Status: string(c.states[block.ID]), Position: position, Total: len(c.plan.Blocks)})
		}
	}
}

// cause joins what every block that did not complete reported, in frozen plan
// order. Blocks run together, so which one failed first is a race; the plan's
// own order is the one an operator already read.
func (c *scheduler) cause() error {
	var reported []diagnostics.Diagnostic
	only := false
	for _, block := range c.plan.Blocks {
		reported = append(reported, c.reported(block.ID)...)
		only = only || errors.As(c.causes[block.ID], new(onlyExit))
	}
	if len(reported) == 0 {
		return nil
	}
	if only {
		return onlyExit{&diagnostics.Failure{Diagnostics: reported}}
	}
	return &diagnostics.Failure{Diagnostics: reported}
}

// reported is what one block's cause says. A cause that carries no diagnostic,
// such as an operation-store write that failed, is still why its block did not
// complete, so it is reported the way the CLI reports any failure that escaped
// a typed boundary: as runtime.internal, naming the block and never the
// failure's own text, which may carry a private path or a raw store message. A
// cancellation carries none either, and is left for the boundary that
// recognizes it.
func (c *scheduler) reported(block string) []diagnostics.Diagnostic {
	err := c.causes[block]
	if err == nil {
		return nil
	}
	if reported := diagnostics.Of(err); len(reported) != 0 {
		return reported
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	message := "the block " + block + " stopped on a failure that escaped a typed boundary"
	if c.unstarted[block] {
		message = "the attempt of the block " + block + " could not record its start, so it performed no effect"
	}
	return diagnostics.Of(failure("runtime.internal", message, ""))
}
