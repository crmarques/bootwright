package cli

import (
	"context"
	"errors"
	"io"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
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
	if selectedStructured(command) {
		if handled, err := writeAdditionalStructuredResult(out, command, path, result); handled {
			return true, err
		}
	}
	switch path {
	case "add-ons list", "add-ons info":
		if handled, err := writeAddOnResult(out, path, result, selectedStructured(command)); handled {
			return true, err
		}
	case "preflight storage-cluster", "preflight container-cluster", "preflight clusters", "preflight infra", "preflight all":
		if result.storagePreflight != nil {
			return true, writeStoragePreflight(out, errOut, path, result.storagePreflight, nil, 0, selectedJSON(command))
		}
	case "controller install", "preflight controller", "controller rebind", "controller uninstall":
		return writeControllerOutcome(out, errOut, path, result, selectedJSON(command))
	case "plan", "status", "diff":
		return r.writeLifecycleInspection(ctx, command, path, result)
	case "apply", "destroy":
		if result.lifecycleOperation != nil {
			if err := writeHumanDiagnostics(errOut, displayDiagnostics(result.lifecycleOperation.Warnings)); err != nil {
				return true, err
			}
			return true, writeLifecycleOperation(out, result.lifecycleOperation)
		}
	case "validate":
		if result.validation != nil {
			return true, writeValidation(out, errOut, path, result.validation, selectedJSON(command))
		}
	case "context init", "context update", "context use", "context list", "context current", "context delete":
		return r.writeContextResult(command, path, result)
	case "secret set", "secret generate", "secret delete":
		if validSecretMutation(path, result.secretMutation) {
			return true, writeSecretMutation(out, path, result.secretMutation)
		}
	case "secret check":
		if availableSecretCheck(result.secretCheck) {
			return true, writeSecretCheck(out, errOut, path, result.secretCheck, nil, 0, selectedJSON(command))
		}
	case "secret info":
		if validSecretList(result.secretList) {
			return true, writeSecretInfo(out, path, result.secretList, selectedStructured(command))
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
	case "machine info":
		if validMachineList(result.machines) {
			return true, writeMachineInfo(out, path, result.machines, selectedStructured(command))
		}
	case "machine list":
		if validMachineList(result.machines) {
			return true, writeMachineList(out, path, result.machines, boolValue(command.Flags(), "short"), selectedJSON(command))
		}
	case "machine trust":
		if validTrustReport(result.trust) {
			return true, writeTrustReport(out, path, result.trust, selectedJSON(command))
		}
	case "machine start", "machine stop", "machine restart":
		if validMachinePower(result.power, false) {
			return true, writeMachinePower(out, path, result.power, selectedJSON(command))
		}
	case "cluster list", "cluster info":
		return writeClusterResult(out, errOut, path, result, selectedJSON(command))
	case "secret show", "cluster kubeconfig", "cluster keyring":
		return writeReveal(out, errOut, command, path, result)
	case "secret encryption init", "secret encryption rotate":
		if validEncryptionMutation(result.encryptionMutation) {
			return true, writeEncryptionMutation(out, path, result.encryptionMutation)
		}
	case "secret encryption status":
		if validEncryptionStatus(result.encryptionStatus) {
			return true, writeEncryptionStatus(out, path, result.encryptionStatus, selectedJSON(command))
		}
	case "render":
		if validRender(result.rendered) {
			return true, writeRender(out, result.rendered)
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

func (r *Runner) writeContextResult(command *cobra.Command, path string, result commandResult) (bool, error) {
	out, errOut := r.config.Out, r.config.ErrOut
	switch path {
	case "context init", "context update":
		if validAdmission(result.admission) {
			return true, writeAdmission(out, errOut, path, result.admission)
		}
	case "context use":
		if result.use != nil && validContextSummary(result.use.Context) {
			return true, writeContextUse(out, result.use)
		}
	case "context list":
		if validContextList(result.list) {
			return true, writeContextList(out, result.list)
		}
	case "context current":
		if result.current != nil && validContextSummary(result.current.Context) {
			return true, writeContextCurrent(out, result.current, boolValue(command.Flags(), "short"))
		}
	case "context delete":
		if result.deletion != nil && result.deletion.Name != "" && result.deletion.Outcome == "deleted" {
			return true, writeContextDelete(out, errOut, result.deletion)
		}
	}
	return false, nil
}

func (r *Runner) writeLifecycleInspection(ctx context.Context, command *cobra.Command, path string, result commandResult) (bool, error) {
	out := r.config.Out
	switch {
	case path == "plan" && result.lifecyclePlan != nil:
		if err := writeHumanDiagnostics(r.config.ErrOut, displayDiagnostics(result.lifecyclePlan.Warnings)); err != nil {
			return true, err
		}
		return true, writeLifecyclePlan(out, result.lifecyclePlan)
	case path == "status" && result.lifecycleStatus != nil:
		if !selectedJSON(command) {
			if err := writeHumanDiagnostics(r.config.ErrOut, displayDiagnostics(result.lifecycleStatus.Warnings)); err != nil {
				return true, err
			}
		}
		return true, writeLifecycleStatus(out, result.lifecycleStatus, selectedJSON(command))
	case path == "diff" && result.lifecycleDiff != nil:
		status, err := writeDiffStatus(ctx, out, result.lifecycleDiff, selectedJSON(command), boolValue(command.Flags(), "exit-code"), r.config.EncodeEffectiveYAML, r.config.EncodeEffectiveJSON)
		r.resultStatus = status
		return true, err
	}
	return false, nil
}

// writeReveal writes an explicit sensitive result: the exact requested bytes,
// with no added LF and nothing else.
func writeReveal(out, errOut io.Writer, command *cobra.Command, path string, result commandResult) (bool, error) {
	if path == "cluster kubeconfig" {
		if result.kubeconfig == nil {
			return false, nil
		}
		return true, writeKubeconfig(out, errOut, result.kubeconfig)
	}
	if path == "cluster keyring" {
		if result.keyring == nil {
			return false, nil
		}
		return true, writeKeyring(out, errOut, result.keyring)
	}
	part := secrets.Part(stringValue(command.Flags(), "part"))
	if result.secretReveal != nil && result.secretReveal.Part == part && validSecretPart(part) {
		return true, writeSecretReveal(out, result.secretReveal, part)
	}
	return false, nil
}

// createdLogs preserves each private log reference even when its invocation failed.
func (r commandResult) createdLogs() []string {
	if r.lifecycleOperation != nil {
		return r.lifecycleOperation.Logs
	}
	return powerLogs(r.power)
}

func (r *Runner) writeNegativeSecretCheck(command *cobra.Command, path string, result commandResult, diagnostics []diagnostic) (bool, error) {
	if path != "secret check" || !negativeSecretCheck(result.secretCheck) {
		return false, nil
	}
	return true, writeSecretCheck(r.config.Out, r.config.ErrOut, path, result.secretCheck, diagnostics, 1, selectedJSON(command))
}

// An executed failure keeps its result, logs and receipt for continuation.
// Human diagnostics use stderr; structured diagnostics share its envelope.
// A refusal registers no operation and has no result.
func (r *Runner) writeExecutedLifecycle(path string, result commandResult, diagnostics []diagnostic) (bool, error) {
	if path != "apply" && path != "destroy" {
		return false, nil
	}
	if result.lifecycleOperation == nil {
		return false, nil
	}
	if structuredFormat(presentationFor(r.config.Out).Format) {
		return true, writeStructuredOperation(r.config.Out, path, result.lifecycleOperation, diagnostics, 1)
	}
	if err := writeLifecycleOperation(r.config.Out, result.lifecycleOperation); err != nil {
		return true, err
	}
	return true, writeDiagnostics(r.config.Out, r.config.ErrOut, path, append(slices.Clone(result.lifecycleOperation.Warnings), diagnostics...), 1, false, nil)
}

// interruptedLifecycle reports an interrupt of an apply or destroy that
// registered its operation. That operation's records already say what it did,
// so its result is still evidence the operator continues from.
func interruptedLifecycle(ctx context.Context, path string, result commandResult) bool {
	if (path != "apply" && path != "destroy") || result.lifecycleOperation == nil {
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

func writeControllerOutcome(out, errOut io.Writer, path string, result commandResult, jsonMode bool) (bool, error) {
	switch {
	case validRebind(result.rebind):
		return true, writeRebind(out, result.rebind)
	case path == "controller uninstall" && validUninstall(result.uninstall):
		return true, writeUninstall(out, result.uninstall)
	case path == "controller install" && validSetupDownload(result.controller):
		return true, writeSetupDownload(out, result.controller)
	case validControllerReport(result.controller) && successfulControllerReport(path, result.controller):
		return true, writeControllerResult(out, errOut, path, result.controller, jsonMode)
	}
	return false, nil
}
