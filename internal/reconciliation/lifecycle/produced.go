package lifecycle

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// capture places what an apply's block produced in custody, in one
// publication through the area the transaction lends, before the block is
// recorded done. Material any other transition offers is discarded, and every
// path clears it; a failure logs only its diagnosis.
func (s Service) capture(ctx context.Context, tx Transaction, boundary *logBoundary, log *operationstore.Log, verb reconciliation.Verb, block reconciliation.Block, state reconciliation.BlockState, produced []Produced) error {
	defer ClearProduced(produced)
	if len(produced) == 0 || verb != reconciliation.Apply || state != reconciliation.BlockDone {
		return nil
	}
	err := s.publishProduced(ctx, tx, block.ID, produced, false)
	for _, reported := range diagnostics.Of(err) {
		_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "custody-failed", Block: block.ID, Detail: reported.Code + ": " + reported.Message})
	}
	return err
}

func (s Service) publishProduced(ctx context.Context, tx Transaction, block string, produced []Produced, unproved bool) error {
	outputs := make([]secretstore.ProducedInput, 0, len(produced))
	names := make(map[string]bool, len(produced))
	for _, output := range produced {
		if !reconciliation.ValidSegment(output.Name) || names[output.Name] {
			return secretstore.Failure("declaration", "a block offered produced material under an invalid or repeated name")
		}
		names[output.Name] = true
		outputs = append(outputs, secretstore.ProducedInput{Name: output.Name, Material: output.Material, Unproved: unproved})
	}
	return tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
		_, err := s.binder.Produce(ctx, selected, area, custody.ProduceRequest{Block: block, Outputs: outputs})
		return err
	})
}

// keepBeforeRemoval moves the copy of produced material a removal would
// delete into custody, marked unproved, before that removal runs, whenever
// custody holds no entry for it, proved or not (D124). The copy is published
// in one publication before the removal starts, so a crash leaves it where it
// was, in custody, or in both, and never in neither. Any failure stops the
// removal before it runs; a removal with no copy to keep runs as it would.
func (s Service) keepBeforeRemoval(ctx context.Context, tx Transaction, boundary *logBoundary, log *operationstore.Log, capability Capability, execution Execution) error {
	keeper, ok := capability.(Keeper)
	if !ok {
		return nil
	}
	entry, keeps, err := keeper.Keeps(execution.Block)
	if err != nil || !keeps {
		return err
	}
	block := execution.Block.ID
	held := false
	err = tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
		var err error
		held, err = s.binder.Holds(ctx, selected, area, entry.Block, entry.Name)
		return err
	})
	if err == nil && !held {
		var published bool
		published, err = s.keep(ctx, tx, keeper, entry, execution)
		if err == nil && published {
			_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "kept-unproved", Block: block, Detail: entry.Block + "/" + entry.Name})
		}
	}
	for _, reported := range diagnostics.Of(err) {
		_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "custody-failed", Block: block, Detail: reported.Code + ": " + reported.Message})
	}
	return err
}

// keep reads the one copy entry names and publishes it, unproved, under the
// block that captures it once proved, and reports whether it published one.
// A copy the removal no longer finds publishes nothing.
func (s Service) keep(ctx context.Context, tx Transaction, keeper Keeper, entry KeptEntry, execution Execution) (bool, error) {
	produced, err := keeper.Keep(ctx, execution)
	defer ClearProduced(produced)
	if err != nil {
		return false, err
	}
	var kept []Produced
	for _, output := range produced {
		if output.Name == entry.Name && len(kept) == 0 {
			kept = append(kept, output)
		}
	}
	if len(kept) == 0 {
		return false, nil
	}
	if err := s.publishProduced(ctx, tx, entry.Block, kept, true); err != nil {
		return false, err
	}
	return true, nil
}

// capturedAttempt is the outcome an attempt records once capture ran: its own
// when custody holds what it produced, canceled when the invocation was, and
// unknown when the publication failed. Capture follows only a proved effect,
// so failed would let a destroy run the inverses that delete the only copy
// without observing this block again; unknown makes the next invocation of
// either verb resolve it, and recapture, first.
func (s Service) capturedAttempt(ctx context.Context, tx Transaction, boundary *logBoundary, log *operationstore.Log, verb reconciliation.Verb, block reconciliation.Block, outcome reconciliation.Outcome, produced []Produced, runErr error) (reconciliation.Outcome, error) {
	_, state, err := reconciliation.AttemptTransition(outcome)
	if err != nil {
		ClearProduced(produced)
		return outcome, runErr
	}
	if err := s.capture(ctx, tx, boundary, log, verb, block, state, produced); err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return reconciliation.OutcomeCanceled, err
		}
		return reconciliation.OutcomeUnknown, err
	}
	return outcome, runErr
}

// withdraw removes every produced entry inside the transaction that records or
// finalizes a completed removal. Nothing else withdraws produced material, so
// a removal that stops part way keeps it.
func (s Service) withdraw(ctx context.Context, tx Transaction) error {
	return tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
		_, err := s.binder.Withdraw(ctx, selected, area)
		return err
	})
}
