package cli

import (
	"context"
	"errors"
	"io"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/spf13/cobra"
)

type Config struct {
	Out                 io.Writer
	ErrOut              io.Writer
	BuildInfo           BuildInfo
	Services            Services
	EncodeEffectiveYAML func(context.Context, api.Catalog) ([]byte, error)
	EncodeEffectiveJSON func(context.Context, api.Catalog) ([]byte, error)
	// BeginOperation derives an invocation context and returns a cleanup that
	// releases and joins its cancellation resources before Run returns.
	BeginOperation func(context.Context) (context.Context, func())
	// FinishProgress runs once the application service returns and before any
	// result or diagnostic is written, so a progress row a terminal is still
	// rewriting is terminated first.
	FinishProgress func()
	// CompletionPaths supplies filesystem candidates for path-valued flags.
	// Completion offers none when it is absent.
	CompletionPaths PathCandidates
}

type Runner struct{ config Config }

type checkedWriter struct {
	writer io.Writer
	err    error
}

func (w *checkedWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func New(config Config) *Runner {
	if config.Out == nil {
		config.Out = io.Discard
	}
	if config.ErrOut == nil {
		config.ErrOut = io.Discard
	}
	return &Runner{config: config}
}

func newCommandTree(r *Runner) (*cobra.Command, error) {
	root := &cobra.Command{Use: "bootwright", Short: "Coordinate declarative day-0 platform bootstrap", SilenceErrors: true, SilenceUsage: true, DisableSuggestions: true}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetOut(r.config.Out)
	root.SetErr(r.config.ErrOut)
	root.SetIn(nil)
	registerFlags(root.PersistentFlags(), globalFlags())
	for _, spec := range commandCatalog() {
		parent := root
		parts := strings.Split(spec.path, " ")
		for _, part := range parts {
			child := exactChild(parent, part)
			if child == nil {
				child = &cobra.Command{Use: part, Short: "Manage " + part, DisableSuggestions: true, SilenceUsage: true, SilenceErrors: true}
				parent.AddCommand(child)
			}
			parent = child
		}
		parent.Short, parent.Long = spec.short, spec.long
		parent.Annotations = map[string]string{"bootwright.command": spec.path}
		if spec.payload {
			parent.Use += " <command>..."
		}
		if spec.path == "help" {
			parent.Use += " [command ...]"
			root.SetHelpCommand(parent)
		}
		registerFlags(parent.Flags(), spec.flags)
		if spec.stopAtPayload {
			parent.Flags().SetInterspersed(false)
		}
		parent.RunE = func(*cobra.Command, []string) error { return nil }
	}
	var catalog completionCatalog
	if r.config.Services.Encryption != nil {
		catalog.secretEncryptionTypes = r.config.Services.Encryption.Types
	}
	catalog.paths = r.config.CompletionPaths
	if err := configureCompletion(root, catalog); err != nil {
		return nil, err
	}
	return root, nil
}

func exactChild(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

func (r *Runner) Run(ctx context.Context, args []string) int {
	output := &checkedWriter{writer: r.config.Out}
	errorsOutput := &checkedWriter{writer: r.config.ErrOut}
	invocationRunner := *r
	invocationRunner.config.Out, invocationRunner.config.ErrOut = output, errorsOutput
	code := invocationRunner.run(ctx, args)
	if output.err != nil || errorsOutput.err != nil {
		return 1
	}
	return code
}

func (r *Runner) run(ctx context.Context, args []string) int {
	root, err := newCommandTree(r)
	if err != nil {
		return r.failure(nil, "", "runtime.internal", "CLI configuration failed", 1, false)
	}
	if message := rawBounds(args); message != "" {
		return r.failure(root, "", "cli.usage", message, 2, false)
	}
	if len(args) > 0 && (args[0] == "__bootwright_complete" || args[0] == "__bootwright_complete_no_desc") {
		command := exactChild(root, args[0])
		command.SetContext(ctx)
		if err := command.RunE(command, args[1:]); err != nil {
			return 1
		}
		return 0
	}
	resolved, err := resolveInvocation(root, args)
	if err != nil {
		path := strings.TrimSpace(strings.TrimPrefix(resolved.command.CommandPath(), "bootwright"))
		return r.failure(resolved.command, path, "cli.usage", trustedResolutionMessage(err), 2, syntaxJSON(resolved.command, resolved.arguments))
	}
	command := resolved.command
	path := strings.TrimPrefix(command.CommandPath(), "bootwright")
	path = strings.TrimSpace(path)
	if err := command.ParseFlags(resolved.arguments); err != nil {
		return r.failure(command, path, "cli.usage", "invalid flag syntax or value", 2, syntaxJSON(command, resolved.arguments))
	}
	if resolved.helpTarget != nil {
		command = resolved.helpTarget
	}
	if resolved.helpTarget != nil || boolValue(command.Flags(), "help") {
		if err := writeHelp(r.config.Out, command); err != nil {
			return 1
		}
		return 0
	}
	if message := validateInvocation(command, path); message != "" {
		return r.failure(command, path, "cli.usage", message, 2, selectedJSON(command))
	}
	if path == "" || path == "completion" || (path == "render" && stringValue(command.Flags(), "input-dir") == "" && stringValue(command.Flags(), "output-dir") == "") {
		if err := writeHelp(r.config.Out, command); err != nil {
			return 1
		}
		return 0
	}
	if command.Annotations["bootwright.command"] == "" {
		return r.failure(command, path, "cli.usage", "a complete command path is required", 2, false)
	}
	if path == "version" {
		if err := writeVersion(r.config.Out, r.config.BuildInfo); err != nil {
			return 1
		}
		return 0
	}
	if strings.HasPrefix(path, "completion ") {
		if err := writeCompletion(r.config.Out, root, strings.TrimPrefix(path, "completion "), boolValue(command.Flags(), "no-descriptions")); err != nil {
			return 1
		}
		return 0
	}
	if r.config.BeginOperation != nil && implementedOperation(path) && ctx.Err() == nil {
		operationContext, finish := r.config.BeginOperation(ctx)
		if finish != nil {
			defer finish()
		}
		if operationContext == nil || finish == nil {
			return r.failure(command, path, "runtime.internal", "operation cancellation is not configured", 1, selectedJSON(command))
		}
		ctx = operationContext
	}
	result, err := r.config.Services.invoke(ctx, path, command.Flags(), command.Flags().Args())
	defer result.clearSensitive()
	logs := result.createdLogs()
	if r.config.FinishProgress != nil {
		r.config.FinishProgress()
	}
	var controllerOutput *controllerOutputFailure
	if errors.As(err, &controllerOutput) {
		return 1
	}
	if err != nil && (path == "setup" || path == "preflight controller") && validControllerReport(result.controller) {
		if presentErr := writeControllerReport(r.config.Out, r.config.ErrOut, path, result.controller); presentErr != nil {
			return 1
		}
	}
	if canceled := ctx.Err(); canceled != nil {
		err = canceled
	}
	if errors.Is(context.Cause(ctx), ErrInterrupted) {
		return r.failureNaming(command, path, "runtime.interrupted", "operation interrupted", 130, selectedJSON(command), logs)
	}
	if errors.Is(err, availability.ErrNotImplemented) {
		return r.failure(command, path, "cli.not-implemented", "bootwright "+path+" is not implemented", 1, selectedJSON(command))
	}
	if errors.Is(err, context.Canceled) {
		return r.failureNaming(command, path, "runtime.canceled", "operation canceled", 1, selectedJSON(command), logs)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return r.failureNaming(command, path, "runtime.deadline", "operation deadline exceeded", 1, selectedJSON(command), logs)
	}
	if diagnostics := diagnostics.Of(err); len(diagnostics) != 0 {
		if handled, presentErr := r.writeNegativeSecretCheck(command, path, result, diagnostics); handled {
			if presentErr != nil {
				return 1
			}
			return 1
		}
		if handled, presentErr := r.writeExecutedLifecycle(path, result, diagnostics); handled {
			if presentErr != nil {
				return 1
			}
			return 1
		}
		if err := writeDiagnostics(r.config.Out, r.config.ErrOut, path, diagnostics, 1, selectedJSON(command), logs); err != nil {
			return 1
		}
		return 1
	}
	// A session's streams and exit status are the remote process's own. There
	// is no result to present and no status to add: the operator has already
	// seen everything the session produced.
	if err == nil && result.session != nil {
		return result.session.ExitCode
	}
	if err == nil {
		handled, presentErr := r.writeResult(ctx, command, path, result)
		var failure *resultFailure
		if errors.As(presentErr, &failure) {
			return r.failure(command, path, failure.code, failure.message, failure.exitCode, selectedJSON(command))
		}
		if presentErr != nil {
			return 1
		}
		if handled {
			return 0
		}
	}
	return r.failureNaming(command, path, "runtime.internal", "application service returned an unsupported result", 1, selectedJSON(command), logs)
}

func selectedJSON(command *cobra.Command) bool {
	return stringValue(command.Flags(), "output") == "json"
}

func (r *Runner) failure(command *cobra.Command, path, code, message string, exitCode int, jsonMode bool) int {
	return r.failureNaming(command, path, code, message, exitCode, jsonMode, nil)
}

// failureNaming is failure for an invocation that may have created private
// logs before it failed, so its envelope still lists them.
func (r *Runner) failureNaming(command *cobra.Command, path, code, message string, exitCode int, jsonMode bool, logs []string) int {
	if err := writeFailure(r.config.Out, r.config.ErrOut, path, code, message, exitCode, jsonMode, logs); err != nil {
		return 1
	}
	if exitCode == 2 && !jsonMode && command != nil {
		if err := writeConciseHelp(r.config.ErrOut, command); err != nil {
			return 1
		}
	}
	return exitCode
}
