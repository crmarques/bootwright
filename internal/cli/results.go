package cli

import (
	"context"
	"errors"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// resultFailure identifies a failure before primary output starts. Writer
// failures remain ordinary errors and must never trigger fallback output.
type resultFailure struct {
	code, message string
	exitCode      int
}

func (e *resultFailure) Error() string { return e.message }

func encodingFailure(ctx context.Context, err error) error {
	if errors.Is(context.Cause(ctx), ErrInterrupted) {
		return &resultFailure{"runtime.interrupted", "operation interrupted", 130}
	}
	if errors.Is(err, context.Canceled) {
		return &resultFailure{"runtime.canceled", "operation canceled", 1}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &resultFailure{"runtime.deadline", "operation deadline exceeded", 1}
	}
	return &resultFailure{"runtime.encode", "effective state could not be encoded", 1}
}

func (r commandResult) contextName() string {
	switch {
	case r.lifecycleOperation != nil:
		return r.lifecycleOperation.Context.Name
	case r.lifecyclePlan != nil:
		return r.lifecyclePlan.Context.Name
	case r.lifecycleStatus != nil:
		return r.lifecycleStatus.Context.Name
	}
	return ""
}

// createdLogs preserves each private log reference even when its invocation failed.
func (r commandResult) createdLogs() []string {
	if r.lifecycleOperation != nil {
		return r.lifecycleOperation.Logs
	}
	return powerLogs(r.power)
}

// An executed failure keeps its result, logs and receipt for continuation.
// Human diagnostics use stderr; structured diagnostics share its envelope.
// A refusal registers no operation and has no result.
func (r *Runner) writeExecutedLifecycle(path string, result commandResult, diagnostics []diagnostic) (bool, error) {
	if result.lifecycleOperation == nil {
		return false, nil
	}
	if structuredFormat(presentationFor(r.config.Out).Format) {
		return true, writeStructuredOperation(r.config.Out, path, result.lifecycleOperation, diagnostics, 1)
	}
	if err := writeLifecycleOperation(r.config.Out, result.lifecycleOperation); err != nil {
		return true, err
	}
	return true, writeHumanDiagnostics(r.config.ErrOut, displayDiagnosticsInOrder(append(slices.Clone(result.lifecycleOperation.Warnings), diagnostics...)))
}

// interruptedLifecycle reports an interrupt of an apply or destroy that
// registered its operation. That operation's records already say what it did,
// so its result is still evidence the operator continues from.
func interruptedLifecycle(ctx context.Context, spec commandSpec, result commandResult) bool {
	if !spec.operation || result.lifecycleOperation == nil {
		return false
	}
	operation := result.lifecycleOperation.Receipt.Operation
	return operation != "" && operation != "none" && errors.Is(context.Cause(ctx), ErrInterrupted)
}

// writeInterruptedLifecycle presents the interrupted operation's result, its
// log reference, next command and receipt, then the interrupt alone: what the
// cancellation made each block report is in the result and its logs.
func (r *Runner) writeInterruptedLifecycle(path string, operation *lifecycle.OperationResult) int {
	if structuredFormat(presentationFor(r.config.Out).Format) {
		reported := []diagnostic{{Severity: "error", Code: "runtime.interrupted", Message: "operation interrupted", Remediation: operation.NextCommand}}
		if writeStructuredOperation(r.config.Out, path, operation, reported, 130) != nil {
			return 1
		}
		return 130
	}
	if err := writeLifecycleOperation(r.config.Out, operation); err != nil {
		return 1
	}
	interrupted := append(slices.Clone(operation.Warnings), diagnostic{Severity: "error", Code: "runtime.interrupted", Message: "operation interrupted", Remediation: operation.NextCommand})
	if err := writeDiagnostics(r.config.Out, r.config.ErrOut, path, interrupted, 130, false, nil); err != nil {
		return 1
	}
	return 130
}
