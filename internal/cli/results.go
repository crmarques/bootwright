package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/secrets"
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
	case "setup", "preflight controller":
		if validControllerReport(result.controller) && successfulControllerReport(path, result.controller) {
			return true, writeControllerReport(out, path, result.controller)
		}
	case "plan":
		if result.lifecyclePlan != nil {
			return true, writeLifecyclePlan(out, result.lifecyclePlan)
		}
	case "apply", "destroy":
		if result.lifecycleOperation != nil {
			return true, writeLifecycleOperation(out, result.lifecycleOperation)
		}
	case "status":
		if result.lifecycleStatus != nil {
			return true, writeLifecycleStatus(out, result.lifecycleStatus, selectedJSON(command))
		}
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
		if result.deletion != nil && result.deletion.Name != "" && result.deletion.Outcome == "deleted" {
			return true, writeContextDelete(out, result.deletion)
		}
	case "secret set", "secret generate", "secret delete":
		if validSecretMutation(path, result.secretMutation) {
			return true, writeSecretMutation(out, path, result.secretMutation)
		}
	case "secret check":
		if availableSecretCheck(result.secretCheck) {
			return true, writeSecretCheck(out, errOut, path, result.secretCheck, nil, 0, selectedJSON(command))
		}
	case "secret list":
		if validSecretList(result.secretList) {
			return true, writeSecretList(out, path, result.secretList, selectedJSON(command))
		}
	case "media add", "media delete":
		if validMediaMutation(result.mediaMutation) {
			return true, writeMediaMutation(out, result.mediaMutation)
		}
	case "media list":
		if result.mediaList != nil {
			return true, writeMediaList(out, path, result.mediaList, selectedJSON(command))
		}
	case "machine list":
		if validMachineList(result.machines) {
			return true, writeMachineList(out, path, result.machines, boolValue(command.Flags(), "silent"), selectedJSON(command))
		}
	case "machine start", "machine stop", "machine restart":
		if validMachinePower(result.power) {
			return true, writeMachinePower(out, path, result.power, selectedJSON(command))
		}
	case "secret show":
		part := secrets.Part(stringValue(command.Flags(), "part"))
		if result.secretReveal != nil && result.secretReveal.Part == part && validSecretPart(part) {
			return true, writeSecretReveal(out, result.secretReveal, part)
		}
	case "secret encryption init", "secret encryption rotate":
		if validEncryptionMutation(result.encryptionMutation) {
			return true, writeEncryptionMutation(out, path, result.encryptionMutation)
		}
	case "secret encryption status":
		if validEncryptionStatus(result.encryptionStatus) {
			return true, writeEncryptionStatus(out, path, result.encryptionStatus, selectedJSON(command))
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

func (r *Runner) writeNegativeSecretCheck(command *cobra.Command, path string, result commandResult, diagnostics []diagnostic) (bool, error) {
	if path != "secret check" || !negativeSecretCheck(result.secretCheck) {
		return false, nil
	}
	return true, writeSecretCheck(r.config.Out, r.config.ErrOut, path, result.secretCheck, diagnostics, 1, selectedJSON(command))
}

// writeExecutedLifecycle presents an operation that ran and then failed. The
// operation summary, its log reference and its receipt are the evidence the
// operator continues from, so a terminal failure keeps them on standard output
// and carries the diagnostics on standard error. A refusal registers no
// operation and therefore has no result to present.
func (r *Runner) writeExecutedLifecycle(path string, result commandResult, diagnostics []diagnostic) (bool, error) {
	if path != "apply" && path != "destroy" {
		return false, nil
	}
	if result.lifecycleOperation == nil {
		return false, nil
	}
	if err := writeLifecycleOperation(r.config.Out, result.lifecycleOperation); err != nil {
		return true, err
	}
	return true, writeDiagnostics(r.config.Out, r.config.ErrOut, path, diagnostics, 1, false)
}
