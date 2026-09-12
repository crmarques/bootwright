package reconciliation

import "github.com/crmarques/bootwright/internal/diagnostics"

type OperationState string

const (
	OperationRunning OperationState = "running"
	OperationFailed  OperationState = "failed"
	OperationUnknown OperationState = "unknown"
	OperationDone    OperationState = "done"
)

type BlockState string

const (
	BlockPending BlockState = "pending"
	BlockRunning BlockState = "running"
	BlockFailed  BlockState = "failed"
	BlockUnknown BlockState = "unknown"
	BlockDone    BlockState = "done"
)

type EffectState string

const (
	EffectNoEffect  EffectState = "no-effect"
	EffectCompleted EffectState = "completed"
	EffectUnknown   EffectState = "unknown"
)

// Outcome is what a capability proves about one attempt. A direct apply or
// destroy reporting no effect is a typed failure, never success, so the
// capability contract has no success value that leaves the target untouched.
type Outcome string

const (
	OutcomeChanged   Outcome = "changed"
	OutcomeUnchanged Outcome = "unchanged"
	OutcomeFailed    Outcome = "failed"
	OutcomeCanceled  Outcome = "canceled"
	OutcomeUnknown   Outcome = "unknown"
)

func ValidOperationState(value OperationState) bool {
	switch value {
	case OperationRunning, OperationFailed, OperationUnknown, OperationDone:
		return true
	}
	return false
}

func ValidBlockState(value BlockState) bool {
	switch value {
	case BlockPending, BlockRunning, BlockFailed, BlockUnknown, BlockDone:
		return true
	}
	return false
}

func ValidEffectState(value EffectState) bool {
	switch value {
	case EffectNoEffect, EffectCompleted, EffectUnknown:
		return true
	}
	return false
}

func ValidOutcome(value Outcome) bool {
	switch value {
	case OutcomeChanged, OutcomeUnchanged, OutcomeFailed, OutcomeCanceled, OutcomeUnknown:
		return true
	}
	return false
}

// AttemptTransition maps one reported outcome to the durable effect and block
// states it justifies. Only positive evidence reaches completed or no-effect.
func AttemptTransition(outcome Outcome) (EffectState, BlockState, error) {
	switch outcome {
	case OutcomeChanged, OutcomeUnchanged:
		return EffectCompleted, BlockDone, nil
	case OutcomeFailed:
		return EffectUnknown, BlockFailed, nil
	case OutcomeCanceled, OutcomeUnknown:
		return EffectUnknown, BlockUnknown, nil
	}
	return "", "", stateError("lifecycle attempt outcome is not recognized")
}

// ResolutionTransition applies the resolution table: only durable positive
// evidence moves an unknown attempt, and positive absence is a failure whose
// retry needs the capability's own safe contract.
func ResolutionTransition(observed EffectState) (EffectState, BlockState, OperationState, error) {
	switch observed {
	case EffectCompleted:
		return EffectCompleted, BlockDone, OperationRunning, nil
	case EffectNoEffect:
		return EffectNoEffect, BlockFailed, OperationFailed, nil
	case EffectUnknown:
		return EffectUnknown, BlockUnknown, OperationUnknown, nil
	}
	return "", "", "", stateError("lifecycle resolution evidence is not recognized")
}

// NextOperationState derives the operation from its blocks. An unknown block
// dominates, because no retry, dependent block or replacement may start while
// one effect remains unproved.
func NextOperationState(states []BlockState) (OperationState, error) {
	done, unknown, failed := 0, false, false
	for _, state := range states {
		if !ValidBlockState(state) {
			return "", stateError("lifecycle block state is not recognized")
		}
		switch state {
		case BlockDone:
			done++
		case BlockUnknown:
			unknown = true
		case BlockFailed:
			failed = true
		}
	}
	switch {
	case unknown:
		return OperationUnknown, nil
	case failed:
		return OperationFailed, nil
	case done == len(states):
		return OperationDone, nil
	}
	return OperationRunning, nil
}

// NextBlock selects the block an operation may work on next: the first that is
// not done, in frozen plan order. An unknown block must be resolved first and
// is reported as such rather than skipped.
func NextBlock(plan Plan, states map[string]BlockState) (Block, BlockState, bool) {
	for _, block := range plan.Blocks {
		state := states[block.ID]
		if state == "" {
			state = BlockPending
		}
		if state != BlockDone {
			return cloneBlock(block), state, true
		}
	}
	return Block{}, "", false
}

func stateError(message string) error {
	return diagnostics.NewFailure("lifecycle.state", message, "")
}

func planError(message string) error {
	return diagnostics.NewFailure("lifecycle.state", message, "")
}
