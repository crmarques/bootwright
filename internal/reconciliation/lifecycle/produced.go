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
	err := s.publishProduced(ctx, tx, block.ID, produced)
	for _, reported := range diagnostics.Of(err) {
		_ = boundary.append(ctx, log, operationstore.LogRecord{Event: "custody-failed", Block: block.ID, Detail: reported.Code + ": " + reported.Message})
	}
	return err
}

func (s Service) publishProduced(ctx context.Context, tx Transaction, block string, produced []Produced) error {
	outputs := make([]secretstore.ProducedInput, 0, len(produced))
	names := make(map[string]bool, len(produced))
	for _, output := range produced {
		if !reconciliation.ValidSegment(output.Name) || names[output.Name] {
			return secretstore.Failure("declaration", "a block offered produced material under an invalid or repeated name")
		}
		names[output.Name] = true
		outputs = append(outputs, secretstore.ProducedInput{Name: output.Name, Material: output.Material})
	}
	return tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
		_, err := s.binder.Produce(ctx, selected, area, custody.ProduceRequest{Block: block, Outputs: outputs})
		return err
	})
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
