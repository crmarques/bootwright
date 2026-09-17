package lifecycle

import (
	"context"
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
}

// scheduler starts every block the frozen plan admits, up to the bound, and
// re-evaluates as each one settles. It owns no durable state: the block
// records remain the truth, and this decides only what runs next.
type scheduler struct {
	service   Service
	tx        Transaction
	store     OperationStore
	approved  bundle
	operation operationstore.Operation
	plan      reconciliation.Plan
	material  map[string]secrets.Material
	selection reconciliation.StageSelection
	bound     int

	states map[string]reconciliation.BlockState
	// running are the blocks this invocation has in flight. It is deliberately
	// separate from the durable running state, which means an executor died
	// mid-attempt and is an unproved effect rather than work in progress.
	running map[string]bool
	// worked names every block this invocation already attempted or observed,
	// so a block it failed is not retried by the same invocation that failed
	// it and one it left unproved is not observed twice.
	worked map[string]bool
	// held maps each exclusive resource in use to the block using it.
	held    map[string]string
	causes  map[string]error
	results chan step
	// observeOnly admits resolutions and nothing else. A removal uses it to
	// prove the outcome of the effects it is about to take back, which is
	// read-only and may start no attempt of the operation it observes.
	observeOnly bool
}

// converge runs the operation's blocks until nothing more may start, and
// reports whether it stopped at a stage boundary and what any block that did
// not complete reported.
func (s Service) converge(ctx context.Context, tx Transaction, store OperationStore, approved bundle, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, material map[string]secrets.Material, selection reconciliation.StageSelection) (bool, error) {
	return s.newScheduler(tx, store, approved, operation, plan, states, material, selection).converge(ctx)
}

// observe resolves every unproved effect of an operation and starts nothing
// else, leaving the block states it was given holding what each one proved.
func (s Service) observe(ctx context.Context, tx Transaction, store OperationStore, approved bundle, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, material map[string]secrets.Material) error {
	run := s.newScheduler(tx, store, approved, operation, plan, states, material, reconciliation.StageSelection{})
	run.observeOnly = true
	_, err := run.converge(ctx)
	return err
}

func (s Service) newScheduler(tx Transaction, store OperationStore, approved bundle, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, material map[string]secrets.Material, selection reconciliation.StageSelection) *scheduler {
	bound := s.options.Concurrency
	if bound <= 0 {
		bound = MaxRunningBlocks
	}
	return &scheduler{
		service: s, tx: tx, store: store, approved: approved, operation: operation,
		plan: plan, material: material, selection: selection, bound: bound,
		states: states, running: map[string]bool{}, worked: map[string]bool{},
		held: map[string]string{}, causes: map[string]error{},
		results: make(chan step, len(plan.Blocks)),
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
	if ctx.Err() != nil {
		return false, c.cause()
	}
	return pendingRemains(c.plan, c.states), c.cause()
}

// admit starts every block the durable state and the bound allow, so a wave of
// independent work goes out together rather than one block at a time.
func (c *scheduler) admit(ctx context.Context) {
	for len(c.running) < c.bound {
		block, position, ok := c.next()
		if !ok {
			return
		}
		c.start(ctx, block, position)
	}
}

// next chooses the one block admission may start now. An unproved effect is
// observed before anything else and admits nothing beside another observation,
// because no retry, dependent block or removal may start while an effect's
// outcome is unproved. A failed block is then the only retry candidate and
// runs alone, because what follows it depends on it succeeding. Otherwise the
// first startable block in frozen order runs, provided nothing already running
// holds a resource it needs to itself.
func (c *scheduler) next() (reconciliation.Block, int, bool) {
	for index, block := range c.plan.Blocks {
		if unproved(c.states[block.ID]) && !c.running[block.ID] && !c.worked[block.ID] {
			return block, index, true
		}
	}
	if c.observeOnly || c.anyState(unproved) {
		return reconciliation.Block{}, 0, false
	}
	failed := func(state reconciliation.BlockState) bool { return state == reconciliation.BlockFailed }
	for index, block := range c.plan.Blocks {
		if failed(c.states[block.ID]) && !c.running[block.ID] && !c.worked[block.ID] {
			return block, index, true
		}
	}
	if c.anyState(failed) {
		return reconciliation.Block{}, 0, false
	}
	startable := reconciliation.Startable(c.plan, c.states, c.selection)
	for index, block := range c.plan.Blocks {
		if c.running[block.ID] || !c.free(block) {
			continue
		}
		if slices.ContainsFunc(startable, func(ready reconciliation.Block) bool { return ready.ID == block.ID }) {
			return block, index, true
		}
	}
	return reconciliation.Block{}, 0, false
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
	c.worked[block.ID] = true
	for _, key := range block.Exclusive {
		c.held[key] = block.ID
	}
	observing := unproved(c.states[block.ID])
	go func() {
		var state reconciliation.BlockState
		var err error
		if observing {
			// The observation's durable transition is authoritative even when
			// it reports a refusal, so the operation state matches the record.
			state, err = c.service.resolveUnknown(ctx, c.tx, c.store, c.approved, c.operation, block, c.material, position+1, len(c.plan.Blocks))
			if state == "" {
				state = reconciliation.BlockUnknown
			}
		} else {
			state, err = c.service.attempt(ctx, c.tx, c.store, c.approved, c.operation, block, c.material, position+1, len(c.plan.Blocks))
		}
		c.results <- step{block: block.ID, state: state, err: err}
	}()
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
	if finished.err != nil {
		c.causes[finished.block] = finished.err
	}
}

// cause joins what every block that did not complete reported, in frozen plan
// order. Blocks run together, so which one failed first is a race; the plan's
// own order is the one an operator already read.
func (c *scheduler) cause() error {
	var reported []diagnostics.Diagnostic
	for _, block := range c.plan.Blocks {
		reported = append(reported, diagnostics.Of(c.causes[block.ID])...)
	}
	if len(reported) == 0 {
		return nil
	}
	return &diagnostics.Failure{Diagnostics: reported}
}
