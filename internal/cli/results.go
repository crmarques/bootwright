package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
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

func (r *Runner) writeResult(ctx context.Context, command *cobra.Command, path string, result commandResult) (bool, error) {
	out, errOut := r.config.Out, r.config.ErrOut
	switch path {
	case "validate":
		if result.validation != nil {
			return true, writeValidation(out, errOut, path, result.validation, selectedJSON(command))
		}
	case "context init", "context update":
		if result.admission != nil && validContextSummary(result.admission.Context) && result.admission.FilesCopied >= 0 && result.admission.Counts.FilesSeen >= 0 && result.admission.Counts.ObjectsDecoded >= 0 {
			return true, writeAdmission(out, errOut, path, result.admission)
		}
	case "context use":
		if result.use != nil && validContextSummary(result.use.Context) {
			return true, writeContextUse(out, result.use)
		}
	case "context list":
		if result.list != nil {
			for _, summary := range result.list.Contexts {
				if !validContextSummary(summary) {
					return false, nil
				}
			}
			return true, writeContextList(out, result.list)
		}
	case "context current":
		if result.current != nil && validContextSummary(result.current.Context) {
			return true, writeContextCurrent(out, result.current, boolValue(command.Flags(), "short"))
		}
	case "context delete":
		if result.deletion != nil && result.deletion.Name != "" && result.deletion.ID != "" && (result.deletion.Outcome == "deleted" || result.deletion.Outcome == "recoveryOnly") {
			return true, writeContextDelete(out, result.deletion)
		}
	case "render effective":
		if result.effective != nil {
			encoder := r.config.EncodeEffectiveYAML
			if selectedJSON(command) {
				encoder = r.config.EncodeEffectiveJSON
			}
			return true, writeEffective(ctx, out, path, result.effective, selectedJSON(command), encoder)
		}
	}
	return false, nil
}
